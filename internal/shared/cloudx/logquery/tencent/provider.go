// Package tencent 腾讯云日志查询 provider(Phase 4 接口占位)。
//
// Phase 0 实测(source-status.md):WAF(ap-shanghai)/ EdgeOne(ap-guangzhou)
// CLS topic 存在但 30 天 0 条,CDN 无离线日志包——数据源未流动。
// 本包仅注册占位 provider:sources 显式列出已知 topic 及未启用状态(引导
// 云侧修复投递);Search 返回明确错误(federation per-source 状态可见),
// 数据源修复后补 CLS 查询实现与 mapper(field-mapping.md §四)。
package tencent

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

func init() {
	logquery.RegisterProvider(domain.CloudProviderTencent, logquery.LogTypeWAF, newProvider(logquery.LogTypeWAF))
	logquery.RegisterProvider(domain.CloudProviderTencent, logquery.LogTypeCDN, newProvider(logquery.LogTypeCDN))
}

// stubSource 已知日志源(Phase 0 实测;数据源修复后由 CLS API 动态枚举替换)。
type stubSource struct {
	region     string
	topicID    string
	name       string
	logType    logquery.LogType
	reason     string
}

// stubs Phase 0 实测的 CLS topic(存在但无数据)。
var stubs = []stubSource{
	{
		region: "ap-shanghai", topicID: "a0bfd8ed-d7b1-480a-879b-3c143f7302b8",
		name: "WAF 访问日志", logType: logquery.LogTypeWAF,
		reason: "topic 存在但 30 天 0 条,需云侧确认 WAF 日志服务投递配置",
	},
	{
		region: "ap-guangzhou", topicID: "f9a20d7f-11cc-4e3c-bd42-7bff6bb20874",
		name: "EdgeOne 日志", logType: logquery.LogTypeCDN,
		reason: "topic 存在但 30 天 0 条,需云侧确认 EdgeOne 日志推送配置",
	},
}

// provider 腾讯云日志 provider(占位实现)。
type provider struct {
	logType logquery.LogType
	account *domain.CloudAccount
}

func newProvider(logType logquery.LogType) logquery.ProviderCreator {
	return func(account *domain.CloudAccount) (logquery.LogProvider, error) {
		return &provider{logType: logType, account: account}, nil
	}
}

// Cloud 实现 LogProvider。
func (p *provider) Cloud() domain.CloudProvider { return domain.CloudProviderTencent }

// LogType 实现 LogProvider。
func (p *provider) LogType() logquery.LogType { return p.logType }

// ListLogSources 列出已知 topic(Enabled=false + 原因引导)。
func (p *provider) ListLogSources(_ context.Context, _ *domain.CloudAccount) ([]logquery.LogSource, error) {
	var out []logquery.LogSource
	for _, s := range stubs {
		if s.logType != p.logType {
			continue
		}
		out = append(out, logquery.LogSource{
			Cloud:       domain.CloudProviderTencent,
			AccountID:   fmt.Sprintf("%d", p.account.ID),
			AccountName: p.account.Name,
			Region:      s.region,
			LogType:     s.logType,
			ResourceID:  s.topicID,
			Name:        s.name + " / " + s.topicID,
			Enabled:     false,
			Note:        s.reason,
		})
	}
	return out, nil
}

// Search 数据源未流动:返回明确错误(federation per-source 状态可见,
// UI 引导云侧修复,而非静默空结果)。
func (p *provider) Search(_ context.Context, _ *domain.CloudAccount, _ logquery.SearchParams) ([]logquery.LogEntry, error) {
	return nil, fmt.Errorf("tencent log delivery not flowing (Phase 0: topic exists, 30d no data); fix delivery in Tencent console first")
}
