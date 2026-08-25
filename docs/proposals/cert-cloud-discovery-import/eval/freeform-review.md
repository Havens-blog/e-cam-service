# Freeform Expert Review

## Background Assessment

这份提案要解决的问题我读下来是：证书管理功能域（台账、引用三态、变更状态机、双云部署器）已经全部交付，但台账是空的，整条链路处于"有引擎没燃料"的状态。现有唯一登记通道是手工收集 PEM 上传，首次登记的人力和遗漏成本让台账长期空置，进而引用扫描落库的 CertReference 无法按指纹关联到证书，到期监控形同虚设。提案的核心思路是：不新做发现能力，而是把最近一次引用扫描快照当作"待登记清单"，用预览（纯 DB 聚合）→ 勾选确认 → 异步逐证 GetCert 取公钥 PEM → 解析登记（fingerprint_only，不碰私钥）→ CloudCertMapping 幂等建档的两步式流程，把"扫描（只读发现）"与"登记（台账写入）"解耦后再闭合。

我对提案引用的证据逐条做了代码核对，基本属实：台账空态文案确在 `D:\Haven\e-cam-web\src\views\cert\ledger\index.vue`（空态卡片 + 批量导入 CTA），前端仓库当前分支确为 `feat/platform-user-management`，`BatchImportModal.vue` 会话进度组件存在；后端 `internal\cert\service\reference_scan_service.go` 的五云+K8s 发现、指纹解析（映射反查 → GetCert fallback → 确定性占位指纹）与提案描述一致；四云 GetCert 的 PEM 来源（阿里 `response.Cert`、腾讯 `CertificatePublicKey`、Azure KeyVault `getSecret`、AWS ACM `GetCertificate`）都真实存在，华为 SCM 确实只有指纹字段无 PEM；`ImportCert` 的 keyPEM 为空分支走 `HostingStatusFingerprintOnly`、`uk_fingerprint` 冲突回 `ErrDuplicateFingerprint`；`CloudCertMappingRepository.Upsert` 按 fp+cloud+accountKey 幂等；批量会话 10 分钟整体限时、单条 panic 隔离；导入类端点均挂 `RequireRoles(RoleOpsEngineer)`。

提案赖以成立的假设主要有三个：一是"被引用的证书"就是首次登记的核心价值集（未被引用的库内证书 v2 再说，用户已确认）；二是 fingerprint_only + 手动补传私钥足以支撑后续托管变更（用户已确认）；三是预览可以完全不碰云 API（扫描时指纹已落库）。前两个假设我在安全与运营角度都认同——云侧私钥下载行为各云不一致且权限敏感，不回传私钥把暴露面压到最小。第三个假设和"到期时间"字段冲突，是下文要展开的问题之一。

需要说明我的审阅基线：本功能是登记型写入（不产生任何云侧变更），不属于"一次换证就是一次全网高危变更"的执行面，所以我的检查重心不在变更回滚，而在三处：私钥/凭证的暴露面是否靠构造而非约定保证、多云数据口径差异被抽象掩盖的程度、以及"发现→登记→引用关联"这个闭环是否真的在导入完成时点闭合。结论是：方案的增量判断（复用三链路、零新云 API 面）是准的，但闭环承诺里有两处（占位指纹关联、多账号映射）与现有代码机制直接矛盾，安全面有一处（Azure）是提案没有意识到的真实红线。

## Key Risks

先说最重的一处安全面。提案的安全 NFR 写的是"公钥 PEM 仅用于解析登记（台账 CertPEM 字段存储，与手工导入同口径）；私钥全程不触及"。这句话在 Azure 上不成立——`D:\Haven\e-cam-service\internal\shared\cloudx\azure\cert_discovery.go` 的 GetCert 走的是 `kv.getSecret(ctx, cloudCertID)`，取回的是 KeyVault secret 的**完整值**，随后 `parseCertLeafPEM(value)` 只是为了算指纹挑出叶子证书。问题在于：KeyVault 证书型 secret 在密钥策略为 exportable 时，其值是"私钥+证书"的完整 PEM bundle（或 PKCS#12），不是纯公钥。扫描链路今天取了就丢，没有持久化风险；而发现导入链路要把"GetCert 取回的 PEM"送进 `ImportCert` 并以 `CertPEM` 字段落库——台账的 CertPEM 是明文字段，不加密、随备份扩散。如果实现按"适配层把原始返回值透出、服务层原样入库"的最自然路径走，私钥就会以明文进入数据库，而提案定义的守护断言完全测不出来。

