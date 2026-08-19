package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultThresholds 默认阈值与 schema.sql DEFAULT 值一致。
func TestDefaultThresholds(t *testing.T) {
	th := DefaultThresholds()
	assert.Equal(t, 24, th.ScanFreshnessHours)
	assert.Equal(t, 24, th.VerifyWindowHours)
	assert.Equal(t, 7, th.RollbackProtectDays)
	assert.Equal(t, 2, th.VerifyConfirmProbes)
	assert.Equal(t, 10, th.VerifyProbeIntervalMinutes)
	assert.Equal(t, 72, th.PauseTimeoutHours)
	assert.Equal(t, 5, th.RecheckDelayMinutes)
	assert.Equal(t, 30, th.ItemHeartbeatTimeoutMinutes)
	assert.Equal(t, 2, th.ScanTimeoutHours)
	assert.Equal(t, []int{30, 14, 7}, th.ExpiryLevels)
}

// TestValidateThresholdsBounds 界值校验：DEFAULT 通过；边界内通过、越界拒绝
// （界值与 schema.sql cert_alert_config.thresholds minimum/maximum 1:1）。
func TestValidateThresholdsBounds(t *testing.T) {
	require.NoError(t, ValidateThresholds(DefaultThresholds()))

	for _, tc := range []struct {
		name  string
		mut   func(*Thresholds)
		field string
	}{
		{"scanFreshnessHours min ok", func(t *Thresholds) { t.ScanFreshnessHours = 1 }, ""},
		{"scanFreshnessHours max ok", func(t *Thresholds) { t.ScanFreshnessHours = 72 }, ""},
		{"scanFreshnessHours below", func(t *Thresholds) { t.ScanFreshnessHours = 0 }, "scanFreshnessHours"},
		{"verifyWindowHours below", func(t *Thresholds) { t.VerifyWindowHours = 1 }, "verifyWindowHours"},
		{"verifyWindowHours above", func(t *Thresholds) { t.VerifyWindowHours = 25 }, "verifyWindowHours"},
		{"rollbackProtectDays below", func(t *Thresholds) { t.RollbackProtectDays = 6 }, "rollbackProtectDays"},
		{"rollbackProtectDays above", func(t *Thresholds) { t.RollbackProtectDays = 15 }, "rollbackProtectDays"},
		{"verifyConfirmProbes above", func(t *Thresholds) { t.VerifyConfirmProbes = 11 }, "verifyConfirmProbes"},
		{"verifyProbeIntervalMinutes below", func(t *Thresholds) { t.VerifyProbeIntervalMinutes = 4 }, "verifyProbeIntervalMinutes"},
		{"verifyProbeIntervalMinutes above", func(t *Thresholds) { t.VerifyProbeIntervalMinutes = 61 }, "verifyProbeIntervalMinutes"},
		{"pauseTimeoutHours below", func(t *Thresholds) { t.PauseTimeoutHours = 23 }, "pauseTimeoutHours"},
		{"pauseTimeoutHours above", func(t *Thresholds) { t.PauseTimeoutHours = 169 }, "pauseTimeoutHours"},
		{"recheckDelayMinutes above", func(t *Thresholds) { t.RecheckDelayMinutes = 61 }, "recheckDelayMinutes"},
		{"itemHeartbeatTimeoutMinutes below", func(t *Thresholds) { t.ItemHeartbeatTimeoutMinutes = 4 }, "itemHeartbeatTimeoutMinutes"},
		{"itemHeartbeatTimeoutMinutes above", func(t *Thresholds) { t.ItemHeartbeatTimeoutMinutes = 181 }, "itemHeartbeatTimeoutMinutes"},
		{"scanTimeoutHours above", func(t *Thresholds) { t.ScanTimeoutHours = 13 }, "scanTimeoutHours"},
		{"expiryLevels empty", func(t *Thresholds) { t.ExpiryLevels = []int{} }, "expiryLevels"},
		{"expiryLevels too many", func(t *Thresholds) { t.ExpiryLevels = []int{90, 80, 70, 60, 50, 40} }, "expiryLevels"},
		{"expiryLevels below range", func(t *Thresholds) { t.ExpiryLevels = []int{0, 30} }, "expiryLevels"},
		{"expiryLevels above range", func(t *Thresholds) { t.ExpiryLevels = []int{91} }, "expiryLevels"},
		{"expiryLevels duplicated", func(t *Thresholds) { t.ExpiryLevels = []int{30, 30} }, "expiryLevels"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := DefaultThresholds()
			tc.mut(&th)
			err := ValidateThresholds(th)
			if tc.field == "" {
				assert.NoError(t, err)
				return
			}
			var invalid *ThresholdsInvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.field, invalid.Field)
			assert.NotEmpty(t, invalid.Reason)
		})
	}
}

// TestDefaultAlertConfig 默认全局配置 ID 固定为 "global"，且通道为空数组（非 null）。
func TestDefaultAlertConfig(t *testing.T) {
	cfg := DefaultAlertConfig()
	assert.Equal(t, AlertConfigID, cfg.ID)
	assert.Equal(t, "global", cfg.ID)
	assert.NotNil(t, cfg.WebhookURLs)
	assert.NotNil(t, cfg.EmailGroup)
	assert.False(t, cfg.ChannelConfirmed)
	assert.Nil(t, cfg.VerifyWindowRoute)
}
