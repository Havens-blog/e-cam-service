package main

// cert 出站端口的远程 HTTP 适配器（cert 服务抽取 · "远程 adapter" + feature flag）。
//
// 目标原文要求 cert 的出站依赖有"本地/远程 adapter"两种形态，默认走本地、灰度
// 切远程。本文件实现远程形态的消费侧 HTTP client，并提供按配置在 local/remote 间
// 选择的 selector（默认 local，fail-closed：选 remote 但缺 base_url 即拒启）。
//
// 关键边界：远程 adapter 只实现 cert 消费侧的 HTTP client，不在 account/asset/audit
// 域单方面焊接生产方端点。生产方端点由各域团队按本文件 client 固定的内部契约
// （path/query/JSON 形状，见各 adapter 注释 + remote_test.go 的 httptest 往返）实现。
// 契约在消费侧由往返测试锁死；真实生产方上线后必须匹配同一契约，否则 cert 远程
// 模式在集成环境即失败——与 CMDB 两侧快照同形的消费侧契约守护。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"bytes"
	"io"
	"strconv"
	"strings"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	assetdao "github.com/Havens-blog/e-cam-service/internal/asset/repository/dao"
	auditdao "github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	auditservice "github.com/Havens-blog/e-cam-service/internal/audit/service"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
)

// defaultRemoteTimeout 远程上游调用超时（覆盖率分母等只读查询，不宜长阻塞）。
const defaultRemoteTimeout = 10 * time.Second

// upstreamConfig 单个出站端口的形态选择。
type upstreamConfig struct {
	Mode    string `mapstructure:"mode"`     // "local"（默认）| "remote"
	BaseURL string `mapstructure:"base_url"` // mode=remote 时必填
	// AllowInsecure 仅对传输敏感数据（如云账号凭据）的端口有意义：显式置 true
	// 才允许 remote 模式走明文 http（例如 service-mesh 内已有 mTLS 的场景）。
	// 缺省 false：携带凭据的端口 remote 模式强制 https，拒绝明文传输 AK/SK。
	AllowInsecure bool `mapstructure:"allow_insecure"`
}

// resolveUpstream 读取 cert-server.upstream.<name>；缺省视为 local。
func resolveUpstream(name string) (upstreamConfig, error) {
	var cfg upstreamConfig
	key := "cert-server.upstream." + name
	if err := viper.UnmarshalKey(key, &cfg); err != nil {
		return cfg, fmt.Errorf("read %s config: %w", key, err)
	}
	if cfg.Mode == "" {
		cfg.Mode = "local"
	}
	switch cfg.Mode {
	case "local":
		return cfg, nil
	case "remote":
		if cfg.BaseURL == "" {
			// fail-closed：声明走远程却没有上游地址，拒绝静默回退本地。
			return cfg, fmt.Errorf("%s.mode=remote 但 base_url 为空（fail-closed：不静默回退本地）", key)
		}
		return cfg, nil
	default:
		return cfg, fmt.Errorf("%s.mode 非法: %q（仅支持 local|remote）", key, cfg.Mode)
	}
}

// ---- InstanceCounter 远程适配器 ----
//
// 内部契约：
//
//	GET {base_url}/internal/v1/instances/count?modelUid=<uid>
//	200 -> {"count": <int64>}
//
// 背后是 asset 域已存在的 InstanceRepository.Count(InstanceFilter{ModelUID})，
// 生产方只需把该方法包一层 HTTP handler 即满足。
type remoteInstanceCounter struct {
	baseURL string
	client  *http.Client
}

func newRemoteInstanceCounter(baseURL string) remoteInstanceCounter {
	return remoteInstanceCounter{
		baseURL: baseURL,
		client:  &http.Client{Timeout: defaultRemoteTimeout},
	}
}

func (r remoteInstanceCounter) CountByModelUID(ctx context.Context, modelUID string) (int64, error) {
	u := r.baseURL + "/internal/v1/instances/count?modelUid=" + url.QueryEscape(modelUID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, fmt.Errorf("remote instance-counter: build request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("remote instance-counter: call upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("remote instance-counter: upstream status %d", resp.StatusCode)
	}
	var body struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("remote instance-counter: decode response: %w", err)
	}
	return body.Count, nil
}

// selectInstanceCounter 按 cert-server.upstream.instance_counter 在 local/remote
// 间选择（默认 local）。local 分支用同库 asset 仓储；remote 分支用 HTTP client。
func selectInstanceCounter(db *mongox.Mongo) (certservice.InstanceCounter, error) {
	cfg, err := resolveUpstream("instance_counter")
	if err != nil {
		return nil, err
	}
	if cfg.Mode == "remote" {
		return newRemoteInstanceCounter(cfg.BaseURL), nil
	}
	return localInstanceCounter{repo: assetrepo.NewInstanceRepository(assetdao.NewInstanceDAO(db))}, nil
}

