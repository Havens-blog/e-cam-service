// Package certtest 提供证书域测试支撑：现场生成证书夹具（标准库，不落盘）与
// 仓储接口的内存假实现，供 service/web 层测试共享（2.2 导入服务与 API 契约测试、
// 2.3 台账查询/删除拦截/统计）。
//
// 硬约束：夹具私钥仅存测试内存；假实现不记录任何明文私钥之外的旁路状态。
package certtest

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 证书夹具（现场生成）
// ---------------------------------------------------------------------

// CertBundle 一套可导入的证书夹具：完整链 PEM + 匹配私钥 PEM（PKCS#8）。
type CertBundle struct {
	CN          string
	CertPEM     []byte // leaf + 中间 CA + 自签根
	KeyPEM      []byte // 匹配私钥（PKCS#8，明文）
	Key         crypto.Signer
	Fingerprint string // SHA256(leaf.Raw) 小写 hex
}

// LeafOnlyPEM 返回仅 leaf 的 PEM（证书链缺失场景）。
func (b *CertBundle) LeafOnlyPEM() []byte {
	block, _ := pem.Decode(b.CertPEM)
	if block == nil {
		return nil
	}
	return pem.EncodeToMemory(block)
}

var (
	fixtureMu      sync.Mutex
	fixtureRoot    *x509.Certificate
	fixtureRootKey crypto.Signer
	fixtureInter   *x509.Certificate
	fixtureInterK  crypto.Signer
	fixtureSerial  uint64
)

// caFixture 共享测试 PKI（自签根 + 中间 CA），所有 leaf 复用；进程内惰性生成。
type caFixture struct {
	root, inter       *x509.Certificate
	rootKey, interKey crypto.Signer
}

// ensureCA 惰性生成共享测试 CA，并发安全。
func ensureCA(t *testing.T) caFixture {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	if fixtureRoot == nil {
		rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("certtest: generate root key: %v", err)
		}
		root := createCert(t, &x509.Certificate{
			SerialNumber:          nextSerial(),
			Subject:               pkix.Name{CommonName: "certtest Root CA", Organization: []string{"e-cam-test"}},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(8760 * time.Hour),
			KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
			IsCA:                  true,
		}, nil, rootKey, rootKey)

		interKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("certtest: generate intermediate key: %v", err)
		}
		inter := createCert(t, &x509.Certificate{
			SerialNumber:          nextSerial(),
			Subject:               pkix.Name{CommonName: "certtest Intermediate CA", Organization: []string{"e-cam-test"}},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(8760 * time.Hour),
			KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
			IsCA:                  true,
		}, root, interKey, rootKey)

		fixtureRoot, fixtureRootKey, fixtureInter, fixtureInterK = root, rootKey, inter, interKey
	}
	return caFixture{root: fixtureRoot, inter: fixtureInter, rootKey: fixtureRootKey, interKey: fixtureInterK}
}

func nextSerial() *big.Int {
	fixtureSerial++
	return big.NewInt(int64(fixtureSerial))
}