风险：Azure KeyVault secret 可能内含私钥，"私钥全程不触及"目前只是约定而非构造保证，且成功标准的断言无法拦截私钥经 CertPEM 字段落库
> "公钥 PEM 仅用于解析登记（台账 CertPEM 字段存储，与手工导入同口径）；私钥全程不触及" —— Azure 的 GetCert 数据源是 KeyVault secret 全量值（见 `internal\shared\cloudx\azure\cert_discovery.go` 中 `kv.getSecret` 后仅 `parseCertLeafPEM` 取叶），exportable 密钥策略下该值含 `PRIVATE KEY` 块。台账 `CertPEM` 为明文存储字段，一旦原始 secret 值被原样登记入库，私钥即随数据库与备份扩散；而提案的守护断言是"EncryptedPrivateKey 为空、hostingStatus=fingerprint_only（单测断言）"，只断言了字段级状态，对"私钥藏进 CertPEM"这一路径零覆盖。这是安全红线问题，必须在适配层或服务层用块级过滤构造性排除，而不是依赖"我们只取公钥"的流程约定。

第二个问题是预览的一个字段没有数据来源。预览承诺"秒出"七个字段，其中"到期时间"（notAfter）在"纯 DB 聚合、无云 API"的约束下无处可取：`cert_references` 集合的文档结构（`internal\cert\domain\cert_reference.go`）只有指纹/云/产品/资源/引用证书 ID/账号/快照 ID/扫描时间八个字段，没有 NotAfter；扫描时 GetCert 明明拿到了 `CloudCertStatus.NotAfter` 却在算完指纹后丢弃。而提案同时把"扫描能力本身的变更（快照/引用/覆盖率逻辑不动）"划出了范围，等于把"给引用补 NotAfter 字段"这条最自然的取数路径也锁死了。

问题：预览承诺的"到期时间"字段在"纯 DB 聚合、无云 API 调用"的成功标准下没有数据来源，三个约束（秒出、不动扫描、显示到期）互相锁死
> "预览列表（基于最近 done 快照纯 DB 聚合比对，秒出：云/账号/云证书 ID/引用资源数/按指纹判定的"已在台账"标记/到期时间）" 与成功标准 "响应耗时 < 1s（纯 DB 聚合，无云 API 调用）" —— 未登记条目（恰恰是预览的主角）的 notAfter 既不在 cert_references 里（该集合无此字段），又不允许调云 API，又不允许改扫描落库结构（Out of Scope："扫描能力本身的变更（快照/引用/覆盖率逻辑不动）"）。到期时间是首次登记的核心决策依据——运营者正是靠它决定先导入哪批、先补哪张私钥；提案自己的 Urgency 都写着"到期监控盲区多存在一天"就多一天线上事故风险。若实现时被迫砍掉该字段或只在 inLedger 条目上有值，预览的价值主张和成功标准第 2 条（"每个条目含 …notAfter…"）都会落空；若偷偷在预览里调 GetCert，则又推翻了提案在 Assumptions Challenged 里刚论证过的"纯 DB 比对即可秒出预览"。

第三处是闭环承诺与现有匹配机制的结构性矛盾，这是我认为本提案最大的逻辑缺口。提案的用户旅程和成功标准都承诺导入完成后存量引用"自动关联"，但正向引用视图（`internal\cert\service\reference_query_service.go` 的 `References`/`groupReferences`）是按 `r.CertFingerprint == fingerprint` **精确匹配**的。对阿里云这类 GetCert 能给出 SHA256 对齐指纹的云，扫描落库的就是真实指纹，导入后即匹配，没问题。但提案自己明确知道存在占位指纹：扫描时无法解析指纹的引用（腾讯 SHA-1 回退、华为、部分 K8s）落库的是 `sha256("certscan-unresolved:"+cloud|accountKey|certID)`，这个值永远不会等于导入时从 PEM 算出的真实指纹。导入流程只写台账和 CloudCertMapping，不改 cert_references；占位指纹要被替换成真实指纹，唯一的既有路径是**下一次扫描**经 `resolveUncached` 的映射反查命中。也就是说，对腾讯云——两大主力云之一——导入完成后台账详情的引用列表依然是空的，要等一轮重扫才恢复。

