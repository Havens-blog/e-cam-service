package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/gotomicro/ego/core/elog"
)

// 默认忽略字段（每次同步都会变化的瞬态字段）
var defaultIgnoreFields = map[string]bool{
	"sync_time":   true,
	"update_time": true,
	"utime":       true,
}

// changeCountCacheTTL total 缓存有效期。
//
// 背景（实测 2026-10-31）：ecam_change_history 有 5,399,871 条记录，
// tenant 3 占 100%，`CountDocuments({tenant_id:3})` 实测 **4.9 秒**。
// 而审计页每次翻页都调一次 ListByAssetID（内部 Count），
// 实测每页均需 2.5 秒，翻页体验极差；GetSummary 又额外算一次 total，页面首屏更慢。
//
// 变更记录是历史数据，总量变化缓慢，TTL 内复用 total 完全可接受。
const changeCountCacheTTL = 60 * time.Second

// changeCountBudget 单次 Count 的时限。
//
// 实测（2026-10-31，ecam_change_history 5,399,871 条）：
//   - 无筛选 / asset_id 筛选（有索引）：2.9~4.9 秒
//   - field_name 筛选（无索引，全表扫）：>30 秒后 Mongo 主动断开连接
//
// 取 8 秒：覆盖有索引的正常情况并留余量，同时避免无索引查询把请求拖到
// Mongo 自己超时断连（那样会抛出 EOF 这类不可控错误）。
// 超时后由 ListByAssetID 走「列表照常返回、total=-1」的降级。
const changeCountBudget = 8 * time.Second

// changeSummaryBudget 变更统计接口的总时限。
//
// 实测单项聚合需 90 秒以上（见 GetSummary 注释），4 项串行远超 HTTP 超时。
// 限时后退化为「部分统计」：已算出的项正常返回，超时项跳过。
// 取值 5s：既能让页面快速出首屏，又给单次 Count（4.9s 且带缓存）留出余量。
const changeSummaryBudget = 5 * time.Second

// changeCountCacheItem total 缓存项。
type changeCountCacheItem struct {
	total    int64
	expireAt time.Time
}

// ChangeTracker 资产变更追踪器
type ChangeTracker struct {
	dao          dao.ChangeRecordDAO
	logger       *elog.Component
	ignoreFields map[string]bool

	// countCache 缓存 Count 结果，避免 540 万行全表 count 在每次翻页/首屏重复执行。
	// key 由 filter 的关键维度拼成（tenant + asset + field + 时间区间），
	// 保证不同筛选条件互不污染。
	countMu    sync.Mutex
	countCache map[string]changeCountCacheItem
}

// NewChangeTracker 创建变更追踪器
func NewChangeTracker(dao dao.ChangeRecordDAO, logger *elog.Component) *ChangeTracker {
	return &ChangeTracker{
		dao:          dao,
		logger:       logger,
		ignoreFields: defaultIgnoreFields,
		countCache:   make(map[string]changeCountCacheItem),
	}
}

// cachedCount 带缓存的 Count。
//
// 540 万行全表 count 实测 4.9 秒；审计页翻页时每页都会触发一次，
// 是「每页 2.5 秒」的根因。此处在 TTL 内复用上一次结果。
//
// 注意：缓存的是「同一筛选条件」下的总数。审计数据为历史记录，
// 新增是追加式的，60 秒延迟对分页器展示无实质影响；
// 若需立即反映，可在 TTL 过期后自然刷新。
func (t *ChangeTracker) cachedCount(ctx context.Context, filter *domain.ChangeFilter) (int64, error) {
	key := countCacheKey(filter)

	t.countMu.Lock()
	if item, ok := t.countCache[key]; ok && time.Now().Before(item.expireAt) {
		t.countMu.Unlock()
		return item.total, nil
	}
	t.countMu.Unlock()

	// 缓存未命中：执行真实 count（可能耗时数秒乃至数十秒），不持锁，避免阻塞其他请求。
	//
	// 加独立时限：按 field_name 筛选时该字段无索引，CountDocuments 在 540 万行上
	// 实测 >30 秒后被 Mongo 断开（接口 500）。限时后调用方能拿到明确的超时错误，
	// 从而走「列表照常返回、total 未知」的降级路径，而不是干等 Mongo 自己断连。
	countCtx, cancel := context.WithTimeout(ctx, changeCountBudget)
	defer cancel()

	total, err := t.dao.Count(countCtx, *filter)
	if err != nil {
		return 0, err
	}

	t.countMu.Lock()
	t.countCache[key] = changeCountCacheItem{total: total, expireAt: time.Now().Add(changeCountCacheTTL)}
	t.countMu.Unlock()
	return total, nil
}

