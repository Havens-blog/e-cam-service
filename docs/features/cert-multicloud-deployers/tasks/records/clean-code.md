---
status: "completed"
started: "2026-09-16 13:08"
completed: "2026-09-16 13:08"
time_spent: ""
---

# Task Record: T-clean-code Simplify and Clean Code

## Summary
cert-multicloud-deployers 代码清理：huawei/aws/azure 三云部署器逐字节重复的 withRetry/generateUploadName/account/指纹口径四处副本收敛为 deployer 包共享单点（新增 deployer_common.go：boundedRetry/formatUploadName/cloudAccountFor/certFingerprint64Pattern/unresolvedPlaceholderFingerprint），StartScan 重复文档注释合并；外部行为不变（错误文案逐字节保持，退避/换名重试语义原样），13 个 scope 文件审阅后判定已清洁跳过，vendor 类型化重复（resolveFingerprint/Stop）与 out-of-scope 文件（aliyun/tencent 部署器、cloudx 适配、并行会话 billing WIP）按 Balance 原则刻意不动

## Changes

### Files Created
- internal/cert/deployer/deployer_common.go

### Files Modified
- internal/cert/deployer/huawei_deployer.go
- internal/cert/deployer/aws_deployer.go
- internal/cert/deployer/azure_deployer.go
- internal/cert/service/reference_scan_service.go

### Key Decisions
- 共享收敛仅覆盖三云部署器（feature scope）：aliyun/tencent（5.4/5.5，非本 feature 变更文件）既有私有副本按 Hard AC 'No files cleaned outside scope' 保持原状，共享函数签名与其方法体逐字段对应留平滑收编缝
- formatUploadName 以 (prefix, maxLen) 参数化：tencent maxLen=50 与三云 63 不同，各云常量及其约束文档（SCM 3~63/ACM 标签 256/KV 127）留在原处不合并
- cloudAccountFor 错误文案用 domain.Cloud 常量值插值（huawei/aws/azure），与收敛前文案逐字节相同；测试断言的 'retries exhausted after 5 attempts' 等字符串经 boundedRetry 原样保留
- resolveFingerprint/resolveUncachedFingerprint/Stop 的三云重复为 vendor 类型驱动（huawei/aws/azure CloudCertRef 异形同构），Go 无结构化字段约束，统一需 getter/泛型闭包——按 Maintain Balance 判定过度抽象，保留
- 工作树中 internal/shared/cloudx/billing/* 与 log-query-optimization index.json 为并行会话未提交 WIP，非本 feature scope，零触碰

## Test Results
- **Tests Executed**: Yes
- **Passed**: 409
- **Failed**: 0
- **Coverage**: 87.6%

## Acceptance Criteria
- [x] Code simplified without changing external behavior
- [x] No files cleaned outside this feature's scope (git diff boundaries)

## Notes
scope 18 文件（feature-context：records 1~4 变更清单；git diff main...HEAD 为空——feature 已并入 main）。质量门：gofmt delta=0（5 文件 tr -d '\r' 判定）、go vet deployer+service 通过、go test -count=1 409 pass/0 fail（deployer 151+service 258，与任务 4 基线一致）、go build ./internal/cert/... 通过（-p 1 OOM 缓解）。coverage 87.6% 为 deployer 包（清理前同值）；service 包 76.2%（既有基线）。净变化：修改 4 文件 +45/-225，新增 deployer_common.go 110 行。justfile 不存在，门禁按 repo 先例映射 go vet/go test 执行。