// createCert 签发一张证书：subjectKey 为证书主体公钥对应私钥，
// signerKey 为签发者私钥（parent=nil 自签时两者相同）；返回解析后的证书。
func createCert(t *testing.T, tmpl, parent *x509.Certificate, subjectKey, signerKey crypto.Signer) *x509.Certificate {
	t.Helper()
	if parent == nil {
		parent = tmpl
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, subjectKey.Public(), signerKey)
	if err != nil {
		t.Fatalf("certtest: create certificate %q: %v", tmpl.Subject.CommonName, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("certtest: parse generated certificate: %v", err)
	}
	return cert
}

// NewBundle 现场生成完整链证书夹具（ECDSA P256，中间 CA 签发）。
// mutate 可覆盖有效期等模板字段（过期/未生效场景）。
func NewBundle(t *testing.T, cn string, sans []string, mutate func(*x509.Certificate)) *CertBundle {
	t.Helper()
	ca := ensureCA(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("certtest: generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: nextSerial(),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"e-cam-test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(8760 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     sans,
	}
	if mutate != nil {
		mutate(tmpl)
	}
	leaf := createCert(t, tmpl, ca.inter, key, ca.interKey)

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("certtest: marshal pkcs8 key: %v", err)
	}
	sum := sha256.Sum256(leaf.Raw)
	return &CertBundle{
		CN:          cn,
		CertPEM:     concatPEM(leaf.Raw, ca.inter.Raw, ca.root.Raw),
		KeyPEM:      pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		Key:         key,
		Fingerprint: hex.EncodeToString(sum[:]),
	}
}

func concatPEM(blocks ...[]byte) []byte {
	var out []byte
	for _, der := range blocks {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return out
}

// NewKeyPEM 生成一把与任何夹具证书无关的新私钥 PEM（不匹配场景）。
func NewKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("certtest: generate unrelated key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("certtest: marshal unrelated key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// NewTestCrypto 构造测试用信封加密组件（版本 1 随机主密钥）。
func NewTestCrypto(t *testing.T) *domain.EnvelopeCrypto {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("certtest: generate master key: %v", err)
	}
	c, err := domain.NewEnvelopeCrypto(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("certtest: new envelope crypto: %v", err)
	}
	return c
}

// ---------------------------------------------------------------------
// 内存假实现（domain 仓储接口）
// ---------------------------------------------------------------------

// FakeCertificateRepo 证书台账内存假实现。
// PanicOnCN 非空时，Create 遇到同 CN 证书即 panic（批量逐文件隔离 recover 测试注入点）。
type FakeCertificateRepo struct {
	mu        sync.Mutex
	byFP      map[string]*domain.Certificate
	byID      map[string]*domain.Certificate
	PanicOnCN string
}

// NewFakeCertificateRepo 创建空台账假实现。
func NewFakeCertificateRepo() *FakeCertificateRepo {
	return &FakeCertificateRepo{
		byFP: map[string]*domain.Certificate{},
		byID: map[string]*domain.Certificate{},
	}
}

// Create 写入证书（模拟 DEFAULT 填充与指纹唯一约束）。
func (f *FakeCertificateRepo) Create(_ context.Context, cert *domain.Certificate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.PanicOnCN != "" && cert.CommonName == f.PanicOnCN {
		panic(fmt.Sprintf("certtest: injected panic on CN %q", f.PanicOnCN))
	}
	if _, dup := f.byFP[cert.Fingerprint]; dup {
		return domain.ErrDuplicateFingerprint
	}
	if cert.CreatedAt.IsZero() {
		cert.CreatedAt = time.Now()
	}
	if cert.ExpiryAlertLevel == "" {
		cert.ExpiryAlertLevel = domain.ExpiryAlertNone
	}
	if cert.ID.IsZero() {
		cert.ID = primitive.NewObjectID()
	}
	stored := *cert
	f.byFP[cert.Fingerprint] = &stored
	f.byID[cert.ID.Hex()] = &stored
	return nil
}

// GetByFingerprint 按指纹查询；未命中返回 mongo.ErrNoDocuments。
func (f *FakeCertificateRepo) GetByFingerprint(_ context.Context, fingerprint string) (domain.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byFP[fingerprint]
	if !ok {
		return domain.Certificate{}, mongo.ErrNoDocuments
	}
	return *c, nil
}

// GetByID 按文档 ID 查询；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (f *FakeCertificateRepo) GetByID(_ context.Context, id string) (domain.Certificate, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return domain.Certificate{}, fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[oid.Hex()]
	if !ok {
		return domain.Certificate{}, mongo.ErrNoDocuments
	}
	return *c, nil
}

// List 返回全量副本。
func (f *FakeCertificateRepo) List(_ context.Context) ([]domain.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Certificate, 0, len(f.byFP))
	for _, c := range f.byFP {
		out = append(out, *c)
	}
	return out, nil
}

// ListPage 内存实现（与 Mongo 仓储语义一致）：notAfter 升序 + _id 升序稳定排序；
// Search 以不区分大小写子串匹配 commonName/sans/fingerprint（等价 $regex+QuoteMeta+"i"）。
func (f *FakeCertificateRepo) ListPage(_ context.Context, flt domain.CertListFilter, skip, limit int) ([]domain.Certificate, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	matched := make([]domain.Certificate, 0, len(f.byFP))
	for _, c := range f.byFP {
		if fakeCertMatches(c, flt) {
			matched = append(matched, *c)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].NotAfter.Equal(matched[j].NotAfter) {
			return matched[i].NotAfter.Before(matched[j].NotAfter)
		}
		return bytes.Compare(matched[i].ID[:], matched[j].ID[:]) < 0
	})
	total := int64(len(matched))
	if skip > 0 {
		if skip >= len(matched) {
			matched = matched[:0]
		} else {
			matched = matched[skip:]
		}
	}
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, total, nil
}

