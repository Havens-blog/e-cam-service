---
created: "2026-09-17"
author: "Haven"
status: Approved
intent: "new-feature"
---

# Proposal: 火山云证书发现导入 + 多云定时增量同步

## Problem

证书主流采购已转火山引擎，但 e-cam 云证书发现导入只覆盖五云（aliyun/tencent/huawei/aws/azure）且只有手动触发——火山在库证书无法入库，云端新购/续期换证后台账长期滞后，probe 误报 diff、到期看板失真。

### Evidence

- 代码事实：`module.go:220-225` 装配的发现导入适配器仅五云；`internal/shared/cloudx/volcano/` 已有全量云适配器（ECS/RDS/CDN/WAF/ALB/DNS…，注册别名 `volcano`/`volcengine`）但**没有任何证书库适配器**。
- 调度事实：`internal/cert/scheduler/jobs.go` 已注册 8 个调度点（scan 02:00 / inspection 05:00 / verify-window / orphan-sweep / crd-recheck / scan-timeout / pause-timeout / executing-timeout），**无任何导入任务**；云发现导入仅 HTTP 手动端点（`DiscoveryHandler`）。
- 线上事实：2026-09-16 实测——CAS 27180182（`*.jlc-cnc.com`）线上已生效指纹 `3669ddbf` 而台账缺失，probe 对 prod-oss/www/ws 全报 diff，手动导入后才收敛。
- 火山 SDK 事实：`volcengine-go-sdk v1.2.9`（已在 go.mod，cloudx/volcano 依赖）已含 `certificateservice`：`CertificateGetInstanceList`（分页）/`CertificateGetInstance`（返回 `Chain` 证书链、`FingerPrintSha256`、`CommonName`、`San`、`NotBefore/After`、`SerialNumber`、`PrivateKey`、`IsCertificateRevoked`），链 + 指纹 + SAN 字段齐备。

### Urgency

无定时导入 → 每张云购证书的新购/续期都是人工动作，漏导直接造成 diff 误报与到期告警失真；成本随证书数量线性增长，而采购已转向火山。2026-09-16 那轮线上生效→台账滞后→diff 误报正是该缺口的生产级复现。

## Proposed Solution

两件事，复用既有管线，不发明新机制：

1. **火山云证书库发现适配器**：`internal/shared/cloudx/volcano/cert.go` 实现发现端口（List 实例清单 / Get 链下载），经 `service.NewVolcanoDiscoveryCertAdapter` 接入既有发现导入管线（幂等台账、映射补建、ALREADY_IN_LEDGER 全复用）。
2. **天级定时增量同步** `cert:cert-import`（01:00，与 scan 02:00 错峰）：调度窄端口驱动同步服务，枚举**全部证书可达云 × active 账号** → 适配器 List → 指纹比对台账 → 仅对「指纹不在台账」或「映射缺失/漂移」的实例执行导入/补刷；其余跳过（不拉链、省云 API）。

**冲突策略**（brainstorm 已确认，用户决策「幂等优先+守卫+漂移留痕」）：

| 冲突场景 | 代码语义 | 策略 |
|---|---|---|
| 手动 vs 定时同时导入同一证书；跨云同指纹 | 台账指纹唯一键 + `ALREADY_IN_LEDGER` 幂等 | 竞态窗口双 Create → 一方 `ErrDuplicateFingerprint` **记为 success**（幂等语义，不算失败） |
| 同一 (cloud,accountKey,cloudCertID) 内容被云端重签发 | mapping 唯一键 = `certFingerprint+cloud+accountKey`（非 cloudCertID） | 新指纹写入新映射行（刷新）；旧指纹映射**留痕不删**；`FindByCloudCertID` 按 `uploadedAt` 降序取最新（已内建换证语义） |
| 定时轮与手动轮同刻触发 | 导入会话无并发守卫 | 调度点加 CAS 防重（与 probe `probeRunning` / scan latest-running 模式一致）；手动入口不阻塞，竞态由幂等消化 |

**首 run = 全量回填**：增量算法对空台账天然等于全量导入，无需特殊代码；验收以「空台账一轮同步后全量入库」断言。

### Innovation Highlights

- **增量判定放在指纹层**（List 元数据指纹 vs 台账指纹），已映射实例不调 Get，控制六云 × 账号 × 分页的云 API 成本。
- **冲突是"语义利用"不是"新冲突解决器"**：mapping 唯一键 + uploadedAt 排序 + 指纹幂等三要素已内建在现有仓储，本期只把它们显式化为策略与测试。
- 调度接线完全对齐现有 8 个调度点的窄端口模式（`scheduler/jobs.go` 只做接线、`ioc/cert.go InitCertJobs` 挂载），零新架构。

