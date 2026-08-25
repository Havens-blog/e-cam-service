# Iteration 0 Report: Pre-Revision (Freeform Findings)

```yaml
iteration: 0
title: "Pre-Revision (Freeform Findings)"
rubric:
  all_dimensions: N/A
```

## Triage Summary

| 层级 | 数量 | 说明 |
|------|------|------|
| Factual correction（直接修改） | 8 | #1 #2 #3 #4 #5 #6 #9 #10 —— 均为代码核对可验证的事实缺陷/内部矛盾 |
| Structural（结构性修改） | 2 | #7 #8 —— 交互链路可用性与数据时效，修改方案无争议 |
| Suggestion 合并项 | 6 | #11–#14 #16 #17 与对应风险/问题成对，随主条目一并修改 |
| Partially accepted | 1 | #15 —— 改写 AWS 风险行（采纳）；真实凭证活体验证清单降级为约束节注记（部分采纳，不新增阻塞范围） |
| Subjective / skipped | 0 | — |
| Borderline | 0 | — |

Triage rate: 17/17 = 100%（≥80% ✓）；Accepted + Partially accepted: 17/17 = 100%（≥60% ✓）
Hit rate: 17/17 = 1.0（无低命中率标注）

## ATTACK_POINTS

- **[high]** Azure KeyVault secret 可能内含私钥，"私钥全程不触及"只是约定而非构造保证，私钥可经 CertPEM 明文字段落库 | quote: "风险：Azure KeyVault secret 可能内含私钥，"私钥全程不触及"目前只是约定而非构造保证，且成功标准的断言无法拦截私钥经 CertPEM 字段落库" | improvement: 定义"仅证书块"净化口径（保留 CERTIFICATE 块、丢弃 PRIVATE KEY/PKCS#12），私钥泄漏断言升级为内容级（入库 CertPEM 不含 "PRIVATE KEY" 字样）
- **[high]** 占位指纹引用（腾讯 SHA-1 回退、华为、未解析 K8s）在导入完成时点无法按指纹自动关联，闭环承诺在最需要的云上断裂 | quote: "风险："存量 CertReference 按指纹自动关联"对占位指纹引用（腾讯 SHA-1 回退、华为、未解析 K8s）在导入完成时点不成立，闭环承诺在最需要的云上断裂" | improvement: 导入会话增加"占位指纹回填"步骤（按 cloud+accountKey+cloudCertId 批量更新 cert_references 为真实指纹），SC 拆分云口径并要求腾讯 SHA-1 回退验收样本
- **[medium]** 预览承诺的 notAfter 字段在"纯 DB 聚合、无云 API"约束下无数据来源，三个约束互相锁死 | quote: "问题：预览承诺的"到期时间"字段在"纯 DB 聚合、无云 API 调用"的成功标准下没有数据来源，三个约束（秒出、不动扫描、显示到期）互相锁死" | improvement: 明确 notAfter 数据来源（inLedger 条目显示台账值、未登记条目显示"—（导入后补全）"），Out of Scope 放宽为"允许 cert_references 增加 notAfter 透传字段（不动指纹/覆盖率逻辑）"
- **[medium]** ErrDuplicateFingerprint 的"跳过"处置与"多账号各建 CloudCertMapping"场景及成功标准直接冲突 | quote: "问题：ErrDuplicateFingerprint 的"跳过"处置与"多账号各建 CloudCertMapping"场景及对应成功标准直接冲突，重复分支缺少"补建映射"的语义" | improvement: 重复分支语义改为"取既有台账证书→补建本云本账号映射→条目记成功"，预览 inLedger 判定改指纹或映射双通道
- **[medium]** CertPEM 链条口径（仅叶 vs fullchain）未定义，"与手工导入同口径"在现有适配实现下不成立，缺陷延迟到换证执行时暴露 | quote: "风险：CertPEM 的链条口径（仅叶 vs fullchain）未做任何定义，"与手工导入同口径"的断言在现有适配实现下不成立，缺陷会延迟到换证执行时才暴露" | improvement: 定义 fullchain 口径（叶在前拼装 Certificate+CertificateChain），与仅证书块净化合并为通道扩展的验收要求
- **[medium]** 无快照引导依赖同步长请求成功返回，多账号规模下被网关/浏览器超时打断 | quote: "风险：无快照引导的"一键触发扫描→自动进入预览"依赖同步长请求成功返回，多账号规模下会被网关/浏览器超时打断" | improvement: 引导改为"触发扫描→轮询快照状态→done 后进入预览"，预览响应携带快照年龄，超龄显著提示
- **[medium]** 快照无时效约束，预览可能基于远古快照做出全量默认勾选的导入决策 | quote: "风险：快照无任何时效约束（"沿用现状"即无过期机制），预览可能基于远古快照做出全量默认勾选的导入决策" | improvement: 预览响应新增 snapshotStartedAt 字段，超阈值（如 7 天）前端显著提示建议重扫
- **[medium]** 成功标准第 1 条去重公式未排除 K8s（crd）引用，按字面实现会混入预览清单 | quote: "问题：成功标准第 1 条的去重公式没有排除 K8s（crd）引用，按字面实现会把 K8s 引用计入预览清单或生成空 cloud 条目" | improvement: 验收公式补"排除 product=crd 引用；空 cloud 条目不计入"
- **[low]** AWS 首号风险描述与代码现状不符，会误导 tech-design 首任务方向 | quote: "问题：AWS 首号风险描述与代码现状不符，会把 tech-design 首任务引向错误方向" | improvement: AWS 风险行改写为真实缺口（CertificateChain 拼接口径、IAM-hosted 非 ARN 证书 ID 降级标记），tech-design 首任务重定向
- **[low]** 后端会话实体与条目形态的改造量未进入任务估算 | quote: "问题：会话"复用"只被当作前端交互复用陈述，后端会话实体与条目形态的改造量未进入任务估算" | improvement: In Scope 单列"发现导入会话实体"（新集合或条目泛型化：cloud/accountKey/cloudCertId/result/errorReason/mappedCertID）
- **[low]** 为导入路径引入仅证书块的构造性净化，私钥泄漏断言升级为内容级 | quote: "建议：为导入路径引入"仅证书块"的构造性净化，并把私钥泄漏断言升级为内容级" | improvement: 与第 1、5 条合并修改：通道层统一块级过滤 + SC 新增"入库 CertPEM 不含 PRIVATE KEY 字样、含且仅含 CERTIFICATE 块"断言
- **[low]** 为预览 notAfter 明确数据来源，三选一写进提案 | quote: "建议：给预览的 notAfter 一个明确的数据来源，三选一并写进提案，而不是留到实现期才发现矛盾" | improvement: 与第 2 条合并修改
- **[low]** 导入会话增加占位指纹回填步骤，闭环承诺改为机制内事实 | quote: "建议：导入会话增加"占位指纹回填"步骤，把闭环承诺改成机制内的事实" | improvement: 与第 3 条合并修改
- **[low]** 重复分支语义从跳过改为补建映射后记成功，预览判定改双通道 | quote: "建议：把重复分支的语义从"跳过"改为"补建映射后记成功"，预览 inLedger 判定改为指纹或映射双通道" | improvement: 与第 4 条合并修改
- **[low]** 改写 AWS 风险行，tech-design 首任务重定向并附真实凭证验证 | quote: "建议：改写 AWS 风险行，并把 tech-design 首任务重定向到真实缺口，附带一次真实凭证活体验证" | improvement: 与第 9 条（AWS 风险行）合并；真实凭证活体验证清单以注记形式写入 Constraints（与 doc-fix-1 平行，不作为本期阻塞项）
- **[low]** 无快照引导改为触发扫描加轮询状态，预览响应携带快照年龄 | quote: "建议：无快照引导改为"触发扫描→轮询快照状态→done 后进入预览"，预览响应携带快照年龄" | improvement: 与第 6、7 条合并修改
- **[low]** 修正验收公式排除 crd 引用，会话实体任务单列 | quote: "建议：修正验收公式并校准任务清单，避免 quick 模式下的实现期返工" | improvement: 与第 8、10 条合并修改

