// CDN 缓存分析编排(proposal:cdn-cache-analysis,任务 2)。
//
// 镜像 WAF 诊断编排骨架:当前窗按维度组并发聚合(cache_hit 计数分布 /
// cache_hit×sum_bytes 字节口径 / status 状态码 / host×nonhit_count 域名未命中 /
// url×nonhit_count URI 未命中,当前窗维度聚合次数 N=5 ≤ 上限),前一等长
// 窗口仅 1 帧(cache_hit 分布对比),拼装 cdncache.CacheAnalyzeInput 调任务 1
// 纯函数规则引擎判定。手动触发;单次分析扫过的窗口帧 ≤2(当前窗 + 前窗),
// 不随明细/聚合查询自动执行;窗口上限 24h,预估扫描量超限默认拦截并提示
// 确认;独立 feature flag 默认关(LOGQUERY_CACHE_ANALYZE_ENABLED),只新增
// 不改既有日志查询路径;summary 为 AI 解读占位(恒空串,本接口不引入 LLM)。
//
// 口径说明:
//   - 命中归类:命中 = hit+partial,未命中 = miss+error,未知("-")计入分母
//     不计入两侧(与任务 1 引擎 NoteClassification 一致);
//   - 单维度聚合无 cache_hit×status 二维交叉,可缓存档剔除沿用任务 1 的不依赖
//     交叉近似(max(cache_hit=error, 4xx+5xx)),与引擎口径一致不矛盾;
//   - 域名/URI 未命中靠 nonhit_count 指标(单维度聚合内按 cache_hit 归一
//     miss+error 过滤,不做 host×uri 交叉);域名排行取未命中 Top N 域名
//     (Count=该域名全请求,Value=未命中数),URI TOP 天然 miss 预筛
//     (order by 未命中数),查询串归一由引擎完成;
//   - cache_hit 归一沿用明细/任务 1 共用单一映射源 logquery.NormalizeCacheHit:
//     聚合组键为源原始值,编排层逐组归一并同态合并,不另起归一路径;
//   - AI 外发隐私边界(本期 summary 恒空,不外发):后置接入时仅限聚合统计,
//     URI 级数据须剥离查询串与 PII。
package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/cdncache"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"golang.org/x/sync/errgroup"
)

// 缓存分析约束(成本护栏;proposal Non-Functional Requirements)。
const (
	// cacheAnalyzeFlagEnv 独立 feature flag(默认关;异常时关闭即回滚)。
	cacheAnalyzeFlagEnv = "LOGQUERY_CACHE_ANALYZE_ENABLED"
	// cacheAnalyzeMaxWindowMs 窗口时长上限 24h。
	cacheAnalyzeMaxWindowMs int64 = 24 * 60 * 60 * 1000
	// cacheAnalyzeConfirmWindowMs 预估扫描量确认阈值:窗口超过 6h 默认拦截。
	cacheAnalyzeConfirmWindowMs int64 = 6 * 60 * 60 * 1000
	// cacheAnalyzeDimGroups 当前窗维度聚合组数(最早 proposal 定 N ≤ 5;后续
	// "下钻补齐"新增域名级字节命中率(host×sum_bytes + host×nonhit_bytes)提至
	// 7 组,均为单维度 group-by 扫描,仍受手动触发 + 窗口/扫描量护栏约束)。
	cacheAnalyzeDimGroups = 7
	// cacheAnalyzeFrames 单次分析窗口帧数(当前窗 + 前窗,恒 2)。
	cacheAnalyzeFrames = 2
)

// cacheAnalyzeEnabled feature flag 读取(默认关;"1/true/on" 开启)。
func cacheAnalyzeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(cacheAnalyzeFlagEnv))) {
	case "1", "true", "on":
		return true
	default:
		return false
	}
}

// CacheAnalyzeRequest CDN 缓存分析请求(字段与 DiagnoseRequest 对齐;窗口为
// 当前窗,前一等长窗口由服务层推导,不可指定。Confirm:预估扫描量超限拦截
// 后的人工确认,默认 false=拦截)。
type CacheAnalyzeRequest struct {
	LogType    logquery.LogType       // 仅 cdn(slb/waf 明确不支持)
	StartTime  int64                  // Unix ms(含)
	EndTime    int64                  // Unix ms(含)
	Query      string                 // 可选,原生检索式透传
	Clouds     []domain.CloudProvider // 可选,限定云
	AccountIDs []int64                // 可选,限定云账号
	Resources  []string               // 可选,限定资源(域名)
	Filters    []logquery.FieldFilter // 可选,结构化字段筛选(AND 叠加)
	Confirm    bool                   // 预估扫描量超限确认(默认拦截)
}

