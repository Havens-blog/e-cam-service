# 双云证书 API 能力 PoC 验证记录（poc-notes）

- 任务：5.12 双云证书 API 能力 PoC 验证记录（feature: ssl-cert-management）
- 日期：2026-08-18
- 验证对象：3.1（阿里云）/3.2（腾讯云）证书适配层 `internal/shared/cloudx/{aliyun,tencent}/cert*.go`
- 结论速览：**桌面验证（SDK 面 + 官方文档 + mock 测试）已完成；真实账号全链路验证���执行环境无云账号凭证未执行**，已登记为待办检查点（§8），任务按 blocked 提交。桌面阶段已发现 2 项高优先级偏差（§5-B1 腾讯云孤儿清理异步语义、§5-B2 阿里云 CAS 证书名唯一性约束），建议 5.4/5.5 实现前先消化 §6 修正清单。

## 0. 验证方法与证据等级披露

本环境**不持有任何阿里云/腾讯云真实凭证**，未发起任何真实云 API 调用。本记录基于三类离线证据 + 一份待执行的真实账号检查点计划：

| 标记 | 含义 | 说明 |
|------|------|------|
| `SDK` | SDK 编译面验证 | `go build ./...` 与 `go vet ./...` 通过；API 结构体/方法存在性由 go.mod 锁定版本编译证明（aliyun SDK v1.63.107；tencent cdn v1.3.36 / clb v1.1.55 / common v1.3.82 / teo v1.3.82） |
| `DOC` | 官方文档已核实 | 2026-08-18 在线抓取的厂商官方文档（来源 URL 见 §9），非转述 |
| `MOCK` | mock 测试覆盖 | 3.1/3.2 适配层测试套件全绿：aliyun 包 135 例（证书域 86 例）、tencent 包 116 例（证书域 76 例），`go test -count=1` 全部 PASS |
| `⏳实网` | 待真实账号验证 | 无法离线证明的行为（实际限流形态、绑定生效语义、字段级更新副作用等），见 §8 检查点 |

凡标注 `⏳实网` 的结论**不得**视为已验证；对应行为在 5.4/5.5 实现中按保守假设处理。

## 1. 总体结论摘要

1. 两云五方法（UploadCert / BindResource / ListReferences / GetCert / CleanupOrphan）的 SDK 面与 mock 行为全部就绪；阿里云五产品（cdn/dcdn/waf/alb/nlb）、腾讯云三产品（cdn/waf(EdgeOne)/clb）的调用链在编译与 mock 层无缺口。
2. 腾讯云 SSL 证书服务三接口（UploadCertificate / DescribeCertificateDetail / DeleteCertificate）文档级限频均为 **10 次/秒**，低于适配层客户端限流 20 QPS → 5.5 编排层按 ssl 服务有效上限 10 QPS 设计重试与并发。
3. **偏差 B1（高）**：腾讯云 `DeleteCertificate` 在 `IsCheckResource=true`（适配层当前取值）下为**异步删除**：需服务角色 `SSL_QCSLinkedRoleInReplaceLoadCertificate`，响应返回 `TaskId` 且 `DeleteResult=false`，需轮询 `DescribeDeleteCertificatesTaskResult` 确认结果。当前 `CleanupOrphan` 按同步成功处理并记日志"清理成功"，与文档行为不符（§5-B1，登记 5.4/5.5 修正）。
4. **偏差 B2（高）**：阿里云 CAS `UploadUserCertificate` 的 `Name` **同一用户下不能重复**（≤63 字符）。部署器必须生成唯一名称，否则同名第二次上传报错（§5-B2，登记 5.4 修正）。
5. 腾讯云 `CertFingerprint` 官方文档明确为**签名证书 SHA1 指纹**（可空）——3.2 代码中"SHA1 形态与台账 SHA256 不一致"的疑虑已被文档证实；指纹复核主路径必须走 PEM→SHA256（适配层已按此实现），SHA1 回退值按"无法复核"处理。
6. EdgeOne（product=waf）多证书主位约定、经典 WAF 扫描盲区等 3.2 已登记项维持原判（§5-B4/B5）。

