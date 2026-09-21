// Package cdncache CDN 缓存命中率规则引擎(proposal:cdn-cache-analysis)。
//
// 纯函数、零 I/O:输入为现有联邦聚合通道可产出的结构(cache_hit 分布、
// status 分布、sum_bytes 双口径、域名×cache_hit 交叉、URI 未命中分布)+
// 前一等长窗口 cache_hit 分布;输出双命中率(请求/字节 × 全请求/可缓存)、
// 健康档位(优/中/差)、请求-字节 gap 双向归因(大文件回源带宽 + 小文件
// 回源请求)、未命中 URI 集中度(查询串归一后)与结构化优化项(可信度分级)。
// 权重与阈值集中在 config.go(DefaultConfig,可校准),判据全部确定性可计算,
// 不引入 LLM/外部调用,不新增数据面。
//
// 口径(文档化,硬规则):命中 = hit+partial,未命中 = miss+error;
// cache_hit 未知("-")计入分母、不计入命中/未命中两侧;消费
// shared/cloudx/logquery.NormalizeCacheHit 的既有归一枚举,不改写归一路径。
package cdncache

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// 健康档位(优/中/差;数据不足为 unknown)。
const (
	GradeGood    = "good"
	GradeFair    = "fair"
	GradePoor    = "poor"
	GradeUnknown = "unknown"
)

// 建议动作(结构化优化项;proposal 约定四类,可执行而非纯文案)。
const (
	ActionAddCacheRule      = "add_cache_rule"      // 加/调缓存规则
	ActionTuneTTL           = "tune_ttl"            // 调整 TTL(大文件场景含分片建议)
	ActionIgnoreQueryString = "ignore_query_string" // 忽略查询串(必附 miss 集中证据)
	ActionOriginVerify      = "origin_verify"       // 强制回源校验(错误码场景)
)

// 可信度分级(高 = gap/URI 集中度证据充分;中 = 启发式/近似口径;
// 低 = 动态接口类启发式判定,前端默认折叠)。
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// 趋势方向(前窗对比)。
const (
	TrendUp   = "up"
	TrendFlat = "flat"
	TrendDown = "down"
)

// 口径标注(集中透出,前端卡面统一展示)。
const (
	// NoteClassification 命中归类文档化(硬规则:partial/error 归类显式,不静默)。
	NoteClassification = "命中归类:命中 = hit+partial,未命中 = miss+error;cache_hit 未知(-)计入分母、不计入命中/未命中两侧"
	// NoteCacheableApprox 可缓存请求档近似口径(单维度聚合无 cache_hit×status 交叉)。
	NoteCacheableApprox = "可缓存档为聚合层近似:剔除量取 cache_hit=error 与 4xx/5xx 的较大值(两单维度聚合重叠未知,避免重复剔除高估命中);method(POST)/认证/Range 无法剔除计入分母,口径偏保守"
	// NoteByteBias partial 按全命中计入 → 字节命中率上偏(proposal Key Risks 显式标注)。
	NoteByteBias = "字节命中率:partial 按全命中计入,口径上偏;待源日志可折算分片命中字节后修正"
	// NoteCacheableByte 可缓存字节口径的对齐近似(状态码字节分布不可得)。
	NoteCacheableByte = "可缓存字节口径仅剔除 cache_hit=error 字节(状态码字节分布不可得,与可缓存请求档对齐为近似)"
)

// HostCacheStat 域名×cache_hit 交叉分布(单域名 cache_hit 四态计数;由聚合层
// 产出,引擎不做二维聚合)。缺失时域名排行降级(置空 + 标注)。
type HostCacheStat struct {
	Host    string `json:"host"`
	Hit     int64  `json:"hit"`
	Partial int64  `json:"partial"`
	Miss    int64  `json:"miss"`
	Error   int64  `json:"error"`
}

// URIMissItem URI 未命中条目(原始 URI 可含查询串,查询串归一由引擎完成并
// 导出供聚合层复用)。Miss 口径同命中归类:未命中 = miss+error。
type URIMissItem struct {
	URI  string `json:"uri"`  // 原始 URI(可含查询串)
	Host string `json:"host"` // 归属域名(可空;全局聚合无域名时置空)
	Miss int64  `json:"miss"` // 未命中请求数
}

// PrevCacheWindow 前一等长窗口 cache_hit 分布(趋势对比;nil 或分布缺失 =
// 前窗无数据,降级为仅当前窗绝对量判定)。
type PrevCacheWindow struct {
	Total        int64               `json:"total"`                    // 前窗总请求数
	CacheHitDist []logquery.TopNItem `json:"cache_hit_dist,omitempty"` // 前窗 cache_hit 计数分布
}

