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

---

### 附：回归与生产健康度核查（2026-10-27 12:00~12:10，等待审核期间）

**回归（CI 态 / registry 依赖）**：origin/main 整仓 `go build` 失败，两处均属同一系统性根因——已提交代码依赖**未发布**的本地兄弟仓能力：
1. `ioc` Wire 生成：`syncerpkg.ISyncer` / `capability.IPermissionProvider`（e-iam 最新发布 tag 即 v0.0.27，类型只存在于本地未发布提交）；
2. `internal/cam/task/executor/sync_ddos.go`：`adapter.DDOS` / `types.DDOSInstance`（e-cloudx-sdk v0.1.0 无 DDOS，本地 main 已有）及 `logquery/service/federation.go` 的 `TopNNote`（同因）。
本仓 cost 域（含本 PR 全部路径）在 CI 态构建健康：`go build ./scripts/backfill_range_collect.go` exit 0、`go test ./internal/cam/cost/collector/...` ok。**系统性修复（发布 e-iam/e-cloudx-sdk 新版本或 CI 侧 workspace 对齐）需维护者决策，未擅动。**

**生产健康度（合并后首次 cron 触发核查）**：12:00:00 采集 cron 准时触发（app.log `ioc/jobs.go:40` + `starting scheduled collection {account_count: 5}`），volcengine 建立采集日志后，12:00:30 并行会话保存文件触发 CompileDaemon 12:00:49 重建重启，进程被硬杀：
- 残留一条 volcengine `running` 僵尸采集日志（无 record_count，仅影响采集页展示；增量计算按 success/failed 过滤，不受影响）；
- 账号锁 TTL 30 分钟，12:30 自愈，18:00 cron 无阻塞；
- 无数据损失：12:00~18:00 的增量将由 18:00 触发时按"最近成功账期"自动覆盖（增量修复的设计自愈路径）。
此前"12:00 未触发"的初判为误报（中文日志 grep 未指定 UTF-8 编码漏检），已更正。

---

## cam-iteration-20261027-collect-log（2026-10-27）

**状态**：**已合并**（e-cam-web#2 于 2026-10-27 15:04 +08 合并，merge commit `4691135`；本记录 PR 待审核）
**代码 PR**：`e-cam-web#2`（分支 `cam-iteration-20261027-collect-log`，commit `2bf5fb7` → `a06cb2f`，已合并为 `4691135`）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

2026-10-27 浏览器巡检「FinOps → 采集管理」，以真实接口与 DOM 对照发现（P2，影响采集故障排查与状态判断）：

1. **「采集时间」列恒为空**：接口返回 `start_time`/`ctime`，没有 `create_time`；前端读 `row.create_time`，20/20 行为 `-`。
2. **带 running 记录打开页面不轮询**：轮询只在手动触发后启动，页面状态永久停在「运行中」、耗时 `-`。
3. **轮询失败清空表格**：`catch` 内 `logs = []`。
4. 另发现（未在本轮修复）：`POST /cost/collect` 对当前管理员返回 403，前端按钮无权限提示；库内 running 日志堆积（14:00 查询 29 条）；账期字段语义混用（见后续建议）。

### 关键决策：账期列不做时区换算（评审回退）

巡检初期判断「6 月账期显示成 5-31 ~ 6-30」是时区 bug，曾在 `2bf5fb7` 中按本地时区重写。独立评审用真实模块三时区实测指出该改造会让结束日 +1 天（更糟），随后抽样真实库 200 条采集日志：

- `bill_start` 时间分量 `00:00:00Z`：140 条（UTC 日历日，定时/UI 路径）
- `16:00:00Z`：22 条（补采脚本，本地月边界）
- 其它（如 `10:06:19Z`）：38 条（定时采集增量窗口 `[上次成功, 现在]`）

→ 语义由写入方决定，前端无法用统一换算得到正确答案。`a06cb2f` **回退账期改造**，保持后端日期原样；口径统一列后端后续事项。

### 改动（最终 `a06cb2f`）

