package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// TestStepExemptionFilter_OnlineExpiredCategory 线上证书过期 → 独立类别
// tls_online_expired（Critical 通道）；普通 diff 仍走 tls_diff；两者分别计数。
func TestStepExemptionFilter_OnlineExpiredCategory(t *testing.T) {
	h := newJobHarness(t)
	notAfter := time.Now().Add(-48 * time.Hour)
	results := []domain.ProbeResult{
		{Domain: "expired.example.com", Status: domain.ProbeStatusDiff, OnlineExpired: true, OnlineNotAfter: &notAfter},
		{Domain: "mismatch.example.com", Status: domain.ProbeStatusDiff},
	}

	m, err := h.job.stepExemptionFilter(context.Background(), results)
	assert.NoError(t, err)

	events := h.pub.Events()
	assert.Len(t, events, 2, "两个 diff 各发布一条告警")

	var expiredEvt, mismatchEvt *service.CertAlertEvent
	for i := range events {
		switch events[i].Domain {
		case "expired.example.com":
			expiredEvt = &events[i]
		case "mismatch.example.com":
			mismatchEvt = &events[i]
		}
	}

	assert.NotNil(t, expiredEvt, "过期域告警已发布")
	assert.Equal(t, service.AlertCategoryTLSOnlineExpired, expiredEvt.Category, "过期走独立类别")
	assert.Contains(t, expiredEvt.Title, "已过期")
	assert.Contains(t, expiredEvt.Detail, "EXPIRED")

	assert.NotNil(t, mismatchEvt, "普通 diff 告警已发布")
	assert.Equal(t, service.AlertCategoryTLSDiff, mismatchEvt.Category, "普通 diff 仍走 tls_diff")

	assert.Equal(t, 1, m.Extra["expiredAlerted"], "过期单独计数")
	assert.Equal(t, 1, m.Extra["diffAlerted"], "普通 diff 计数不受影响")
}
