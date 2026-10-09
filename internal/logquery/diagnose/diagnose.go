// Package diagnose WAF 流量诊断规则引擎(proposal:网站被刷检测)。
//
// 纯函数、零 I/O:输入为现有联邦聚合通道可产出的结构(分桶 / TopN 维度聚合,
// 见 cloudx/logquery.AggregateResult)+ 前一等长窗口对比值;输出风险分 /
// 风险等级 / 疑似攻击类型 / 措施文案 / Top 攻击源。权重与阈值集中在
// config.go(DefaultConfig,可校准),判据全部确定性可计算,不引入
// LLM / 外部调用,不新增数据面。
package diagnose

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cloudx-sdk/logquery"
)

// 风险等级(无/低/中/高)。
const (
	RiskLevelNone   = "none"
	RiskLevelLow    = "low"
	RiskLevelMedium = "medium"
	RiskLevelHigh   = "high"
)

// 疑似攻击类型(≥3 类可判 + 正常两态)。
const (
	AttackTypeNormal      = "normal"       // 正常流量
	AttackTypeCCFlood     = "cc_flood"     // CC 刷量:单 IP 极高频率(且突增,或绝对量极速)
	AttackTypeCrawler     = "crawler"      // 恶意爬虫:UA 异常/单一 + 4xx 高(或高请求量)
	AttackTypeBruteForce  = "brute_force"  // 接口爆破:单 IP 高频 + 4xx 高 + URI 集中
	AttackTypeNormalBurst = "normal_burst" // 正常高流量:突增但来源分散、UA 多样、无攻击动作(大促)
)

// PrevWindow 前一等长窗口对比值(突增判据;由前窗聚合查询产出)。
type PrevWindow struct {
	Total      int64 `json:"total"`        // 前窗总请求数
	TopIPCount int64 `json:"top_ip_count"` // 前窗 Top1 IP 请求数(0=无 Top IP 数据)
}

// DiagnoseInput 诊断输入:当前窗聚合行 + 前窗对比(全部来自现有聚合通道
// 可产出结构;TopN 列表跨源归并后为前 10,占比为下界,判据据此保守设计)。
type DiagnoseInput struct {
	WindowSec int64 `json:"window_sec"` // 当前窗口时长(秒;≤0 视为缺失,频率判据降级)
	Total     int64 `json:"total"`      // 当前窗总请求数

	TopIPs      []logquery.TopNItem        `json:"top_ips,omitempty"`      // client_ip 维度 TopN
	TopUAs      []logquery.TopNItem        `json:"top_uas,omitempty"`      // user_agent 维度 TopN
	TopURIs     []logquery.TopNItem        `json:"top_uris,omitempty"`     // 可选:URI 维度 TopN(缺失时爆破 URI 集中度判据跳过)
	StatusCodes []logquery.TopNItem        `json:"status_codes,omitempty"` // status 维度分布
	Actions     []logquery.TopNItem        `json:"actions,omitempty"`      // WAF 动作维度分布
	Buckets     []logquery.AggregateBucket `json:"buckets,omitempty"`      // 时间分桶(随聚合行透传,当前评分未加权)

	Prev *PrevWindow `json:"prev,omitempty"` // 前窗对比;nil 或 Total≤0 = 前窗无数据(降级)
}

// DiagnoseSource Top 攻击源(占比 = 该 IP 请求数 / 当前窗 Total)。
type DiagnoseSource struct {
	IP    string  `json:"ip"`
	Count int64   `json:"count"`
	Share float64 `json:"share"`
}

// DiagnoseResult 诊断结论(风险分/等级/类型/措施/Top 攻击源 + 降级标注)。
type DiagnoseResult struct {
	RiskScore      int              `json:"risk_score"`                // 0-100
	RiskLevel      string           `json:"risk_level"`                // none/low/medium/high
	AttackType     string           `json:"attack_type"`               // normal/cc_flood/crawler/brute_force/normal_burst
	Measures       []string         `json:"measures"`                  // 措施文案(≥3 条,含具体值插值)
	TopSources     []DiagnoseSource `json:"top_sources"`               // Top 攻击源 IP(默认前 5,含占比)
	Degraded       bool             `json:"degraded"`                  // 判据缺失降级标注
	DegradedReason string           `json:"degraded_reason,omitempty"` // 降级原因(空=未降级)
	// SurgeMultiplier 突增倍数(前窗可用时 >0;Total 倍数与 Top IP 倍数取大,
	// 与风险分突增分量同源;0=前窗无数据未计算,见 Degraded)。
	SurgeMultiplier float64 `json:"surge_multiplier"`
}

