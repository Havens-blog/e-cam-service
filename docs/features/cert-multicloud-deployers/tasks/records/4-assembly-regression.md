---
status: "completed"
started: "2026-09-16 12:01"
completed: "2026-09-16 12:51"
time_spent: "~50m"
---

# Task Record: 4 模块装配：三云 RegisterDeployer + discoveryOnlyClouds 移除 + 清单可执行回归

## Summary
模块装配收敛：module.go 对 huawei/aws/azure 三云 RegisterDeployer（产品集与扫描面一致：华为 cdn/waf/alb/nlb、AWS cdn/alb/nlb、Azure cdn/alb），三云完整 CertAdapter 与扫描适配共享实例（aliyun 模式）；NewHuaweiScanAdapter/NewAwsScanAdapter/NewAzureScanAdapter 签名从 *CertDiscoveryAdapter 改为完整 *CertAdapter（只读方法自内嵌提升，huawei GetCert 升级为完整适配 SHA-256 对齐口径）；changelist_generator 移除 discoveryOnlyClouds 名单与 reasonDiscoveryOnly 文案，assessChangeable 云通道恒可执行，唯一不可执行来源回归 K8s 管理权/托管资源（warningPartitionFmt 同步改写）。回归测试：新增 TestGenerateChangeList_MulticloudExecutable（9 个 cloud×product 组合全部 AutoChangeable=true、无 ERR_DISCOVERY_ONLY、全部 pending、无分区汇总），重写 TestGenerateChangeList_NonExecutablePartition（三云引用转为可执行，仅 K8s 管理权信号项 skipped）。

## Changes

### Files Created
无

### Files Modified
- internal/cert/module.go
- internal/cert/service/changelist_generator.go
- internal/cert/service/changelist_generator_test.go
- internal/cert/service/reference_scan_service.go

### Key Decisions
- 三云 CertAdapter 单实例构造后同时注入 RegisterDeployer 与 NewXxxScanAdapter（任务书'与部署器共享实例，aliyun 模式'）；aliyun/tencent 装配行保持逐字不动（回归锁定 Hard Rule）
- Azure NewCertAdapter 不传 vault Option：装配层无 KV 配置来源，按设计优雅降级为 env（AZURE_KEY_VAULT_NAME/URI）回退，上传路径显式报错、发现/绑定/清理不受影响（任务 3 既定模式）
- discovery 导入服务（NewXxxDiscoveryCertAdapter）维持 CertDiscoveryAdapter 只读装配不动——其 discovery-only 语义仍成立，非本任务发现装配面
- Stop() 透传无需装配层接线：三云部署器 Stop() 经接口断言透传导配层限流器（cloudx 三云无令牌协程 → no-op），与 aliyun/tencent 既有口径一致（module 层无 shutdown hook）
- web/execute 测试中残留的 'ERR_DISCOVERY_ONLY' 字符串为任意 fixture 文案（持久化项 reason 透传断言），与静态名单无耦合，按 Surgical Changes 不动

## Test Results
- **Tests Executed**: Yes
- **Passed**: 490
- **Failed**: 0
- **Coverage**: 76.2%

## Acceptance Criteria
- [x] module.go 对 huawei/aws/azure 三云 RegisterDeployer（产品集：华为 cdn/waf/alb/nlb、AWS cdn/alb/nlb、Azure cdn/alb），并 Stop() 透传
- [x] 扫描适配 NewHuaweiScanAdapter/NewAwsScanAdapter/NewAzureScanAdapter 改用完整 CertAdapter 实例（与部署器共享，aliyun 模式），替换 discovery-only 适配器装配
- [x] discoveryOnlyClouds 移除 huawei/aws/azure；assessChangeable 对三云云通道返回 true（可执行）
- [x] 回归测试：三云产品引用生成变更清单时 AutoChangeable=true、不再含 ERR_DISCOVERY_ONLY 原因；discovery-only 空名单后无引用落入不可执行分区
- [x] 全仓 go build ./... 通过 + cert 域测试套件（deployer/service/web）全绿

## Notes
测试计数：deployer 151 + service 258 + web 78 + cert 根包 3（TestInitCertModule_BootSmoke 经 CERT_TEST_MONGODB_DSN 指向可达实例实测 PASS——五云装配 boot 级验证；TestUnavailableDispatcher/TestRegisterRoutes 同过）。coverage 76.2% 为 service 包整体（既有基线，未触碰面）；本任务改动文件 changelist_generator.go 的 GenerateChangeList/buildChangeItems/assessChangeable/listWarnings 均 100%。环境注记：go build ./... 直跑两次 VirtualAlloc OOM（宿主 commit 限顶），-p 1 通过（既有 hazard 8）；-race 本宿主不可用（无 gcc）；测试链接用 -ldflags="-s -w" 降链接峰值内存。fmt 门禁：4 个改动文件经 tr -d '\r' 判定 gofmt-delta=0（修复了同文件两处既有对齐偏差）；顺手修复同文件 2 处既有 U+FFFD 坏字注释（写入/内存，权威旁证法）。
