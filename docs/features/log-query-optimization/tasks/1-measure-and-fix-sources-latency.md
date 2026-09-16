---
id: "1"
title: "定位并修复 sources/search/aggregate 接口延迟(sources 实测 20s)"
priority: "P0"
estimated_time: "2h"
complexity: "medium"
dependencies: []
surface-key: ""
surface-type: ""
breaking: false
type: "coding.enhancement"
mainSession: false
---

# 1: 定位并修复 sources/search/aggregate 接口延迟(sources 实测 20s)

## Description
用户实测 `GET /logs/sources?log_type=waf` 接近 20 秒(历史曾优化到 ~0.37s,现退化)。先复现并区分"环境噪音(热重载窗口/冷缓存)"与"代码退化",再针对根因定向优化(阶段耗时埋点、并发化、进程级预热、缓存命中日志)。目标:三接口热缓存 <300ms、冷启动 <3s(AWS S3 前缀列举物理慢,单独报告不阻塞整体)。

## Reference Files
- `docs/proposals/log-query-optimization/proposal.md` — Source proposal;核心在 «Problem» «Risks» «Success Criteria»
- `e-cam-service/internal/shared/cloudx/logquery/aliyun/provider.go`: ListLogSources/域名枚举/并发
- `e-cam-service/internal/shared/cloudx/logquery/aliyun/domain_cache.go`: 进程级 10min 缓存(枚举/logstore)
- `e-cam-service/internal/shared/cloudx/logquery/aws/provider.go`: S3 前缀枚举/扫码(物理慢点)
- `e-cam-service/internal/logquery/service/federation.go`: ListSources 编排/并发

## Acceptance Criteria
- [ ] 复现 `GET /logs/sources?log_type=waf` 20s,并给出根因结论(环境噪音 vs 代码退化,至少二选一并附测量)
- [ ] 逐 provider 阶段耗时可见(ListSource 内部各源枚举耗时,日志或临时 curl 测量)
- [ ] 热缓存(10min 内)实测 `sources`/`search`/`aggregate` 均 <300ms
- [ ] 冷启动(清缓存后首次)实测 <3s;AWS S3 源若超时,单独记录耗时与原因,不阻塞整体 SC
- [ ] `go build ./internal/...` 与 `go test ./internal/shared/cloudx/logquery/...` 全绿
- [ ] 行为不变:源清单/域名枚举/资源过滤语义与当前一致(无新探测扫描量)

## Hard Rules
仅修改 e-cam-service 仓(本任务);SLS 检索|SQL 扫描量不放大(域名枚举命中缓存,不得新增每条请求的枚举 SQL);不得为提速破坏现有 ADR D4 采样/截断语义。

## Implementation Notes
- 复现协议:先连续请求两次区分热/冷;记录后端进程是否刚经历热重载(dev 服务重启窗口 ~20s 天然造成 502/慢)。
- 已知候选:S3 前缀 root list 实测 7s+、Akamai activeDomains SQL 每 ~1.5s;域名/前缀/logstore 缓存为进程级 10min,冷启动即全量重查。
- 定向手段(按测量选择,勿全套上):
  1. ListSources 各 provider 并行已被实现,检查是否退化(enumTask slots/goroutine 泄漏);
  2. sources 冷启动可加"首次串行预热+标注";三次内近源可合并(如 Akamai activeDomains 与探测量同窗);
  3. AWS 前缀发现走 10min 缓存已实现,冷启动无法避免时可返回已缓存过期的旧清单+后台刷新(标注 fresh 与否);
  4. 为 sources/search/aggregate 补缓存命中/阶段耗时 log 字段(不新增端点)。
- 验收用 curl -w 实测并记录在任务回复;不要凭感觉。