package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 夹具
// ---------------------------------------------------------------------

// ledgerFixture 台账服务测试夹具（内存假实现）。
type ledgerFixture struct {
	svc   LedgerService
	certs *certtest.FakeCertificateRepo
	refs  *certtest.FakeCertReferenceRepo
	snaps *certtest.FakeScanSnapshotRepo
}

func newLedgerFixture(t *testing.T) *ledgerFixture {
	t.Helper()
	certs := certtest.NewFakeCertificateRepo()
	refs := certtest.NewFakeCertReferenceRepo()
	snaps := certtest.NewFakeScanSnapshotRepo()
	return &ledgerFixture{
		svc:   NewLedgerService(certs, refs, snaps),
		certs: certs, refs: refs, snaps: snaps,
	}
}

// fp 生成互异的 64 位小写 hex 指纹（pattern 满足 ^[0-9a-f]{64}$；
// 种子前置，确保前缀片段可作 search 夹具）。
func fp(i int) string { return fmt.Sprintf("aa%04x%058x", i, i) }

// seedCert 直接落一张台账证书（绕过导入解析，便于控制 notAfter/hostingStatus/protectUntil）。
func (f *ledgerFixture) seedCert(t *testing.T, fingerprint string, mutate func(*domain.Certificate)) string {
	t.Helper()
	c := &domain.Certificate{
		Fingerprint:   fingerprint,
		CommonName:    fmt.Sprintf("cn-%s.example.com", fingerprint[:6]),
		Sans:          []string{fmt.Sprintf("san-%s.example.com", fingerprint[:6])},
		Issuer:        "certtest Intermediate CA",
		SerialNumber:  "serial-" + fingerprint[:6],
		NotBefore:     time.Now().Add(-24 * time.Hour),
		NotAfter:      time.Now().Add(365 * 24 * time.Hour),
		KeyAlgorithm:  domain.KeyAlgorithmECDSA,
		HostingStatus: domain.HostingStatusComplete,
	}
	if mutate != nil {
		mutate(c)
	}
	require.NoError(t, f.certs.Create(context.Background(), c))
	return c.ID.Hex()
}

// seedDoneSnapshot 建立一个成功快照（offset 控制新旧，coverage 声明扫描范围），返回快照 ID。
func (f *ledgerFixture) seedDoneSnapshot(t *testing.T, startedAtOffset time.Duration, coverage []domain.CoverageMeta) string {
	t.Helper()
	snap := &domain.ScanSnapshot{
		StartedAt:    time.Now().Add(startedAtOffset),
		CoverageMeta: coverage,
	}
	id, err := f.snaps.Create(context.Background(), snap)
	require.NoError(t, err)
	require.NoError(t, f.snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
	return id
}

// seedRef 写入一条引用（归属快照 snapID）。
func (f *ledgerFixture) seedRef(t *testing.T, fingerprint, snapID string, cloud domain.Cloud, product domain.Product) {
	t.Helper()
	_, err := f.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint: fingerprint,
		Cloud:           cloud,
		Product:         product,
		ResourceID:      "res-" + fingerprint[:6],
		SnapshotID:      snapID,
	}})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------
// AC1：列表（分页 / 筛选 / search / refCount）
// ---------------------------------------------------------------------

func TestLedgerListPagination(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	for i := 1; i <= 25; i++ {
		offset := time.Duration(i) * 24 * time.Hour // notAfter 互异，验证排序
		f.seedCert(t, fp(i), func(c *domain.Certificate) { c.NotAfter = time.Now().Add(offset) })
	}

	res, err := f.svc.ListCerts(ctx, ListCertsQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(25), res.Total)
	assert.Equal(t, 1, res.Page)
	assert.Equal(t, DefaultCertPageSize, res.PageSize)
	assert.Len(t, res.Items, 20, "默认每页 20")

	// notAfter 升序：最快到期优先
	assert.True(t, res.Items[0].NotAfter.Before(res.Items[1].NotAfter))
	assert.Equal(t, fp(1), res.Items[0].Fingerprint)

	res2, err := f.svc.ListCerts(ctx, ListCertsQuery{Page: 2})
	require.NoError(t, err)
	assert.Len(t, res2.Items, 5)
	assert.Equal(t, int64(25), res2.Total)

	res3, err := f.svc.ListCerts(ctx, ListCertsQuery{Page: 3})
	require.NoError(t, err)
	assert.Empty(t, res3.Items)
	assert.Equal(t, int64(25), res3.Total)

	// page<1 归一化为 1；pageSize 上限 100
	res4, err := f.svc.ListCerts(ctx, ListCertsQuery{Page: -1, PageSize: 1000})
	require.NoError(t, err)
	assert.Equal(t, 1, res4.Page)
	assert.Equal(t, MaxCertPageSize, res4.PageSize)
	assert.Len(t, res4.Items, 25)
}

