// Package budget 预算管理器
package budget

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/internal/alert/service"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
)

const (
	// alertTypeBudgetThreshold 预算阈值告警类型
	alertTypeBudgetThreshold = "budget_threshold"
)

// BudgetProgress 预算消耗进度
type BudgetProgress struct {
	BudgetID        int64   `json:"budget_id"`
	Name            string  `json:"name"`
	AmountLimit     float64 `json:"amount_limit"`
	CurrentSpend    float64 `json:"current_spend"`
	RemainingAmount float64 `json:"remaining_amount"`
	UsagePercent    float64 `json:"usage_percent"`
	Status          string  `json:"status"`
}

// BudgetService 预算管理服务
type BudgetService struct {
	budgetDAO repository.BudgetDAO
	billDAO   repository.BillDAO
	alertSvc  *service.AlertService
	logger    *elog.Component
}

// NewBudgetService 创建预算管理服务
func NewBudgetService(
	budgetDAO repository.BudgetDAO,
	billDAO repository.BillDAO,
	alertSvc *service.AlertService,
	logger *elog.Component,
) *BudgetService {
	return &BudgetService{
		budgetDAO: budgetDAO,
		billDAO:   billDAO,
		alertSvc:  alertSvc,
		logger:    logger,
	}
}

// CreateBudget 创建预算规则
func (s *BudgetService) CreateBudget(ctx context.Context, budget costdomain.BudgetRule) (int64, error) {
	if budget.Name == "" {
		return 0, fmt.Errorf("budget name cannot be empty")
	}
	if budget.AmountLimit <= 0 {
		return 0, fmt.Errorf("budget amount_limit must be positive")
	}
	if len(budget.Thresholds) == 0 {
		return 0, fmt.Errorf("budget thresholds cannot be empty")
	}
	// Validate thresholds: sorted ascending, each in (0, 100]
	for i, t := range budget.Thresholds {
		if t <= 0 || t > 100 {
			return 0, fmt.Errorf("threshold %.2f must be in (0, 100]", t)
		}
		if i > 0 && budget.Thresholds[i] <= budget.Thresholds[i-1] {
			return 0, fmt.Errorf("thresholds must be sorted in ascending order")
		}
	}

	now := time.Now().Unix()
	budget.Status = "active"
	budget.Period = "monthly"
	budget.NotifiedAt = make(map[string]time.Time)
	budget.CreateTime = now
	budget.UpdateTime = now

	return s.budgetDAO.Create(ctx, budget)
}

// UpdateBudget 更新预算规则
func (s *BudgetService) UpdateBudget(ctx context.Context, budget costdomain.BudgetRule) error {
	if budget.Name == "" {
		return fmt.Errorf("budget name cannot be empty")
	}
	if budget.AmountLimit <= 0 {
		return fmt.Errorf("budget amount_limit must be positive")
	}
	if len(budget.Thresholds) > 0 {
		for i, t := range budget.Thresholds {
			if t <= 0 || t > 100 {
				return fmt.Errorf("threshold %.2f must be in (0, 100]", t)
			}
			if i > 0 && budget.Thresholds[i] <= budget.Thresholds[i-1] {
				return fmt.Errorf("thresholds must be sorted in ascending order")
			}
		}
	}

	budget.UpdateTime = time.Now().Unix()
	return s.budgetDAO.Update(ctx, budget)
}

// DeleteBudget 删除预算规则
func (s *BudgetService) DeleteBudget(ctx context.Context, id int64) error {
	return s.budgetDAO.Delete(ctx, id)
}

