// Package detector 告警检测器
package detector

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/internal/alert/service"
	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/gotomicro/ego/core/elog"
)

// ChangeDetector 资源变更检测器
type ChangeDetector struct {
	alertService *service.AlertService
	logger       *elog.Component
}

// NewChangeDetector 创建变更检测器
func NewChangeDetector(alertService *service.AlertService, logger *elog.Component) *ChangeDetector {
	return &ChangeDetector{alertService: alertService, logger: logger}
}

// DetectChanges 检测资源变更并触发告警
// oldInstances: 同步前的资产列表, newInstances: 同步后的资产列表
func (d *ChangeDetector) DetectChanges(
	ctx context.Context,
	tenantID string,
	accountID int64,
	provider string,
	region string,
	resourceType string,
	oldInstances []camdomain.Instance,
	newInstances []camdomain.Instance,
) error {
	oldMap := make(map[string]camdomain.Instance, len(oldInstances))
	for _, inst := range oldInstances {
		oldMap[inst.AssetID] = inst
	}

	newMap := make(map[string]camdomain.Instance, len(newInstances))
	for _, inst := range newInstances {
		newMap[inst.AssetID] = inst
	}

	var changes []domain.ResourceChange

	// 检测新增
	for assetID, inst := range newMap {
		if _, exists := oldMap[assetID]; !exists {
			changes = append(changes, domain.ResourceChange{
				ChangeType:   "added",
				ResourceType: resourceType,
				AssetID:      assetID,
				AssetName:    inst.AssetName,
				AccountID:    accountID,
				Provider:     provider,
				Region:       region,
			})
		}
	}

	// 检测删除
	for assetID, inst := range oldMap {
		if _, exists := newMap[assetID]; !exists {
			changes = append(changes, domain.ResourceChange{
				ChangeType:   "removed",
				ResourceType: resourceType,
				AssetID:      assetID,
				AssetName:    inst.AssetName,
				AccountID:    accountID,
				Provider:     provider,
				Region:       region,
			})
		}
	}

	// 检测状态变更
	for assetID, newInst := range newMap {
		if oldInst, exists := oldMap[assetID]; exists {
			oldStatus := oldInst.GetStringAttribute("status")
			newStatus := newInst.GetStringAttribute("status")
			if oldStatus != "" && newStatus != "" && oldStatus != newStatus {
				changes = append(changes, domain.ResourceChange{
					ChangeType:   "modified",
					ResourceType: resourceType,
					AssetID:      assetID,
					AssetName:    newInst.AssetName,
					AccountID:    accountID,
					Provider:     provider,
					Region:       region,
					Details: map[string]any{
						"field":     "status",
						"old_value": oldStatus,
						"new_value": newStatus,
					},
				})
			}
		}
	}

	if len(changes) == 0 {
		return nil
	}

	d.logger.Info("检测到资源变更",
		elog.String("resource_type", resourceType),
		elog.String("region", region),
		elog.Int("added", countByType(changes, "added")),
		elog.Int("removed", countByType(changes, "removed")),
		elog.Int("modified", countByType(changes, "modified")))

	// 合并变更为一个告警事件
	return d.emitChangeEvent(ctx, tenantID, accountID, provider, region, resourceType, changes)
}

func (d *ChangeDetector) emitChangeEvent(
	ctx context.Context,
	tenantID string,
	accountID int64,
	provider, region, resourceType string,
	changes []domain.ResourceChange,
) error {
	added := countByType(changes, "added")
	removed := countByType(changes, "removed")
	modified := countByType(changes, "modified")

	severity := domain.SeverityInfo
	if removed > 0 {
		severity = domain.SeverityWarning
	}

	title := fmt.Sprintf("资源变更: %s [%s/%s]", resourceType, provider, region)

	content := map[string]any{
		"resource_type":  resourceType,
		"account_id":     float64(accountID),
		"provider":       provider,
		"region":         region,
		"added_count":    added,
		"removed_count":  removed,
		"modified_count": modified,
		"changes":        changes,
	}

	event := domain.AlertEvent{
		Type:     domain.AlertTypeResourceChange,
		Severity: severity,
		Title:    title,
		Content:  content,
		Source:   fmt.Sprintf("change_detector:%s:%s", resourceType, region),
		TenantID: tenantID,
	}

	return d.alertService.EmitEvent(ctx, event)
}

// DetectSyncFailure 检测同步失败并触发告警
func (d *ChangeDetector) DetectSyncFailure(
	ctx context.Context,
	tenantID string,
	taskID string,
	accountID int64,
	accountName string,
	reason string,
) error {
	event := domain.AlertEvent{
		Type:     domain.AlertTypeSyncFailure,
		Severity: domain.SeverityCritical,
		Title:    fmt.Sprintf("同步失败: %s", accountName),
		Content: map[string]any{
			"task_id":      taskID,
			"account_id":   float64(accountID),
			"account_name": accountName,
			"reason":       reason,
		},
		Source:   fmt.Sprintf("sync_task:%s", taskID),
		TenantID: tenantID,
	}

	return d.alertService.EmitEvent(ctx, event)
}

// DetectExpiration 检测资源过期
func (d *ChangeDetector) DetectExpiration(
	ctx context.Context,
	tenantID string,
	instances []camdomain.Instance,
	reminderDays []int, // e.g. [7, 3, 1]
) error {
	now := time.Now()

	for _, inst := range instances {
		expireTimeStr := inst.GetStringAttribute("expire_time")
		if expireTimeStr == "" {
			continue
		}

		expireTime, err := time.Parse(time.RFC3339, expireTimeStr)
		if err != nil {
			continue
		}

		daysLeft := int(expireTime.Sub(now).Hours() / 24)
		if daysLeft < 0 {
			continue
		}

		for _, days := range reminderDays {
			if daysLeft == days {
				resourceType := inst.GetStringAttribute("resource_type")
				if resourceType == "" {
					resourceType = inst.ModelUID
				}

				event := domain.AlertEvent{
					Type:     domain.AlertTypeExpiration,
					Severity: d.expirationSeverity(daysLeft),
					Title:    fmt.Sprintf("资源即将过期: %s (%d天)", inst.AssetName, daysLeft),
					Content: map[string]any{
						"resource_type": resourceType,
						"asset_id":      inst.AssetID,
						"asset_name":    inst.AssetName,
						"expire_time":   expireTimeStr,
						"days_left":     float64(daysLeft),
						"account_id":    float64(inst.AccountID),
					},
					Source:   fmt.Sprintf("expiration:%s", inst.AssetID),
					TenantID: tenantID,
				}

				if err := d.alertService.EmitEvent(ctx, event); err != nil {
					d.logger.Error("触发过期告警失败",
						elog.String("asset_id", inst.AssetID),
						elog.FieldErr(err))
				}
				break
			}
		}
	}

	return nil
}

func (d *ChangeDetector) expirationSeverity(daysLeft int) domain.Severity {
	if daysLeft <= 1 {
		return domain.SeverityCritical
	}
	if daysLeft <= 3 {
		return domain.SeverityWarning
	}
	return domain.SeverityInfo
}

func countByType(changes []domain.ResourceChange, changeType string) int {
	count := 0
	for _, c := range changes {
		if c.ChangeType == changeType {
			count++
		}
	}
	return count
}
