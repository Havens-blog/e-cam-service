// 诊断规则集中配置:权重与阈值全部收敛在此,可校准。
// 锚定依据(proposal 实测):阿里 WAF 近 6h 全窗 8000 万行(约 3700 次/秒整体),
// 单 IP 频率阈值为保守首版,待真实 Top client_ip 聚合观察后调整。
package diagnose

// ScoreBand 分段评分带:分量值 v ∈ [Min, 下一档 Min) 区间内线性插值得分。
// 首档 {0,0} 之下按 v/Min 线性放大;超过最高档 Min 封顶为最高档分值。
type ScoreBand struct {
	Min   float64 // 分量值下界(升序)
	Score int     // 该下界对应的分量分(0-100)
}

// ScoreWeights 风险分四分量权重(百分比,建议合计 100;超出由封顶兜底)。
type ScoreWeights struct {
	TopIPRate   int // 单 IP 最高频率(次/秒)
	Surge       int // 前窗突增倍数(Total 与 Top IP 突增取大)
	UAAnomaly   int // UA 异常(Top UA 集中度与可疑 UA 占比取大)
	AttackRatio int // 5xx 占比与 WAF 攻击动作占比取大
}

// Config 诊断规则配置(权重 + 阈值 + 特征字典,集中可校准)。
type Config struct {
	// ---- 分量权重(合计 100)----
	Weights ScoreWeights

	// ---- 分量分段评分表(0-100,线性插值)----
	TopIPRateBands   []ScoreBand // 单 IP 请求速率(次/秒)→ 分量分
	SurgeBands       []ScoreBand // 突增倍数(≥前窗等长窗口)→ 分量分
	AnomalyBands     []ScoreBand // UA 异常度(0-1 占比)→ 分量分
	AttackRatioBands []ScoreBand // max(5xx 占比, 攻击动作占比)→ 分量分

	// ---- 风险等级临界分(含)----
	LevelLowMin    int // ≥ 低风险
	LevelMediumMin int // ≥ 中风险
	LevelHighMin   int // ≥ 高风险

	// ---- 单 IP 频率阈值(次/秒)----
	SuspiciousIPRate float64 // 可疑下限:进入爆破/爬虫判据
	HighIPRate       float64 // 高速:与突增同时满足判 CC
	CriticalIPRate   float64 // 极速:绝对量降级路径判 CC(前窗缺失/无突增也判)

	// ---- CC 刷量判据 ----
	CCSurgeMultiplier float64 // 突增路径所需倍数
	CCMinIPShare      float64 // 单 IP 占比下限(来源分散的高频边缘 IP 不判 CC,防大促误报)

	// ---- 接口爆破判据 ----
	BruteForce4xxRatio float64 // 4xx 占比下限
	BruteForceURIShare float64 // Top URI 集中度下限(TopURIs 未聚合时不要求该判据)

	// ---- 恶意爬虫判据 ----
	CrawlerUAShare    float64 // 可疑 UA(关键字命中)占比下限
	CrawlerUATopShare float64 // Top1 UA 集中度下限(与可疑占比满足其一)
	Crawler4xxRatio   float64 // 4xx 占比下限(与整体速率下限满足其一)
	CrawlerTotalRate  float64 // 整体请求速率下限(次/秒,高请求量替代 4xx)

	// ---- 正常高流量(大促)判据:全部满足才判,并封顶"低风险" ----
	NormalBurstSurge       float64 // 突增倍数下限
	NormalBurstTopIPShare  float64 // Top1 IP 占比上限(来源分散)
	NormalBurstTopUAShare  float64 // Top1 UA 占比上限(UA 多样)
	NormalBurstMinUAs      int     // TopN 内不同 UA 数下限
	NormalBurstActionRatio float64 // 攻击动作占比上限
	NormalBurst5xxRatio    float64 // 5xx 占比上限

	// ---- 特征字典 ----
	AttackActionValues   []string // WAF 动作中视为"攻击动作"的取值(小写精确匹配)
	SuspiciousUAKeywords []string // 可疑 UA 关键字(小写包含匹配;bot/spider 等误伤可接受,可校准)

	// ---- 输出 ----
	TopSourcesLimit int // Top 攻击源条数
}

// DefaultConfig 默认配置(阈值均为首版保守值,按真实流量校准后调整)。
var DefaultConfig = Config{
	Weights: ScoreWeights{
		TopIPRate:   40,
		Surge:       25,
		UAAnomaly:   15,
		AttackRatio: 20,
	},
	TopIPRateBands: []ScoreBand{
		{Min: 0, Score: 0}, {Min: 0.5, Score: 10}, {Min: 2, Score: 30},
		{Min: 5, Score: 50}, {Min: 20, Score: 75}, {Min: 100, Score: 100},
	},
	SurgeBands: []ScoreBand{
		{Min: 0, Score: 0}, {Min: 1, Score: 0}, {Min: 2, Score: 30},
		{Min: 5, Score: 60}, {Min: 10, Score: 85}, {Min: 30, Score: 100},
	},
	AnomalyBands: []ScoreBand{
		{Min: 0, Score: 0}, {Min: 0.3, Score: 20}, {Min: 0.5, Score: 45},
		{Min: 0.7, Score: 70}, {Min: 0.9, Score: 100},
	},
	AttackRatioBands: []ScoreBand{
		{Min: 0, Score: 0}, {Min: 0.02, Score: 15}, {Min: 0.05, Score: 35},
		{Min: 0.2, Score: 65}, {Min: 0.5, Score: 100},
	},
	LevelLowMin:    20,
	LevelMediumMin: 40,
	LevelHighMin:   70,

	SuspiciousIPRate: 5,
	HighIPRate:       20,
	CriticalIPRate:   200,

	CCSurgeMultiplier: 3,
	CCMinIPShare:      0.2,

	BruteForce4xxRatio: 0.5,
	BruteForceURIShare: 0.6,

	CrawlerUAShare:    0.3,
	CrawlerUATopShare: 0.7,
	Crawler4xxRatio:   0.4,
	CrawlerTotalRate:  50,

	NormalBurstSurge:       3,
	NormalBurstTopIPShare:  0.3,
	NormalBurstTopUAShare:  0.5,
	NormalBurstMinUAs:      3,
	NormalBurstActionRatio: 0.05,
	NormalBurst5xxRatio:    0.1,

	AttackActionValues:   []string{"block", "deny", "drop", "captcha", "js", "intercept"},
	SuspiciousUAKeywords: []string{"curl", "wget", "python-requests", "python-urllib", "scrapy", "go-http-client", "java/", "okhttp", "httpclient", "bot", "spider", "crawler"},

	TopSourcesLimit: 5,
}
