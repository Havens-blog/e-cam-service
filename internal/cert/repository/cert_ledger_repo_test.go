package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestCertificateRepo_ListPage（集成）台账列表：筛选/分页/排序/总数。
func TestCertificateRepo_ListPage(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertificateRepository(db)

	now := time.Now()
	seed := []struct {
		fp     string
		cn     string
		sans   []string
		offset time.Duration // notAfter = now + offset
		status domain.HostingStatus
	}{
		{fp: testFingerprint(1), cn: "shop.example.com", sans: []string{"api.shop.example.com"}, offset: 3 * 24 * time.Hour, status: domain.HostingStatusComplete},
		{fp: testFingerprint(2), cn: "portal.example.org", sans: []string{"portal.example.org"}, offset: 40 * 24 * time.Hour, status: domain.HostingStatusFingerprintOnly},
		{fp: testFingerprint(3), cn: "old.example.net", sans: []string{"old.example.net"}, offset: -24 * time.Hour, status: domain.HostingStatusComplete},
		{fp: testFingerprint(4), cn: "mid.example.io", sans: []string{"mid.example.io"}, offset: 10 * 24 * time.Hour, status: domain.HostingStatusComplete},
	}
	for _, s := range seed {
		require.NoError(t, repo.Create(ctx, &domain.Certificate{
			Fingerprint:   s.fp,
			CommonName:    s.cn,
			Sans:          s.sans,
			NotBefore:     now.Add(-24 * time.Hour),
			NotAfter:      now.Add(s.offset),
			HostingStatus: s.status,
		}))
	}

	// 全量：notAfter 升序（过期最先），total=4
	page, total, err := repo.ListPage(ctx, domain.CertListFilter{}, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, page, 4)
	assert.Equal(t, testFingerprint(3), page[0].Fingerprint)
	assert.Equal(t, testFingerprint(2), page[3].Fingerprint)

	// 分页
	page2, total2, err := repo.ListPage(ctx, domain.CertListFilter{}, 3, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total2)
	assert.Len(t, page2, 1)

	// hostingStatus 筛选
	_, total3, err := repo.ListPage(ctx, domain.CertListFilter{HostingStatus: domain.HostingStatusFingerprintOnly}, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total3)

	// daysLeft 换算的 notAfter 区间：expired=notAfter≤now
	_, total4, err := repo.ListPage(ctx, domain.CertListFilter{NotAfterTo: &now}, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total4)
	// le7：now < notAfter ≤ now+7d
	to7 := now.Add(7 * 24 * time.Hour)
	_, total5, err := repo.ListPage(ctx, domain.CertListFilter{NotAfterFrom: &now, NotAfterTo: &to7}, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total5)

	// search：SAN 子串 + 指纹片段 + 大小写不敏感
	_, total6, err := repo.ListPage(ctx, domain.CertListFilter{Search: "api.shop"}, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total6)
	_, total7, err := repo.ListPage(ctx, domain.CertListFilter{Search: testFingerprint(1)[:10]}, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total7)
	_, total8, err := repo.ListPage(ctx, domain.CertListFilter{Search: "SHOP.EXAMPLE"}, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total8)
	// 正则元字符按字面处理
	_, total9, err := repo.ListPage(ctx, domain.CertListFilter{Search: "shop.*"}, 0, 20)
	require.NoError(t, err)
	assert.Zero(t, total9)

	// 无命中：空页 + total=0
	page3, total10, err := repo.ListPage(ctx, domain.CertListFilter{Search: "nomatch"}, 0, 20)
	require.NoError(t, err)
	assert.Empty(t, page3)
	assert.Zero(t, total10)
}

// TestScanSnapshotRepo_LatestDone（集成）最新成功快照：done 过滤、startedAt 降序、
// running/failed 不参与、无成功快照返回 ErrNoDocuments。
func TestScanSnapshotRepo_LatestDone(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewScanSnapshotRepository(db)

	// 无快照
	_, err := repo.LatestDone(ctx)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)

	older := &domain.ScanSnapshot{StartedAt: time.Now().Add(-2 * time.Hour)}
	olderID, err := repo.Create(ctx, older)
	require.NoError(t, err)
	require.NoError(t, repo.MarkFinished(ctx, olderID, domain.ScanStatusDone, ""))

	// 更新的 failed 快照不参与（仅 status=done）
	failed := &domain.ScanSnapshot{StartedAt: time.Now().Add(-1 * time.Hour)}
	failedID, err := repo.Create(ctx, failed)
	require.NoError(t, err)
	require.NoError(t, repo.MarkFinished(ctx, failedID, domain.ScanStatusFailed, "boom"))

	got, err := repo.LatestDone(ctx)
	require.NoError(t, err)
	assert.Equal(t, olderID, got.ID.Hex())

	// 更新的 done 快照胜出
	newer := &domain.ScanSnapshot{
		StartedAt:    time.Now().Add(-30 * time.Minute),
		CoverageMeta: []domain.CoverageMeta{{Cloud: "aliyun", Product: "cdn", Covered: 2, Total: 5}},
	}
	newerID, err := repo.Create(ctx, newer)
	require.NoError(t, err)
	require.NoError(t, repo.MarkFinished(ctx, newerID, domain.ScanStatusDone, ""))

	got, err = repo.LatestDone(ctx)
	require.NoError(t, err)
	assert.Equal(t, newerID, got.ID.Hex())
	require.Len(t, got.CoverageMeta, 1)
	assert.Equal(t, "aliyun", got.CoverageMeta[0].Cloud)
}

// TestCertReferenceRepo_ListBySnapshotID（集成）按快照聚合（refCount/stats 分母数据源）。
func TestCertReferenceRepo_ListBySnapshotID(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertReferenceRepository(db)
	snaps := NewScanSnapshotRepository(db)

	idA, err := snaps.Create(ctx, &domain.ScanSnapshot{StartedAt: time.Now().Add(-time.Hour)})
	require.NoError(t, err)
	idB, err := snaps.Create(ctx, &domain.ScanSnapshot{StartedAt: time.Now().Add(-30 * time.Minute)})
	require.NoError(t, err)

	n, err := repo.CreateMulti(ctx, []domain.CertReference{
		{CertFingerprint: testFingerprint(1), Cloud: domain.CloudAliyun, Product: domain.ProductCDN, SnapshotID: idA},
		{CertFingerprint: testFingerprint(1), Cloud: domain.CloudTencent, Product: domain.ProductWAF, SnapshotID: idA},
		{CertFingerprint: testFingerprint(2), Cloud: domain.CloudAliyun, Product: domain.ProductCDN, SnapshotID: idB},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	refsA, err := repo.ListBySnapshotID(ctx, idA)
	require.NoError(t, err)
	assert.Len(t, refsA, 2)
	refsB, err := repo.ListBySnapshotID(ctx, idB)
	require.NoError(t, err)
	assert.Len(t, refsB, 1)

	// 跨快照累计视图
	all, err := repo.ListByFingerprint(ctx, testFingerprint(1))
	require.NoError(t, err)
	assert.Len(t, all, 2)
}