## 2. 逐云逐产品验证矩阵

状态记法：`SDK+MOCK` = 编译面+mock 已验证；`⏳` = 该步骤仍需真实账号验证（全链路均含 ⏳，因真实请求/响应未发生）。

### 2.1 阿里云（aliyun，CAS 证书库 + 五产品）

| 产品 | UploadCert | BindResource | ListReferences | GetCert | CleanupOrphan |
|------|-----------|--------------|----------------|---------|---------------|
| cdn  | SDK+MOCK+DOC⏳（CAS 统一上传） | SDK+MOCK⏳ `SetCdnDomainSSLCertificate(CertType=cas, CertRegion, SSLProtocol=on)` | SDK+MOCK⏳ `DescribeCdnHttpsDomainList`（PageNumber/PageSize=50 翻页） | SDK+MOCK+DOC⏳（CAS `GetUserCertificateDetail(CertFilter=true)`，产品无关） | SDK+MOCK⏳（CAS `DeleteUserCertificate`，404/NotExist 幂等） |
| dcdn | SDK+MOCK⏳（同上 CAS 上传） | SDK+MOCK⏳ `SetDcdnDomainSSLCertificate`（同 CDN 参数形态） | SDK+MOCK⏳ `DescribeDcdnHttpsDomainList` | 同上 | 同上 |
| waf  | SDK+MOCK⏳（同上） | SDK+MOCK⏳ CommonRequest 直调 wafopenapi 2021-10-01：DescribeDomainDetail 读 → ModifyDomain 读改写（保留 Redirect 等字段；无 Listen/HttpsPorts 显式报错） | SDK+MOCK⏳ DescribeInstance→DescribeDefenseResources→DescribeDomainDetail | 同上 | 同上 |
| alb  | SDK+MOCK+DOC⏳（CAS 上传，cloudCertId=`{certId}-{region}` 形态） | SDK+MOCK⏳ `UpdateListenerAttribute`（新证书置默认位，非默认扩展证书保留；HTTP 监听显式报错） | SDK+MOCK⏳ `ListListeners`（NextToken 翻页）+`ListListenerCertificates` | 同上（ID 归一化 `{certId}-{region}`→数字） | 同上 |
| nlb  | SDK+MOCK⏳（同 alb） | SDK+MOCK⏳ `UpdateListenerAttribute`（仅 TLS 监听；首位替换，扩展保留；**校验响应体 Success 字段**，HTTP 200 不代表成功） | SDK+MOCK⏳ `ListListeners`（监听器内联 CertificateIds） | 同上 | 同上 |

### 2.2 腾讯云（tencent，SSL 证书库 + 三产品）

| 产品 | UploadCert | BindResource | ListReferences | GetCert | CleanupOrphan |
|------|-----------|--------------|----------------|---------|---------------|
| cdn  | SDK+MOCK+DOC⏳（ssl `UploadCertificate`，`Repeatable=true`） | SDK+MOCK⏳ `UpdateDomainConfig` 回写 `Https.CertInfo.CertId`（Switch=on；依赖字段级部分更新语义不触碰 Https 开关外配置，⏳重点核实） | SDK+MOCK⏳ `DescribeDomainsConfig`（Offset/Limit=50 翻页） | SDK+MOCK+DOC⏳（ssl `DescribeCertificateDetail`） | SDK+MOCK+DOC⏳（ssl `DeleteCertificate(IsCheckResource=true)`，**文档证实为异步**，§5-B1） |
| waf(EdgeOne) | SDK+MOCK+DOC⏳（同上 ssl 上传） | SDK+MOCK⏳ `ModifyHostsCertificate(Mode=sslcert)`：读当前清单→新证书置首位、原首位移除、扩展保留（首位=主证书为**我方约定**，API 无默认位标记，§5-B4） | SDK+MOCK⏳ `DescribeZones`→逐站点 `DescribeHostsSetting`（各自 Offset/Limit 翻页）；resourceID=`{ZoneId}/{Host}` | 同上 | 同上 |
| clb  | SDK+MOCK+DOC⏳（同上） | SDK+MOCK⏳ `ModifyListener`：单证书走 `Certificate{SSLMode,CertId}`（CA/客户端校验保留）；SNI 多证书走 `MultiCertInfo.CertList=[新证书+扩展+CA]`（⏳核实 CertCaId 混排 CertList 的接受度） | SDK+MOCK⏳ 按地域 `DescribeLoadBalancers`→逐实例 `DescribeListeners`（主证书 CertId + ExtCertIds 均计引用）；resourceID=`{LoadBalancerId}/{ListenerId}` | 同上 | 同上 |

