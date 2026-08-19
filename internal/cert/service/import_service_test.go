package service

import (
	"context"
	"crypto/x509"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// newSvc 构造基于内存假实现的被测服务（含其信封加密句柄，供落库密文回读校验）。
func newSvc(t *testing.T) (ImportService, *certtest.FakeCertificateRepo, *certtest.FakeBatchSessionRepo, *domain.EnvelopeCrypto) {
	t.Helper()
	certs := certtest.NewFakeCertificateRepo()
	batches := certtest.NewFakeBatchSessionRepo()
	crypto := certtest.NewTestCrypto(t)
	return NewImportService(certs, batches, crypto), certs, batches, crypto
}

// waitForTerminal 轮询会话直至终态（completed/partial_failed），超时失败。
func waitForTerminal(t *testing.T, batches *certtest.FakeBatchSessionRepo, batchID string) domain.CertBatchSession {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sess, err := batches.GetByID(context.Background(), batchID)
		require.NoError(t, err)
		if sess.Status != domain.BatchSessionRunning {
			return sess
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("batch session did not reach terminal state within deadline")
	return domain.CertBatchSession{}
}

// decryptedKey 断言辅助：解密存储密文并与明文私钥比对后清零。
func decryptedKey(t *testing.T, secret *domain.EncryptedSecret, c *domain.EnvelopeCrypto) []byte {
	t.Helper()
	require.NotNil(t, secret)
	pt, err := c.Decrypt(secret.Ciphertext, secret.KeyVersion)
	require.NoError(t, err)
	return pt
}

// ---- AC1：单张导入（complete / fingerprint_only / expectedDomain 提示比对）----

func TestImportCertComplete(t *testing.T) {
	svc, certs, _, crypto := newSvc(t)
	b := certtest.NewBundle(t, "www.example.com", []string{"www.example.com", "api.example.com"}, nil)

	res, err := svc.ImportCert(context.Background(), b.CertPEM, b.KeyPEM, "")
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusComplete, res.HostingStatus)
	assert.Equal(t, b.Fingerprint, res.Fingerprint)
	assert.NotEmpty(t, res.CertID)
	assert.Empty(t, res.ExpectedDomainMissing, "无 expectedDomain 时无比对提示")

	stored, err := certs.GetByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusComplete, stored.HostingStatus)
	require.NotNil(t, stored.EncryptedPrivateKey)
	assert.Equal(t, domain.AlgoAES256GCM, stored.EncryptedPrivateKey.Algo)
	assert.NotEqual(t, string(b.KeyPEM), stored.EncryptedPrivateKey.Ciphertext, "密文不得等于明文私钥")
	assert.NotContains(t, stored.EncryptedPrivateKey.Ciphertext, "PRIVATE KEY")
	assert.Equal(t, b.CN, stored.CommonName)
	assert.Equal(t, []string{"www.example.com", "api.example.com"}, stored.Sans)
	assert.Contains(t, stored.Issuer, "certtest Intermediate CA")
	assert.Equal(t, domain.KeyAlgorithmECDSA, stored.KeyAlgorithm)
	assert.Equal(t, string(b.CertPEM), stored.CertPEM, "证书束原文落库（补传匹配校验依据）")

	// 落库密文可解密回原私钥（信封加密回读校验）
	pt := decryptedKey(t, stored.EncryptedPrivateKey, crypto)
	assert.Equal(t, b.KeyPEM, pt)
	domain.Zeroize(&pt)
}

func TestImportCertFingerprintOnly(t *testing.T) {
	svc, certs, _, _ := newSvc(t)
	b := certtest.NewBundle(t, "fp.example.com", []string{"fp.example.com"}, nil)

	res, err := svc.ImportCert(context.Background(), b.CertPEM, nil, "")
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, res.HostingStatus)

	stored, err := certs.GetByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, stored.HostingStatus)
	assert.Nil(t, stored.EncryptedPrivateKey)
	assert.NotEmpty(t, stored.CertPEM, "仅指纹登记同样保留证书束（补传校验依据）")
}

func TestImportCertExpectedDomainAdvisory(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	b := certtest.NewBundle(t, "adv.example.com", []string{"www.example.com", "*.wild.example.com"}, nil)

	// 不一致 → 提示，不拦截
	res, err := svc.ImportCert(context.Background(), b.CertPEM, nil, "blog.example.com")
	require.NoError(t, err, "expectedDomain 不一致不得拦截导入")
	assert.Equal(t, []string{"blog.example.com"}, res.ExpectedDomainMissing)

	// 通配符覆盖 → 无提示
	res, err = svc.ImportCert(context.Background(),
		certtest.NewBundle(t, "adv2.example.com", []string{"*.wild.example.com"}, nil).CertPEM, nil, "a.wild.example.com")
	require.NoError(t, err)
	assert.Empty(t, res.ExpectedDomainMissing)

	// 精确命中 → 无提示
	res, err = svc.ImportCert(context.Background(),
		certtest.NewBundle(t, "adv3.example.com", []string{"www.example.com"}, nil).CertPEM, nil, "www.example.com")
	require.NoError(t, err)
	assert.Empty(t, res.ExpectedDomainMissing)
}