风险："存量 CertReference 按指纹自动关联"对占位指纹引用（腾讯 SHA-1 回退、华为、未解析 K8s）在导入完成时点不成立，闭环承诺在最需要的云上断裂
> "完成刷新台账，存量 CertReference 按指纹自动关联，覆盖率即时生效" 以及成功标准 "导入完成后，存量 CertReference 按指纹自动关联（台账详情引用列表非空）" —— 与提案自己的场景 "占位指纹引用（扫描时无法解析指纹，如腾讯 SHA-1 口径）：预览标记"导入时解析"" 相矛盾：占位指纹引用落库的指纹是确定性伪值，导入时解析出的真实指纹与之不相等，引用视图按指纹精确匹配（`reference_query_service.go`）自然关联不上；现有代码里唯一的修复路径是下轮扫描经 CloudCertMapping 反查（`resolveUncached` 先查映射）。后果是运营者导入完刷新台账，看到腾讯证书"无引用"，与扫描视图里明明有引用互相打架，要么当成 bug 报障，要么据此做出错误的换证范围判断；成功标准第 6 条若用腾讯证书做验收用例会直接失败。同样的机制也使 "K8s 侧 CRD 引用不纳入导入（引用关联自动生效，但证书材料不从 K8s 读取）" 里的"自动生效"只对已解析出真实指纹的引用成立。

第四处是重复语义的自相矛盾，它在多账号这个提案自己列出的关键场景里直接打架。导入主流程是"逐证 GetCert 取公钥 PEM→解析→指纹登记→CloudCertMapping 幂等建档"，而指纹登记复用 `ImportCert`——它对已存在指纹的行为是整体报错（`uk_fingerprint` 冲突 → `ErrDuplicateFingerprint`，见 `internal\cert\service\import_service.go`），并不返回既有证书。提案对这条错误的处置是"记为该条跳过"。如果"跳过"意味着该条目到此为止，那么第二个账号的 CloudCertMapping 永远建不出来——可多账号场景明确要求"多账号各建 CloudCertMapping"，成功标准也要求"CloudCertMapping 按账号各 1 条"。

问题：ErrDuplicateFingerprint 的"跳过"处置与"多账号各建 CloudCertMapping"场景及对应成功标准直接冲突，重复分支缺少"补建映射"的语义
> "导入中并发到达同指纹 → 依赖现有 uk_fingerprint 哨兵（ErrDuplicateFingerprint）记为该条跳过，不报错" 与 "多账号各建 CloudCertMapping（uk_fp_cloud_account 两段去重）" —— 同一证书被两个账号各自上传引用是多云运维的常态（各地域/各账号独立入册同一张 EV/OV 证书），第二账号条目导入时 ImportCert 必然撞 uk_fingerprint 报错，若按"跳过"中止该条，第二账号的映射缺失；而映射恰是后续换证变更按账号定位云证书、以及下轮扫描指纹反查的锚点。缺映射的后果是：该账号的引用要么继续挂占位指纹，要么换证时变更单无法生成该账号的部署项——问题被推迟到最高危的时刻才暴露。附带地，预览的"按指纹判定的'已在台账'标记"对占位指纹条目同样失灵：重跑预览时占位指纹与台账真实指纹比对不上，已导入的腾讯条目会再次出现在"未登记"列表里被默认勾选，幂等退化为靠导入时撞唯一键兜底，与"重跑仅处理剩余项"的体验承诺不符。

第五处是证书材料口径。提案轻描淡写地说 CertPEM 存储"与手工导入同口径"，但现有四云 GetCert 的实现全部是 `parseCertLeafPEM` 取**叶子**算指纹后即弃——没有任何一处保留完整链。AWS 的 `GetCertificate` 响应里 `Certificate` 与 `CertificateChain` 是两个成员，现有代码只读前者（`internal\shared\cloudx\aws\cert_discovery.go`）；而手工导入的口径（见 `internal\cert\certtest\certtest.go` 的约定：leaf + 中间 CA + 自签根）是 fullchain。台账 CertPEM 不是死数据——它是后续换证时部署器 `UploadCert` 的上传材料源。如果发现导入落库的是 leaf-only，那么当运营者对这些证书"补传私钥升级 complete→发起换证"时，上传到云端的证书缺中间链，部分产品（CDN/WAF 全链校验）会拒收或线上浏览器报缺中间证书——故障在最危险的操作时刻（批量换证执行中）才显形。

