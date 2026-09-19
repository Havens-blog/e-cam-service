package diagnose

import (
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// item 构造 TopN 条目(测试辅助)。
func item(name string, count int64) logquery.TopNItem {
	return logquery.TopNItem{Name: name, Count: count}
}

// TestLevelFor 风险等级映射边界(临界分:20/40/70)。AC2
func TestLevelFor(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{0, RiskLevelNone},
		{19, RiskLevelNone},
		{20, RiskLevelLow},
		{39, RiskLevelLow},
		{40, RiskLevelMedium},
		{69, RiskLevelMedium},
		{70, RiskLevelHigh},
		{100, RiskLevelHigh},
	}
	for _, c := range cases {
		if got := levelFor(c.score); got != c.want {
			t.Errorf("levelFor(%d) = %s, want %s", c.score, got, c.want)
		}
	}
}

// TestScoreFromBands 分段线性分量评分(含插值)。AC1
func TestScoreFromBands(t *testing.T) {
	cases := []struct {
		v     float64
		bands []ScoreBand
		want  float64
	}{
		{0, DefaultConfig.TopIPRateBands, 0},
		{0.25, DefaultConfig.TopIPRateBands, 5},  // {0,0}-{0.5,10} 线性放大: 0.25/0.5*10
		{150, DefaultConfig.TopIPRateBands, 100}, // 超过最高档封顶
		{2, DefaultConfig.SurgeBands, 30},        // 恰在档位下界
		{3, DefaultConfig.SurgeBands, 40},        // {2,30}-{5,60} 插值: 30+(1/3)*30
		{5, DefaultConfig.SurgeBands, 60},
		{0.3, DefaultConfig.AnomalyBands, 20},
		{0.4, DefaultConfig.AnomalyBands, 32.5}, // {0.3,20}-{0.5,45}: 20+(0.1/0.2)*25
	}
	for _, c := range cases {
		got := scoreFromBands(c.v, c.bands)
		if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("scoreFromBands(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

// TestEvaluateScoreDeterministic 风险分确定性:单 IP 速率 + 突增(Top IP 突增
// 与总量突增取大) + UA 异常 + 攻击占比加权。AC1
// 输入:窗口 60s,Total=12000,单 IP 全量(200 次/秒),UA 全为 curl,
// 前窗 Total=2000/TopIP=1000 → 速率分量 100(40 分),突增 max(6,12)=12 →
// 86.5(21.625 分),UA 异常 100(15 分),攻击占比 0(0 分) → 76.625 → 77。
func TestEvaluateScoreDeterministic(t *testing.T) {
	res := Evaluate(&DiagnoseInput{
		WindowSec:   60,
		Total:       12000,
		TopIPs:      []logquery.TopNItem{item("1.2.3.4", 12000)},
		TopUAs:      []logquery.TopNItem{item("curl/8.0", 12000)},
		StatusCodes: []logquery.TopNItem{item("200", 12000)},
		Prev:        &PrevWindow{Total: 2000, TopIPCount: 1000},
	})
	if res.RiskScore != 77 {
		t.Errorf("RiskScore = %d, want 77", res.RiskScore)
	}
	if res.RiskLevel != RiskLevelHigh {
		t.Errorf("RiskLevel = %s, want %s", res.RiskLevel, RiskLevelHigh)
	}
	if res.AttackType != AttackTypeCCFlood {
		t.Errorf("AttackType = %s, want %s", res.AttackType, AttackTypeCCFlood)
	}
	if res.Degraded {
		t.Errorf("Degraded = true, want false")
	}
}

// TestEvaluateAttackTypes 攻击类型判定(≥3 类)。AC3
func TestEvaluateAttackTypes(t *testing.T) {
	cases := []struct {
		name  string
		input *DiagnoseInput
		want  string
	}{
		{
			// CC 突增路径:单 IP 30 次/秒(≥HighIPRate) + 突增 4.5 倍(≥3) + 单 IP 集中
			name: "cc_by_surge",
			input: &DiagnoseInput{
				WindowSec:   60,
				Total:       1800,
				TopIPs:      []logquery.TopNItem{item("9.9.9.9", 1800)},
				TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 (Linux; Android 10) Chrome/120.0", 1800)},
				StatusCodes: []logquery.TopNItem{item("200", 1800)},
				Prev:        &PrevWindow{Total: 400, TopIPCount: 400},
			},
			want: AttackTypeCCFlood,
		},
		{
			// CC 绝对量降级路径:单 IP 300 次/秒(≥CriticalIPRate),前窗缺失
			name: "cc_absolute_degraded",
			input: &DiagnoseInput{
				WindowSec:   60,
				Total:       18000,
				TopIPs:      []logquery.TopNItem{item("10.0.0.1", 18000)},
				TopUAs:      []logquery.TopNItem{item("curl/8.0", 18000)},
				StatusCodes: []logquery.TopNItem{item("200", 18000)},
			},
			want: AttackTypeCCFlood,
		},
		{
			// 接口爆破:单 IP 高频(100 次/秒) + 4xx 占比 0.9 + URI 集中(0.95)
			name: "brute_force",
			input: &DiagnoseInput{
				WindowSec:   60,
				Total:       6000,
				TopIPs:      []logquery.TopNItem{item("2.2.2.2", 6000)},
				TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 (Windows NT 10.0; Win64; x64)", 6000)},
				StatusCodes: []logquery.TopNItem{item("404", 5400), item("200", 600)},
				Actions:     []logquery.TopNItem{item("block", 5400)},
				TopURIs:     []logquery.TopNItem{item("/admin/login", 5700), item("/health", 300)},
				Prev:        &PrevWindow{Total: 3000, TopIPCount: 3000},
			},
			want: AttackTypeBruteForce,
		},
		{
			// 接口爆破:URI 维度未聚合时退化为 单 IP 高频 + 4xx 高
			name: "brute_force_without_uri",
			input: &DiagnoseInput{
				WindowSec:   60,
				Total:       6000,
				TopIPs:      []logquery.TopNItem{item("2.2.2.2", 6000)},
				TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 (Windows NT 10.0; Win64; x64)", 6000)},
				StatusCodes: []logquery.TopNItem{item("404", 5400), item("200", 600)},
				Prev:        &PrevWindow{Total: 3000, TopIPCount: 3000},
			},
			want: AttackTypeBruteForce,
		},
		{
			// 恶意爬虫:可疑 UA 占比 0.8(≥0.3) + 4xx 占比 0.45(≥0.4,<0.5 不判爆破)
			name: "crawler",
			input: &DiagnoseInput{
				WindowSec: 60,
				Total:     5000,
				TopIPs:    []logquery.TopNItem{item("1.1.1.1", 2400)},
				TopUAs: []logquery.TopNItem{
					item("python-requests/2.31.0", 4000),
					item("Mozilla/5.0 (X11; Linux) Firefox/119.0", 1000),
				},
				StatusCodes: []logquery.TopNItem{item("404", 2250), item("200", 2750)},
				Prev:        &PrevWindow{Total: 6000, TopIPCount: 6000},
			},
			want: AttackTypeCrawler,
		},
		{
			// 正常流量:低速率、无突增、UA 多样
			name: "normal_low_traffic",
			input: &DiagnoseInput{
				WindowSec: 60,
				Total:     600,
				TopIPs:    []logquery.TopNItem{item("7.7.7.7", 120)},
				TopUAs: []logquery.TopNItem{
					item("Mozilla/5.0 (Windows NT 10.0) Chrome/120.0", 240),
					item("Mozilla/5.0 (Macintosh) Safari/17.0", 200),
					item("Mozilla/5.0 Firefox/119.0", 160),
				},
				StatusCodes: []logquery.TopNItem{item("200", 600)},
				Prev:        &PrevWindow{Total: 600, TopIPCount: 120},
			},
			want: AttackTypeNormal,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Evaluate(c.input)
			if res.AttackType != c.want {
				t.Errorf("AttackType = %s, want %s", res.AttackType, c.want)
			}
		})
	}
}

// TestEvaluatePromoHighTraffic 大促高流量不误报:突增 4 倍但 UA 多样(5 种)、
// 来源分散(Top IP 占比 0.05)、无攻击动作 → 判正常高流量且等级封顶"低"。AC3
// 分量:速率 50 次/秒 → 84.375(33.75 分),突增 4 → 50(12.5 分),
// UA 异常 0.3 → 20(3 分),攻击占比 0 → 0,合计 49.25 → 49(不封顶为"中")。
func TestEvaluatePromoHighTraffic(t *testing.T) {
	res := Evaluate(&DiagnoseInput{
		WindowSec: 60,
		Total:     60000,
		TopIPs:    []logquery.TopNItem{item("203.0.113.9", 3000)},
		TopUAs: []logquery.TopNItem{
			item("Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile Safari/604.1", 18000),
			item("Mozilla/5.0 (Linux; Android 13; Pixel 7) Chrome/120.0", 15000),
			item("Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0", 12000),
			item("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari/17.0", 9000),
			item("Mozilla/5.0 (Windows NT 10.0; Win64; x64) Firefox/119.0", 6000),
		},
		StatusCodes: []logquery.TopNItem{item("200", 58000), item("499", 2000)},
		Prev:        &PrevWindow{Total: 15000, TopIPCount: 2000},
	})
	if res.AttackType != AttackTypeNormalBurst {
		t.Errorf("AttackType = %s, want %s", res.AttackType, AttackTypeNormalBurst)
	}
	if res.RiskScore != 49 {
		t.Errorf("RiskScore = %d, want 49", res.RiskScore)
	}
	if res.RiskLevel != RiskLevelLow {
		t.Errorf("RiskLevel = %s, want %s(大促不误报,封顶低风险)", res.RiskLevel, RiskLevelLow)
	}
}

// TestEvaluateDegraded 降级标注:前窗无数据 / 窗口时长缺失。AC5(降级路径)
func TestEvaluateDegraded(t *testing.T) {
	// 前窗缺失:突增判据缺失,按绝对量判定
	res := Evaluate(&DiagnoseInput{
		WindowSec: 60,
		Total:     600,
		TopIPs:    []logquery.TopNItem{item("7.7.7.7", 120)},
		TopUAs: []logquery.TopNItem{
			item("Mozilla/5.0 (Windows NT 10.0) Chrome/120.0", 300),
			item("Mozilla/5.0 Firefox/119.0", 300),
		},
		StatusCodes: []logquery.TopNItem{item("200", 600)},
	})
	if !res.Degraded {
		t.Errorf("Degraded = false, want true(前窗无数据)")
	}
	if !strings.Contains(res.DegradedReason, "前窗") {
		t.Errorf("DegradedReason = %q, want 包含 前窗", res.DegradedReason)
	}

	// 窗口时长缺失:单 IP 频率分量失效(0 分),突增/UA 占比分量仍有效
	// (突增 2.0 倍 → 30 分权重 25% = 7.5;UA 占比 1.0 → 100 分权重 15% = 15)→ 22.5 → 23
	res = Evaluate(&DiagnoseInput{
		WindowSec:   0,
		Total:       1000,
		TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0", 1000)},
		StatusCodes: []logquery.TopNItem{item("200", 1000)},
		Prev:        &PrevWindow{Total: 500, TopIPCount: 500},
	})
	if !res.Degraded {
		t.Errorf("Degraded = false, want true(窗口时长缺失)")
	}
	if !strings.Contains(res.DegradedReason, "窗口") {
		t.Errorf("DegradedReason = %q, want 包含 窗口", res.DegradedReason)
	}
	if res.RiskScore != 23 {
		t.Errorf("RiskScore = %d, want 23(单 IP 频率分量失效,突增 + UA 占比计分)", res.RiskScore)
	}
	if res.RiskLevel != RiskLevelLow {
		t.Errorf("RiskLevel = %s, want %s(23 分落入低风险档)", res.RiskLevel, RiskLevelLow)
	}
}

// TestEvaluateMeasures 措施映射:按判定输出 ≥3 条带具体值的文案。AC4
func TestEvaluateMeasures(t *testing.T) {
	t.Run("cc_flood", func(t *testing.T) {
		res := Evaluate(&DiagnoseInput{
			WindowSec:   60,
			Total:       12000,
			TopIPs:      []logquery.TopNItem{item("1.2.3.4", 12000)},
			TopUAs:      []logquery.TopNItem{item("curl/8.0", 12000)},
			StatusCodes: []logquery.TopNItem{item("200", 12000)},
			Prev:        &PrevWindow{Total: 2000, TopIPCount: 1000},
		})
		if len(res.Measures) < 3 {
			t.Fatalf("Measures = %d 条, want ≥3", len(res.Measures))
		}
		assertMeasureContains(t, res.Measures, "封禁")
		assertMeasureContains(t, res.Measures, "1.2.3.4")
		assertMeasureContains(t, res.Measures, "12000 次")
		assertMeasureContains(t, res.Measures, "CC 防护")
		assertMeasureContains(t, res.Measures, "次/分钟")
		assertMeasureContains(t, res.Measures, "curl/8.0")
	})
	t.Run("crawler_ua_blacklist", func(t *testing.T) {
		res := Evaluate(&DiagnoseInput{
			WindowSec: 60,
			Total:     5000,
			TopIPs:    []logquery.TopNItem{item("1.1.1.1", 2400)},
			TopUAs: []logquery.TopNItem{
				item("python-requests/2.31.0", 4000),
				item("Mozilla/5.0 (X11; Linux) Firefox/119.0", 1000),
			},
			StatusCodes: []logquery.TopNItem{item("404", 2250), item("200", 2750)},
			Prev:        &PrevWindow{Total: 6000, TopIPCount: 6000},
		})
		if len(res.Measures) < 3 {
			t.Fatalf("Measures = %d 条, want ≥3", len(res.Measures))
		}
		assertMeasureContains(t, res.Measures, "python-requests/2.31.0")
		assertMeasureContains(t, res.Measures, "黑名单")
	})
	t.Run("brute_force_uri", func(t *testing.T) {
		res := Evaluate(&DiagnoseInput{
			WindowSec:   60,
			Total:       6000,
			TopIPs:      []logquery.TopNItem{item("2.2.2.2", 6000)},
			TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 (Windows NT 10.0; Win64; x64)", 6000)},
			StatusCodes: []logquery.TopNItem{item("404", 5400), item("200", 600)},
			Actions:     []logquery.TopNItem{item("block", 5400)},
			TopURIs:     []logquery.TopNItem{item("/admin/login", 5700), item("/health", 300)},
			Prev:        &PrevWindow{Total: 3000, TopIPCount: 3000},
		})
		if len(res.Measures) < 3 {
			t.Fatalf("Measures = %d 条, want ≥3", len(res.Measures))
		}
		assertMeasureContains(t, res.Measures, "/admin/login")
	})
	t.Run("normal_burst_observe", func(t *testing.T) {
		res := Evaluate(&DiagnoseInput{
			WindowSec: 60,
			Total:     60000,
			TopIPs:    []logquery.TopNItem{item("203.0.113.9", 3000)},
			TopUAs: []logquery.TopNItem{
				item("Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) Mobile Safari/604.1", 18000),
				item("Mozilla/5.0 (Linux; Android 13) Chrome/120.0", 15000),
				item("Mozilla/5.0 (Windows NT 10.0) Chrome/120.0", 12000),
				item("Mozilla/5.0 (Macintosh) Safari/17.0", 9000),
				item("Mozilla/5.0 Firefox/119.0", 6000),
			},
			StatusCodes: []logquery.TopNItem{item("200", 58000), item("499", 2000)},
			Prev:        &PrevWindow{Total: 15000, TopIPCount: 2000},
		})
		if len(res.Measures) < 3 {
			t.Fatalf("Measures = %d 条, want ≥3", len(res.Measures))
		}
		assertMeasureContains(t, res.Measures, "突增")
	})
	t.Run("normal_traffic", func(t *testing.T) {
		res := Evaluate(&DiagnoseInput{
			WindowSec:   60,
			Total:       600,
			TopIPs:      []logquery.TopNItem{item("7.7.7.7", 120)},
			TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 (Windows NT 10.0) Chrome/120.0", 600)},
			StatusCodes: []logquery.TopNItem{item("200", 600)},
			Prev:        &PrevWindow{Total: 600, TopIPCount: 120},
		})
		if len(res.Measures) < 3 {
			t.Fatalf("Measures = %d 条, want ≥3", len(res.Measures))
		}
	})
	t.Run("empty_window_no_panic", func(t *testing.T) {
		res := Evaluate(&DiagnoseInput{WindowSec: 60})
		if len(res.Measures) < 3 {
			t.Fatalf("Measures = %d 条, want ≥3(空窗口也给出可执行建议)", len(res.Measures))
		}
	})
}

// assertMeasureContains 断言措施文案中存在包含 substr 的条目。
func assertMeasureContains(t *testing.T, measures []string, substr string) {
	t.Helper()
	for _, m := range measures {
		if strings.Contains(m, substr) {
			return
		}
	}
	t.Errorf("措施文案中未找到包含 %q 的条目, got %v", substr, measures)
}

// TestEvaluateTopSources Top 攻击源(前 5,含占比)。AC5(输出结构)
func TestEvaluateTopSources(t *testing.T) {
	res := Evaluate(&DiagnoseInput{
		WindowSec: 60,
		Total:     100,
		TopIPs: []logquery.TopNItem{
			item("10.0.0.1", 60), item("10.0.0.2", 20), item("10.0.0.3", 10),
			item("10.0.0.4", 5), item("10.0.0.5", 3), item("10.0.0.6", 2),
		},
		TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 (Windows NT 10.0) Chrome/120.0", 100)},
		StatusCodes: []logquery.TopNItem{item("200", 100)},
		Prev:        &PrevWindow{Total: 100, TopIPCount: 60},
	})
	if len(res.TopSources) != 5 {
		t.Fatalf("TopSources = %d 条, want 5", len(res.TopSources))
	}
	if res.TopSources[0].IP != "10.0.0.1" || res.TopSources[0].Count != 60 {
		t.Errorf("TopSources[0] = %+v, want 10.0.0.1/60", res.TopSources[0])
	}
	if res.TopSources[0].Share < 0.599 || res.TopSources[0].Share > 0.601 {
		t.Errorf("TopSources[0].Share = %v, want 0.6", res.TopSources[0].Share)
	}
	if res.TopSources[4].IP != "10.0.0.5" || res.TopSources[4].Count != 3 {
		t.Errorf("TopSources[4] = %+v, want 10.0.0.5/3(第 6 名被截断)", res.TopSources[4])
	}
}

// TestEvaluateNilInput 空输入不 panic(纯函数防御)。AC5
func TestEvaluateNilInput(t *testing.T) {
	res := Evaluate(nil)
	if res == nil {
		t.Fatal("Evaluate(nil) = nil, want 非空结果")
	}
	if res.RiskLevel != RiskLevelNone || res.AttackType != AttackTypeNormal {
		t.Errorf("nil 输入判定 = %s/%s, want %s/%s",
			res.RiskLevel, res.AttackType, RiskLevelNone, AttackTypeNormal)
	}
}

// TestEvaluateInputNotMutated 纯函数不改写输入切片(不可变约定)。AC5
func TestEvaluateInputNotMutated(t *testing.T) {
	input := &DiagnoseInput{
		WindowSec:   60,
		Total:       100,
		TopIPs:      []logquery.TopNItem{item("10.0.0.2", 20), item("10.0.0.1", 60)},
		TopUAs:      []logquery.TopNItem{item("Mozilla/5.0 Chrome/120.0", 100)},
		StatusCodes: []logquery.TopNItem{item("200", 100)},
		Prev:        &PrevWindow{Total: 100, TopIPCount: 60},
	}
	before := make([]logquery.TopNItem, len(input.TopIPs))
	copy(before, input.TopIPs)
	_ = Evaluate(input)
	for i := range before {
		if input.TopIPs[i] != before[i] {
			t.Errorf("输入 TopIPs 被改写: index %d, before %v, after %v", i, before[i], input.TopIPs[i])
		}
	}
}

// TestEvaluateSurgeMultiplier 突增倍数随结论透出(任务 2 响应契约:
// 前窗可用时 >0 且取 Total / Top IP 倍数大者;前窗缺失恒 0)。
func TestEvaluateSurgeMultiplier(t *testing.T) {
	noPrev := Evaluate(&DiagnoseInput{WindowSec: 60, Total: 100})
	if noPrev.SurgeMultiplier != 0 {
		t.Errorf("no prev: surge = %v, want 0", noPrev.SurgeMultiplier)
	}
	if !noPrev.Degraded {
		t.Error("no prev should be degraded")
	}
	// Total 倍数 500/50=10,Top IP 倍数 300/20=15 → 取大 15。
	withPrev := Evaluate(&DiagnoseInput{
		WindowSec: 60, Total: 500,
		TopIPs: []logquery.TopNItem{item("10.0.0.1", 300)},
		Prev:   &PrevWindow{Total: 50, TopIPCount: 20},
	})
	if withPrev.SurgeMultiplier != 15 {
		t.Errorf("surge = %v, want 15(取大)", withPrev.SurgeMultiplier)
	}
	if withPrev.Degraded {
		t.Error("prev available should not degrade")
	}
}