// fakeCertMatches 单证书是否命中筛选条件。
func fakeCertMatches(c *domain.Certificate, flt domain.CertListFilter) bool {
	if flt.HostingStatus != "" && c.HostingStatus != flt.HostingStatus {
		return false
	}
	if flt.NotAfterFrom != nil && !c.NotAfter.After(*flt.NotAfterFrom) {
		return false
	}
	if flt.NotAfterTo != nil && c.NotAfter.After(*flt.NotAfterTo) {
		return false
	}
	if flt.Search != "" {
		q := strings.ToLower(flt.Search)
		if !strings.Contains(strings.ToLower(c.CommonName), q) &&
			!containsFold(c.Sans, q) &&
			!strings.Contains(strings.ToLower(c.Fingerprint), q) {
			return false
		}
	}
	return true
}

// containsFold 切片中是否存在不区分大小写的子串命中。
func containsFold(values []string, lowerQuery string) bool {
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), lowerQuery) {
			return true
		}
	}
	return false
}

// UpdateExpiryAlertLevel 更新到期分级状态。
func (f *FakeCertificateRepo) UpdateExpiryAlertLevel(_ context.Context, fingerprint string, level domain.ExpiryAlertLevel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byFP[fingerprint]
	if !ok {
		return mongo.ErrNoDocuments
	}
	c.ExpiryAlertLevel = level
	return nil
}

// DeleteByFingerprint 按指纹删除。
func (f *FakeCertificateRepo) DeleteByFingerprint(_ context.Context, fingerprint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byFP[fingerprint]
	if !ok {
		return nil
	}
	delete(f.byFP, fingerprint)
	delete(f.byID, c.ID.Hex())
	return nil
}

// AttachPrivateKey 补传私钥升级：密文写入与 hostingStatus=complete 同步生效。
func (f *FakeCertificateRepo) AttachPrivateKey(_ context.Context, id string, secret *domain.EncryptedSecret) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[oid.Hex()]
	if !ok {
		return mongo.ErrNoDocuments
	}
	c.EncryptedPrivateKey = secret
	c.HostingStatus = domain.HostingStatusComplete
	return nil
}

// SetProtectUntil 设置回滚保护期截止（仅延长不缩短，任务 5.1；未命中无操作）。
func (f *FakeCertificateRepo) SetProtectUntil(_ context.Context, fingerprint string, until time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byFP[fingerprint]
	if !ok {
		return nil
	}
	if c.ProtectUntil == nil || c.ProtectUntil.Before(until) {
		u := until
		c.ProtectUntil = &u
	}
	return nil
}

// FakeBatchSessionRepo 批量导入会话内存假实现。
type FakeBatchSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*domain.CertBatchSession
}

// NewFakeBatchSessionRepo 创建空会话假实现。
func NewFakeBatchSessionRepo() *FakeBatchSessionRepo {
	return &FakeBatchSessionRepo{sessions: map[string]*domain.CertBatchSession{}}
}

// Create 写入会话（模拟 DEFAULT 填充）并返回 batchId。
func (f *FakeBatchSessionRepo) Create(_ context.Context, s *domain.CertBatchSession) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.Status == "" {
		s.Status = domain.BatchSessionRunning
	}
	if s.Files == nil {
		s.Files = []domain.BatchSessionFile{}
	}
	if s.ID.IsZero() {
		s.ID = primitive.NewObjectID()
	}
	stored := *s
	stored.Files = append([]domain.BatchSessionFile(nil), s.Files...)
	f.sessions[s.ID.Hex()] = &stored
	return s.ID.Hex(), nil
}

// GetByID 查询会话；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (f *FakeBatchSessionRepo) GetByID(_ context.Context, id string) (domain.CertBatchSession, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return domain.CertBatchSession{}, fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[oid.Hex()]
	if !ok {
		return domain.CertBatchSession{}, mongo.ErrNoDocuments
	}
	out := *s
	out.Files = append([]domain.BatchSessionFile(nil), s.Files...)
	return out, nil
}

// RecordFileResult 记录单文件结果并递增 progress（与真实仓储原子语义一致）。
func (f *FakeBatchSessionRepo) RecordFileResult(_ context.Context, id string, fileIndex int, result domain.BatchFileResult, certID, errorReason string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[oid.Hex()]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if fileIndex < 0 || fileIndex >= len(s.Files) {
		return fmt.Errorf("certtest: file index %d out of range (%d files)", fileIndex, len(s.Files))
	}
	s.Files[fileIndex].Result = result
	s.Files[fileIndex].CertID = certID
	s.Files[fileIndex].ErrorReason = errorReason
	switch result {
	case domain.BatchFileSuccess:
		s.Progress.Done++
	case domain.BatchFileFailed:
		s.Progress.Failed++
	}
	return nil
}