风险：CertPEM 的链条口径（仅叶 vs fullchain）未做任何定义，"与手工导入同口径"的断言在现有适配实现下不成立，缺陷会延迟到换证执行时才暴露
> "公钥 PEM 仅用于解析登记（台账 CertPEM 字段存储，与手工导入同口径）" —— 四云 GetCert 现状均为"解析叶子→算指纹→丢弃 PEM"（阿里/腾讯/Azure/AWS 同构，AWS 还显式忽略 CertificateChain），把通道改为"暴露 PEM"时若无链条拼装规则，最自然的实现就是只存叶子；而台账 CertPEM 是后续变更执行时部署器上传云端的材料（UploadCert 签名直接消费 certPEM）。leaf-only 材料在部分云产品的证书链校验和浏览器信任上不完整，意味着这批"发现导入"的证书在第一次真实换证时才被发现不可用——把数据口径问题转化成了高危变更的执行期故障。

第六处相对小但会误导排期：风险表的首号风险是过时的。

问题：AWS 首号风险描述与代码现状不符，会把 tech-design 首任务引向错误方向
> "AWS ACM GetCertificate 响应未映射 PEM（仅有接口定义）" —— `internal\shared\cloudx\aws\cert_discovery.go` 的 GetCert 已经消费 `output.Certificate` 并 `parseCertLeafPEM` 得出指纹与 NotAfter，"仅有接口定义"不成立。AWS 真实的缺口有两个：`CertificateChain` 成员被忽略（见上一条链条口径问题），以及非 ARN 形态 ID 直接报错（"IAM-hosted certificate not covered"，CloudFront 引用的历史形态），后者会在导入会话里表现为整类条目失败。若首任务定为"复核响应字段是否存在"会一无所获，真正该复核的是链拼接与 IAM-hosted 降级。

第七处是交互链路的可用性。无快照场景的引导流程建立在同步扫描请求之上，而现有 `StartScan` 是"同步执行至终态"——五云 × 多账号 × 多产品、逐通道限流串行，账号规模一大这就是分钟级长请求。浏览器或网关超时切断连接后 ctx 取消，扫描中途失败；"完成后自动进入预览"的前提（这一次 HTTP 请求活着等到 done）在大账号下不成立。

风险：无快照引导的"一键触发扫描→自动进入预览"依赖同步长请求成功返回，多账号规模下会被网关/浏览器超时打断
> "无扫描快照：预览端点返回明确错误码（NO_SNAPSHOT），前端引导一键触发既有扫描，完成后自动进入预览。" —— StartScan 的契约是同步执行至终态（`reference_scan_service.go` 注释与实现一致），五云多账号限速串行的全量扫描时长随账号数线性增长；请求被超时切断后上下文取消，扫描通道大面积失败甚至整体转 failed，"自动进入预览"落空且用户看到的是失败态。这个引导路径恰是"台账为空 + 无快照"的冷启动第一步——最需要它可靠的地方它最脆弱。

第八处是数据时效。预览的全部价值建立在"最近 done 快照"仍是现状的近似，而提案明确放弃了时效校验。

风险：快照无任何时效约束（"沿用现状"即无过期机制），预览可能基于远古快照做出全量默认勾选的导入决策
> "数据源依赖：必须先有 status=done 的 ScanSnapshot（前端已引导）。快照过期策略沿用现状，不做新校验。" —— 现状是 cert_references 按 snapshotId 累积、无 TTL 无过期判定，一个三个月前的 done 快照照样可用；唯一缓解是"预览明确标注'基于快照时点'"，一行标注挡不住"默认全选未登记项"的批量确认。基于陈旧快照导入的直接后果有限（fingerprint_only 登记可重跑幂等、云侧删除会被导入时 GetCert 拦下），但它系统性扭曲运营者的判断：引用资源数、覆盖率、哪些证书"在被使用"全是旧世界的数据，到期监控建立在旧引用集上。

第九处是验收标准的口径漏洞。

问题：成功标准第 1 条的去重公式没有排除 K8s（crd）引用，按字面实现会把 K8s 引用计入预览清单或生成空 cloud 条目
> "预览端点返回唯一证书清单条目数 = 快照内引用按（cloud+accountKey+cloudCertId）去重后的证书数（含占位指纹条目与华为云不可选组）" —— crd 引用同样携带 ReferencedCloudCertID（其值为 CRD 证书字段的任意字符串，如 secret 名或跨云证书 ID），cloud/accountKey 为空串；按公式去重它们会形成独立条目或与云引用混入同一桶，与 "K8s 侧 CRD 引用不纳入导入（引用关联自动生效，但证书材料不从 K8s 读取）" 冲突。验收公式需显式写明 product=crd 排除，否则测试用例会出现"预览条目数对不上"或"K8s 条目可勾选"两类返工。