两云 `UploadCert/GetCert/CleanupOrphan` 均收敛到各自证书库服务（阿里云 CAS / 腾讯云 ssl），产品无关；产品差异集中在 BindResource/ListReferences。

## 3. 限制与配额记录表

| # | 云/服务 | 限制项 | 数值/规则 | 证据 | 状态 |
|---|---------|--------|-----------|------|------|
| L1 | 腾讯云 ssl | UploadCertificate 频率 | 10 次/秒 | DOC（400/41665） | 已核实 |
| L2 | 腾讯云 ssl | DescribeCertificateDetail 频率 | 10 次/秒 | DOC（intl 1007/36587） | 已核实 |
| L3 | 腾讯云 ssl | DeleteCertificate 频率 | 10 次/秒 | DOC（intl 1007/36589） | 已核实 |
| L4 | 腾讯云 ssl | 重复上传 | `Repeatable=true` 允许同内容证书重复上传生成独立 ID；`false` 时返回既有证书 ID（`RepeatCertId`）或报 `FailedOperation.CertificateExists` | DOC + 3.2 代码取值 | 已核实（响应路径⏳实网） |
| L5 | 腾讯云 ssl | 免费证书配额 | 超出报 `FailedOperation.ExceedsFreeLimit`（上传来源证书计入免费配额；具体数值随账号套餐，需控制台核实） | DOC（错误码表） | 阈值⏳实网 |
| L6 | 腾讯云 ssl | 删除前置 | `IsCheckResource=true` 需服务角色 `SSL_QCSLinkedRoleInReplaceLoadCertificate`，删除转异步（TaskId+轮询）；证书仍关联资源时报 `FailedOperation.DeleteResourceFailed`；1 小时内申请的免费证书不可删（`CannotBeDeletedWithinHour`） | DOC | 已核实（§5-B1 偏差） |
| L7 | 阿里云 CAS | UploadUserCertificate 频率 | 单用户 100 次/秒 | DOC（help.aliyun.com） | 已核实 |
| L8 | 阿里云 CAS | 证书命名 | `Name` 必填，≤63 字符，**同一用户下唯一** | DOC | 已核实（§5-B2 偏差） |
| L9 | 阿里云 CAS | 证书格式 | `Cert`/`Key` PEM 文本；国密走 EncryptCert/SignCert 独立字段（Cert/Key 非空时国密字段失效） | DOC | 已核实 |
| L10 | 阿里云 CAS | 接入地域 | 仅 cn-hangzhou / ap-southeast-1（适配层 `certCASRegion` 按账号默认地域映射） | 3.1 调研注释 | ⏳实网复核 |
| L11 | 两云适配层 | 客户端限流 | 20 QPS（3.1/3.2 内建 RateLimiter）；ListReferences 分页 50/页 | 代码 | 已实现 |
| L12 | 绑定粒度 | 各产品 | CDN/DCDN/WAF(EdgeOne)=域名级（EdgeOne 为 `{ZoneId}/{Host}`）；ALB/NLB/CLB=监听器级（复合 resourceID 联合寻址） | 代码+SDK | 已实现（生效语义⏳实网） |

## 4. 失败与限制场景记录（文档级错误形态）