func TestLedgerListHostingStatusFilter(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	f.seedCert(t, fp(1), nil)
	f.seedCert(t, fp(2), func(c *domain.Certificate) { c.HostingStatus = domain.HostingStatusFingerprintOnly })

	res, err := f.svc.ListCerts(ctx, ListCertsQuery{HostingStatus: domain.HostingStatusFingerprintOnly})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, fp(2), res.Items[0].Fingerprint)
	assert.Equal(t, int64(1), res.Total)

	// 非法枚举 → 明确错误
	_, err = f.svc.ListCerts(ctx, ListCertsQuery{HostingStatus: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostingStatus")
}

func TestLedgerListDaysLeftTiers(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	now := time.Now()
	seed := func(i int, notAfter time.Time) {
		f.seedCert(t, fp(i), func(c *domain.Certificate) { c.NotAfter = notAfter })
	}
	seed(1, now.Add(-24*time.Hour))        // 已过期
	seed(2, now.Add(3*24*time.Hour))       // ≤7
	seed(3, now.Add(10*24*time.Hour))      // ≤14
	seed(4, now.Add(20*24*time.Hour))      // ≤30
	seed(5, now.Add(45*24*time.Hour))      // >30
	seed(6, now.Add(500*time.Millisecond)) // 边界：未过期但 <1 天，落入 ≤7

	list := func(tier DaysLeftTier) []string {
		res, err := f.svc.ListCerts(ctx, ListCertsQuery{DaysLeft: tier})
		require.NoError(t, err)
		fps := make([]string, 0, len(res.Items))
		for _, it := range res.Items {
			fps = append(fps, it.Fingerprint)
		}
		return fps
	}

	assert.ElementsMatch(t, []string{fp(1)}, list(DaysLeftExpired), "expired=notAfter≤now")
	assert.ElementsMatch(t, []string{fp(2), fp(6)}, list(DaysLeftLE7))
	assert.ElementsMatch(t, []string{fp(2), fp(3), fp(6)}, list(DaysLeftLE14))
	assert.ElementsMatch(t, []string{fp(2), fp(3), fp(4), fp(6)}, list(DaysLeftLE30))
	assert.ElementsMatch(t, []string{fp(5)}, list(DaysLeftGT30), "已过期不落入 gt30")

	// 非法档位 → 明确错误
	_, err := f.svc.ListCerts(ctx, ListCertsQuery{DaysLeft: "tomorrow"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daysLeft")
}

func TestLedgerListSearch(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	f.seedCert(t, fp(1), func(c *domain.Certificate) {
		c.CommonName = "shop.example.com"
		c.Sans = []string{"api.shop.example.com", "cdn.shop.example.com"}
	})
	f.seedCert(t, fp(2), func(c *domain.Certificate) {
		c.CommonName = "portal.example.org"
		c.Sans = []string{"portal.example.org"}
	})

	search := func(q string) []string {
		res, err := f.svc.ListCerts(ctx, ListCertsQuery{Search: q})
		require.NoError(t, err)
		out := make([]string, 0, len(res.Items))
		for _, it := range res.Items {
			out = append(out, it.Fingerprint)
		}
		return out
	}

	assert.ElementsMatch(t, []string{fp(1)}, search("shop.example"), "commonName 子串")
	assert.ElementsMatch(t, []string{fp(1)}, search("cdn.shop"), "SAN 子串")
	assert.ElementsMatch(t, []string{fp(1)}, search(fp(1)[:12]), "指纹片段")
	assert.ElementsMatch(t, []string{fp(1)}, search("SHOP.EXAMPLE"), "不区分大小写")
	assert.Empty(t, search("nomatch.example"), "无命中返回空页（total=0）")

	// 正则元字符按字面处理，不得 500/误匹配
	res, err := f.svc.ListCerts(ctx, ListCertsQuery{Search: "shop.*"})
	require.NoError(t, err)
	assert.Empty(t, res.Items, "元字符不构成通配匹配")
}

func TestLedgerListRefCount(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	f.seedCert(t, fp(1), nil)
	f.seedCert(t, fp(2), nil)
	f.seedCert(t, fp(3), nil)

	// 无快照：全部 refCount=0
	res, err := f.svc.ListCerts(ctx, ListCertsQuery{})
	require.NoError(t, err)
	for _, it := range res.Items {
		assert.Zero(t, it.RefCount)
	}

	// 最新成功快照：fp1 两条、fp2 一条、fp3 零条
	snapID := f.seedDoneSnapshot(t, -time.Hour, nil)
	f.seedRef(t, fp(1), snapID, domain.CloudAliyun, domain.ProductCDN)
	f.seedRef(t, fp(1), snapID, domain.CloudTencent, domain.ProductWAF)
	f.seedRef(t, fp(2), snapID, domain.CloudAliyun, domain.ProductCDN)

	res, err = f.svc.ListCerts(ctx, ListCertsQuery{})
	require.NoError(t, err)
	byFP := map[string]int{}
	for _, it := range res.Items {
		byFP[it.Fingerprint] = it.RefCount
	}
	assert.Equal(t, map[string]int{fp(1): 2, fp(2): 1, fp(3): 0}, byFP)
}

// ---------------------------------------------------------------------
// AC2：详情（全要素 + hasKey 语义 + 引用三态）
// ---------------------------------------------------------------------

func TestLedgerGetCertDetail(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()

	id1 := f.seedCert(t, fp(1), func(c *domain.Certificate) {
		c.EncryptedPrivateKey = &domain.EncryptedSecret{Ciphertext: "Y2lwaGVydGV4dA==", KeyVersion: 1, Algo: domain.AlgoAES256GCM}
		c.ExpectedDomain = "san-aa0001.example.com"
		c.ProtectUntil = ptrTime(time.Now().Add(48 * time.Hour))
	})
	// 最新快照有引用 → has_refs
	snapID := f.seedDoneSnapshot(t, -time.Hour, []domain.CoverageMeta{{Cloud: "aliyun", Product: "cdn", Covered: 1, Total: 1}})
	f.seedRef(t, fp(1), snapID, domain.CloudAliyun, domain.ProductCDN)

	d, err := f.svc.GetCert(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, fp(1), d.Fingerprint)
	assert.Equal(t, "cn-aa0001.example.com", d.CommonName)
	assert.Equal(t, []string{"san-aa0001.example.com"}, d.Sans)
	assert.Equal(t, "certtest Intermediate CA", d.Issuer)
	assert.Equal(t, "serial-aa0001", d.SerialNumber)
	assert.Equal(t, domain.KeyAlgorithmECDSA, d.KeyAlgorithm)
	assert.Equal(t, domain.HostingStatusComplete, d.HostingStatus)
	assert.True(t, d.HasKey, "有密文私钥 → hasKey=true")
	assert.False(t, d.NotBefore.IsZero())
	assert.False(t, d.CreatedAt.IsZero())
	assert.InDelta(t, 365, float64(d.DaysLeft), 1, "约一年后到期（种子与查询时点边界容忍 ±1 天）")
	require.NotNil(t, d.ProtectUntil)
	assert.Equal(t, domain.RefStatusHasRefs, d.ReferenceStatus)
	assert.Equal(t, 1, d.RefCount)

	// 仅指纹登记 → hasKey=false
	id2 := f.seedCert(t, fp(2), func(c *domain.Certificate) { c.HostingStatus = domain.HostingStatusFingerprintOnly })
	d2, err := f.svc.GetCert(ctx, id2)
	require.NoError(t, err)
	assert.False(t, d2.HasKey)
	assert.Equal(t, domain.RefStatusNoRefsScanned, d2.ReferenceStatus, "历史无引用=空涉及集，成功快照存在即视为已覆盖")

	// 未命中 / 非法 ID
	_, err = f.svc.GetCert(ctx, "000000000000000000000000")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	_, err = f.svc.GetCert(ctx, "not-hex")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

// ---------------------------------------------------------------------
// AC3：删除拦截三分支
// ---------------------------------------------------------------------

func TestLedgerDeleteBranchHasRefs(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	id := f.seedCert(t, fp(1), nil)
	snapID := f.seedDoneSnapshot(t, -time.Hour, nil)
	f.seedRef(t, fp(1), snapID, domain.CloudAliyun, domain.ProductCDN)
	f.seedRef(t, fp(1), snapID, domain.CloudTencent, domain.ProductCLB)

	err := f.svc.DeleteCert(ctx, id)
	require.Error(t, err)
	var blocked *domain.DeleteBlockedError
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, domain.RefStatusHasRefs, blocked.ReferenceStatus)
	assert.Equal(t, 2, blocked.RefCount)
	assert.Contains(t, blocked.Reason, "2")
	assert.Nil(t, blocked.ProtectUntil)

	// 拦截后证书仍在台账
	_, err = f.certs.GetByID(ctx, id)
	require.NoError(t, err)
}

func TestLedgerDeleteBranchBlindSpotNoSnapshot(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	id := f.seedCert(t, fp(1), nil)
	// 不建任何成功快照 → blind_spot

	err := f.svc.DeleteCert(ctx, id)
	require.Error(t, err)
	var blocked *domain.DeleteBlockedError
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, domain.RefStatusBlindSpot, blocked.ReferenceStatus)
	assert.NotEmpty(t, blocked.Reason, "blind_spot 附盲区原因")
	assert.Contains(t, blocked.Reason, "snapshot")
}

func TestLedgerDeleteBranchBlindSpotScopeUncovered(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	id := f.seedCert(t, fp(1), nil)

	// 旧快照：覆盖 tencent/waf，发现该指纹引用（构成"涉及云/产品"）
	oldSnap := f.seedDoneSnapshot(t, -2*time.Hour, []domain.CoverageMeta{{Cloud: "tencent", Product: "waf", Covered: 1, Total: 1}})
	f.seedRef(t, fp(1), oldSnap, domain.CloudTencent, domain.ProductWAF)
	// 最新快照：仅覆盖 aliyun/cdn，无该指纹引用 → 未覆盖涉及范围 → blind_spot
	f.seedDoneSnapshot(t, -1*time.Hour, []domain.CoverageMeta{{Cloud: "aliyun", Product: "cdn", Covered: 0, Total: 5}})

	err := f.svc.DeleteCert(ctx, id)
	require.Error(t, err)
	var blocked *domain.DeleteBlockedError
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, domain.RefStatusBlindSpot, blocked.ReferenceStatus)
	assert.Contains(t, blocked.Reason, "tencent/waf", "盲区原因指明未覆盖云/产品")
}

func TestLedgerDeleteBranchProtectionPeriod(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	f.seedDoneSnapshot(t, -time.Hour, nil) // 成功快照存在，证书无引用 → no_refs_scanned

	// 保护期内（>=now 禁删）
	until := time.Now().Add(48 * time.Hour)
	id := f.seedCert(t, fp(1), func(c *domain.Certificate) { c.ProtectUntil = &until })
	err := f.svc.DeleteCert(ctx, id)
	require.Error(t, err)
	var blocked *domain.DeleteBlockedError
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, domain.RefStatusNoRefsScanned, blocked.ReferenceStatus)
	require.NotNil(t, blocked.ProtectUntil)
	assert.WithinDuration(t, until, *blocked.ProtectUntil, time.Second)
	assert.Contains(t, blocked.Reason, "protection")
	_, err = f.certs.GetByID(ctx, id)
	require.NoError(t, err, "保护期内不得删除")

	// 保护期已过（protectUntil < now）→ 放行
	expired := time.Now().Add(-time.Hour)
	id2 := f.seedCert(t, fp(2), func(c *domain.Certificate) { c.ProtectUntil = &expired })
	require.NoError(t, f.svc.DeleteCert(ctx, id2))
	_, err = f.certs.GetByID(ctx, id2)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)

	// 无保护期 + no_refs_scanned → 放行
	id3 := f.seedCert(t, fp(3), nil)
	require.NoError(t, f.svc.DeleteCert(ctx, id3))
	_, err = f.certs.GetByID(ctx, id3)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

