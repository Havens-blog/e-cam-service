// @feature cert-volcano-deployer @api-functional
//
// Volcano replacement execution-closure journeys ( cert-volcano-deployer
// task 5 ): the 6th cloud driven through the production two-phase engine —
//   - two-phase deploy ( UploadCertForProduct -> mapping active -> BindResource )
//     with {product}:{id} normalized cloud cert IDs;
//   - bind-failure compensation ( mapping active->orphan + idempotent
//     CleanupOrphan );
//   - verify window ( ProbeDomains cloud-agnostic reuse );
//   - rollback ( GetCert three-judgment precheck + rebind per product );
//   - channel dispatch visibility ( volcano targets reach the volcano
//     deployer ).
//
// Journey invariants: 火山与五云同一状态机、同一两段式编排；差异仅在证书库
// 形态（四产品库独立 → 第一段按产品定向上传，产物即该库可绑定证书）。
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package volcano_cert_replacement

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Journey: happy path over all four products — generate ( 4 volcano refs ) ->
// confirm batched -> per-batch two-phase execute ( product-directed upload ->
// mapping active -> bind ) -> verify-window probes -> old-cert orphan
// cleanup. Covers AC-1 ( two-phase + mapping form ), AC-3 ( verify window
// cloud-agnostic reuse ) and AC-5 ( per-product dispatch to the volcano
// deployer ).
func TestVolcanoCertReplacement_FullLifecycleSmoke(t *testing.T) {
	stub := NewStubVolcanoCertLibrary()
	h := newVolcanoHarness(t, stub)
	w := seedVolcanoReplacement(t, h)
	seedLiveResources(t, stub, w)

	// ---- Step 1: 生成覆盖火山四产品的变更清单 ----
	list := h.MustGenerate(w.OldFP, w.NewCertID)
	require.Len(t, list.Items, 4)
	for _, item := range list.Items {
		assert.True(t, item.AutoChangeable, "volcano refs are executable (cloud_api channel)")
		assert.Equal(t, "volcano", item.Target.Cloud)
	}

	// ---- Step 2: 确认并分批执行（2 批，每批 2 项） ----
	h.MustConfirm(list.OrderID, batchedVolcanoConf())

	for batch := 1; batch <= 2; batch++ {
		h.MustExecute(list.OrderID)
		progress := h.MustProgress(list.OrderID)
		for _, state := range progress.ItemStates {
			if state.BatchNo == batch {
				assert.Equal(t, string(domain.ItemStatusSuccess), state.Status,
					"batch %d item succeeded (error=%s)", batch, state.Error)
			}
		}
		if batch < 2 {
			// 非终批：进入验证窗口后人工续批。
			h.Prober.SetOnlineFingerprint(w.Domain, w.NewFP)
			probeRounds(t, h, verifyConfirmProbes)
			h.MustConfirmBatch(list.OrderID)
		}
	}

	// AC-5（编排层 product 分发可见性）：第一段按目标产品定向其产品证书库
	// 上传——csv 统一库不再产生上传产物；alb/nlb 共用 ALB 监听证书库。
	csvN, cdnN, wafN, albN := stub.UploadCounts()
	assert.Equal(t, 0, csvN, "第一段不再落 csv 统一库（产品库定向上传接通）")
	assert.Equal(t, 1, cdnN, "cdn 项定向上传 CDN 证书库")
	assert.Equal(t, 1, wafN, "waf 项定向上传 WAF 服务证书库")
	assert.Equal(t, 2, albN, "alb/nlb 项共用 ALB 监听证书库")

	// ---- 深度（跨实体）：四产品映射齐备、{product}:{id} 归一形态、绑定消费
	// 本条上传产物 ----
	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 4)
	products := map[string]bool{}
	for _, item := range items {
		require.NotEmpty(t, item.NewCloudCertID)
		prefix, _, ok := strings.Cut(item.NewCloudCertID, ":")
		require.True(t, ok, "cloud cert id normalized {product}:{id}: %s", item.NewCloudCertID)
		assert.Equal(t, string(item.ResourceRef.Product), prefix,
			"映射前缀=目标产品（产品库定向产物）")
		products[prefix] = true

		mapping, merr := h.MappingByCloudCert("volcano", item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, merr, "mapping for %s", prefix)
		assert.Equal(t, domain.MappingStatusActive, mapping.Status)
		assert.Equal(t, w.NewFP, mapping.CertFingerprint)

		// 两段式顺序：绑定调用使用的是本条上传产物（同一 raw ID）。
		binds := stub.BindsByProduct(prefix)
		require.NotEmpty(t, binds, "binds for %s", prefix)
		assert.Contains(t, item.NewCloudCertID, binds[len(binds)-1].CloudCertID)
	}
	assert.True(t, products["cdn"] && products["waf"] && products["alb"] && products["nlb"],
		"四产品 ID 空间全部接通")

	// ---- Step 3: 验证窗口拨测确认线上指纹（终批，云无关复用） ----
	h.Prober.SetOnlineFingerprint(w.Domain, w.NewFP)
	probeRounds(t, h, verifyConfirmProbes)
	detail := h.MustDetail(list.OrderID)
	require.NotNil(t, detail.Report)
	assert.Equal(t, string(domain.ChangeStatusCompleted), detail.Report.Status, "window met completes the order")
	assert.Equal(t, w.NewFP, detail.Report.Verify.ExpectedNew, "期望指纹=新证书（ProbeDomains 云无关）")
	assert.Equal(t, 0, detail.Report.Verify.ProbeDiff, "线上指纹全部命中新证书")

	// ---- Step 4: 旧证书孤儿清理（终态后事件触发消费） ----
	for _, item := range items {
		h.SeedMapping(w.OldFP, "volcano", item.ResourceRef.AccountKey, item.OldCloudCertID)
	}
	h.ExpireProtectUntil(w.OldFP, time.Now().Add(-time.Hour))
	consumed, err := h.Cleanup.ConsumeOrderQueue(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, 4, consumed, "one old-cert orphan per product consumed")
	for _, item := range items {
		_, err := h.MappingByCloudCert("volcano", item.ResourceRef.AccountKey, item.OldCloudCertID)
		assert.Error(t, err, "old cloud cert mapping deleted for %s", item.OldCloudCertID)
	}
	// 清理动作路由到火山部署器：四产品库删除 API 各一次（alb/nlb 共用 ALB
	// 监听证书库删除 API，记录前缀统一 alb:——按 raw ID 断言）。
	assert.Len(t, stub.Deletes(), 4)
	for _, item := range items {
		_, rawID, _ := strings.Cut(item.OldCloudCertID, ":")
		assert.True(t, stub.DeleteRecordedFor(rawID), "delete routed for %s", item.OldCloudCertID)
	}
	final := h.MustDetail(list.OrderID)
	require.NotNil(t, final.Report)
	require.Len(t, final.Report.OrphanCleanup, 4)
	for _, o := range final.Report.OrphanCleanup {
		assert.Equal(t, service.OrphanActionCleanup, o.Action)
		assert.True(t, o.Success)
	}
}

