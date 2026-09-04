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
)

// discoveryPreviewDeps 发现预览服务测试依赖（内存假实现句柄）。
//
// Hard Rule（纯 DB 聚合）的测试面证明：NewDiscoveryPreviewService 仅接受
// 四个仓储端口（快照/引用/台账/映射），无任何云适配器或账号源可注入——
// 编译期即不存在云 API 调用面，本文件全部用例不构造任何云侧桩。
type discoveryPreviewDeps struct {
	snaps    *certtest.FakeScanSnapshotRepo
	refs     *certtest.FakeCertReferenceRepo
	certs    *certtest.FakeCertificateRepo
	mappings *certtest.FakeCloudCertMappingRepo
}

func newDiscoveryPreviewDeps() *discoveryPreviewDeps {
	return &discoveryPreviewDeps{
		snaps:    certtest.NewFakeScanSnapshotRepo(),
		refs:     certtest.NewFakeCertReferenceRepo(),
		certs:    certtest.NewFakeCertificateRepo(),
		mappings: certtest.NewFakeCloudCertMappingRepo(),
	}
}

func (d *discoveryPreviewDeps) svc() DiscoveryPreviewService {
	return NewDiscoveryPreviewService(d.snaps, d.refs, d.certs, d.mappings)
}

// dfp 互异 64 位 hex 指纹（与 web 层 lfp 同构，service 包内独立）。
func dfp(i int) string { return fmt.Sprintf("bb%04x%058x", i, i) }

// seedDoneSnapshot 固定 startedAt 的 done 快照。
func (d *discoveryPreviewDeps) seedDoneSnapshot(t *testing.T, startedAt time.Time) string {
	t.Helper()
	id, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: startedAt})
	require.NoError(t, err)
	require.NoError(t, d.snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
	return id
}

// seedRef 单条引用播种（fp 为引用指纹；product 默认 cdn）。
func (d *discoveryPreviewDeps) seedRef(t *testing.T, snapID string, cloud domain.Cloud, accountKey, cloudCertID, fp, resourceID string) {
	t.Helper()
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint:       fp,
		Cloud:                 cloud,
		Product:               domain.ProductCDN,
		ResourceID:            resourceID,
		ReferencedCloudCertID: cloudCertID,
		AccountKey:            accountKey,
		SnapshotID:            snapID,
	}})
	require.NoError(t, err)
}

// seedLedgerCert 台账证书（NotAfter 固定便于断言）。
func (d *discoveryPreviewDeps) seedLedgerCert(t *testing.T, fp string, notAfter time.Time) {
	t.Helper()
	require.NoError(t, d.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   fp,
		CommonName:    "cn-" + fp[:6],
		NotAfter:      notAfter,
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}))
}

// ---------------------------------------------------------------------
// SC-1：去重公式与排除口径（纯 DB 聚合，台账空）
// ---------------------------------------------------------------------

