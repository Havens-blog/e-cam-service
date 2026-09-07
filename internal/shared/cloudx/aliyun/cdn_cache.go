package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cdn"
	"github.com/gotomicro/ego/core/elog"
)

// GetCacheConfig 查询 CDN/DCDN 域名的缓存规则(set_ttl)。
// CDN 域名走 cdn API;非 CDN 域名(DCDN 全站加速)报错时回退 dcdn API。
func (a *CDNAdapter) GetCacheConfig(ctx context.Context, domainName, domainID string) ([]types.CDNCacheRule, error) {
	if domainName == "" {
		return nil, fmt.Errorf("阿里云CDN缓存配置需要域名")
	}

	rules, err := a.getCDNCacheConfig(domainName)
	if err != nil {
		a.logger.Debug("CDN缓存配置查询失败,尝试DCDN", elog.String("domain", domainName), elog.FieldErr(err))
		rules, err = a.getDCDNCacheConfig(domainName)
		if err != nil {
			return nil, err
		}
	}
	return rules, nil
}

// getCDNCacheConfig CDN 域名:DescribeCdnDomainConfigs FunctionNames=set_ttl
func (a *CDNAdapter) getCDNCacheConfig(domainName string) ([]types.CDNCacheRule, error) {
	client, err := a.createClient()
	if err != nil {
		return nil, err
	}

	request := cdn.CreateDescribeCdnDomainConfigsRequest()
	request.DomainName = domainName
	request.FunctionNames = "set_ttl"

	response, err := client.DescribeCdnDomainConfigs(request)
	if err != nil {
		return nil, fmt.Errorf("查询CDN域名配置失败: %w", err)
	}

	rules := make([]types.CDNCacheRule, 0)
	for _, cfg := range response.DomainConfigs.DomainConfig {
		if cfg.FunctionName != "set_ttl" {
			continue
		}
		if rule, ok := parseSetTTLArgs(cfg.FunctionArgs.FunctionArg); ok {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

// parseSetTTLArgs 解析 set_ttl 函数参数:
// file_type=文件类型(jpg,png / 0=全部) ttl=秒 weight=权重 mode=cache缓存/ignore不缓存
func parseSetTTLArgs(args []cdn.FunctionArg) (types.CDNCacheRule, bool) {
	path, ttlStr, weight, mode := "", "", 0, ""
	for _, arg := range args {
		switch arg.ArgName {
		case "file_type":
			path = arg.ArgValue
		case "ttl":
			ttlStr = arg.ArgValue
		case "weight":
			weight, _ = strconv.Atoi(arg.ArgValue)
		case "mode":
			mode = arg.ArgValue
		}
	}
	if path == "" && ttlStr == "" {
		return types.CDNCacheRule{}, false
	}

	ttl, _ := strconv.ParseInt(ttlStr, 10, 64)
	if mode == "ignore" {
		ttl = 0 // 不缓存
	}
	return types.CDNCacheRule{
		Path:     normalizeAliyunPath(path),
		Type:     classifyPath(path),
		TTL:      ttl,
		Priority: weight,
	}, true
}

// getDCDNCacheConfig DCDN 域名:DescribeDcdnDomainConfigs FunctionNames=set_ttl(通用请求)
func (a *CDNAdapter) getDCDNCacheConfig(domainName string) ([]types.CDNCacheRule, error) {
	client, err := sdk.NewClientWithAccessKey(a.defaultRegion, a.accessKeyID, a.accessKeySecret)
	if err != nil {
		return nil, err
	}

	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = "dcdn.aliyuncs.com"
	request.Version = "2018-01-15"
	request.ApiName = "DescribeDcdnDomainConfigs"
	request.QueryParams["DomainName"] = domainName
	request.QueryParams["FunctionNames"] = "set_ttl"

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		return nil, fmt.Errorf("查询DCDN域名配置失败: %w", err)
	}

	var resp struct {
		DomainConfigs struct {
			DomainConfig []struct {
				FunctionName string `json:"FunctionName"`
				FunctionArgs []struct {
					ArgName  string `json:"ArgName"`
					ArgValue string `json:"ArgValue"`
				} `json:"FunctionArgs"`
			} `json:"DomainConfig"`
		} `json:"DomainConfigs"`
	}
	if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
		return nil, fmt.Errorf("解析DCDN配置响应失败: %w", err)
	}

	rules := make([]types.CDNCacheRule, 0)
	for _, cfg := range resp.DomainConfigs.DomainConfig {
		if cfg.FunctionName != "set_ttl" {
			continue
		}
		args := make([]cdn.FunctionArg, 0, len(cfg.FunctionArgs))
		for _, arg := range cfg.FunctionArgs {
			args = append(args, cdn.FunctionArg{ArgName: arg.ArgName, ArgValue: arg.ArgValue})
		}
		if rule, ok := parseSetTTLArgs(args); ok {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

// normalizeAliyunPath file_type 为空或 "0" 表示全站
func normalizeAliyunPath(path string) string {
	if path == "" || path == "0" {
		return "*"
	}
	return path
}

// classifyPath 按路径形态归类匹配类型
func classifyPath(path string) string {
	if path == "" || path == "0" || path == "*" {
		return "all"
	}
	if strings.HasPrefix(path, "/") {
		return "directory"
	}
	return "file_ext"
}
