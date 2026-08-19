package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// ---------------------------------------------------------------------
// 证书域 webhook 通道（任务 4.3）：POST JSON，payload 字段白名单硬约束
// ---------------------------------------------------------------------

// 路由来源标记（与 service.RouteCertAlert 输出一致）。
const (
	// CertRouteRegular 常规通道路由。
	CertRouteRegular = "regular"
	// CertRouteVerifyWindow verifyWindowRoute 专用通道路由。
	CertRouteVerifyWindow = "verify_window"
)

// maxCertSANsInPayload webhook payload SAN 摘要上限（超出截断并以计数提示结尾；
// 防超长 SAN 列表撑爆接收方消息体）。
const maxCertSANsInPayload = 20

// CertWebhookPayload 证书域告警 webhook 载荷。
//
// Hard Rule（任务 4.3）：webhook payload 字段白名单——本结构体为固定字段集，
// 不存在 map/any 透传字段，私钥/凭证类材料在类型层即无承载位置；
// 构造一律经 BuildCertWebhookPayload，禁止手工拼装。
type CertWebhookPayload struct {
	Category            string   `json:"category"`                      // 四类业务告警之一 | ops | test
	Title               string   `json:"title"`                         // 人读标题（摘要）
	Severity            string   `json:"severity"`                      // info | warning | critical
	Fingerprint         string   `json:"fingerprint,omitempty"`         // 关联证书 SHA256 指纹
	Level               string   `json:"level,omitempty"`               // expiry 分级 none/L30/L14/L7/expired
	DaysLeft            int      `json:"daysLeft,omitempty"`            // expiry 剩余天数
	NotAfter            string   `json:"notAfter,omitempty"`            // expiry 到期时间（RFC3339）
	Domain              string   `json:"domain,omitempty"`              // tls_diff/change_linked 关联域名
	OrderID             string   `json:"orderId,omitempty"`             // change_linked/rollback_failed 变更单号
	SANs                []string `json:"sans,omitempty"`                // SAN 摘要（截断至上限）
	Detail              string   `json:"detail,omitempty"`              // 机器可读补充
	At                  string   `json:"at"`                            // 事件时间（RFC3339）
	RoutedVia           string   `json:"routedVia"`                     // regular | verify_window
	ChangeLinked        bool     `json:"changeLinked"`                  // 变更关联标记（enabled=false 复用常规通道时 true）
	ExpectedFingerprint string   `json:"expectedFingerprint,omitempty"` // verify_window 路由：预期终态指纹
	PassCount           int      `json:"passCount,omitempty"`           // verify_window 路由：窗口达标计数
}

// BuildCertWebhookPayload 按白名单构造 webhook 载荷。
//
// routedVia 取 CertRouteRegular / CertRouteVerifyWindow；changeLinked 为
// "verifyWindowRoute.enabled=false 时复用常规通道但附变更关联标记"的标记位。
func BuildCertWebhookPayload(
	evt certservice.CertAlertEvent,
	severity domain.Severity,
	routedVia string,
	changeLinked bool,
) *CertWebhookPayload {
	p := &CertWebhookPayload{
		Category:     string(evt.Category),
		Title:        evt.Title,
		Severity:     string(severity),
		Fingerprint:  evt.Fingerprint,
		Level:        string(evt.Level),
		DaysLeft:     evt.DaysLeft,
		Domain:       evt.Domain,
		OrderID:      evt.OrderID,
		SANs:         truncateSANs(evt.SANs),
		Detail:       evt.Detail,
		RoutedVia:    routedVia,
		ChangeLinked: changeLinked,
	}
	if !evt.NotAfter.IsZero() {
		p.NotAfter = evt.NotAfter.UTC().Format(time.RFC3339)
	}
	if evt.At.IsZero() {
		p.At = time.Now().UTC().Format(time.RFC3339)
	} else {
		p.At = evt.At.UTC().Format(time.RFC3339)
	}
	if vw := evt.VerifyWindow; vw != nil {
		// 窗口上下文（5.10 填充）：专用路由附预期指纹与达标计数。
		// OrderID 以窗口上下文为权威值（缺省回退事件字段）。
		if vw.OrderID != "" {
			p.OrderID = vw.OrderID
		}
		p.ExpectedFingerprint = vw.ExpectedFingerprint
		p.PassCount = vw.PassCount
	}
	return p
}

// truncateSANs SAN 摘要截断（>maxCertSANsInPayload 时保留前 N 项 + 计数提示）。
func truncateSANs(sans []string) []string {
	if len(sans) <= maxCertSANsInPayload {
		if len(sans) == 0 {
			return nil
		}
		return append([]string(nil), sans...)
	}
	out := make([]string, 0, maxCertSANsInPayload+1)
	out = append(out, sans[:maxCertSANsInPayload]...)
	out = append(out, fmt.Sprintf("...(+%d more)", len(sans)-maxCertSANsInPayload))
	return out
}

// CertWebhookSender 证书域 webhook 发送器：POST JSON。
//
// 自带 POST 实现而非复用 postJSON：错误信息需做净化（Hard Rule——告警内容/
// 失败原因不得含凭证片段；webhook URL 常内嵌鉴权 token，*url.Error 会携带
// 完整 URL，故网络错误仅保留内层错误、状态错误仅保留状态码）。
type CertWebhookSender struct{}

// NewCertWebhookSender 创建证书域 webhook 发送器。
func NewCertWebhookSender() *CertWebhookSender { return &CertWebhookSender{} }

// Send 向单个 URL POST 载荷；非 200/网络错误返回净化后的 error（不含 URL/凭证）。
// 429 限流与其他失败同路处理——退避重试由发布方（service.CertAlertPublisher）
// 统一控制（有界序列），此处不重试。
func (s *CertWebhookSender) Send(ctx context.Context, webhookURL string, payload *CertWebhookPayload) error {
	if payload == nil {
		return fmt.Errorf("cert webhook payload is nil")
	}
	if webhookURL == "" {
		return fmt.Errorf("cert webhook url is empty")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cert webhook marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cert webhook invalid url") // 不透出 URL 细节
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// *url.Error 携带完整 URL（可能内嵌 token）——仅保留内层错误。
		if ue, ok := err.(*url.Error); ok {
			return fmt.Errorf("cert webhook unreachable: %v", ue.Err)
		}
		return fmt.Errorf("cert webhook request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cert webhook status %d", resp.StatusCode)
	}
	return nil
}
