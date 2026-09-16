---
id: "5"
title: "明细区按云·账号折叠分组展示(组头含条数/耗时)"
priority: "P1"
estimated_time: "2h"
complexity: "medium"
dependencies: []
surface-key: ""
surface-type: ""
breaking: false
type: "coding.feature"
mainSession: false
---

# 5: 明细区按云·账号折叠分组展示(组头含条数/耗时)

## Description
多云明细(如 WAF 84 源)目前平铺混排,难以区分来源。改为:明细表上方按「云 · 账号」分组头(默认折叠或展开可切换),组头显示账号名、该组条数(sum 样本内按 meta 过滤)、实时/失败状态与耗时(sources strip 数据可复用于组头);组内仍是统一时间倒序明细。不改变明细数据来源与排序。

## Reference Files
- `docs/proposals/log-query-optimization/proposal.md` — Source proposal;«Proposed Solution» «Key Scenarios»
- `e-cam-web/src/views/logs/index.vue`: allEntries/resp.sources/detailVisible 明细区
- `e-cam-web/src/views/logs/components/LogDetailDrawer.vue`(不改,参考 entry.meta 结构)

## Acceptance Criteria
- [ ] 明细按「云 · 账号」分组,组头:账号展示名 + 组内条数 + 该组源状态(失败/耗时,来自 resp.sources)
- [ ] 组头可折叠/展开全部(默认全展开;提供「全部折叠/全部展开」)
- [ ] 组内明细保持统一时间倒序且与现状一致(翻页追加仍全局倒序展示)
- [ ] 单源失败不整组报错:失败源在组头标注并可展开看该源错误
- [ ] vue-tsc / eslint 通过;分页(加载更早/回到最新)、统计图不受影响

## Hard Rules
不改后端响应结构;明细仍为全字段树(Drawer 结构不变)。

## Implementation Notes
- 分组键 = `meta.cloud + meta.account_name`;组数据 = `allEntries.filter`。
- 保留时间倒序:组内直接使用 allEntries 过滤(顺序天然保持)。
- 「全部折叠/展开」状态设一个 ref;组头用按钮非表格行,避免与行点击开详情冲突。