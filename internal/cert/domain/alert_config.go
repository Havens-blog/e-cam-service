package domain

import "fmt"

// AlertConfigID 全局告警配置单文档固定 _id。
const AlertConfigID = "global"

// AlertConfig 全局告警配置文档（cert_alert_config，单文档 _id="global"）。
// thresholds 越界由 $jsonSchema 强制（int 范围）；默认值由 repository 写入路径保证。
type AlertConfig struct {
	ID                     string             `bson:"_id"`                        // 恒为 "global"
	WebhookURLs            []string           `bson:"webhookUrls,omitempty"`      // DEFAULT=[]
	EmailGroup             []string           `bson:"emailGroup,omitempty"`       // DEFAULT=[]
	ChannelConfirmed       bool               `bson:"channelConfirmed,omitempty"` // DEFAULT=false
	Thresholds             Thresholds         `bson:"thresholds"`
	VerifyWindowRoute      *VerifyWindowRoute `bson:"verifyWindowRoute,omitempty"`      // DEFAULT=null（复用常规通道+标记）
	WildcardProbeOverrides map[string]string  `bson:"wildcardProbeOverrides,omitempty"` // 通配符SAN→具体探测子域名
}

// VerifyWindowRoute 验证窗口告警路由（change_linked_diff 专用通道）。
type VerifyWindowRoute struct {
	Enabled     bool     `bson:"enabled"` // DEFAULT=false
	WebhookURLs []string `bson:"webhookUrls,omitempty"`
	EmailGroup  []string `bson:"emailGroup,omitempty"`
}

// Thresholds 全局阈值配置。
// 字段范围与 schema.sql cert_alert_config.thresholds 1:1 对齐
// （minimum/maximum 由 $jsonSchema 与 API 写路径 ValidateThresholds 双重强制，
// 任务 4.5 Hard Rule：界值必须服务端校验，越界整体拒绝）。
type Thresholds struct {
	ScanFreshnessHours          int   `bson:"scanFreshnessHours"`          // 1~72，DEFAULT=24
	VerifyWindowHours           int   `bson:"verifyWindowHours"`           // 2~24，DEFAULT=24
	RollbackProtectDays         int   `bson:"rollbackProtectDays"`         // 7~14，DEFAULT=7
	VerifyConfirmProbes         int   `bson:"verifyConfirmProbes"`         // 1~10，DEFAULT=2
	VerifyProbeIntervalMinutes  int   `bson:"verifyProbeIntervalMinutes"`  // 5~60，DEFAULT=10
	PauseTimeoutHours           int   `bson:"pauseTimeoutHours"`           // 24~168，DEFAULT=72
	RecheckDelayMinutes         int   `bson:"recheckDelayMinutes"`         // 1~60，DEFAULT=5
	ItemHeartbeatTimeoutMinutes int   `bson:"itemHeartbeatTimeoutMinutes"` // 5~180，DEFAULT=30
	ScanTimeoutHours            int   `bson:"scanTimeoutHours"`            // 1~12，DEFAULT=2
	ExpiryLevels                []int `bson:"expiryLevels"`                // 1~5 项，各项 1~90；DEFAULT=[30,14,7]
}

// DefaultThresholds schema.sql DEFAULT 值集合（repository 未配置时返回）。
func DefaultThresholds() Thresholds {
	return Thresholds{
		ScanFreshnessHours:          24,
		VerifyWindowHours:           24,
		RollbackProtectDays:         7,
		VerifyConfirmProbes:         2,
		VerifyProbeIntervalMinutes:  10,
		PauseTimeoutHours:           72,
		RecheckDelayMinutes:         5,
		ItemHeartbeatTimeoutMinutes: 30,
		ScanTimeoutHours:            2,
		ExpiryLevels:                []int{30, 14, 7},
	}
}

// DefaultAlertConfig 返回填充 schema.sql 全部 DEFAULT 的全局告警配置。
func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		ID:          AlertConfigID,
		WebhookURLs: []string{},
		EmailGroup:  []string{},
		Thresholds:  DefaultThresholds(),
	}
}

// ThresholdsInvalidError thresholds 界值校验失败（4.5 Hard Rule：服务端强制，
// 越界 PUT 整体拒绝、不做部分写入）。Field 为 thresholds 内字段名（camelCase，
// 与 API/bson 一致），Reason 为固定文案（合法区间/项数约束），供 web 层映射 400。
type ThresholdsInvalidError struct {
	Field  string
	Reason string
}

// Error 实现 error；文案仅含字段名与合法区间，无内部细节。
func (e *ThresholdsInvalidError) Error() string {
	return fmt.Sprintf("cert: invalid thresholds.%s: %s", e.Field, e.Reason)
}

// expiryLevels 界值：1~5 项、各项 1~90、去重（schema.sql uniqueItems）。
const (
	minExpiryLevels    = 1
	maxExpiryLevels    = 5
	minExpiryLevelDays = 1
	maxExpiryLevelDays = 90
)

// ValidateThresholds 服务端界值校验（界值与 schema.sql
// cert_alert_config.thresholds minimum/maximum 1:1）。
// 任一字段越界即返回 *ThresholdsInvalidError（首个命中字段）；
// 通过返回 nil。expiryLevels 需非空 1~5 项、各项 1~90 且不重复。
func ValidateThresholds(t Thresholds) error {
	checks := []struct {
		field    string
		value    int
		min, max int
	}{
		{"scanFreshnessHours", t.ScanFreshnessHours, 1, 72},
		{"verifyWindowHours", t.VerifyWindowHours, 2, 24},
		{"rollbackProtectDays", t.RollbackProtectDays, 7, 14},
		{"verifyConfirmProbes", t.VerifyConfirmProbes, 1, 10},
		{"verifyProbeIntervalMinutes", t.VerifyProbeIntervalMinutes, 5, 60},
		{"pauseTimeoutHours", t.PauseTimeoutHours, 24, 168},
		{"recheckDelayMinutes", t.RecheckDelayMinutes, 1, 60},
		{"itemHeartbeatTimeoutMinutes", t.ItemHeartbeatTimeoutMinutes, 5, 180},
		{"scanTimeoutHours", t.ScanTimeoutHours, 1, 12},
	}
	for _, c := range checks {
		if c.value < c.min || c.value > c.max {
			return &ThresholdsInvalidError{
				Field:  c.field,
				Reason: fmt.Sprintf("must be between %d and %d", c.min, c.max),
			}
		}
	}
	if len(t.ExpiryLevels) < minExpiryLevels || len(t.ExpiryLevels) > maxExpiryLevels {
		return &ThresholdsInvalidError{
			Field:  "expiryLevels",
			Reason: fmt.Sprintf("must contain %d to %d entries", minExpiryLevels, maxExpiryLevels),
		}
	}
	seen := make(map[int]bool, len(t.ExpiryLevels))
	for _, l := range t.ExpiryLevels {
		if l < minExpiryLevelDays || l > maxExpiryLevelDays {
			return &ThresholdsInvalidError{
				Field:  "expiryLevels",
				Reason: fmt.Sprintf("each entry must be between %d and %d", minExpiryLevelDays, maxExpiryLevelDays),
			}
		}
		if seen[l] {
			return &ThresholdsInvalidError{
				Field:  "expiryLevels",
				Reason: "entries must be unique",
			}
		}
		seen[l] = true
	}
	return nil
}