// CacheAnalyzeInput 缓存分析输入(全部来自现有单维度聚合通道可产出结构;
// 同一时间边界 + 同一执行批次并发查询 → 同一输出,口径可复现)。
type CacheAnalyzeInput struct {
	TotalRequests int64               `json:"total_requests"`       // 当前窗总请求数(聚合精确总数;≤0 回退分布求和)
	CacheHitDist  []logquery.TopNItem `json:"cache_hit_dist"`       // cache_hit 维度计数分布(hit/miss/partial/error/-)
	CacheHitBytes []logquery.TopNItem `json:"cache_hit_bytes"`      // cache_hit × sum_bytes(Value=字节)
	TotalBytes    int64               `json:"total_bytes"`          // 全请求字节总量(sum_bytes 总额;≤0 回退 CacheHitBytes 求和)
	StatusDist    []logquery.TopNItem `json:"status_dist"`          // status 维度分布(4xx/5xx 占比;状态×cache_hit 交叉为后续增强)
	HostStats     []HostCacheStat     `json:"host_stats,omitempty"` // 域名×cache_hit 交叉(缺 → 域名排行降级)
	URIMisses     []URIMissItem       `json:"uri_misses,omitempty"` // URI 未命中分布(缺 → URI 集中度降级)
	Prev          *PrevCacheWindow    `json:"prev,omitempty"`       // 前窗对比(nil → 仅当前窗绝对量判定)
}

// HitRateTier 单口径命中率(分子/分母透出供前端核对口径;分母≤0 时不可用)。
type HitRateTier struct {
	Rate        float64 `json:"rate"`        // 0-1
	Numerator   int64   `json:"numerator"`   // 命中请求/字节(hit+partial)
	Denominator int64   `json:"denominator"` // 总请求/字节
	Available   bool    `json:"available"`   // 分母>0
}

// HitRatePair 双口径命中率:All = 全请求(主口径);Cacheable = 可缓存(聚合层近似)。
type HitRatePair struct {
	All       HitRateTier `json:"all"`
	Cacheable HitRateTier `json:"cacheable"`
}

// DomainHitStat 域名命中率排行条目(请求数降序 Top 8;档位按域名阈值覆盖)。
type DomainHitStat struct {
	Host             string  `json:"host"`
	Requests         int64   `json:"requests"`
	HitRate          float64 `json:"hit_rate"`           // 全请求口径 (hit+partial)/requests
	CacheableHitRate float64 `json:"cacheable_hit_rate"` // 可缓存近似口径(仅剔除 cache_hit=error)
	MissTrafficRatio float64 `json:"miss_traffic_ratio"` // 该域名未命中(miss+error)占全局未命中
	Grade            string  `json:"grade"`              // good/fair/poor/unknown(域名级阈值)
}

// URIMissTop 未命中 URI 条目(查询串归一后按 miss 降序,Top N)。
type URIMissTop struct {
	URI         string  `json:"uri"`                    // 归一路径(无查询串)
	Host        string  `json:"host"`                   // 归属域名(贡献最大者;可空)
	MissCount   int64   `json:"miss_count"`             // 归并后未命中请求数
	MissShare   float64 `json:"miss_share"`             // 占全部未命中请求(TopN 截断时为下界)
	Variants    int     `json:"variants"`               // 归并的查询串变体数(≥2 = 仅查询串不同的资源被拆散)
	SampleQuery string  `json:"sample_query,omitempty"` // 贡献最大变体的排序归一查询串(证据展示)
}

// StatusOverview 状态码分布概览(4xx/5xx 占总响应;占比分母为总请求数,
// 状态 TopN 截断时计数为下界)。
type StatusOverview struct {
	Total            int64   `json:"total"`              // 状态码分布合计
	ClientErrorCount int64   `json:"client_error_count"` // 4xx 计数
	ServerErrorCount int64   `json:"server_error_count"` // 5xx 计数
	ClientErrorRatio float64 `json:"client_error_ratio"` // 4xx/总响应
	ServerErrorRatio float64 `json:"server_error_ratio"` // 5xx/总响应
}

// GapAnalysis 请求-字节 gap 双向归因(两方向独立判定,可同时成立)。
type GapAnalysis struct {
	ByteGapRatio     float64 `json:"byte_gap_ratio"`     // 请求命中率(全请求) − 字节命中率(全请求);字节不可用时 0
	LargeFileGap     bool    `json:"large_file_gap"`     // 请求命中尚可而字节明显低 → 大文件未命中(回源带宽浪费)
	SmallFileReverse bool    `json:"small_file_reverse"` // 请求命中率低而字节尚可 → 小文件大量 miss(回源请求数/QPS 浪费)
}

// OptimizationItem 结构化优化项(可执行对象,非纯文案;卡面统一注明
// "建议,执行前请验证")。MissTrafficRatio 为该项关联未命中流量占比:
// 请求口径项 = 未命中请求占比;字节 gap 项 = 未命中字节占总字节;状态码项 = 错误响应占比。
type OptimizationItem struct {
	Domain           string  `json:"domain,omitempty"`     // 域名(空=全局)
	URIPrefix        string  `json:"uri_prefix,omitempty"` // URI 前缀/归一路径(空=不限)
	Action           string  `json:"action"`               // add_cache_rule/tune_ttl/ignore_query_string/origin_verify
	MissTrafficRatio float64 `json:"miss_traffic_ratio"`   // 未命中流量占比(口径随项类型,见结构注释)
	Confidence       string  `json:"confidence"`           // high/medium/low
	LowConfidence    bool    `json:"low_confidence"`       // 低可信度(前端默认折叠)
	Evidence         string  `json:"evidence"`             // 判据证据(含具体数值;忽略查询串必附 miss 集中证据)
}