func TestLedgerDeleteNotFound(t *testing.T) {
	f := newLedgerFixture(t)
	err := f.svc.DeleteCert(context.Background(), "000000000000000000000000")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	err = f.svc.DeleteCert(context.Background(), "zz")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

// ---------------------------------------------------------------------
// AC4/AC5：stats 双口径覆盖率（并集去重 / 实时聚合）
// ---------------------------------------------------------------------

func TestLedgerStatsUnionDedup(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()

	// 台账：A complete、B fingerprint_only、C complete
	f.seedCert(t, fp(1), nil)
	f.seedCert(t, fp(2), func(c *domain.Certificate) { c.HostingStatus = domain.HostingStatusFingerprintOnly })
	f.seedCert(t, fp(3), nil)

	// 最新成功快照发现：A×2（同指纹去重）、X、Y（未登记）
	snapID := f.seedDoneSnapshot(t, -time.Hour, nil)
	f.seedRef(t, fp(1), snapID, domain.CloudAliyun, domain.ProductCDN)
	f.seedRef(t, fp(1), snapID, domain.CloudTencent, domain.ProductWAF)
	f.seedRef(t, fp(11), snapID, domain.CloudAliyun, domain.ProductCDN)
	f.seedRef(t, fp(12), snapID, domain.CloudAWS, domain.ProductCDN)

	st, err := f.svc.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, st.Total)
	assert.Equal(t, 2, st.Complete)
	assert.Equal(t, 1, st.FingerprintOnly)
	assert.Equal(t, 5, st.Denominator, "分母=|{A,B,C}∪{A,X,Y}|=5（部分重叠并集去重）")
	assert.Equal(t, 2, st.MissingRegistrations, "扫描发现未登记 X/Y")
	assert.Equal(t, 3, st.DenominatorSources.ScannedUniqueFingerprints, "扫描指纹去重 {A,X,Y}")
	assert.Equal(t, 2, st.DenominatorSources.ManualOnlyFingerprints, "台账独有 {B,C}")
	assert.InDelta(t, 0.6, st.RegistrationRate, 1e-9, "登记覆盖率=3/5")
	assert.InDelta(t, 0.4, st.ReplaceableRate, 1e-9, "可更换托管覆盖率=2/5")
	assert.InDelta(t, 1.0/3.0, st.FingerprintOnlyRate, 0.0001, "仅指纹占比=台账内 1/3")
}

