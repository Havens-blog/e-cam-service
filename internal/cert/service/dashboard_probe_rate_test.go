package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestComputeProbeRates 探测新口径（T3）：分母 = 总数 − internal − protocol_skipped
// − wildcard_skipped；拨通率/差异率以该分母计算；分母 ≤0 时取 0（不产生 NaN）。
func TestComputeProbeRates(t *testing.T) {
	testCases := []struct {
		name          string
		summary       DashboardSummary
		wantProbeable int
		wantReachable float64
		wantDiffRate  float64
	}{
		{
			name: "典型：3000 域中 755 内网 + 32 协议 + 61 通配符",
			summary: DashboardSummary{
				ProbeTotalCount:      3000,
				InternalCount:        755,
				ProtocolSkippedCount: 32,
				WildcardSkippedCount: 61,
				ProbeSuccessCount:    1567, // consistent+diff+change_linked_diff+exempt
				DiffAlertCount:       189,
			},
			wantProbeable: 2152,
			wantReachable: 1567.0 / 2152.0,
			wantDiffRate:  189.0 / 2152.0,
		},
		{
			name: "无跳过类：分母等于总数",
			summary: DashboardSummary{
				ProbeTotalCount:   10,
				ProbeSuccessCount: 8,
				DiffAlertCount:    2,
			},
			wantProbeable: 10,
			wantReachable: 0.8,
			wantDiffRate:  0.2,
		},
		{
			name: "全部被跳过：分母 0，两个率均 0（不产生 NaN）",
			summary: DashboardSummary{
				ProbeTotalCount:      5,
				InternalCount:        5,
				ProtocolSkippedCount: 0,
				WildcardSkippedCount: 0,
			},
			wantProbeable: 0,
			wantReachable: 0,
			wantDiffRate:  0,
		},
		{
			name:          "空数据：分母 0",
			summary:       DashboardSummary{},
			wantProbeable: 0,
			wantReachable: 0,
			wantDiffRate:  0,
		},
		{
			name: "跳过数超过总数（脏数据防御）：分母为负按 0 处理",
			summary: DashboardSummary{
				ProbeTotalCount:   3,
				InternalCount:     5,
				ProbeSuccessCount: 1,
			},
			wantProbeable: -2, // 计数仍如实体现（便于观察脏数据）
			wantReachable: 0,
			wantDiffRate:  0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.summary
			computeProbeRates(&s)
			assert.Equal(t, tc.wantProbeable, s.ProbeableCount)
			assert.InDelta(t, tc.wantReachable, s.ReachableRateOnProbeable, 1e-9)
			assert.InDelta(t, tc.wantDiffRate, s.DiffRateOnProbeable, 1e-9)
		})
	}
}

// TestProbeRateImprovesWithNewDenominator 决策验收口径：剔除内网域后拨通率提升
// （现状被约 25 个百分点稀释 → 新口径还原真实水平）。
func TestProbeRateImprovesWithNewDenominator(t *testing.T) {
	// 3000 域：1567 拨通、1212 不可达（其中 755 内网）、61 通配符、32 协议
	old := float64(1567) / float64(3000)
	s := DashboardSummary{
		ProbeTotalCount:      3000,
		InternalCount:        755,
		ProtocolSkippedCount: 32,
		WildcardSkippedCount: 61,
		ProbeSuccessCount:    1567,
	}
	computeProbeRates(&s)
	assert.Greater(t, s.ReachableRateOnProbeable, old,
		"新口径拨通率必须高于旧口径（内网域不再稀释）")
	assert.InDelta(t, float64(1567)/float64(2152), s.ReachableRateOnProbeable, 1e-9)
}