// assess 单次判定的中间上下文(输入派生指标集中计算一次)。
type assess struct {
	cfg   *Config
	input *DiagnoseInput

	ips, uas, uris []logquery.TopNItem // 按请求数降序(输入的克隆,不改写调用方数据)
	total          int64
	windowSec      float64

	topIP     logquery.TopNItem
	hasTopIP  bool
	topURI    logquery.TopNItem
	hasTopURI bool

	topRate    float64 // Top1 IP 请求速率(次/秒)
	totalRate  float64 // 整体请求速率(次/秒)
	surge      float64 // 突增倍数(Total 突增与 Top IP 突增取大)
	surgeKnown bool    // 前窗数据可用

	topIPShare      float64
	topUAShare      float64
	suspiciousShare float64
	suspiciousUA    logquery.TopNItem
	hasSuspiciousUA bool
	fourXR, fiveXR  float64 // 4xx / 5xx 占比
	actionR         float64 // 攻击动作占比
	topURIShare     float64
}

// Evaluate 诊断入口(纯函数:不改写输入、无 I/O;nil 输入返回安全空结论)。
func Evaluate(input *DiagnoseInput) *DiagnoseResult {
	if input == nil {
		return &DiagnoseResult{
			RiskLevel:  RiskLevelNone,
			AttackType: AttackTypeNormal,
			Measures:   []string{"诊断输入为空,未执行判定;请确认聚合查询已返回结果"},
			TopSources: []DiagnoseSource{},
		}
	}
	cfg := &DefaultConfig
	a := buildAssess(input, cfg)
	res := &DiagnoseResult{AttackType: AttackTypeNormal, RiskLevel: RiskLevelNone}

	// 降级标注:前窗无数据 → 突增判据缺失,降级为绝对量判定;窗口缺失 → 频率判据失效。
	var reasons []string
	if !a.surgeKnown {
		reasons = append(reasons, "前窗无数据,突增判据缺失,已降级为当前窗绝对量判定")
	}
	if a.input.WindowSec <= 0 {
		reasons = append(reasons, "窗口时长缺失,单 IP 频率判据失效,仅按占比与突增判定")
	}
	if len(reasons) > 0 {
		res.Degraded = true
		res.DegradedReason = strings.Join(reasons, ";")
	}

	// 风险分:四分量加权(AC1)。
	anomaly := math.Max(a.topUAShare, a.suspiciousShare)
	attackR := math.Max(a.fiveXR, a.actionR)
	score := (float64(cfg.Weights.TopIPRate)*scoreFromBands(a.topRate, cfg.TopIPRateBands) +
		float64(cfg.Weights.Surge)*scoreFromBands(a.surge, cfg.SurgeBands) +
		float64(cfg.Weights.UAAnomaly)*scoreFromBands(anomaly, cfg.AnomalyBands) +
		float64(cfg.Weights.AttackRatio)*scoreFromBands(attackR, cfg.AttackRatioBands)) / 100
	res.RiskScore = clampScore(int(math.Round(score)))

	// 攻击类型(AC3):爆破 → CC → 爬虫 → 正常高流量 → 正常。
	res.AttackType = classifyType(a)
	// 大促不误报:正常高流量封顶"低风险"。
	res.RiskLevel = levelFor(res.RiskScore)
	if res.AttackType == AttackTypeNormalBurst && levelRank(res.RiskLevel) > levelRank(RiskLevelLow) {
		res.RiskLevel = RiskLevelLow
	}
	// 突增倍数随结论透出(前窗可用时 >0;与风险分突增分量同源,防两处口径漂移)。
	if a.surgeKnown {
		res.SurgeMultiplier = a.surge
	}

	res.TopSources = buildTopSources(a.ips, a.total, cfg.TopSourcesLimit)
	res.Measures = buildMeasures(a, res.AttackType, res.RiskLevel, res.RiskScore)
	return res
}

