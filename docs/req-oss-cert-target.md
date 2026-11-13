# 需求卡：证书部署目标覆盖 OSS 自定义域名绑定

**状态**：需求卡待评审（未实现）。
**来源**：2026-11-12 真实环境探测证据（见「一、证据」）。

---

## 一、证据（真实探测，非推断）

对同一 `*.jlc-layout.com` 证书覆盖下的域名做 TLS 实测（SNI 拨测，取线上叶子证书）：

| 域名 | CNAME 指向 | 线上实际服务证书 | 指纹(SHA256 前16) | 探测结论 |
|---|---|---|---|---|
| order-static.jlc-layout.com | `...c.vedcdnlb.com`（CDN 层）| `*.jlc-layout.com`（GeoTrust）| `d8ef18ba…` | 一致 |
| pub-static.jlc-layout.com | `...c.vedcdnlb.com` | `*.jlc-layout.com` | `d8ef18ba…` | 一致 |
| temp-static.jlc-layout.com | `...c.vedcdnlb.com` | `*.jlc-layout.com` | `d8ef18ba…` | 一致 |
| **test.order-static**.jlc-layout.com | **直连 OSS 桶** `jlc-uat-smt-jlcsj-order.oss-cn-shenzhen.aliyuncs.com` | `*.oss-cn-shenzhen.aliyuncs.com`（GlobalSign）| `579b1947…` | **差异** |
| **test.pub-static**.jlc-layout.com | 直连 `…-public.oss-…` | 同上 | `579b1947…` | **差异** |
| **test.temp-static**.jlc-layout.com | 直连 `…-temp.oss-…` | 同上 | `579b1947…` | **差异** |

判定口径（`internal/cert/service/probe_service.go` `probeOne`）：线上指纹 ∉ 台账中覆盖该域名的证书指纹集合 → `ProbeStatusDiff`。

**根因**：非 test 版经 CDN 层终结 TLS（服务平台证书）；test 版旁路 CDN、CNAME 直连 OSS 桶，OSS 按 SNI 返回区域通配证——即 **OSS 自定义域名上的证书绑定未对该确切主机名生效**（或绑定缺失/绑在错误桶/引用了旧证书）。浏览器直接访问上述 test 域名会报证书不匹配。

## 二、现状与缺口

| 层 | 现状 |
|---|---|
| OSS 侧 | 自定义域名证书需在 **OSS 控制台逐域名手工绑定**，脱离证书平台换证流程 |
| SDK（e-cloudx-sdk）| `aliyun` 包 `CertProduct` 仅 `cdn/dcdn/waf/alb/nlb`；`oss.go` 只有资产/监控，**无自定义域名证书绑定能力** |
| 平台（e-cam-service）| `DeployTarget.Product` 取值同上五类，`DeployTarget.Validate`/schema 无 `oss` 分支 |
| 后果 | 直连 OSS 的域名每年换证后不会自动更新 → 证书差异持续告警，且有真实 HTTPS 不可用风险 |

## 三、目标 / 非目标

**目标**
1. 证书平台支持「OSS 自定义域名」为一等部署目标：换证时自动把新证书绑定到桶的自定义域名。
2. 发现侧可枚举「哪些桶的哪些自定义域名引用了哪张证书」，纳入统一台账与差异探测闭环。
3. 回滚时恢复旧证书绑定。

**非目标**
1. 不改 OSS 桶的其它配置（静态网站、跨域、防盗链等）。
2. 首期只做**阿里云 OSS**；其它对象存储（腾讯 COS、AWS S3+CloudFront）留作后续同构扩展。
3. 不迁移既有手工绑定（迁移策略另行评估，见「七、迁移」）。

## 四、用户故事与操作流程

**角色**：云平台运维（证书管理员）。

**流程（自动换证）**
1. 证书平台发起换证变更单，目标资源包含「OSS 自定义域名」条目。
2. 执行通道两段式：① 上传证书至 CAS（沿用现有 `aliyunUploadProduct`）；② 绑定到目标 OSS 自定义域名。
3. 探测复检：新指纹与线上一致 → `consistent`；否则 `diff` 告警。

**流程（发现）**：只读扫描桶的自定义域名 + 其引用的证书，形成 `CertReference`（product=`oss`，resourceID=自定义域名）。

## 五、契约（API / 类型）

**SDK（e-cloudx-sdk `aliyun`）**
```go
// 新增 product 常量
CertProductOSS = "oss"

// CertAdapter 增补（签名收敛在既有五方法族，避免破坏性改动）
// 绑定/更新：把 CAS 证书绑定到桶的自定义域名
BindOSS(creds *CloudAccount, bucket string, domain string, certID string) error
// 只读发现：枚举桶自定义域名及其证书
ListOSSCertRefs(creds *CloudAccount, bucket string) ([]CloudCertRef, error)
```
> 底层调用 OSS `PutCname`（携带 `CertificateConfiguration`）/`ListCname`/`GetCname`；具体 API 形态在实现期以 OSS 官方文档为准并写进单测 fake。

**平台（e-cam-service）**
- `domain.Product` 增加 `oss`（`internal/cert/domain`）。
- `DeployTarget.Product` 允许 `oss`；`Validate` 增加：`product=oss` 时 `ResourceID` = 自定义域名（FQDN 形态），`Cloud` 必须 `aliyun`。
- `cert_change_items.resourceRef` 的 anyOf 校验器（schema.sql）增加 `oss` 分支。
- `AliyunDeployer.BindResource` 路由 `oss` → SDK `BindOSS`；`ListReferences` 路由 `oss` → `ListOSSCertRefs`。

