---
feature: "ssl-cert-management"
created: "2026-08-14"
status: prd
---

# Feature: ssl-cert-management

<!-- Status flow: prd → design → tasks → in-progress → completed -->

## Documents

| Document | Path | Summary |
|----------|------|---------|
| PRD Spec | prd/prd-spec.md | 证书托管/引用发现/到期监控/一键批量更换的完整需求规格（含流程图、数据流、安全需求与评估遗留项消解） |
| User Stories | prd/prd-user-stories.md | 6 个故事覆盖三角色：导入、引用查看、批量更换、存量导入、审计配置、到期看板 |
| UI Functions | prd/prd-ui-functions.md | 5 个 UI 功能（台账/引用关系/到期看板/变更向导与管理/全局配置），web 平台 6 个新页面 |

## Traceability

| PRD Section | Design Section | UI Component | Tasks |
|-------------|----------------|--------------|-------|
| 证书托管台账（In Scope 1-2） | — | UF-1 | — |
| 完整性检查（In Scope 3） | — | UF-1 | — |
| 引用关系发现（In Scope 4） | — | UF-2 | — |
| 到期监控与告警（In Scope 5） | — | UF-3, UF-5 | — |
| TLS 主动探测（In Scope 6） | — | UF-3, UF-5 | — |
| 一键批量更换（In Scope 7-9） | — | UF-4 | — |
| 部署器与映射表（In Scope 10） | — | — | — |
| 执行通道抽象（In Scope 11） | — | — | — |
| 权限与审计（In Scope 12） | — | 全部 UF | — |
| 前端页面（In Scope 13） | — | UF-1~5 | — |