- e-cam-web：`src/views/cam/collect/index.vue`、`src/views/cam/collect/format.ts`（新增）、`src/views/cam/collect/format.test.ts`（新增 14 例）、`src/api/types/finops.ts`。
  - 采集时间：`start_time` 优先、`ctime` 兜底；
  - 运行中耗时「已运行 X分YY秒」，结束沿用 `duration_ms`；
  - 打开页面有 running 即轮询；触发后 3 分钟宽限期；
  - 递归 `setTimeout` + 单飞 + 失败指数退避（5s→60s）；卸载 `disposed` 短路并清理定时器；
  - 静默刷新不整表 loading；失败保留数据 + 行内提示 + 立即重试；请求序号防竞态；
  - `CollectLog` 类型对齐真实字段（移除臆造的 `create_time`）。
- e-cam-service：无代码改动，仅本记录。

### API 契约（后端零改动）

`start_time`（RFC3339 UTC）、`ctime`（ms，零值时回退）、`bill_start`/`bill_end`（原样展示）、`duration_ms`。

### 验收（真实浏览器）

环境：隔离 worktree + Vite `:5175`（base `/cam/`）+ 真实 `e-cam-service:8001` + 测试库；Chromium 141 headless；1440×900 / 390×844；`:8888` 统一登录（既有测试账号，未记录口令）。

- A. 真实接口：采集时间 `2026-10-27 12:00:00`（20/20 行非空）；账期原样 `2026-08-31 ~ 2026-10-27`；running 行 `已运行 3分07秒`→`3分22秒`。
- B. 真实轮询：17s 内 4 次日志请求，间隔 5029/5048/5036ms（最终提交）。
- C. 注入时序（仅验证异步行为）：12/12 通过——自动轮询、运行时长增长、完成→成功（266.2s）→停轮询、失败保留数据与重试恢复；正常路径 0 console error，失败注入 2 条预期错误。
- D. 真实任务完成翻转：**最终提交未观测**（库内 running 日志堆积：14:00 `status=running` 查询 29 条，含 09:43 的 `id=1940`、12:00 的 `id=1959`；UI 触发被 403 拒绝）。上一版提交 `2bf5fb7` 曾观测到 11:30 启动任务在页面自动变为「成功 / 330.1s」（轮询实现已在 `a06cb2f` 重写）。
- E. 窄屏 390×844：页面无整页溢出；表格滚动容器 `.el-scrollbar__wrap` 可横向滚动（scrollLeft 0→1028，末列可见）；但侧栏默认展开时内容区仅 122–170px，需横向滚动（既有壳层问题）。
- 未执行：其它租户/无权限视角（403 现象已另行记录）、Firefox/Safari、移动真机。

### 测试

- `vitest run`：52 files / 927 tests 通过（`format.test.ts` 14 例，固定时区字面量断言）。
- `vue-tsc -b` exit 0；`eslint` exit 0；`vite build` exit 0。
- 首次全量运行有 1 例既有用例 5s 超时（assets raw-import 样式断言），单跑通过、复跑全量通过 → 负载抖动，与本次无关。

### 子 Agent 与复核

- 独立审查 Agent（read-only）：request changes（账期时区化 blocker、卸载后 timer 重建、测试自证）。blocker 以「回退 + 真实取样」解决，timer 已修复，测试改为固定时区断言；其确认类型与 Go 结构体一一对应、无其它代码依赖被删字段、无密钥/云写操作。
- 独立 QA Agent（验证对象 `2bf5fb7`，未复跑回退后的提交）：A/C/D/H 真实 PASS；E/F 仅注入 PASS；其「390 表格列不可达」结论经总控复核否定（测的是 `overflow:hidden` 外层，真实滚动容器为 `.el-scrollbar__wrap`）。

### 版本组合

- 前端：`a06cb2f`（本 PR）。
- 后端：验证期间为 `main@77a13cc` → `main@77f53f5`（并发会话合并 PR #8/#10 后热编译）；`git diff 77a13cc HEAD -- internal/cam/cost/{domain/collect.go,handler/collector_handler.go}` 为空，契约一致。
- 建议合并顺序：e-cam-web#2（无后端依赖）。

### 风险与回滚