// Journey: bind failure compensation — the second phase fails on one volcano
// item; the production channel flips the new-cert mapping active->orphan ( the
// 5.9 cleanup queue ) and compensates with CleanupOrphan, which is idempotent
// against the cloud-side deleted state ( double call, same result ). AC-2.
func TestVolcanoCertReplacement_BindFailureCompensation(t *testing.T) {
	stub := NewStubVolcanoCertLibrary()
	h := newVolcanoHarness(t, stub)
	w := seedVolcanoReplacement(t, h)
	seedLiveResources(t, stub, w)

	// Single-item world: one cdn reference whose bind always fails ( explicit
	// startedAt — Windows clock granularity can tie with the four-product
	// snapshot seeded above and flip LatestDone ordering ).
	h.SeedDoneSnapshotWithRefs([]multicloudtest.RefSpec{
		{Cloud: volcanoCloud, Product: domain.ProductCDN, AccountKey: w.Accounts[domain.ProductCDN],
			ResourceID: w.Domain, CloudCertID: w.OldCloudIDs[domain.ProductCDN], Fingerprint: w.OldFP},
	}, time.Now().Add(time.Minute))

	stub.SetBindErr("cdn", assert.AnError)

	list := h.MustGenerate(w.OldFP, w.NewCertID)
	require.Len(t, list.Items, 1)
	h.MustConfirm(list.OrderID, nil) // 单批全量
	h.MustExecute(list.OrderID)

	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	item := items[0]
	assert.Equal(t, domain.ItemStatusFailed, item.Status, "bind failure fails the item (error=%s)", item.Error)
	assert.Contains(t, item.Error, "bind", "item error carries the channel bind failure")

	// 第一段产物已上传且绑定失败：映射 active->orphan（5.9 清理队列入口）。
	// 失败项不落 newCloudCertId（引擎仅成功项持久化上传产物），孤儿映射即
	// 补偿产物锚点。
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	newCloudCertID := orphans[0].CloudCertID
	assert.True(t, strings.HasPrefix(newCloudCertID, "cdn:"), "第一段产物为产品库定向归一形态: %s", newCloudCertID)
	assert.Equal(t, w.NewFP, orphans[0].CertFingerprint)
	mapping, err := h.MappingByCloudCert("volcano", item.ResourceRef.AccountKey, newCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, mapping.Status, "compensation flips the new-cert mapping to orphan")

	// 补偿已尽力清理云侧孤儿（第一段产物被删除）。
	assert.True(t, stub.DeleteRecordedFor(strings.TrimPrefix(newCloudCertID, "cdn:")))

	// AC-2：CleanupOrphan 幂等——同一孤儿双调用同结果（第二次命中云侧
	// not-found，部署器归一为成功）。
	for i := 0; i < 2; i++ {
		creds, cerr := h.Creds.CloudCredential(context.Background(), "volcano", item.ResourceRef.AccountKey)
		require.NoError(t, cerr)
		cerr = h.Channel.CleanupOrphanCert(context.Background(), creds, "volcano", newCloudCertID)
		assert.NoError(t, cerr, "cleanup orphan call %d is idempotent", i+1)
	}
	assert.Len(t, stub.Deletes(), 3, "compensation + 2 idempotent calls all recorded")

	// 映射仍在清理队列（5.9 重放安全），队列消费幂等收口。
	require.Len(t, h.OrphanMappings(), 1)
}