## 六、验收标准（可测/可浏览器判定）

1. 单测（表驱动）：`DeployTarget.Validate` 对 `oss` 分支的必填/非法值；`AliyunDeployer` 对 `oss` 的绑定路由与错误包装；fake adapter 断言调用参数。
2. 集成（真实环境，复用 2026-11-12 的三个 test 域名作为首批验证对象）：
   - 把 `test.order-static.jlc-layout.com` 作为 `oss` 目标执行部署 → OSS 控制台该自定义域名证书变为 `*.jlc-layout.com`；
   - 探测复检转为 `consistent`（SNI 拨测指纹 = `d8ef18ba…`）；
   - 回滚恢复旧绑定后重新变回 `diff`。
3. 浏览器判定：`https://test.order-static.jlc-layout.com` 不再出现证书警告。

## 七、迁移（既有手工绑定）

首期**只做发现 + 新变更自动绑定**；既有手工绑定通过「发现 → 与台账比对」，差异域名进入现有告警闭环，由运维决定是否补录为 OSS 目标。不自动改写存量绑定（避免误伤）。

## 八、风险与回滚

| 风险 | 缓解 |
|---|---|
| OSS `PutCname` 覆盖证书时若参数不全可能影响域名可用性 | 绑定前先 `GetCname` 快照；失败即回滚旧证书 ID；整机操作串行 + 重试上限沿用 `withRetry` |
| 自定义域名与桶不在同一账号 | 发现侧以账号为前缀（`AccountKey`），跨账号目标在 Validate 阶段拒绝 |
| 证书链形态差异（OSS 要求完整链）| 上传前用 CAS 侧已有的链拼接结果；单测覆盖缺中级证场景 |
| 误把 OSS 桶当 CDN 域名绑定 | `product` 必填且强校验；错误分支返回 `ErrInvalidTarget` |

回滚策略：沿用变更单的两段式回滚（重新绑定 `oldRef.cloudCertID`），失败则保持 `failed` 并告警（与现有云通道语义一致）。

## 九、涉及端与工作量预估

| 仓库 | 改动 | 预估 |
|---|---|---|
| e-cloudx-sdk | `CertProductOSS` + `BindOSS`/`ListOSSCertRefs` + 单测 | 1 天 |
| e-cam-service | Product/Validate/schema/Deployer 路由 + 表驱动单测 | 1 天 |
| 联调 | 三个 test 域名真实绑定验证 + 回滚验证 | 0.5 天 |

## 十、未决问题（需产品/运维确认）

1. **目标形态**：这三个 test 域名长期应「直连 OSS + 平台托管证书」，还是「回归 CDN 层」（改 CNAME 一步解决）？前者才需要本需求；后者只需修 DNS。
2. 是否把「发现到的 OSS 自定义域名」自动纳入探测域清单（当前探测域来自台账证书 SAN）。

---

## 更正（2026-11-13，证据驱动）

**原判断**：「OSS 自定义域名的证书绑定未生效 / 缺失 / 绑错桶」。

**更正后的根因**：**证书 SAN 本身不覆盖这些主机名**。`*.jlc-layout.com` 按 RFC 6125 只覆盖单标签（如 order-static.jlc-layout.com），**不覆盖四级的 test.order-static.jlc-layout.com**。因此无论 OSS 绑定是否执行，TLS 都无法为该主机名正确服务；实测线上回落到 OSS 区域通配证书（*.oss-cn-shenzhen.aliyuncs.com）与此一致。

### 证据

1. 平台判定口径：`internal/cert/domain/cert_parse.go` 的 `CheckSANCover` 注释与实现——通配符按单标签通配，不覆盖多级；
2. 单测钉死口径：`cert_parse_test.go` 用例 `"wildcard does not cover two labels"`；
3. 探测归属索引同口径：`probe_service.go` 的 `coverageIndex.covers` → `coversSingleLabel`；
4. 线上证书 SAN 实测：仅 `*.jlc-layout.com` + `jlc-layout.com` 两条，无四级域显式 SAN（对应指纹 d8ef18ba…）；
5. 对照组：非 test 版（三级域）经 CDN 正常一致——与「标签层级」结论吻合。

### 对需求卡的影响

- 本需求（OSS 自定义域名纳入证书平台自动化）**仍然成立**，但它**不是**这三个 test 域差异的根因，降级为 P2 平台能力建设；
- 差异的直接修复是**证书 SAN 覆盖**，三选一（另有一个不成立项）：
  - **A（推荐）**：为各 test 区签发 `*.order-static.jlc-layout.com` / `*.pub-static.jlc-layout.com` / `*.temp-static.jlc-layout.com`——这些通配符正好覆盖其下的四级域名；
  - **B**：在现有证书上追加三条显式 SAN（三个 test FQDN），每新增测试域需重签；
  - **C**：测试域改回三级命名（如 `test-order-static.jlc-layout.com`），现有 `*.jlc-layout.com` 直接覆盖，零证书变更；
  - **D（不可单独成立）**：改走 CDN——CDN 同样校验证书是否覆盖域名，证书不换依然失败。

**结论**：三个 test 域当前的 `diff` 告警是**真实配置缺口**（非探测误报）；按 A/B/C 修复证书后告警自然消除。