// PrevTrend 前窗对比趋势(基期:前一等长窗口;前窗仅 1 帧 cache_hit 分布,
// 对比为全请求口径)。
type PrevTrend struct {
	Available          bool    `json:"available"`             // 前窗数据可用
	PrevRequestHitRate float64 `json:"prev_request_hit_rate"` // 前窗命中率(全请求口径,0-1)
	Delta              float64 `json:"delta"`                 // 当前 − 前窗(0-1;正=上升)
	Direction          string  `json:"direction"`             // up/flat/down
	Alert              bool    `json:"alert"`                 // 下降 ≥ 阈值 → 提示关注
}

// CacheAnalyzeResult 缓存分析结论(口径标注集中 Notes;同一输入 → 同一输出)。
type CacheAnalyzeResult struct {
	Grade           string             `json:"grade"`            // good/fair/poor/unknown(可缓存口径)
	RequestHitRate  HitRatePair        `json:"request_hit_rate"` // 请求命中率(全请求/可缓存)
	ByteHitRate     HitRatePair        `json:"byte_hit_rate"`    // 字节命中率(全请求/可缓存)
	DomainRanking   []DomainHitStat    `json:"domain_ranking"`   // 域名命中率排行(请求数降序 Top 8)
	MissURITop      []URIMissTop       `json:"miss_uri_top"`     // 未命中 URI TOP(归一后,Top 5)
	Status          StatusOverview     `json:"status"`           // 状态码分布(4xx/5xx 占比)
	Gap             GapAnalysis        `json:"gap"`              // 请求-字节 gap 双向归因
	Recommendations []OptimizationItem `json:"recommendations"`  // 结构化优化项(非优档按占比 Top 5;优档仅 gap 证据项或 0 条)
	Prev            PrevTrend          `json:"prev_trend"`       // 前窗对比
	Notes           []string           `json:"notes"`            // 口径/降级标注(partial 上偏、可缓存近似等)
}

// Evaluate 缓存分析入口(纯函数:不改写输入、无 I/O;nil 输入返回安全空结论)。
func Evaluate(input *CacheAnalyzeInput) *CacheAnalyzeResult {
	return evaluate(input, withDefaults(nil))
}

// EvaluateWithConfig 自定义配置入口(部分字段零值回退默认;不改写调用方配置)。
func EvaluateWithConfig(input *CacheAnalyzeInput, cfg *Config) *CacheAnalyzeResult {
	return evaluate(input, withDefaults(cfg))
}

// withDefaults 配置归一:零值字段回退默认(克隆归一,不改写调用方配置)。
func withDefaults(cfg *Config) *Config {
	merged := DefaultConfig
	if cfg == nil {
		return &merged
	}
	if cfg.DefaultLevels.GoodMin > 0 {
		merged.DefaultLevels.GoodMin = cfg.DefaultLevels.GoodMin
	}
	if cfg.DefaultLevels.MediumMin > 0 {
		merged.DefaultLevels.MediumMin = cfg.DefaultLevels.MediumMin
	}
	merged.DomainLevels = cfg.DomainLevels
	if cfg.ByteGapLarge > 0 {
		merged.ByteGapLarge = cfg.ByteGapLarge
	}
	if cfg.URIMissConcentration > 0 {
		merged.URIMissConcentration = cfg.URIMissConcentration
	}
	if cfg.URIMissTopLimit > 0 {
		merged.URIMissTopLimit = cfg.URIMissTopLimit
	}
	if cfg.ErrorStatusRatio > 0 {
		merged.ErrorStatusRatio = cfg.ErrorStatusRatio
	}
	if len(cfg.DynamicURIHints) > 0 {
		merged.DynamicURIHints = cfg.DynamicURIHints
	}
	if cfg.DomainTopLimit > 0 {
		merged.DomainTopLimit = cfg.DomainTopLimit
	}
	if cfg.TopRecommendations > 0 {
		merged.TopRecommendations = cfg.TopRecommendations
	}
	if cfg.TrendFlatDelta > 0 {
		merged.TrendFlatDelta = cfg.TrendFlatDelta
	}
	if cfg.TrendDropAlertDelta > 0 {
		merged.TrendDropAlertDelta = cfg.TrendDropAlertDelta
	}
	return &merged
}

// assess 单次判定中间上下文(输入派生指标集中计算一次)。
type assess struct {
	cfg   *Config
	input *CacheAnalyzeInput

	totalReq   int64
	hit        int64
	partial    int64
	miss       int64
	errState   int64
	unknown    int64
	hitBytes   int64
	partialBts int64
	missBytes  int64
	errBytes   int64
	unknownBts int64
	byteTotal  int64 // 全请求字节(TotalBytes ≤0 回退分布求和)

	statusSum   int64 // 状态码分布合计(TopN 截断时 < TotalRequests)
	four        int64
	five        int64
	totalMiss   int64  // miss + errState(命中归类口径的全局未命中)
	topMissHost string // 未命中最多域名(并列取字典序最小;空=无域名交叉)
}

