// Package anomaly 成本异常检测
package anomaly

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	alertservice "github.com/Havens-blog/e-cam-service/internal/alert/service"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
)

const (
	// alertTypeCostAnomaly 成本异常告警类型
	alertTypeCostAnomaly = "cost_anomaly"

	// defaultThresholdPct 默认偏离阈值百分比
	defaultThresholdPct = 50.0

	// baselineWindowDays 基线计算窗口（天）
	baselineWindowDays = 30
)

// 异常检测维度
var detectDimensions = []string{"cloud_account", "service_type", "service_tree_node"}

// AnomalyService 异常检测服务
type AnomalyService struct {
	anomalyDAO   repository.AnomalyDAO
	billDAO      repository.BillDAO
	alertSvc     *alertservice.AlertService
	logger       *elog.Component
	thresholdPct float64 // 偏离阈值百分比
}

// NewAnomalyService 创建异常检测服务
func NewAnomalyService(
	anomalyDAO repository.AnomalyDAO,
	billDAO repository.BillDAO,
	alertSvc *alertservice.AlertService,
	logger *elog.Component,
) *AnomalyService {
	return &AnomalyService{
		anomalyDAO:   anomalyDAO,
		billDAO:      billDAO,
		alertSvc:     alertSvc,
		logger:       logger,
		thresholdPct: defaultThresholdPct,
	}
}

// SetThreshold 设置偏离阈值百分比
func (s *AnomalyService) SetThreshold(pct float64) {
	s.thresholdPct = pct
}

// DetectAnomalies 每日异常检测
func (s *AnomalyService) DetectAnomalies(ctx context.Context, tenantID, date string) error {
	// 计算基线窗口
	targetDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return fmt.Errorf("parse date %q: %w", date, err)
	}
	baselineStart := targetDate.AddDate(0, 0, -baselineWindowDays).Format("2006-01-02")
	baselineEnd := targetDate.AddDate(0, 0, -1).Format("2006-01-02")

	var anomalies []costdomain.CostAnomaly

	for _, dim := range detectDimensions {
		dimAnomalies, err := s.detectForDimension(ctx, tenantID, dim, date, baselineStart, baselineEnd)
		if err != nil {
			s.logger.Error("detect anomalies for dimension failed",
				elog.String("dimension", dim),
				elog.String("tenant_id", tenantID),
				elog.FieldErr(err))
			continue
		}
		anomalies = append(anomalies, dimAnomalies...)
	}

	if len(anomalies) == 0 {
		return nil
	}

	// 批量保存异常事件
	if _, err := s.anomalyDAO.CreateBatch(ctx, anomalies); err != nil {
		return fmt.Errorf("create anomaly batch: %w", err)
	}

	// 发送告警
	for _, a := range anomalies {
		if err := s.emitAlert(ctx, a); err != nil {
			s.logger.Error("emit anomaly alert failed",
				elog.String("dimension", a.Dimension),
				elog.String("dimension_value", a.DimensionValue),
				elog.FieldErr(err))
		}
	}

	return nil
}