func TestDiscoveryPreview_SC1_DedupAndExclusions(t *testing.T) {
	d := newDiscoveryPreviewDeps()
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	snapID := d.seedDoneSnapshot(t, started)

	// 同三元组多资源引用（cdn-res-1/2 同证书 cert-A）→ 去重为 1 条
	d.seedRef(t, snapID, domain.CloudAliyun, "acct-main", "cert-A", dfp(1), "cdn-res-1")
	d.seedRef(t, snapID, domain.CloudAliyun, "acct-main", "cert-A", dfp(1), "cdn-res-2")
	// 同云同账号不同证书 → 独立条目
	d.seedRef(t, snapID, domain.CloudAliyun, "acct-main", "cert-B", dfp(2), "cdn-res-3")
	// 同证书不同账号 → 独立条目（多账号各建映射语义）
	d.seedRef(t, snapID, domain.CloudAliyun, "acct-other", "cert-A", dfp(1), "cdn-res-4")
	// 占位指纹条目（腾讯 SHA-1 口径回退）——计入
	d.seedRef(t, snapID, domain.CloudTencent, "acct-tx", "ssl-9",
		placeholderFingerprintFor("tencent", "acct-tx", "ssl-9"), "waf-res-1")
	// 华为云不可选组——计入（整组 parseable=false）
	d.seedRef(t, snapID, domain.CloudHuawei, "acct-hw", "cert-H", dfp(3), "hw-res-1")
	// AWS ARN 形态（可解析）与 IAM-hosted 非 ARN（降级）——均计入
	d.seedRef(t, snapID, domain.CloudAWS, "acct-aws", "arn:aws:acm:us-east-1:123:certificate/abc", dfp(4), "aws-res-1")
	d.seedRef(t, snapID, domain.CloudAWS, "acct-aws", "iam-cert-123", dfp(5), "aws-res-2")
	// 排除口径：product=crd（K8s 引用）
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint: dfp(6), Product: domain.ProductCRD, ClusterID: "cluster-a",
		ResourceID: "gw-1", ReferencedCloudCertID: "k8s-cert", SnapshotID: snapID,
	}})
	require.NoError(t, err)
	// 排除口径：空 cloud 条目（非 crd 也排除）
	_, err = d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint: dfp(7), Product: domain.ProductCDN,
		ResourceID: "res-empty-cloud", ReferencedCloudCertID: "cert-X", SnapshotID: snapID,
	}})
	require.NoError(t, err)

	got, err := d.svc().Preview(context.Background())
	require.NoError(t, err)

	// 条目数 = 三元组去重数（crd 与空 cloud 不计入；含占位/华为/IAM-hosted 组）
	require.Len(t, got.Items, 7)
	assert.Equal(t, snapID, got.SnapshotID)
	assert.Equal(t, started, got.SnapshotStartedAt)

	// 字典序稳定（cloud → accountKey → cloudCertId）
	wantOrder := []struct{ cloud, account, certID string }{
		{"aliyun", "acct-main", "cert-A"},
		{"aliyun", "acct-main", "cert-B"},
		{"aliyun", "acct-other", "cert-A"},
		{"aws", "acct-aws", "arn:aws:acm:us-east-1:123:certificate/abc"},
		{"aws", "acct-aws", "iam-cert-123"},
		{"huawei", "acct-hw", "cert-H"},
		{"tencent", "acct-tx", "ssl-9"},
	}
	for i, w := range wantOrder {
		assert.Equal(t, w.cloud, got.Items[i].Cloud, "item %d cloud", i)
		assert.Equal(t, w.account, got.Items[i].AccountKey, "item %d accountKey", i)
		assert.Equal(t, w.certID, got.Items[i].CloudCertID, "item %d cloudCertId", i)
	}
	// 同三元组两资源引用 → refCount=2；台账空 → 全部未登记
	assert.Equal(t, 2, got.Items[0].RefCount)
	for _, it := range got.Items {
		assert.False(t, it.InLedger, "台账空时全部 inLedger=false（%s/%s）", it.Cloud, it.CloudCertID)
		assert.Nil(t, it.NotAfter)
	}
	// label = 首个引用 resourceId（预览可读名：cas=证书名称、cdn/waf=域名）
	assert.Equal(t, "cdn-res-1", got.Items[0].Label)
	assert.Equal(t, "waf-res-1", got.Items[6].Label)
}

// ---------------------------------------------------------------------
// SC-2：七类字段 + 双通道 inLedger + notAfter 口径
// ---------------------------------------------------------------------