| # | 场景 | 云/接口 | 错误形态 | 适配层处置 | 状态 |
|---|------|---------|----------|-----------|------|
| F1 | 限流 | 腾讯云 ssl 三接口 | `LimitExceeded.RateLimitExceeded`（10 QPS 超限） | `isRateLimitCode` 前缀匹配（`LimitExceeded.`）→ 哨兵 `ErrCloudRateLimited` | 映射已测（MOCK）；实际响应⏳实网 |
| F2 | 限流 | 阿里云 | `Throttling`/`Throttling.User`（SDK ServerError） | `IsThrottlingError` → 哨兵 `ErrCloudRateLimited` | 同上 |
| F3 | 格式拒绝 | 腾讯云 UploadCertificate | `FailedOperation.CertificateParseError`（PEM 解析失败）/ `CertificateMatchError`/`CertificateMismatch`（证书私钥不匹配） | 透传带产品上下文错误（非哨兵） | DOC 已核实；⏳实网复现 |
| F4 | 重复上传 | 腾讯云 UploadCertificate | `Repeatable=false`：返回 `RepeatCertId`（既有证书 ID，`CertificateId` 为空）或 `FailedOperation.CertificateExists`；`Repeatable=true`：新 ID | 适配层固定 `Repeatable=true`；`CertificateId` 空时回退 `RepeatCertId`（防御） | DOC 已核实 |
| F5 | 证书不存在 | 腾讯云 ssl | `FailedOperation.CertificateNotFound` | GetCert→`Exists=false`（非错误）；CleanupOrphan→幂等成功 | MOCK 已测 |
| F6 | 证书不存在 | 阿里云 CAS | HTTP 404 / NotExist 类错误码 | 同上（`isCertNotFoundError`） | MOCK 已测；实际码⏳实网 |
| F7 | 删除被拒 | 腾讯云 DeleteCertificate | `FailedOperation.DeleteResourceFailed`（证书仍关联云资源）/ `BoundResources`；角色缺失 `RoleNotFoundAuthorization` | 当前透传；**B1 修正后应区分处理** | DOC 已核实（§5-B1） |
| F8 | 配额超限 | 腾讯云 UploadCertificate | `FailedOperation.ExceedsFreeLimit` | 透传；5.4/5.5 应按不可重试配置错误呈现（§6-C3） | DOC 已核实 |
| F9 | 命名冲突 | 阿里云 CAS UploadUserCertificate | 同名上传报错（错误码文案⏳实网确认） | 当前透传；**B2 修正后由部署器生成唯一名**（§6-C2） | 规则已核实，错误码⏳实网 |

## 5. 发现的偏差与坑点（按优先级）

### B1（高）腾讯云 CleanupOrphan 异步删除语义不符
- 现状：`tencent/cert.go CleanupOrphan` 以 `IsCheckResource=true` 调 `DeleteCertificate`，收到 2xx 即记日志"腾讯云孤儿证书清理成功"并返回 nil。
- 文档事实（intl 1007/36589）：`IsCheckResource=true` 时删除**转异步**——响应为 `{"DeleteResult": false, "TaskId": "..."}`，需调 `DescribeDeleteCertificatesTaskResult` 轮询结果；且前置要求账号已授权服务角色 `SSL_QCSLinkedRoleInReplaceLoadCertificate`（未授权报 `RoleNotFoundAuthorization`）。
- 影响：清理队列会把"已提交删除任务"误记为"已删除"；孤儿清理的收敛性判断失真。
- 处置：**登记 5.4/5.5 修正项**（§6-C1）。不改 3.2 代码——异步轮询属编排层职责，且方案需 5.4/5.5 设计定夺（方案 A/B 见 §6）。

### B2（高）阿里云 CAS 证书名用户级唯一
- 文档事实：`UploadUserCertificate.Name` 同一用户下不能重复（≤63 字符）。
- 影响：部署器若用固定名（如域名）上传，第二次更换即命名冲突失败；与腾讯云（Alias 可重复）行为不对称。
- 处置：**登记 5.4 修正项**（§6-C2）：CAS 侧名称生成规则 `ecam-{域名或指纹前缀}-{时间戳/序号}` 并截断至 63 字符。