// MarkFinished 终态收敛。
func (f *FakeBatchSessionRepo) MarkFinished(_ context.Context, id string, status domain.BatchSessionStatus) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[oid.Hex()]
	if !ok {
		return mongo.ErrNoDocuments
	}
	s.Status = status
	now := time.Now()
	s.FinishedAt = &now
	return nil
}

// ---------------------------------------------------------------------
// 扫描快照/引用发现内存假实现（任务 2.3 台账查询）
// ---------------------------------------------------------------------

// FakeScanSnapshotRepo 扫描快照内存假实现。
type FakeScanSnapshotRepo struct {
	mu    sync.Mutex
	snaps []*domain.ScanSnapshot
}

// NewFakeScanSnapshotRepo 创建空快照假实现。
func NewFakeScanSnapshotRepo() *FakeScanSnapshotRepo {
	return &FakeScanSnapshotRepo{}
}

// Create 写入快照（模拟 DEFAULT 填充：startedAt=now、status=running）并返回 ID。
func (f *FakeScanSnapshotRepo) Create(_ context.Context, snap *domain.ScanSnapshot) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if snap.StartedAt.IsZero() {
		snap.StartedAt = time.Now()
	}
	if snap.Status == "" {
		snap.Status = domain.ScanStatusRunning
	}
	if snap.ID.IsZero() {
		snap.ID = primitive.NewObjectID()
	}
	stored := *snap
	stored.CoverageMeta = append([]domain.CoverageMeta(nil), snap.CoverageMeta...)
	f.snaps = append(f.snaps, &stored)
	return stored.ID.Hex(), nil
}

// GetByID 查询快照；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (f *FakeScanSnapshotRepo) GetByID(_ context.Context, id string) (domain.ScanSnapshot, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return domain.ScanSnapshot{}, fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.snaps {
		if s.ID == oid {
			return cloneSnapshot(s), nil
		}
	}
	return domain.ScanSnapshot{}, mongo.ErrNoDocuments
}

// LatestDone 最新成功快照（status=done 中 startedAt 最新）；无成功快照返回 mongo.ErrNoDocuments。
func (f *FakeScanSnapshotRepo) LatestDone(_ context.Context) (domain.ScanSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *domain.ScanSnapshot
	for _, s := range f.snaps {
		if s.Status != domain.ScanStatusDone {
			continue
		}
		if best == nil || s.StartedAt.After(best.StartedAt) {
			best = s
		}
	}
	if best == nil {
		return domain.ScanSnapshot{}, mongo.ErrNoDocuments
	}
	return cloneSnapshot(best), nil
}

// LatestRunning 当前运行中快照（status=running 中 startedAt 最新）；
// 无运行中快照返回 mongo.ErrNoDocuments（任务 3.5 扫描防重）。
func (f *FakeScanSnapshotRepo) LatestRunning(_ context.Context) (domain.ScanSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *domain.ScanSnapshot
	for _, s := range f.snaps {
		if s.Status != domain.ScanStatusRunning {
			continue
		}
		if best == nil || s.StartedAt.After(best.StartedAt) {
			best = s
		}
	}
	if best == nil {
		return domain.ScanSnapshot{}, mongo.ErrNoDocuments
	}
	return cloneSnapshot(best), nil
}

// Latest 最新快照（不限状态中 startedAt 最新）；无任何快照返回 mongo.ErrNoDocuments
// （cert-cloud-discovery-import 任务 3 snapshot-status 数据源）。
func (f *FakeScanSnapshotRepo) Latest(_ context.Context) (domain.ScanSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *domain.ScanSnapshot
	for _, s := range f.snaps {
		if best == nil || s.StartedAt.After(best.StartedAt) {
			best = s
		}
	}
	if best == nil {
		return domain.ScanSnapshot{}, mongo.ErrNoDocuments
	}
	return cloneSnapshot(best), nil
}

// ListRunningBefore 运行中且 startedAt 早于 before 的快照（scan-timeout 恢复扫描集）。
func (f *FakeScanSnapshotRepo) ListRunningBefore(_ context.Context, before time.Time) ([]domain.ScanSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ScanSnapshot{}
	for _, s := range f.snaps {
		if s.Status == domain.ScanStatusRunning && s.StartedAt.Before(before) {
			out = append(out, cloneSnapshot(s))
		}
	}
	return out, nil
}

