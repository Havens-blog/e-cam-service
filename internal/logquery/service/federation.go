// Package service 联邦日志查询编排(Phase 1.4,plan.md §4.4)。
//
// 职责:解析租户内活跃云账号 -> 按 (cloud, logType) 构造 provider ->
// 并发 fan-out -> 归并排序 -> 截断。单源失败/超时隔离,不阻塞其他源
// (响应携带 per-source 状态,UI 逐源展示)。
package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"golang.org/x/sync/errgroup"
)

// 联邦查询上限(ADR D4:窗口内每源上限+截断标记,不支持深翻页)。
const (
	// DefaultPerSourceLimit 单源默认拉取上限。
	DefaultPerSourceLimit = 100
	// MaxPerSourceLimit 单源上限硬顶(防大窗口拖垮联邦)。
	MaxPerSourceLimit = 500
	// FederationTimeout 整个联邦查询超时;超时返回已完成部分+标记。
	FederationTimeout = 30 * time.Second
)

// SearchRequest 联邦查询请求(租户由 web 层会话解析)。
type SearchRequest struct {
	LogType    logquery.LogType       // 必填
	StartTime  int64                  // Unix ms(含)
	EndTime    int64                  // Unix ms(含)
	Query      string                 // 可选,原生检索式透传
	Clouds     []domain.CloudProvider // 可选,限定云
	AccountIDs []int64                // 可选,限定云账号
	Resources  []string               // 可选,限定资源(域名/LB ID)
	Limit      int                    // 每日志源上限(默认 100,硬顶 500;ADR D4)
}

// SourceOutcome 单源查询结果状态(UI 逐源展示;失败不静默)。
type SourceOutcome struct {
	Cloud       domain.CloudProvider `json:"cloud"`
	AccountID   string               `json:"account_id"`
	AccountName string               `json:"account_name"`
	Count       int                  `json:"count"` // 该源返回条数
	Error       string               `json:"error"` // 失败原因(空=成功)
	DurationMs  int64                `json:"duration_ms"`
}

// SearchResponse 联邦查询响应。
type SearchResponse struct {
	LogType   string              `json:"log_type"`
	Total     int                 `json:"total"`
	Truncated bool                `json:"truncated"` // 任一源触顶或联邦超时
	Entries   []logquery.LogEntry `json:"entries"`   // 时间倒序
	Sources   []SourceOutcome     `json:"sources"`   // per-source 状态
}

// AggregateRequest 联邦聚合请求(字段与 SearchRequest 对齐,无 limit——
// 聚合下推云引擎,只回传分桶/分组,不受采样上限约束)。
type AggregateRequest struct {
	LogType    logquery.LogType
	StartTime  int64
	EndTime    int64
	Query      string
	Clouds     []domain.CloudProvider
	AccountIDs []int64
	Resources  []string
}

// AggregateSourceOutcome 单源聚合状态(不支持聚合的源显式标注,不静默缺失)。
type AggregateSourceOutcome struct {
	Cloud       domain.CloudProvider `json:"cloud"`
	AccountID   string               `json:"account_id"`
	AccountName string               `json:"account_name"`
	Total       int64                `json:"total"` // 该源窗口精确总数
	Error       string               `json:"error"` // 失败/不支持原因(空=成功)
	DurationMs  int64                `json:"duration_ms"`
}

// AggregateResponse 联邦聚合响应(跨源归并:分桶求和、TopN 求和取前 10)。
type AggregateResponse struct {
	LogType string                        `json:"log_type"`
	Total   int64                         `json:"total"`   // 窗口精确总数(全源求和)
	Buckets []logquery.AggregateBucket    `json:"buckets"` // 时间分桶(真实分布)
	TopN    []logquery.TopNItem           `json:"topn"`    // TopN(域名/规则按类型)
	Sources []AggregateSourceOutcome      `json:"sources"`
}