// GetBudgetProgress 获取预算消耗进度
func (s *BudgetService) GetBudgetProgress(ctx context.Context, budgetID int64) (*BudgetProgress, error) {
	budget, err := s.budgetDAO.GetByID(ctx, budgetID)
	if err != nil {
		return nil, fmt.Errorf("get budget: %w", err)
	}

	currentSpend, err := s.calculateCurrentSpend(ctx, budget)
	if err != nil {
		return nil, fmt.Errorf("calculate current spend: %w", err)
	}

	usagePercent := 0.0
	if budget.AmountLimit > 0 {
		usagePercent = currentSpend / budget.AmountLimit * 100
	}
	remaining := math.Max(0, budget.AmountLimit-currentSpend)

	return &BudgetProgress{
		BudgetID:        budget.ID,
		Name:            budget.Name,
		AmountLimit:     budget.AmountLimit,
		CurrentSpend:    currentSpend,
		RemainingAmount: remaining,
		UsagePercent:    usagePercent,
		Status:          budget.Status,
	}, nil
}

// ListBudgets 查询预算规则列表
func (s *BudgetService) ListBudgets(ctx context.Context, filter repository.BudgetFilter) ([]costdomain.BudgetRule, int64, error) {
	budgets, err := s.budgetDAO.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list budgets: %w", err)
	}
	count, err := s.budgetDAO.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count budgets: %w", err)
	}
	return budgets, count, nil
}

// CheckBudgets 检查所有预算规则的消耗进度（每日定时执行）
func (s *BudgetService) CheckBudgets(ctx context.Context, tenantID string) error {
	budgets, err := s.budgetDAO.ListActive(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list active budgets: %w", err)
	}

	for _, budget := range budgets {
		if err := s.checkSingleBudget(ctx, budget); err != nil {
			s.logger.Error("check budget failed",
				elog.Int64("budget_id", budget.ID),
				elog.String("budget_name", budget.Name),
				elog.FieldErr(err))
		}
	}
	return nil
}

// checkSingleBudget 检查单个预算规则
func (s *BudgetService) checkSingleBudget(ctx context.Context, budget costdomain.BudgetRule) error {
	currentSpend, err := s.calculateCurrentSpend(ctx, budget)
	if err != nil {
		return fmt.Errorf("calculate spend: %w", err)
	}

	if budget.AmountLimit <= 0 {
		return nil
	}

	usagePercent := currentSpend / budget.AmountLimit * 100

	// Sort thresholds ascending to process from lowest to highest
	thresholds := make([]float64, len(budget.Thresholds))
	copy(thresholds, budget.Thresholds)
	sort.Float64s(thresholds)

	notifiedAt := budget.NotifiedAt
	if notifiedAt == nil {
		notifiedAt = make(map[string]time.Time)
	}
	updated := false

	for _, threshold := range thresholds {
		if usagePercent < threshold {
			continue
		}

		thresholdKey := strconv.FormatFloat(threshold, 'f', -1, 64)

		// Check if already notified this month
		if lastNotified, ok := notifiedAt[thresholdKey]; ok {
			if isInCurrentMonth(lastNotified) {
				continue
			}
		}

		// Trigger alert
		severity := thresholdSeverity(threshold)
		event := domain.AlertEvent{
			Type:     domain.AlertType(alertTypeBudgetThreshold),
			Severity: severity,
			Title:    fmt.Sprintf("预算告警: %s 已达 %.0f%% 阈值", budget.Name, threshold),
			Content: map[string]any{
				"budget_id":     budget.ID,
				"budget_name":   budget.Name,
				"amount_limit":  budget.AmountLimit,
				"current_spend": currentSpend,
				"usage_percent": usagePercent,
				"threshold":     threshold,
				"scope_type":    budget.ScopeType,
				"scope_value":   budget.ScopeValue,
			},
			Source:     fmt.Sprintf("budget:%d", budget.ID),
			TenantID:   budget.TenantID,
			Status:     domain.EventStatusPending,
			CreateTime: time.Now(),
		}

		if err := s.alertSvc.EmitEvent(ctx, event); err != nil {
			s.logger.Error("emit budget alert failed",
				elog.Int64("budget_id", budget.ID),
				elog.Any("threshold", threshold),
				elog.FieldErr(err))
			continue
		}

		notifiedAt[thresholdKey] = time.Now()
		updated = true

		s.logger.Info("budget threshold alert triggered",
			elog.Int64("budget_id", budget.ID),
			elog.String("budget_name", budget.Name),
			elog.Any("threshold", threshold),
			elog.Any("usage_percent", usagePercent))
	}

	if updated {
		if err := s.budgetDAO.UpdateNotifiedAt(ctx, budget.ID, notifiedAt); err != nil {
			return fmt.Errorf("update notified_at: %w", err)
		}
	}

	return nil
}