## Requirements Analysis

### Key Scenarios

- **Happy path**：火山新购证书 → 天级轮检测到新指纹 → 导入台账 + 映射 active → probe 由 diff 收敛 consistent（3669ddbf 案例自动化）。
- **云端续期换证**：同 cloudCertID 出现新指纹 → 新映射刷新、旧映射留痕；反查取最新指纹。
- **手动与定时竞态**：同一证书被手动/定时同时导入 → 恰好一条台账，双会话均 success（无 failed 条目）。
- **空/失效账号**：账号无证书或不可达 → 该云该账号跳过，不中断其他云。
- **云 API 失败/限流**：单云单账号隔离，errorReason 记录，会话终态 partial_failed，其余云正常完成。
- **首 run**：空台账一轮后，全部 List 实例入账且映射完整。

### Non-Functional Requirements

- **安全**：凭证仅内存传递（既有 AccountScanSource 语义）；云侧错误细节不进响应仅日志。
- **幂等**：定时轮任意重复执行结果收敛（不产生重复台账/映射）。
- **只读纪律**：定时轮只入账，不触发任何线上变更（替换走变更清单，本期不做）。
- **性能**：已映射指纹不调 Get；List 分页上限与既有发现适配器对齐。
- **可观测**：同步轮结果（导入/跳过/补刷/漂移/失败）逐项落入会话 `Items[].result/errorReason`，`operator="scheduler"` 标识来源。

### Constraints & Dependencies

- `volcengine-go-sdk v1.2.9` 已在 go.mod（cloudx/volcano 依赖），无新依赖。
- 火山账号以 provider `volcano`/`volcengine` 登记（`domain.CloudProviderVolcano` 常量已存在；`accountScanSource.ActiveByCloud` 已支持按 provider 枚举）。
- 依赖 `644b067`（checkChain 缺自签根回退系统信任库）——火山 CAS 链若只存 leaf+中间亦可正常导入。

## Alternatives & Industry Benchmarking

### Industry Solutions

云厂商证书生命周期管理（阿里 CAS/火山 certificateservice/ACM）都是「证书库 + 资源绑定」模型；多云编排工具（cert-manager、Lemur）的统一姿势是「定时同步证书源到统一存储」。e-cam 幂等导入管线即此模式的轻量实现，本期只是补源 + 定时化。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing（继续手动导入） | — | 零改动 | 每次人工；漏导即 diff 误报与到期失真（2026-09-16 已证） | Rejected：成本随证书量线性增长，采购已转向火山 |
| 仅火山适配器，不加定时 | 本 proposal 简化 | 解决火山入账 | 新购/续期仍靠手动触发，滞后窗口仍在 | Rejected：只解决一半，调度才是痛点主体 |
| 每日全量重扫（不做增量） | 简化实现 | 无需指纹比对 | 六云×账号每轮全量拉链，API 成本高；已导入实例重复处理 | Rejected：增量跳过是成本红线 |
| **Chosen：火山适配器 + 指纹增量定时同步 + 幂等冲突策略** | 既有发现导入管线 + 既有调度模式 | 复用幂等/映射语义，增量控成本，冲突显式化；与现有 8 调度点同构 | 需火山适配器 + 同步服务 + 调度接线 + 冲突测试（quick 体量内） | **Selected：第一性原理下最简路径——云侧证书库是台账唯一真源，定时增量对齐它，冲突由已内建的指纹幂等天然消化** |

## Feasibility Assessment

### Technical Feasibility

SDK 已就绪（certificateservice 两方法 + 链下载）、账号体系已按 provider 枚举、发现导入管线幂等已生产验证（2026-09-16 实测）；缺口仅适配器 + 同步服务 + 调度接线 + 测试，无 showstopper。

### Resource & Timeline

参照既有五云适配器模式（aliyun_cert.go / NewAliyunDiscoveryCertAdapter），团队能力匹配；quick 模式单 feature 可容（火山适配器 + 同步服务 + 调度点 + 冲突测试）。

### Dependency Readiness