// evaluate 主流程(纯函数;输出排序全量确定性,保证同一输入同一输出)。
func evaluate(input *CacheAnalyzeInput, cfg *Config) *CacheAnalyzeResult {
	res := &CacheAnalyzeResult{
		DomainRanking:   []DomainHitStat{},
		MissURITop:      []URIMissTop{},
		Recommendations: []OptimizationItem{},
		Notes:           []string{NoteClassification},
	}
	if input == nil {
		res.Grade = GradeUnknown
		res.Notes = append(res.Notes, "分析输入为空,未执行判定;请确认聚合查询已返回结果")
		return res
	}
	a := buildAssess(input, cfg)

	// ---- 请求命中率(全请求/可缓存两档)----
	hitAll := a.hit + a.partial
	res.RequestHitRate.All = HitRateTier{
		Rate: ratio(hitAll, a.totalReq), Numerator: hitAll, Denominator: a.totalReq,
		Available: a.totalReq > 0,
	}
	if a.totalReq > 0 {
		res.Notes = append(res.Notes, NoteCacheableApprox)
		// 可缓存口径(聚合层近似):剔除量取 max(cache_hit=error, 4xx+5xx)——
		// 两个单维度聚合重叠未知,取大者避免重复剔除导致高估命中;
		// 4xx/5xx 按未命中近似,剔除不触碰命中。
		removed := maxI64(a.errState, a.four+a.five)
		denom := a.totalReq - removed
		if denom < hitAll {
			denom = hitAll
		}
		if denom < 0 {
			denom = 0
		}
		res.RequestHitRate.Cacheable = HitRateTier{
			Rate: ratio(hitAll, denom), Numerator: hitAll, Denominator: denom,
			Available: denom > 0,
		}
		if denom <= 0 {
			res.Notes = append(res.Notes, "可缓存口径分母为空,可缓存档不可用")
		}
	}

	// ---- 字节命中率(全请求主口径 / 可缓存)----
	if a.byteTotal > 0 {
		res.Notes = append(res.Notes, NoteByteBias, NoteCacheableByte)
		byteHit := a.hitBytes + a.partialBts // partial 按全命中计入(上偏已标注)
		res.ByteHitRate.All = HitRateTier{
			Rate: ratio(byteHit, a.byteTotal), Numerator: byteHit, Denominator: a.byteTotal, Available: true,
		}
		bDenom := a.byteTotal - a.errBytes // 可缓存字节口径:仅剔除 error 字节
		if bDenom < byteHit {
			bDenom = byteHit
		}
		if bDenom < 0 {
			bDenom = 0
		}
		res.ByteHitRate.Cacheable = HitRateTier{
			Rate: ratio(byteHit, bDenom), Numerator: byteHit, Denominator: bDenom, Available: bDenom > 0,
		}
	} else {
		res.Notes = append(res.Notes, "字节口径数据缺失(sum_bytes 未返回),字节命中率不可用")
	}

	// ---- 健康档位(可缓存口径;不可用回退全请求口径)----
	if a.totalReq <= 0 {
		res.Grade = GradeUnknown
		res.Notes = append(res.Notes, "请求数据缺失,未执行命中率判定")
	} else {
		tier := res.RequestHitRate.Cacheable
		if !tier.Available {
			tier = res.RequestHitRate.All
			res.Notes = append(res.Notes, "可缓存档不可用,档位按全请求口径判定")
		}
		res.Grade = gradeFor(tier.Rate, cfg.DefaultLevels)
	}

	// ---- 域名命中率排行 ----
	if len(input.HostStats) == 0 {
		res.Notes = append(res.Notes, "域名×cache_hit 交叉分布缺失,域名排行降级(仅总览命中率)")
	} else {
		res.DomainRanking = buildDomainRanking(input.HostStats, a.totalMiss, cfg)
	}

	// ---- 未命中 URI TOP(查询串归一)----
	if len(input.URIMisses) == 0 {
		res.Notes = append(res.Notes, "URI 未命中分布缺失,URI 集中度与查询串建议不可用")
	} else {
		res.MissURITop = buildURIMissTop(input.URIMisses, a.totalMiss, cfg)
		res.Notes = append(res.Notes, "URI 未命中为 TopN 聚合,占比为下界")
	}

	// ---- 状态码分布 ----
	res.Status = buildStatusOverview(a)

	// ---- 请求-字节 gap 双向归因 ----
	res.Gap = buildGap(a, res)

	// ---- 结构化优化项 ----
	res.Recommendations = buildRecommendations(a, res, cfg)

	// ---- 前窗趋势 ----
	res.Prev = buildPrevTrend(res.RequestHitRate.All.Rate, input.Prev, cfg)
	if !res.Prev.Available {
		res.Notes = append(res.Notes, "前窗无数据,仅当前窗绝对量判定(对比基期:前一等长窗口)")
	}
	return res
}

// buildAssess 输入派生指标一次算齐(不改写调用方数据)。
func buildAssess(input *CacheAnalyzeInput, cfg *Config) *assess {
	a := &assess{cfg: cfg, input: input}
	a.hit, a.partial, a.miss, a.errState, a.unknown = sumStateCounts(input.CacheHitDist)
	a.hitBytes, a.partialBts, a.missBytes, a.errBytes, a.unknownBts = sumStateBytes(input.CacheHitBytes)
	a.totalReq = input.TotalRequests
	if a.totalReq <= 0 {
		a.totalReq = a.hit + a.partial + a.miss + a.errState + a.unknown
	}
	a.byteTotal = input.TotalBytes
	if a.byteTotal <= 0 {
		a.byteTotal = a.hitBytes + a.partialBts + a.missBytes + a.errBytes + a.unknownBts
	}
	for _, it := range input.StatusDist {
		a.statusSum += it.Count
	}
	a.four, a.five = errorStatusCounts(input.StatusDist)
	a.totalMiss = a.miss + a.errState
	a.topMissHost = topMissHost(input.HostStats)
	return a
}