第十处是任务估算的盲区。"复用批量导入会话交互"在前端层面成立（BatchImportModal.vue 的进度交互确实可复用），但后端的 CertBatchSession 文档模型是 `Files[]{FileName, Result, CertID, ErrorReason}` 加 TTL 30 天与 operator 语义——发现导入的条目是 (cloud, accountKey, cloudCertId) 三元组，既没有"文件名"也没有 multipart 来源。要么泛化 schema、要么新建会话集合，二者都是提案"后端 4-5 个任务"里没有显式列出的形态工作。风险表里"前端 Modal 与既有批量导入进度组件耦合改坏现有交互"盯着的是前端，真正的形态差异在后端。

问题：会话"复用"只被当作前端交互复用陈述，后端会话实体与条目形态的改造量未进入任务估算
> "进度轮询（复用批量导入会话交互）" 与 "进度组件仅复用不改内部实现；新增交互独立组件" —— 批量会话的持久化模型（`internal\cert\domain\batch_session.go`）以文件为条目原子（FileName 是展示与错误定位的主键），发现导入条目需要承载云/账号/云证书 ID 与逐条 errorReason，还要求导入完成后能按条目回查 CloudCertMapping 建档结果；直接沿用会把 cloudCertId 塞进 FileName 之类的伪装字段。这是小而非零的工作量，且属于"会话先持久化再异步执行（浏览器中断不丢结果，重开可见）"这条 NFR 的载体，估算失真会在 quick 模式无 design 的情况下直接变成实现期返工。

## Improvement Suggestions

以下建议按对风险的面貌影响排序。前四条对应上面的核心矛盾与安全红线，建议全部吸收进 In Scope 与成功标准后再进入 `/quick-tasks`；后三条是验收与排期校准。

建议：为导入路径引入"仅证书块"的构造性净化，并把私钥泄漏断言升级为内容级
Addresses: Azure KeyVault 私钥落库风险；链条口径风险
> What changes: GetCert 扩展暴露 PEM 时，返回值定义为"过滤后的 CERTIFICATE PEM 块序列"而非原始响应——在适配层或服务编排层统一执行块级过滤（保留 `CERTIFICATE` 块、丢弃 `PRIVATE KEY`/PKCS#12 内容，Azure 必须走此净化），AWS 侧同时把 `Certificate` 与 `CertificateChain` 按叶在前的顺序拼接，形成与手工导入一致的 fullchain 口径；原始 buffer（尤其 Azure secret 值）用后即 `domain.Zeroize` 置零。成功标准在"EncryptedPrivateKey 为空"之外新增两条断言：入库 CertPEM 不含 "PRIVATE KEY" 字样、含且仅含 CERTIFICATE 块；错误路径（解析失败）的 errorReason 采用静态文案，不携带响应片段（对齐扫描链路 `scanFailureReason` 的截断口径）。另外建议把"发现 KeyVault secret 含可导出私钥"记为一条安全审计事件——这本身是有价值的密钥治理发现，而不是需要静默丢弃的噪音。

建议：给预览的 notAfter 一个明确的数据来源，三选一并写进提案，而不是留到实现期才发现矛盾
Addresses: 到期时间无数据来源的问题
> What changes: 推荐组合方案——(a) 预览对 inLedger 条目显示台账 NotAfter、对未登记条目显示"—（导入后补全）"，保住"纯 DB 聚合 < 1s"的成功标准；(b) 把 Out of Scope 中"扫描能力本身的变更（快照/引用/覆盖率逻辑不动）"修改为"允许 cert_references 增加 notAfter 透传字段（不动指纹/覆盖率逻辑）"，让下轮扫描起未登记条目也能显示到期。若产品上坚持未登记条目也要即时显示到期，则明确预览改为分区加载（先秒出清单、到期列异步补齐），并同步修订"响应耗时 < 1s（纯 DB 聚合，无云 API 调用）"这条成功标准的适用范围。三种方案都比现状好——现状是一个注定无法同时满足的成功标准。