纯前端；轮询仅在 running 或触发后 3 分钟内运行，失败退避至 60s，卸载清理定时器。回滚：revert `a06cb2f`、`2bf5fb7`。
环境风险：验证期间后端被并发会话重启、远程库一次连接超时、12:00 资产同步高峰导致部分请求 500；均与本改动无关。

### 后续建议（按优先级）

1. **手动触发采集 403（P2，待产品确认）**：`POST /cost/collect` 被 `psdk.CheckPolicy()` 拒绝，前端按钮仍可点击且无权限说明；需确认权限码与可见性策略后再改。
2. **采集任务超时/心跳（P1，后端）**：`status=running` 查询 29 条（14:00 核实），含 09:43 的 `id=1940`、12:00 的 `id=1959`；12:00 一条已由并发会话记录归因为 CompileDaemon 重建重启，其余需启动对账 + 超时回收，前端只能持续轮询。
3. **账期字段口径统一（P2，后端/脚本）**：UTC 日历日、本地月边界、增量窗口三种语义并存；建议记录 `bill_start_date/bill_end_date`（日历日字符串）或统一写入口径。
4. **移动端壳层（P2）**：390px 固定侧栏 + 固定列宽，需横向滚动；建议侧栏折叠/抽屉。
5. 「账号 ID」改为账号名（需确认已删除账号回退规则）。
6. 本文件上一轮「本轮结论」在 main 上仍为「待补」，建议由对应 PR 补档（不属本轮范围）。

---

## cam-iteration-20261027-collect-log-filters（2026-10-27）

**状态**：实现和验收完成，待审核
**代码 PR**：`e-cam-web#3`（分支 `cam-iteration-20261027-collect-log-filters`，commits `fb62415` → `2f32f4c` → `f7a770d`）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

2026-10-27 巡检「FinOps → 采集管理」：日志已 **1959 条**，页面固定 20 条/页且无任何筛选；账号列只有数字 ID，运维无法按账号/状态定位失败记录。

真实分布（API 实测）：火山 417、阿里 388、AWS 385、腾讯 388、华为 385；`status=running` 29、`status=failed` 806。后端 `GET /cost/collect/logs` 已支持 `account_id`/`status`，前端未使用。优先级 P2。

### 目标与非目标

- 目标：账号/状态筛选；账号显示名称+ID；总条数；空结果区分；筛选回第 1 页；筛选与轮询共存。
- 非目标：后端改动、任务调度/状态、日期区间筛选（后端不支持日志时间过滤）、URL 状态持久化。

### 改动

- e-cam-web：`src/api/finops.ts`（`CollectLogQuery`/`CollectLogStatus`/`status` 参数）、`src/views/cam/collect/index.vue`、`src/views/cam/collect/filters.ts`（新增）、`src/views/cam/collect/filters.test.ts`（新增 11 例）。
- e-cam-service：无代码改动，仅本记录。

### API 契约（后端零改动）

`GET /api/v1/cam/cost/collect/logs?account_id=&status=&offset=&limit=`；`status` 取 `running|success|failed`；空值不下发；响应 `data.items[]` + `data.total`。

### 验收（真实浏览器 + 真实后端）

环境：隔离 worktree + Vite `:5178`（base `/cam/`）+ 真实 `e-cam-service:8001` + 测试库；Chromium 141 headless；1440×900 / 390×844；`:8888` 统一登录（既有测试账号，未记录口令）。

