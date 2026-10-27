# 云管模块迭代记录（agent-iterations）

> 本记录由自动化迭代代理维护。每轮一个需求切片，记录需求、分支、Commit、PR、
> 验收结果、证据位置与剩余事项。协议要点：浏览器 UI 验证为完成门槛；接口成功
> ≠ UI 验收成功；模拟云服务成功 ≠ 真实云验证成功；PR 创建 ≠ 需求合并。

## 项目识别（2026-10-27 首轮盘点）

### 仓库与技术栈

| 仓库 | 用途 | 技术栈 | CI |
|---|---|---|---|
| `e-cam-service` | 后端 API（云管主体） | Go 1.25 + ego/ginx + gin + mongo-driver + go-redis；go.work 联动 e-cloudx-sdk / e-common-go / eiam | `ci.yml`（test job：mongodb/redis service containers）+ `deploy.yml` |
| `e-cam-web` | 前端 | Vue 3 + Vite 7 + element-plus + pnpm；vitest + happy-dom（无既有 E2E 框架） | `ci.yml` + `deploy.yml` |
| `e-cloudx-sdk` | 多云 SDK（适配层） | Go；`providers/providers.go` 为厂商实现唯一注册入口（blank import 清单） | 无 ci.yml |

### 本地启动与测试

- 统一入口 `D:\Haven\dev.ps1`（start/stop/restart/status/logs），注册 9 个服务；日志 `D:\Haven\logs\dev\`。Go 服务经 CompileDaemon 改源码热编译（先编译成功才重启）。
- 后端 `e-cam-service start`，配置 `config/prod.yaml`，`:8001`；测试 `go test ./internal/cam/...`（`tests/` 大套件按既有惯例分包执行，勿单发全量）。
- 前端 e-cam-web `pnpm run dev`（`:5173`，vite base=`/cam/`，`/api` 代理 → `:8001`）；typecheck `vue-tsc -b`；单测 `npx vitest run`。
- e-cam-web 以 `link:../haven-ui` 引用兄弟仓 `@haven/ui`（cookie/provider/format 等下沉件）。

### 前端请求后端 / 登录与权限

- 浏览器 → vite(:5173, base `/cam/`) → 代理 `/api` → `:8001`。
- 认证：EIAM 会话（cookie `ecmdb-token-key`），后端 `RequireTenant` 中间件按会话租户过滤；会话与成本缓存共用 Redis db 1（`106.52.187.69:6379`）。
- 敏感数据：云账号 `AccessKeySecret` AES 加密存储（`crypto.InitDefaultCrypto(security.encryption_key)`，键在 `config/prod.yaml`；仓库层解密失败会**静默回退密文**——已知隐患，见本轮建议）。

### 云平台适配层与调用边界

- e-cloudx-sdk：billing 适配器 `billing.RegisterBillingAdapter`（init 注册），`FetchBillDetails` 契约 = 返回 `[StartTime, EndTime]` 覆盖的**全部账期月**明细；`billing.EnumerateMonths` 为逐月遍历公共助手。
- 采集链路（生产路径）：`collector.CollectAccount`：账号级 Redis 锁 → 采集日志（ecam_cost_collect_log）→ 适配器拉取 → normalizer 标准化 → 按月先删后插去重（raw_bill / unified_bill）→ 汇总表按范围自动重建（`ecam_cost_daily_summary`）。
- 查询链路：`analysis.CostService` → 预聚合表 `ecam_cost_daily_summary`（summaryDAO 为 nil 才回退 unified_bill）；缓存 `finops:cost:summary:*`(TTL 5min)、`finops:cost:trend:*`(TTL 30min)。
- 云侧边界事实：采集/资产同步为**只读云 API 调用**（账单、资产列表），本模块不存在云资源写操作；开发环境直连用户配置的真实云账号（用户明确要求的实盘数据场景）。

### 浏览器测试工具

- 项目内无 E2E 框架。本轮按协议"最小必要引入"处理：优先以真实浏览器手工路径 + 控制台/网络证据记录；如引入 Playwright 需遵守 pnpm 依赖约定并单独评审。

### 现存云管功能（代码/数据实证，非推测）

- 云账号管理：CRUD、连通性测试、5 个活跃账号（aliyun/huawei/aws/tencent/volcengine，tenant_id=3）。
- 账单采集：6 小时 cron + 「采集管理」手动触发 + `scripts/backfill_*.go` 补采工具。
- 成本分析：概览、分布（provider/service_type/region）、趋势、对比；数据源=汇总表。
- 其余：资源同步、异常检测、预算、分摊规则等（见 `docs/cam-module-summary.md`）。

### 已知环境事实与并发

- 前序会话遗留未提交改动：e-cam-service `cmd/probe-applog/main.go` + `go.mod`（属已合并 PR #4 的配套工具）；e-cam-web 统一登录/导航切片（多文件）。本轮提交**不纳入**这些文件。
- 并行会话活跃：2026-10-27 09:48 前后 `e-cloudx-sdk/logquery/volcengine/*` 存在进行中编辑（一度导致整仓编译失败，09:52 恢复）。迭代代理遵守：不触碰他人 WIP，轮询等待编译恢复。

---

## cam-iteration-20261027-multicloud-cost

**状态**：见文末"本轮结论"。

### 需求来源

用户确认需求（2026-10-26/27 对话）："成本统计为什么现在只有阿里云的，其他云也要实现"。上一轮（协议外）已定位并修复两个采集缺陷并热部署，本轮为协议内落地：提交/PR + 历史账期补齐 + 浏览器验收。

### 缺陷根因（已修复）

1. `collector.calculateIncrementalRange`：`GetLastFailed` 返回的历史失败日志被无条件用作重试起点 → 增量起点被永久钉在旧账期。
   修复：仅当失败覆盖账期晚于最近一次成功覆盖范围（`failed.BillEnd.After(success.BillEnd)`）时才重试；判定用账期时间而非日志创建时间（重试失败日志创建于"现在"、账期却是历史值）。
2. e-cloudx-sdk tencent/volcano/huawei 计费适配器只拉 `StartTime` 所在单月，违反适配器契约。
   修复：新增 `billing.EnumerateMonths`，三个适配器按 `[StartTime, EndTime]` 逐月遍历（aliyun 既有实现即此语义）。

### 本轮改动

- e-cam-service（分支 `cam-iteration-20261027-multicloud-cost`）：
  - `internal/cam/cost/collector/service.go`：增量范围修复。
  - `internal/cam/cost/collector/collector_property_test.go`：新增 `stale_failed_log_ignored_after_later_success` 属性用例锁定修复（修复过程曾误用 CreateTime 比较，被既有属性用例 `failed_log_takes_priority_over_older_success` 拦下，改账期比较后全绿）。
  - `scripts/backfill_range_collect.go`：新增——指定 provider+账期范围复用生产 `CollectAccount` 补采（默认干跑，`-apply` 执行；自动初始化加密组件，失败即退出）。
  - `scripts/backfill_huawei_unified_bill.go`：上轮遗留的未完成 rebuildSummary 调用块改为指向独立汇总重建脚本（恢复可编译）。
  - `scripts/rebuild_cost_summary.go`：上轮产出，纳入本轮提交。
  - `docs/agent-iterations.md`：本记录。
- e-cloudx-sdk（同名分支）：
  - `billing/months.go`（新增）、`billing/{tencent,volcano,huawei}/adapter.go`（逐月遍历改造）。
- e-cam-web：本轮无改动（不制造空提交）。

### 验收

**A. 本地真实前后端 + 测试数据库（实盘只读云 API）**

- `go build ./...`（e-cloudx-sdk / e-cam-service 相关包）exit 0；`go vet` exit 0。
- `go test -count=1 ./internal/cam/cost/...`：collector 修复后全绿（含新增回归用例）。
- 修复生效实证（生产 cron，2026-10-27 00:04/06:04 两轮）：tencent `2026-10` 29,700 条 ¥152,332.83；volcano `2026-09` 72,202 条 ¥1,623,924.84 + `2026-10` 36,212 条 ¥734,700.77 入库（此前 13 天只重复拉回 3 月/2 月旧账期）。
- 历史补采（`backfill_range_collect.go -apply`）：tencent 2026-03~10、volcengine 2025-12~2026-10 —— 结果见"本轮结论"。

**B. 浏览器 UI 验证** —— 见"本轮结论"。

**C. 云侧边界声明**

- 本轮全部云调用为**账单只读查询**（与 6 小时定时采集同类），未做云资源写操作；未验证"真实云写行为"（本模块不存在该类行为）。
- 补采脚本对云厂商 API 的历史账期可用性按实际返回为准，失败会在采集日志与脚本输出如实记录。

### 风险与回滚

- 采集器为热部署生产组件：回滚 = revert 两个提交并等 CompileDaemon 重编译；数据层面按月先删后插幂等，重跑补采即可修复数据。
- 适配器逐月遍历在"起点被钉住"的场景会放大拉取量（一次性补齐后增量恢复正常）。

### 本轮结论

（待补：补采结果、UI 验证、PR 链接）

### 后续建议

1. `internal/account/repository/account.go toDomain`：解密失败静默回退密文是高危隐患（本轮补采首次运行即踩中：脚本未初始化加密组件 → 签名错误）。建议改为解密失败返回错误，或至少告警日志。
2. 采集日志 `bill_start/bill_end` 记录的是"请求范围"而非"实际返回账期范围"，排查历史缺口时可读性差，可考虑记录适配器实际覆盖的账期月清单。
3. e-cam-web 无 E2E 框架，成本页核心流程（筛选→分布→趋势）值得补 Playwright 冒烟（需先确认登录测试账号约定）。
4. 华为云 `bss:billDetail:view`、AWS Cost Explorer 授权仍是云侧权限缺口（运维动作，非代码）。

---

## cam-iteration-20261027-volcano-months

**状态：实现和验收完成，待审核**

### 需求来源

延续已确认需求「成本统计其他云也要实现」（`cam-iteration-20261027-multicloud-cost` 轮遗留）：火山引擎 2026-03~08 六个账期月缺失。

### 关键更正（对上轮结论）

上轮记录"火山账单明细 API 对 2026-03~08 不可用（云侧限制）"**判断有误**：本轮 11:03 用多数据面探针对同一账期月复测，`ListBillDetail` 对 2026-03 正常返回 32,610 条（与该月其他 API 及库内口径一致）。上轮 10:30~10:40 的连续失败实为**火山服务端瞬时过载窗口**——分段与单月粒度在 10 分钟内密集重试，全部撞进同一故障窗。
教训：对云侧"不可用"结论，必须隔开故障窗复测后再下（本轮探针含 2026-02 正常月做对照）。

### 本轮过程与结果

- 多数据面对照探针（只读）：`ListBillDetail` / `ListSplitBillDetail` / `ListAmortizedCostBillDetail`（需 AmortizedMonth 参数）/ `ListBill`（月度汇总，交叉验证）。确认 `ListBillDetail` 恢复后，**无需任何备选 API 或代码改动**，直接走生产补采路径。
- 逐月补采（`backfill_range_collect.go -apply`，月间 20s 间隔，全部一次成功）：

| 账期月 | 记录数 | 金额 (¥) |
|---|---|---|
| 2026-03 | 32,610 | 436,743.90 |
| 2026-04 | 33,594 | 715,668.28 |
| 2026-05 | 33,772 | 628,106.49 |
| 2026-06 | 34,130 | 657,125.38 |
| 2026-07 | 34,902 | 705,521.55 |
| 2026-08 | 35,801 | 765,334.27 |

- 火山账期覆盖达 **11/11 个月**（2025-12 ~ 2026-10），无代码变更。

### 验收

- **数据一致性**：缺口窗口（2026-03-01~2026-08-31）distribution API 返回 volcano ¥3,908,499.87（23.0%），与库内 6 个月合计**分毫不差**；aliyun 74.5% / tencent 2.5% 同窗合理。
- **浏览器 UI**（Playwright，localhost:8888 实链路）：未登录跳转→登录→成本页→缺口窗口火山出现（此前为零数据）→ 宽窗四云齐现 ✅；截图与 API 响应存证于 `docs/agent-iterations-evidence/cam-iteration-20261027-volcano-months/`。
- 云侧边界：同前轮，全部为账单只读查询，无云资源写操作。

### 踩坑记录（供后续会话复用）

- `ListAccounts` 返回的账号**不携带完整凭证**（密文态）；必须 `GetAccountWithCredentials(ctx, id)` 才拿到解密密钥（与采集器一致）。探针曾直接用 List 结果构造客户端 → `Invalid 'Authorization' header`，一度误判为密钥失效/依赖漂移——区分方法：对照正常账期月 + 换用凭证级接口。

### 后续建议

1. 火山适配器重试间隔过短（`retry.WithBackoff` 3 次共 ~3s），服务端过载窗内全灭；建议对历史账期拉长退避（如 5s/15s/45s）或在采集器层做跨轮次重试。
2. 账期缺口巡检：按 provider×月核对 unified_bill 覆盖（如每日 cron 发现缺失月告警/自动补采），避免每次靠人工比对发现。
3. `ListAmortizedCostBillDetail` 需要 `AmortizedMonth`（非 BillPeriod），如未来引入摊销口径注意参数差异。