// buildAssess 输入派生指标一次算齐(输入切片克隆排序,不改写调用方数据)。
func buildAssess(input *DiagnoseInput, cfg *Config) *assess {
	a := &assess{cfg: cfg, input: input, total: input.Total, windowSec: float64(input.WindowSec)}
	a.ips = sortByCount(input.TopIPs)
	a.uas = sortByCount(input.TopUAs)
	a.uris = sortByCount(input.TopURIs)

	a.topIP, a.hasTopIP = first(a.ips)
	a.topURI, a.hasTopURI = first(a.uris)

	if input.WindowSec > 0 {
		a.topRate = float64(a.topIP.Count) / a.windowSec
		a.totalRate = float64(a.total) / a.windowSec
	}

	// 突增:Total 倍数与 Top IP 倍数取大(前窗 Top IP 缺失时仅用 Total)。
	if prev := input.Prev; prev != nil && prev.Total > 0 {
		a.surgeKnown = true
		a.surge = float64(a.total) / float64(prev.Total)
		if prev.TopIPCount > 0 && a.hasTopIP {
			if s := float64(a.topIP.Count) / float64(prev.TopIPCount); s > a.surge {
				a.surge = s
			}
		}
	}

	a.topIPShare = ratio(a.topIP.Count, a.total)
	topUA, _ := first(a.uas)
	a.topUAShare = ratio(topUA.Count, a.total)
	suspiciousCount, topSusUA, hasSusUA := suspiciousUASum(a.uas, cfg)
	a.suspiciousShare = ratio(suspiciousCount, a.total)
	a.suspiciousUA, a.hasSuspiciousUA = topSusUA, hasSusUA
	a.fourXR, a.fiveXR = statusRatios(input.StatusCodes, a.total)
	a.actionR = actionRatio(input.Actions, a.total, cfg)
	a.topURIShare = ratio(a.topURI.Count, a.total)
	return a
}

// classifyType 攻击类型判定(优先级:爆破 → CC → 爬虫 → 正常高流量 → 正常)。
// 判据草案见 task/proposal:窗口缺失时频率类判据自然失效,占比类判据(爬虫)仍可用。
func classifyType(a *assess) string {
	cfg := a.cfg
	// 接口爆破:单 IP 高频 + 4xx 高(+ URI 集中;URI 未聚合时跳过该判据)。
	if a.topRate >= cfg.SuspiciousIPRate && a.fourXR >= cfg.BruteForce4xxRatio &&
		(!a.hasTopURI || a.topURIShare >= cfg.BruteForceURIShare) {
		return AttackTypeBruteForce
	}
	// CC 刷量:高速 + 突增 + 单 IP 集中(来源分散不判,防大促误报);
	// 或绝对量极速(降级路径:前窗缺失/无突增的持续刷)。
	ccBySurge := a.topRate >= cfg.HighIPRate && a.surgeKnown &&
		a.surge >= cfg.CCSurgeMultiplier && a.topIPShare >= cfg.CCMinIPShare
	if ccBySurge || a.topRate >= cfg.CriticalIPRate {
		return AttackTypeCCFlood
	}
	// 恶意爬虫:UA 异常(可疑占比或 Top1 集中度)+ 4xx 高 / 高请求量。
	if (a.suspiciousShare >= cfg.CrawlerUAShare || a.topUAShare >= cfg.CrawlerUATopShare) &&
		(a.fourXR >= cfg.Crawler4xxRatio || a.totalRate >= cfg.CrawlerTotalRate) {
		return AttackTypeCrawler
	}
	// 正常高流量(大促):突增但来源分散、UA 多样、无攻击动作、5xx 低。
	if a.surgeKnown && a.surge >= cfg.NormalBurstSurge &&
		a.topIPShare < cfg.NormalBurstTopIPShare &&
		a.topUAShare < cfg.NormalBurstTopUAShare && len(a.uas) >= cfg.NormalBurstMinUAs &&
		a.actionR < cfg.NormalBurstActionRatio && a.fiveXR < cfg.NormalBurst5xxRatio {
		return AttackTypeNormalBurst
	}
	return AttackTypeNormal
}

