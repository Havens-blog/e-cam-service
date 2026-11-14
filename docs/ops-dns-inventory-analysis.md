# 探测目标数据质量全量分析（3029 域，2026-11-14）

**方法**：对 3029 个探测域名逐个做 DNS CNAME/A 解析（并发 25，全部查询完成，0 错误），据 CNAME 目标判定目标类型。

## 一、全量分布

| 分类 | 数量 | 占比 | 说明 |
|---|---|---|---|
| 有 CNAME | 1288 | 42.5% | 走 CDN/ALB/NLB/OSS 等 |
| 无 CNAME（A 记录）| 1317 | 43.5% | 直连 IP |
| **NXDOMAIN** | **424** | **14.0%** | **域名在公网已不存在** |
| 解析错误 | 0 | — | — |

## 二、关键发现

### 2.1 NXDOMAIN 424 个（14%）——T4 僵尸打标的实际规模

此前基于 `unreachable` 子集抽样外推「≈147 个」，全量解析后实际是 **424 个**（因为部分 NXDOMAIN 域在探针侧先表现为别的失败）。这直接决定 T4 上线后「疑似僵尸」标记的预期数量级：**约 424 个域名将在连续 3 轮后被打标**。

### 2.2 「随机标签主机名」这个规则是错的

我此前把 81 个随机短标签域视为一类候选，全量核对后发现它们**不是同质的**：

| 子类 | 例 | 是否应过滤 |
|---|---|---|
| Google 站点验证别名 | `gv-c6vh3ouihmgdcp.dv.googlehosted.com` | **是**（非 TLS 端点）|
| Bing 站点验证 | `verify.bing.com` | **是** |
| QQ 邮箱验证 | `mail.qq.com` | **是** |
| 真实云资源（长标签但真实）| `attachment.forface3d.com.c.vedcdnlb.com`、`alb-*.aliyuncsslbintl.com`、`*.elb.amazonaws.com`、`*.cloudfront.net` | **否**（是真实端点，部分已 consistent）|
| 重复后缀（已由 #634 修）| `flexheater.jlcpcb.com.jlcpcb.com` | 已在生成点修复 |

**结论：不应按「标签是否随机」过滤，应按「CNAME 是否指向已知验证服务」过滤。**

### 2.3 验证类别名（可安全跳过的集合）：34 个

| CNAME 目标 | 数量 |
|---|---|
| `*.dv.googlehosted.com`（Google Workspace 站点验证）| 25 |
| `mail.qq.com`（腾讯企业邮验证）| 4 |
| `verify.bing.com`（Bing 站长验证）| 2 |
| `*.helpscout.net` | 2 |
| `*.baidu.com`（百度站长验证）| 1 |
| **合计** | **34**（占 1.1%）|

这些域名永远不可能 `consistent`（它们不是我们的 TLS 端点），当前会被判 `unreachable`/`diff`，属纯噪声。

## 三、建议（按收益排序）

| # | 动作 | 收益 | 工作量 |
|---|---|---|---|
| 1 | **不做**「随机标签过滤」（规则错误，会误伤真实云资源）| 避免新 bug | 0 |
| 2 | 验证类别名跳过：新增 `verification_skipped`（CNAME 命中 Google/Bing/QQ/Baidu/HelpScout 验证目标）| 清 34 个噪声 | 0.3 天 |
| 3 | T4 的僵尸阈值按 424 规模评估告警策略（建议打标不告警，仅列表可见 + 清理建议清单）| 避免 424 条告警淹没 | 0（T4 已是打标不告警）|
| 4 | 重复后缀（#634 已修）下一轮采集验证 252→0 | 已交付 | 0 |

## 四、数据落盘

- 全量解析结果：`D:\Haven\.browser-verify\cam-t5\verify-alias.json`（含 stats 与 34 条验证别名清单）
- 探测原始数据：`D:\Haven\.browser-verify\cam-urlaudit\probes-all.json`