// FinishScan 扫描收敛（status/failReason/finishedAt/coverageMeta/partialFailures 原子更新）。
func (f *FakeScanSnapshotRepo) FinishScan(_ context.Context, id string, status domain.ScanStatus, failReason string, meta []domain.CoverageMeta, partials []domain.ScanChannelFailure) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.snaps {
		if s.ID == oid {
			s.Status = status
			s.FailReason = failReason
			now := time.Now()
			s.FinishedAt = &now
			s.CoverageMeta = append([]domain.CoverageMeta(nil), meta...)
			s.PartialFailures = append([]domain.ScanChannelFailure(nil), partials...)
			return nil
		}
	}
	return mongo.ErrNoDocuments
}

// MarkFinished 结束快照（status/finishedAt/failReason 同步更新）。
func (f *FakeScanSnapshotRepo) MarkFinished(_ context.Context, id string, status domain.ScanStatus, failReason string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.snaps {
		if s.ID == oid {
			s.Status = status
			s.FailReason = failReason
			now := time.Now()
			s.FinishedAt = &now
			return nil
		}
	}
	return mongo.ErrNoDocuments
}

// cloneSnapshot 深拷贝（隔离内部指针状态）。
func cloneSnapshot(s *domain.ScanSnapshot) domain.ScanSnapshot {
	out := *s
	out.CoverageMeta = append([]domain.CoverageMeta(nil), s.CoverageMeta...)
	out.PartialFailures = append([]domain.ScanChannelFailure(nil), s.PartialFailures...)
	if s.FinishedAt != nil {
		fa := *s.FinishedAt
		out.FinishedAt = &fa
	}
	return out
}

// FakeCertReferenceRepo 引用扫描发现内存假实现。
type FakeCertReferenceRepo struct {
	mu   sync.Mutex
	refs []domain.CertReference
}

// NewFakeCertReferenceRepo 创建空引用假实现。
func NewFakeCertReferenceRepo() *FakeCertReferenceRepo {
	return &FakeCertReferenceRepo{}
}

// CreateMulti 批量写入引用（模拟 DEFAULT 填充：scannedAt=now），返回写入条数。
func (f *FakeCertReferenceRepo) CreateMulti(_ context.Context, refs []domain.CertReference) (int, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for i := range refs {
		if refs[i].ScannedAt.IsZero() {
			refs[i].ScannedAt = now
		}
		if refs[i].ID.IsZero() {
			refs[i].ID = primitive.NewObjectID()
		}
		f.refs = append(f.refs, refs[i])
	}
	return len(refs), nil
}

// ListByFingerprint 按指纹查询全部引用（跨快照累计视图）。
func (f *FakeCertReferenceRepo) ListByFingerprint(_ context.Context, fingerprint string) ([]domain.CertReference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.CertReference
	for _, r := range f.refs {
		if r.CertFingerprint == fingerprint {
			out = append(out, r)
		}
	}
	return out, nil
}

// ListBySnapshotID 按快照查询全部引用。
func (f *FakeCertReferenceRepo) ListBySnapshotID(_ context.Context, snapshotID string) ([]domain.CertReference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.CertReference
	for _, r := range f.refs {
		if r.SnapshotID == snapshotID {
			out = append(out, r)
		}
	}
	return out, nil
}

// DeleteBySnapshotID 按快照清理引用，返回删除条数。
func (f *FakeCertReferenceRepo) DeleteBySnapshotID(_ context.Context, snapshotID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.refs[:0]
	var n int64
	for _, r := range f.refs {
		if r.SnapshotID == snapshotID {
			n++
			continue
		}
		kept = append(kept, r)
	}
	f.refs = kept
	return n, nil
}

