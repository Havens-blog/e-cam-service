// WAF 流量诊断编排(proposal:网站被刷检测,任务 2)。
//
// 复用联邦聚合通道:当前窗按维度并发聚合(client_ip / user_agent / uri / status /
// action),前一等长窗口仅 1 帧(client_ip 维度,一次聚合同时产出精确 Total
// 与 Top client_ip —— Total 由分桶求和,不单独多跑 count 扫描),拼装
// diagnose.DiagnoseInput 后调纯函数规则引擎判定。手动触发;单次诊断扫过的
// 窗口帧 ≤2(当前窗 + 前窗),不随明细/聚合查询自动执行;per-source 状态
// 与降级标注随响应透出;summary 为 AI 解读后置占位(恒空串,不引入 LLM)。
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/diagnose"
	"github.com/Havens-blog/e-cam-service/internal/logquery/llm"
	"github.com/Havens-blog/e-cloudx-sdk/logquery"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"
	"golang.org/x/sync/errgroup"
)

// 诊断聚合维度(/types 字段字典键;均为 WAF 实测可聚合维度)。
const (
	diagDimClientIP  = "client_ip"
	diagDimUserAgent = "user_agent"
	diagDimStatus    = "status"
	diagDimAction    = "action"
	diagDimURI       = "uri"
)

// DiagnoseRequest WAF 流量诊断请求(字段与 AggregateRequest 对齐;窗口为
// 当前窗,前一等长窗口由服务层推导,不可指定)。
type DiagnoseRequest struct {
	LogType    logquery.LogType       // 仅 waf(slb/cdn 明确不支持)
	StartTime  int64                  // Unix ms(含)
	EndTime    int64                  // Unix ms(含)
	Query      string                 // 可选,原生检索式透传
	Clouds     []domain.CloudProvider // 可选,限定云
	AccountIDs []int64                // 可选,限定云账号
	Resources  []string               // 可选,限定资源(域名)
	Filters    []logquery.FieldFilter // 可选,结构化字段筛选(AND 叠加)
}

// DiagnoseResponse WAF 流量诊断响应(判定结论 + 判据输入明细 + per-source 状态)。
type DiagnoseResponse struct {
	LogType   string `json:"log_type"`
	WindowSec int64  `json:"window_sec"` // 当前窗口时长(秒)
	Total     int64  `json:"total"`      // 当前窗总请求数(全源精确求和)

	// 当前窗聚合明细(判定输入透传,诊断卡展示趋势/分布用)。
	Buckets     []logquery.AggregateBucket `json:"buckets"`      // 时间分桶
	TopIPs      []logquery.TopNItem        `json:"top_ips"`      // client_ip TopN
	TopUAs      []logquery.TopNItem        `json:"top_uas"`      // user_agent TopN
	TopURIs     []logquery.TopNItem        `json:"top_uris"`     // 请求量高的 URI/URL TopN
	StatusCodes []logquery.TopNItem        `json:"status_codes"` // 状态码分布
	Actions     []logquery.TopNItem        `json:"actions"`      // WAF 动作分布

	// 前一等长窗口对比(突增判据)。Prev=nil=前窗不可用(调用失败或全源失败,
	// 见 PrevError);Prev.Total=0=前窗确实无数据 —— 两者均降级为当前窗
	// 绝对量判定,不报错,见 Result.Degraded。
	Prev        *diagnose.PrevWindow     `json:"prev,omitempty"`
	PrevError   string                   `json:"prev_error,omitempty"`   // 前窗不可用原因(空=成功/无数据)
	PrevSources []AggregateSourceOutcome `json:"prev_sources,omitempty"` // 前窗 per-source 状态

	// Result 规则引擎判定(风险分/等级/疑似攻击类型/措施/Top 攻击源/降级标注/
	// 突增倍数;summary 字段在响应根上,不入结果体)。
	Result *diagnose.DiagnoseResult `json:"result"`

	// Sources 当前窗 per-source 状态(跨维度合并:同源任一维度失败即标注,
	// error 为去重并集;已聚合源照常判定)。
	Sources []AggregateSourceOutcome `json:"sources"`
	// DimensionNotes 非主维度缺失说明(某源维度聚合失败/不可下推;仅该维度
	// 判据退化,不影响整体判定)。
	DimensionNotes string `json:"dimension_notes,omitempty"`
	// AggregateFrames 本次诊断扫过的窗口帧数(当前窗 + 前窗,恒 2;成本标注)。
	AggregateFrames int `json:"aggregate_frames"`
	// Summary AI 解读(后置:模型接入前恒空串;本接口不引入 LLM)。
	Summary string `json:"summary"`

	// Cached/CacheStale 结果缓存标注(语义同 SearchResponse)。
	Cached     bool `json:"cached"`
	CacheStale bool `json:"cache_stale"`
}