### B3（中）腾讯云指纹回退口径为 SHA1（文档证实）
- `DescribeCertificateDetail.CertFingerprint` 文档明确为"签名证书的 **SHA1** 指纹"，且可为 null（审核中证书示例全为 null）。
- 适配层主路径 PEM→SHA256 正确；仅当云侧不回 PEM（`CertificatePublicKey` 为 null）时回退 SHA1，与台账 `^[0-9a-f]{64}$` 必然不匹配。
- 处置：维持 3.2 实现（回退值长度 40 hex，上层按"无法复核"处理即可）；5.5 校验逻辑不得将指纹不匹配直接判"证书不一致"。⏳实网补充：确认上传来源（upload）证书 `CertificatePublicKey` 是否稳定返回。

### B4（中）EdgeOne 多证书主机"首位=主证书"为我方约定
- API 无默认/主证书标记；绑定=新证书置首位、原首位移除、其余保留。待更换证书位于非首位时，更换后旧证书仍保留绑定。
- 处置：维持 3.2 约定 + 部署后复扫核实（5.5 验证窗口钩子）；⏳实网验证多证书主机行为。

### B5（中）腾讯云经典 WAF 扫描盲区
- 首期 product=waf 映射 EdgeOne；经典 WAF（waf.tencentcloudapi.com）域名证书不在发现范围。维持 3.2 登记，文档化"扫描盲区"口径，不做静默处理。

### B6（中）阿里云 NLB Success-in-body
- NLB `UpdateListenerAttribute` 存在 HTTP 200 但响应体 `Success=false` 的情况，适配层已显式校验。维持；⏳实网复现一次确认文案。

### B7（低）腾讯云 CDN UpdateDomainConfig 字段级部分更新假设
- 绑定仅携带 `Https.Switch/CertInfo`，依赖云侧字段级合并保持域名其余配置不变（含 Https 内 HTTP2/OCSP 等子字段）。⏳实网重点核实：绑定前后 DescribeDomainsConfig diff 应仅证书相关字段变化。

### B8（低）阿里云 WAF 3.0 走 CommonRequest 直调
- SDK 无 waf 3.0 模块，RPC 形态（DescribeInstance→DescribeDefenseResources→DescribeDomainDetail→ModifyDomain 读改写）仅经 mock 验证。⏳实网全链路重点项。

### B9（低）ALB/NLB `{certId}-{region}` 复合 ID 形态
- 适配层上传后自行拼接 `-{CAS地域}` 并在 GetCert/CleanupOrphan 侧归一化解析。⏳实网确认监听引用返回的证书 ID 与拼接形态一致。

## 6. 对 5.4/5.5 的修正建议清单

| # | 修正项 | 建议 | 归属 |
|---|--------|------|------|
| C1 | 腾讯云孤儿清理异步语义（B1） | 方案 A（推荐，v1 简单）：改 `IsCheckResource=false` 同步删除，删除前置条件"引用已收敛"由编排层保证（清理队列仅在 ListReferences 复核无引用后入队）；方案 B：保持 true + 适配层/编排层轮询 `DescribeDeleteCertificatesTaskResult`（超时转人工），并要求目标账号预授权 `SSL_QCSLinkedRoleInReplaceLoadCertificate`（角色缺失视为账号配置错误，非重试错误）。两方案都需把"已提交删除"与"已删除"状态区分落库 | 5.4（编排）+5.5（状态机） |
| C2 | 阿里云 CAS 命名唯一（B2） | 部署器生成 `ecam-{domain|指纹前8}-{unix秒}`，超 63 字符截断；命名冲突错误按可重试处理（重新生成名） | 5.4 |
| C3 | 配额/不可重试错误分类 | `ExceedsFreeLimit`、`RoleNotFoundAuthorization`、`CertificateExists`（若出现）标记为**不可重试**，直接转人工/呈现配置错误；`ErrCloudRateLimited` 才走退避重试 | 5.5 |
| C4 | ssl 服务有效 QPS=10 | 腾讯云 ssl 三接口限频 10 QPS < 客户端 20 QPS；5.5 并发/重试按 10 QPS/账号 设计，`LimitExceeded.RateLimitExceeded` 退避建议 ≥2s 起步 | 5.5 |
| C5 | 指纹复核口径 | 复核主路径=GetCert PEM→SHA256（两云一致）；回退指纹（腾讯 SHA1 40hex / 阿里 CAS 原始指纹）一律按"无法复核"处理，不判不一致 | 5.5 |
| C6 | 部署后验证窗口 | EdgeOne 多证书主机（B4）与腾讯 CDN 字段级更新（B7）在验证窗口内做 ListReferences 复扫 diff，异常转人工 | 5.5 |
| C7 | 上传幂等策略 | 腾讯云固定 `Repeatable=true`（每次独立副本，孤儿按 CloudCertMapping 归属收敛）维持；阿里云按 C2 唯一名。两云上传失败重试**不得**复用可能已成功的名称/别名（重试即新副本，孤儿清理兜底） | 5.4 |