// CacheAnalyzeResponse CDN 缓存分析响应(引擎结论 + 口径输入明细 + per-source
// 状态;Result 内含双命中率/域名排行/未命中 URI TOP/状态码分布/结构化优化项)。
type CacheAnalyzeResponse struct {
	LogType    string `json:"log_type"`
	WindowSec  int64  `json:"window_sec"`  // 当前窗口时长(秒)
	Total      int64  `json:"total"`       // 当前窗总请求数(全源精确求和)
	TotalBytes int64  `json:"total_bytes"` // 全请求字节总量(cache_hit×sum_bytes 求和;已覆盖源口径)

	// Result 规则引擎判定(双命中率/健康档位/域名排行/未命中 URI TOP/状态码
	// 分布/gap 归因/结构化优化项/前窗趋势/口径标注 Notes;summary 在响应根)。
	Result *cdncache.CacheAnalyzeResult `json:"result"`

	// 前一等长窗口 cache_hit 分布(趋势对比输入;Prev=nil=前窗不可用,见
	// PrevError;Prev.Total=0=前窗确实无数据 —— 两者均降级为当前窗绝对量
	// 判定,不报错,见 Result.Prev.Available)。
	Prev        *cdncache.PrevCacheWindow `json:"prev,omitempty"`
	PrevError   string                    `json:"prev_error,omitempty"`   // 前窗不可用原因(空=成功/无数据)
	PrevSources []AggregateSourceOutcome  `json:"prev_sources,omitempty"` // 前窗 per-source 状态

	// Sources 当前窗 per-source 状态(跨维度合并:同源任一维度失败即标注);
	// DimensionNotes 非主维度缺失/覆盖范围说明(缺失源在此标注,不白屏)。
	Sources        []AggregateSourceOutcome `json:"sources"`
	DimensionNotes string                   `json:"dimension_notes,omitempty"`
	// AggregateFrames 本次分析扫过的窗口帧数(当前窗 + 前窗,恒 2;成本标注)。
	AggregateFrames int `json:"aggregate_frames"`
	// Summary AI 解读占位(后置:本接口不引入 LLM,恒空串;外发仅限聚合统计)。
	Summary string `json:"summary"`

	// Cached/CacheStale 结果缓存标注(语义同 SearchResponse)。
	Cached     bool `json:"cached"`
	CacheStale bool `json:"cache_stale"`
}