// ---- AC2：校验失败映射（四类拦截，错误码断言）----

func TestImportCertValidationErrors(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	b := certtest.NewBundle(t, "val.example.com", []string{"val.example.com"}, nil)

	tests := []struct {
		name    string
		certPEM []byte
		keyPEM  []byte
		code    string
	}{
		{"key mismatch", b.CertPEM, certtest.NewKeyPEM(t), domain.CodeCertKeyMismatch},
		{"chain incomplete", b.LeafOnlyPEM(), nil, domain.CodeCertChainIncomplete},
		{"parse fail garbage", []byte("not a pem"), nil, domain.CodeCertParseFail},
		{"parse fail expired", certtest.NewBundle(t, "exp.example.com", []string{"exp.example.com"},
			func(c *x509.Certificate) {
				c.NotBefore = time.Now().Add(-48 * time.Hour)
				c.NotAfter = time.Now().Add(-24 * time.Hour)
			}).CertPEM, nil, domain.CodeCertParseFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ImportCert(context.Background(), tt.certPEM, tt.keyPEM, "")
			require.Error(t, err)
			ce, ok := domain.AsCertError(err)
			require.True(t, ok, "error must carry CertError: %v", err)
			assert.Equal(t, tt.code, ce.Code())
		})
	}
}

func TestImportCertDuplicateFingerprint(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	b := certtest.NewBundle(t, "dup.example.com", []string{"dup.example.com"}, nil)

	_, err := svc.ImportCert(context.Background(), b.CertPEM, nil, "")
	require.NoError(t, err)
	_, err = svc.ImportCert(context.Background(), b.CertPEM, b.KeyPEM, "")
	require.ErrorIs(t, err, domain.ErrDuplicateFingerprint)
}

// ---- AC3：批量导入（逐文件隔离 + 会话持久化 + 终态收敛）----

func TestImportBatchMixedResults(t *testing.T) {
	svc, certs, batches, _ := newSvc(t)
	good1 := certtest.NewBundle(t, "b1.example.com", []string{"b1.example.com"}, nil)
	good2 := certtest.NewBundle(t, "b2.example.com", []string{"b2.example.com"}, nil)

	batchID, err := svc.ImportBatch(context.Background(), []BatchFileInput{
		{FileName: "a.crt", CertPEM: good1.CertPEM, KeyPEM: good1.KeyPEM},
		{FileName: "b.crt", CertPEM: []byte("garbage")},
		{FileName: "c.crt", CertPEM: good2.CertPEM},
	}, "op-user")
	require.NoError(t, err)
	assert.NotEmpty(t, batchID)

	sess := waitForTerminal(t, batches, batchID)
	assert.Equal(t, domain.BatchSessionPartialFailed, sess.Status)
	assert.Equal(t, domain.BatchProgress{Total: 3, Done: 2, Failed: 1}, sess.Progress)
	require.Len(t, sess.Files, 3)

	assert.Equal(t, domain.BatchFileSuccess, sess.Files[0].Result)
	assert.NotEmpty(t, sess.Files[0].CertID)
	assert.Equal(t, domain.BatchFileFailed, sess.Files[1].Result)
	assert.Contains(t, sess.Files[1].ErrorReason, domain.CodeCertParseFail)
	assert.NotContains(t, sess.Files[1].ErrorReason, "PRIVATE KEY", "errorReason 不得泄露私钥材料")
	assert.Equal(t, domain.BatchFileSuccess, sess.Files[2].Result)
	assert.NotNil(t, sess.FinishedAt)

	// 成功文件均入库（单文件失败不阻塞其他文件）
	for _, b := range []*certtest.CertBundle{good1, good2} {
		_, err := certs.GetByFingerprint(context.Background(), b.Fingerprint)
		require.NoError(t, err, "成功文件 %s 应入库", b.CN)
	}
	// 会话 operator 落档
	got, err := batches.GetByID(context.Background(), batchID)
	require.NoError(t, err)
	assert.Equal(t, "op-user", got.Operator)
}

func TestImportBatchAllSuccess(t *testing.T) {
	svc, _, batches, _ := newSvc(t)
	b1 := certtest.NewBundle(t, "ok1.example.com", []string{"ok1.example.com"}, nil)
	b2 := certtest.NewBundle(t, "ok2.example.com", []string{"ok2.example.com"}, nil)

	batchID, err := svc.ImportBatch(context.Background(), []BatchFileInput{
		{FileName: "a.crt", CertPEM: b1.CertPEM},
		{FileName: "b.crt", CertPEM: b2.CertPEM},
	}, "op")
	require.NoError(t, err)

	sess := waitForTerminal(t, batches, batchID)
	assert.Equal(t, domain.BatchSessionCompleted, sess.Status)
	assert.Equal(t, domain.BatchProgress{Total: 2, Done: 2, Failed: 0}, sess.Progress)
}

