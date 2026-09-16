---
status: "completed"
started: "2026-09-16 09:55"
completed: "2026-09-16 10:33"
time_spent: "~38m"
---

# Task Record: 1 华为云部署器：SCM 证书库 + CDN/WAF/ALB/NLB 四产品绑定

## Summary
实现华为云五方法 CloudDeployer（HuaweiDeployer）+ cloudx 完整证书适配器（huawei.CertAdapter）：证书库=SCM（ImportCertificate 上传/ShowCertificate 详情/ExportCertificate 指纹导出/DeleteCertificate 清理），产品路由 cdn（UpdateDomainMultiCertificates，certificate_type=2 SCM 托管+cert_name 经 SCM 解析）/waf（ListHost hostname 过滤+精确匹配定位实例 ID → UpdateHost）/alb+nlb（ShowListener 跨地域定位+SNI 保留 → UpdateListener）。部署器层含限流有界退避（次数+总时长双闸）、上传名冲突换名重试、ecam-{fp8}-{unix秒}-{rand} ≤63 唯一上传名、ListReferences 三级指纹解析（映射反查→GetCert SHA256 对齐→确定性占位）、GetCert/CleanupOrphan（已删除幂等成功）；经 5.3 CloudAPIChannel 端到端验证两段式成功/二段失败补偿/映射 active→orphan。50 个新单元测试（28 adapter fake SDK + 22 deployer）全绿，cert 域全部套件无回归。

## Changes

### Files Created
- internal/shared/cloudx/huawei/cert.go
- internal/shared/cloudx/huawei/cert_test.go
- internal/cert/deployer/huawei_deployer.go
- internal/cert/deployer/huawei_deployer_test.go

### Files Modified
- internal/shared/cloudx/huawei/cert_discovery.go

### Key Decisions
- 新增 huawei.CertAdapter（内嵌 *CertDiscoveryAdapter 复用 ListReferences/限流/工厂设施，覆盖三个写方法与 GetCert 为真实云调用），保留 discovery-only 适配器并存——只读哨兵约束由发现适配器类型自身承载，Task 4 装配切换（扫描适配与部署器共享 CertAdapter 实例，aliyun 模式）
- GetCert 经 SCM ExportCertificate 导出材料解析叶证书 SHA-256（64hex 对齐台账口径）：回滚前置三判定（rollback_service precheckTargets）对指纹做硬等值比对，仅靠 ShowCertificate 原生 SHA-1 指纹会让华为云回滚恒被阻断；导出失败降级 SHA-1（上层按无法复核处理，回滚 fail-safe 阻断不误判有效）；导出响应含私钥字段——本层永不读取该字段，原始字节副本净化后即刻归零
- ELB 监听证书 ID 形态归一化=幂等透传：华为云 SCM 为全局服务、证书 ID 全局唯一，无 aliyun {certId}-{region} 式地域后缀形态；归一化以单点接缝函数承载（normalizeHuaweiListenerCertID），实网复核若出现形态差异在此扩展
- UploadCert DuplicateCheck=false（与腾讯 Repeatable=true 同口径：每次更换独立云证书副本，孤儿清理按 CloudCertMapping 归属收敛）；上传名冲突换名重试分支按任务 AC 要求保守保留（SCM name 非唯一键，常态休眠）
- CDN/WAF 绑定必填 certificatename——经 SCM ShowCertificate 由证书 ID 解析名称；发现侧 CDN 引用以证书名标识（无独立证书 ID），回滚旧引用若为证书名空间则 SCM 详情不存在 → 显式失败不猜测（proposal 显式失败原则），登记为已知限制
- module.go/changelist_generator.go 不动：RegisterDeployer 装配与 discoveryOnlyClouds 移除归 Task 4（breaking 标记在 Task 4），本任务产物仅测试消费

## Test Results
- **Tests Executed**: Yes
- **Passed**: 171
- **Failed**: 0
- **Coverage**: 87.0%

## Acceptance Criteria
- [x] UploadCert：上传证书束至华为云 SCM 返回 SCM 证书 ID；上传名唯一生成（ecam-{指纹前8}-{unix秒}-{随机}，≤63 字符）；私钥明文仅内存、用后 Zeroize
- [x] BindResource：按产品路由绑定——CDN 加速域名、WAF 域名、ELB（ALB/NLB 监听）证书引用；ELB 监听证书 ID 形态归一化（幂等透传，SCM ID 无地域后缀形态）
- [x] GetCert：SCM 证书在库状态（存在性 + SHA256 指纹对齐口径）+ 回滚目标有效性校验
- [x] CleanupOrphan：SCM 孤儿证书删除（已删除幂等成功）
- [x] ListReferences：复用发现适配引用形态 + 指纹解析（映射反查 → GetCert fallback → 确定性占位指纹）
- [x] 单元测试：fake SCM/CDN/WAF/ELB SDK 覆盖五方法 × 四产品（含限流退避分支、上传名冲突换名、绑定失败补偿路径）

## Notes
coverage=87.0% 为 internal/cert/deployer 包整体；internal/shared/cloudx/huawei 包任务核心文件 cert.go 86.3%（包整体 14.0%，因包含 ~25 个非本任务范围的既有 asset 适配文件）；两包均超 80% 目标。testsPassed=171 为两触达包全部通过测试数（含既有套件），本任务新增 50 个（28 adapter + 22 deployer，含 CloudAPIChannel 端到端）。宿主限制：-race 不可用（无 cgo/gcc）、staticcheck 不可用（构建版本 go1.24.1 vs 模块要求 go1.25.5），lint 以 go vet 承接（EXIT 0）；go build -p 1 ./... EXIT 0；gofmt 全部触达文件干净；cert 域全部套件（domain/repository/service/web/scheduler/certtest/deployer/huawei）绿。未实网确认项（活体验证归验收阶段，登记 poc 检查点语义）：WAF ListHost hostname 过滤精确语义（适配层已做精确匹配兜底）、SCM name 唯一性（换名分支保守保留）、SCM ExportCertificate 对全部 UPLOAD 证书的可导出性（失败已降级 fail-safe）、华为 SCM/CDN/WAF/ELB 绑定 API 的活体行为。