// ---- 各判定构建 ----

// gradeFor 档位判定(临界含下界:rate ≥ GoodMin → 优;≥ MediumMin → 中;否则差)。
func gradeFor(rate float64, lv LevelThresholds) string {
	switch {
	case rate >= lv.GoodMin:
		return GradeGood
	case rate >= lv.MediumMin:
		return GradeFair
	default:
		return GradePoor
	}
}

// buildDomainRanking 域名命中率排行(请求数降序、并列按域名升序;档位按域名
// 阈值覆盖,取不到回退默认;域名级可缓存口径仅剔除 cache_hit=error)。
func buildDomainRanking(stats []HostCacheStat, totalMiss int64, cfg *Config) []DomainHitStat {
	out := make([]DomainHitStat, 0, len(stats))
	for _, s := range stats {
		req := s.Hit + s.Partial + s.Miss + s.Error
		hit := s.Hit + s.Partial
		hitRate := ratio(hit, req)
		cDenom := req - s.Error
		cRate := hitRate
		if cDenom > 0 {
			cRate = ratio(hit, cDenom)
		}
		lv, ok := cfg.DomainLevels[s.Host]
		if !ok {
			lv = cfg.DefaultLevels
		}
		gradeRate := cRate
		if cDenom <= 0 {
			gradeRate = hitRate // 可缓存分母为空回退全请求口径
		}
		out = append(out, DomainHitStat{
			Host:             s.Host,
			Requests:         req,
			HitRate:          hitRate,
			CacheableHitRate: cRate,
			MissTrafficRatio: ratio(s.Miss+s.Error, totalMiss),
			Grade:            gradeFor(gradeRate, lv),
		})
	}
	slices.SortStableFunc(out, func(x, y DomainHitStat) int {
		if c := cmp.Compare(y.Requests, x.Requests); c != 0 {
			return c
		}
		return strings.Compare(x.Host, y.Host)
	})
	if len(out) > cfg.DomainTopLimit {
		out = out[:cfg.DomainTopLimit]
	}
	return out
}

// uriMissAgg 归一路径的未命中聚合(变体 = 排序归一后仍不同的查询串)。
type uriMissAgg struct {
	miss     int64
	variants map[string]int64 // 排序归一查询串 → miss 合计(""=无查询串)
	hosts    map[string]int64
}

// buildURIMissTop 未命中 URI TOP:原始 URI 查询串归一(剥离/排序)后按路径聚合,
// 同资源不同参数不再拆散稀释集中度;按 miss 降序取 Top N,归属域名取贡献最大者。
func buildURIMissTop(items []URIMissItem, totalMiss int64, cfg *Config) []URIMissTop {
	groups := make(map[string]*uriMissAgg)
	for _, it := range items {
		if it.Miss <= 0 {
			continue
		}
		path := NormalizeURIPath(it.URI)
		g := groups[path]
		if g == nil {
			g = &uriMissAgg{variants: map[string]int64{}, hosts: map[string]int64{}}
			groups[path] = g
		}
		g.miss += it.Miss
		g.variants[queryOf(NormalizeURIQuery(it.URI))] += it.Miss
		if it.Host != "" {
			g.hosts[it.Host] += it.Miss
		}
	}
	out := make([]URIMissTop, 0, len(groups))
	for path, g := range groups {
		out = append(out, URIMissTop{
			URI:         path,
			Host:        topKey(g.hosts),
			MissCount:   g.miss,
			MissShare:   ratio(g.miss, totalMiss),
			Variants:    len(g.variants),
			SampleQuery: topKey(g.variants),
		})
	}
	slices.SortStableFunc(out, func(x, y URIMissTop) int {
		if c := cmp.Compare(y.MissCount, x.MissCount); c != 0 {
			return c
		}
		return strings.Compare(x.URI, y.URI)
	})
	if len(out) > cfg.URIMissTopLimit {
		out = out[:cfg.URIMissTopLimit]
	}
	return out
}

// buildStatusOverview 状态码分布概览(占比分母为总请求数;缺失时回退分布合计)。
func buildStatusOverview(a *assess) StatusOverview {
	denom := a.totalReq
	if denom <= 0 {
		denom = a.statusSum
	}
	return StatusOverview{
		Total:            a.statusSum,
		ClientErrorCount: a.four,
		ServerErrorCount: a.five,
		ClientErrorRatio: ratio(a.four, denom),
		ServerErrorRatio: ratio(a.five, denom),
	}
}

// buildGap 请求-字节 gap 双向归因:大文件(请求命中率尚可而字节明显低 → 回源
// 带宽/流量浪费)与小文件(可缓存请求命中率低而字节尚可 → 回源请求数/QPS 浪费),
// 两方向独立判定、同时纳入优化项;字节口径不可用时 gap 不判定。
func buildGap(a *assess, res *CacheAnalyzeResult) GapAnalysis {
	g := GapAnalysis{}
	if !res.ByteHitRate.All.Available || !res.RequestHitRate.All.Available {
		return g
	}
	g.ByteGapRatio = res.RequestHitRate.All.Rate - res.ByteHitRate.All.Rate
	// 大文件:请求命中率 ≥ 中档下界(尚可)而字节明显低。
	if res.RequestHitRate.All.Rate >= a.cfg.DefaultLevels.MediumMin && g.ByteGapRatio >= a.cfg.ByteGapLarge {
		g.LargeFileGap = true
	}
	// 小文件:可缓存请求命中率低于中档下界而字节命中率尚可。
	if res.RequestHitRate.Cacheable.Available &&
		res.RequestHitRate.Cacheable.Rate < a.cfg.DefaultLevels.MediumMin &&
		res.ByteHitRate.All.Rate >= a.cfg.DefaultLevels.MediumMin {
		g.SmallFileReverse = true
	}
	return g
}