func TestDiscoveryPreview_SC2_FieldsAndDualChannel(t *testing.T) {
	d := newDiscoveryPreviewDeps()
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	snapID := d.seedDoneSnapshot(t, started)
	ledgerNA := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	mappedNA := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	// 通道一（台账指纹命中）：cert-B 引用真实指纹 dfp(2)，台账已有该证书
	d.seedRef(t, snapID, domain.CloudAliyun, "acct-main", "cert-B", dfp(2), "cdn-res-3")
	d.seedLedgerCert(t, dfp(2), ledgerNA)
	// 通道二（映射命中）：ssl-8 为占位指纹条目，CloudCertMapping 已建档
	//（导入后重跑预览的灰选场景——指纹通道对占位条目恒 miss）
	d.seedRef(t, snapID, domain.CloudTencent, "acct-tx", "ssl-8",
		placeholderFingerprintFor("tencent", "acct-tx", "ssl-8"), "waf-res-8")
	d.seedLedgerCert(t, dfp(9), mappedNA)
	require.NoError(t, d.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: dfp(9), Cloud: "tencent", AccountKey: "acct-tx", CloudCertID: "ssl-8",
	}))
	// 未登记占位条目（无映射）：notAfter 占位、可解析标记 deferred_parse
	d.seedRef(t, snapID, domain.CloudTencent, "acct-tx", "ssl-7",
		placeholderFingerprintFor("tencent", "acct-tx", "ssl-7"), "waf-res-7")

	got, err := d.svc().Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Items, 3)

	byKey := map[string]DiscoveryPreviewEntry{}
	for _, it := range got.Items {
		byKey[it.Cloud+"/"+it.AccountKey+"/"+it.CloudCertID] = it
	}

	// 通道一：真实指纹在台账 → inLedger + 台账 NotAfter
	fpHit := byKey["aliyun/acct-main/cert-B"]
	assert.True(t, fpHit.InLedger, "台账指纹命中 → inLedger=true")
	require.NotNil(t, fpHit.NotAfter)
	assert.Equal(t, ledgerNA, *fpHit.NotAfter)
	assert.True(t, fpHit.Parseable)
	assert.Empty(t, fpHit.ParseReason)
	assert.Equal(t, 1, fpHit.RefCount)

	// 通道二：占位指纹条目靠映射灰选 + 映射指纹台账 NotAfter
	mapHit := byKey["tencent/acct-tx/ssl-8"]
	assert.True(t, mapHit.InLedger, "FindByCloudCertID 命中 → inLedger=true（占位条目重跑灰选）")
	require.NotNil(t, mapHit.NotAfter)
	assert.Equal(t, mappedNA, *mapHit.NotAfter)

	// 未登记占位条目：inLedger=false、notAfter nil（web 层占位显示）、
	// parseable=true + deferred_parse（导入时解析，仍可选）
	pending := byKey["tencent/acct-tx/ssl-7"]
	assert.False(t, pending.InLedger)
	assert.Nil(t, pending.NotAfter)
	assert.True(t, pending.Parseable)
	assert.Equal(t, DiscoveryParseDeferred, pending.ParseReason)
}

// TestDiscoveryPreview_ParseableMatrix 可解析标记口径（SC-8 后端部分）：
// 华为整组不可选、AWS IAM-hosted 同语义降级、占位指纹可选（导入时解析）。
func TestDiscoveryPreview_ParseableMatrix(t *testing.T) {
	d := newDiscoveryPreviewDeps()
	snapID := d.seedDoneSnapshot(t, time.Now())
	d.seedRef(t, snapID, domain.CloudHuawei, "acct-hw", "cert-H", dfp(3), "hw-res-1")
	d.seedRef(t, snapID, domain.CloudAWS, "acct-aws", "arn:aws:acm:us-east-1:123:certificate/abc", dfp(4), "aws-res-1")
	d.seedRef(t, snapID, domain.CloudAWS, "acct-aws", "iam-cert-123", dfp(5), "aws-res-2")

	got, err := d.svc().Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Items, 3)

	byCloud := map[string]DiscoveryPreviewEntry{}
	for _, it := range got.Items {
		byCloud[it.Cloud+"/"+it.CloudCertID] = it
	}
	hw := byCloud["huawei/cert-H"]
	assert.False(t, hw.Parseable, "华为云整组不可选")
	assert.Equal(t, DiscoveryParseUnsupportedCloud, hw.ParseReason)
	acm := byCloud["aws/arn:aws:acm:us-east-1:123:certificate/abc"]
	assert.True(t, acm.Parseable, "AWS ARN 形态可选")
	assert.Empty(t, acm.ParseReason)
	iam := byCloud["aws/iam-cert-123"]
	assert.False(t, iam.Parseable, "AWS IAM-hosted（非 ARN）降级不可选")
	assert.Equal(t, DiscoveryParseIAMHosted, iam.ParseReason)
}

// ---------------------------------------------------------------------
// SC-3：无 done 快照 → NO_SNAPSHOT 哨兵
// ---------------------------------------------------------------------