// Diagnose WAF 流量诊断入口(手动触发;同参复用诊断级 SWR 缓存)。
func (s *FederationService) Diagnose(ctx context.Context, tenantID int64, req DiagnoseRequest) (*DiagnoseResponse, error) {
	// 硬性约束:诊断仅 WAF 类型开放(SLB/CDN 明确报错,不静默)。
	if req.LogType != logquery.LogTypeWAF {
		return nil, fmt.Errorf("diagnose only supports waf log type, got: %s", req.LogType)
	}
	if req.EndTime <= req.StartTime {
		return nil, fmt.Errorf("invalid time window: end %d <= start %d", req.EndTime, req.StartTime)
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
	resp, cached, stale, err := cachedCall(ctx, s.cache, "diagnose", tenantID, req, func(cctx context.Context) (*DiagnoseResponse, error) {
		return s.diagnoseUncached(cctx, tenantID, req)
	})
	if err != nil {
		return nil, err
	}
	resp.Cached, resp.CacheStale = cached, stale
	s.logger.Info("[logquery] diagnose done",
		elog.String("log_type", string(req.LogType)),
		elog.String("risk_level", resp.Result.RiskLevel),
		elog.String("attack_type", resp.Result.AttackType),
		elog.Int("risk_score", resp.Result.RiskScore),
		elog.String("degraded", strconv.FormatBool(resp.Result.Degraded)),
		elog.String("cache_hit", strconv.FormatBool(cached)),
		elog.Int64("duration_ms", time.Since(start).Milliseconds()))
	return resp, nil
}

// diagnoseUncached 真实诊断编排(无缓存路径):当前窗 5 维 + 前窗 1 帧全部
// 并发。主帧(client_ip,承载 Total/分桶/Top IP)失败才整体报错;其余维度
// 失败仅记入 DimensionNotes(该维度判据退化);前窗失败降级不报错。
func (s *FederationService) diagnoseUncached(ctx context.Context, tenantID int64, req DiagnoseRequest) (*DiagnoseResponse, error) {
	span := req.EndTime - req.StartTime
	aggReq := func(start, end int64, dimension string) AggregateRequest {
		return AggregateRequest{
			LogType: req.LogType, StartTime: start, EndTime: end,
			Query: req.Query, Clouds: req.Clouds, AccountIDs: req.AccountIDs,
			Resources: req.Resources, Filters: req.Filters,
			Dimension: dimension, Metric: "count",
		}
	}
	prevStart := req.StartTime - span // 前一等长窗口:[end-2*span, end-span)

	var (
		curIP, curUA, curURI, curStatus, curAction, prev                   *AggregateResponse
		curIPErr, curUAErr, curURIErr, curStatusErr, curActionErr, prevErr error
	)
	var g errgroup.Group
	g.Go(func() error {
		curIP, curIPErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, diagDimClientIP))
		return nil // 维度失败不中断并发组,统一在下方裁决
	})
	g.Go(func() error {
		curUA, curUAErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, diagDimUserAgent))
		return nil
	})
	g.Go(func() error {
		curURI, curURIErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, diagDimURI))
		return nil
	})
	g.Go(func() error {
		curStatus, curStatusErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, diagDimStatus))
		return nil
	})
	g.Go(func() error {
		curAction, curActionErr = s.Aggregate(ctx, tenantID, aggReq(req.StartTime, req.EndTime, diagDimAction))
		return nil
	})
	g.Go(func() error {
		prev, prevErr = s.Aggregate(ctx, tenantID, aggReq(prevStart, req.StartTime, diagDimClientIP))
		return nil
	})
	_ = g.Wait()

	if curIPErr != nil || curIP == nil {
		return nil, fmt.Errorf("diagnose: current window aggregate: %w", curIPErr)
	}

	// 前窗对比值(Top IP 取各分组最大 Count,不依赖联邦排序)。注意:源级失败
	// 在聚合层已被隔离(不返回调用级错误),只记在 Sources[].Error —— 前窗
	// 全源失败时按"前窗不可用"降级(Prev=nil + PrevError),真无数据时保留
	// 空前窗(Prev={0,0}),两者均降级为当前窗绝对量判定,不报错。
	var prevWin *diagnose.PrevWindow
	prevFail := errText(prevErr)
	if prevErr == nil && prev != nil {
		switch {
		case prev.Total > 0:
			var topIP int64
			for _, it := range prev.TopN {
				if it.Count > topIP {
					topIP = it.Count
				}
			}
			prevWin = &diagnose.PrevWindow{Total: prev.Total, TopIPCount: topIP}
		case len(prev.Sources) > 0 && allSourcesFailed(prev.Sources):
			prevFail = joinSourceErrors(prev.Sources)
		default:
			prevWin = &diagnose.PrevWindow{} // 前窗确实无数据(源正常但窗口为空)
		}
	}

	res := diagnose.Evaluate(&diagnose.DiagnoseInput{
		WindowSec:   span / 1000,
		Total:       curIP.Total,
		TopIPs:      curIP.TopN,
		TopUAs:      topNOf(curUA),
		StatusCodes: topNOf(curStatus),
		Actions:     topNOf(curAction),
		Buckets:     curIP.Buckets,
		Prev:        prevWin,
	})

	var prevSources []AggregateSourceOutcome
	if prev != nil {
		prevSources = prev.Sources
	}
	resp := &DiagnoseResponse{
		LogType:         string(req.LogType),
		WindowSec:       span / 1000,
		Total:           curIP.Total,
		Buckets:         curIP.Buckets,
		TopIPs:          curIP.TopN,
		TopUAs:          topNOf(curUA),
		TopURIs:         topNOf(curURI),
		StatusCodes:     topNOf(curStatus),
		Actions:         topNOf(curAction),
		Prev:            prevWin,
		PrevError:       prevFail,
		PrevSources:     prevSources,
		Result:          res,
		Sources:         mergeAggregateSources(curIP, curUA, curURI, curStatus, curAction),
		DimensionNotes:  strings.Join(diagDimensionNotes(curUA, curUAErr, curURI, curURIErr, curStatus, curStatusErr, curAction, curActionErr), ";"),
		AggregateFrames: 2, // 当前窗 + 前窗(成本标注;前窗失败也计一次扫描尝试)
	}
	// AI 解读(后置落地):网关配置时尽力而为 —— 只喂聚合指标(隐私边界),超时/
	// 失败降级空串(前端不渲染),绝不阻塞或污染诊断主流程。
	if llm.Enabled() {
		var prevTotal *int64
		if prevWin != nil {
			prevTotal = &prevWin.Total
		}
		sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if text, err := llm.SummarizeDiagnose(sctx, llm.FromDiagnose(res, curIP.Total, span/1000, prevTotal,
			topStrs(topNOf(curURI), 5), topStrs(topNOf(curStatus), 6), topStrs(topNOf(curAction), 6))); err == nil && text != "" {
			resp.Summary = text
		}
		cancel()
	}
	return resp, nil
}