建议：导入会话增加"占位指纹回填"步骤，把闭环承诺改成机制内的事实
Addresses: 占位指纹引用关联断裂的风险
> What changes: 每条导入成功（含补建映射的重复条目）后，按 (cloud, accountKey, cloudCertId) 将 cert_references 中仍为占位指纹（`certscan-unresolved:` 派生值）的引用批量更新为真实指纹——这是导入侧的补偿写，不触碰扫描编排本身，Out of Scope 的表述相应改为"扫描编排不动，引用指纹回填由导入会话承担"。若不想加写路径，替代方案是导入完成后自动触发一次重扫描并在 UI 明示"引用关联将在重扫后生效"；但那会把闭环时点拉长一整个扫描周期，与"覆盖率即时生效"的叙事更不匹配。成功标准第 6 条同步拆成分云口径：真实指纹引用导入后即关联、占位指纹引用在回填后关联，验收用例必须包含一个腾讯 SHA-1 回退样本。

建议：把重复分支的语义从"跳过"改为"补建映射后记成功"，预览 inLedger 判定改为指纹或映射双通道
Addresses: ErrDuplicateFingerprint 与多账号 CloudCertMapping 的矛盾
> What changes: 导入编排捕获 `ErrDuplicateFingerprint` 后不复用"失败记因"路径，而是 `GetByFingerprint`（仓储接口已有）取既有台账证书，继续 `CloudCertMappingRepository.Upsert` 建立本云本账号映射，条目结果记 success 并附"已在台账，已补建映射"说明；仅当 GetCert 或解析失败才走失败记因。预览的"已在台账"判定改为"台账指纹命中 或 FindByCloudCertID(cloud, accountKey, cloudCertId) 映射命中"（该方法已支持精确三元组查询），占位指纹条目在重跑预览时才能被正确灰选，"重跑仅处理剩余项（幂等）"从导入期兜底上移到预览期呈现。

建议：改写 AWS 风险行，并把 tech-design 首任务重定向到真实缺口，附带一次真实凭证活体验证
Addresses: AWS 风险描述过时的问题；fake 适配器无法覆盖真实 PEM 形态差异
> What changes: 风险表的 AWS 行改为两个真实条目——"GetCertificate 的 CertificateChain 拼接口径"（联动链条口径建议）与"IAM-hosted（非 ARN）证书 ID 的降级标记"（预览标记不可选，与会话失败记因区分）。tech-design 首任务相应从"复核响应字段是否存在"改为"复核链拼接与 IAM-hosted 降级"。同时建议增加一份与 doc-fix-1 解耦的单次手动验证清单（每云一张真实证书走一遍发现导入全链路，核对 PEM 块数/链序/secret 形态），因为"本功能单测以 fake 适配器覆盖，不依赖真实凭证"意味着各云真实响应形态（尤其 Azure secret 与腾讯 PEM 缺失场景）在 CI 里完全没有被看见——四条"已在代码中验证可得"的结论验证的是"代码路径存在"，不是"云侧真的这么返回"。

建议：无快照引导改为"触发扫描→轮询快照状态→done 后进入预览"，预览响应携带快照年龄
Addresses: 同步扫描长请求超时风险；陈旧快照风险
> What changes: 前端触发扫描后不再依赖该次 HTTP 请求存活至终态，而是轮询快照状态（running→done/failed，done 后拉预览，failed 展示 partialFailures）；后端本来就有 running 防重与超时恢复（15 分钟周期调度），轮询模式与其天然契合。预览响应新增 snapshotStartedAt（或快照年龄）字段，超过阈值（例如 7 天）时前端在预览顶部显著提示"快照已过期 X 天，建议重扫后再导入"，把"基于快照时点"从一行角落标注升级为决策前置条件。若愿意更进一步，预览端点对超龄快照返回可覆盖的 STALE_SNAPSHOT 提示码，与 NO_SNAPSHOT 形成同一族引导。

建议：修正验收公式并校准任务清单，避免 quick 模式下的实现期返工
Addresses: 成功标准未排除 crd 引用的问题；会话实体形态改造未估算的问题
> What changes: 成功标准第 1 条补一句"（排除 product=crd 引用；空 cloud 条目不计入）"；In Scope 的后端任务中把"发现导入会话实体"单列（新集合或 Files 条目泛型化，字段至少含 cloud/accountKey/cloudCertId/result/errorReason/mappedCertID），不与前端"复用进度组件"混为同一项工作。quick 模式没有 design 兜底，这类形态决策前置到提案里写死，比留给执行期现场发挥的成本低一个量级。