// DeactivateBudgetsByScope 预算失效处理：适用范围对应的云账号被删除时标记为 inactive
func (s *BudgetService) DeactivateBudgetsByScope(ctx context.Context, tenantID string, scopeType string, scopeValue string) error {
	filter := repository.BudgetFilter{
		TenantID:  tenantID,
		ScopeType: scopeType,
		Status:    "active",
	}
	budgets, err := s.budgetDAO.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("list budgets by scope: %w", err)
	}

	for _, budget := range budgets {
		if budget.ScopeValue != scopeValue {
			continue
		}

		if err := s.budgetDAO.UpdateStatus(ctx, budget.ID, "inactive"); err != nil {
			s.logger.Error("deactivate budget failed",
				elog.Int64("budget_id", budget.ID),
				elog.FieldErr(err))
			continue
		}

		// Notify admin about budget deactivation
		event := domain.AlertEvent{
			Type:     domain.AlertType(alertTypeBudgetThreshold),
			Severity: domain.SeverityWarning,
			Title:    fmt.Sprintf("预算规则已失效: %s", budget.Name),
			Content: map[string]any{
				"budget_id":   budget.ID,
				"budget_name": budget.Name,
				"reason":      "适用范围对应的云账号已被删除",
				"scope_type":  budget.ScopeType,
				"scope_value": budget.ScopeValue,
			},
			Source:     fmt.Sprintf("budget:%d", budget.ID),
			TenantID:   budget.TenantID,
			Status:     domain.EventStatusPending,
			CreateTime: time.Now(),
		}

		if err := s.alertSvc.EmitEvent(ctx, event); err != nil {
			s.logger.Error("emit budget deactivation alert failed",
				elog.Int64("budget_id", budget.ID),
				elog.FieldErr(err))
		}

		s.logger.Info("budget deactivated due to scope deletion",
			elog.Int64("budget_id", budget.ID),
			elog.String("scope_type", scopeType),
			elog.String("scope_value", scopeValue))
	}

	return nil
}

// calculateCurrentSpend 计算当月实际支出
func (s *BudgetService) calculateCurrentSpend(ctx context.Context, budget costdomain.BudgetRule) (float64, error) {
	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	endDate := now.Format("2006-01-02")

	filter := repository.UnifiedBillFilter{
		TenantID:  budget.TenantID,
		StartDate: startDate,
		EndDate:   endDate,
	}

	switch budget.ScopeType {
	case "account":
		accountID, err := strconv.ParseInt(budget.ScopeValue, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid account scope value %q: %w", budget.ScopeValue, err)
		}
		filter.AccountID = accountID
	case "provider":
		filter.Provider = budget.ScopeValue
	case "all":
		// No additional filter
	}

	return s.billDAO.SumAmount(ctx, filter)
}

// thresholdSeverity 根据阈值百分比确定告警级别
func thresholdSeverity(threshold float64) domain.Severity {
	switch {
	case threshold >= 100:
		return domain.SeverityCritical
	case threshold >= 80:
		return domain.SeverityWarning
	default:
		return domain.SeverityInfo
	}
}

// isInCurrentMonth 判断时间是否在当前月份
func isInCurrentMonth(t time.Time) bool {
	now := time.Now()
	return t.Year() == now.Year() && t.Month() == now.Month()
}