## 7. 请求样例（脱敏）

> 来源标注：以下样例由**适配层实际构造代码 + 厂商官方文档示例**合成，**非实网抓包**（本环境无凭证）。`...`/`****` 为脱敏占位；任何样例不含 AK/SK/私钥明文。

### 7.1 阿里云 CAS 上传（UploadCert 第一段）

```json
// POST cas.cn-hangzhou.aliyuncs.com UploadUserCertificate（SDK: cas.CreateUploadUserCertificateRequest）
{
  "Name": "ecam-poc-example.example.com-1765-xxxxxxx",   // ≤63 字符，用户内唯一（L8/B2）
  "Cert": "-----BEGIN CERTIFICATE-----\nMIIF...(测试自签证书 PEM，脱敏)\n-----END CERTIFICATE-----",
  "Key":  "-----BEGIN RSA PRIVATE KEY-----\n...(脱敏)\n-----END RSA PRIVATE KEY-----"
}
// 响应（文档示例）
{ "CertId": 12345, "RequestId": "BDB81BA2-****", "ResourceId": "cas-upload-xki1d0" }
// 适配层：ALB/NLB 产品追加地域 → cloudCertId = "12345-cn-hangzhou"
```

### 7.2 阿里云 CDN 绑定（BindResource 第二段，cert_cdn.go 实际参数）

```json
// SetCdnDomainSSLCertificate
{
  "DomainName": "poc-example.example.com",
  "CertType": "cas",
  "CertId": 12345,
  "CertRegion": "cn-hangzhou",     // CAS 地域（L10），非账号默认地域
  "SSLProtocol": "on"
}
```

### 7.3 腾讯云 ssl 上传（UploadCert 第一段，cert.go 实际参数）

```json
// POST ssl.tencentcloudapi.com UploadCertificate（CommonRequest 直调）
{
  "CertificatePublicKey":  "-----BEGIN CERTIFICATE-----\n...(测试自签证书 PEM，脱敏)\n-----END CERTIFICATE-----",
  "CertificatePrivateKey": "...(脱敏)",
  "Alias": "ecam-poc-example",
  "Repeatable": true          // 每次更换生成独立副本（C7）
}
// 响应：{ "Response": { "CertificateId": "hhe****jjsj", "RepeatCertId": null, "RequestId": "..." } }
// 失败形态：CertificateParseError / CertificateMatchError / ExceedsFreeLimit / LimitExceeded.RateLimitExceeded
```

### 7.4 腾讯云 ssl 删除（CleanupOrphan，B1 关键证据）

```json
// DeleteCertificate
{ "CertificateId": "tey****hdh", "IsCheckResource": true }
// 文档示例响应——注意 DeleteResult=false + TaskId（异步）：
{ "Response": { "DeleteResult": false, "TaskId": "1251261", "RequestId": "14727a68-****" } }
// 需再轮询 DescribeDeleteCertificatesTaskResult(TaskId) 才能确认删除完成
```