// CacheAnalyze CDN 缓存分析入口(手动触发;同参复用诊断级 SWR 缓存)。
func (s *FederationService) CacheAnalyze(ctx context.Context, tenantID int64, req CacheAnalyzeRequest) (*CacheAnalyzeResponse, error) {
	// 独立 feature flag 默认关:关闭时明确报错(AC:开关关闭时接口明确报错)。
	if !cacheAnalyzeEnabled() {
		return nil, fmt.Errorf("cache-analyze is disabled: feature flag %s is not enabled", cacheAnalyzeFlagEnv)
	}
	// 硬性约束:仅 CDN 类型开放(SLB/WAF 明确报错,不静默)。
	if req.LogType != logquery.LogTypeCDN {
		return nil, fmt.Errorf("cache-analyze only supports cdn log type, got: %s", req.LogType)
	}
	if req.EndTime <= req.StartTime {
		return nil, fmt.Errorf("invalid time window: end %d <= start %d", req.EndTime, req.StartTime)
	}
	// 窗口上限 24h(手动触发 + 帧数受限的成本护栏)。
	span := req.EndTime - req.StartTime
	if span > cacheAnalyzeMaxWindowMs {
		return nil, fmt.Errorf("time window %s exceeds max 24h", time.Duration(span)*time.Millisecond)
	}
	// 执行前预估扫描量:窗口 > 6h 默认拦截,提示确认后放行(Confirm=true)。
	if span > cacheAnalyzeConfirmWindowMs && !req.Confirm {
		return nil, fmt.Errorf("estimated scan volume is high: window %s × %d frames × %d dimension groups, blocked by default; confirm with confirm=true to proceed",
			time.Duration(span)*time.Millisecond, cacheAnalyzeFrames, cacheAnalyzeDimGroups)
	}
	for _, f := range req.Filters {
		if !logquery.IsValidFieldFilterOp(f.Op) {
			return nil, fmt.Errorf("invalid filter op: %s", f.Op)
		}
		if f.Field == "" || f.Value == "" {
			return nil, fmt.Errorf("incomplete field filter: %+v", f)
		}
	}
	start := time.Now()
	resp, cached, stale, err := cachedCall(ctx, s.cache, "cache_analyze", tenantID, req, func(cctx context.Context) (*CacheAnalyzeResponse, error) {
		return s.cacheAnalyzeUncached(cctx, tenantID, req)
	})
	if err != nil {
		return nil, err
	}
	resp.Cached, resp.CacheStale = cached, stale
	s.logger.Info("[logquery] cache analyze done",
		elog.String("log_type", string(req.LogType)),
		elog.String("grade", resp.Result.Grade),
		elog.String("degraded_notes", strconv.FormatBool(resp.DimensionNotes != "")),
		elog.String("cache_hit", strconv.FormatBool(cached)),
		elog.Int64("duration_ms", time.Since(start).Milliseconds()))
	return resp, nil
}

