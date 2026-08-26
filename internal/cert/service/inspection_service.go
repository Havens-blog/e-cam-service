package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// 到期分级告警引擎（任务 4.2，tech-design"到期分级告警（去重状态机）"）
// ---------------------------------------------------------------------

// 巡检常量（schema.sql cert_certificates / cert_alert_config DEFAULT 口径）。
const (
	// expiryDayDuration daysLeft 计算的天单位（24h）。
	expiryDayDuration = 24 * time.Hour
)

// defaultExpiryLevels schema.sql cert_alert_config.thresholds.expiryLevels DEFAULT。
var defaultExpiryLevels = []int{30, 14, 7}

// expiryTierLabels 档位序号（降序阈值数组下标）→ 持久化枚举标签：
// 最缓档→L30、次档→L14、第三档及更紧急→L7（枚举仅三个非过期标签，
// 超出部分折叠进最紧急档；映射单调，升级去重不变式不受影响）。
var expiryTierLabels = []domain.ExpiryAlertLevel{
	domain.ExpiryAlertL30,
	domain.ExpiryAlertL14,
	domain.ExpiryAlertL7,
}

// InspectionService 到期分级告警引擎：天级按 notAfter 计算 daysLeft 命中
// thresholds.expiryLevels（默认 [30,14,7] 降序取最紧急级）或 expired 级；
// Certificate.expiryAlertLevel 持久化去重——仅升级触发告警，同级不重发，
// 换证后（notAfter 回升出全部级别区间）重置 none 并重新计级。
//
// 完整巡检流水线（完整性复检+探测调度+豁免过滤）在 4.4 组装；调度注册天级任务。
type InspectionService interface {
	// InspectLedger 全量台账分级巡检：逐证计级 → 升级去重判定 → 到期分级���件发布
	// → 去重状态持久化。单证失败不中断整轮（错误聚合返回）。
	InspectLedger(ctx context.Context) (InspectionSummary, error)
}

// InspectionSummary 单轮巡检汇总。
type InspectionSummary struct {
	Evaluated int // 参与计级的证书数（notAfter 缺失的跳过不计）
	Triggered int // 升级触发数（事件发布+状态更新均成功）
	Reset     int // 换证重置数（notAfter 回升出全部级别区间 → none）
}

type inspectionService struct {
	certs     domain.CertificateRepository
	alertCfg  domain.AlertConfigRepository
	publisher CertAlertPublisher // nil 时构造函数回退日志实现
	now       func() time.Time   // 时钟注入缝（测试推进天级时序）
}

// NewInspectionService 创建巡检服务。deps 说明：
//   - certs：台账读取 + UpdateExpiryAlertLevel 去重状态持久化
//   - alertCfg：thresholds.expiryLevels 读取（单文档 _id="global"，缺省回退 DEFAULT）
//   - publisher：到期分级事件发布端口；nil 回退日志实现（4.3 通道前的默认路径）
func NewInspectionService(
	certs domain.CertificateRepository,
	alertCfg domain.AlertConfigRepository,
	publisher CertAlertPublisher,
) InspectionService {
	if publisher == nil {
		publisher = NewLoggingAlertPublisher()
	}
	return &inspectionService{
		certs:     certs,
		alertCfg:  alertCfg,
		publisher: publisher,
		now:       time.Now,
	}
}

// InspectLedger 单轮巡检。处理顺序（单证内）：计级 → 判定 → 发布 → 落状态；
// 发布失败不落状态（at-least-once：下轮巡检重发），落状态失败不中断其他证。
func (s *inspectionService) InspectLedger(ctx context.Context) (InspectionSummary, error) {
	certs, err := s.certs.List(ctx)
	if err != nil {
		return InspectionSummary{}, fmt.Errorf("inspection: list ledger certificates: %w", err)
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return InspectionSummary{}, fmt.Errorf("inspection: get alert config: %w", err)
	}
	levels := cfg.Thresholds.ExpiryLevels
	now := s.now()

	summary := InspectionSummary{}
	var errs []error
	for _, cert := range certs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("inspection: round cancelled: %w", err))
			break
		}
		// notAfter 缺失（零值）无法计级：跳过，不误报 expired
		if cert.NotAfter.IsZero() {
			continue
		}
		// 盘点容忍登记的已过期证书不参与到期告警（已知存量的处置动作是换证，
		// 反复告警无运营价值；台账列表 daysLeft 负值仍可见）——cert-cloud-discovery-import
		if cert.MaterialIssue == domain.MaterialIssueExpired {
			continue
		}
		computed := ComputeExpiryLevel(cert.NotAfter, now, levels)
		persisted := normalizeExpiryAlertLevel(cert.ExpiryAlertLevel)
		summary.Evaluated++

		switch {
		case computed == domain.ExpiryAlertNone:
			// 换证：notAfter 回升出全部级别区间 → 重置 none（不发告警）
			if persisted != domain.ExpiryAlertNone {
				if err := s.certs.UpdateExpiryAlertLevel(ctx, cert.Fingerprint, domain.ExpiryAlertNone); err != nil {
					errs = append(errs, fmt.Errorf("inspection: reset level for %s: %w", cert.Fingerprint, err))
					continue
				}
				summary.Reset++
			}

		case expiryLevelRank(computed) > expiryLevelRank(persisted):
			// 升级（L30→L14→L7→expired 单向）：先发布，后持久化——
			// 发布失败不落状态，下轮重发（at-least-once）
			event := s.buildExpiryEvent(cert, computed, now)
			if err := s.publisher.PublishAlert(ctx, event); err != nil {
				errs = append(errs, fmt.Errorf("inspection: publish expiry alert for %s: %w", cert.Fingerprint, err))
				continue
			}
			if err := s.certs.UpdateExpiryAlertLevel(ctx, cert.Fingerprint, computed); err != nil {
				errs = append(errs, fmt.Errorf("inspection: persist level for %s: %w", cert.Fingerprint, err))
				continue
			}
			summary.Triggered++

		default:
			// 同级/降级：不重复触发、不改写状态
		}
	}
	return summary, errors.Join(errs...)
}