// buildRecommendations 结构化优化项:候选生成 → 按未命中流量占比降序 → Top N。
// gap 双向归因项为强证据,不受档位限制(优档允许 0 条 = 无强证据不硬凑);
// URI 集中度/状态码/域名级项仅在非优档生成。
// 可信度:高 = gap/URI 集中度证据充分;中 = 启发式/近似口径(域名级可缓存近似、
// 4xx/5xx 整体口径);低 = 动态接口类启发式判定(供前端默认折叠)。
func buildRecommendations(a *assess, res *CacheAnalyzeResult, cfg *Config) []OptimizationItem {
	cands := make([]OptimizationItem, 0, 6)
	nonGood := res.Grade == GradeFair || res.Grade == GradePoor

	// 1) 大文件未命中(字节 gap 方向):未命中字节占总字节 = 回源带宽浪费占比。
	if res.Gap.LargeFileGap {
		byteMiss := maxI64(a.byteTotal-(a.hitBytes+a.partialBts), 0)
		it := OptimizationItem{
			Action:           ActionTuneTTL,
			MissTrafficRatio: ratio(byteMiss, a.byteTotal),
			Confidence:       ConfidenceHigh,
			Evidence: fmt.Sprintf("请求命中率 %.1f%%(全请求口径)而字节命中率仅 %.1f%%,gap %.1f 个百分点;未命中字节占 %.1f%%,疑似大文件未命中,回源带宽/流量浪费",
				res.RequestHitRate.All.Rate*100, res.ByteHitRate.All.Rate*100, res.Gap.ByteGapRatio*100, ratio(byteMiss, a.byteTotal)*100),
		}
		if a.topMissHost != "" {
			it.Domain = a.topMissHost
			it.Evidence += fmt.Sprintf(";未命中 Top 域名:%s", a.topMissHost)
		}
		it.Evidence += ";建议对热点大文件调大 TTL 或开启分片缓存,执行前请验证"
		cands = append(cands, it)
	}

	// 2) 小文件大量 miss(请求方向):未命中请求占总请求 = 回源请求数/QPS 浪费占比。
	if res.Gap.SmallFileReverse {
		it := OptimizationItem{
			Action:           ActionAddCacheRule,
			MissTrafficRatio: ratio(a.totalMiss, a.totalReq),
			Confidence:       ConfidenceHigh,
			Evidence: fmt.Sprintf("可缓存请求命中率 %.1f%% 低于中档下界 %.0f%%,而字节命中率 %.1f%% 尚可,疑似小文件大量 miss,回源请求数/QPS 与按请求计费浪费",
				res.RequestHitRate.Cacheable.Rate*100, cfg.DefaultLevels.MediumMin*100, res.ByteHitRate.All.Rate*100),
		}
		if a.topMissHost != "" {
			it.Domain = a.topMissHost
		}
		it.Evidence += ";建议补充缓存规则或预缓存热点小资源,执行前请验证"
		cands = append(cands, it)
	}

	if !nonGood {
		return sortAndCap(cands, cfg)
	}

	// 3) 未命中 URI 集中度(查询串归一后):变体 ≥2 → 忽略查询串(附 miss 集中
	//    证据);命中动态接口特征 → 低可信度(疑似动态接口误开缓存,执行前须确认)。
	for _, u := range res.MissURITop {
		if u.MissShare < cfg.URIMissConcentration {
			continue
		}
		action, evidence := ActionAddCacheRule, fmt.Sprintf("归一路径 %s 未命中 %d 次,占全部未命中 %.1f%%(≥ 集中度阈值 %.0f%%)",
			u.URI, u.MissCount, u.MissShare*100, cfg.URIMissConcentration*100)
		conf := ConfidenceHigh
		if u.Variants >= 2 {
			action = ActionIgnoreQueryString
			evidence += fmt.Sprintf(";忽略查询串证据:该路径存在 %d 个仅查询串不同的变体(最多变体查询串:%s),合计未命中 %d 次占 %.1f%% —— 查询串未忽略导致同资源缓存被拆散",
				u.Variants, displayQuery(u.SampleQuery), u.MissCount, u.MissShare*100)
		}
		if hint, isDyn := dynamicURIHint(u.URI, cfg); isDyn {
			conf = ConfidenceLow
			evidence += fmt.Sprintf(";路径命中动态接口特征(%s),疑似动态接口误开缓存,执行前须人工确认", hint)
		}
		evidence += ";建议按缓存规则处理,执行前请验证"
		cands = append(cands, OptimizationItem{
			Domain:           u.Host,
			URIPrefix:        u.URI,
			Action:           action,
			MissTrafficRatio: u.MissShare,
			Confidence:       conf,
			LowConfidence:    conf == ConfidenceLow,
			Evidence:         evidence,
		})
	}

	// 4) 4xx/5xx 占比高:错误响应缓存无效 → 强制回源校验(整体口径,中可信度)。
	errDenom := statusDenom(a)
	errRatio := ratio(a.four+a.five, errDenom)
	if errRatio >= cfg.ErrorStatusRatio {
		cands = append(cands, OptimizationItem{
			Action:           ActionOriginVerify,
			MissTrafficRatio: errRatio,
			Confidence:       ConfidenceMedium,
			Evidence: fmt.Sprintf("4xx/5xx 占总响应 %.1f%%(4xx %.1f%%、5xx %.1f%%),错误响应缓存无效;建议对错误码强制回源校验或配置缓存规则排除错误响应(占比为整体口径,状态×cache_hit 交叉为后续增强),执行前请验证",
				errRatio*100, ratio(a.four, errDenom)*100, ratio(a.five, errDenom)*100),
		})
	}

	// 5) 域名级未命中集中(可缓存口径为聚合层近似 → 中可信度)。
	for _, d := range res.DomainRanking {
		if d.MissTrafficRatio < cfg.URIMissConcentration || d.Grade == GradeGood {
			continue
		}
		cands = append(cands, OptimizationItem{
			Domain:           d.Host,
			Action:           ActionAddCacheRule,
			MissTrafficRatio: d.MissTrafficRatio,
			Confidence:       ConfidenceMedium,
			Evidence: fmt.Sprintf("域名 %s 未命中占全部未命中 %.1f%%,命中率 %.1f%%(可缓存近似口径);建议为该域名补充/调整缓存规则(聚合层近似,执行前请验证)",
				d.Host, d.MissTrafficRatio*100, d.CacheableHitRate*100),
		})
	}
	return sortAndCap(cands, cfg)
}