func TestLedgerStatsNoSnapshot(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	f.seedCert(t, fp(1), nil)
	f.seedCert(t, fp(2), func(c *domain.Certificate) { c.HostingStatus = domain.HostingStatusFingerprintOnly })

	st, err := f.svc.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, st.Total)
	assert.Equal(t, 2, st.Denominator, "无成功快照时并集自然退化为台账集合（非口径退化）")
	assert.Zero(t, st.MissingRegistrations)
	assert.Equal(t, 2, st.DenominatorSources.ManualOnlyFingerprints)
	assert.Zero(t, st.DenominatorSources.ScannedUniqueFingerprints)
	assert.InDelta(t, 1.0, st.RegistrationRate, 1e-9)
	assert.InDelta(t, 0.5, st.ReplaceableRate, 1e-9)
	assert.InDelta(t, 0.5, st.FingerprintOnlyRate, 1e-9)
}

func TestLedgerStatsFailedSnapshotIgnored(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	f.seedCert(t, fp(1), nil)

	// 仅 failed 快照（含其引用）不得计入分母
	snap := &domain.ScanSnapshot{StartedAt: time.Now().Add(-time.Hour)}
	snapID, err := f.snaps.Create(ctx, snap)
	require.NoError(t, err)
	require.NoError(t, f.snaps.MarkFinished(ctx, snapID, domain.ScanStatusFailed, "boom"))
	f.seedRef(t, fp(11), snapID, domain.CloudAliyun, domain.ProductCDN)

	st, err := f.svc.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Denominator, "failed 快照不作为分母来源（仅 status=done）")
	assert.Zero(t, st.MissingRegistrations)
}

func TestLedgerStatsRealtimeAndEmpty(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()

	st, err := f.svc.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, st.Total)
	assert.Zero(t, st.Denominator)
	assert.Zero(t, st.RegistrationRate, "分母为 0 时比率取 0")

	// 实时聚合：台账变化立即反映（无存储快照）
	f.seedCert(t, fp(9), nil)
	st, err = f.svc.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Total)
	assert.Equal(t, 1, st.Denominator)
}

// ---------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------

func ptrTime(t time.Time) *time.Time { return &t }