### 7.5 腾讯云 CDN 绑定（cert_cdn.go 实际参数）

```json
// UpdateDomainConfig（仅携带 Https 字段，依赖字段级部分更新，B7）
{
  "Domain": "poc-example.example.com",
  "Https": { "Switch": "on", "CertInfo": { "CertId": "hhe****jjsj" } }
}
```

### 7.6 腾讯云 CLB 多证书监听绑定（cert_clb.go 实际参数形态）

```json
// ModifyListener（SNI 多证书路径：MultiCertInfo 整单回写）
{
  "LoadBalancerId": "lb-****",
  "ListenerId": "lbl-****",
  "MultiCertInfo": {
    "SSLMode": "unilateral",
    "CertList": [ { "CertId": "hhe****jjsj" }, { "CertId": "(原扩展证书保留)" }, { "CertId": "(原CA证书, 双向认证时)" } ]
  }
}
```

其余产品（DCDN/WAF/ALB/NLB/EdgeOne）样例同理由代码+文档合成，实网抓包后在本文档追加"实测样例"小节。

## 8. 真实账号待验证检查点（PENDING）

执行前提（Hard Rules 遵守）：仅用**开发/测试账号**（与生产隔离）；仅用**现场生成自签测试证书**；**验证后全量清理**（证书+绑定）；连接信息经 env 注入，不落文件。

驱动方式（按任务 Implementation Notes）：临时 `-tags=poc` 驱动（main 或 TestMain），复用 3.1/3.2 适配器真实客户端工厂，验证后驱动代码保留在 poc 标签内或删除。建议 env 约定：`POC_ALIYUN_AK/SK/REGION`、`POC_ALIYUN_TEST_DOMAIN`、`POC_TENCENT_AK/SK/REGION`、`POC_TENCENT_TEST_DOMAIN/ZONE_ID`、`POC_TENCENT_LB_ID/LISTENER_ID`（值由执行人从测试账号控制台取）。

| # | 检查点 | 步骤要点 | 通过判据 | 优先级 |
|---|--------|----------|----------|--------|
| P1 | 阿里云全链路 ×5 产品 | 各产品：UploadCert（唯一名）→BindResource（测试资源）→GetCert（指纹/有效期）→ListReferences 含该引用→CleanupOrphan，记录每步请求/响应要点（脱敏） | 全链路成功+清理干净 | 高（AC-1） |
| P2 | 腾讯云全链路 ×3 产品 | 同上（EdgeOne 用测试 Zone/Host；CLB 用测试监听器） | 同上 | 高（AC-2） |
| P3 | B1 异步删除复现 | IsCheckResource=true 删除有引用/无引用证书各一次，抓 TaskId/轮询结果/错误码；角色未授权账号复现 RoleNotFoundAuthorization | 确认方案 A/B 取舍 | 高 |
| P4 | 限流形态 | 腾讯 ssl 加压 >10 QPS、阿里 CAS >100 QPS，抓实际错误码/响应头 | 与 F1/F2 映射一致 | 中 |
| P5 | B7 字段级更新 | 腾讯 CDN 绑定前后 DescribeDomainsConfig 全量 diff | 仅证书相关字段变化 | 中 |
| P6 | B8 WAF 3.0 RPC | 阿里 WAF 读改写全链路 | 域名证书生效 | 中 |
| P7 | B9 ALB/NLB 复合 ID | 上传→绑定→ListListeners 回读证书 ID 形态 | 与 `{certId}-{region}` 一致 | 中 |
| P8 | B3/B4 指纹与多证书 | 腾讯上传源证书 DescribeCertificateDetail 是否回 PEM+指纹形态；EdgeOne 多证书主机更换复扫 | 明确回退路径触发条件 | 中 |
| P9 | 配额实测 | 腾讯免费配额余量、ExceedsFreeLimit 文案；CAS 同名二次上传错误码 | 补全 L5/F9 | 低 |

