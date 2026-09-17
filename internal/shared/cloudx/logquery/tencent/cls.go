// CLS(腾讯云日志服务)查询辅助:client 按区域缓存、原始日志分页拉取、分析
// (检索|SQL)结果解析。EdgeOne 访问日志投递 CLS topic,Search/Aggregate
// 均走 CLS 检索分析下推(实测 cast/group by/sum/avg/approx_percentile 可用)。
package tencent

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cls "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cls/v20201016"
)

// clsClient 返回区域 CLS client(进程内缓存,client 无状态可复用)。
func (p *provider) clsClient(region string) (*cls.Client, error) {
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	if p.clients == nil {
		p.clients = make(map[string]*cls.Client)
	}
	if c, ok := p.clients[region]; ok {
		return c, nil
	}
	cred := common.NewCredential(p.account.AccessKeyID, p.account.AccessKeySecret)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "cls.tencentcloudapi.com"
	c, err := cls.NewClient(cred, region, cpf)
	if err != nil {
		return nil, fmt.Errorf("tencent cls: new client %s: %w", region, err)
	}
	p.clients[region] = c
	return c, nil
}

// clsAnalysis 分析查询(检索|SQL):返回 AnalysisResults 逐行 KV(from/to 为毫秒)。
// CLS 分析行 = LogItems{Data:[{Key,Value}]};limit 为返回行数上限。
func (p *provider) clsAnalysis(ctx context.Context, region, topicID string, fromMs, toMs int64, sql string, limit int) ([]map[string]string, error) {
	client, err := p.clsClient(region)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	req := cls.NewSearchLogRequest()
	req.TopicId = &topicID
	req.From = &fromMs
	req.To = &toMs
	req.Query = &sql
	req.Limit = int64Ptr(int64(limit))

	resp, err := client.SearchLogWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	body := resp.Response
	if body == nil {
		return nil, nil
	}
	var rows []map[string]string
	for _, lr := range body.AnalysisResults {
		if lr == nil {
			continue
		}
		row := make(map[string]string, len(lr.Data))
		for _, it := range lr.Data {
			if it == nil || it.Key == nil {
				continue
			}
			row[*it.Key] = valStr(it.Value)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// clsSearchLogs 分页拉取原始日志(单 topic,游标 Context 翻页至 limit)。
func (p *provider) clsSearchLogs(ctx context.Context, region, topicID string, fromMs, toMs int64, query string, limit int, meta logquery.LogMeta) ([]logquery.LogEntry, error) {
	client, err := p.clsClient(region)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	const pageSize = 100
	req := cls.NewSearchLogRequest()
	req.TopicId = &topicID
	req.From = &fromMs
	req.To = &toMs
	req.Query = &query

	var entries []logquery.LogEntry
	for len(entries) < limit {
		pg := pageSize
		if remaining := limit - len(entries); remaining < pg {
			pg = remaining
		}
		req.Limit = int64Ptr(int64(pg))
		resp, err := client.SearchLogWithContext(ctx, req)
		if err != nil {
			return nil, err
		}
		body := resp.Response
		if body == nil || len(body.Results) == 0 {
			break
		}
		for _, lr := range body.Results {
			if lr == nil {
				continue
			}
			raw := eoLogJsonMap(valStr(lr.LogJson))
			if len(raw) == 0 {
				continue
			}
			if raw["__TIMESTAMP__"] == "" && lr.Time != nil {
				raw["Time"] = fmt.Sprintf("%d", *lr.Time)
			}
			if e := eoLog(meta, raw); e != nil {
				entries = append(entries, e)
			}
		}
		if body.Context == nil || *body.Context == "" || (body.ListOver != nil && *body.ListOver) {
			break
		}
		req.Context = body.Context
	}
	return entries, nil
}

// eoProbeTopic 采样确认 topic 是否为 EdgeOne 访问日志(近 24h 拉 1 条,
// 含 RequestHost + EdgeResponseStatusCode 字段即判定;0 条/失败返回 false)。
func (p *provider) eoProbeTopic(ctx context.Context, region, topicID string) bool {
	client, err := p.clsClient(region)
	if err != nil {
		return false
	}
	now := time.Now().UnixMilli()
	req := cls.NewSearchLogRequest()
	req.TopicId = &topicID
	from := now - 24*3600*1000
	to := now
	req.From = &from
	req.To = &to
	one := int64(1)
	req.Limit = &one
	star := "*"
	req.Query = &star
	resp, err := client.SearchLogWithContext(ctx, req)
	if err != nil {
		return false
	}
	body := resp.Response
	if body == nil || len(body.Results) == 0 {
		return false
	}
	for _, lr := range body.Results {
		if lr == nil {
			continue
		}
		raw := eoLogJsonMap(valStr(lr.LogJson))
		if raw["RequestHost"] != "" && raw["EdgeResponseStatusCode"] != "" {
			return true
		}
	}
	return false
}

// int64Ptr *int64 指针(SDK 请求参数)。
func int64Ptr(v int64) *int64 { return &v }

// valStr 空指针兜底。
func valStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