| # | 验收 | 结果 |
|---|---|---|
| 1 | 表头「账号」，不再是「账号 ID」 | ✅ |
| 2 | 总条数 + 账号名 | ✅ `共 1959 条`，`集团-火山云 #5` |
| 3 | 账号名与 ID 同时可见 | ✅ |
| 4 | 账号筛选 | ✅ 火山云 417，请求含 `account_id=5` |
| 5 | 账号+状态组合 | ✅ 火山云+失败 48，全为失败 |
| 6 | 空结果区分 | ✅ AWS+成功 0，空态「没有匹配的采集记录」+ 清空 |
| 7 | 清空筛选恢复 | ✅ 1959 |
| 8 | 筛选回第 1 页 | ✅ 第 2 页 → 选运行中 → 第 1 页、29 条 |
| 9 | 运行中筛选下轮询 | ✅ 请求均含 `status=running` |
| 10 | 刷新保留筛选 | ✅ |
| 11 | 窄屏 390 | ⚠️ 无整页溢出、筛选可操作；壳层内容区仅 122–170px（既有问题） |
| 12 | 正常路径 console | ✅ 被测页 0 error |
| 13 | 筛选「失败」视图仍持续轮询 | ✅ 12s 内 2 次失败视图请求 + 2 次 `status=running&limit=1` 探针 |
| 14 | 探针 total=0 时停止轮询 | ✅ 停止后 12s 内 0 请求 |
| 15 | 账号名 title | ✅ `title="集团-火山云"` |

### 测试

- `vitest run`：53 files / 938 tests 通过（`filters.test.ts` 11 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全绿（以最终 commit 为准）。

### 子 Agent 与复核

- 独立审查（read-only）：首轮 **request changes**，指出筛选视图下轮询误停（major）。修复后复核 **approve with nits**，确认 major 已解决、世代 token 无泄漏；剩余 nit 已处理（探针竞态守卫、探针带账号、BOM/空行清理），未处理项为已知限制（>100 账号、僵尸 running 长期轮询）。
- 独立 QA（自写脚本）：首轮 A–J 29/29 PASS；最终提交复跑 27/27 回归 + 新增 N1（失败视图仍轮询）/N2（无任务即停）/N3（title）全部 PASS；被测页 console 0 error。

### 版本组合

- 前端：`f7a770d`（本 PR）。
- 后端：`main@f09e290`（采集日志接口无 diff）。
- 建议合并顺序：`e-cam-web#3`（无后端依赖）。

### 风险与回滚

纯前端；筛选不写 URL；非 running 筛选下每次拉取伴随 1 次探针（请求约翻倍，当前量级可接受）；已知限制：账号下拉最多 100 个、僵尸 running 会使轮询持续。回滚：revert 三个提交。

### 后续建议（按优先级）

1. **P1 采集任务 running 堆积/无超时回收**：`status=running` 29 条，前端只能持续轮询。
2. **P1 自动同步任务积压/重复入队**（本轮新发现）：`/cam/tasks` 总数 6996，最近 100 条中 43 pending + 5 running；最老 pending 6 小时；同账号出现 2 个 running；调度器去重仅内存态，进程重启后失效。
3. **P2 手动触发采集 403**：需确认权限码与前端可见性。
4. **P2 账期字段口径统一**。
5. **P2 移动端壳层**（390px 固定侧栏 + 固定列宽）。
6. **P3 采集日志筛选写入 URL**；账号下拉改为按 total 分页加载。

---

## cam-iteration-20261027-log-type-url（日志页 t=app 链接还原修复）

**需求来源**：排查日志查询 400（同期 PR #11）时发现的 encode/decode 不对称——`encodeUrlState` 在应用业务日志页签写 `t=app`，`decodeUrlState` 白名单漏 app，分享/刷新链接静默落 CDN 页签；根因 `LogType` 联合类型漏列 app（后端 /types 已有 5 类）。

**改动**（仅 e-cam-web，配套 PR **Havens-blog/e-cam-web#4**，分支 `cam-iteration-20261027-log-type-url`）：decode 白名单收敛为 LOG_TYPES 集合（与 /types 字典一致）；LogType 联合类型补 app。无后端改动。