// sortAndCap 候选排序(未命中流量占比降序 → 可信度降序 → domain/uri/action
// 字典序,全量确定性)并截断 Top N。
func sortAndCap(cands []OptimizationItem, cfg *Config) []OptimizationItem {
	slices.SortStableFunc(cands, func(x, y OptimizationItem) int {
		if c := cmp.Compare(y.MissTrafficRatio, x.MissTrafficRatio); c != 0 {
			return c
		}
		if c := cmp.Compare(confidenceRank(y.Confidence), confidenceRank(x.Confidence)); c != 0 {
			return c
		}
		if c := strings.Compare(x.Domain, y.Domain); c != 0 {
			return c
		}
		if c := strings.Compare(x.URIPrefix, y.URIPrefix); c != 0 {
			return c
		}
		return strings.Compare(x.Action, y.Action)
	})
	if len(cands) > cfg.TopRecommendations {
		cands = cands[:cfg.TopRecommendations]
	}
	return cands
}

// buildPrevTrend 前窗对比(全请求口径;前窗分布缺失 → 不可用,不虚构趋势)。
func buildPrevTrend(curRate float64, prev *PrevCacheWindow, cfg *Config) PrevTrend {
	if prev == nil {
		return PrevTrend{}
	}
	pHit, pPartial, pMiss, pErr, pUnknown := sumStateCounts(prev.CacheHitDist)
	distSum := pHit + pPartial + pMiss + pErr + pUnknown
	if distSum <= 0 {
		return PrevTrend{} // 分布缺失(Total 有值也无从计算命中率)
	}
	pTotal := prev.Total
	if pTotal <= 0 {
		pTotal = distSum
	}
	prevRate := ratio(pHit+pPartial, pTotal)
	delta := curRate - prevRate
	direction := TrendFlat
	switch {
	case delta >= cfg.TrendFlatDelta:
		direction = TrendUp
	case delta <= -cfg.TrendFlatDelta:
		direction = TrendDown
	}
	return PrevTrend{
		Available:          true,
		PrevRequestHitRate: prevRate,
		Delta:              delta,
		Direction:          direction,
		Alert:              direction == TrendDown && -delta >= cfg.TrendDropAlertDelta,
	}
}

// ---- URI 查询串归一(导出供聚合层复用)----

// NormalizeURIPath URI 查询串剥离归一:去查询串与 fragment,保留路径(补前导
// "/");未命中集中度按路径聚合,避免同一资源带不同参数被拆散稀释 miss 集中度。
func NormalizeURIPath(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "/"
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// NormalizeURIQuery URI 查询串排序归一:保留路径,query 参数按名称(同名按值)
// 升序重组 —— 同参异序归一为同一形态;用于变体计数与证据展示(幂等)。
func NormalizeURIQuery(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	path, query := s, ""
	if i := strings.IndexByte(s, '?'); i >= 0 {
		path, query = s[:i], s[i+1:]
	}
	switch {
	case path == "":
		path = "/"
	case !strings.HasPrefix(path, "/"):
		path = "/" + path
	}
	if query == "" {
		return path
	}
	pairs := strings.Split(query, "&")
	type kv struct{ k, v string }
	list := make([]kv, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		if i := strings.IndexByte(p, '='); i >= 0 {
			list = append(list, kv{p[:i], p[i+1:]})
		} else {
			list = append(list, kv{p, ""})
		}
	}
	if len(list) == 0 {
		return path
	}
	slices.SortFunc(list, func(x, y kv) int {
		if c := strings.Compare(x.k, y.k); c != 0 {
			return c
		}
		return strings.Compare(x.v, y.v)
	})
	var b strings.Builder
	b.WriteString(path)
	b.WriteByte('?')
	for i, p := range list {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p.k)
		if p.v != "" {
			b.WriteByte('=')
			b.WriteString(p.v)
		}
	}
	return b.String()
}