// cacheAnalyzeUncached 真实分析编排(无缓存路径):当前窗 5 个维度组 + 前窗
// 1 帧全部并发。主帧(cache_hit 计数分布,承载 Total/命中率口径)失败才整体
// 报错;其余维度组失败仅记入 DimensionNotes(该维度判据退化);前窗失败降级
// 不报错。
func (s *FederationService) cacheAnalyzeUncached(ctx context.Context, tenantID int64, req CacheAnalyzeRequest) (*CacheAnalyzeResponse, error) {
	span := req.EndTime - req.StartTime
	aggReq := func(start, end int64, dimension, metric string) AggregateRequest {
		return AggregateRequest{
			LogType: req.LogType, StartTime: start, EndTime: end,
			Query: req.Query, Clouds: req.Clouds, AccountIDs: req.AccountIDs,
			Resources: req.Resources, Filters: req.Filters,
			Dimension: dimension, Metric: metric,
		}
	}
	prevStart := req.StartTime - span // 前一等长窗口:[end-2*span, end-span)

	var (
		curHit, curBytes, curStatus, curHost, curURI, curHostBytes, curHostHitBytes, prev                         *AggregateResponse
		curHitErr, curBytesErr, curStatusErr, curHostErr, curURIErr, curHostBytesErr, curHostHitBytesErr, prevErr error
	)
	var g errgroup.Group
	g.Go(func() error { // 主帧:cache_hit 计数分布(命中率口径 + 精确总数)
		curHit, curHitErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, "cache_hit", "count"))
		return nil // 维度失败不中断并发组,统一在下方裁决
	})
	g.Go(func() error { // 字节口径:cache_hit × sum_bytes(全请求/可缓存双字节)
		curBytes, curBytesErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, "cache_hit", "sum_bytes"))
		return nil
	})
	g.Go(func() error { // 状态码分布(4xx/5xx 占比;状态×cache_hit 交叉为后续增强)
		curStatus, curStatusErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, "status", "count"))
		return nil
	})
	g.Go(func() error { // 域名未命中(host × nonhit_count:Count=域名全请求,Value=未命中数)
		curHost, curHostErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, "host", "nonhit_count"))
		return nil
	})
	g.Go(func() error { // 域名总字节(host × sum_bytes:Value=该域名全请求字节)
		curHostBytes, curHostBytesErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, "host", "sum_bytes"))
		return nil
	})
	g.Go(func() error { // 域名未命中字节(host × nonhit_bytes:Value=该域名 miss+error 字节)
		curHostHitBytes, curHostHitBytesErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, "host", "nonhit_bytes"))
		return nil
	})
	g.Go(func() error { // URI 未命中(uri_host × nonhit_count,组合键 域名|路径;miss 预筛)
		curURI, curURIErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, "uri_host", "nonhit_count"))
		return nil
	})
	g.Go(func() error { // 前一等长窗口:cache_hit 计数分布 1 帧(趋势对比)
		prev, prevErr = s.Aggregate(ctx, tenantID, aggReq(prevStart, req.StartTime, "cache_hit", "count"))
		return nil
	})
	_ = g.Wait()

	if curHitErr != nil || curHit == nil {
		return nil, fmt.Errorf("cache-analyze: current window cache_hit aggregate: %w", curHitErr)
	}

	// cache_hit 分布组键为源原始值(TCP_HIT / hit_info 复合值等),经单一映射
	// 源归一并同态合并(hit/miss/partial/error/-),与明细层/任务 1 同源。
	cacheHitDist := normalizeCacheHitDist(curHit.TopN)

	// 字节口径:cache_hit × sum_bytes(Value=该状态字节;组数 ≤5 无截断,
	// 求和即全请求字节总量)。
	byteDist := normalizeCacheHitDist(topNOf(curBytes))
	var totalBytes int64
	for _, it := range byteDist {
		totalBytes += int64(it.Value)
	}

	// 域名/URI 未命中分布(nonhit_count:Count=全请求,Value=未命中数;
	// 失败/缺失由 notes 标注,引擎对空输入自动降级)。
	hostStats := hostStatsFromMissTop(topNOf(curHost))
	// 域名级字节命中率下钻:host×sum_bytes(总字节)与 host×nonhit_bytes(未命中
	// 字节)同域名对齐;命中字节 = 总字节 − 未命中字节(miss+error),与域名级
	// 请求口径一致(partial/未知计入命中)。任一字节帧缺失时域名字节列置空。
	attachHostBytes(hostStats, hostBytesByHost(curHostBytes), hostMissBytesByHost(curHostHitBytes))
	uriMisses := uriMissesFromMissTop(topNOf(curURI))

	// 前窗对比值:cache_hit 分布(组键同样归一)。源级失败在聚合层已被隔离
	// (不返回调用级错误),只记在 Sources[].Error —— 前窗全源失败时按
	// "前窗不可用"降级(Prev=nil + PrevError),真无数据时保留空前窗,两者
	// 均降级为当前窗绝对量判定,不报错。
	var prevWin *cdncache.PrevCacheWindow
	prevFail := errText(prevErr)
	if prevErr == nil && prev != nil {
		switch {
		case prev.Total > 0:
			prevWin = &cdncache.PrevCacheWindow{Total: prev.Total, CacheHitDist: normalizeCacheHitDist(prev.TopN)}
		case len(prev.Sources) > 0 && allSourcesFailed(prev.Sources):
			prevFail = joinSourceErrors(prev.Sources)
		default:
			prevWin = &cdncache.PrevCacheWindow{} // 前窗确实无数据(源正常但窗口为空)
		}
	}

	res := cdncache.Evaluate(&cdncache.CacheAnalyzeInput{
		TotalRequests: curHit.Total,
		CacheHitDist:  cacheHitDist,
		CacheHitBytes: byteDist,
		TotalBytes:    totalBytes,
		StatusDist:    topNOf(curStatus),
		HostStats:     hostStats,
		URIMisses:     uriMisses,
		Prev:          prevWin,
	})

	var prevSources []AggregateSourceOutcome
	if prev != nil {
		prevSources = prev.Sources
	}
	resp := &CacheAnalyzeResponse{
		LogType:         string(req.LogType),
		WindowSec:       span / 1000,
		Total:           curHit.Total,
		TotalBytes:      totalBytes,
		Result:          res,
		Prev:            prevWin,
		PrevError:       prevFail,
		PrevSources:     prevSources,
		Sources:         mergeAggregateSources(curHit, curBytes, curStatus, curHost, curURI, curHostBytes, curHostHitBytes),
		DimensionNotes:  strings.Join(cacheDimensionNotes(curBytes, curBytesErr, curStatus, curStatusErr, curHost, curHostErr, curURI, curURIErr, curHostBytes, curHostBytesErr, curHostHitBytes, curHostHitBytesErr, curHit.Sources), ";"),
		AggregateFrames: cacheAnalyzeFrames, // 当前窗 + 前窗(成本标注;前窗失败也计一次扫描尝试)
	}
	// Summary 恒空:本接口不引入 LLM(硬规则),字段为 AI 解读后置占位。
	return resp, nil
}