func TestDiscoveryPreview_SC3_NoSnapshot(t *testing.T) {
	t.Run("zero snapshots", func(t *testing.T) {
		d := newDiscoveryPreviewDeps()
		_, err := d.svc().Preview(context.Background())
		assert.ErrorIs(t, err, domain.ErrNoSnapshot)
	})
	t.Run("only running snapshot is not previewable", func(t *testing.T) {
		d := newDiscoveryPreviewDeps()
		_, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{})
		require.NoError(t, err)
		_, err = d.svc().Preview(context.Background())
		assert.ErrorIs(t, err, domain.ErrNoSnapshot, "running 快照不可作预览数据源")
	})
	t.Run("only failed snapshot is not previewable", func(t *testing.T) {
		d := newDiscoveryPreviewDeps()
		id, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{})
		require.NoError(t, err)
		require.NoError(t, d.snaps.MarkFinished(context.Background(), id, domain.ScanStatusFailed, "SCAN_DISCOVERY_FAILED"))
		_, err = d.svc().Preview(context.Background())
		assert.ErrorIs(t, err, domain.ErrNoSnapshot)
	})
}

// ---------------------------------------------------------------------
// snapshot-status：状态/startedAt/partialFailures + 零快照空态
// ---------------------------------------------------------------------

func TestDiscoverySnapshotStatus(t *testing.T) {
	t.Run("zero snapshots empty state (not NO_SNAPSHOT)", func(t *testing.T) {
		d := newDiscoveryPreviewDeps()
		got, err := d.svc().SnapshotStatus(context.Background())
		require.NoError(t, err, "零快照为空态响应而非错误——区别于 preview 的 NO_SNAPSHOT 引导语义")
		assert.False(t, got.HasSnapshot)
		assert.Empty(t, got.Status)
		require.NotNil(t, got.PartialFailures)
		assert.Empty(t, got.PartialFailures)
	})

	t.Run("running latest", func(t *testing.T) {
		d := newDiscoveryPreviewDeps()
		started := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
		_, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: started})
		require.NoError(t, err)
		got, err := d.svc().SnapshotStatus(context.Background())
		require.NoError(t, err)
		assert.True(t, got.HasSnapshot)
		assert.Equal(t, domain.ScanStatusRunning, got.Status)
		assert.Equal(t, started, got.StartedAt)
	})

	t.Run("latest regardless of status (newer failed shadows older done)", func(t *testing.T) {
		d := newDiscoveryPreviewDeps()
		d.seedDoneSnapshot(t, time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC))
		failedID, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{
			StartedAt: time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		require.NoError(t, d.snaps.FinishScan(context.Background(), failedID,
			domain.ScanStatusFailed, domain.FailReasonScanDiscoveryFailed, nil,
			[]domain.ScanChannelFailure{
				{Cloud: "huawei", Product: "cdn", Account: "acct-hw", Reason: "list refs failed"},
			}))

		got, err := d.svc().SnapshotStatus(context.Background())
		require.NoError(t, err)
		assert.True(t, got.HasSnapshot)
		assert.Equal(t, domain.ScanStatusFailed, got.Status, "最新快照不限状态（failed 需携带 partialFailures 供引导呈现）")
		assert.Equal(t, failedID, got.SnapshotID)
		assert.Equal(t, domain.FailReasonScanDiscoveryFailed, got.FailReason)
		require.Len(t, got.PartialFailures, 1)
		assert.Equal(t, "huawei", got.PartialFailures[0].Cloud)
		assert.Equal(t, "acct-hw", got.PartialFailures[0].Account)
	})

	t.Run("done latest", func(t *testing.T) {
		d := newDiscoveryPreviewDeps()
		id := d.seedDoneSnapshot(t, time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC))
		got, err := d.svc().SnapshotStatus(context.Background())
		require.NoError(t, err)
		assert.True(t, got.HasSnapshot)
		assert.Equal(t, domain.ScanStatusDone, got.Status)
		assert.Equal(t, id, got.SnapshotID)
		require.NotNil(t, got.PartialFailures)
	})
}

// TestDiscoveryPreview_EmptyItems done 快照无云引用 → 空清单（非错误）。
func TestDiscoveryPreview_EmptyItems(t *testing.T) {
	d := newDiscoveryPreviewDeps()
	d.seedDoneSnapshot(t, time.Now())
	got, err := d.svc().Preview(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got.Items)
	assert.Empty(t, got.Items, "无可用云引用 → 空清单（K8s-only 快照等场景）")
}

