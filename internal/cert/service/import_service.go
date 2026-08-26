// Package service 实现证书域业务服务（导入/批量导入/补传私钥，任务 2.2）。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ErrEmptyBatch 批量导入未携带���何证书文件。
var ErrEmptyBatch = errors.New("cert: batch import contains no files")

// errCryptoMissing 信封加密组件缺失（构造期注入失败，fail-fast）。
var errCryptoMissing = errors.New("cert: envelope crypto not configured")

// batchProcessTimeout 单个批量导入会话整体处理时限（防止 goroutine 泄漏）。
const batchProcessTimeout = 10 * time.Minute

// ImportResult 导入/补传成功结果。
// 仅含 certId/fingerprint/hostingStatus 三个响应字段（Hard Rule：导入成功响应
// 仅含这三项）；ExpectedDomainMissing 为 expectedDomain 提示性比对结果，
// 由 web 层放入响应 meta（不拦截、不进 data）。
type ImportResult struct {
	CertID                string
	Fingerprint           string
	HostingStatus         domain.HostingStatus
	ExpectedDomainMissing []string
}

// BatchFileInput 批量导入单文件输入（cert 必填，key 可选：同名 .key/.pem 私钥）。
type BatchFileInput struct {
	FileName string
	CertPEM  []byte
	KeyPEM   []byte
}

// ImportService 证书导入服务（单张/批量/补传私钥）。
type ImportService interface {
	// ImportCert 单张导入：certPEM 必填、keyPEM 可选（缺省走仅指纹登记），
	// expectedDomain 仅提示性比对（CheckSANCover，不拦截）。
	// 校验失败返回携带 CERT_* 错误码的错误；重复指纹返回 domain.ErrDuplicateFingerprint。
	ImportCert(ctx context.Context, certPEM, keyPEM []byte, expectedDomain string) (ImportResult, error)
	// ImportBatch 批量导入：创建 CertBatchSession 会话（status=running、files=pending）
	// 并异步逐文件处理（单文件失败/panic 不中断会话），返回 batchId。
	ImportBatch(ctx context.Context, files []BatchFileInput, operator string) (string, error)
	// GetBatchSession 进度轮询数据源（GET /api/v1/certs/batch/:batchId）。
	GetBatchSession(ctx context.Context, batchID string) (domain.CertBatchSession, error)
	// UploadKey 补传私钥：对台账证书重跑 2.1 匹配校验，通过后信封加密落库并
	// 升级 hostingStatus=complete；不匹配返回 CERT_KEY_MISMATCH。
	UploadKey(ctx context.Context, certID string, keyPEM []byte) (ImportResult, error)
}

type importService struct {
	certs   domain.CertificateRepository
	batches domain.CertBatchSessionRepository
	crypto  *domain.EnvelopeCrypto
}

// NewImportService 创建导入服务；crypto 为空时带私钥路径显式失败（fail-fast，
// 防止明文私钥在无加密组件时绕过信封加密落库）。
func NewImportService(certs domain.CertificateRepository, batches domain.CertBatchSessionRepository, crypto *domain.EnvelopeCrypto) ImportService {
	return &importService{certs: certs, batches: batches, crypto: crypto}
}

// ImportCert 单张导入：解析校验（2.1）→ 信封加密（1.1）→ 落库（重复指纹 409 哨兵）。
func (s *importService) ImportCert(ctx context.Context, certPEM, keyPEM []byte, expectedDomain string) (ImportResult, error) {
	parsed, err := domain.ParseCertAndKey(certPEM, keyPEM)
	if err != nil {
		return ImportResult{}, err
	}

	cert := &domain.Certificate{
		Fingerprint:    parsed.Fingerprint,
		CommonName:     parsed.CommonName,
		Sans:           parsed.Sans,
		Issuer:         parsed.Issuer,
		SerialNumber:   parsed.SerialNumber,
		NotBefore:      parsed.NotBefore,
		NotAfter:       parsed.NotAfter,
		KeyAlgorithm:   parsed.KeyAlgorithm,
		CertPEM:        string(certPEM), // 补传匹配校验与云上传依据；永不出现在 API 响应
		ExpectedDomain: expectedDomain,
	}
	if len(keyPEM) > 0 {
		secret, err := s.encryptKey(keyPEM)
		if err != nil {
			return ImportResult{}, err
		}
		cert.EncryptedPrivateKey = secret
		cert.HostingStatus = domain.HostingStatusComplete
	} else {
		cert.HostingStatus = domain.HostingStatusFingerprintOnly
	}

	if err := s.certs.Create(ctx, cert); err != nil {
		return ImportResult{}, err // uk_fingerprint 冲突 → ErrDuplicateFingerprint
	}

	res := ImportResult{
		CertID:        cert.ID.Hex(),
		Fingerprint:   parsed.Fingerprint,
		HostingStatus: cert.HostingStatus,
	}
	if expectedDomain != "" {
		res.ExpectedDomainMissing = domain.CheckSANCover(parsed.Sans, []string{expectedDomain})
	}
	return res, nil
}