// normalizeCacheHitDist cache_hit 聚合组键归一(单一映射源):组键为源原始值
// (TCP_HIT / hit_info 复合值 / EdgeCacheStatus 等),逐组经
// logquery.NormalizeCacheHit 归一为 hit/miss/partial/error/- 枚举并同态合并
// 计数与指标值 —— 与明细层共用同一归一函数,不另起归一路径。保持首次出现
// 顺序,输出确定。
func normalizeCacheHitDist(items []logquery.TopNItem) []logquery.TopNItem {
	if len(items) == 0 {
		return nil
	}
	merged := make(map[string]*logquery.TopNItem, len(items))
	order := make([]string, 0, len(items))
	for _, it := range items {
		state := logquery.NormalizeCacheHit(it.Name)
		g, ok := merged[state]
		if !ok {
			g = &logquery.TopNItem{Name: state}
			merged[state] = g
			order = append(order, state)
		}
		g.Count += it.Count
		g.Value += it.Value
	}
	out := make([]logquery.TopNItem, 0, len(merged))
	for _, state := range order {
		out = append(out, *merged[state])
	}
	return out
}

// hostStatsFromMissTop host×nonhit_count 聚合 → 域名×cache_hit 交叉(近似):
// Count=该域名全请求,Value=未命中数(miss+error 合并,命中归类口径)。
// Hit = 全请求 − 未命中(partial/未知计入命中,域名级近似口径;Error 并入
// Miss,域名级可缓存档不剔除 error,口径偏保守,由引擎 Notes 标注)。
func hostStatsFromMissTop(items []logquery.TopNItem) []cdncache.HostCacheStat {
	out := make([]cdncache.HostCacheStat, 0, len(items))
	for _, it := range items {
		if it.Name == "" || it.Count <= 0 {
			continue
		}
		miss := int64(it.Value)
		if miss < 0 {
			miss = 0
		}
		if miss > it.Count {
			miss = it.Count
		}
		out = append(out, cdncache.HostCacheStat{Host: it.Name, Hit: it.Count - miss, Miss: miss})
	}
	return out
}

// hostBytesByHost host×sum_bytes → 域名→全请求字节映射(Value=字节;同主机重名
// 取和,确定性)。
func hostBytesByHost(resp *AggregateResponse) map[string]int64 {
	out := make(map[string]int64)
	for _, it := range topNOf(resp) {
		if it.Name == "" {
			continue
		}
		out[it.Name] += int64(it.Value)
	}
	return out
}

// hostMissBytesByHost host×nonhit_bytes → 域名→未命中(miss+error)字节映射。
func hostMissBytesByHost(resp *AggregateResponse) map[string]int64 {
	out := make(map[string]int64)
	for _, it := range topNOf(resp) {
		if it.Name == "" {
			continue
		}
		out[it.Name] += int64(it.Value)
	}
	return out
}

// attachHostBytes 域名级字节命中率下钻:按 host 对齐总字节与未命中字节,命中
// 字节 = 总字节 − 未命中字节(与域名级请求口径同判:partial/未知计入命中)。
// 字节帧缺失/不完整时对应宿主 Stat.ByteHitRate 分母 ≤0,由引擎输出置空
// (Type 层=可用/不可用),不静默伪造。
func attachHostBytes(stats []cdncache.HostCacheStat, totalBytes, missBytes map[string]int64) {
	for i := range stats {
		tb, mb := totalBytes[stats[i].Host], missBytes[stats[i].Host]
		stats[i].TotalBytes = tb
		stats[i].MissBytes = mb
		if mb < 0 {
			stats[i].MissBytes = 0
		}
		if stats[i].MissBytes > tb {
			stats[i].MissBytes = tb
		}
	}
}

