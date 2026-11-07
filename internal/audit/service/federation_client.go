package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/auth"
	"github.com/gotomicro/ego/core/elog"
)

// federationHTTPTimeout 跨服务联邦查询超时；审计检索是旁路查询，
// 挂死会拖住统一页请求，10s 上限与仓库内 HTTP 客户端范式一致。
const federationHTTPTimeout = 10 * time.Second

// IAMEventFederationClient e-iam 统一事件联邦读客户端（F2）。
// 调用 e-iam GET /api/s2s/audit/events（S2STokenAuth 验签 + 租户白名单），
// 把 IAM 侧统一事件并入 e-cam 统一检索。
type IAMEventFederationClient struct {
	baseURL       string // eiam 基址（不带尾斜杠）
	tokenProvider auth.TokenProvider
	client        *http.Client
	logger        *elog.Component
}

// NewIAMEventFederationClient 创建联邦读客户端。
// baseURL/tokenProvider 为空时构造不报错，调用时报错（上层降级为单源），
// 避免装配期因配置缺失阻断服务启动。
func NewIAMEventFederationClient(baseURL string, tokenProvider auth.TokenProvider, logger *elog.Component) *IAMEventFederationClient {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &IAMEventFederationClient{
		baseURL:       baseURL,
		tokenProvider: tokenProvider,
		client:        &http.Client{Timeout: federationHTTPTimeout},
		logger:        logger,
	}
}

// federationResp e-iam S2S 端点响应（ginx.Result 信封 + 统一事件列表）。
// list 元素字段与 auditlog.AuditEvent 的 JSON 契约一致（e-iam#6 VO 对齐）。
type federationResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Total int64                `json:"total"`
		List  []auditlog.AuditEvent `json:"list"`
	} `json:"data"`
}

// FetchIAMEvents 拉取 e-iam 侧统一审计事件（at 降序）。
// tenantID 来自 e-cam 侧会话租户（经 S2S 令牌租户上下文 + 查询参数双重传递，
// e-iam 侧白名单校验后 fail-closed 过滤）。
func (c *IAMEventFederationClient) FetchIAMEvents(ctx context.Context, tenantID int64, filter *dao.AuditEventFilter, offset, limit int) ([]auditlog.AuditEvent, int64, error) {
	if c.baseURL == "" {
		return nil, 0, fmt.Errorf("eiam.base_url 未配置，联邦读不可用")
	}
	if c.tokenProvider == nil {
		return nil, 0, fmt.Errorf("S2S 令牌换取器未配置，联邦读不可用")
	}

	token, err := c.tokenProvider.TokenFor(ctx, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("换取 S2S 令牌失败: %w", err)
	}

	q := url.Values{}
	q.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	if filter.Service != "" {
		q.Set("service", filter.Service)
	}
	if filter.Domain != "" {
		q.Set("domain", filter.Domain)
	}
	if filter.Action != "" {
		q.Set("action", filter.Action)
	}
	if filter.Result != "" {
		q.Set("result", filter.Result)
	}
	if filter.ActorID != "" {
		q.Set("actor_id", filter.ActorID)
	}
	if filter.ResourceType != "" {
		q.Set("resource_type", filter.ResourceType)
	}
	if filter.StartTime != nil {
		q.Set("start_time", strconv.FormatInt(*filter.StartTime, 10))
	}
	if filter.EndTime != nil {
		q.Set("end_time", strconv.FormatInt(*filter.EndTime, 10))
	}
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/s2s/audit/events?"+q.Encode(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("构造联邦请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("联邦请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("联邦请求非 200: HTTP %d", resp.StatusCode)
	}

	var payload federationResp
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, 0, fmt.Errorf("解析联邦响应失败: %w", err)
	}
	if payload.Code != 0 {
		return nil, 0, fmt.Errorf("联邦读侧业务失败: code=%d msg=%s", payload.Code, payload.Msg)
	}
	return payload.Data.List, payload.Data.Total, nil
}
