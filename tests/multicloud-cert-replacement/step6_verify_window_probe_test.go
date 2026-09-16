// @feature cert-multicloud-deployers @api-functional
//
// Contract: multicloud-cert-replacement / Step 6 — 验证窗口拨测确认线上指纹.
// Outcomes under test: success, probe-mismatch-unmet.
//
// The TLS dial boundary is stubbed at the ProbeService seam: the verify-window
// service consumes the stub prober through the production interface, so the
// streak judgment ( 连续 VerifyConfirmProbes 轮一致 ), window closing and
// expiry finalization run real code.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedVerifyingWindow seeds a verifying order with the verify expectation
// already sealed ( domains = old cert SAN set, expected = new fingerprint )
// and the order-level verify window set ( production EnterVerify writes both
// together; the window jobs scan orders by VerifyWindowUntil ).
func seedVerifyingWindow(t *testing.T, h *multicloudtest.Harness, w replacementWorld, windowUntil time.Time) string {
	t.Helper()
	expected := &domain.VerifyExpected{
		NewCertFingerprint: w.NewFP,
		Domains:            []string{w.Domain},
		WindowUntil:        windowUntil,
	}
	return h.SeedOrder(domain.ChangeStatusVerifying, nil, expected, w.OldFP, w.NewCertID, windowUntil)
}

// Contract outcome "success": 拨测线上指纹与期望新证书指纹连续 2 轮一致 →
// 窗口达成，变更单收敛 completed；拨测结果逐轮落库且分类记为一致。
func TestVerifyWindow_ConsecutiveProbesConfirmCompletion(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	// 未分批（终批）验证中单：窗口未过期。
	orderID := seedVerifyingWindow(t, h, w, time.Now().Add(24*time.Hour))

	// 拨测线上指纹 = 期望新证书指纹（连续 2 轮）。
	h.Prober.SetOnlineFingerprint(w.Domain, w.NewFP)
	probeRounds(t, h, verifyConfirmProbes)

	// Output: 窗口达成 → 变更单收敛 completed。
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCompleted, order.Status, "window met finalizes the order")

	// State: 拨测结果逐轮落库（>= 2 条），指纹与期望一致。
	results, err := h.Probes.LatestPerDomain(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, w.NewFP, results[0].OnlineFingerprint)
	assert.Equal(t, domain.ProbeStatusConsistent, results[0].Status,
		"new cert is in the ledger, so the observation classifies consistent")

	// Report: 终态单 GET /:id 附带 ChangeReport，verify 汇总可见达标计数。
	detail := h.MustDetail(orderID)
	require.NotNil(t, detail.Report, "terminal order carries the report")
	assert.Equal(t, string(domain.ChangeStatusCompleted), detail.Report.Status)
	assert.Equal(t, w.NewFP, detail.Report.Verify.ExpectedNew)
	assert.Equal(t, 1, detail.Report.Verify.ProbePass, "one target domain met the expected fingerprint")
	assert.Zero(t, detail.Report.Verify.Unmet)
	assert.Empty(t, detail.Report.UnmetDomains)
}

// Contract outcome "probe-mismatch-unmet": 线上指纹不等于期望（边缘未同步）→
// 不标记验证通过，窗口保持 verifying；到期仍未达成 → partial_completed +
// 持久化未达成域清单，不因单次拨测失败误判成功。
func TestVerifyWindow_ProbeMismatchNotPassedAndExpiryFinalization(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	// Two verifying orders over the same domain but distinct old
	// fingerprints ( the active-mutex token is the old fingerprint, so two
	// active-status orders cannot share one ): one with an active window
	// ( probe rounds keep running inside it ), one already expired ( the
	// window-expiry finalizer judges it from the stored probe results ).
	activeID := seedVerifyingWindow(t, h, w, time.Now().Add(24*time.Hour))
	secondOldFP := multicloudtest.FP("second-old-cert")
	h.SeedFingerprintOnlyCert(secondOldFP)
	expiredID := h.SeedOrder(domain.ChangeStatusVerifying, nil, &domain.VerifyExpected{
		NewCertFingerprint: w.NewFP,
		Domains:            []string{w.Domain},
		WindowUntil:        time.Now().Add(-time.Hour),
	}, secondOldFP, w.NewCertID, time.Now().Add(-time.Hour))

	staleEdgeFP := multicloudtest.FP("edge-not-synced")
	h.Prober.SetOnlineFingerprint(w.Domain, staleEdgeFP)
	probeRounds(t, h, verifyConfirmProbes+1)

	// Output: 不标记验证通过——活动窗口订单保持 verifying。
	order, err := h.Orders.GetByID(context.Background(), activeID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status,
		"a mismatch never marks the window as met")

	// State: 拨测结果落库且与期望不一致（diff 分类，非变更关联一致）。
	results, err := h.Probes.LatestPerDomain(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, staleEdgeFP, results[0].OnlineFingerprint)
	assert.NotEqual(t, w.NewFP, results[0].OnlineFingerprint)

	// 窗口到期终局判定（生产入口 cert:window-expiry 调度任务）：终批未达标 →
	// partial_completed + 未达成域清单持久化。
	expired, err := h.Verify.FinalizeExpiredWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, expired, 1)
	assert.Equal(t, expiredID, expired[0])

	order, err = h.Orders.GetByID(context.Background(), expiredID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPartialCompleted, order.Status, "expiry finalizes partial")

	unmet, err := h.Unmet.ListUnmetDomains(context.Background(), expiredID)
	require.NoError(t, err)
	require.Len(t, unmet, 1)
	assert.Equal(t, w.Domain, unmet[0], "unmet domain list persisted")

	// Report: partial_completed 单附带未达标域清单。
	detail := h.MustDetail(expiredID)
	require.NotNil(t, detail.Report)
	assert.Equal(t, string(domain.ChangeStatusPartialCompleted), detail.Report.Status)
	assert.Contains(t, detail.Report.UnmetDomains, w.Domain)
	assert.Equal(t, 1, detail.Report.Verify.Unmet)
}