// ImportBatch 批量导入：先持久化会话（浏览器中断后重开 Modal 仍可见结果），
// 再异步逐文件处理。返回 batchId 即 cert_batch_sessions._id。
func (s *importService) ImportBatch(ctx context.Context, files []BatchFileInput, operator string) (string, error) {
	if len(files) == 0 {
		return "", ErrEmptyBatch
	}
	session := &domain.CertBatchSession{
		Files:    make([]domain.BatchSessionFile, len(files)),
		Progress: domain.BatchProgress{Total: len(files)},
		Operator: operator,
	}
	for i, f := range files {
		session.Files[i] = domain.BatchSessionFile{FileName: f.FileName, Result: domain.BatchFilePending}
	}
	batchID, err := s.batches.Create(ctx, session)
	if err != nil {
		return "", err
	}

	// 会话处理不随请求生命周期终止（浏览器中断不影响）；整体限时防泄漏。
	go s.runBatch(batchID, files)
	return batchID, nil
}

// GetBatchSession 会话查询（轮询端点数据源）。
func (s *importService) GetBatchSession(ctx context.Context, batchID string) (domain.CertBatchSession, error) {
	return s.batches.GetByID(ctx, batchID)
}

// UploadKey 补传私钥：以落库证书束重跑 2.1 校验（含私钥匹配），
// 通过后信封加密并原子升级 hostingStatus=complete。
func (s *importService) UploadKey(ctx context.Context, certID string, keyPEM []byte) (ImportResult, error) {
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return ImportResult{}, err // ErrInvalidID / mongo.ErrNoDocuments
	}
	// 存量文档缺证书束（导入路径之外写入）时按解析失败拒绝，不静默放行
	if _, err := domain.ParseCertAndKey([]byte(cert.CertPEM), keyPEM); err != nil {
		return ImportResult{}, err
	}
	secret, err := s.encryptKey(keyPEM)
	if err != nil {
		return ImportResult{}, err
	}
	if err := s.certs.AttachPrivateKey(ctx, certID, secret); err != nil {
		return ImportResult{}, err
	}
	return ImportResult{
		CertID:        certID,
		Fingerprint:   cert.Fingerprint,
		HostingStatus: domain.HostingStatusComplete,
	}, nil
}

// runBatch 逐文件处理会话：单文件失败/panic 落 errorReason 后继续（Hard Rule），
// 全部完成后按失败计数收敛终态（completed / partial_failed）。
func (s *importService) runBatch(batchID string, files []BatchFileInput) {
	ctx, cancel := context.WithTimeout(context.Background(), batchProcessTimeout)
	defer cancel()

	for i := range files {
		s.processBatchFile(ctx, batchID, i, files[i])
		domain.Zeroize(&files[i].KeyPEM) // 明文私钥处理完即清零
	}

	sess, err := s.batches.GetByID(ctx, batchID)
	if err != nil {
		slog.Error("cert batch: load session for finalize failed",
			slog.String("batchId", batchID), slog.Any("err", err))
		return
	}
	status := domain.BatchSessionCompleted
	if sess.Progress.Failed > 0 {
		status = domain.BatchSessionPartialFailed
	}
	if err := s.batches.MarkFinished(ctx, batchID, status); err != nil {
		slog.Error("cert batch: mark finished failed",
			slog.String("batchId", batchID), slog.Any("err", err))
	}
}

// processBatchFile 处理单个文件并记录结果；recover 兜底保证 panic 不中断会话。
func (s *importService) processBatchFile(ctx context.Context, batchID string, idx int, f BatchFileInput) {
	defer func() {
		if r := recover(); r != nil {
			// 静态文案，不携带 panic 值（可能含任意堆数据）
			slog.Error("cert batch: panic during file import",
				slog.String("batchId", batchID), slog.String("fileName", f.FileName))
			_ = s.batches.RecordFileResult(ctx, batchID, idx, domain.BatchFileFailed, "",
				"INTERNAL_ERROR: unexpected failure during import")
		}
	}()

	res, err := s.ImportCert(ctx, f.CertPEM, f.KeyPEM, "")
	if err != nil {
		_ = s.batches.RecordFileResult(ctx, batchID, idx, domain.BatchFileFailed, "", batchErrorReason(err))
		return
	}
	_ = s.batches.RecordFileResult(ctx, batchID, idx, domain.BatchFileSuccess, res.CertID, "")
}

// batchErrorReason 批量失败行 errorReason：错误码 + 完整链文案（保留 wrapped
// 静态细节如 expired at <日期>，与 discoveryParseReason 同口径），不含私钥/密文片段。
func batchErrorReason(err error) string {
	if ce, ok := domain.AsCertError(err); ok {
		return ce.Code() + ": " + err.Error()
	}
	if errors.Is(err, domain.ErrDuplicateFingerprint) {
		return domain.CodeCertDuplicateFingerprint + ": " + err.Error()
	}
	return "INTERNAL_ERROR: " + err.Error()
}

// encryptKey 信封加密私钥（1.1）；crypto 缺失时显式失败。
func (s *importService) encryptKey(keyPEM []byte) (*domain.EncryptedSecret, error) {
	if s.crypto == nil {
		return nil, errCryptoMissing
	}
	ciphertext, keyVersion, err := s.crypto.Encrypt(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("cert: encrypt private key: %w", err)
	}
	return &domain.EncryptedSecret{
		Ciphertext: ciphertext,
		KeyVersion: keyVersion,
		Algo:       domain.AlgoAES256GCM,
	}, nil
}
