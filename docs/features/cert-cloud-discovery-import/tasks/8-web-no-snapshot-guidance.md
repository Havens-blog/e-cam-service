---
id: "8"
title: "前端：无快照引导流程"
priority: "P1"
estimated_time: "1h"
complexity: "medium"
dependencies: [3, 6]
surface-key: ""
surface-type: ""
breaking: false
type: "coding.feature"
mainSession: false
---

# 8: 前端：无快照引导流程

## Description

台账空 + 无扫描快照的冷启动引导：预览返回 NO_SNAPSHOT 时展示"先执行扫描"引导——触发既有扫描端点 → 轮询 GET /discovery/snapshot-status（running→done/failed）→ done 自动进入预览；failed 展示 partialFailures。不依赖单次长请求同步返回（避免多账号规模下网关/浏览器超时打断引导）。

## Reference Files
- `docs/proposals/cert-cloud-discovery-import/proposal.md` — Key Scenarios 无扫描快照条目、Success Criteria SC-3、Dependency Readiness 缺口 (1) (ref: Key Scenarios; Success Criteria; Dependency Readiness)
- `e-cam-web/src/views/cert/ledger/components/DiscoveryImportModal.vue`: NO_SNAPSHOT 分支引导视图（任务 6 暴露的触发点）
- `e-cam-web/src/views/cert/ledger/`: 扫描触发与 snapshot-status 轮询 API 封装

## Acceptance Criteria

- [ ] 预览返回 NO_SNAPSHOT 时展示"先执行扫描"引导（含说明文案与触发按钮），不展示错误堆栈
- [ ] 触发扫描后轮询 snapshot-status 端点（间隔轮询，running 态展示进行中），不依赖触发请求同步返回终态
- [ ] done → 自动拉取预览进入列表；failed → 展示 partialFailures 明细并提供重试入口
- [ ] vitest 用例覆盖：NO_SNAPSHOT 分支、running→done 收敛、failed 展示

## Implementation Notes

- 扫描触发沿用既有引用扫描端点（POST，同步至终态语义）——前端不等待其响应体终态，触发即转轮询（请求超时/中断由轮询兜底；后端 running 防重与 15 分钟超时恢复调度已存在）
- 轮询间隔与超时上限与任务 7 进度轮询保持同族配置