// Journey: rollback — the order sits in the verify window after a successful
// two-phase execution; rollback of all four success items passes the GetCert
// three-judgment precheck ( cdn via the native SHA-256 channel; waf/alb/nlb
// via the mapping reverse-lookup fallback — their libraries expose no
// fingerprint channel ) and rebinds every resource back to the old
// {product}:{id} cert; the replaced new-cert mappings enter the cleanup queue
// and are consumed after the order settles. AC-4.
func TestVolcanoCertReplacement_RollbackRestoresOldCertPerProduct(t *testing.T) {
	stub := NewStubVolcanoCertLibrary()
	h := newVolcanoHarness(t, stub)
	w := seedVolcanoReplacement(t, h)
	seedLiveResources(t, stub, w)
	seedRollbackTargets(t, h, stub, w)

	list := h.MustGenerate(w.OldFP, w.NewCertID)
	h.MustConfirm(list.OrderID, batchedVolcanoConf())
	for batch := 1; batch <= 2; batch++ {
		h.MustExecute(list.OrderID)
		if batch < 2 {
			h.Prober.SetOnlineFingerprint(w.Domain, w.NewFP)
			probeRounds(t, h, verifyConfirmProbes)
			h.MustConfirmBatch(list.OrderID)
		}
	}
	// 订单处于验证中（回滚合法入口），四项全部成功。
	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 4)
	itemIDs := make([]string, 0, 4)
	for _, item := range items {
		require.Equal(t, domain.ItemStatusSuccess, item.Status)
		require.NotEmpty(t, item.OldCloudCertID, "old cloud cert captured for rollback")
		itemIDs = append(itemIDs, item.ID.Hex())
	}

	// ---- 回滚：GetCert 三判定（目标有效）→ 反绑恢复旧 ID ----
	h.MustRollback(list.OrderID, itemIDs...)

	items, err = h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	bindingsByProduct := map[string]string{}
	for _, item := range items {
		assert.Equal(t, domain.ItemStatusRolledBack, item.Status,
			"item rolled back (error=%s)", item.Error)
		bindingsByProduct[string(item.ResourceRef.Product)] = item.OldCloudCertID
		// 反绑恢复旧 ID：四产品最后一次绑定均恢复 {product}:{id} 旧证书。
		binds := stub.BindsByProduct(string(item.ResourceRef.Product))
		require.NotEmpty(t, binds)
		last := binds[len(binds)-1]
		assert.Contains(t, item.OldCloudCertID, last.CloudCertID,
			"%s rebound to the old cloud cert", item.ResourceRef.Product)
		// 被替换下的新云证书映射标 orphan（5.9 队列入口）。
		mapping, merr := h.MappingByCloudCert("volcano", item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, merr)
		assert.Equal(t, domain.MappingStatusOrphan, mapping.Status)
	}
	require.Len(t, bindingsByProduct, 4, "all four products rolled back")

	detail := h.MustDetail(list.OrderID)
	require.Equal(t, string(domain.ChangeStatusRolledBack), detail.Status, "order converges to rolled_back")

	// ---- 终态后消费清理队列：被替换的新云证书孤儿逐一清理（幂等口径） ----
	consumed, err := h.Cleanup.ConsumeOrderQueue(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, 4, consumed, "one new-cert orphan per product consumed")
	for _, item := range items {
		_, err := h.MappingByCloudCert("volcano", item.ResourceRef.AccountKey, item.NewCloudCertID)
		assert.Error(t, err, "new cloud cert mapping deleted for %s", item.NewCloudCertID)
	}
}