// buildExpiryEvent 构造到期分级事件（按证书粒度聚合；不含任何私钥字段）。
func (s *inspectionService) buildExpiryEvent(cert domain.Certificate, level domain.ExpiryAlertLevel, now time.Time) CertAlertEvent {
	days := daysLeftCeil(cert.NotAfter, now)
	return CertAlertEvent{
		Category:    AlertCategoryExpiry,
		Title:       fmt.Sprintf("证书到期分级 %s：剩余 %d 天", level, days),
		Fingerprint: cert.Fingerprint,
		SANs:        cert.Sans,
		Level:       level,
		DaysLeft:    days,
		NotAfter:    cert.NotAfter,
		At:          now,
	}
}

// ---------------------------------------------------------------------
// 分级计算（纯函数，4.4 巡检流水线与看板统计可复用）
// ---------------------------------------------------------------------

// ComputeExpiryLevel 到期分级计算：daysLeft = ceil((notAfter-now)/24h)，
// 命中 thresholds（降序匹配取最紧急命中级）或已过期返回 expired。
//
// 档位标签映射：thresholds 降序排序后按下标取标签（0→L30、1→L14、>=2→L7），
// 默认 [30,14,7] 恰为恒等映射；levels 空/全非法时回退 schema.sql DEFAULT。
func ComputeExpiryLevel(notAfter, now time.Time, levels []int) domain.ExpiryAlertLevel {
	if !notAfter.After(now) {
		return domain.ExpiryAlertExpired
	}
	tiers := normalizeExpiryLevels(levels)
	days := daysLeftCeil(notAfter, now)
	// 降序数组自尾（最小阈值=最紧急档）向前找首个命中档：days <= t
	for i := len(tiers) - 1; i >= 0; i-- {
		if days <= tiers[i] {
			return expiryTierLabel(i)
		}
	}
	return domain.ExpiryAlertNone
}

// daysLeftCeil daysLeft = ceil((notAfter-now)/24h)：部分天向上取整
// （29.5 天 → 30）；notAfter <= now 返回 0（调用方先行判 expired）。
func daysLeftCeil(notAfter, now time.Time) int {
	d := notAfter.Sub(now)
	if d <= 0 {
		return 0
	}
	return int((d + expiryDayDuration - 1) / expiryDayDuration)
}

// normalizeExpiryLevels 阈值清理：剔除非正值、降序排序；空集回退 DEFAULT [30,14,7]。
func normalizeExpiryLevels(levels []int) []int {
	tiers := make([]int, 0, len(levels))
	for _, l := range levels {
		if l > 0 {
			tiers = append(tiers, l)
		}
	}
	if len(tiers) == 0 {
		return defaultExpiryLevels
	}
	sort.Sort(sort.Reverse(sort.IntSlice(tiers)))
	return tiers
}

// expiryTierLabel 档位序号 → 枚举标签（越界折叠：负值防御性 none、超上限取 L7）。
func expiryTierLabel(i int) domain.ExpiryAlertLevel {
	if i < 0 {
		return domain.ExpiryAlertNone
	}
	if i >= len(expiryTierLabels) {
		i = len(expiryTierLabels) - 1
	}
	return expiryTierLabels[i]
}

// expiryLevelRank 紧急度序：none(含历史缺省空串)=0 < L30=1 < L14=2 < L7=3 < expired=4。
// 升级判定即 rank(new) > rank(persisted)。
func expiryLevelRank(level domain.ExpiryAlertLevel) int {
	switch level {
	case domain.ExpiryAlertExpired:
		return 4
	case domain.ExpiryAlertL7:
		return 3
	case domain.ExpiryAlertL14:
		return 2
	case domain.ExpiryAlertL30:
		return 1
	default:
		// none 与历史文档缺省字段（bson omitempty 解码为空串）同权
		return 0
	}
}

// normalizeExpiryAlertLevel 历史文档缺省字段（空串）归一为 none。
func normalizeExpiryAlertLevel(level domain.ExpiryAlertLevel) domain.ExpiryAlertLevel {
	if level == "" {
		return domain.ExpiryAlertNone
	}
	return level
}
