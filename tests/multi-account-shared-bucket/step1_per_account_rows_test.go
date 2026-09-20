// @feature oss-ops-insight @api-functional
//
// Contract step-1-per-account-rows: 三账号同名 bucket 同日各留一行 — per
// account isolation under the (account_id, bucket_name, date) unique key,
// concurrent same-key writes collapse to one surviving row, and a single
// account failure never contaminates the others.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_bucket

import (
	"errors"
	"sync"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome three-accounts-isolated-rows: 同日 shared-assets 在 A/B/C 下各有一行,
// 互不覆盖、互不合并,容量/对象数各自反映各账号视角的真实值。
func TestStep1_ThreeAccountsIsolatedRows(t *testing.T) {
	h := osstest.NewHarness()
	p := h.NewPerAccountProvider()
	for _, id := range []int64{1, 2, 3} {
		h.SeedAccountNamed(id, p.Name)
		h.SeedBucket(id, p.Name, sharedBucketName)
		// 各账号视角容量互不相同(厂商按账号返回各自口径)
		p.QuerierFor(id).SetMetrics(osstest.Metric(sharedBucketName, osstest.Today(), float64(100*id), int64(10*id)))
	}

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	requireSharedRows(t, h, osstest.Today(), map[int64]float64{1: 100, 2: 200, 3: 300})
}

// Outcome concurrent-write-same-key: 两个采集 goroutine 并发写同一唯一键 —
// 唯一键约束保证至多一行生效,不产生重复行或脏数据,写入不报唯一键冲突错误。
func TestStep1_ConcurrentWriteSameKey(t *testing.T) {
	h, _ := newJourneyHarness()

	mk := func(storage float64) types.OSSMetric {
		m := osstest.Metric(sharedBucketName, osstest.Today(), storage, 10)
		m.AccountID = 1
		m.Provider = "concurrent-test"
		return m
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	// 首写生效路径与覆盖更新路径并发各 4 次
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func(v float64) {
			defer wg.Done()
			errCh <- h.MetricDAO.BulkInsertIfAbsent(testCtx(), []types.OSSMetric{mk(v)})
		}(float64(100 + i))
		go func(v float64) {
			defer wg.Done()
			errCh <- h.MetricDAO.BulkUpsertMetrics(testCtx(), []types.OSSMetric{mk(v)})
		}(float64(200 + i))
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "并发写不得报唯一键冲突错误")
	}

	require.Equal(t, 1, h.MetricDAO.Count(), "同键并发写后恰有一行(唯一键约束)")
	row, _ := h.MetricDAO.Row(1, sharedBucketName, osstest.Today())
	require.GreaterOrEqual(t, row.StorageSize, float64(100), "存活行必须是合法写入值之一")
}

// Outcome single-account-failure-isolated: 三账号中某一账号适配器调用失败,
// 仅失败账号计入 failures 明细,正常两账号的 shared-assets 行照常落库。
func TestStep1_SingleAccountFailureIsolated(t *testing.T) {
	h := osstest.NewHarness()
	p := h.NewPerAccountProvider()
	for _, id := range []int64{1, 2, 3} {
		h.SeedAccountNamed(id, p.Name)
		h.SeedBucket(id, p.Name, sharedBucketName)
	}
	p.QuerierFor(2).Fail(errors.New("injected: account 2 credentials expired"))
	p.QuerierFor(1).SetMetrics(osstest.Metric(sharedBucketName, osstest.Today(), 100, 10))
	p.QuerierFor(3).SetMetrics(osstest.Metric(sharedBucketName, osstest.Today(), 300, 30))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单账号失败不得中断整轮采集")

	// 失败明细定位到账号 2;正常两账号照常落库
	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1)
	require.Equal(t, int64(2), failures[0].AccountID)
	require.Contains(t, failures[0].LastError, "credentials expired")

	requireSharedRows(t, h, osstest.Today(), map[int64]float64{1: 100, 3: 300})
	require.Equal(t, 2, h.MetricDAO.Count(), "失败账号无行,正常两账号各一行")
}