// topStrs TopN 条目 → "name(count)" 摘要串(供 AI 输入;数量已截断)。
func topStrs(items []logquery.TopNItem, n int) []string {
	if len(items) > n {
		items = items[:n]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("%s(%d)", it.Name, it.Count))
	}
	return out
}

// topNOf nil 安全取 TopN(维度聚合失败 = 该维度缺失,判据自动退化)。
func topNOf(resp *AggregateResponse) []logquery.TopNItem {
	if resp == nil {
		return nil
	}
	return resp.TopN
}

// errText nil 安全错误文本(空=成功)。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// diagDimensionNotes 非主维度缺失说明(UA/状态码/动作;维度调用失败、全源
// 失败或部分源不可下推时提示,UI 据此标注判据完整性)。
func diagDimensionNotes(curUA *AggregateResponse, curUAErr error, curURI *AggregateResponse, curURIErr error, curStatus *AggregateResponse, curStatusErr error, curAction *AggregateResponse, curActionErr error) []string {
	type entry struct {
		dim  string
		resp *AggregateResponse
		err  error
	}
	entries := []entry{
		{dim: diagDimUserAgent, resp: curUA, err: curUAErr},
		{dim: diagDimURI, resp: curURI, err: curURIErr},
		{dim: diagDimStatus, resp: curStatus, err: curStatusErr},
		{dim: diagDimAction, resp: curAction, err: curActionErr},
	}
	var out []string
	for _, e := range entries {
		switch {
		case e.err != nil:
			out = append(out, fmt.Sprintf("%s 维度聚合失败:%s", e.dim, e.err))
		case e.resp == nil:
			out = append(out, fmt.Sprintf("%s 维度聚合失败:无结果", e.dim))
		case e.resp.TopNSkip != "":
			out = append(out, fmt.Sprintf("%s TopN 部分源缺失:%s", e.dim, e.resp.TopNSkip))
		case len(e.resp.TopN) == 0 && len(e.resp.Sources) > 0 && allSourcesFailed(e.resp.Sources):
			out = append(out, fmt.Sprintf("%s 维度聚合失败:%s", e.dim, joinSourceErrors(e.resp.Sources)))
		}
	}
	return out
}