// Journey: channel dispatch visibility ( AC-5 ) — after the volcano
// registration, the production CloudAPIChannel routes volcano targets to the
// volcano deployer: Discover enumerates all four products into
// {product}:{id}-shaped references ( fingerprint resolved via the mapping
// reverse-lookup on the same key the scan adapter produces ),
// InspectCloudCert routes GetCert, CleanupOrphanCert routes the per-library
// delete.
func TestVolcanoChannelDispatch_ProductVisibility(t *testing.T) {
	stub := NewStubVolcanoCertLibrary()
	h := newVolcanoHarness(t, stub)

	cdnFP := multicloudtest.FP("dispatch-cdn")
	notAfter := time.Now().Add(365 * 24 * time.Hour)
	stub.SetCDNCert("cert-cdn-1", cdnFP, notAfter.Unix(), "www.example.com")
	stub.SetWAFServiceCert(101, notAfter.Format("2006-01-02 15:04:05"))
	stub.SetWAFDomain("www.example.com", 101, 1)
	stub.SetALBListener("lsn-alb-1", "vol-alb-1", "https", "cert-alb-1")
	stub.SetNLBListener("lsn-nlb-1", "vol-nlb-1", "cert-nlb-1")

	// 映射反查命中键与扫描产出同键（{product}:{id}）。
	h.SeedMapping(cdnFP, "volcano", "acc-volcano", "cdn:cert-cdn-1")

	creds, err := h.Creds.CloudCredential(context.Background(), "volcano", "acc-volcano")
	require.NoError(t, err)
	refs, err := h.Channel.Discover(context.Background(), creds, deployer.DiscoverScope{
		Clouds:     []string{"volcano"},
		Products:   []string{"cdn", "waf", "alb", "nlb"},
		SnapshotID: "snap-dispatch",
	})
	require.NoError(t, err)
	require.Len(t, refs, 4)

	byProduct := map[string]domain.CertReference{}
	for _, ref := range refs {
		assert.Equal(t, "volcano", string(ref.Cloud))
		assert.Equal(t, "snap-dispatch", ref.SnapshotID, "scope.SnapshotID written back")
		byProduct[string(ref.Product)] = ref
	}
	assert.Equal(t, "cdn:cert-cdn-1", byProduct["cdn"].ReferencedCloudCertID)
	assert.Equal(t, "waf:101", byProduct["waf"].ReferencedCloudCertID)
	assert.Equal(t, "alb:cert-alb-1", byProduct["alb"].ReferencedCloudCertID)
	assert.Equal(t, "nlb:cert-nlb-1", byProduct["nlb"].ReferencedCloudCertID)
	assert.Equal(t, "www.example.com", byProduct["cdn"].ResourceID)
	assert.Equal(t, "vol-alb-1/lsn-alb-1", byProduct["alb"].ResourceID, "ALB 监听复合 ID 形态")
	assert.Equal(t, cdnFP, byProduct["cdn"].CertFingerprint, "指纹经映射反查命中（与扫描同键）")

	// GetCert 路由（回滚目标校验数据源）。
	info, err := h.Channel.InspectCloudCert(context.Background(), creds, "volcano", "cdn:cert-cdn-1")
	require.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, cdnFP, info.Fingerprint, "CDN 原生 SHA-256 指纹通道")

	// waf/alb/nlb 无指纹通道：映射反查回退填充，三判定可通过。
	h.SeedMapping(multicloudtest.FP("dispatch-waf"), "volcano", "acc-volcano", "waf:101")
	info, err = h.Channel.InspectCloudCert(context.Background(), creds, "volcano", "waf:101")
	require.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, multicloudtest.FP("dispatch-waf"), info.Fingerprint, "无指纹通道库回退映射反查")

	// 孤儿清理路由（per-library delete）。
	require.NoError(t, h.Channel.CleanupOrphanCert(context.Background(), creds, "volcano", "waf:101"))
	assert.Contains(t, stub.Deletes(), "waf:101")

	// 已删除证书清理幂等（云侧 not-found 归一成功）。
	require.NoError(t, h.Channel.CleanupOrphanCert(context.Background(), creds, "volcano", "waf:101"))

	// 未注册云不可达（分发边界）。
	foreign, err := h.Creds.CloudCredential(context.Background(), "tencent", "acc-t")
	require.NoError(t, err)
	_, err = h.Channel.Discover(context.Background(), foreign, deployer.DiscoverScope{
		Clouds: []string{"tencent"}, Products: []string{"cdn"}, SnapshotID: "snap-x",
	})
	assert.Error(t, err, "未注册部署器的云显式失败（不静默）")
}
