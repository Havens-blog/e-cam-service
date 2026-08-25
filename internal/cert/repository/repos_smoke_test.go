package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCertReferenceRepo（集成）批量写入/按指纹查��/按快照清理。
func TestCertReferenceRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertReferenceRepository(db)

	fp := testFingerprint(6)
	refs := []domain.CertReference{
		{CertFingerprint: fp, Cloud: domain.CloudAliyun, Product: domain.ProductCDN,
			ResourceID: "res-1", ReferencedCloudCertID: "cloud-cert-1", SnapshotID: "snap-1"},
		{CertFingerprint: fp, Cloud: domain.CloudTencent, Product: domain.ProductCLB,
			ResourceID: "res-2", SnapshotID: "snap-1"},
		{CertFingerprint: fp, Product: domain.ProductCRD, ClusterID: "cluster-1",
			ResourceID: "crd-inst", SnapshotID: "snap-2"},
	}
	n, err := repo.CreateMulti(ctx, refs)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.False(t, refs[0].ScannedAt.IsZero(), "DEFAULT scannedAt=now")

	got, err := repo.ListByFingerprint(ctx, fp)
	require.NoError(t, err)
	assert.Len(t, got, 3)

	deleted, err := repo.DeleteBySnapshotID(ctx, "snap-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	got, err = repo.ListByFingerprint(ctx, fp)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

// TestCertReferenceRepo_BackfillFingerprint（集成，任务 4）占位指纹引用回填：
// (cloud,accountKey,cloudCertId) 定位 + fromFingerprint CAS——真实指纹引用
// 永不被覆盖、相邻占位引用不受影响、from==to 无操作。
func TestCertReferenceRepo_BackfillFingerprint(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertReferenceRepository(db)

	from, to := testFingerprint(7), testFingerprint(8)
	refs := []domain.CertReference{
		{CertFingerprint: from, Cloud: domain.CloudTencent, Product: domain.ProductCDN,
			ResourceID: "res-1", ReferencedCloudCertID: "ssl-9", AccountKey: "acct-tx", SnapshotID: "snap-1"},
		{CertFingerprint: from, Cloud: domain.CloudTencent, Product: domain.ProductWAF,
			ResourceID: "res-2", ReferencedCloudCertID: "ssl-9", AccountKey: "acct-tx", SnapshotID: "snap-1"},
		// 真实指纹引用（同三元组）——永不被覆盖
		{CertFingerprint: to, Cloud: domain.CloudTencent, Product: domain.ProductCDN,
			ResourceID: "res-3", ReferencedCloudCertID: "ssl-9", AccountKey: "acct-tx", SnapshotID: "snap-1"},
		// 相邻占位引用：不同三元组
		{CertFingerprint: from, Cloud: domain.CloudTencent, Product: domain.ProductCDN,
			ResourceID: "res-4", ReferencedCloudCertID: "ssl-9", AccountKey: "acct-tx2", SnapshotID: "snap-1"},
	}
	_, err := repo.CreateMulti(ctx, refs)
	require.NoError(t, err)

	n, err := repo.BackfillFingerprint(ctx, "tencent", "acct-tx", "ssl-9", from, to)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "仅 acct-tx@ssl-9 的占位引用被回填")

	got, err := repo.ListByFingerprint(ctx, to)
	require.NoError(t, err)
	assert.Len(t, got, 3, "2 条回填 + 1 条既有真实指纹引用")

	// from==to 无操作（同值幂等）
	n, err = repo.BackfillFingerprint(ctx, "tencent", "acct-tx", "ssl-9", to, to)
	require.NoError(t, err)
	assert.Zero(t, n)
}

// TestScanSnapshotRepo（集成）快照创建默认 running + 终止收敛。
func TestScanSnapshotRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewScanSnapshotRepository(db)

	snap := &domain.ScanSnapshot{
		CoverageMeta: []domain.CoverageMeta{
			{Cloud: "aliyun", Product: "cdn", Covered: 10, Total: 12},
			{Cloud: "huawei", Product: "waf", Covered: 3, Total: -1}, // 分母不可用
		},
	}
	id, err := repo.Create(ctx, snap)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusRunning, got.Status, "DEFAULT status=running")
	assert.False(t, got.StartedAt.IsZero())
	require.Len(t, got.CoverageMeta, 2)
	assert.Equal(t, -1, got.CoverageMeta[1].Total)

	require.NoError(t, repo.MarkFinished(ctx, id, domain.ScanStatusFailed, domain.FailReasonScanTimedOut))
	finished, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusFailed, finished.Status)
	assert.Equal(t, domain.FailReasonScanTimedOut, finished.FailReason)
	assert.NotNil(t, finished.FinishedAt)

	_, err = repo.GetByID(ctx, "bogus-id")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