// Aggregate 窗口内真实聚合入口(plan.md §8):分桶下推各云引擎,百万级行
// 不过网关;单源失败/不支持/超时隔离,聚合结果由已完成源合成。
func (s *FederationService) Aggregate(ctx context.Context, tenantID int64, req AggregateRequest) (*AggregateResponse, error) {
	if !logquery.IsValidLogType(req.LogType) {
		return nil, fmt.Errorf("invalid log type: %s", req.LogType)
	}
	if req.EndTime <= req.StartTime {
		return nil, fmt.Errorf("invalid time window: end %d <= start %d", req.EndTime, req.StartTime)
	}
	bucketSec := logquery.PickBucketSec(req.StartTime, req.EndTime)
	accounts, err := s.activeAccounts(ctx, tenantID, req.Clouds, req.AccountIDs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, FederationTimeout)
	defer cancel()

	var (
		mu       sync.Mutex
		buckets  = make(map[int64]int64)
		topn     = make(map[string]int64)
		outcomes []AggregateSourceOutcome
	)
	g, gctx := errgroup.WithContext(ctx)
	for i := range accounts {
		acc := accounts[i]
		creator, err := logquery.GetProvider(acc.Provider, req.LogType)
		if err != nil {
			mu.Lock()
			outcomes = append(outcomes, AggregateSourceOutcome{
				Cloud: acc.Provider, AccountID: fmt.Sprintf("%d", acc.ID),
				AccountName: acc.Name, Error: "provider not registered",
			})
			mu.Unlock()
			continue
		}
		g.Go(func() error {
			start := time.Now()
			p, err := creator(&acc)
			var result *logquery.AggregateResult
			// 可选能力:未实现 Aggregator 的 provider(如 S3 文件类)显式标注
			if agg, ok := p.(logquery.Aggregator); err == nil && ok {
				result, err = agg.Aggregate(gctx, &acc, logquery.AggregateParams{
					StartTime: req.StartTime,
					EndTime:   req.EndTime,
					Query:     req.Query,
					BucketSec: bucketSec,
					Resources: req.Resources,
				})
			} else if err == nil {
				err = errAggregateUnsupported
			}
			oc := AggregateSourceOutcome{
				Cloud:       acc.Provider,
				AccountID:   fmt.Sprintf("%d", acc.ID),
				AccountName: acc.Name,
				DurationMs:  time.Since(start).Milliseconds(),
			}
			if err != nil {
				if gctx.Err() == nil { // 联邦级超时不计入单源失败
					oc.Error = err.Error()
				}
			} else {
				oc.Total = result.Total
			}
			mu.Lock()
			outcomes = append(outcomes, oc)
			if result != nil {
				for _, b := range result.Buckets {
					buckets[b.Timestamp] += b.Count
				}
				for _, t := range result.TopN {
					topn[t.Name] += t.Count
				}
			}
			mu.Unlock()
			return nil // 单源失败隔离,永不中断 errgroup
		})
	}
	_ = g.Wait()

	resp := &AggregateResponse{
		LogType: string(req.LogType),
		Sources: outcomes,
	}
	for ts, c := range buckets {
		resp.Buckets = append(resp.Buckets, logquery.AggregateBucket{Timestamp: ts, Count: c})
		resp.Total += c
	}
	sort.Slice(resp.Buckets, func(i, j int) bool {
		return resp.Buckets[i].Timestamp < resp.Buckets[j].Timestamp
	})
	for name, c := range topn {
		resp.TopN = append(resp.TopN, logquery.TopNItem{Name: name, Count: c})
	}
	sort.Slice(resp.TopN, func(i, j int) bool {
		return resp.TopN[i].Count > resp.TopN[j].Count
	})
	const federatedTopN = 10
	if len(resp.TopN) > federatedTopN {
		resp.TopN = resp.TopN[:federatedTopN]
	}
	return resp, nil
}

// errAggregateUnsupported provider 未实现聚合能力(显式标注用)。
var errAggregateUnsupported = fmt.Errorf("aggregate not supported for this provider")

// AccountSource 云账号源(仓储窄接口:联邦层只需按过滤条件列账号;
// accountrepo.CloudAccountRepository 结构性满足,凭证解密在仓储读取路径完成)。
type AccountSource interface {
	List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error)
}

// FederationService 联邦查询编排服务。
type FederationService struct {
	accounts AccountSource
	logger   *elog.Component
}

// NewFederationService 创建联邦查询服务。
func NewFederationService(accounts AccountSource, logger *elog.Component) *FederationService {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &FederationService{accounts: accounts, logger: logger}
}

