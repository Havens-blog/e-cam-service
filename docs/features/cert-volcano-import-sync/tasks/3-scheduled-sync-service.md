---
id: "3"
title: "多云定时增量同步服务"
priority: "P0"
estimated_time: "2h"
complexity: "high"
dependencies: ["2"]
surface-key: ""
surface-type: "api"
breaking: false
type: "coding.feature"
mainSession: false
---

# 3: 多云定时增量同步服务

## Description

同步服务 = 定时轮与手工触发共用的核心：枚举全部证书可达云（volcano + aliyun/tencent/huawei/aws/azure）× active 账号 → 适配器 List → 指纹比对台账 → **增量**处理（未入账指纹才导入、已映射跳过、映射缺失/漂移补刷），把"云端证书库是台账唯一真源"变成定时纪律。冲突策略（brainstorm 已确认「幂等优先+守卫+漂移留痕」）在本任务实现并测试。

## Reference Files

- `docs/proposals/cert-volcano-import-sync/proposal.md` — Proposed Solution / 冲突策略表 / Key Risks / Success Criteria
- `internal/cert/service/discovery_import_service.go` — ImportFromDiscovery 幂等管线（ALREADY_IN_LEDGER / ErrDuplicateFingerprint / 会话记录）
- `internal/cert/service/reference_scan_service.go` — `accountScanSource.ActiveByCloud` 逐云账号枚举模式
- `internal/cert/repository/cloud_cert_mapping.go` — Upsert（uk_fp_cloud_account）/ FindByCloudCertID（uploadedAt 降序取最新）语义

## Acceptance Criteria

- [ ] 枚举全部证书可达云 × active 账号（含火山）；单云单账号失败经 errorReason 隔离、不中断其余（会话终态 partial_failed 语义）
- [ ] 指纹不在台账 → 生成导入条目经既有幂等管线入账（台账 + 映射 active），会话 `operator="scheduler"`
- [ ] 指纹已在台账且 (cloud,accountKey,cloudCertID) 映射完整 → 跳过且**不调用 Get**（断言 Get 调用计数为 0）
- [ ] 映射缺失 → 补建；同 cloudCertID 新指纹（漂移）→ 新映射刷新 + 旧映射留痕（`FindByCloudCertID` 取最新指纹）
- [ ] 并发双会话导入同一指纹 → 台账恰 1 条、双会话均无 failed（`ErrDuplicateFingerprint` 归 success，幂等语义）
- [ ] 空台账首轮同步 = 全量回填（fake 云全部 List 实例入账且映射完整）

## Hard Rules

- 只读纪律：同步路径只调用云侧 List/Get 读方法，**绝不调用任何云写方法**（单测/审查断言）。
- 整体限时复用 `discoveryImportTimeout` 语义；到期剩余条目记超时失败因，不悬挂。
- 不修改 mapping 表结构、不引入新幂等机制——全部复用既有仓储语义。

## Implementation Notes

### 冲突策略实现要点

- **竞态**：无线程锁跨进程——依赖指纹唯一键兜底：双方 GetByFingerprint 未命中 → 双 Create → 一方 `ErrDuplicateFingerprint` → 该条目记 success（reason=ALREADY_IN_LEDGER 语义）。
- **漂移留痕**：Upsert 唯一键 (新fp, cloud, account) 自建新行；旧行不删；断言 `FindByCloudCertID(cloud, account, cloudCertID)` 返回新指纹。
- **防重守卫**：同步入口 CAS（atomic bool，对齐 probe `probeRunning`），running 中二次触发返回 `ErrSyncRunning`（Task 4 断言依赖此语义）。
- **增量判定**：以 List 层元数据（cloudCertID/指纹/状态）与「台账指纹 + 现有映射」比对，命中即 skip（不 Get）。

### File Scope

新增：`internal/cert/service/cert_sync_service.go`、`cert_sync_service_test.go`（fake 全部适配器 + fake 台账/映射仓储，复用 certtest）。
修改：`internal/cert/module.go`（服务装配暴露）、必要时 `internal/cert/service/discovery_import_service.go`（仅暴露既有幂等语义所需方法/哨兵，不改变行为）。

### Test Impact

- 新增同步服务单测（增量/幂等/漂移/首run 回填/并发竞态）。
- 现有多云部署器与发现导入测试全绿回归（本任务不改既有行为）。风险 low。