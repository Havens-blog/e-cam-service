package service

import (
	"context"
	"encoding/json"
	"fmt"
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

// ChangeTracker 资产变更追踪器
type ChangeTracker struct {
	dao          dao.ChangeRecordDAO
	logger       *elog.Component
	ignoreFields map[string]bool
}

// NewChangeTracker 创建变更追踪器
func NewChangeTracker(dao dao.ChangeRecordDAO, logger *elog.Component) *ChangeTracker {
	return &ChangeTracker{
		dao:          dao,
		logger:       logger,
		ignoreFields: defaultIgnoreFields,
	}
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
	total, err := t.dao.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("统计变更历史失败: %w", err)
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

	if total, err := t.dao.Count(ctx, filter); err != nil {
		failed++
		t.logger.Warn("统计变更总数失败，已跳过", elog.FieldErr(err))
	} else {
		succeeded++
		summary.Total = total
	}

	if byResource, err := t.dao.CountByModelUID(ctx, filter); err != nil {
		failed++
		t.logger.Warn("按资源类型统计失败，已跳过", elog.FieldErr(err))
	} else {
		succeeded++
		summary.ByResourceType = byResource
	}

	if byField, err := t.dao.CountByField(ctx, filter); err != nil {
		failed++
		t.logger.Warn("按字段统计失败，已跳过", elog.FieldErr(err))
	} else {
		succeeded++
		summary.ByField = byField
	}

	if byProvider, err := t.dao.CountByProvider(ctx, filter); err != nil {
		failed++
		t.logger.Warn("按云厂商统计失败，已跳过", elog.FieldErr(err))
	} else {
		succeeded++
		summary.ByProvider = byProvider
	}

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