// TestCloudCertMappingRepo（集成）按唯一键 upsert 去重 + 状态迁移。
func TestCloudCertMappingRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCloudCertMappingRepository(db)

	fp := testFingerprint(7)
	m := &domain.CloudCertMapping{
		CertFingerprint: fp, Cloud: "aliyun", AccountKey: "ak-1", CloudCertID: "cc-1",
	}
	require.NoError(t, repo.Upsert(ctx, m))
	assert.Equal(t, domain.MappingStatusActive, m.Status, "DEFAULT status=active")

	// 同指纹+云+账号再 upsert：更新而非新增（uk_fp_cloud_account 去重）
	m.CloudCertID = "cc-2"
	require.NoError(t, repo.Upsert(ctx, m))
	list, err := repo.ListByFingerprint(ctx, fp)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "cc-2", list[0].CloudCertID)

	require.NoError(t, repo.UpdateStatus(ctx, list[0].ID.Hex(), domain.MappingStatusOrphan))
	list, err = repo.ListByFingerprint(ctx, fp)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, list[0].Status)
}

// TestProbeResultRepo（集成）最近探测查询按 probeAt 降序。
func TestProbeResultRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewProbeResultRepository(db)

	base := now()
	// 显式指定 probeAt（同一毫秒内快速写入会导致排序不稳定）
	probes := []*domain.ProbeResult{
		{Domain: "www.example.com", Status: domain.ProbeStatusDiff,
			OnlineFingerprint: testFingerprint(8), ProbeAt: base.Add(-2 * time.Minute)},
		{Domain: "www.example.com", Status: domain.ProbeStatusConsistent,
			OnlineFingerprint: testFingerprint(9), ProbeAt: base.Add(-1 * time.Minute)},
		{Domain: "www.example.com", Status: domain.ProbeStatusChangeLinkedDiff,
			OnlineFingerprint: testFingerprint(10), ProbeAt: base},
	}
	for _, p := range probes {
		require.NoError(t, repo.Create(ctx, p))
	}

	latest, err := repo.LatestByDomain(ctx, "www.example.com")
	require.NoError(t, err)
	assert.Equal(t, domain.ProbeStatusChangeLinkedDiff, latest.Status, "最近一次探测应为 probeAt 最大者")
	assert.Equal(t, testFingerprint(10), latest.OnlineFingerprint)
}

// TestExemptionRepo（集成）豁免 upsert 去重（uk_domain）。
func TestExemptionRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewExemptionRepository(db)

	e := &domain.Exemption{Domain: "legacy.example.com", Reason: "历史系统", Operator: "op-1"}
	require.NoError(t, repo.Upsert(ctx, e))
	e.Reason = "变更原因"
	require.NoError(t, repo.Upsert(ctx, e))

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "变更原因", list[0].Reason)

	require.NoError(t, repo.DeleteByDomain(ctx, "legacy.example.com"))
	list, err = repo.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// TestAlertConfigRepo（集成）未初始化返回默认配置；Save 后读回持久化值。
func TestAlertConfigRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewAlertConfigRepository(db)

	// 未保存：返回 schema.sql DEFAULT 填充的默认配置
	cfg, err := repo.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.DefaultThresholds(), cfg.Thresholds)

	saved := domain.DefaultAlertConfig()
	saved.WebhookURLs = []string{"https://hooks.example.com/cert"}
	saved.VerifyWindowRoute = &domain.VerifyWindowRoute{
		Enabled:     true,
		WebhookURLs: []string{"https://hooks.example.com/verify"},
		EmailGroup:  []string{"ops@example.com"},
	}
	saved.WildcardProbeOverrides = map[string]string{"*.example.com": "probe.example.com"}
	saved.Thresholds.ScanFreshnessHours = 12
	require.NoError(t, repo.Save(ctx, &saved))

	got, err := repo.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, 12, got.Thresholds.ScanFreshnessHours)
	assert.True(t, got.VerifyWindowRoute.Enabled)
	assert.Equal(t, "probe.example.com", got.WildcardProbeOverrides["*.example.com"])
}

// TestBatchSessionRepo（集成）会话创建/进度原子递增/终态收敛。
func TestBatchSessionRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertBatchSessionRepository(db)

	s := &domain.CertBatchSession{
		Files: []domain.BatchSessionFile{
			{FileName: "a.pem", Result: domain.BatchFilePending},
			{FileName: "b.pem", Result: domain.BatchFilePending},
		},
		Progress: domain.BatchProgress{Total: 2},
		Operator: "op-1",
	}
	id, err := repo.Create(ctx, s)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.BatchSessionRunning, got.Status, "DEFAULT status=running")
	assert.False(t, got.CreatedAt.IsZero())

	// 单文件成功：结果与 progress.done 原子递增
	require.NoError(t, repo.RecordFileResult(ctx, id, 0, domain.BatchFileSuccess, testFingerprint(11), ""))
	// 单文件失败：结果与 progress.failed 原子递增
	require.NoError(t, repo.RecordFileResult(ctx, id, 1, domain.BatchFileFailed, "", "CERT_DUPLICATE_FINGERPRINT: 已存在"))

	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.BatchFileSuccess, got.Files[0].Result)
	assert.Equal(t, testFingerprint(11), got.Files[0].CertID)
	assert.Equal(t, domain.BatchFileFailed, got.Files[1].Result)
	assert.Equal(t, 1, got.Progress.Done)
	assert.Equal(t, 1, got.Progress.Failed)

	require.NoError(t, repo.MarkFinished(ctx, id, domain.BatchSessionPartialFailed))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.BatchSessionPartialFailed, got.Status)
	assert.NotNil(t, got.FinishedAt)

	_, err = repo.GetByID(ctx, "zzz")
	assert.ErrorIs(t, err, domain.ErrInvalidID, "非法 hex 应返回 ErrInvalidID 而非查询")
}