火山 certificateservice API 由云厂商保证（SDK v1.2.9 已含该 service）；`644b067` 已落 main；无新增依赖。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| 定时导入需要新增并发/冲突解决机制 | Occam's Razor / Need Gate | **Confirmed（机制面）**：指纹唯一键 + `ALREADY_IN_LEDGER` + mapping uploadedAt 排序已内建，本期显式化为策略与测试，不新增冲突器 |
| 火山适配器需从零搭建 SDK/账号接入 | Provable from codebase | **Overturned**：SDK 已在 go.mod，cloudx/volcano 全量云适配器与 provider 枚举已存在，仅缺证书库端口 |
| 定时轮需要专用的全量回填代码 | Assumption Flip | **Overturned**：增量算法对空台账天然等价全量，首 run 语义由验收断言承载 |
| 映射漂移必须主动清理旧指纹 | Need Gate | **Overturned**：反查按 uploadedAt 取最新已内建换证语义，本期仅留痕 + 会话记录漂移事件，主动孤儿回收留给清理域 |

## Scope

### In Scope

- 火山云证书库发现适配器：`internal/shared/cloudx/volcano/cert.go`（List 实例清单 / Get 链下载 → 指纹+SAN），fake SDK 单测。
- `service.NewVolcanoDiscoveryCertAdapter` + `internal/cert/module.go` 装配（加入发现导入适配器列表）。
- 多云定时增量同步服务：`cert:cert-import` 同步逻辑（云×账号枚举、指纹比对增量、映射补建/漂移刷新、未入账指纹转交导入会话、`operator="scheduler"`、CAS 防重守卫）。
- 调度点接线：`internal/cert/scheduler/jobs.go` 新增 1 个调度点（窄端口 + spec `0 1 * * *`）+ `ioc/cert.go InitCertJobs` 挂载。
- 冲突策略实现与测试：并发竞态幂等（ErrDuplicateFingerprint 视为 success）、调度防重、映射漂移留痕刷新。
- 手工触发入口：同步服务同构一个手动命令/端点（与 8 调度点共享同一切口），用于天级前试跑与运维。
- 测试：火山适配器单测、同步服务增量/幂等/漂移/首run 回填单测、调度点注册断言、六云+火山矩阵功能测试。

### Out of Scope

- 火山证书**替换上线**（变更清单 upload_and_bind → 火山 CDN/ALB/WAF 部署器）——独立 feature（Next Steps），维护「只入账、不上线」纪律。
- 火山 CDN/WAF/ALB 证书**引用扫描**（reference_scan 五云口径不变）。
- 映射旧指纹主动孤儿回收（本期仅留痕；孤儿清理域已有独立天级任务，增量评估是否纳入）。

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| 火山 certificateservice 分页/字段形态与假想不符 | M | M | Get 依赖最小字段集（Chain/FingerPrintSha256/San）；List 分页按响应结构文档化；fake 严格按 SDK 模型生成 |
| 定时轮与手动导入并发双 Create 竞态 | M | L | ErrDuplicateFingerprint 记为 success（幂等语义）；单测断言「恰一条台账 + 双会话 success」 |
| 六云×账号×分页云 API 限流/耗时 | M | M | 增量跳过已映射指纹不调 Get；逐云逐账号隔离失败；整体限时复用 discoveryImportTimeout 语义 |
| 同步轮误把「撤销/审核中」实例入账 | M | M | List 过滤 status 非 issued 实例（对齐签发语义）；IsCertificateRevoked 标记不入账，会话留痕迹 |
| 只读纪律被后续替换需求破坏 | L | M | Out of Scope 显式声明 + Success Criteria 断言「定时轮不触发任何云写操作」 |

## Success Criteria

- [ ] 火山适配器：List/Get 单测覆盖（fake volcengine SDK），chain→指纹(sha256)/SAN 解析与 CAS 口径一致；revoked/非 issued 实例不入账。
- [ ] 定时同步服务单测：新指纹实例→导入并建 active 映射；已映射实例→跳过且不调用 Get；映射缺失→补建；同 cloudCertID 新指纹（漂移）→新映射刷新 + 旧映射留痕。
- [ ] 并发竞态单测：同一指纹双会话并发导入 → 台账恰 1 条、双会话均 success（无 failed 条目）。
- [ ] 调度点注册：`cert:cert-import` spec=`0 1 * * *`、CAS 防重守卫生效（running 中再次触发不重启）。
- [ ] 首 run 回填：空台账一轮同步后，fake 云全部 List 实例入库且映射完整。
- [ ] 手工触发入口可用：`6 云 × active 账号` 枚举正确；单云失败不中断其余（会话终态 partial_failed 语义）。
- [ ] 只读纪律：同步执行路径不调用任何云写方法（单测/代码审查断言）。
- [ ] 回归：既有五云发现导入与清单生成测试全绿（新适配器不破坏现有行为）。

## Next Steps

- Proceed to `/quick-tasks` 生成任务（quick 模式，无 PRD）并执行。
- 火山证书替换（deployer UploadCert/BindResource）作为后续 feature 单独 proposal。