**验收**：url-state 单测 9/9（含 t=app 往返 + 全类型往返表）、logs 视图全套 127/127、vue-tsc 通过；浏览器真实链路验收 PASS——本地 vite 实例 + 真实登录会话（eiam Cookie，localhost 域端口无关自动携带）+ 真实后端，`/cam/logs?t=app` 正确激活「应用业务日志」页签并按 `log_type:app` 查询（截图 url-app-after-5174.png；对照旧构建 8888 同 URL 页签未渲染）。证据留存本地 `D:\Haven\logs\dev\`。

**遗留**：运行中 8888 部署实例待合并重建生效；CI 系统性失败（未发布 SDK）仍待维护者处理。

---

## cam-iteration-20261027-contract-alignment（契约对齐小修打包）

**需求来源**：轮次 2 委派子 agent 的只读契约审计（6 契约面/9 差异）。本切片取其中可独立交付项：① `/types` 字典加载晚于 URL 水合,3d 类型（slb/access/app）分享长窗链接打开后按 7d 兜底超窗查询;② limit 注释「硬顶 500」与后端 MaxPerSourceLimit=2000 漂移;③ 搜索/聚合响应的 `cached/cache_stale/topn_note/not_supported` 字段前端类型未声明。

**改动**（仅 e-cam-web,配套 PR **Havens-blog/e-cam-web#6**,分支 `cam-iteration-20261027-contract-alignment`）：新增 `clampWindow` 纯函数（end 钳 now/start 收到 end-cap/倒挂回退 1h）,hydrate 与 onMounted 字典到位后复钳共用;limit 解码放宽为正整数透传（白名单仍在 useLogUrlSync）;注释 500→2000;响应类型补全（仅类型层）。无后端改动。

**验收**：logs 全套单测 150/150（新增 7 例）、vue-tsc 通过;浏览器真实链路（真实登录+真实后端:8001）——slb 分享 5d 链接打开 picker 收紧到 now-3d、查询载荷 start_time=now-3d（PASS）,cdn 对照 5d 保留（PASS）。证据：`D:\Haven\logs\dev\clamp-*.png`。

**环境边界（如实记录）**：验证中并行会话 18:31 重建的后端开始对非 8888 来源的 `/api/v1/cam/logs/*` 一律 403（旧构建 8888 对照无 4xx）——客户端断言在该现象出现前已通过,数据渲染 E2E 属环境阻断而非本轮引入,已写入 PR 边界说明。

**遗留（候选,不擅自实现）**：app 专属 KPI（需确认口径）;聚合/诊断补 account_ids（需账号级筛选 UI）。

---

## cam-iteration-20261027-task-filters（2026-10-27）

**状态**：**已合并**（`e-cam-web#5`，merge commit `6812aa1`，2026-10-27 19:00 +08）
**代码 PR**：`e-cam-web#5`（commits `5bda04a` → `0ab25a2` → `a3940da`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

2026-10-27 巡检「资源 → 任务管理」：

1. **任务类型筛选失效**：前端选项值 `sync_assets` / `discover_assets`，后端真实类型是 `cam:sync_assets` 等。实测 `GET /tasks?type=sync_assets` → **total=0**，`?type=cam:sync_assets` → **6534**。
2. **类型显示为原始字符串**：任务卡/详情页标签映射同样用无前缀键，页面直接显示 `cam:sync_assets`。
3. **队列积压不可见**：任务总量 7000，其中待执行 67、执行中 3、已完成 6926、失败 4；页面只有分页总数，无按状态概览。

另：QA 复核发现 4 条 failed 任务均为同一后端错误 `tenant_id: cannot decode 64-bit integer into a string type`（见后续建议）。

### 需求卡

- 目标用户：运维/云管管理员，关注资产同步任务积压与失败。
- 目标：类型筛选可用、类型标签中文化、队列概览（全部/待执行/执行中/已完成/失败）可点击筛选、积压提示、积压时自动刷新。
- 非目标：后端改动、任务调度/重试/超时回收（P1 另立项）、任务详情页重构。
- API 契约：复用 `GET /api/v1/cam/tasks?type=&status=&offset=&limit=`；`type ∈ cam:sync_assets|cam:discover_assets|cam:sync_billing|cam:create_ecs`，`status ∈ pending|running|completed|failed|cancelled`；响应 `data.tasks[]` + `data.total`。**后端零改动。**
- 风险/回滚：纯前端；概览额外 5 次 `limit=1` 统计（加载/手动刷新/类型变更/每 30s 补刷）；回滚 `git revert a3940da 0ab25a2 5bda04a`。

### 改动

- `src/api/types/task.ts`：`TaskType` 对齐后端真实值（4 种，含 `cam:create_ecs`），`Task` 增加 `error` 字段。
- `src/views/tasks/taskFilters.ts`（新增）：类型/状态选项、概览状态、查询构造、标签、积压判定。
- `src/views/tasks/taskFilters.test.ts`（新增，11 例）。
- `src/views/tasks/index.vue`：
  - 类型/状态下拉使用真实值；
  - 队列概览条（5 档计数，点击筛选；参与类型筛选口径）；
  - 积压提示「待执行 X · 执行中 Y」；
  - 自动刷新：有积压 5s 刷列表（每 30s 补概览），空闲 30s 低频；卸载/竞态守卫。
- `src/views/tasks/components/{TaskCard,TaskStatusBadge}.vue`、`detail.vue`：类型标签/图标覆盖 4 种类型；状态文案统一「待执行/执行中」；失败任务错误信息改读后端真实字段 `error`。

### 验收（真实浏览器 + 真实后端）

环境：隔离 worktree + Vite `:5179`（base `/cam/`）+ 真实 `e-cam-service:8001` + 测试库；Chromium 141 headless；1440×900 / 390×844；`:8888` 统一登录（既有测试账号，未记录口令）。

| # | 验收 | 结果 |
|---|---|---|
| 1 | 概览五档计数与接口一致 | ✅ 7002 / 64 / 5 / 6929 / 4 |
| 2 | 积压提示 | ✅ `队列积压：待执行 64 · 执行中 5` |
| 3 | 类型筛选真实类型 | ✅ 同步资产 6536 = API，卡片标签均「同步资产」 |
| 3b | 类型筛选后概览收敛 | ✅ chip「全部 6536」= 列表 total = API |
| 4 | 无原始类型字符串 | ✅ |
| 5 | chip 点击 + 组合筛选 | ✅ 待执行 63 = API，chip 高亮 |
| 6 | 清空筛选 | ✅ 7002 |
| 7 | 积压自动刷新 | ✅ 12s 内 2 次，间隔 5028ms，无整页 loading |
| 8 | 窄屏 390 | ⚠️ 无整页溢出、概览可见；壳层内容区仍 170px（既有问题） |
| 9 | 正常路径 console | ✅ 0 error / 0 warning |
| 10 | 失败任务错误信息 | ✅ 4 条 failed 全部展示后端 `error` |

### 测试

- `vitest run`：54 files / 951 tests 通过（`taskFilters.test.ts` 11 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查（read-only）：首轮 **request changes**（首屏空态、概览与列表口径矛盾 2 个 major），修复后复核 **approve with nits**，确认 major 已实质修复；剩余 nit 已在 `a3940da` 收敛。
- 独立 QA（自写脚本、真实接口零 mock）：对 `0ab25a2` 与 `a3940da` 各完整复跑，**18/18 PASS**（概览计数、积压提示、五 chip 逐个点击、类型筛选 6536、chip 收敛、组合、清空、5s 轮询、390、console 0、按钮存在性、失败错误信息透出）；零积压空闲分支因无法构造（禁止改数据）未执行。

### 版本组合

- 前端：`a3940da`（合并为 `6812aa1`）。
- 后端：`main@f06a20c`（任务接口无 diff）。
- 合并顺序：`e-cam-web#5` 已合；本记录 PR 随后。

### 后续建议（按优先级）

1. **P1 自动同步任务积压/重复入队**：7000+ 任务、60+ 待执行；调度器去重仅内存态、进程重启后失效；需启动对账 + 持久化去重 + 超时回收。
2. **P1 任务执行失败根因**（QA 新证据）：4 条 failed 均为 `获取云账号失败: error decoding key tenant_id: cannot decode 64-bit integer into a string type` —— 账号解码类型问题，需后端修复（与采集/同步链路相关）。
3. **P2 采集任务 running 29 条无超时回收**；**P2 触发采集 403 权限码确认**；**P2 账期口径统一**；**P2 移动端壳层**。
4. **P3**：分页 `Total` 英文（全站未配 Element Plus zh-cn locale）；概览无法覆盖 disk:/nas:/oss:/rds: 指标任务（跨模块队列）。