// TestDiscoveryImportSessionRepo（集成）云端发现导入会话：
// 创建/逐条目进度原子递增/按失败计数收敛终态（completed 与 partial_failed 两分支）。
func TestDiscoveryImportSessionRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewDiscoveryImportSessionRepository(db)

	s := &domain.DiscoveryImportSession{
		Items: []domain.DiscoveryImportItem{
			{Cloud: "aliyun", AccountKey: "acc-1", CloudCertID: "cert-1", Result: domain.DiscoveryItemPending},
			{Cloud: "tencent", AccountKey: "acc-2", CloudCertID: "cert-2", Result: domain.DiscoveryItemPending},
		},
		Progress: domain.DiscoveryImportProgress{Total: 2},
		Operator: "op-1",
	}
	id, err := repo.Create(ctx, s)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportRunning, got.Status, "DEFAULT status=running")
	assert.False(t, got.CreatedAt.IsZero(), "DEFAULT createdAt=now")
	assert.Equal(t, "aliyun", got.Items[0].Cloud)
	assert.Equal(t, "acc-1", got.Items[0].AccountKey)
	assert.Equal(t, "cert-1", got.Items[0].CloudCertID)

	// 单条目成功：结果与 progress.succeeded 原子递增
	require.NoError(t, repo.RecordItemResult(ctx, id, 0, domain.DiscoveryItemSuccess, testFingerprint(11), ""))
	// 单条目失败：结果与 progress.failed 原子递增
	require.NoError(t, repo.RecordItemResult(ctx, id, 1, domain.DiscoveryItemFailed, "", "CERT_GET_FAILED: 云侧已不存在"))

	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryItemSuccess, got.Items[0].Result)
	assert.Equal(t, testFingerprint(11), got.Items[0].MappedCertID)
	assert.Equal(t, domain.DiscoveryItemFailed, got.Items[1].Result)
	assert.Equal(t, "CERT_GET_FAILED: 云侧已不存在", got.Items[1].ErrorReason)
	assert.Equal(t, 1, got.Progress.Succeeded)
	assert.Equal(t, 1, got.Progress.Failed)

	// 终态收敛（按失败计数）：failed>0 → partial_failed
	require.NoError(t, repo.MarkFinished(ctx, id))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportPartialFailed, got.Status)
	assert.NotNil(t, got.FinishedAt)

	// 全成功会话 → completed
	s2 := &domain.DiscoveryImportSession{
		Items: []domain.DiscoveryImportItem{
			{Cloud: "aws", AccountKey: "acc-3", CloudCertID: "arn:aws:acm:cn-north-1:1:certificate/guid", Result: domain.DiscoveryItemPending},
		},
		Progress: domain.DiscoveryImportProgress{Total: 1},
		Operator: "op-1",
	}
	id2, err := repo.Create(ctx, s2)
	require.NoError(t, err)
	require.NoError(t, repo.RecordItemResult(ctx, id2, 0, domain.DiscoveryItemSuccess, testFingerprint(12), ""))
	require.NoError(t, repo.MarkFinished(ctx, id2))
	got2, err := repo.GetByID(ctx, id2)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportCompleted, got2.Status)
	assert.NotNil(t, got2.FinishedAt)

	_, err = repo.GetByID(ctx, "zzz")
	assert.ErrorIs(t, err, domain.ErrInvalidID, "非法 hex 应返回 ErrInvalidID 而非查询")
}

// TestCrdRegistrationRepo（集成）登记唯一去重 + enabled 过滤。
func TestCrdRegistrationRepo(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCrdRegistrationRepository(db)

	reg := &domain.CrdRegistration{
		ClusterID: "cluster-1", APIGroup: "alb.alibabacloud.com", Kind: "AlbConfig",
		CertFieldPath: "spec.certificates[].certificateId", Operator: "op-1",
	}
	require.NoError(t, repo.Create(ctx, reg))
	assert.True(t, reg.Enabled, "DEFAULT enabled=true（登记即纳入扫描）")

	err := repo.Create(ctx, &domain.CrdRegistration{
		ClusterID: "cluster-1", APIGroup: "alb.alibabacloud.com", Kind: "AlbConfig",
		CertFieldPath: "spec.certificates[].certificateId", Operator: "op-2",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrDuplicateCrdRegistration)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	enabled, err := repo.ListEnabled(ctx)
	require.NoError(t, err)
	assert.Len(t, enabled, 1)

	require.NoError(t, repo.SetEnabled(ctx, reg.ID.Hex(), false))
	enabled, err = repo.ListEnabled(ctx)
	require.NoError(t, err)
	assert.Empty(t, enabled, "停用后不在扫描范围")

	require.NoError(t, repo.DeleteByID(ctx, reg.ID.Hex()))
	list, err = repo.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}
