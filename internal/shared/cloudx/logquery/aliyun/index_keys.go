// logstore 索引字段探测:SLS 中 select/group by 某列要求该列开过 KV 分析
// 索引(GetIndex → keys)。透传自定义维度(如 user_agent)前先校验列在索引
// 集内,否则 Group By 报 400 "列不存在或非索引" —— 探测一次即可给用户
// 明确的"该 logstore 可用聚合字段清单",而不是盲试报错。
package aliyun

import (
	"sort"
	"strings"

	"github.com/gotomicro/ego/core/elog"
)

// indexKeysCache 进程级 logstore → 索引字段集缓存(与域名枚举同型复用:
// 字段集分钟级稳定,GetIndex 每次 ~数百 ms,聚合每请求都要用)。
var indexKeysCache = newDomainCache()

// storeIndexKeys 取 logstore 已建索引(可聚合)字段集,按名排序返回副本。
// GetIndex 失败(无读索引配置权限/未建索引)返回 nil —— 调用方降级为
// "透传放行,报错再显式 TopNSkipReason",不阻塞主流程。
func (p *provider) storeIndexKeys(region, project, logstore string) []string {
	key := region + "/" + project + "/" + logstore
	keys, _ := indexKeysCache.get(key, func() []string {
		idx, err := p.clientFor(region).GetIndex(project, logstore)
		if err != nil {
			p.logger.Debug("[logquery-aliyun] get store index failed",
				elog.String("project", project), elog.String("logstore", logstore), elog.FieldErr(err))
			return nil
		}
		if idx == nil || len(idx.Keys) == 0 {
			return nil
		}
		out := make([]string, 0, len(idx.Keys))
		for k := range idx.Keys {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	})
	return keys
}

// passthroughColumn 判定"透传"自定义维度并返回其列名:维度过白名单
// (dimColumnExpr 无映射)且本身是合法标识符时原样透传。映射列/函数表达式
// (如 CDNOffline host 的 regexp_extract)返回 ("", false) —— 跳过索引校验。
func passthroughColumn(kind mapperKind, dim string) (string, bool) {
	if dim == "" {
		return "", false
	}
	if _, mapped := dimColumnExpr[kind][dim]; mapped {
		return "", false
	}
	if identifierLike(dim) {
		return dim, true
	}
	return "", false
}

// indexHint 透传列不在索引集时的提示(含该 logstore 可用字段清单),
// 列可用或探测失败(keys=nil)时返回空。
func indexHint(col string, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	for _, k := range keys {
		if k == col {
			return "" // 已索引,可聚合
		}
	}
	const maxHint = 16
	limit := len(keys)
	if limit > maxHint {
		limit = maxHint
	}
	names := make([]string, 0, limit+1)
	for _, k := range keys[:limit] {
		names = append(names, k)
	}
	if len(keys) > maxHint {
		names = append(names, "…")
	}
	return "字段 " + col + " 未开启分析索引,该源不可聚合;可用字段: " + strings.Join(names, ", ")
}