// buildMeasures 措施映射(AC4):按判定输出 ≥3 条带具体值(IP/请求数/限频/
// 占比)的文案,由判定数据插值生成,非纯静态。
func buildMeasures(a *assess, attackType, level string, score int) []string {
	measures := make([]string, 0, 6)
	label := LevelLabel(level)

	// 1. 结论句
	if isAttack(attackType) {
		measures = append(measures, fmt.Sprintf("判定疑似 %s(风险分 %d/100,%s),建议按以下措施处置", AttackTypeLabel(attackType), score, label))
	} else {
		measures = append(measures, fmt.Sprintf("窗口 %.0f 秒共 %d 次请求,未发现明显攻击特征(风险分 %d/100,%s),建议保持观察", a.windowSec, a.total, score, label))
	}
	// 2. 封禁 Top 攻击源(攻击类判定时)
	if isAttack(attackType) && a.hasTopIP {
		measures = append(measures, fmt.Sprintf("封禁 Top 攻击源 IP:%s(窗口内 %d 次,占比 %.1f%%),加入 WAF 黑名单",
			joinTopIPs(a.ips, 3), a.topIP.Count, a.topIPShare*100))
	}
	// 3. CC 防护/限流(建议限频 = 当前最高单 IP 速率 2 倍换算到分钟)
	if attackType == AttackTypeCCFlood || attackType == AttackTypeBruteForce {
		perMin := int(math.Ceil(a.topRate*2)) * 60
		measures = append(measures, fmt.Sprintf("开启 CC 防护/频次限流:建议单 IP 限频 ≤%d 次/分钟(当前最高单 IP %.1f 次/秒)", perMin, a.topRate))
	}
	// 4. UA 黑名单(爬虫判定,或存在可疑 UA)
	ua, hasUA := a.suspiciousUA, a.hasSuspiciousUA
	if !hasUA && attackType == AttackTypeCrawler && len(a.uas) > 0 {
		ua, hasUA = a.uas[0], true
	}
	if hasUA {
		measures = append(measures, fmt.Sprintf("将异常 UA「%s」(窗口内 %d 次,占比 %.1f%%)加入 UA 黑名单",
			ua.Name, ua.Count, ratio(ua.Count, a.total)*100))
	}
	// 5. 爆破路径防护
	if attackType == AttackTypeBruteForce {
		if a.hasTopURI {
			measures = append(measures, fmt.Sprintf("对高频 4xx 接口限流/封禁(4xx 占比 %.1f%%,Top URI「%s」占 %.1f%%)", a.fourXR*100, a.topURI.Name, a.topURIShare*100))
		} else {
			measures = append(measures, fmt.Sprintf("对高频 4xx 接口限流/封禁(4xx 占比 %.1f%%,URI 维度未聚合,建议按 Top 接口排查)", a.fourXR*100))
		}
	}
	// 6. 大促观察
	if attackType == AttackTypeNormalBurst {
		measures = append(measures, fmt.Sprintf("流量呈活动/大促特征(较前窗突增 %.1f 倍,来源分散、UA 多样),建议保持观察并确认业务预期", a.surge))
	}
	// 7. 防护基线(整体速率插值)
	measures = append(measures, fmt.Sprintf("当前整体速率 %.1f 次/秒,建议配置 CC 防护基线(单 IP 限频)预防突发刷量", a.totalRate))
	// 8. Top 来源关注 / 无数据提示
	if a.hasTopIP {
		measures = append(measures, fmt.Sprintf("关注最高请求源 IP %s(窗口内 %d 次,占比 %.1f%%),如持续增长建议配置限频",
			a.topIP.Name, a.topIP.Count, a.topIPShare*100))
	} else {
		measures = append(measures, "窗口内无请求数据,建议确认日志采集与接入状态")
	}
	return measures
}

// ---- 判定辅助 ----

// levelFor 风险分 → 等级(临界分含下界:20/40/70,见 Config)。
func levelFor(score int) string {
	switch {
	case score >= DefaultConfig.LevelHighMin:
		return RiskLevelHigh
	case score >= DefaultConfig.LevelMediumMin:
		return RiskLevelMedium
	case score >= DefaultConfig.LevelLowMin:
		return RiskLevelLow
	default:
		return RiskLevelNone
	}
}

// levelRank 等级序(封顶比较用)。
func levelRank(level string) int {
	switch level {
	case RiskLevelLow:
		return 1
	case RiskLevelMedium:
		return 2
	case RiskLevelHigh:
		return 3
	default:
		return 0
	}
}

// clampScore 风险分封顶(权重配置失当时兜底)。
func clampScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// scoreFromBands 分段线性评分:升序档位间插值;首档之下线性放大,最高档之上封顶。
func scoreFromBands(v float64, bands []ScoreBand) float64 {
	if len(bands) == 0 || v < 0 {
		return 0
	}
	if v <= bands[0].Min {
		if bands[0].Min <= 0 {
			return float64(bands[0].Score)
		}
		return v / bands[0].Min * float64(bands[0].Score)
	}
	for i := 1; i < len(bands); i++ {
		if v <= bands[i].Min {
			prev, cur := bands[i-1], bands[i]
			span := cur.Min - prev.Min
			if span <= 0 {
				return float64(cur.Score)
			}
			return float64(prev.Score) + (v-prev.Min)/span*float64(cur.Score-prev.Score)
		}
	}
	return float64(bands[len(bands)-1].Score)
}

