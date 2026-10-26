// EIAMTenantClient 是 TenantGovernancePort 的远程 HTTP 实现：
// 调用 eiam 租户详情端点 GET /api/tenant/detail/:id 读取治理配置。
//
// 端点契约对应 eiam/internal/web/tenant（ecodeclub/ginx 信封）：
//   - 成功：HTTP 200，{"code":0,"msg":"","data":{"max_accounts":N,"allowed_vendors":[...]}}
//   - 业务失败（如租户不存在）：HTTP 500，{"code":4020906,"msg":"...","data":null}
//   - 未认证：会话中间件 401（无响应体契约）
//
// 鉴权现状（阻塞，详见任务记录 records/1-1.md）：detail 端点挂在 eiam
// PrivateRoutes 下走用户会话鉴权；eiam 现有服务间令牌机制（discovery 的
// Bearer eiam_sct_*）仅覆盖资产上报端点，不适用本端点。此处按约定提供
// 可配置的 Bearer token（viper eiam.service_token），真实服务间鉴权机制
// 落地前对真实 eiam 的调用预期 401，由上层 fail-closed 拒绝创建。
package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gotomicro/ego/core/elog"
)

// eiamTenantDetailPath eiam 租户详情端点路径前缀（后接租户 ID）
const eiamTenantDetailPath = "/api/tenant/detail/"

// errBaseURLNotConfigured base_url 未配置（查询期报错，上层 fail-closed）
var errBaseURLNotConfigured = errors.New("eiam.base_url 未配置，无法查询租户治理配置")

// governanceHTTPTimeout 跨服务查询超时；CreateAccount 是同步写路径，
// 治理查询挂死会拖住整个创建请求，10s 上限与仓库内 HTTP 客户端范式一致
const governanceHTTPTimeout = 10 * time.Second

// EIAMTenantClient eiam 租户治理配置 HTTP 客户端。
type EIAMTenantClient struct {
	baseURL string // eiam 基址，如 http://127.0.0.1:9000（不带尾斜杠）
	token   string // 可选 Bearer token（服务间鉴权机制落地前的占位，可空）
	client  *http.Client
	logger  *elog.Component
}

// NewEIAMTenantClient 创建 eiam 租户治理配置客户端。
// baseURL 为空时构造不报错，查询时报错（上层 fail-closed），
// 避免装配期因配置缺失阻断整个服务启动。
func NewEIAMTenantClient(baseURL, token string, logger *elog.Component) *EIAMTenantClient {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &EIAMTenantClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: governanceHTTPTimeout},
		logger:  logger,
	}
}

// tenantDetailResp eiam ginx.Result 信封（只取治理相关字段）
type tenantDetailResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		MaxAccounts    int64    `json:"max_accounts"`
		AllowedVendors []string `json:"allowed_vendors"`
	} `json:"data"`
}

// GetGovernance 实现 TenantGovernancePort。
func (c *EIAMTenantClient) GetGovernance(ctx context.Context, tenantID int64) (int64, []string, error) {
	if tenantID <= 0 {
		return 0, nil, fmt.Errorf("非法租户 ID: %d", tenantID)
	}
	if c.baseURL == "" {
		return 0, nil, errBaseURLNotConfigured
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+eiamTenantDetailPath+strconv.FormatInt(tenantID, 10), nil)
	if err != nil {
		return 0, nil, fmt.Errorf("构造 eiam 租户详情请求失败: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("调用 eiam 租户详情失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("eiam 租户详情返回非 200: %d", resp.StatusCode)
	}

	var body tenantDetailResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, nil, fmt.Errorf("解析 eiam 租户详情响应失败: %w", err)
	}
	// eiam ginx 成功信封 code=0（零值）；兼容 200 以覆盖信封变体
	if body.Code != 0 && body.Code != http.StatusOK {
		c.logger.Error("eiam 租户详情返回业务错误",
			elog.Int("code", body.Code), elog.String("msg", body.Msg))
		return 0, nil, fmt.Errorf("eiam 租户详情返回业务错误: code=%d msg=%s", body.Code, body.Msg)
	}

	// 空字段按零值语义放行（配额 0 = 无限制、白名单空 = 全部允许），
	// 由调用方统一裁决，这里只做透传
	return body.Data.MaxAccounts, body.Data.AllowedVendors, nil
}
