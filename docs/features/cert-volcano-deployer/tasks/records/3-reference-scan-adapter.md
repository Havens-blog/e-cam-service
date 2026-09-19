---
status: "completed"
started: "2026-09-19 12:22"
completed: "2026-09-19 12:51"
time_spent: "~29m"
---

# Task Record: 3 火山引用扫描适配器（4 产品 → CertReference）

## Summary
火山引用扫描适配器：四产品（CDN/WAF/ALB/NLB）资源证书引用接入 reference_scan_service CloudScanAdapter 只读端口（第 6 云）。CDN/WAF 域名粒度、ALB/NLB={lbId}/{listenerId} 监听复合 ID + ALB served domains（转发规则 Host 条件）展开；云证书 ID 归一 {product}:{id}（与任务 2 BindResource 共享消费形态约定）。指纹解析：映射反查 → GetCert 要素（cdn 经 ListCdnCertInfo 原生 SHA256、csv 复用 cloudx/volcano/cert.go 链解析指纹基础；waf/alb/nlb 证书库无 sha256 通道→无法复核哨兵，对齐华为口径）→ certscan-unresolved: 占位与 5 云一致。单产品/单账号失败隔离经服务层 partials 验证；fake SDK（volcanoScanAPI 5 方法 + csv 指纹源）全注入零真实云调用。SDK 窄接口只读构造性保证（Hard Rule：扫描只读；指纹解析不复制部署器逻辑）。

## Changes

### Files Created
- internal/cert/service/volcano_scan_adapter.go
- internal/cert/service/volcano_scan_adapter_test.go

### Files Modified
- docs/features/cert-volcano-deployer/tasks/index.json

### Key Decisions
- GetCert 按归一前缀路由：cdn→ListCdnCertInfo 原生 sha256（64hex 对齐直读）；csv→cloudx/volcano.CertAdapter（接口面 volcanoCSVFingerprintSource，可注入）；waf/alb/nlb 证书库查询 API 无 sha256 指纹→errVolcanoScanFingerprintUnavailable 哨兵（对齐华为 SCM SHA-1 无法复核语义），指纹仅经映射反查或落占位
- ReferencedCloudCertID 归一 {product}:{id}——变更清单回滚 GetCert/CleanupOrphan（任务 1 部署器）按前缀路由消费同一形态；ResourceID 形态（CDN/WAF=域名、ALB/NLB=lb/listener 复合）与任务 2 BindResource 共享约定按 proposal In Scope 对齐
- SDK 窄接口 volcanoScanAPI 仅 5 个只读方法（构造性保证扫描只读）；volcanoScanClients 显式转发四服务客户端（对齐部署器 volcanoSDKClients 先例，避免嵌入选择器歧义）
- ALB 主证书 CertificateId 缺省回退 CertCenterCertificateId、SNI 同 ID 去重、HTTP 监听跳过、DescribeRules 失败 served 置空不阻塞（对齐 aliyun listALBServedDomains 口径）；实网复核项以注释单点标注

## Test Results
- **Tests Executed**: Yes
- **Passed**: 14
- **Failed**: 0
- **Coverage**: 77.2%

## Acceptance Criteria
- [x] NewVolcanoScanAdapter 对齐 5 云形态（domain.Cloud("volcano") 映射 + 编译期接口断言）
- [x] CDN/WAF 域名粒度 + ALB/NLB served domains 展开的 CertReference 产出正确（指纹解析：映射反查→GetCert 要素→certscan-unresolved: 占位语义与 5 云一致）
- [x] 单产品单账号失败隔离（不中断该云其余产品/账号）
- [x] fake SDK 单测：4 产品引用枚举 + 指纹解析命中/占位两态 + 失败隔离

## Notes
新增文件业务逻辑函数覆盖 80-100%（低于 80% 者仅 5 个一行 SDK 转发桩与 wrapVolcanoScanErr nil 早退分支，与部署器同形态不测）；包整体 77.2% 为既有基线。既有 5 云引用扫描测试全绿回归（internal/cert/... 全部 ok）。-race 不可用（宿主无 cgo）由 go vet + 确定性测试承载；golangci-lint 不可用由 go vet 承接。任务 2 对接锚点：resourceId/referencedCloudCertID 形态见 volcano_scan_adapter.go 文件头『引用形态』节。