// BackfillFingerprint 占位指纹引用回填（任务 4 CAS 语义：仅 certFingerprint
// 仍等于 fromFingerprint 的引用被更新为 toFingerprint；from==to 无操作）。
func (f *FakeCertReferenceRepo) BackfillFingerprint(_ context.Context, cloud, accountKey, cloudCertID, fromFingerprint, toFingerprint string) (int64, error) {
	if fromFingerprint == toFingerprint {
		return 0, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for i := range f.refs {
		r := &f.refs[i]
		if r.Cloud == domain.Cloud(cloud) &&
			r.AccountKey == accountKey &&
			r.ReferencedCloudCertID == cloudCertID &&
			r.CertFingerprint == fromFingerprint {
			r.CertFingerprint = toFingerprint
			n++
		}
	}
	return n, nil
}

// ---------------------------------------------------------------------
// K8s 凭证 / CRD 登记内存假实现（任务 3.4）
// ---------------------------------------------------------------------

// FakeK8sCredentialRepo K8s 集群凭证内存假实现。
// 硬约束：与真实仓储一致，仅持有密文形态（Kubeconfig 指针指向密文 EncryptedSecret），
// 不提供任何返回明文的方法。
type FakeK8sCredentialRepo struct {
	mu        sync.Mutex
	byCluster map[string]*domain.K8sCredential
}

// NewFakeK8sCredentialRepo 创建空凭证假实现。
func NewFakeK8sCredentialRepo() *FakeK8sCredentialRepo {
	return &FakeK8sCredentialRepo{byCluster: map[string]*domain.K8sCredential{}}
}

// Create 写入凭证（模拟 DEFAULT 填充与 clusterName 唯一约束）。
func (f *FakeK8sCredentialRepo) Create(_ context.Context, c *domain.K8sCredential) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.byCluster[c.ClusterName]; dup {
		return domain.ErrDuplicateClusterName
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	if c.ID.IsZero() {
		c.ID = primitive.NewObjectID()
	}
	stored := *c
	if c.Kubeconfig != nil {
		sec := *c.Kubeconfig
		stored.Kubeconfig = &sec
	}
	f.byCluster[c.ClusterName] = &stored
	return nil
}

// GetByClusterName 按集群名查询；未命中返回 mongo.ErrNoDocuments。
func (f *FakeK8sCredentialRepo) GetByClusterName(_ context.Context, clusterName string) (domain.K8sCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byCluster[clusterName]
	if !ok {
		return domain.K8sCredential{}, mongo.ErrNoDocuments
	}
	return cloneK8sCredential(c), nil
}

// List 返回全量副本（按 clusterName 稳定排序，便于断言）。
func (f *FakeK8sCredentialRepo) List(_ context.Context) ([]domain.K8sCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.byCluster))
	for name := range f.byCluster {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]domain.K8sCredential, 0, len(names))
	for _, name := range names {
		out = append(out, cloneK8sCredential(f.byCluster[name]))
	}
	return out, nil
}

// DeleteByClusterName 按集群名删除。
func (f *FakeK8sCredentialRepo) DeleteByClusterName(_ context.Context, clusterName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byCluster, clusterName)
	return nil
}

// cloneK8sCredential 深拷贝（隔离密文指针状态）。
func cloneK8sCredential(c *domain.K8sCredential) domain.K8sCredential {
	out := *c
	if c.Kubeconfig != nil {
		sec := *c.Kubeconfig
		out.Kubeconfig = &sec
	}
	return out
}

// FakeCrdRegistrationRepo CRD 扫描登记内存假实现。
type FakeCrdRegistrationRepo struct {
	mu   sync.Mutex
	byID map[string]*domain.CrdRegistration
}

// NewFakeCrdRegistrationRepo 创建空登记假实现。
func NewFakeCrdRegistrationRepo() *FakeCrdRegistrationRepo {
	return &FakeCrdRegistrationRepo{byID: map[string]*domain.CrdRegistration{}}
}

// Create 写入登记（模拟 DEFAULT 填充：createdAt=now、enabled=true、预生成 _id 回填）；
// clusterId+apiGroup+kind 冲突返回 ErrDuplicateCrdRegistration。
func (f *FakeCrdRegistrationRepo) Create(_ context.Context, reg *domain.CrdRegistration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.ClusterID == reg.ClusterID && existing.APIGroup == reg.APIGroup && existing.Kind == reg.Kind {
			return domain.ErrDuplicateCrdRegistration
		}
	}
	if reg.CreatedAt.IsZero() {
		reg.CreatedAt = time.Now()
	}
	reg.Enabled = true
	if reg.ID.IsZero() {
		reg.ID = primitive.NewObjectID()
	}
	stored := *reg
	f.byID[reg.ID.Hex()] = &stored
	return nil
}

// GetByID 按文档 ID 查询；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (f *FakeCrdRegistrationRepo) GetByID(_ context.Context, id string) (domain.CrdRegistration, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return domain.CrdRegistration{}, fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	reg, ok := f.byID[oid.Hex()]
	if !ok {
		return domain.CrdRegistration{}, mongo.ErrNoDocuments
	}
	return *reg, nil
}