// detectForDimension 对单个维度执行异常检测
func (s *AnomalyService) detectForDimension(
	ctx context.Context,
	tenantID, dimension, date, baselineStart, baselineEnd string,
) ([]costdomain.CostAnomaly, error) {
	// 获取基线数据：过去 30 天按维度聚合的日均成本
	baselineDays, err := s.billDAO.AggregateDailyAmount(ctx, tenantID, baselineStart, baselineEnd, repository.UnifiedBillFilter{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("aggregate baseline: %w", err)
	}

	// 获取当日按维度聚合的成本
	currentCosts, err := s.billDAO.AggregateByField(ctx, tenantID, dimension, date, date)
	if err != nil {
		return nil, fmt.Errorf("aggregate current costs: %w", err)
	}

	// 计算基线：按维度值聚合的日均成本
	baselineByDim, err := s.computeBaseline(ctx, tenantID, dimension, baselineStart, baselineEnd)
	if err != nil {
		return nil, fmt.Errorf("compute baseline: %w", err)
	}
	// 使用 baselineDays 来确定实际天数
	dayCount := len(baselineDays)
	if dayCount == 0 {
		dayCount = baselineWindowDays
	}
	_ = dayCount // baseline already computed in computeBaseline

	var anomalies []costdomain.CostAnomaly
	for _, cur := range currentCosts {
		baseline, ok := baselineByDim[cur.Key]
		if !ok || baseline <= 0 {
			// 无基线数据，跳过（或基线为零）
			continue
		}

		deviationPct := (cur.AmountCNY - baseline) / baseline * 100
		if deviationPct <= s.thresholdPct {
			continue
		}

		severity := classifySeverity(deviationPct)
		cause := fmt.Sprintf("成本突增: %s=%s 日成本 %.2f 元，偏离基线 %.2f 元 %.0f%%",
			dimension, cur.Key, cur.AmountCNY, baseline, deviationPct)

		anomalies = append(anomalies, costdomain.CostAnomaly{
			Dimension:      dimension,
			DimensionValue: cur.Key,
			AnomalyDate:    date,
			ActualAmount:   cur.AmountCNY,
			BaselineAmount: baseline,
			DeviationPct:   deviationPct,
			Severity:       severity,
			PossibleCause:  cause,
			TenantID:       tenantID,
		})
	}

	return anomalies, nil
}

// computeBaseline 计算各维度值的日均基线
func (s *AnomalyService) computeBaseline(
	ctx context.Context,
	tenantID, dimension, startDate, endDate string,
) (map[string]float64, error) {
	results, err := s.billDAO.AggregateByField(ctx, tenantID, dimension, startDate, endDate)
	if err != nil {
		return nil, err
	}

	// 计算日期范围内的天数
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return nil, fmt.Errorf("parse start date: %w", err)
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return nil, fmt.Errorf("parse end date: %w", err)
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days <= 0 {
		days = 1
	}

	baseline := make(map[string]float64, len(results))
	for _, r := range results {
		baseline[r.Key] = r.AmountCNY / float64(days)
	}
	return baseline, nil
}

// classifySeverity 根据偏离百分比确定严重程度
func classifySeverity(deviationPct float64) string {
	switch {
	case deviationPct > 200:
		return "critical"
	case deviationPct > 100:
		return "warning"
	default:
		return "info"
	}
}

// emitAlert 发送异常告警
func (s *AnomalyService) emitAlert(ctx context.Context, anomaly costdomain.CostAnomaly) error {
	severity := domain.SeverityInfo
	switch anomaly.Severity {
	case "critical":
		severity = domain.SeverityCritical
	case "warning":
		severity = domain.SeverityWarning
	}

	event := domain.AlertEvent{
		Type:     domain.AlertType(alertTypeCostAnomaly),
		Severity: severity,
		Title:    fmt.Sprintf("成本异常: %s=%s 偏离基线 %.0f%%", anomaly.Dimension, anomaly.DimensionValue, anomaly.DeviationPct),
		Content: map[string]any{
			"dimension":       anomaly.Dimension,
			"dimension_value": anomaly.DimensionValue,
			"anomaly_date":    anomaly.AnomalyDate,
			"actual_amount":   anomaly.ActualAmount,
			"baseline_amount": anomaly.BaselineAmount,
			"deviation_pct":   anomaly.DeviationPct,
			"severity":        anomaly.Severity,
			"possible_cause":  anomaly.PossibleCause,
		},
		Source:     fmt.Sprintf("anomaly:%s:%s:%s", anomaly.Dimension, anomaly.DimensionValue, anomaly.AnomalyDate),
		TenantID:   anomaly.TenantID,
		Status:     domain.EventStatusPending,
		CreateTime: time.Now(),
	}

	return s.alertSvc.EmitEvent(ctx, event)
}

// GetAnomalyEvents 获取异常事件列表
func (s *AnomalyService) GetAnomalyEvents(ctx context.Context, tenantID string, filter repository.AnomalyFilter) ([]costdomain.CostAnomaly, int64, error) {
	filter.TenantID = tenantID
	anomalies, err := s.anomalyDAO.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list anomalies: %w", err)
	}
	count, err := s.anomalyDAO.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count anomalies: %w", err)
	}
	return anomalies, count, nil
}