// isAttack 是否攻击类判定(措施映射用)。
func isAttack(t string) bool {
	return t == AttackTypeCCFlood || t == AttackTypeCrawler || t == AttackTypeBruteForce
}

// ---- 特征辅助 ----

// sortByCount 按请求数降序(克隆后排序,不改写调用方切片)。
func sortByCount(items []logquery.TopNItem) []logquery.TopNItem {
	if len(items) == 0 {
		return nil
	}
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(x, y logquery.TopNItem) int {
		return cmp.Compare(y.Count, x.Count)
	})
	return out
}

// first 首条(降序后即 Top1)。
func first(items []logquery.TopNItem) (logquery.TopNItem, bool) {
	if len(items) == 0 {
		return logquery.TopNItem{}, false
	}
	return items[0], true
}

// ratio 占比(total≤0 返回 0,防除零)。
func ratio(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total)
}

// statusRatios 4xx/5xx 占比(状态码名称按整数值归类,非数值忽略;
// TopN 截断使占比为下界,判据保守设计)。
func statusRatios(items []logquery.TopNItem, total int64) (fourXR, fiveXR float64) {
	var four, five int64
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
	return ratio(four, total), ratio(five, total)
}

// actionRatio 攻击动作占比(动作取值在配置字典内的请求占总数比)。
func actionRatio(items []logquery.TopNItem, total int64, cfg *Config) float64 {
	var attacked int64
	for _, it := range items {
		if isAttackAction(it.Name, cfg) {
			attacked += it.Count
		}
	}
	return ratio(attacked, total)
}

// isAttackAction 动作取值是否攻击动作(小写精确匹配配置字典)。
func isAttackAction(name string, cfg *Config) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	return slices.Contains(cfg.AttackActionValues, n)
}

// suspiciousUASum 可疑 UA 命中请求数与 Top1 可疑 UA(降序列表首个命中)。
func suspiciousUASum(uas []logquery.TopNItem, cfg *Config) (sum int64, top logquery.TopNItem, ok bool) {
	for _, it := range uas {
		if isSuspiciousUA(it.Name, cfg) {
			sum += it.Count
			if !ok {
				top, ok = it, true
			}
		}
	}
	return sum, top, ok
}

// isSuspiciousUA UA 是否可疑(小写包含任一关键字)。
func isSuspiciousUA(name string, cfg *Config) bool {
	n := strings.ToLower(name)
	for _, kw := range cfg.SuspiciousUAKeywords {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

// joinTopIPs Top N 攻击源拼接("ip(count 次)" 顿号连接)。
func joinTopIPs(ips []logquery.TopNItem, n int) string {
	parts := make([]string, 0, n)
	for i, it := range ips {
		if i >= n {
			break
		}
		parts = append(parts, fmt.Sprintf("%s(%d 次)", it.Name, it.Count))
	}
	return strings.Join(parts, "、")
}

// buildTopSources Top 攻击源(前 limit 条,含占比)。
func buildTopSources(ips []logquery.TopNItem, total int64, limit int) []DiagnoseSource {
	if limit <= 0 {
		limit = DefaultConfig.TopSourcesLimit
	}
	out := make([]DiagnoseSource, 0, min(len(ips), limit))
	for i, it := range ips {
		if i >= limit {
			break
		}
		out = append(out, DiagnoseSource{IP: it.Name, Count: it.Count, Share: ratio(it.Count, total)})
	}
	return out
}

// ---- 展示标签(供 API 层/前端复用)----

// LevelLabel 风险等级中文标签。
func LevelLabel(level string) string {
	switch level {
	case RiskLevelLow:
		return "低风险"
	case RiskLevelMedium:
		return "中风险"
	case RiskLevelHigh:
		return "高风险"
	default:
		return "无风险"
	}
}

// AttackTypeLabel 攻击类型中文标签。
func AttackTypeLabel(t string) string {
	switch t {
	case AttackTypeCCFlood:
		return "CC 刷量"
	case AttackTypeCrawler:
		return "恶意爬虫"
	case AttackTypeBruteForce:
		return "接口爆破"
	case AttackTypeNormalBurst:
		return "正常高流量"
	default:
		return "正常流量"
	}
}