// countCacheKey 由筛选条件的关键维度拼出缓存键。
func countCacheKey(f *domain.ChangeFilter) string {
	var b strings.Builder
	b.WriteString(strconv.FormatInt(f.TenantID, 10))
	b.WriteByte('|')
	b.WriteString(f.AssetID)
	b.WriteByte('|')
	b.WriteString(f.FieldName)
	b.WriteByte('|')
	if f.StartTime != nil {
		b.WriteString(strconv.FormatInt(*f.StartTime, 10))
	}
	b.WriteByte('|')
	if f.EndTime != nil {
		b.WriteString(strconv.FormatInt(*f.EndTime, 10))
	}
	return b.String()
}

// TrackChanges 对比新旧实例属性，记录变更
// 返回变更字段数量
func (t *ChangeTracker) TrackChanges(ctx context.Context, meta domain.ChangeMetadata, oldAttrs, newAttrs map[string]interface{}) (int, error) {
	now := time.Now().UnixMilli()
	var records []domain.ChangeRecord

	// 检测修改和新增的字段
	for key, newVal := range newAttrs {
		if t.ignoreFields[key] {
			continue
		}
		oldVal, exists := oldAttrs[key]
		if !exists {
			// 新增字段
			records = append(records, domain.ChangeRecord{
				AssetID:      meta.AssetID,
				AssetName:    meta.AssetName,
				ModelUID:     meta.ModelUID,
				TenantID:     meta.TenantID,
				AccountID:    meta.AccountID,
				Provider:     meta.Provider,
				Region:       meta.Region,
				FieldName:    key,
				OldValue:     "",
				NewValue:     toJSON(newVal),
				ChangeSource: meta.ChangeSource,
				ChangeTaskID: meta.ChangeTaskID,
				Ctime:        now,
			})
			continue
		}
		// 对比值是否变化
		oldJSON := toJSON(oldVal)
		newJSON := toJSON(newVal)
		if oldJSON != newJSON {
			records = append(records, domain.ChangeRecord{
				AssetID:      meta.AssetID,
				AssetName:    meta.AssetName,
				ModelUID:     meta.ModelUID,
				TenantID:     meta.TenantID,
				AccountID:    meta.AccountID,
				Provider:     meta.Provider,
				Region:       meta.Region,
				FieldName:    key,
				OldValue:     oldJSON,
				NewValue:     newJSON,
				ChangeSource: meta.ChangeSource,
				ChangeTaskID: meta.ChangeTaskID,
				Ctime:        now,
			})
		}
	}

	// 检测删除的字段
	for key, oldVal := range oldAttrs {
		if t.ignoreFields[key] {
			continue
		}
		if _, exists := newAttrs[key]; !exists {
			records = append(records, domain.ChangeRecord{
				AssetID:      meta.AssetID,
				AssetName:    meta.AssetName,
				ModelUID:     meta.ModelUID,
				TenantID:     meta.TenantID,
				AccountID:    meta.AccountID,
				Provider:     meta.Provider,
				Region:       meta.Region,
				FieldName:    key,
				OldValue:     toJSON(oldVal),
				NewValue:     "",
				ChangeSource: meta.ChangeSource,
				ChangeTaskID: meta.ChangeTaskID,
				Ctime:        now,
			})
		}
	}

	if len(records) == 0 {
		return 0, nil
	}

	if err := t.dao.BatchCreate(ctx, records); err != nil {
		t.logger.Warn("记录资产变更失败",
			elog.FieldErr(err),
			elog.String("asset_id", meta.AssetID),
			elog.Int("changes", len(records)),
		)
		return 0, fmt.Errorf("记录资产变更失败: %w", err)
	}

	t.logger.Debug("记录资产变更",
		elog.String("asset_id", meta.AssetID),
		elog.Int("changes", len(records)),
	)
	return len(records), nil
}