// Search 联邦查询入口。
func (s *FederationService) Search(ctx context.Context, tenantID int64, req SearchRequest) (*SearchResponse, error) {
	if !logquery.IsValidLogType(req.LogType) {
		return nil, fmt.Errorf("invalid log type: %s", req.LogType)
	}
	if req.EndTime <= req.StartTime {
		return nil, fmt.Errorf("invalid time window: end %d <= start %d", req.EndTime, req.StartTime)
	}
	perSource := req.Limit
	if perSource <= 0 {
		perSource = DefaultPerSourceLimit
	}
	if perSource > MaxPerSourceLimit {
		perSource = MaxPerSourceLimit
	}

	accounts, err := s.activeAccounts(ctx, tenantID, req.Clouds, req.AccountIDs)
	if err != nil {
		return nil, err
	}

	// ctx 超时:超时返回已完成部分(调用方聚合,不因超时报错)
	ctx, cancel := context.WithTimeout(ctx, FederationTimeout)
	defer cancel()

	var (
		mu       sync.Mutex
		entries  []logquery.LogEntry
		outcomes []SourceOutcome
		truncate bool
	)
	g, gctx := errgroup.WithContext(ctx)
	for i := range accounts {
		acc := accounts[i]
		creator, err := logquery.GetProvider(acc.Provider, req.LogType)
		if err != nil {
			// 该云未注册此类型 provider(如腾讯预留):记入状态,跳过
			mu.Lock()
			outcomes = append(outcomes, SourceOutcome{
				Cloud: acc.Provider, AccountID: fmt.Sprintf("%d", acc.ID),
				AccountName: acc.Name, Error: "provider not registered",
			})
			mu.Unlock()
			continue
		}
		g.Go(func() error {
			start := time.Now()
			p, err := creator(&acc)
			var got []logquery.LogEntry
			if err == nil {
				got, err = p.Search(gctx, &acc, logquery.SearchParams{
					StartTime: req.StartTime,
					EndTime:   req.EndTime,
					Query:     req.Query,
					Limit:     perSource,
					Resources: req.Resources,
				})
			}
			oc := SourceOutcome{
				Cloud:       acc.Provider,
				AccountID:   fmt.Sprintf("%d", acc.ID),
				AccountName: acc.Name,
				DurationMs:  time.Since(start).Milliseconds(),
			}
			if err != nil {
				if gctx.Err() == nil { // 联邦级超时不计入单源失败
					oc.Error = err.Error()
				}
			} else {
				oc.Count = len(got)
			}
			mu.Lock()
			outcomes = append(outcomes, oc)
			entries = append(entries, got...)
			mu.Unlock()
			return nil // 单源失败隔离,永不中断 errgroup
		})
	}
	_ = g.Wait()
	// 截断标记:联邦超时,或归并结果触达联邦硬顶(limit 为每日志源上限,
	// provider 归并后总量可合理超过单源上限,不能以 Count>=perSource 判截断)
	if ctx.Err() != nil {
		truncate = true
	}
	// 跨源归并:时间倒序
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GetTimestamp() > entries[j].GetTimestamp()
	})
	// 联邦级硬顶 1000:多账号多源归并后防响应过大(每源上限由 provider 侧执行)
	const federatedCap = 1000
	if len(entries) > federatedCap {
		entries = entries[:federatedCap]
		truncate = true
	}
	return &SearchResponse{
		LogType:   string(req.LogType),
		Total:     len(entries),
		Truncated: truncate,
		Entries:   entries,
		Sources:   outcomes,
	}, nil
}

// ListSources 日志源清单(按云账号 fan-out,含 Enabled 状态)。
func (s *FederationService) ListSources(ctx context.Context, tenantID int64, logType logquery.LogType, clouds []domain.CloudProvider, accountIDs []int64) ([]logquery.LogSource, error) {
	if !logquery.IsValidLogType(logType) {
		return nil, fmt.Errorf("invalid log type: %s", logType)
	}
	accounts, err := s.activeAccounts(ctx, tenantID, clouds, accountIDs)
	if err != nil {
		return nil, err
	}
	var (
		mu  sync.Mutex
		out []logquery.LogSource
		wg  sync.WaitGroup
	)
	for i := range accounts {
		acc := accounts[i]
		creator, err := logquery.GetProvider(acc.Provider, logType)
		if err != nil {
			continue // 未注册云静默跳过(sources 只列可用)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := creator(&acc)
			if err != nil {
				s.logger.Warn("[logquery] create provider failed",
					elog.String("cloud", string(acc.Provider)),
					elog.Int64("account", acc.ID), elog.FieldErr(err))
				return
			}
			sources, err := p.ListLogSources(ctx, &acc)
			if err != nil {
				s.logger.Warn("[logquery] list sources failed",
					elog.String("cloud", string(acc.Provider)),
					elog.Int64("account", acc.ID), elog.FieldErr(err))
				return
			}
			mu.Lock()
			out = append(out, sources...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}

// activeAccounts 租户内活跃账号(按云/账号过滤)。
func (s *FederationService) activeAccounts(ctx context.Context, tenantID int64, clouds []domain.CloudProvider, accountIDs []int64) ([]domain.CloudAccount, error) {
	filter := domain.CloudAccountFilter{
		Status:   domain.CloudAccountStatusActive,
		TenantID: tenantID,
	}
	if len(clouds) == 1 {
		filter.Provider = clouds[0]
	}
	accounts, _, err := s.accounts.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("logquery: list active accounts: %w", err)
	}
	if len(clouds) > 1 {
		allowed := make(map[domain.CloudProvider]bool, len(clouds))
		for _, c := range clouds {
			allowed[c] = true
		}
		filtered := accounts[:0]
		for _, a := range accounts {
			if allowed[a.Provider] {
				filtered = append(filtered, a)
			}
		}
		accounts = filtered
	}
	if len(accountIDs) > 0 {
		want := make(map[int64]bool, len(accountIDs))
		for _, id := range accountIDs {
			want[id] = true
		}
		filtered := accounts[:0]
		for _, a := range accounts {
			if want[a.ID] {
				filtered = append(filtered, a)
			}
		}
		accounts = filtered
	}
	return accounts, nil
}
