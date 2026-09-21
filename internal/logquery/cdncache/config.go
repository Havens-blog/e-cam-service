package cdncache

import (
	"math"
	"slices"
)

// LevelThresholds 健康档位边界(可缓存请求命中率口径,临界含下界):
// rate ≥ GoodMin → 优;≥ MediumMin → 中;否则 → 差。
type LevelThresholds struct {
	GoodMin   float64 `json:"good_min"`   // 优档下界(默认 0.90,参数化可校准)
	MediumMin float64 `json:"medium_min"` // 中档下界(默认 0.80,参数化可校准)
}

// Config 缓存分析规则配置(阈值集中可校准;镜像 diagnose.Config 的组织方式)。
type Config struct {
	// ---- 健康档位阈值(可缓存请求命中率口径)----
	DefaultLevels LevelThresholds            // 全局默认档位边界
	DomainLevels  map[string]LevelThresholds // 域名级覆盖(动态占比高域名按历史命中率分位数经 CalibrateThresholds 初始化;取不到回退默认)

	// ---- 请求-字节 gap 判定(双向归因)----
	ByteGapLarge float64 // 请求命中率 − 字节命中率 ≥ 此值 且请求命中尚可 → 大文件未命中(回源带宽浪费)

	// ---- 未命中 URI 集中度(查询串归一后)----
	URIMissConcentration float64 // 归一路径 miss 占全部未命中 ≥ 此值 → 判集中并生成优化项
	URIMissTopLimit      int     // 未命中 URI TOP N

	// ---- 状态码 ----
	ErrorStatusRatio float64 // (4xx+5xx)/总响应 ≥ 此值 → 强制回源校验优化项

	// ---- 动态接口启发式(低可信度提示;小写包含匹配)----
	DynamicURIHints []string

	// ---- 输出上限 ----
	DomainTopLimit     int // 域名命中率排行 Top N
	TopRecommendations int // 优化项 Top N(非优档按未命中流量占比降序;优档 0 条)

	// ---- 前窗趋势(对比基期:前一等长窗口)----
	TrendFlatDelta      float64 // |Δ命中率| < 此值 → 平稳
	TrendDropAlertDelta float64 // 较前窗下降 ≥ 此值(绝对占比)→ 提示关注
}

// DefaultConfig 默认配置(阈值均为首版保守值,按真实流量校准后调整;
// 上线初期按各域名历史命中率分位数经 CalibrateThresholds 初始化 DomainLevels,
// 漂移监控不属本期,本期仅单次快照 + 参数化)。
var DefaultConfig = Config{
	DefaultLevels: LevelThresholds{GoodMin: 0.90, MediumMin: 0.80},

	ByteGapLarge: 0.15,

	URIMissConcentration: 0.10,
	URIMissTopLimit:      5,

	ErrorStatusRatio: 0.10,

	// 动态路径特征(小写包含匹配):命中即视为疑似动态接口(低可信度提示)。
	DynamicURIHints: []string{"/api/", ".php", ".jsp", ".asp", ".ashx", ".cgi", "ajax", "/act", "/gateway", "/service"},

	DomainTopLimit:     8,
	TopRecommendations: 5,

	TrendFlatDelta:      0.005,
	TrendDropAlertDelta: 0.05,
}

// CalibrateThresholds 按域名历史命中率分位数初始化档位边界(纯函数;历史数据
// 来源于一次性查询现有 /aggregate 按域名分组的 cache_hit 聚合,不新增采集/存储)。
// 规则:中档下界 = min(默认, 历史分 25 位);优档下界 = min(默认, 历史分 75 位),
// 并保证 GoodMin > MediumMin(档距兜底 0.01);hist 为空或无合法值(∈[0,1])
// 时回退默认阈值。
func CalibrateThresholds(hist []float64) LevelThresholds {
	def := DefaultConfig.DefaultLevels
	vals := make([]float64, 0, len(hist))
	for _, v := range hist {
		if v >= 0 && v <= 1 {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return def
	}
	slices.Sort(vals)
	medium := math.Min(def.MediumMin, quantile(vals, 0.25))
	good := math.Min(def.GoodMin, quantile(vals, 0.75))
	if good <= medium {
		good = math.Min(def.GoodMin, medium+0.01)
	}
	if good <= medium { // 默认档距被压穿的极端配置兜底
		good = medium + 0.01
	}
	return LevelThresholds{GoodMin: good, MediumMin: medium}
}

// quantile 线性插值分位数(q ∈ [0,1];切片须已升序)。
func quantile(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	pos := q * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo < 0 {
		lo = 0
	}
	if hi >= n {
		hi = n - 1
	}
	if lo >= hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
