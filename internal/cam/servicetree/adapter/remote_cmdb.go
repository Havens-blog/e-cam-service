// Package adapter 的远程实现：通过 HTTP 调用独立部署的 e-cmdb-service。
//
// 与 LocalCMDBAdapter 二选一：当 CMDB 拆分为独立服务后，在 wire 装配处把
// NewLocalCMDBAdapter 换成 NewRemoteCMDBAdapter 即可，servicetree 业务代码零改动。
//
// 端点契约与 e-cmdb-service 的 internal/web/internal_api.go 一一对应。
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/port"
)

// RemoteCMDBAdapter 远程 CMDB 适配器（HTTP 客户端）。
type RemoteCMDBAdapter struct {
	baseURL string // e-cmdb-service 基址，如 http://e-cmdb-service:8002
	client  *http.Client
}

// NewRemoteCMDBAdapter 创建远程 CMDB 适配器。
func NewRemoteCMDBAdapter(baseURL string) *RemoteCMDBAdapter {
	return &RemoteCMDBAdapter{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// ---- 跨服务 DTO：与 e-cmdb-service internal/web 的结构镜像 ----

type instanceDTO struct {
	ID         int64                  `json:"id"`
	ModelUID   string                 `json:"model_uid"`
	AssetID    string                 `json:"asset_id"`
	AssetName  string                 `json:"asset_name"`
	TenantID   int64                  `json:"tenant_id"`
	AccountID  int64                  `json:"account_id"`
	Attributes map[string]interface{} `json:"attributes"`
	CreateTime int64                  `json:"create_time"`
	UpdateTime int64                  `json:"update_time"`
}

type statsDTO struct {
	Total       int64          `json:"total"`
	ByAssetType []typeCountDTO `json:"by_asset_type"`
	ByProvider  []typeCountDTO `json:"by_provider"`
}

type typeCountDTO struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

const internalPrefix = "/api/v1/cmdb/internal"

func (a *RemoteCMDBAdapter) ListByIDs(ctx context.Context, ids []int64) ([]port.CMDBInstance, error) {
	var resp struct {
		Instances []instanceDTO `json:"instances"`
	}
	if err := a.postJSON(ctx, internalPrefix+"/instances/batch-get", map[string]any{"ids": ids}, &resp); err != nil {
		return nil, err
	}
	return toCMDBInstances(resp.Instances), nil
}

func (a *RemoteCMDBAdapter) ListUnbound(ctx context.Context, tenantID int64, offset, limit int64) ([]port.CMDBInstance, error) {
	var resp struct {
		Instances []instanceDTO `json:"instances"`
	}
	q := url.Values{}
	q.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("limit", strconv.FormatInt(limit, 10))
	if err := a.getJSON(ctx, internalPrefix+"/instances/unbound?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return toCMDBInstances(resp.Instances), nil
}

func (a *RemoteCMDBAdapter) CountUnbound(ctx context.Context, tenantID int64) (int64, error) {
	var resp struct {
		Count int64 `json:"count"`
	}
	q := url.Values{}
	q.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	if err := a.getJSON(ctx, internalPrefix+"/instances/unbound/count?"+q.Encode(), &resp); err != nil {
		return 0, err
	}
	return resp.Count, nil
}

func (a *RemoteCMDBAdapter) AggregateStatsByIDs(ctx context.Context, ids []int64) (*port.AssetStatsResult, error) {
	var resp statsDTO
	if err := a.postJSON(ctx, internalPrefix+"/stats/by-ids", map[string]any{"ids": ids}, &resp); err != nil {
		return nil, err
	}
	return toStatsResult(resp), nil
}

func (a *RemoteCMDBAdapter) AggregateAllStats(ctx context.Context, tenantID int64) (*port.AssetStatsResult, error) {
	var resp statsDTO
	q := url.Values{}
	q.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	if err := a.getJSON(ctx, internalPrefix+"/stats/all?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return toStatsResult(resp), nil
}

func (a *RemoteCMDBAdapter) AggregateUnboundStats(ctx context.Context, tenantID int64) (*port.AssetStatsResult, error) {
	var resp statsDTO
	q := url.Values{}
	q.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	if err := a.getJSON(ctx, internalPrefix+"/stats/unbound?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return toStatsResult(resp), nil
}

// ---- HTTP 辅助 ----

func (a *RemoteCMDBAdapter) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		return err
	}
	return a.do(req, out)
}

func (a *RemoteCMDBAdapter) postJSON(ctx context.Context, path string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return a.do(req, out)
}

func (a *RemoteCMDBAdapter) do(req *http.Request, out any) error {
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("调用 CMDB 服务失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CMDB 服务返回非 200: %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---- DTO → port 类型转换 ----

func toCMDBInstances(dtos []instanceDTO) []port.CMDBInstance {
	out := make([]port.CMDBInstance, len(dtos))
	for i, d := range dtos {
		out[i] = port.CMDBInstance{
			ID:         d.ID,
			ModelUID:   d.ModelUID,
			AssetID:    d.AssetID,
			AssetName:  d.AssetName,
			TenantID:   d.TenantID,
			AccountID:  d.AccountID,
			Attributes: d.Attributes,
			CreateTime: time.UnixMilli(d.CreateTime),
			UpdateTime: time.UnixMilli(d.UpdateTime),
		}
	}
	return out
}

func toStatsResult(d statsDTO) *port.AssetStatsResult {
	byType := make([]port.AssetTypeCount, len(d.ByAssetType))
	for i, item := range d.ByAssetType {
		byType[i] = port.AssetTypeCount{AssetType: item.Key, Count: item.Count}
	}
	byProvider := make([]port.ProviderCount, len(d.ByProvider))
	for i, item := range d.ByProvider {
		byProvider[i] = port.ProviderCount{Provider: item.Key, Count: item.Count}
	}
	return &port.AssetStatsResult{
		Total:       d.Total,
		ByAssetType: byType,
		ByProvider:  byProvider,
	}
}