// ListByAssetID 查询资产变更历史
func (t *ChangeTracker) ListByAssetID(ctx context.Context, filter domain.ChangeFilter) ([]domain.ChangeRecord, int64, error) {
	records, err := t.dao.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("查询变更历史失败: %w", err)
	}
	total, err := t.cachedCount(ctx, &filter)
	if err != nil {
		// count 失败不应让整个列表请求失败。
		//
		// 【实测根因】ecam_change_history 5,399,871 条。按 field_name 筛选时
		// 该字段无索引，CountDocuments 需全表扫描：实测 >30 秒后 Mongo 断开连接，
		// 接口返回 500（"统计变更历史失败: connection(...) socket was unexpectedly
		// closed: EOF"）。而 asset_id 有索引（asset_id_1_ctime_-1），同条件仅 2.9 秒。
		//
		// 原实现把 count 失败当成整个请求失败，导致「按字段名筛选」完全不可用
		//（列表数据其实已取到，却被一并丢弃）。
		// 现在：列表照常返回，total=-1 表示「未知」，由前端决定如何展示。
		t.logger.Warn("统计变更总数失败，列表照常返回（total=-1）",
			elog.String("asset_id", filter.AssetID),
			elog.String("field_name", filter.FieldName),
			elog.FieldErr(err))
		return records, -1, nil
	}
	return records, total, nil
}

// GetSummary 获取变更统计汇总
func (t *ChangeTracker) GetSummary(ctx context.Context, filter domain.ChangeFilter) (*domain.ChangeSummary, error) {
	// 容错策略：各项统计相互独立。任一项失败（如 540 万条记录上聚合超时、
	// 服务端断开连接）时只记告警并跳过，其余项照常返回。
	//
	// 原实现是全有或全无：任一聚合失败即整接口 500，前端顶部统计条完全不可用
	// （实测数据量大时该接口 46s 后返回 500）。改为部分成功，让能算出的先展示。
	// 若全部失败则返回错误，避免用空数据误导调用方。
	var (
		summary   = &domain.ChangeSummary{}
		failed    int
		succeeded int
	)

	// 统计项串行执行 + 总时限，超时即返回已算出的部分。
	//
	// 【实测根因】（2026-10-31，ecam_change_history 5,399,871 条）
	//   单次 group by model_uid  → 1 分 38 秒
	//   单次 group by field_name → 超过 200 秒仍未返回
	//   单次 CountDocuments      → 4.9 秒
	// 因此：
	//   - 原实现串行 4 项 → 接口实测 20 秒以上不返回，线上 300s 后 504
	//   - **不能并行**：4 个重聚合会争抢 Mongo 连接池，
	//     实测出现 `total connections: 0` 与 `timed out while checking out a connection`，
	//     反而会拖垮其它请求（这是本轮试过并回退的方案）。
	//
	// 采用策略：总时限内能算多少算多少，超时项跳过并告警，
	// 让接口在可接受时间内返回部分统计，而不是挂死。
	// 真正的性能修复需要为 (tenant_id, model_uid/field_name/provider)
	// 建覆盖索引，属生产库结构变更，需批准后执行。
	deadline := time.Now().Add(changeSummaryBudget)
	budgetCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	step := func(name string, fn func(context.Context) error) {
		if budgetCtx.Err() != nil {
			failed++
			t.logger.Warn("变更统计超出总时限，已跳过", elog.String("item", name))
			return
		}
		if err := fn(budgetCtx); err != nil {
			failed++
			t.logger.Warn("变更统计项失败，已跳过", elog.String("item", name), elog.FieldErr(err))
			return
		}
		succeeded++
	}

	step("total", func(c context.Context) error {
		v, err := t.cachedCount(c, &filter)
		if err == nil {
			summary.Total = v
		}
		return err
	})
	step("by_resource_type", func(c context.Context) error {
		v, err := t.dao.CountByModelUID(c, filter)
		if err == nil {
			summary.ByResourceType = v
		}
		return err
	})
	step("by_field", func(c context.Context) error {
		v, err := t.dao.CountByField(c, filter)
		if err == nil {
			summary.ByField = v
		}
		return err
	})
	step("by_provider", func(c context.Context) error {
		v, err := t.dao.CountByProvider(c, filter)
		if err == nil {
			summary.ByProvider = v
		}
		return err
	})

	if succeeded == 0 {
		return nil, fmt.Errorf("变更统计全部失败（%d 项）", failed)
	}
	if failed > 0 {
		t.logger.Warn("变更统计部分失败", elog.Int("failed", failed), elog.Int("succeeded", succeeded))
	}
	return summary, nil
}

func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