## BORDERLINE_FINDINGS

（无）

## SKIPPED_FINDINGS

（无）


## Reviser Edit Results (P0.5e execution)

Pre-revision applied 8 substantive fixes to proposal.md (17/17 attack points addressed):

1. NFR 安全条目重写为"构造性净化"（仅 CERTIFICATE 块净化序列、块级过滤、丢弃 PRIVATE KEY/PKCS#12、Azure KeyVault secret 全量值必须走此净化、原始 buffer Zeroize）+ 私钥 SC 升级为内容级断言 → #1/#11
2. 占位指纹回填机制写入 Key Scenarios/In Scope/Out of Scope，SC-6 拆分云口径并要求腾讯 SHA-1 回退验收样本 → #3/#13
3. notAfter 数据来源定案（inLedger 显示台账值，未登记显示"—（导入后补全）"）→ #2/#12
4. ErrDuplicateFingerprint 语义改"补建映射后记 success"，预览 inLedger 双通道判定 → #4/#14
5. fullchain 口径定义（叶在前，AWS 拼接 Certificate+CertificateChain）→ #5
6. 无快照引导改"触发→轮询→done 进预览"，预览携带 snapshotStartedAt 超 7 天提示 → #7/#8/#16
7. SC-1 公式排除 product=crd；发现导入会话实体 In Scope 单列；任务估算 9-13 → #9/#10/#17
8. AWS 风险行改写为两个真实条目（CertificateChain 拼接口径、IAM-hosted 降级）；Constraints 增手动验证清单注记 → #6/#15

代码侧核实（只读）：aws/cert_discovery.go（GetCert 已实现/CertificateChain 未读取/IAM-hosted 报错）、certtest/change_fakes.go:754（FindByCloudCertID）、deployer/tencent_deployer.go:220（占位指纹公式）。