// ---- CloudAccountLister 远程适配器 ----
//
// 安全敏感：CloudAccount 携带 AccessKeyID/AccessKeySecret（云账号凭据）。远程化
// 意味着凭据跨 HTTP 内部跳转，故 selector 施加 https 安全闸（secure-by-default）：
// remote 模式默认强制 https，除非显式 allow_insecure=true（如 mesh 内已有 mTLS）。
//
// 线上类型直接复用共享 SDK 的 sharedomain.CloudAccount（cert 与 account 两侧共用
// 同一带 json tag 的已发布类型），故零翻译 DTO、无跨仓字段漂移风险——这是三个
// 远程端口里契约最稳的一个。内部契约：
//
//	GET {base}/internal/v1/cloud-accounts?status=&provider=&environment=&tenantId=&offset=&limit=
//	200 -> {"accounts": [CloudAccount...], "total": <int64>}
type remoteCloudAccountLister struct {
	baseURL string
	client  *http.Client
}

func newRemoteCloudAccountLister(baseURL string) remoteCloudAccountLister {
	return remoteCloudAccountLister{
		baseURL: baseURL,
		client:  &http.Client{Timeout: defaultRemoteTimeout},
	}
}

func (r remoteCloudAccountLister) List(ctx context.Context, filter sharedomain.CloudAccountFilter) ([]sharedomain.CloudAccount, int64, error) {
	q := url.Values{}
	if filter.Provider != "" {
		q.Set("provider", string(filter.Provider))
	}
	if filter.Environment != "" {
		q.Set("environment", string(filter.Environment))
	}
	if filter.Status != "" {
		q.Set("status", string(filter.Status))
	}
	if filter.TenantID != 0 {
		q.Set("tenantId", strconv.FormatInt(filter.TenantID, 10))
	}
	if filter.Offset != 0 {
		q.Set("offset", strconv.FormatInt(filter.Offset, 10))
	}
	if filter.Limit != 0 {
		q.Set("limit", strconv.FormatInt(filter.Limit, 10))
	}
	u := r.baseURL + "/internal/v1/cloud-accounts"
	if encoded := q.Encode(); encoded != "" {
		u += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("remote cloud-account-lister: build request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("remote cloud-account-lister: call upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("remote cloud-account-lister: upstream status %d", resp.StatusCode)
	}
	var body struct {
		Accounts []sharedomain.CloudAccount `json:"accounts"`
		Total    int64                      `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, 0, fmt.Errorf("remote cloud-account-lister: decode response: %w", err)
	}
	return body.Accounts, body.Total, nil
}

// selectCloudAccountLister 按 cert-server.upstream.cloud_account_lister 在
// local/remote 间选择（默认 local）。remote 模式施加 https 安全闸：凭据端口
// 不得走明文 http，除非 allow_insecure=true。
func selectCloudAccountLister(db *mongox.Mongo) (certservice.CloudAccountLister, error) {
	cfg, err := resolveUpstream("cloud_account_lister")
	if err != nil {
		return nil, err
	}
	if cfg.Mode == "remote" {
		if err := requireSecureForCredentials("cloud_account_lister", cfg); err != nil {
			return nil, err
		}
		return newRemoteCloudAccountLister(cfg.BaseURL), nil
	}
	return accountrepo.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(db)), nil
}

// requireSecureForCredentials 对传输云账号凭据的端口施加 https 安全闸：remote
// 模式的 base_url 必须是 https，除非显式 allow_insecure=true。fail-closed：
// 默认拒绝明文传输 AK/SK。
func requireSecureForCredentials(name string, cfg upstreamConfig) error {
	if cfg.AllowInsecure {
		return nil
	}
	if !strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://") {
		return fmt.Errorf("cert-server.upstream.%s.mode=remote 传输云账号凭据(AK/SK)，base_url 必须为 https（当前 %q）；如上游在 mTLS 服务网格内可显式设 allow_insecure=true",
			name, cfg.BaseURL)
	}
	return nil
}

// ---- ChangeAuditStore 远程适配器 ----
//
// 比 InstanceCounter 实质复杂：含写路径 + 幂等语义 + 嵌套载荷（*bool / []string）
// 的 JSON 往返。内部契约：
//
//	POST {base}/internal/v1/change-audit            body=auditEntryDTO            -> 200
//	POST {base}/internal/v1/change-audit/dedup      body=auditEntryDTO            -> {"inserted": <bool>}
//	GET  {base}/internal/v1/change-audit?orderId=<id>                             -> {"entries": [auditEntryDTO...]}
//	GET  {base}/internal/v1/change-audit/orphan-cleanup?orderId=<id>             -> {"entries": [auditEntryDTO...]}
//	GET  {base}/internal/v1/change-audit/unmet-domains?orderId=<id>             -> {"domains": [<string>...]}
//
// 背后是 audit 域已存在的 ChangeOrderAuditService；生产方把其方法包成上述 handler
// 即满足。写路径与 dedup 幂等由 remote_test.go 的 httptest 往返锁死。
type auditEntryDTO struct {
	OrderID      string   `json:"orderId"`
	ItemID       string   `json:"itemId"`
	Actor        string   `json:"actor"`
	Action       string   `json:"action"`
	Detail       string   `json:"detail"`
	At           int64    `json:"at"`
	Cloud        string   `json:"cloud,omitempty"`
	CloudCertID  string   `json:"cloudCertId,omitempty"`
	OrphanAction string   `json:"orphanAction,omitempty"`
	Success      *bool    `json:"success,omitempty"`
	UnmetDomains []string `json:"unmetDomains,omitempty"`
	DedupKey     string   `json:"dedupKey,omitempty"`
}

func toAuditDTO(e certservice.ChangeAuditEntry) auditEntryDTO {
	return auditEntryDTO{
		OrderID: e.OrderID, ItemID: e.ItemID, Actor: e.Actor, Action: e.Action,
		Detail: e.Detail, At: e.At, Cloud: e.Cloud, CloudCertID: e.CloudCertID,
		OrphanAction: e.OrphanAction, Success: e.Success, UnmetDomains: e.UnmetDomains,
		DedupKey: e.DedupKey,
	}
}

func fromAuditDTO(d auditEntryDTO) certservice.ChangeAuditEntry {
	return certservice.ChangeAuditEntry{
		OrderID: d.OrderID, ItemID: d.ItemID, Actor: d.Actor, Action: d.Action,
		Detail: d.Detail, At: d.At, Cloud: d.Cloud, CloudCertID: d.CloudCertID,
		OrphanAction: d.OrphanAction, Success: d.Success, UnmetDomains: d.UnmetDomains,
		DedupKey: d.DedupKey,
	}
}

func fromAuditDTOs(ds []auditEntryDTO) []certservice.ChangeAuditEntry {
	out := make([]certservice.ChangeAuditEntry, len(ds))
	for i, d := range ds {
		out[i] = fromAuditDTO(d)
	}
	return out
}

type remoteAuditStore struct {
	baseURL string
	client  *http.Client
}

func newRemoteAuditStore(baseURL string) remoteAuditStore {
	return remoteAuditStore{
		baseURL: baseURL,
		client:  &http.Client{Timeout: defaultRemoteTimeout},
	}
}

// doJSON 发起请求并把 200 响应体解码进 out（out 为 nil 时丢弃响应体）。
func (r remoteAuditStore) doJSON(ctx context.Context, method, u string, in, out any) error {
	var bodyReader io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("call upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (r remoteAuditStore) Record(ctx context.Context, entry certservice.ChangeAuditEntry) error {
	return r.doJSON(ctx, http.MethodPost, r.baseURL+"/internal/v1/change-audit", toAuditDTO(entry), nil)
}

func (r remoteAuditStore) RecordDedup(ctx context.Context, entry certservice.ChangeAuditEntry) (bool, error) {
	var body struct {
		Inserted bool `json:"inserted"`
	}
	if err := r.doJSON(ctx, http.MethodPost, r.baseURL+"/internal/v1/change-audit/dedup", toAuditDTO(entry), &body); err != nil {
		return false, err
	}
	return body.Inserted, nil
}

func (r remoteAuditStore) ListByOrder(ctx context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	return r.listEntries(ctx, "/internal/v1/change-audit", orderID)
}

func (r remoteAuditStore) ListOrphanCleanupResults(ctx context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	return r.listEntries(ctx, "/internal/v1/change-audit/orphan-cleanup", orderID)
}

func (r remoteAuditStore) listEntries(ctx context.Context, path, orderID string) ([]certservice.ChangeAuditEntry, error) {
	u := r.baseURL + path + "?orderId=" + url.QueryEscape(orderID)
	var body struct {
		Entries []auditEntryDTO `json:"entries"`
	}
	if err := r.doJSON(ctx, http.MethodGet, u, nil, &body); err != nil {
		return nil, err
	}
	return fromAuditDTOs(body.Entries), nil
}

func (r remoteAuditStore) ListUnmetDomains(ctx context.Context, orderID string) ([]string, error) {
	u := r.baseURL + "/internal/v1/change-audit/unmet-domains?orderId=" + url.QueryEscape(orderID)
	var body struct {
		Domains []string `json:"domains"`
	}
	if err := r.doJSON(ctx, http.MethodGet, u, nil, &body); err != nil {
		return nil, err
	}
	return body.Domains, nil
}

// selectAuditStore 按 cert-server.upstream.audit_store 在 local/remote 间选择
// （默认 local）。local 分支用同库 audit 服务（含索引初始化）；remote 分支用
// HTTP client。
func selectAuditStore(db *mongox.Mongo, logger *elog.Component) (certservice.ChangeAuditStore, error) {
	cfg, err := resolveUpstream("audit_store")
	if err != nil {
		return nil, err
	}
	if cfg.Mode == "remote" {
		return newRemoteAuditStore(cfg.BaseURL), nil
	}
	auditDAO := auditdao.NewChangeOrderAuditDAO(db)
	if err := auditDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("cert-server: 审计索引初始化失败（仅告警，不阻断启动）", elog.FieldErr(err))
	}
	return localAuditStore{svc: auditservice.NewChangeOrderAuditService(auditDAO, logger)}, nil
}
