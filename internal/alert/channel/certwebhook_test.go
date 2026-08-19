package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// certWebhookPayloadWhitelist Hard Rule：webhook payload 允许出现的全部 JSON 键。
// 测试断言实际载荷键集为其子集——白名单外字段（私钥/凭证等）无法出现。
var certWebhookPayloadWhitelist = map[string]bool{
	"category": true, "title": true, "severity": true, "fingerprint": true,
	"level": true, "daysLeft": true, "notAfter": true, "domain": true,
	"orderId": true, "sans": true, "detail": true, "at": true,
	"routedVia": true, "changeLinked": true,
	"expectedFingerprint": true, "passCount": true,
}

func fullCertEvent() certservice.CertAlertEvent {
	notAfter := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return certservice.CertAlertEvent{
		Category:    certservice.AlertCategoryExpiry,
		Title:       "证书到期分级 L7：剩余 7 天",
		Fingerprint: strings.Repeat("ab", 32),
		SANs:        []string{"a.example.com", "b.example.com"},
		Level:       "L7",
		DaysLeft:    7,
		NotAfter:    notAfter,
		At:          time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestBuildCertWebhookPayloadWhitelist(t *testing.T) {
	evt := fullCertEvent()
	p := BuildCertWebhookPayload(evt, domain.SeverityCritical, CertRouteRegular, false)

	data, err := json.Marshal(p)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	for k := range raw {
		assert.True(t, certWebhookPayloadWhitelist[k], "payload key %q outside whitelist", k)
	}
	// 白名单内必出现字段（摘要：category/title/severity/at）
	for _, k := range []string{"category", "title", "severity", "at", "routedVia", "changeLinked"} {
		assert.Contains(t, raw, k)
	}
	assert.Equal(t, "expiry", raw["category"])
	assert.Equal(t, "critical", raw["severity"])
	assert.Equal(t, "regular", raw["routedVia"])
	assert.Equal(t, false, raw["changeLinked"])
}

func TestBuildCertWebhookPayloadChangeLinkedContext(t *testing.T) {
	evt := fullCertEvent()
	evt.Category = certservice.AlertCategoryChangeLinked
	evt.Domain = "www.example.com"
	evt.OrderID = "order-1"
	evt.VerifyWindow = &certservice.VerifyWindowContext{
		Active:              true,
		OrderID:             "order-ctx",
		ExpectedFingerprint: strings.Repeat("cd", 32),
		PassCount:           2,
	}
	p := BuildCertWebhookPayload(evt, domain.SeverityWarning, CertRouteVerifyWindow, false)
	assert.Equal(t, "change_linked", p.Category)
	assert.Equal(t, "order-ctx", p.OrderID) // 窗口上下文为权威值
	assert.Equal(t, strings.Repeat("cd", 32), p.ExpectedFingerprint)
	assert.Equal(t, 2, p.PassCount)
	assert.Equal(t, CertRouteVerifyWindow, p.RoutedVia)
}

func TestBuildCertWebhookPayloadSANsTruncation(t *testing.T) {
	evt := fullCertEvent()
	sans := make([]string, 30)
	for i := range sans {
		sans[i] = "h" + string(rune('a'+i)) + ".example.com"
	}
	evt.SANs = sans
	p := BuildCertWebhookPayload(evt, domain.SeverityWarning, CertRouteRegular, false)
	assert.Len(t, p.SANs, 21) // 前 20 + "...(+10 more)"
	assert.Equal(t, "...(+10 more)", p.SANs[20])
}

func TestCertWebhookSenderSuccess(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	evt := fullCertEvent()
	evt.OrderID = "order-9"
	p := BuildCertWebhookPayload(evt, domain.SeverityCritical, CertRouteRegular, false)
	s := NewCertWebhookSender()
	require.NoError(t, s.Send(context.Background(), srv.URL, p))
	assert.Equal(t, "expiry", gotBody["category"])
	assert.Equal(t, "order-9", gotBody["orderId"]) // AC: POST JSON 含 category/orderId/摘要
}

func TestCertWebhookSenderFailureStatus(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		p := BuildCertWebhookPayload(fullCertEvent(), domain.SeverityWarning, CertRouteRegular, false)
		err := NewCertWebhookSender().Send(context.Background(), srv.URL, p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status", "status %d", status)
		assert.NotContains(t, err.Error(), srv.URL, "错误信息不得携带 webhook URL")
		srv.Close()
	}
}

func TestCertWebhookSenderUnreachableSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	// 先构造带 token 的 URL 再关闭端口，模拟不可达端点。
	target := srv.URL + "/hook?access_token=secret-token-xyz"
	srv.Close()

	p := BuildCertWebhookPayload(fullCertEvent(), domain.SeverityWarning, CertRouteRegular, false)
	err := NewCertWebhookSender().Send(context.Background(), target, p)
	require.Error(t, err)
	// Hard Rule：网络错误经 *url.Error 会携带完整 URL（含 token）——必须净化；
	// 内层 dial 错误仅含 host:port，不含路径与查询串。
	assert.NotContains(t, err.Error(), "secret-token-xyz")
	assert.NotContains(t, err.Error(), "/hook")
	assert.Contains(t, err.Error(), "unreachable")
}

func TestCertWebhookSenderInputGuards(t *testing.T) {
	s := NewCertWebhookSender()
	assert.Error(t, s.Send(context.Background(), "https://x.example.com", nil))
	assert.Error(t, s.Send(context.Background(), "", BuildCertWebhookPayload(fullCertEvent(), domain.SeverityWarning, CertRouteRegular, false)))
}