清理核对（每检查点后）：证书库无 `ecam-*`/`ecam-poc-*` 残留；测试���源绑定还原；`-tags=poc` 产物不入 main 分支。

## 9. 引用来源

- 腾讯云 UploadCertificate：https://cloud.tencent.com/document/product/400/41665 （10 QPS；Repeatable；错误码表）
- 腾讯云 DescribeCertificateDetail：https://intl.cloud.tencent.com/document/product/1007/36587 （中站 400/41673；10 QPS；CertFingerprint=SHA1；字段可空性）
- 腾讯云 DeleteCertificate：https://intl.cloud.tencent.com/document/product/1007/36589 （IsCheckResource 异步语义；服务角色；错误码表）
- 阿里云 UploadUserCertificate：https://help.aliyun.com/zh/ssl-certificate/developer-reference/api-cas-2020-04-07-uploadusercertificate （100 QPS；Name ≤63 唯一；PEM/国密字段）
- 本仓 3.1/3.2 适配层源码与测试：`internal/shared/cloudx/aliyun/cert*.go`、`internal/shared/cloudx/tencent/cert*.go`（测试 135+116 例全绿）

未核实项（在线抓取失败/未定位）：阿里云 GetUserCertificateDetail / DeleteUserCertificate 官方参数页、CAS 地域限制官方页 → 已在 L10/F6/F9 标注 ⏳实网复核，不影响桌面结论。

## 10. 5.5 增补记录（2026-08-19，腾讯云部署器实现时消化）

### 10.1 B1 异步删除语义已落地（§6-C1 取方案 B）

- **实现位置**：`internal/shared/cloudx/tencent/cert.go CleanupOrphan` 适配层内部（唯一持有 TaskId 的层）；deployer 层（5.5）纯透传，与 5.4 结构对称。
- **行为**：`DeleteCertificate(IsCheckResource=true)` 响应分派——
  - `DeleteResult=true` → 同步删除完成，直接成功；
  - `DeleteResult=false` 且 `TaskId` 非空 → 有界轮询 `DescribeDeleteCertificatesTaskResult(TaskIds=[TaskId])`：间隔 2s × 最多 10 次（最坏阻塞 20s，禁无限轮询）；任务失败/Error 字段非空 → 返回错误；删除中/任务未出现 → 继续轮询；单次查询失败（限流等）不中断、占用一次预算继续；预算耗尽 → 错误返回（清理队列重放安全：已删除证书再删返回不存在 → 幂等成功，F5 口径）；
  - 两者皆无 → 防御性失败（"no delete confirmation"，不得记清理成功）。
- **仍 ⏳实网（并入 P3 检查点）**：① `DeleteTaskResult.Status` 数值映射（当前按 0=删除中/1=成功/2=失败 实现并登记为待复核，防御双信号=Error 字段非空一律判失败，状态未知不静默视���成功）；② `IsCheckResource=true` 真实响应是否恒携带 TaskId（文档 §7.4 样例如此）；③ 删除进行中重复调用 DeleteCertificate 的云侧行为（当前按"队列重放幂等"假设设计）。

### 10.2 resourceId 粒度约定实现确认（L12 在 5.5 的落地）

- cdn=域名；waf(EdgeOne)=`{ZoneId}/{Host}`；clb=监听器级复合 `{LoadBalancerId}/{ListenerId}`——三形态由 3.2 适配层解析消费，deployer 层 `BindResource`/`ListReferences` 原样透传，变更项持久化即 `ChangeItem.resourceRef.resourceId`（schema.sql 口径），已在 `tencent_deployer.go` 类型注释登记。
- 腾讯云与 5.4 的结构差异：无 CAS 命名冲突重试（Alias 可重复，L4）、无监听证书 ID 地域后缀归一化（无 B9 对应形态）、上传别名公式与 5.4 共用（`ecam-{指纹前8}-{unix秒}-{随机4hex}`，防御性截断 50——腾讯云未公布 Alias 长度硬限制，取保守值）。