// uriMissesFromMissTop uri_host×nonhit_count 聚合 → URI 未命中分布:组键为
// 组合键「域名|路径」(DCDN concat)或完整 URL(离线转存 RequestURL),拆分后
// 归属域名与路径;查询串归一由引擎完成(剥离/排序/变体归并)。
func uriMissesFromMissTop(items []logquery.TopNItem) []cdncache.URIMissItem {
	out := make([]cdncache.URIMissItem, 0, len(items))
	for _, it := range items {
		miss := int64(it.Value)
		if it.Name == "" || miss <= 0 {
			continue
		}
		host, path := splitURIComposite(it.Name)
		out = append(out, cdncache.URIMissItem{URI: path, Host: host, Miss: miss})
	}
	return out
}

// splitURIComposite 解析 URI 聚合组键 → (归属域名, 路径+查询串):
//   - 「域名|路径」(DCDN uri_host concat):按首个 "|" 拆分(域名不含 "|");
//   - scheme://host/path(离线转存 RequestURL):url.Parse 提取 host;
//   - 纯路径形态(源未开 uri_host,透传降级):域名置空、原样返回。
func splitURIComposite(raw string) (host, path string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "/"
	}
	if i := strings.IndexByte(s, '|'); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		p := u.EscapedPath()
		if p == "" {
			p = "/"
		}
		if u.RawQuery != "" {
			p += "?" + u.RawQuery
		}
		return u.Hostname(), p
	}
	return "", s
}

// cacheDimensionNotes 缓存分析维度缺失说明(状态码/字节口径/域名未命中/
// URI 未命中;维度调用失败、全源失败或部分源不可下推时提示)+ 命中率覆盖
// 范围(缺失源在此标注,不白屏)。UI 据此标注判据完整性。
func cacheDimensionNotes(curBytes *AggregateResponse, curBytesErr error,
	curStatus *AggregateResponse, curStatusErr error,
	curHost *AggregateResponse, curHostErr error,
	curURI *AggregateResponse, curURIErr error,
	curHostBytes *AggregateResponse, curHostBytesErr error,
	curHostHitBytes *AggregateResponse, curHostHitBytesErr error,
	hitSources []AggregateSourceOutcome) []string {
	var out []string
	// 非主维度组失败/缺失(域名排行与 URI 集中度自动降级,不整卡失败)。
	for _, e := range []struct {
		label string
		resp  *AggregateResponse
		err   error
	}{
		{"字节口径(sum_bytes)", curBytes, curBytesErr},
		{"status 状态码", curStatus, curStatusErr},
		{"域名未命中(host×nonhit)", curHost, curHostErr},
		{"URI 未命中(uri_host×nonhit,高基数或列缺失时降级)", curURI, curURIErr},
		{"域名总字节(host×sum_bytes)", curHostBytes, curHostBytesErr},
		{"域名未命中字节(host×nonhit_bytes)", curHostHitBytes, curHostHitBytesErr},
	} {
		switch {
		case e.err != nil:
			out = append(out, fmt.Sprintf("%s 聚合失败:%s", e.label, e.err))
		case e.resp == nil:
			out = append(out, fmt.Sprintf("%s 聚合失败:无结果", e.label))
		case e.resp.TopNSkip != "":
			out = append(out, fmt.Sprintf("%s 部分源缺失/不支持:%s", e.label, e.resp.TopNSkip))
		case len(e.resp.TopN) == 0 && len(e.resp.Sources) > 0 && allSourcesFailed(e.resp.Sources):
			out = append(out, fmt.Sprintf("%s 聚合失败:%s", e.label, joinSourceErrors(e.resp.Sources)))
		}
	}
	// 覆盖范围:命中率以 cache_hit 分布已覆盖源为准(缺失源显式标注)。
	if len(hitSources) > 0 {
		covered, missing := 0, make([]string, 0, len(hitSources))
		for _, oc := range hitSources {
			if oc.Error == "" {
				covered++
				continue
			}
			missing = append(missing, fmt.Sprintf("%s(%s)", oc.AccountName, oc.AccountID))
		}
		if len(missing) == 0 {
			out = append(out, fmt.Sprintf("命中率为已覆盖源口径:覆盖 %d/%d 源", covered, len(hitSources)))
		} else {
			out = append(out, fmt.Sprintf("命中率为已覆盖源口径:覆盖 %d/%d 源(缺失:%s,缺失源未映射 cache_hit 或聚合失败,不参与计算)",
				covered, len(hitSources), strings.Join(missing, ",")))
		}
	}
	return out
}