// List 全量登记（按 createdAt 稳定排序）。
func (f *FakeCrdRegistrationRepo) List(_ context.Context) ([]domain.CrdRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.CrdRegistration, 0, len(f.byID))
	for _, reg := range f.byID {
		out = append(out, *reg)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID.Hex() < out[j].ID.Hex()
	})
	return out, nil
}

// ListEnabled enabled=true 登记项。
func (f *FakeCrdRegistrationRepo) ListEnabled(_ context.Context) ([]domain.CrdRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.CrdRegistration, 0, len(f.byID))
	for _, reg := range f.byID {
		if reg.Enabled {
			out = append(out, *reg)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID.Hex() < out[j].ID.Hex()
	})
	return out, nil
}

// SetEnabled 启停登记；未命中返回 mongo.ErrNoDocuments。
func (f *FakeCrdRegistrationRepo) SetEnabled(_ context.Context, id string, enabled bool) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	reg, ok := f.byID[oid.Hex()]
	if !ok {
		return mongo.ErrNoDocuments
	}
	reg.Enabled = enabled
	return nil
}

// DeleteByID 按文档 ID 删除登记。
func (f *FakeCrdRegistrationRepo) DeleteByID(_ context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, oid.Hex())
	return nil
}

// ---------------------------------------------------------------------
// 探测结果 / 豁免清单 / 全局告警配置内存假实现（任务 4.5 看板与配置端点）
// ---------------------------------------------------------------------

// FakeProbeResultRepo TLS 探测结果内存假实现。
type FakeProbeResultRepo struct {
	mu      sync.Mutex
	created []domain.ProbeResult
}

// NewFakeProbeResultRepo 创建空探测结果假实现。
func NewFakeProbeResultRepo() *FakeProbeResultRepo {
	return &FakeProbeResultRepo{}
}

// Create 写入探测结果（模拟 DEFAULT 填充：probeAt=now）。
func (f *FakeProbeResultRepo) Create(_ context.Context, r *domain.ProbeResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ProbeAt.IsZero() {
		r.ProbeAt = time.Now()
	}
	stored := *r
	if stored.OnlineNotAfter != nil {
		na := *stored.OnlineNotAfter
		stored.OnlineNotAfter = &na
	}
	f.created = append(f.created, stored)
	return nil
}

// LatestByDomain 最近一次探测；未命中返回 mongo.ErrNoDocuments。
func (f *FakeProbeResultRepo) LatestByDomain(_ context.Context, domainName string) (domain.ProbeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *domain.ProbeResult
	for i := range f.created {
		r := f.created[i]
		if r.Domain != domainName {
			continue
		}
		if best == nil || r.ProbeAt.After(best.ProbeAt) {
			best = &r
		}
	}
	if best == nil {
		return domain.ProbeResult{}, mongo.ErrNoDocuments
	}
	return *best, nil
}

// LatestPerDomain 每个域名的最近一次探测（domain 字典序稳定返回）。
func (f *FakeProbeResultRepo) LatestPerDomain(_ context.Context) ([]domain.ProbeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	latest := make(map[string]domain.ProbeResult)
	for _, r := range f.created {
		if cur, ok := latest[r.Domain]; !ok || r.ProbeAt.After(cur.ProbeAt) {
			latest[r.Domain] = r
		}
	}
	names := make([]string, 0, len(latest))
	for name := range latest {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]domain.ProbeResult, 0, len(names))
	for _, name := range names {
		out = append(out, latest[name])
	}
	return out, nil
}

// ListRecentByDomains 批量查询各域名最近探测记录（任务 5.10 验证窗口达标判定）：
// 每域名按 probeAt 降序至多 limit 条；domain 字典序、同域 probeAt 降序稳定返回。
func (f *FakeProbeResultRepo) ListRecentByDomains(_ context.Context, domains []string, limit int) ([]domain.ProbeResult, error) {
	if len(domains) == 0 || limit <= 0 {
		return []domain.ProbeResult{}, nil
	}
	set := make(map[string]bool, len(domains))
	for _, d := range domains {
		set[d] = true
	}
	f.mu.Lock()
	grouped := make(map[string][]domain.ProbeResult)
	for _, r := range f.created {
		if !set[r.Domain] {
			continue
		}
		stored := r
		if stored.OnlineNotAfter != nil {
			na := *stored.OnlineNotAfter
			stored.OnlineNotAfter = &na
		}
		grouped[r.Domain] = append(grouped[r.Domain], stored)
	}
	f.mu.Unlock()
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []domain.ProbeResult{}
	for _, name := range names {
		rs := grouped[name]
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].ProbeAt.After(rs[j].ProbeAt) })
		if len(rs) > limit {
			rs = rs[:limit]
		}
		out = append(out, rs...)
	}
	return out, nil
}

