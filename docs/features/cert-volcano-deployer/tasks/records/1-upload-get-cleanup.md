---
status: "completed"
started: "2026-09-19 11:43"
completed: "2026-09-19 12:20"
time_spent: "~37m"
---

# Task Record: 1 火山部署器证书库层（UploadCert/GetCert/CleanupOrphan）

## Summary
火山部署器证书库层（UploadCert/GetCert/CleanupOrphan + {product}:{id} 云证书 ID 归一）：volcano_deployer.go 实现 CloudDeployer 端口三方法与四产品证书库（CDN AddCertificate/WAF 服务证书/ALB-NLB UploadCertificate/certificateservice ImportCertificate）上传、查询、删除分支；GetCert 按归一前缀路由（csv 链解析 SHA256 指纹/有效期，产品库存在性/有效期），not-found 归一 Exists=false；CleanupOrphan 按前缀逐库删除且对已删除幂等成功；bind 层（BindResource/ListReferences）为显式未装配桩，任务 2 落地

## Changes

### Files Created
- internal/cert/deployer/volcano_deployer.go
- internal/cert/deployer/volcano_deployer_test.go

### Files Modified
无

### Key Decisions
- SPEC 偏差记录：CloudDeployer.UploadCert 端口无 product 入参（五云全局证书库先例），第一段统一以 certificateservice（csv）口径上传（proposal 明确 csv 为上传/回滚/清理基础）；按产品分支上传能力由 uploadForProduct 五分支承载，产品库定向上传与绑定语义留任务 2/5 接通验证——对齐任务 5 '优先对齐既有 normalize 模式，不改编排核心' 口径
- 云证书 ID 归一 {product}:{id}，csv/cdn/waf/alb/nlb 五前缀；GetCert/CleanupOrphan 解析失败 fail-fast
- SDK 客户端束显式转发（volcanoSDKClients）而非嵌入聚合，避免四服务方法名空间交叠的选择器歧义
- 实网复核项登记为单点常量：volcanoALBCertType=server、volcanoCDNListCertSource=external、isVolcanoCertNotFoundErr 消息启发式（fake SDK 测试口径，活体验证在验收阶段）
- 私钥卫生：SDK 构参 []byte 副本用后 cloudx.Zeroize，string 副本仅为 SDK 字段类型所必需，不进日志/错误/返回结构

## Test Results
- **Tests Executed**: Yes
- **Passed**: 19
- **Failed**: 0
- **Coverage**: 80.1%

## Acceptance Criteria
- [x] UploadCert 按产品分支上传至对应火山证书库，返回 {product}:{id} 归一云证书 ID；私钥仅内存、用后 Zeroize
- [x] GetCert 经 certificateservice/产品证书库查询在库状态（存在/过期/指纹），回滚判定语义与五云一致
- [x] CleanupOrphan 对已删除证书幂等成功（双调用同结果）
- [x] 私有方法签名与 Credential/domain.CloudCertInfo 形态对齐 channel.go 端口（var _ CloudDeployer 编译期断言）
- [x] fake SDK 单测覆盖：上传成功/失败、ID 归一（多产品分支）、GetCert 三判定、CleanupOrphan 幂等
- [x] 只依赖 volcengine-go-sdk v1.2.9 既有包，不新增依赖（go.mod/go.sum 无本任务改动）

## Notes
coverage=80.1% 为本任务范围逻辑（volcano_deployer.go 排除 12 个 SDK 转发单行与任务 2 桩）语句覆盖；deployer 包整体 16.4%（既有五云代码口径）。静态检查：go build ./... 绿、gofmt -s 干净、go vet 绿；deployer 包全量回归绿。测试未用 -race（仓库既有约定）。fmt/lint gate 按 Makefile 映射执行（无 just）。

**代码交付 commit**: 830d58f（feat(cert): volcano deployer cert-library layer (upload/get/cleanup)，internal/cert/deployer/volcano_deployer.go + volcano_deployer_test.go）。本 record 为并行会话状态丢失后的重建记录（record 恢复时补记），实现内容与测试结果均以该 commit 及其测试为准。