// Hard Rule：一个文件处理 panic 不得中断会话（recover + errorReason 落档）。
func TestImportBatchPanicIsolation(t *testing.T) {
	certs := certtest.NewFakeCertificateRepo()
	certs.PanicOnCN = "boom.example.com"
	batches := certtest.NewFakeBatchSessionRepo()
	svc := NewImportService(certs, batches, certtest.NewTestCrypto(t))

	boom := certtest.NewBundle(t, "boom.example.com", []string{"boom.example.com"}, nil)
	good := certtest.NewBundle(t, "fine.example.com", []string{"fine.example.com"}, nil)

	batchID, err := svc.ImportBatch(context.Background(), []BatchFileInput{
		{FileName: "boom.crt", CertPEM: boom.CertPEM},
		{FileName: "fine.crt", CertPEM: good.CertPEM},
	}, "op")
	require.NoError(t, err)

	sess := waitForTerminal(t, batches, batchID)
	assert.Equal(t, domain.BatchSessionPartialFailed, sess.Status)
	require.Len(t, sess.Files, 2)
	assert.Equal(t, domain.BatchFileFailed, sess.Files[0].Result)
	assert.Contains(t, sess.Files[0].ErrorReason, "INTERNAL_ERROR")
	assert.Equal(t, domain.BatchFileSuccess, sess.Files[1].Result, "panic 文件之后的文件仍被处理")
}

func TestImportBatchEmpty(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	_, err := svc.ImportBatch(context.Background(), nil, "op")
	assert.ErrorIs(t, err, ErrEmptyBatch)
}

// ---- AC4：会话查询 ----

func TestGetBatchSession(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	b := certtest.NewBundle(t, "q.example.com", []string{"q.example.com"}, nil)
	batchID, err := svc.ImportBatch(context.Background(),
		[]BatchFileInput{{FileName: "a.crt", CertPEM: b.CertPEM}}, "op")
	require.NoError(t, err)

	_, err = svc.GetBatchSession(context.Background(), batchID)
	require.NoError(t, err)

	_, err = svc.GetBatchSession(context.Background(), "000000000000000000000000")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	_, err = svc.GetBatchSession(context.Background(), "not-hex")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

// ---- AC5：补传私钥升级 ----

func TestUploadKeyUpgrade(t *testing.T) {
	svc, certs, _, crypto := newSvc(t)
	b := certtest.NewBundle(t, "up.example.com", []string{"up.example.com"}, nil)

	res, err := svc.ImportCert(context.Background(), b.CertPEM, nil, "")
	require.NoError(t, err)

	up, err := svc.UploadKey(context.Background(), res.CertID, b.KeyPEM)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusComplete, up.HostingStatus)
	assert.Equal(t, b.Fingerprint, up.Fingerprint)
	assert.Equal(t, res.CertID, up.CertID)

	stored, err := certs.GetByID(context.Background(), res.CertID)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusComplete, stored.HostingStatus)
	pt := decryptedKey(t, stored.EncryptedPrivateKey, crypto)
	assert.Equal(t, b.KeyPEM, pt, "落库密文应可解密回原私钥")
	domain.Zeroize(&pt)
}

func TestUploadKeyMismatch(t *testing.T) {
	svc, certs, _, _ := newSvc(t)
	b := certtest.NewBundle(t, "mm.example.com", []string{"mm.example.com"}, nil)

	res, err := svc.ImportCert(context.Background(), b.CertPEM, nil, "")
	require.NoError(t, err)

	_, err = svc.UploadKey(context.Background(), res.CertID, certtest.NewKeyPEM(t))
	require.Error(t, err)
	ce, ok := domain.AsCertError(err)
	require.True(t, ok)
	assert.Equal(t, domain.CodeCertKeyMismatch, ce.Code())
	assert.True(t, strings.HasPrefix(err.Error(), "cert: "), "错误消息保持静态文案")

	stored, err := certs.GetByID(context.Background(), res.CertID)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, stored.HostingStatus, "校验失败不得升级")
	assert.Nil(t, stored.EncryptedPrivateKey)
}

func TestUploadKeyNotFound(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	b := certtest.NewBundle(t, "nf.example.com", []string{"nf.example.com"}, nil)

	_, err := svc.UploadKey(context.Background(), "000000000000000000000000", b.KeyPEM)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	_, err = svc.UploadKey(context.Background(), "bogus", b.KeyPEM)
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestNewImportServiceRequiresCrypto(t *testing.T) {
	certs := certtest.NewFakeCertificateRepo()
	batches := certtest.NewFakeBatchSessionRepo()
	b := certtest.NewBundle(t, "nc.example.com", []string{"nc.example.com"}, nil)

	svc := NewImportService(certs, batches, nil)
	_, err := svc.ImportCert(context.Background(), b.CertPEM, b.KeyPEM, "")
	require.Error(t, err, "crypto 缺失时带私钥导入必须显式失败（fail-fast）")
}
