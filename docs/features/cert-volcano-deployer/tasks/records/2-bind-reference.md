---
status: "completed"
started: "2026-09-19 12:52"
completed: "2026-09-19 13:24"
time_spent: "~32m"
---

# Task Record: 2 火山部署器绑定层（BindResource/ListReferences × 4 产品）

## Summary
火山部署器绑定层落地：BindResource 四产品分支（CDN BatchDeployCert 逐域结果校验 / WAF ListDomain 定位读 AccessMode + UpdateDomain 三字段 / ALB ModifyListenerAttributes / NLB clb 服务 ModifyNLBListenerAttributes，逐地域 ListenerIds 定点定位对齐 aliyun findALBListener 先例）+ ListReferences 四产品活体重查面（resourceId/ReferencedCloudCertID 形态与任务 3 扫描适配器同口径：CDN/WAF=域名、ALB/NLB={lbId}/{listenerId} 复合 ID、{product}:{id} 归一前缀、ALB served domains 展开且规则失败置空不阻塞）；指纹解析复用既有语义（映射反查 → GetCert 要素〔csv 链解析/cdn 原生 sha256〕→ unresolvedPlaceholderFingerprint 占位，公式与 3.5 扫描路径可对账）。幂等：绑定 API 置位语义，同一 (resource, cloudCertID) 重绑收敛。SDK 窄接口扩 9 方法（含 volcanoSDKClients 新增 nlb 客户端显式转发 + newVolcanoSDKClientsForRegion 地域级工厂，任务 1 证书库层默认地域口径不受影响）。15 个新测试全绿（34/34 volcano 总计），包覆盖 85.3%，internal/cert/... 全树回归通过。

## Changes

### Files Created
无

### Files Modified
- internal/cert/deployer/volcano_deployer.go
- internal/cert/deployer/volcano_deployer_test.go

### Key Decisions
- csv 前缀绑定 fail-fast：csv 统一证书库实例私钥不可再导出，无法在适配层内晋升到产品库；新增哨兵 ErrVolcanoCSVCertNotBindable 显式暴露（不猜测、不静默降级），作为任务 5「暴露编排层与火山 ID 归一不匹配」的落点——回滚路径（扫描产出的 {product}:{id} 旧引用反绑）不受影响
- alb/nlb 共用 ALB 监听证书库 → 绑定兼容互跨（与 GetCert/CleanupOrphan 路由口径一致），其余前缀不匹配走 errVolcanoBindCertProductMismatch 哨兵
- 映射键形态对齐（衔接注记 2）：BindResource 消费与 ListReferences 产出统一 {product}:{id} 前缀，与扫描 ReferencedCloudCertID 同键，映射反查不 miss；WAF bind AccessMode 从 ListDomain 现网值读取（缺失 fail-fast 不猜默认值），UpdateDomain 仅携带 Domain/CertificateID/AccessMode 三字段
- CDN BatchDeployCert 逐域 DeployResult Status 非 success 即失败（携带云侧 ErrorMsg）；实网复核项：Status 枚举大小写、WAF UpdateDomain 未携带字段保留语义、ALB 监听 CertificateId 与 CertCenterCertificateId 两形态互斥性
- 地域级客户端工厂 newClientsForRegion 与任务 1 的 newClients 分离（fake 测试注入互不干扰）；测试 fake 按入参分页切片，翻页分支（refPageSize 缩小 / NLB NextToken 偏移 token）可测

## Test Results
- **Tests Executed**: Yes
- **Passed**: 34
- **Failed**: 0
- **Coverage**: 85.3%

## Acceptance Criteria
- [x] BindResource 按 product 分支：CDN=BatchDeployCert(加速域名)、WAF=域名证书替换、ALB=监听证书、NLB=监听证书；失败返回可识别错误（对齐 wrapCertCloudErr 哨兵）
- [x] ListReferences 四产品资源枚举 → CertReference（指纹解析：映射反查→GetCert 要素→确定性占位），alb/nlb 监听按 served domains 展开（对齐 5 云口径）
- [x] 幂等：同一 (resource, cloudCertID) 重绑结果收敛（对齐既有幂等重跑测试）
- [x] fake SDK 单测覆盖：4 产品 × 绑定成功/失败、ListReferences 全产品枚举 + 占位指纹语义
- [x] 与任务 1 的 {product}:{id} ID 归一互操作（绑定引用归一 ID 可被回滚 GetCert 解析）

## Notes
宿主限制：-race 不可用（无 cgo/gcc）由 go vet + 逐包顺序全绿承接；golangci-lint 不可用由 go vet ./internal/cert/... 承接（干净）。静态门禁：go build -p 1 ./... 过；gofmt 真实 delta 0（CRLF 剥离判别法）；U+FFFD 扫描 0。零覆盖项仅 volcanoSDKClients 生产转发薄壳（测试注入 fake 直连，任务 1 同口径）。回归：internal/cert/... 全树 ok。