// allSourcesFailed 该聚合响应是否全部源均失败(无任何成功源)。
func allSourcesFailed(sources []AggregateSourceOutcome) bool {
	if len(sources) == 0 {
		return false
	}
	for _, oc := range sources {
		if oc.Error == "" {
			return false
		}
	}
	return true
}

// joinSourceErrors 去重拼接源失败原因(前窗不可用/维度缺失标注用)。
func joinSourceErrors(sources []AggregateSourceOutcome) string {
	seen := make(map[string]bool, len(sources))
	parts := make([]string, 0, len(sources))
	for _, oc := range sources {
		if oc.Error == "" || seen[oc.Error] {
			continue
		}
		seen[oc.Error] = true
		parts = append(parts, oc.Error)
	}
	return strings.Join(parts, "; ")
}

// mergeAggregateSources 跨维度合并 per-source 状态(同云账号同源:任一维度
// 失败即标注;error 为去重并集,Total/耗时取各维度最大值)。
func mergeAggregateSources(resps ...*AggregateResponse) []AggregateSourceOutcome {
	index := make(map[string]int)
	var out []AggregateSourceOutcome
	for _, r := range resps {
		if r == nil {
			continue
		}
		for _, oc := range r.Sources {
			key := string(oc.Cloud) + "/" + oc.AccountID
			i, ok := index[key]
			if !ok {
				out = append(out, oc)
				index[key] = len(out) - 1
				continue
			}
			if oc.Total > out[i].Total {
				out[i].Total = oc.Total
			}
			if oc.DurationMs > out[i].DurationMs {
				out[i].DurationMs = oc.DurationMs
			}
			if oc.Error != "" && !strings.Contains(out[i].Error, oc.Error) {
				if out[i].Error != "" {
					out[i].Error += "; "
				}
				out[i].Error += oc.Error
			}
		}
	}
	return out
}