// queryOf 排序归一 URI 的查询串部分(无查询串返回空)。
func queryOf(normalized string) string {
	if i := strings.IndexByte(normalized, '?'); i >= 0 {
		return normalized[i+1:]
	}
	return ""
}

// displayQuery 证据中的查询串展示(空值占位)。
func displayQuery(q string) string {
	if q == "" {
		return "(无查询串)"
	}
	return "?" + q
}

// dynamicURIHint URI 是否命中动态接口特征(小写包含匹配;返回命中关键字)。
func dynamicURIHint(uri string, cfg *Config) (string, bool) {
	l := strings.ToLower(uri)
	for _, h := range cfg.DynamicURIHints {
		if h != "" && strings.Contains(l, h) {
			return h, true
		}
	}
	return "", false
}

// ---- 判定辅助 ----

// stateOf cache_hit 取值归类(消费 shared/cloudx/logquery.NormalizeCacheHit 的
// 输出枚举 hit/miss/partial/error/-;未识别取值归入 unknown,不改写既有归一路径)。
func stateOf(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hit":
		return "hit"
	case "partial":
		return "partial"
	case "miss":
		return "miss"
	case "error":
		return "error"
	default:
		return "unknown"
	}
}

// sumStateCounts cache_hit 计数分布按四态 + unknown 汇总。
func sumStateCounts(items []logquery.TopNItem) (hit, partial, miss, errState, unknown int64) {
	for _, it := range items {
		switch stateOf(it.Name) {
		case "hit":
			hit += it.Count
		case "partial":
			partial += it.Count
		case "miss":
			miss += it.Count
		case "error":
			errState += it.Count
		default:
			unknown += it.Count
		}
	}
	return
}

// sumStateBytes cache_hit × sum_bytes 分布按四态 + unknown 汇总(Value=字节)。
func sumStateBytes(items []logquery.TopNItem) (hit, partial, miss, errState, unknown int64) {
	for _, it := range items {
		switch stateOf(it.Name) {
		case "hit":
			hit += int64(it.Value)
		case "partial":
			partial += int64(it.Value)
		case "miss":
			miss += int64(it.Value)
		case "error":
			errState += int64(it.Value)
		default:
			unknown += int64(it.Value)
		}
	}
	return
}

// errorStatusCounts 状态码分布中 4xx/5xx 计数(非数值/其他区间忽略;
// TopN 截断时为下界,判据保守设计)。
func errorStatusCounts(items []logquery.TopNItem) (four, five int64) {
	for _, it := range items {
		code, err := strconv.Atoi(strings.TrimSpace(it.Name))
		if err != nil {
			continue
		}
		switch {
		case code >= 400 && code <= 499:
			four += it.Count
		case code >= 500 && code <= 599:
			five += it.Count
		}
	}
	return
}

// statusDenom 状态码占比分母(总请求数;缺失回退分布合计)。
func statusDenom(a *assess) int64 {
	if a.totalReq > 0 {
		return a.totalReq
	}
	return a.statusSum
}

// topMissHost 未命中(cache_hit 归类 miss+error)最多的域名(并列取字典序最小,
// 保证确定性;无域名交叉返回空)。
func topMissHost(stats []HostCacheStat) string {
	var best string
	var bestMiss int64
	for _, s := range stats {
		m := s.Miss + s.Error
		if m > bestMiss || (m == bestMiss && bestMiss > 0 && s.Host < best) {
			best, bestMiss = s.Host, m
		}
	}
	return best
}

// topKey 计数最大键(并列取字典序最小,保证确定性;空集返回空)。
func topKey(counter map[string]int64) string {
	var best string
	var bestN int64
	first := true
	for k, n := range counter {
		if first || n > bestN || (n == bestN && k < best) {
			best, bestN, first = k, n, false
		}
	}
	return best
}

// confidenceRank 可信度序(排序兜底用)。
func confidenceRank(c string) int {
	switch c {
	case ConfidenceHigh:
		return 3
	case ConfidenceMedium:
		return 2
	case ConfidenceLow:
		return 1
	default:
		return 0
	}
}

// ratio 占比(total≤0 返回 0,防除零)。
func ratio(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total)
}

// maxI64 取大。
func maxI64(x, y int64) int64 {
	if x > y {
		return x
	}
	return y
}

// ---- 展示标签(供 API 层/前端复用)----

// GradeLabel 健康档位中文标签。
func GradeLabel(g string) string {
	switch g {
	case GradeGood:
		return "优"
	case GradeFair:
		return "中"
	case GradePoor:
		return "差"
	default:
		return "未知"
	}
}

// ActionLabel 建议动作中文标签。
func ActionLabel(a string) string {
	switch a {
	case ActionAddCacheRule:
		return "加/调缓存规则"
	case ActionTuneTTL:
		return "调整 TTL"
	case ActionIgnoreQueryString:
		return "忽略查询串"
	case ActionOriginVerify:
		return "强制回源校验"
	default:
		return a
	}
}

// ConfidenceLabel 可信度中文标签。
func ConfidenceLabel(c string) string {
	switch c {
	case ConfidenceHigh:
		return "高"
	case ConfidenceMedium:
		return "中"
	case ConfidenceLow:
		return "低"
	default:
		return c
	}
}
