package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
)

// fakeSummaryRefresher 记录 RebuildFromSource 的调用窗口。
// err 表示恒定失败；failFirst 表示「前 N 次失败、之后成功」，用于验证重试。
type fakeSummaryRefresher struct {
	calls     []string
	err       error
	failFirst int
}

func (f *fakeSummaryRefresher) RebuildFromSource(_ context.Context, startDate, endDate string) error {
	f.calls = append(f.calls, startDate+"~"+endDate)
	if f.failFirst > 0 {
		f.failFirst--
		return errors.New("transient mongo failure")
	}
	return f.err
}

// TestSummaryRebuildRange 汇总表重建窗口（表驱动）。
// 账期为月初，故窗口起点必须回退到月初，否则起始月的账期行会漏在窗口外。
func TestSummaryRebuildRange(t *testing.T) {
	tests := []struct {
		name               string
		start, end         time.Time
		wantStart, wantEnd string
	}{
		{
			name:      "月中起算 - 起点回退到月初",
			start:     time.Date(2026, 9, 12, 13, 45, 0, 0, time.UTC),
			end:       time.Date(2026, 10, 12, 4, 12, 0, 0, time.UTC),
			wantStart: "2026-09-01",
			wantEnd:   "2026-10-12",
		},
		{
			name:      "已是月初 - 保持不变",
			start:     time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
			wantStart: "2026-02-01",
			wantEnd:   "2026-10-12",
		},
		{
			name:      "跨年 - 起点对齐到 12 月月初",
			start:     time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC),
			end:       time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			wantStart: "2025-12-01",
			wantEnd:   "2026-01-15",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end := summaryRebuildRange(tc.start, tc.end)
			assert.Equal(t, tc.wantStart, start)
			assert.Equal(t, tc.wantEnd, end)
		})
	}
}

// TestRefreshSummary 采集成功后刷新汇总表：
// 已注入则按月初对齐窗口调用一次；未注入则静默跳过（不 panic）。
func TestRefreshSummary(t *testing.T) {
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC)

	t.Run("已注入 - 调用一次且窗口月初对齐", func(t *testing.T) {
		f := &fakeSummaryRefresher{}
		svc := &CollectorService{logger: elog.DefaultLogger}
		svc.SetSummaryRefresher(f)

		svc.refreshSummary(context.Background(), start, end)

		assert.Equal(t, []string{"2026-09-01~2026-10-12"}, f.calls)
	})

	t.Run("未注入 - 静默跳过不 panic", func(t *testing.T) {
		svc := &CollectorService{logger: elog.DefaultLogger}
		assert.NotPanics(t, func() {
			svc.refreshSummary(context.Background(), start, end)
		})
	})
}

// TestRefreshSummary_ErrorIsNonFatal 汇总重建恒失败属 best-effort：
// 重试耗尽后只告警、不 panic、不向上返回错误（账单明细已落库，查询可降级明细表）。
func TestRefreshSummary_ErrorIsNonFatal(t *testing.T) {
	f := &fakeSummaryRefresher{err: errors.New("mongo unavailable")}
	svc := &CollectorService{logger: elog.DefaultLogger}
	svc.SetSummaryRefresher(f)

	assert.NotPanics(t, func() {
		svc.refreshSummary(context.Background(), time.Now(), time.Now())
	})
	assert.Len(t, f.calls, summaryRefreshAttempts, "恒失败应重试到上限")
}

// TestRefreshSummary_RetriesTransientFailure 瞬时失败必须被重试救回：
// 汇总表一旦陈旧就要等下次启动对账才自愈，不能让一次 mongo 抖动把它留在旧口径。
func TestRefreshSummary_RetriesTransientFailure(t *testing.T) {
	tests := []struct {
		name      string
		failFirst int
		wantCalls int
	}{
		{name: "首次即成功 - 不重试", failFirst: 0, wantCalls: 1},
		{name: "失败 1 次后成功 - 重试 1 次", failFirst: 1, wantCalls: 2},
		{name: "失败 2 次后成功 - 重试 2 次", failFirst: 2, wantCalls: 3},
	}

	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSummaryRefresher{failFirst: tc.failFirst}
			// 直接构造：summaryRefreshRetryDelay 为 0，测试不为退避睡真实秒数
			svc := &CollectorService{logger: elog.DefaultLogger}
			svc.SetSummaryRefresher(f)

			svc.refreshSummary(context.Background(), start, end)

			assert.Len(t, f.calls, tc.wantCalls)
			for _, c := range f.calls {
				assert.Equal(t, "2026-09-01~2026-10-12", c, "每次重试的窗口都应月初对齐")
			}
		})
	}
}

// TestRefreshSummary_ContextCanceled 上下文取消后立即停止重试，不空转剩余次数。
func TestRefreshSummary_ContextCanceled(t *testing.T) {
	f := &fakeSummaryRefresher{err: errors.New("mongo unavailable")}
	svc := &CollectorService{
		logger:                   elog.DefaultLogger,
		summaryRefreshRetryDelay: time.Millisecond,
	}
	svc.SetSummaryRefresher(f)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 首次失败后，退避阶段的 ctx.Done() 立即命中

	assert.NotPanics(t, func() {
		svc.refreshSummary(ctx, time.Now(), time.Now())
	})
	assert.Len(t, f.calls, 1, "取消后不应再发起重试")
}