// TestDiscoveryPreview_CASLibraryEntries 证书库条目（product=cas）与资源引用
// 条目同管道聚合（cert-cas-library-scan）：预览/导入零接口改动——cas 条目按
// (cloud, accountKey, cloudCertId) 三元组去重出条目、parseable=true 可勾选。
// 同证书多形态条目（cas "27029968" vs waf "27029968-cn-hangzhou"）是预期行为：
// 两个三元组各自展示，导入收敛同指纹。
func TestDiscoveryPreview_CASLibraryEntries(t *testing.T) {
	d := newDiscoveryPreviewDeps()
	snapID := d.seedDoneSnapshot(t, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))

	// 同三元组两条 cas 引用（同证书不同引用名）→ 去重 1 条 refCount=2
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{
		{CertFingerprint: dfp(1), Cloud: domain.CloudAliyun, Product: domain.ProductCAS,
			ResourceID: "jlccam.com-2026-09", ReferencedCloudCertID: "27029968",
			AccountKey: "acct-main", SnapshotID: snapID},
		{CertFingerprint: dfp(1), Cloud: domain.CloudAliyun, Product: domain.ProductCAS,
			ResourceID: "jlccam.com-2026-09-san", ReferencedCloudCertID: "27029968",
			AccountKey: "acct-main", SnapshotID: snapID},
		// 同证书库另一张证书 → 独立条目
		{CertFingerprint: dfp(2), Cloud: domain.CloudAliyun, Product: domain.ProductCAS,
			ResourceID: "legacy.example.com-2025", ReferencedCloudCertID: "20275346",
			AccountKey: "acct-main", SnapshotID: snapID},
		// 同证书绑定 WAF 的资源引用形态（cloudCertId 带 region 后缀）→ 独立条目
		{CertFingerprint: dfp(1), Cloud: domain.CloudAliyun, Product: domain.ProductWAF,
			ResourceID: "jlccam.com", ReferencedCloudCertID: "27029968-cn-hangzhou",
			AccountKey: "acct-main", SnapshotID: snapID},
	})
	require.NoError(t, err)

	got, err := d.svc().Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Items, 3)

	byKey := map[string]DiscoveryPreviewEntry{}
	for _, it := range got.Items {
		byKey[it.Cloud+"/"+it.AccountKey+"/"+it.CloudCertID] = it
	}
	casEntry := byKey["aliyun/acct-main/27029968"]
	assert.Equal(t, 2, casEntry.RefCount, "同三元组两条 cas 引用去重后 refCount=2")
	assert.True(t, casEntry.Parseable, "cas 条目可选（非华为/AWS IAM-hosted）")
	assert.Empty(t, casEntry.ParseReason)
	assert.False(t, casEntry.InLedger, "台账空 → 未登记可导入")

	assert.Equal(t, 1, byKey["aliyun/acct-main/20275346"].RefCount)
	assert.Equal(t, 1, byKey["aliyun/acct-main/27029968-cn-hangzhou"].RefCount,
		"waf 资源引用形态独立三元组（双条目预期行为，不做合并）")

	// 字典序（cloudCertId）：20275346 < 27029968 < 27029968-cn-hangzhou
	assert.Equal(t, "20275346", got.Items[0].CloudCertID)
	assert.Equal(t, "27029968", got.Items[1].CloudCertID)
	assert.Equal(t, "27029968-cn-hangzhou", got.Items[2].CloudCertID)
}

// TestPlaceholderFingerprintForFormula 占位公式与扫描侧同源（certscan-unresolved
// 前缀 + 三元组 pipe 连接 cacheKey），确定性可重算。
func TestPlaceholderFingerprintForFormula(t *testing.T) {
	fp := placeholderFingerprintFor("tencent", "acct-tx", "ssl-9")
	assert.Regexp(t, `^[0-9a-f]{64}$`, fp)
	// 与扫描侧公式逐字一致（resolveUncached 的 cacheKey 口径）
	assert.Equal(t, sha256Hex("certscan-unresolved:tencent|acct-tx|ssl-9"), fp)
	assert.Equal(t, fp, placeholderFingerprintFor("tencent", "acct-tx", "ssl-9"), "确定性可重算")
	assert.NotEqual(t, fp, placeholderFingerprintFor("tencent", "acct-tx", "ssl-10"))
}