// FakeExemptionRepo 探测豁免清单内存假实现（domain 唯一）。
type FakeExemptionRepo struct {
	mu       sync.Mutex
	byDomain map[string]domain.Exemption
}

// NewFakeExemptionRepo 创建空豁免清单假实现。
func NewFakeExemptionRepo() *FakeExemptionRepo {
	return &FakeExemptionRepo{byDomain: map[string]domain.Exemption{}}
}

// Upsert 按唯一 domain 写入（模拟 DEFAULT 填充：createdAt=now；重写不重置时间）。
func (f *FakeExemptionRepo) Upsert(_ context.Context, e *domain.Exemption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	if e.ID.IsZero() {
		e.ID = primitive.NewObjectID()
	}
	f.byDomain[e.Domain] = *e
	return nil
}

// List 全量豁免清单（domain 字典序稳定返回）。
func (f *FakeExemptionRepo) List(_ context.Context) ([]domain.Exemption, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.byDomain))
	for name := range f.byDomain {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]domain.Exemption, 0, len(names))
	for _, name := range names {
		out = append(out, f.byDomain[name])
	}
	return out, nil
}

// DeleteByDomain 按域名删除豁免。
func (f *FakeExemptionRepo) DeleteByDomain(_ context.Context, domainName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byDomain, domainName)
	return nil
}

// FakeAlertConfigRepo 全局告警配置内存假实现（单文档；未写入时返回 DEFAULT）。
type FakeAlertConfigRepo struct {
	mu  sync.Mutex
	cfg *domain.AlertConfig
}

// NewFakeAlertConfigRepo 创建空配置假实现（Get 返回 DefaultAlertConfig）。
func NewFakeAlertConfigRepo() *FakeAlertConfigRepo {
	return &FakeAlertConfigRepo{}
}

// Get 读取配置；未写入时返回 schema.sql DEFAULT 填充的默认配置（对齐真实仓储）。
func (f *FakeAlertConfigRepo) Get(_ context.Context) (domain.AlertConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cfg == nil {
		return domain.DefaultAlertConfig(), nil
	}
	return cloneAlertConfig(*f.cfg), nil
}

// Save 以 _id="global" upsert 保存（对齐真实仓储：ID 强制、空通道/空分级回退 DEFAULT）。
func (f *FakeAlertConfigRepo) Save(_ context.Context, cfg *domain.AlertConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := cloneAlertConfig(*cfg)
	stored.ID = domain.AlertConfigID
	if len(stored.WebhookURLs) == 0 {
		stored.WebhookURLs = []string{}
	}
	if len(stored.EmailGroup) == 0 {
		stored.EmailGroup = []string{}
	}
	if len(stored.Thresholds.ExpiryLevels) == 0 {
		stored.Thresholds = domain.DefaultThresholds()
	}
	f.cfg = &stored
	return nil
}

// cloneAlertConfig 深拷贝（隔离切片/映射指针状态）。
func cloneAlertConfig(cfg domain.AlertConfig) domain.AlertConfig {
	out := cfg
	out.WebhookURLs = append([]string(nil), cfg.WebhookURLs...)
	out.EmailGroup = append([]string(nil), cfg.EmailGroup...)
	out.Thresholds.ExpiryLevels = append([]int(nil), cfg.Thresholds.ExpiryLevels...)
	if cfg.VerifyWindowRoute != nil {
		vwr := *cfg.VerifyWindowRoute
		vwr.WebhookURLs = append([]string(nil), cfg.VerifyWindowRoute.WebhookURLs...)
		vwr.EmailGroup = append([]string(nil), cfg.VerifyWindowRoute.EmailGroup...)
		out.VerifyWindowRoute = &vwr
	}
	if cfg.WildcardProbeOverrides != nil {
		overrides := make(map[string]string, len(cfg.WildcardProbeOverrides))
		for k, v := range cfg.WildcardProbeOverrides {
			overrides[k] = v
		}
		out.WildcardProbeOverrides = overrides
	}
	return out
}
