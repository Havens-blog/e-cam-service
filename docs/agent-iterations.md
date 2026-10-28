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

---

## cam-iteration-20261027-app-drilldown（app TopN 下钻明细恒空修复）

**需求来源**：契约审计面 D 两条 —— app 无聚合 TopN,采样回退图按 `entryHost`(回退 resource_id)分组名实不符;点击下钻生成 `host eq` 筛选,app 无 host 字段 → **明细静默恒空**。

**改动**：前端(e-cam-web#8,分支 `cam-iteration-20261027-app-drilldown`)—— 新增 `entryService`/`buildTopServices`,app 采样第三图按服务分组(图题「服务 Top」),`defaultTopnField(app)='service'`;LogEntry 联合补 AppLogEntry 形态;entryHost/entryStatus 窄化守卫。配套 e-cloudx-sdk#5 —— app 过滤语义单测 12 例(锁 service eq 命中,此前零覆盖)。无后端服务改动。

**验收**：logs 全套 138/138、vue-tsc 通过;SDK go test 全绿 + vet 干净;浏览器真实链路(白名单端口 3333 + 真实登录/后端)—— app 页签激活、search 200、第三图标题「服务 Top」、0 控制台错误。**数据边界**：应用业务日志源当前无存量数据(1h/3d 均 0 条),图与下钻点击无法用真实数据演示,由单测链保证语义。截图 `D:\Haven\logs\dev\app-*.png`。

**环境勘误（重要,修正上轮记录）**：上轮报告的"非 8888 来源 403"实为后端 CORS 白名单**设计内机制**(`corsDefaultOrigins`=8888/5173/3333,ioc/gin.go corsHdl)——验证端口 5174 在白名单外;改用 3333 后一切正常。轮次 1 的 5174 验收当时只断言了请求侧未断言响应状态,存在盲区,特此记录。

**遗留候选**：① SDK AppLogEntry 缺 `case "exception"`(app 异常堆栈筛选恒空,下一切片首选);② app 专属 KPI(待业务口径)。

---

## cam-iteration-20261027-budget-progress（2026-10-27）

**状态**：**已合并**（`e-cam-web#7`，merge commit `49af22a`，2026-10-27 19:52 +08）
**代码 PR**：`e-cam-web#7`（commits `d5ab286` → `7a90904`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

2026-10-27 巡检「FinOps → 预算管理」：

- 预算「成本控制」上限 **¥100,000**，后端进度接口返回 **已消耗 ¥3,169,088.99（3169.09%）、剩余 ¥0**；但列表完全不展示消耗，运维无法一眼发现严重超支。
- 点击行打开「预算消耗进度」抽屉直接报错：`TypeError: Cannot read properties of undefined (reading 'toFixed')`（Vue render error）。根因：前端 `BudgetProgress` 按 `progress_percent` / `remaining` 读取，后端实际返回 `usage_percent` / `remaining_amount`。

### 需求卡

- 目标用户：运维/财务管理员，需要判断预算是否超支。
- 目标：列表直接展示消耗金额/真实百分比/超支标识；「进度」按钮与行点击均可打开抽屉；抽屉展示预算上限、已消耗、真实百分比、剩余额度、超支金额；异常/旧版载荷不崩溃。
- 非目标：预算计算规则、告警触发、后端改动、创建/编辑表单。
- API 契约：复用 `GET /api/v1/cam/budget`、`GET /api/v1/cam/budget/{id}/progress`；字段 `amount_limit / current_spend / usage_percent / remaining_amount`。**后端零改动。**
- 风险/回滚：纯前端；每个预算 1 次进度请求（批量并发 4）；回滚 `git revert 7a90904 d5ab286`。

### 改动

- `src/api/types/finops.ts`：`BudgetProgress` 对齐真实字段（`usage_percent`、`remaining_amount`）。
- `src/views/cam/budget/progress.ts`（新增）：`clampPercent`、`isOverBudget(>100)`、`isAtLimit(=100)`、`formatUsagePercent`、`overBudgetAmount`、`progressColor`、`currentSpend/amountLimit/remainingAmount`、`normalizeProgress` 形状校验。
- `src/views/cam/budget/progress.test.ts`（新增，10 例，含 legacy payload / NaN / Infinity / -0）。
- `src/views/cam/budget/index.vue`：
  - 列表新增「消耗进度」列（进度条 + 已消耗 + 真实百分比 + 超支/已达上限标签）；
  - 操作列新增「进度」按钮，保留行点击；
  - 抽屉展示上限/已消耗/剩余/超支金额，进度条 clamp 100、文字真实值；
  - 批量进度 allSettled + 并发 4 + 请求令牌；失败行「获取失败，点击重试」；
  - 抽屉 stale-while-revalidate，编辑/删除/新建后 `await fetchAll()`。

### 验收（真实浏览器 + 真实后端）

环境：隔离 worktree + Vite `:5180` + 真实 `e-cam-service:8001`；Chromium 141 headless；1440×900 / 390×844；`:8888` 统一登录（既有测试账号，未记录口令）。

| # | 验收 | 结果 |
|---|---|---|
| 1 | 列表展示已消耗 | ✅ ¥3169089.00 = 接口 |
| 2 | 真实百分比 | ✅ 3169.1% |
| 3 | 超支标识 | ✅ 「超支」 |
| 4-7 | 抽屉上限/已消耗/百分比/剩余 | ✅ ¥100000.00 / ¥3169089.00 / 3169.1% / ¥0.00 |
| 8 | 超支金额 | ✅ ¥3069089.00 |
| 9 | 进度条文字真实值 | ✅ 3169.1%（条宽 100%） |
| 10 | 刷新后保持 | ✅ |
| 11 | 390 无整页溢出 | ⚠️ 字面 PASS；内容区仅 122px（壳层既有 O1） |
| 12 | 正常 console | ✅ 0 error（修复前 undefined.toFixed 崩溃） |
| 13 | 旧版/异常载荷 | ✅ 降级「获取失败，点击重试」，不崩、可恢复 |
| 14 | 失败行点击重试回填 | ✅ 抽屉与列表均回填 |

独立 QA 另验：恰好 100% →「已达上限」、未超支 42.5% 路径、消耗 0 不误判失败；**15/15 + CONTRACT 7/7 + 边界 7/7 PASS**。

### 测试

- `vitest run`：55 files / 968 tests 通过（`progress.test.ts` 10 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：首轮 **request changes**（编辑后进度缓存失效、异常载荷仍可能崩溃 2 major），修复后复核 **approve with nits**，两个 major 均已实质修复。
- 独立 QA：先发现「移动靶」（工作区并行修改），定稿后以 `7a90904` 为唯一快照复测（运行前后文件 SHA256 一致），15/15 PASS；legacy payload、缺字段、500 注入、失败重试回填均 PASS。
- 未验证：编辑/新建/删除后的实际刷新（需改数据，按边界未执行）；窄屏可用性（O1）。

### 版本组合

- 前端：`7a90904`（合并为 `49af22a`）。
- 后端：`main@cb3d692`（预算进度接口无 diff）。
- 合并顺序：`e-cam-web#7` 已合；本记录 PR 随后。

### 后续建议

1. **P2 遮罩观感**：当前 `loading || progressLoadingAll` 会让表格在进度分批返回期间整体遮罩（预算多时约 2s 不可交互），建议只遮列表、进度用列内占位。
2. **P3 浮点边界**：`usage_percent` 仅比 100 多 0.0033 时会显示「超支 ¥0.00」；建议按 `overBudgetAmount >= 0.005` 判定。
3. **P3 异常字段**：`normalizeProgress` 对 `remaining_amount` 缺失仍回退 0，后端若只改此字段名会显示 ¥0.00 而非失败，建议一并强校验。
4. **P2 移动端壳层**（O1，跨页面）：390px 侧栏固定 220px、内容区 122px，预算表需横滚 1032px；应在 `src/layouts`/`PageContainer` 加断点，非单页问题。
5. **既有 P1 待办**：自动同步任务积压/持久化去重、采集 running 无超时回收、触发采集 403、账期口径。

---

## cam-iteration-20261027-app-exception-filter（app 异常堆栈筛选恒空修复,审计收官）

**需求来源**：契约审计面 D 最后一个 [高] 项 —— `/types` 字典为 app 注册 exception 字段,但 SDK `entryFieldValue` 无该 case,app 按「异常堆栈」筛选时 `EntryMatches` 每条返回 false,**明细静默恒空**。

**改动**：仅 e-cloudx-sdk(e-cloudx-sdk#6,分支 `cam-iteration-20261027-app-exception-filter`)—— AppLogEntry 分支补 `case "exception"`,filter_test.go 补 eq/contains/不命中三态并修正注释。无服务/前端改动。

**验收**：`go test ./logquery/` 全绿(exception 三态 + 既有 12 例回归)、`go vet` 干净、gofmt 通过。**边界(如实)**：生效需 SDK 发版 + 服务 go.mod 升级(系统性发布门控);app 源无存量数据,端到端演示不可行,语义由单测锁定。

**审计收官状态**：面 A(url-state/LogType)→ e-cam-web#4;面 D(exception/下钻)→ SDK#6 + e-cam-web#8;面 E/F 小修 → e-cam-web#6。审计 9 项差异中 8 项已落地,余 2 项候选(app 专属 KPI 待业务口径;聚合/诊断 account_ids 需账号筛选 UI)。

---

## cam-iteration-20261027-cost-comparison（2026-10-27）

**状态**：**已合并**（`e-cam-web#9`，merge commit `858523d`，2026-10-27 20:46 +08）
**代码 PR**：`e-cam-web#9`（commit `b5b8e07`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

2026-10-27 巡检「FinOps → 成本概览」同比卡片：

- 区间 `2026-08-01 ~ 2026-10-27`：接口 `/cost/comparison` 返回 `current_amount=10,529,255.90`、`previous_amount=0`、`change_percent=0`（2025 年同期无账单）。
- 页面渲染为「去年同期 ¥0.00 ｜ 0.0%」，看起来像「同比持平」，与事实不符，属数据可信度问题。
- 后端 `analysis/service.go` 的判定同样是 `if prevAmount > 0` 才计算百分比，前端规则与后端同构；有基线时的 `0.0%` 是真正的持平，应保留。

### 需求卡

- 目标用户：运维/财务，用同比判断成本走势。
- 目标：去年同期无基线显示「—」与「去年同期无数据或消耗为 0，无法计算同比」，不出现 `¥0.00 / 0.0%`；有基线保持 `+x.x% / -x.x%` 与涨跌配色。
- 非目标：后端计算口径、环比、其它卡片。
- API 契约：复用 `GET /api/v1/cam/cost/comparison?start_date=&end_date=`；`current_amount / previous_amount / change_percent / current_period / previous_period`。**后端零改动。**
- 规则：`previous_amount > 0` 为有基线；`== 0` 为无基线（无账单或去年消耗为 0，文案已涵盖）。
- 风险/回滚：纯前端；单提交回滚。

### 改动

- `src/views/cam/cost/comparison.ts`（新增）：`hasComparisonBaseline`、`formatPreviousAmount`、`formatChangePercent`、`comparisonHint`。
- `src/views/cam/cost/comparison.test.ts`（新增，7 例）。
- `src/views/cam/cost/index.vue`：去年同期金额与百分比改走纯函数；无基线显示提示，有基线保留原配色与符号。

### 验收（真实浏览器 + 真实后端）

环境：隔离 worktree + Vite `:5180` + 真实 `e-cam-service:8001`；Chromium 141 headless；1440×900 / 390×844；`:8888` 统一登录（既有测试账号，未记录口令）。

| # | 验收 | 结果 |
|---|---|---|
| 1 | 无基线不显示 ¥0.00 | ✅ 显示「—」 |
| 2 | 无基线不显示 0.0% | ✅ |
| 3 | 无基线提示 | ✅ 「去年同期无数据或消耗为 0，无法计算同比」 |
| 4 | 当期金额 | ✅ ¥10529255.90 = 接口 |
| 5 | 390 无整页溢出 | ⚠️ overflow=0；卡片仅 122px、金额被裁切（壳层既有 O1） |
| 6 | console | ✅ 0 error |
| 7 | CONTRACT 有基线路径 | ✅ 伪造 `+12.34` → `+12.3%`（increase）、`-5.6` → `-5.6%`（decrease）、`0` → `0.0%`（中性）；恢复真实接口回无基线 |
| 8 | 切换日期/厂商 | ✅ 无报错、数值随筛选变化 |

独立 QA：**14/14 PASS**（两次运行一致）。

### 测试

- `vitest run`：56 files / 975 tests 通过（`comparison.test.ts` 7 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：**approve with nits**（无 blocker/major）；交叉验证后端 `prevAmount > 0` 判定与前端一致，`0.0%` 仅在真持平时出现。
- 独立 QA：14/14 PASS，含 CONTRACT 伪造增/减/持平与恢复；确认 null 边界降级正常、颜色语义一致。
- 证据修正：作者 `00-before.png / 01-after.png` 因滚动容器是 `.page-container`、`fullPage` 只截首屏而字节相同，**截图无区分力**；有效视觉证据以 QA 的 `S1–S7` 卡片截图为准（`D:\Haven\.browser-verify\qa-comparison\`）。

### 版本组合

- 前端：`b5b8e07`（合并为 `858523d`）。
- 后端：`main@9c8734f`（comparison 接口无 diff）。

### 后续建议

1. **P3 极小基线**：`0 < previous_amount < 0.005` 时仍会显示 `¥0.00`（四舍五入），可把基线阈值改为 `>= 0.005` 或说明理由。
2. **P3 负基线**：抵扣/退款可能使 `previous_amount < 0`，当前显示「—」并断言「无数据或为 0」；建议展示带符号金额并调整文案。
3. **P3 符号舍入**：`+0.04` 会显示 `+0.0%`，建议先舍入再判符号。
4. **P2 移动端壳层**（跨页面）：390px 下同比卡片金额被裁切（内容区 122px），需在 `src/layouts`/`PageContainer` 加断点。
5. **既有 P1 待办**：任务积压/持久化去重、采集 running 超时回收、触发采集 403。

---

## cam-iteration-20261027-reject-logging-verify（已合并 #11 的部署验证,400 归因闭环）

**需求来源**：轮次 1 审计的 400 归因可观测性悬案 —— e-cam-service#11(400 拒因结构化日志)已合并,但并行开发实例长期未拉取;本日 21:22 实例重建(HEAD 27b9c4f,含 f09e290 祖先)后可验证。

**验证(A 级真实链路)**：真实登录会话经 8888 → 后端 :8001 发构造请求 `log_type=nope` → **HTTP 400** `invalid log type: nope`;`logs/app.log` 21:39:01/21:40:29 两条 WARN:`{"stage":"validate","path":"/api/v1/cam/logs/search","tenant_id":3,"client_ip":"127.0.0.1","reason":"invalid log type: nope"}`(web/handler.go:478)—— 五字段与 #11 设计一致,**闭环**。

**边界发现(如实,不改行为)**：`log_type=cdn + 30d 窗口` → **200**(total=417)——服务端**不强制** max_window_days,窗口钳制是前端 clampWindow(url-state.ts)单侧契约。超窗大查询当前可达聚合/检索层。如需服务端强制(防绕过),属新需求待确认。

**本轮无代码改动**(验证轮)。

---

## cam-iteration-20261027-responsive-shell（2026-10-27）

**状态**：**已合并**（`e-cam-web#10`，merge commit `4f2e871`，2026-10-27 21:56 +08）
**代码 PR**：`e-cam-web#10`（commits `dd1c374` → `0fbbe55`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 连续两轮验收：390×844 下侧边栏固定展开 **220px**，内容区仅 **122px**；预算表格仅约 88px、消耗进度需横向滚动 1032px；成本同比金额 `¥10529255.90` 被卡片右边裁切（QA O1）。
- 侧边栏已有折叠态（64px）与折叠按钮，但 `isCollapsed` 初始 false 且只由点击切换，窄屏从不自动折叠。

### 需求卡

- 目标用户：手机/窄窗口下查看云管数据的运维。
- 目标：≤768px 自动折叠侧栏为 64px 图标态、主内容边距 24→12px；769~1024 保持展开（保证二级菜单可达）；宽屏不变；断点内保留用户手动折叠/展开；首帧即按视口渲染。
- 非目标：移动端抽屉导航、一级 flyout、顶栏重排、表格卡片化。
- 规则：`width ≤ 768` 为窄屏；跨断点自动切换，断点内手动优先。纯展示，无权限/资源语义。
- API 契约：无（前端外壳）。
- 风险/回滚：全站共用外壳；布局测试 26 例 + 全量 986 例回归；回滚单提交。

### 改动

- `src/layouts/responsive.ts`（新增）：`NARROW_BREAKPOINT=768`、`isNarrowViewport(width)`。
- `src/layouts/responsive.test.ts`（新增，4 例）。
- `src/layouts/MainLayout.vue`：
  - setup 内按 `window.innerWidth` 初始化 `isNarrow`/`isCollapsed`（首帧即折叠，无 220px 闪烁）；
  - `applyViewport` + `resize` 监听，跨断点自动折叠/展开，断点内不覆盖手动选择；
  - `@media (max-width: 768px)` 主内容 padding 12px。
- `src/layouts/__tests__/MainLayout.test.ts`：新增 3 例集成测试（600 挂载折叠、跨断点恢复、手动保留、卸载移除监听）。

### 验收（真实浏览器 + 真实后端）

| # | 验收 | 结果 |
|---|---|---|
| 1 | 1440 | ✅ 220 / padding 24 / 未折叠 |
| 2 | 1024（评审修正后） | ✅ **220 展开**、content 804（二级菜单可用） |
| 3 | 769 / 768 | ✅ 769=220；768=64 / padding 12 |
| 4 | 390 | ✅ 64 / content **326**（改前约 122）/ padding 12 / 溢出 0 |
| 5 | 390 四页（cost/tasks/collect） | ✅ 均 64/326/12/0 |
| 6 | 384 回归 | ✅ 64 / content 320 |
| 7 | 390 手动展开/收起 | ✅ 64→220→64 |
| 8 | 跨断点双向 | ✅ 1440→390 折叠；390→1440 展开；1024→769 保持 220 |
| 9 | 390 首帧 | ✅ 218 帧采样只出现 64px，无 220 闪烁 |
| 10 | 1024 二级菜单 | ✅ 6 项可见、展开/收起/再展开、告警事件真实跳转 |
| 11 | 功能回归 | ✅ 预算抽屉、任务概览 chip 正常 |
| 12 | console | ✅ 主流程 0 error |

独立 QA：**22/22 PASS**（快照 `0fbbe55`，运行前后 hash 一致）。

### 测试

- `vitest run`：58 files / 986 tests 通过（布局 26 例，含新增 3 例集成测试）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：首轮 2 major（自动折叠零集成测试、769~1024 图标态二级菜单不可达）；修复后将断点收敛到 768 并补 3 例集成测试，复核 **Approve**（全量 986 例绿）。
- 独立 QA：首轮 16/16 PASS；复测 22/22 PASS，含 1024 二级菜单真实跳转、390 首帧 218 帧无闪烁、384 回归。
- 残留（不阻塞）：测试加固 n2-n4（跨断点断言空转、innerWidth 描述符未还原、监听移除身份断言）、≤768 图标态导航需先展开、顶栏 390 拥挤。

### 新发现（下一轮候选）

- **服务树菜单指向已移除路由**：`menu-config.ts` 仍保留 `/service-tree` 等入口，而路由已在 `5739d7e`（CMDB 拆出至 e-cmdb-web）移除，点击进入独立 404 且外壳卸载。属既有问题、非本轮引入，建议下一轮清理/重定向。
- 未登录冷启动直访 `/cam/*` 有 1 条既有 `Invalid navigation guard`（与视口无关），主流程 0。

### 版本组合

- 前端：`0fbbe55`（合并为 `4f2e871`）。
- 后端：`main@ef5b307`（无接口改动）。

---

## cam-iteration-20261027-collect-running-summary（2026-10-27）

**状态**：**已合并**（`e-cam-web#11`，merge commit `22eecbd`，2026-10-27 22:57 +08）
**代码 PR**：`e-cam-web#11`（commits `e9dac10` → `51ec413` → `df32dbe`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 采集日志共 1959 条，其中 **29 条运行中**；最早一条开始于 **2026-03-05**，最长已运行 **236 天**，多条 `duration_ms=0`，属进程中断后未收敛的僵尸任务。
- 页面已有逐行「已运行 X」，但无全局汇总：运维需切到「运行中」筛选才能知道积压规模与最老任务。

### 需求卡

- 目标用户：运维，判断采集任务是否积压/卡死。
- 目标：日志标题下展示「运行中 N 条，最长已运行 X天Y小时」（全局口径）；「只看运行中」快捷筛选；手动刷新与 5s 轮询按 60s 节流刷新汇总。
- 非目标：自动关闭/重试僵尸任务（需产品确认真实任务最长时长）、后端改动。
- 规则：仅统计 `status=running`；「最长」按 `start_time`（缺失回退 `ctime`）计算；纯展示，不改变任务状态。
- API 契约：复用 `GET /api/v1/cam/cost/collect/logs?status=running&offset=0&limit=200`，**后端零改动**。
- 风险/回滚：额外汇总请求（60s 节流 + 在途复用）；>200 条时最长时长采样偏小（count 用 `total`）；单提交回滚。

### 改动

- `src/views/cam/collect/format.ts`：新增 `formatLongDuration`（跨天 X天Y小时、非有限值保护）、`summarizeRunningLogs`（数量/最早开始/最长时长）。
- `src/views/cam/collect/format.test.ts`：新增 12 例（跨天、空数组、无时间 running、未来时间、ctime 回退、NaN、跨天边界）。
- `src/views/cam/collect/index.vue`：
  - 日志标题下汇总条 + 「只看运行中」；
  - 汇总独立请求，60s 节流、`summaryInflight` 在途复用、`summarySeq` 防并发覆盖；
  - 列表成功后节流刷新汇总；手动刷新/只看运行中强制刷新；
  - `flex-wrap`、`aria-live`（纯文本 span）、`count` 取后端 `total`。

### 验收（真实浏览器 + 真实后端）

| # | 验收 | 结果 |
|---|---|---|
| 1 | 汇总条文案 | ✅「运行中 29 条，最长已运行 236天2小时」 |
| 2 | 数量/最长与接口一致 | ✅ 29/29、236/236 天（差值 0） |
| 3 | 挂载仅 1 次汇总请求 | ✅ 在途复用生效 |
| 4 | 筛选不改变汇总 | ✅ 切状态/账号后文案逐字不变且不重取 |
| 5 | 只看运行中 | ✅ 20/20 行运行中、请求带 status、按钮消失 |
| 6 | 清空筛选 | ✅ 恢复混合列表与分页 |
| 7 | 手动刷新 | ✅ 汇总请求 +1，无重复并发 |
| 8 | 刷新后不陈旧 | ✅ 条带 = 实时接口 |
| 9 | 轮询刷新汇总 | ✅ 60s 节流到期后零操作自动更新（注入 7 条/3天4小时被采纳） |
| 10 | 500 降级 | ✅ 保留旧值、列表正常、无 pageerror |
| 11 | console | ✅ 干净阶段 0 error（500 注入的 2 条单独归档） |
| 12 | 390 | ✅ 条带折行可见、整页溢出 0 |

独立 QA：**13/13 PASS**（快照 `df32dbe`，跑前跑后 blob 一致）。

首版汇总条使用 `--el-color-warning-light-9` 在深色主题下数字不可见（截图发现），已改为 `color-mix(in srgb, var(--el-color-warning) 12%, transparent)` + 边框，实测对比度 5.23:1 / 数字 15.96:1。

### 测试

- `vitest run`：59 files / 994 tests 通过（collect 33 例，format 22 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS；首轮 Vitest 因 `src/views/logs/useLogUrlSync.ts` 的 `window is not defined`（既有异步计时器问题）失败，**重跑即绿**。

### 子 Agent 与复核

- 独立审查：首轮 1 major（汇总只在挂载/手动刷新取快照，60s 节流不可达）+ 若干 minor；修复后复核 **可合入**，确认节流、`total`、`summarySeq`、`offset`、头部刷新、a11y 均落地。
- 独立 QA：13/13 PASS，含「轮询自动刷新汇总」「刷新后不陈旧」「500 保留旧值」「挂载仅 1 次请求」「a11y 单 live region」。
- 残留（不阻塞）：汇总 500 时共享 axios 拦截器仍弹全局 toast；最长时长按 200 条采样（>200 偏小）；`formatLongDuration` 与表格「已运行」粒度不同（≥1 天丢分钟）。

### 版本组合

- 前端：`df32dbe`（合并为 `22eecbd`）。
- 后端：`main@08fc306`（采集日志接口无 diff）。

### 后续建议

1. **P1 僵尸任务治理**：为采集任务增加「启动对账 + 超时回收」，需产品确认真实任务最长允许时长（当前最老 236 天）。
2. **P3 汇总失败静默**：为汇总请求单独关闭全局错误 toast。
3. **P3 >200 采样**：后端提供 count/max(start_time) 聚合字段后可去掉采样。
4. **既有 390 问题**：采集页「账期范围」控件被 page-container 裁掉约 36px（非本轮引入）。

---

## cam-iteration-20261027-task-pending-timing（2026-10-27）

**状态**：**已合并**（`e-cam-web#12`，merge commit `09b904b`，2026-10-27 23:5x +08）
**代码 PR**：`e-cam-web#12`（commits `3055663` → `acc31d2`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 任务页筛选「待执行」后，卡片显示 **开始时间** 与 **耗时**（示例「50分51秒」），且随时间增长，语义上像正在执行。
- 接口核对：`GET /tasks?status=pending&limit=100` 的 38 条待执行中，**27 条仍带 `started_at` 与 progress（22%~60%）**；QA 复测 33 条中 24 条带 `started_at`——任务重排/重试后状态回到 pending，但执行时间戳未清空。
- 规则：pending 不使用执行耗时口径，改展示自 `created_at` 起的「已等待 X」；运行中/已完成/失败/取消仍展示开始时间与耗时（终态无 completed_at 时不展示耗时）。

### 需求卡

- 目标用户：运维，判断任务是在排队还是执行中。
- 目标：待执行卡片/详情页展示「已等待 X」，不出现「开始时间/耗时/完成时间」；运行中继续展示开始时间与耗时。
- 非目标：修复后端 pending 残留 `started_at` 的根因（任务生命周期/重试语义，P1 另立项）。
- API 契约：复用 `GET /api/v1/cam/tasks`、`/tasks/{id}`，**后端零改动**。
- 风险/回滚：纯前端展示；等待时长随 5s 轮询刷新；单提交回滚。

### 改动

- `src/views/tasks/timing.ts`（新增）：`shouldUseExecutionTiming`、`formatDurationMs`（天/小时/分/秒，NaN/Infinity/负值兜底）、`pendingWaitDurationText`（created_at 口径）。
- `src/views/tasks/timing.test.ts`（新增，6 例）。
- `TaskCard.vue` / `detail.vue`：pending 显示「已等待」；开始时间/完成时间/耗时均受执行口径控制；耗时统一 `formatDurationMs`，非 running 且无 completed_at 不展示。

### 验收（真实浏览器 + 真实后端）

| # | 验收 | 结果 |
|---|---|---|
| 1 | 数据前提 | ✅ pending 33 条，其中 24 条带 started_at |
| 2 | 待执行不显示开始时间/耗时 | ✅ 前 20 卡命中 0 |
| 3 | 待执行显示已等待 | ✅ 20/20 |
| 4 | 已等待与 created_at 一致 | ✅ 1.29h vs UI 1小时17分（差 0.00h） |
| 5 | 详情页同规则 | ✅ 有「已等待」、无开始时间/耗时；残留 started_at 任务显示 13小时40分 |
| 6 | 运行中仍显示开始时间+耗时 | ✅ 5/5，且无「已等待」 |
| 7 | 70s 增长采样 | ✅ 5/5 增长（+60s/+120s），非静态快照 |
| 8 | 390 无整页/卡片内溢出 | ✅ 0，已等待可见 20/20 |
| 9 | console | ✅ 0 error / 0 pageerror（8888 登录噪音 0） |

独立 QA：**A–G 全 PASS + 5 项补充断言 PASS**（快照 `acc31d2`，跑前/跑后 4 文件 SHA256 一致）。

### 测试

- `vitest run`：60 files / 1000 tests 通过（tasks 17 例：filters 11 + timing 6）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：**Approve**（无 blocker/major）；指出完成时间未纳入口径、耗时未复用 helper（NaN/负数）、cancelled 残留 started_at 等 6 minor；已修前两类并补测试。
- 独立 QA：首轮在干净 `3055663` 全 PASS；最终在 `acc31d2` 复跑 A–G 全 PASS，并记录源码哈希一致性。
- 残留（不阻塞）：`cancelled` 若只改状态不写 `completed_at` 仍可能显示耗时（依赖后端语义）；「已等待」把重试前执行时长计入；组件级测试缺失（纯函数覆盖）；`formatDurationMs` 与 collect 的时长格式化重复。

### 版本组合

- 前端：`acc31d2`（合并为 `09b904b`）。
- 后端：`main@da8949a`（任务接口无 diff）。

### 后续建议

1. **P1 任务生命周期治理**：pending 残留 started_at/progress 说明重排未清执行态；建议统一「重排重置执行字段 + 超时回收 + 持久化去重」，并补 `attempt/last_attempt_at` 字段以区分「首次排队/重试等待」。
2. **P3**：`cancelled` 无 completed_at 的耗时口径需后端确认；时长格式化可下沉 `utils/formatters` 统一。
3. **P3**：TaskProgress 运行中进度百分比重复显示（条右 + 条下）、详情页头部「新增」按钮等既有观察，非本轮范围。

---

## cam-iteration-20261028-collect-export（2026-10-28）

**状态**：**已合并**（`e-cam-web#13`，merge commit `c5679a8`，2026-10-28 02:29 +08）
**代码 PR**：`e-cam-web#13`（commits `c187e5f` → `38441a9`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 采集日志页只能在线翻页查看（当时 1964 条），运维做周报/对账需要把日志带走，页面没有导出能力。
- 页面已有账号/状态筛选与分页，错误信息包含逗号/引号/换行（华为/AWS 失败行），对 CSV 转义有真实要求。

### 需求卡

- 目标用户：运维/财务，离线核对采集结果与失败原因。
- 目标：按当前筛选导出 CSV；列名与表格一致；Excel 中文不乱码；特殊字符不破坏列结构；大量数据有上限与提示。
- 非目标：后端导出任务、异步大文件、审计留痕。
- 规则：遵循账号/状态筛选；单次上限 **5000** 条；文件名 `collect-logs-YYYYMMDD-HHmm.csv`；UTF-8 BOM；空结果不生成文件。
- API 契约：复用 `GET /api/v1/cam/cost/collect/logs?account_id=&status=&offset=&limit=`，**后端零改动**。
- 正常/加载/空/失败/恢复：导出中按钮 loading 防重复；空结果提示「当前筛选无数据，未生成文件」；失败 toast；失败不留半份文件。

### 改动

- `src/views/cam/collect/export.ts`（新增）：`csvCell`（RFC4180 转义 + Excel 公式注入中和）、`buildCollectLogCsv`（BOM + 9 列表头）、`exportFileName`、`EXPORT_MAX_ROWS=5000`。
- `src/views/cam/collect/export.test.ts`（新增，8 例）。
- `src/views/cam/collect/index.vue`：导出按钮；导出前快照筛选；分页拉取（每页 200）以短页为终止条件、`total` 仅提示；按 id 去重；空结果不下载；`disposed` 检查；`revokeObjectURL` 延迟 1s。

### 验收（真实浏览器 + 真实后端）

| # | 验收 | 结果 |
|---|---|---|
| 1 | 全量导出 | ✅ 1964 行 = 接口 total 1964；BOM；9 列表头精确 |
| 2 | 逐字段比对 | ✅ 1964 行 0 差异（账号/厂商/状态/账期/记录数/耗时/错误/采集时间） |
| 3 | 转义 | ✅ 692 特殊行（逗号 691/引号 259/换行 306），恒 9 列，引号双写，解码=原值 |
| 4 | 筛选导出 | ✅ 运行中 29 行 = 接口；账号 #2 386 行 = 接口 |
| 5 | 交互 | ✅ loading/disabled，连点 2 次仅 1 次下载、1 组分页 |
| 6 | 空结果（评审后行为） | ✅ 无下载、提示「当前筛选无数据，未生成文件」 |
| 7 | 390 | ✅ 按钮可见、整页溢出 0 |
| 8 | console | ✅ 0 error / 0 pageerror / 0 requestfailed |
| 9 | 上限 CONTRACT（total=6000） | ✅ 5000 行 / 25 页 / 「共 6000 条，仅导出前 5000 条」 |

独立 QA：**核心 69/69 PASS + 上限 CONTRACT 5/5 PASS**（快照 `38441a9`，与 c187e5f 同数据 CSV 逐字节一致）。

### 测试

- `vitest run`：61 files / 1007 tests 通过（collect 41 例，export 8 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：**无 blocker，2 major**（导出期间筛选漂移、total 缺失静默截断）+ 若干 minor；已修 major 与公式注入/空结果/去重/revoke/离页保护。
- 独立 QA：首轮 c187e5f 54/54；修复后以 `38441a9` 复跑 **69/69**，并专门验证快照筛选（导出中切筛选仍 1964 行）、短页终止、去重、公式中和、空结果无下载、上限提示。
- 未直接验证：`revokeObjectURL` 1s 时序（仅确认下载完整）；导出中离页 `disposed` 分支；真实 >5000 数据（CONTRACT 造数）。

### 版本组合

- 前端：`38441a9`（合并为 `c5679a8`）。
- 后端：`main@6fdc34e`（采集日志接口无 diff）。

### 后续建议

1. **P3 口径统一**：CSV 账期为原始 RFC3339、表格显示日期；运行中耗时 CSV「进行中」、表格「已运行 X」——已注释为导出快照口径，如需完全一致可复用 `formatElapsed/formatDate`。
2. **P3 性能**：5000 条上限下最多 25 次串行请求；仓库已有 `utils/exportAll.ts#fetchAllRows`（带进度）可复用，或把每页提升到 500。
3. **P3 审计**：导出不含操作者/租户审计字段；如需留痕应走后端导出任务。
4. **P1 仍待办**：任务生命周期治理（pending 残留 started_at）、采集 running 超时回收、菜单死链（`menu-config.ts` 仍被其他会话占用）。

---

## cam-iteration-20261028-task-oldest-pending（2026-10-28）

**状态**：**已合并**（`e-cam-web#14`，merge commit `96fcf19`，2026-10-28 03:16 +08）
**代码 PR**：`e-cam-web#14`（commits `6f50042` → `f4e3724`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 任务页概览只有「待执行 19 · 执行中 5」数量，没有积压年龄；列表按创建时间倒序，最早待执行任务不在首屏。
- 接口核对：`GET /tasks?status=pending&limit=100` 最早 `created_at=2026-10-27T00:32:41Z`，页面观测时已等待约 **17 小时**，无法判断是短暂排队还是队列饿死。
- 规则：取 `pending` 任务 `created_at` 最早值计算等待时长；纯展示，不改变任务状态。

### 需求卡

- 目标用户：运维，判断任务队列是否长期积压。
- 目标：概览积压提示追加「最老等待 X」；随列表刷新按 60s 节流更新；手动刷新/类型变更强制更新；与类型筛选同口径。
- 非目标：调度策略、超时回收、pending 残留 started_at 的后端修复（P1）。
- API 契约：复用 `GET /api/v1/cam/tasks?type=&status=pending&offset=&limit=`，**后端零改动**。
- 边界：无 pending 或时间非法时隐藏；pending=0 隐藏；失败保留上次值；pending>100 时补取最后一条（后端固定 created_at DESC）。

### 改动

- `src/views/tasks/queue.ts`（新增）：`oldestPendingWaitText`（仅 pending、非法/未来时间处理、`formatDurationMs`）。
- `src/views/tasks/queue.test.ts`（新增，3 例）。
- `src/views/tasks/index.vue`：概览追加「最老等待 X」；`fetchOldestPending`（60s 节流、seq、失败保留、携带 type、total>一页时补取最后一条）；类型变更/清空/手动刷新强制；pending=0 隐藏；时长 `nowrap`。

### 验收（真实浏览器 + 真实后端）

| # | 验收 | 结果 |
|---|---|---|
| 1 | 概览出现最老等待 | ✅「待执行 16 · 执行中 5 · 最老等待 17小时12分」 |
| 2 | 与接口最早 pending 一致 | ✅ UI 17.20h vs 接口 17.21h（delta 0.01h） |
| 3 | 首屏仅 1 次 pending 查询 | ✅（旧版 2 次）；total=pageCount 时不补请求 |
| 4 | 刷新后一致 | ✅ 查询 +1、文案不变 |
| 5 | 状态筛选不影响全局口径 | ✅「已完成」下文案逐字相同 |
| 6 | 类型筛选跟随类型 | ✅ 同步资产：计数 16→10、请求带 `type=cam:sync_assets`、最老与类型接口一致 |
| 7 | M1 pending>100 精确最老 | ✅ total=250 时补 `offset=249&limit=1`，显示 1天6小时（非截断的 2 小时） |
| 8 | pending=0 隐藏最老等待 | ✅ 提示保留、`.oldest-wait` 为 0 |
| 9 | 390 无孤字 | ✅ nowrap 行盒=1、无裁切、无整页溢出 |
| 10 | console | ✅ 应用/登录均 0 |

独立 QA：**27 条断言 26 PASS**，唯一 FAIL 为测试自身未拦截 M1 补请求（已复核为断言范围问题，非产品缺陷）。

### 测试

- `vitest run`：62 files / 1011 tests 通过（tasks 20 例：filters 11 + timing 6 + queue 3）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：**2 major**（pending>100 低估、类型筛选口径混用）+ 3 minor；已修 major 与 pending=0 残留、390 孤字、首屏重复请求。
- 独立 QA：首轮 15/15；修复后以 `f4e3724` 复跑 26/27，覆盖类型口径、M1 合成 250 条、pending=0、390 nowrap。
- 残留（不阻塞）：提示中「待执行/执行中」与上方 chip 数值重复；pending>100 时补请求在超大集合下 skip 成本需留意；无 aria-live。

### 版本组合

- 前端：`f4e3724`（合并为 `96fcf19`）。
- 后端：`main@ffcd221`（任务接口无 diff）。

### 后续建议

1. **P1 任务生命周期治理**：pending 残留 started_at/progress、队列长期积压（当前 pending 16~21、最老 17h）；建议重排重置执行字段 + 超时回收 + 持久化去重，并补 `attempt/last_attempt_at`。
2. **P3 口径**：最老等待与卡片「已等待」同用 created_at，重试前执行时长仍计入等待。
3. **P3 交互**：概览与 chip 数值重复，可考虑概览只保留新增信息（最老等待）或合并展示。

---

## cam-iteration-20261028-task-requeued-tag（2026-10-28）

**状态**：**已合并**（`e-cam-web#15`，merge commit `c39bf0c`，2026-10-28 03:00 +08）
**代码 PR**：`e-cam-web#15`（commits `3998955` → `3012ddd`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 2026-10-27 巡检：`GET /tasks?status=pending&limit=100` 的 **38 条待执行中 27 条带 `started_at`（progress 22%~60%）**，说明任务被重排/重试后回到 pending，但执行时间戳未清空。
- 上一轮（#12）为避免误读，pending 卡片不再展示开始时间/耗时，改为「已等待」；副作用是**重排痕迹对运维完全不可见**。
- 本轮补纯事实标记：pending 且 `started_at` 可解析 → 卡片「曾执行·待重排」、详情「上次执行: 曾开始执行（当前等待重排）· 上次开始时间」。不改变任务状态。

### 需求卡

- 目标用户：运维，判断任务是否被反复重排/重试。
- 目标：待执行任务残留执行时间时有可见标记与解释；运行中/终态不显示；非法/空时间不误标。
- 非目标：修复后端重排未清执行字段的根因（P1，需产品确认重试语义/超时规则）、重试入口（后端无 retry 接口）。
- 规则：`status==='pending' && startedAt 可解析` → 标记；纯展示。
- API 契约：复用 `GET /api/v1/cam/tasks`、`/tasks/{id}`，**后端零改动**。
- 风险/回滚：标签可能被误读为「执行过且失败」——已用 info 色 + 「待重排」措辞 + tooltip 弱化；单提交回滚。

### 改动

- `src/views/tasks/timing.ts`：新增 `isRequeuedPending`（校验 `Date.parse` 可解析）。
- `src/views/tasks/timing.test.ts`：+2 例（含 failed/cancelled/空串/非法串）。
- `TaskCard.vue`：meta 行「曾执行·待重排」info 标签（title + aria-label + 窄屏 align-self）。
- `detail.vue`：「上次执行」信息项，附上次开始时间。

### 验收

> 验证时真实 pending 已排空（total=0），渲染逻辑用 `page.route` 合成 3 条 pending（2 条带 started_at）+ 详情；运行中/终态用合成与真实数据复核。

| # | 验收 | 结果 |
|---|---|---|
| 1 | 合成 3 卡中恰好 2 卡标记 | ✅ tagged=2、untagged=1，按 id 对齐 |
| 2 | 标签文案/title/aria | ✅「曾执行·待重排」、title 含重排、aria-label 正确 |
| 3 | 详情（带 started_at） | ✅「曾开始执行（当前等待重排）· 3小时前」 |
| 4 | 详情（不带 started_at）不显示 | ✅ |
| 5 | 执行中/终态不标记 | ✅ 合成 running/completed/failed 均 0；真实已完成 20 卡 0 |
| 6 | 真实 pending=0 页面正常 | ✅ 20 卡渲染、无崩溃 |
| 7 | 390 标签不拉伸/无溢出/不重叠 | ✅ 91×20、交集 0、docOverflow 0 |
| 8 | console | ✅ :5187 0 error / 0 pageerror；:8888 噪音 0 |
| 9 | 非法/空 started_at 不标记 | ✅ |

独立 QA：**27/27 断言 PASS**（快照 `3012ddd`，运行前后 HEAD 一致）。

配色对比度：修复前 warning 2.00:1（深色主题突兀），修复后 info **5.25:1**（WCAG AA）。

### 测试

- `vitest run`：63 files / 1013 tests 通过（timing 8 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：1 major（warning 色 + 「曾执行」易读成告警）、5 minor；已修配色/文案/时间校验/详情时间/窄屏对齐。
- 独立 QA：`3998955` 首轮 29/29；修复后 `3012ddd` **27/27 PASS**，含几何/对比度/真实空队列回归。
- 未验证：真实 pending 数据的渲染（验证时队列为空，属环境时序；真实数据证据见需求来源）；触屏/键盘 tooltip 可达性（观察到原生 title 不可达）。

### 新发现（非本轮引入）

- **390 下任务列表内部横向溢出 37px**：归因 `.el-pagination`（宽 424，右边界 439 > 容器 402），与本次标签无关；建议后续修分页在窄屏的布局。
- 原生 `title` 在触屏/键盘不可达，`aria-label` 落在无 role 的 span 上读屏可能忽略；如需解释可改 `el-tooltip` 或卡片内直出说明。

### 版本组合

- 前端：`3012ddd`（合并为 `c39bf0c`）。
- 后端：`main@48c01cf`（任务接口无 diff）。

### 后续建议

1. **P1 任务生命周期治理**：重排放回 pending 未清 `started_at/progress/completed_at`；建议统一重置 + 超时回收 + 持久化去重，并补 `attempt/last_attempt_at`，届时标签可升级为「重试 N 次」。
2. **P3 窄屏分页**：修复 `.el-pagination` 390 溢出（当前 37px 内部滚动）。
3. **P3 tooltip 可达性**：触屏/键盘场景下补充可见说明或 `el-tooltip`。

---

## cam-iteration-20261028-pagination-narrow（2026-10-28）

**状态**：**已合并**（`e-cam-web#16`，merge commit `baa94cf`，2026-10-28 03:58 +08）
**代码 PR**：`e-cam-web#16`（commits `40471f2` → `7ea6aac`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮独立 QA 在 390×844 实测任务页 `.pagination-container` 内部横向溢出 37px，归因 `.el-pagination`（宽 424 > 容器 402）。
- 本轮复测：390 下分页 **677px** / 容器 **310px**，内部溢出 **185px**；`layout="total, sizes, prev, pager, next, jumper"` 不换行导致控件被裁。
- 审查进一步指出：仅按 ≤768 加媒体查询会漏掉 **769–940**（768 起侧栏由 64 恢复 220，可用宽度反向跳变）；369/820/900 实测仍溢出 84/68/18px。

### 需求卡

- 目标用户：手机/窄窗口运维。
- 目标：分页控件在任何可用宽度不足时自动换行，不超出容器；宽屏保持单行、视觉不变。
- 非目标：改分页 layout/pager-count、重排控件、采集页分页（当前无溢出）、页面级 12px bleed（既有问题）。
- 规则：纯 CSS，对 `:deep(.el-pagination)` 无条件 `flex-wrap: wrap`；宽度足够时天然单行。
- API 契约：无。
- 风险/回滚：仅任务页样式；1440 截图与修复前逐像素一致；单提交回滚。

### 变更内容

- `src/views/tasks/index.vue` scoped 样式：`.pagination-container` 内 `:deep(.el-pagination)` 设置 `flex-wrap: wrap; justify-content: center; row-gap: 8px; width: 100%`（不设媒体查询，覆盖侧栏展开区间）。

### 验收（真实浏览器 + 真实后端）

| 视口 | 容器内溢出（修复前 → 后） | pagination 尺寸（后） |
|---|---|---|
| 390 | 185 → **0** | 276×112（3 行） |
| 768 | — → **0** | 654×72（2 行） |
| 769 | 84 → **0** | 475×72（2 行） |
| 820 | 68 → **0** | 526×72 |
| 900 | 18 → **0** | 606×72 |
| 1024 | — → **0** | 730×32（单行） |
| 1440 | 0 → **0** | 1146×32（单行，截图与修复前逐像素一致） |

- 390 翻页可用：点第 2 页 active=2、卡片 ID 变化、翻页后三类溢出均 0、6 控件可见。
- 全矩阵 `elementFromPoint` 命中自身、无遮挡；与列表 gap 16px。

独立 QA：**1–7 项全部 PASS**（快照 `7ea6aac`）。

### 测试

- `vitest run`：63 files / 1013 tests 通过（纯样式改动，无新增用例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：1 major（断点 768 漏掉 769–940）+ 2 minor（页面级 12px bleed、390 高度依赖滚动条）+ nits；已修 major，M2 作为既有问题记录。
- 独立 QA：首轮 390/768/1440 PASS、769 FAIL；修复后 `7ea6aac` **全 PASS**，769/820/900 溢出归零，1440 与旧版逐像素一致。
- 未处理：页面级 12px bleed（`.tasks-page` 负 margin -24 与窄屏 padding 12 不匹配，既有）；390 下 3 行 112px；`width:100%` 使元素盒变宽但内容坐标不变。

### 版本组合

- 前端：`7ea6aac`（合并为 `baa94cf`）。
- 后端：`main@059eb2c`（无接口改动）。

### 后续建议

1. **P3 页面级 bleed**：`tasks-page` 负 margin 在 ≤768 改为 -12px（或去掉 bleed），消除 12px 横向滚动；需同时评估分页可用宽度（可能需 `pager-count=5`）。
2. **P3 窄屏分页更紧凑**：≤400px 可考虑 `:small` / `pager-count=5`，减少 3 行高度。
3. **P1 仍待办**：任务生命周期治理（pending 残留执行态、超时回收、持久化去重）、采集 running 超时、菜单死链（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-tasks-bleed（2026-10-28）

**状态**：**已合并**（`e-cam-web#17`，merge commit `44f23cc`，2026-10-28 05:26 +08）
**代码 PR**：`e-cam-web#17`（commits `761c3ab` → `f7364d9`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮独立 QA 两次记录：390/768 下任务页存在 **12px 页面级横向滚动**；归因 `.tasks-page { margin: -24px }` 仍按外层 24px padding 设计，而 ≤768 时 `MainLayout` 把 padding 改为 12px。
- 本轮修复前复测：390/768 `main-content scrollWidth-clientWidth = 12`；769/1440 为 0（padding 24 与 -24 匹配）。

### 需求卡

- 目标用户：手机/窄窗口运维。
- 目标：≤768 消除页面级 12px 横向滚动；分页容器仍能放下；≥769 与 1440 不变；头部与卡片左右对齐。
- 非目标：改动 MainLayout 全局 padding、分页控件布局、其它 22 个负 margin 页面（记录为后续）。
- 规则：窄屏 `margin: -12px`、`height: calc(100% + 24px)`；`tasks-content` 左右 padding 20→8px；`page-top` 左右 padding 20→8px。
- API 契约：无。
- 风险/回滚：仅任务页样式；769/1440 截图与修复前逐字节一致；单提交回滚。

### 变更内容

- `src/views/tasks/index.vue` scoped 样式：
  - `.tasks-page`：≤768 `margin:-12px; height:calc(100% + 24px)`；
  - `.tasks-content`：≤768 `padding: 12px 8px`；
  - `.page-top`：≤768 `padding: 16px 8px 12px`（对齐修复）。

### 验收（真实浏览器 + 真实后端）

| 指标 | 390 | 768 | 769 | 1440 |
|---|---|---|---|---|
| main-content 横向溢出（前→后） | 12→**0** | 12→**0** | 0→0 | 0→0 |
| 实际可横滚 scrollLeft | 12→**0** | 12→**0** | 0 | 0 |
| 分页容器 delta | **0** | **0** | 0 | 0 |
| pagination / 容器内宽 | 276/278 | 654/656 | 475/477 | 1146/1148 |
| 头部左 / 卡片左 | **72 / 72** | — | 240/240 | 240/240 |

- 390 翻页：`GET /tasks?offset=20&limit=20` 200、active=2、20 条新 ID、与第 1 页重叠 0；分页 703..815 完整可见。
- 390 全 DOM 越界元素 0；console 0 error/warning/pageerror。
- 769/1440 截图与修复前**逐字节相同**。

### 已知取舍/观察

- 769 分页为 2 行（72px）：单行需视口 ≥970px，该行为来自已合入的 #16（无条件换行），非本次回归。
- 390 分页余量仅 2px（276/278），总数变长会多换一行（wrap 兜底，不会横向溢出）。
- `tasks-content` 20→8 与页边 −24→−12 叠加后卡片横向位置不变，仅页面背景盒内收 12px。
- **新发现（既有，独立缺陷）**：筛选区两个 `el-select` 计算宽度恒 44px、只有箭头无文案（390/768/1440 均如此，修复前后逐像素一致）——建议下一轮排查表单样式。

### 测试

- `vitest run`：63 files / 1013 tests 通过（纯样式）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：通过、无 blocker/major；3 minor（头部对齐、魔法数、22 个同类页面）+ 4 nit；已修头部对齐。
- 独立 QA：`f7364d9` 复跑 1–5 项 PASS（3 项中 769 单行按字面 FAIL 但为 #16 既有行为）；确认头部/卡片左对齐、翻页、console。
- 未处理：魔法数统一（建议 `--page-gutter` 变量）、其余 22 个负 margin 页面。

### 版本组合

- 前端：`f7364d9`（合并为 `44f23cc`）。
- 后端：`main@059eb2c`（无接口改动）。

### 后续建议

1. **P3 bleed 统一**：23 处 `margin:-24px` 建议走 `--page-gutter` 变量，一次性修掉 22 个页面 ≤768 的 12px 滚动。
2. **P3 筛选下拉宽度异常**：两个 `el-select` 44px 仅箭头，疑似表单样式塌缩，建议排查（影响 390/768/1440 全端）。
3. **P1 仍待办**：任务生命周期治理（pending 残留执行态、超时回收、持久化去重）、采集 running 超时、菜单死链（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-filter-width（2026-10-28）

**状态**：**已合并**（`e-cam-web#18`，merge commit `51856ca`，2026-10-28 06:12 +08）
**代码 PR**：`e-cam-web#18`（commits `4a551b9` → `6f463f3`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮 QA 观察：任务页筛选区两个 `el-select` 恒为 **44×32**、文本盒 0、placeholder 与选中值不可见（1440/390 均如此），只剩箭头。
- 根因：Element Plus `el-select` 默认 `width:100%`，inline 表单的 `.el-form-item__content` 为 `flex:1; min-width:0`，百分比在收缩语境解析为 0，只能由 min-content 撑开（padding 24 + gap 6 + caret 14 = 44px）。
- 属**内在尺寸塌陷**而非空间不足：1440 下筛选区 inner ~1148px，只需 472px，仍塌陷。

### 需求卡

- 目标用户：运维，使用任务类型/状态筛选。
- 目标：下拉有可读宽度，placeholder 与选中值可见；选择/清空正常；窄屏可换行且不横向溢出。
- 非目标：cost 页同类问题（记录为后续）、共享 FilterBar 统一改造。
- 规则：`.filters-form` flex + wrap，`gap: 8px 24px`；`:deep(.el-select)` 显式 160px。
- API 契约：无。
- 风险/回滚：仅任务页样式；1440/1024 单行、769/390 两行；单提交回滚。

### 变更内容

- `src/views/tasks/index.vue` scoped 样式：`.filters-form` 增加 `display:flex; flex-wrap:wrap; gap:8px 24px`；表单项 `margin-right:0`；`:deep(.el-select)` 固定 `width:160px`。

### 验收（真实浏览器 + 真实后端）

| 视口 | 下拉宽度 | 行数 | main-content 溢出 | 交互 |
|---|---|---|---|---|
| 1440 | 160×32 | 1 | 0 | placeholder 可见、类型筛选 total 7046→6574→7046 |
| 1024 | 160×32 | 1 | 0 | 选择「同步账单」total 2 后恢复 |
| 900 | 160×32 | 1 | 0 | 选择/清空正常 |
| 769 | 160×32 | 2 | 0 | 选择/清空正常（内容宽 475 < 480，差 5px 折行） |
| 390 | 160×32 | 2 | 0 | 两行间距 8、无重叠；下拉面板在视口内 |

- 键盘：Tab 聚焦 `role=combobox`、Enter 展开、Esc 关闭且焦点保留；label `for` 与 input id 对应。
- 长文案：「创建云主机」70px / 文本盒 116px，无需更宽。
- A/B：浏览器内覆盖 `width:100% !important` 可复现 44px + 文本盒 0，证明修复生效。

独立 QA：**1–8 项全部 PASS**（快照 `6f463f3`）。

### 测试

- `vitest run`：63 files / 1013 tests 通过（纯样式，无新增用例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：Approve with nits（0 blocker/major）；确认 44px 可被 EP 样式算术复原、`:deep` 作用域正确；指出间距 32→16、140 档收益有限、cost 页同类问题。
- 已按审查：移除 140 档、间距改 24px。
- 独立 QA：`6f463f3` 8/8 PASS，含 A/B 反证、键盘、390/769/900/1024 矩阵。
- 观察（不阻断）：placeholder 对比度约 **2.32:1**（选中值 12.29:1），为既有 token 问题；769/1024 顶栏右侧裁切为既有 shell 问题；分页英文 `Total`。

### 版本组合

- 前端：`6f463f3`（合并为 `51856ca`）。
- 后端：`main@b210cb2`（无接口改动）。

### 后续建议

1. **P2 cost 页同类缺陷**：`cost/index.vue` 的 inline 表单 + 无宽度 select 同样 44px，建议下一轮修复。
2. **P3 placeholder 对比度**：2.32:1 低于 AA，建议调整 `--el-text-color-placeholder` 在深色主题下的取值。
3. **P3 顶栏裁切**：769/1024 右侧按钮被裁，既有 shell 问题。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、菜单死链（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-zh-locale（2026-10-28）

**状态**：**已合并**（`e-cam-web#19`，merge commit `a114665`，2026-10-28 07:17 +08）
**代码 PR**：`e-cam-web#19`（commits `afb1703` → `8bb7755`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 多轮 QA 观察：分页显示英文 `Total 7046`、`Go to`；Element Plus 默认 locale 为英文。
- 根因：应用入口未配置 `ElConfigProvider` locale，所有 Element 组件走默认 en。

### 需求卡

- 目标用户：全部中文用户。
- 目标：分页、日期面板、下拉空态、对话框等 Element 内置文案统一中文；布局与功能不变。
- 非目标：自研文案替换、日期格式调整、多语言切换。
- 规则：`App.vue` 外层 `el-config-provider` 绑定 `element-plus/es/locale/lang/zh-cn`。
- API 契约：无。
- 风险/回滚：影响所有 Element 组件文案；分页中文宽度略小于英文，390/1440 已验证；单提交回滚。

### 变更内容

- `src/App.vue`：引入 `zhCn`，根节点外包 `<el-config-provider :locale="zhCn">`。
- `src/components.d.ts`：自动注册 `ElConfigProvider`（构建生成，评审后补提交）。

### 验收（真实浏览器 + 真实后端）

| # | 验收 | 结果 |
|---|---|---|
| 1 | 分页中文 | ✅「共 7048 条 20条/页 … 353 前往 页」；无 Total/Go to/page |
| 2 | 翻页/跳页可用 | ✅ 下一页 active 1→2；「前往」填 1 回第 1 页 |
| 3 | 日期面板中文 | ✅「2026 年10 月 日 一 二 三 四 五 六」，英文月份 0 命中；Esc 关闭 |
| 4 | 预算对话框中文 | ✅ 标题/5 个标签/取消/确定；打开+取消期间写请求 0 |
| 5 | 下拉空态文案 | ✅ locale 取值：无数据/无匹配数据/请选择/前往/共 N 条/年 |
| 6 | 1440 / 390 布局 | ✅ doc/body 溢出 0；390 分页折 3 行（112px）仍可用 |
| 7 | console | ✅ 0 error / 0 pageerror |
| 8 | 英文残留 | ✅ 三页全文本 0 命中；属性级（placeholder/aria/title/展开面板）13 类 0 命中 |

独立 QA：**23/23 PASS**（快照 `8bb7755`）。

### 测试

- `vitest run`：63 files / 1013 tests 通过。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- `build:gz` + `ci:gzip-budget`：全量 **2099.7 / 2300 KB**，各桶 OK（入口 gzip 31.86/40 KB）。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：Approve（0 blocker/major）；确认 ElConfigProvider 覆盖 teleport 与命令式弹窗（模块级 globalConfig 回退），导入路径匹配 2.11.7，产物内联 zhCn；指出遗漏 `components.d.ts`、建议验证 gzip 预算。
- 已修：补提交 `components.d.ts`；已跑 gzip 预算通过。
- 独立 QA：`8bb7755` 23/23 PASS（分页/日期/对话框/下拉/1440/390/console/英文残留），并审计 457 个请求全为 GET。
- 未执行：`el-message` 等命令式弹窗的 UI 实测（依赖 EP 内部回退，审查在 happy-dom 验证）；全站页面逐一目视。

### 版本组合

- 前端：`8bb7755`（合并为 `a114665`）。
- 后端：`main@b9b51f3`（无接口改动）。

### 后续建议

1. **P3 分页窄屏高度**：中文 locale 后 390 分页折 3 行 112px，可配合 `pager-count=5`/`:small` 更紧凑。
2. **P3 多入口**：若未来新增入口（如独立登录页用 Element 组件），需重复配置 provider；建议抽 `installElementLocale(app)`。
3. **P3 测试**：`App.vue` 无挂载测试，建议补一条 locale 断言（或对命令式弹窗按钮文案断言）。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、菜单死链（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-pagination-compact（2026-10-28）

**状态**：**已合并**（`e-cam-web#20`，merge commit `ccdce6c`，2026-10-28 08:38 +08）
**代码 PR**：`e-cam-web#20`（commits `10f3588` → `19d3d4b` → `bdc094b` → `befcc0b`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮中文 locale 后 QA 实测：390 分页完整 layout 折 3 行 112px、768 折 2 行 72px。
- 审查进一步指出：用视口宽度做断点会在 769（侧栏 64→220）后失效，769–950 仍折行（"窗口变大反而变高"）。
- QA 复验又发现深页问题：390 末页出现两处省略号后单行需 283.5px > 可用 276px（56px 两行）；1024 深页完整 layout 需 ~740px（72px 两行）。

### 需求卡

- 目标用户：手机/平板运维。
- 目标：分页在可用宽度不足时自动降级为单行紧凑形态；宽屏保持桌面完整 layout。
- 非目标：页码 URL 同步（现状刷新回第 1 页）、其它页面分页统一、分页样式重设计。
- 规则（按容器实测宽度）：
  - `< 300px` → `prev, pager, next`（隐藏「共 N 条」，5 页码）；
  - `< 740px` → `total, prev, pager, next`（5 页码）；
  - `>= 740px` → `total, sizes, prev, pager, next, jumper`（7 页码）。
- 技术：`ResizeObserver` 监听 `.pagination-container`；`:size` 替代已废弃的 `:small`；`pagerCount` 恒为 5（Element 要求 >4 奇数）。
- API 契约：无。
- 风险/回滚：≤1024 失去每页条数/跳页；390 不显示总数；单提交回滚。

### 变更内容

- `src/views/tasks/pagination.ts`（新增）：`paginationUiForContentWidth` + 三档 layout 常量 + 阈值。
- `src/views/tasks/pagination.test.ts`（新增，3 例）。
- `src/views/tasks/index.vue`：`ResizeObserver` + `paginationUi` 绑定 `:layout/:pager-count/:size`；small 页码 24px 兜底样式。

### 验收（真实浏览器 + 真实后端）

| 视口 | 容器宽 | 形态 | 高度 | 页码 | 说明 |
|---|---|---|---|---|---|
| 390 | 310 | 紧凑（无总数） | 24px | 5 | 首页/末页/深页均单行 |
| 480 | 368 | 紧凑 | 24px | 5 | 含总数 |
| 768 | 688 | 紧凑 | 24px | 5 | 含总数 |
| 769 | 509 | 紧凑 | **72→24px** | 5 | 侧栏展开后不再折行 |
| 900 | 640 | 紧凑 | 24px | 5 | |
| 1024 | 730 | 紧凑 | 32→**24px** | 5 | 深页不再折行 |
| 1440 | 1180 | 桌面 | 32px | 7 | sizes+jumper，未变 |

- 390 翻页 1→2、上一页回 1 且列表一致；「…」步长 +3；跨 resize 页码保持。
- console error/warning/pageerror 均为 0（修复前 3 页码方案有 99 条 `pagerCount` 警告）。
- 边界：内容宽 296 无总数、301 有总数；320 视口（内容 206）仍 56px（低于验收集，仅记录）。

独立 QA：**F1/F2/F3 全部关闭，`befcc0b` 验收通过**（3 轮递进验证）。

### 测试

- `vitest run`：64 files / 1016 tests 通过（pagination 3 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：无 blocker；**1 major**（视口断点在 769 失效）已修；minor 包含 pagerCount 校验、small 废弃、CSS 覆盖顺序、测试缺口。
- 独立 QA：`19d3d4b` 发现 F1（390 深页 56px）、F2（1024 深页 72px）→ 修复后 `bdc094b` 又发现 F3（pagerCount=3 触发 99 warning + 省略号步长退化）→ `befcc0b` 全部通过。
- 总控：处理 major + F1/F2/F3，自测 390/480/768/769/900/1024/1440 与深页矩阵。

### 已知取舍（需产品确认）

1. **390 不显示「共 N 条」**：为在 276px 内容宽内保持 5 页码 + 单行。
2. **1024 为紧凑形态**：730px < 740px 阈值，失去每页条数/跳页，换取深页不折行。
3. **页码不写 URL**：刷新回第 1 页（既有行为，未在本轮扩展）。

### 版本组合

- 前端：`befcc0b`（合并为 `ccdce6c`）。
- 后端：`main@7a08622`（无接口改动）。

### 后续建议

1. **P3 页码 URL 同步**：`?page=&size=` 写入 query，刷新/分享保持（新需求）。
2. **P3 分页公共组件**：其它页面仍用完整 layout，窄屏同样折行；建议抽 `<AppPagination>` 统一三档降级。
3. **P3 超窄视口**：320/340 仍两行，可考虑 `pager-count=3`（但需绕过 Element 校验）或隐藏省略号。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、菜单死链（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-page-url（2026-10-28）

**状态**：**已合并**（`e-cam-web#21`，merge commit `58357f3`，2026-10-28 09:25 +08）
**代码 PR**：`e-cam-web#21`（commits `9fe044c` → `4ce4a9e` → `484156c` → `551f722`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮分页验收记录：任务页页码不写 URL，刷新后回第 1 页，无法分享；实测 `/cam/tasks` 翻到第 3 页 URL 仍不变。
- 审查指出：只持久化 page 而不持久化筛选，会让"刷新/分享/返回"恢复出与当时不一致的列表切片（筛选 failed 第 2 页 → 返回变成全部任务第 2 页）。

### 需求卡

- 目标用户：运维，需要把"某筛选条件下的某页"发给同事或刷新继续。
- 目标：`page/size/type/status` 与 URL 双向同步；深链恢复；刷新保持；前进后退同步；非法值清理；越界收敛。
- 非目标：其它页面分页、排序列入 URL、查询条件全量同步。
- 规则：
  - `page` 仅纯数字且 ≤1e6；`size` 仅 10/20/50/100；
  - `type/status` 必须在选项白名单内，否则视为未筛选；
  - 默认值（page=1、size=20、无筛选）不写入 URL；
  - 页码超过总页数或结果为空时收敛到最后一页/第 1 页并更新 URL；
  - 翻页/筛选用 `router.replace`（不制造历史记录）；外部导航/前进后退由 `watch(route.query)` 同步；仅类型变化才重刷概览与最老等待。
- API 契约：无（offset/limit 仍由前端参数计算，后端已鉴权）。
- 风险/回滚：URL 变更影响书签（新增参数，向后兼容）；单提交回滚。

### 变更内容

- `src/views/tasks/pagination.ts`：新增 `readPageQuery`、`readPageSizeQuery`、`readFilterQuery`、`buildPageQuery`、`clampPageToTotal`、`PAGE_SIZE_OPTIONS`。
- `src/views/tasks/pagination.test.ts`：6 例（含非法页码、允许列表、空 filters 清理、数组保留、total=0 收敛）。
- `src/views/tasks/index.vue`：初始化读 URL；`syncPaginationQuery`；翻页/改 size/类型/状态/chip/清空均同步；`watch(route.query)` 处理前进后退；越界收敛（silent 单次重取）；模板 page-sizes 用常量。
- `src/layouts/MainLayout.vue`：`router-view :key` 由 `route.fullPath` 改为 `route.path`，query 变化不再销毁页面（QA #8）。

### 验收（真实浏览器 + 真实后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | 深链 `?type=cam:sync_assets&page=2&size=50` | ✅ 类型=同步资产、第 2 页、50 卡、URL 保持 |
| 2 | 点第 3 页 | ✅ URL `page=3&size=50` |
| 3 | 刷新 | ✅ 仍第 3 页/50 条 |
| 4 | chip「失败」 | ✅ 回第 1 页、URL 去 page 并带 status |
| 5 | 刷新 `?status=completed&page=2` | ✅ 状态=已完成、第 2 页 |
| 6 | 非法 `type=bogus` / `size=7` | ✅ URL 清理、回退 20 条 |
| 7 | `?page=9999` | ✅ 收敛末页 353、URL 更新 |
| 8 | A→B→后退 | ✅ 恢复 A 的状态与页码 |
| 9 | 默认 `/cam/tasks` | ✅ URL 无参数 |
| 10 | console | ✅ 0 error |

审查复核：M1（chip 未同步）、M2（筛选未进 URL）与 5 项 minor 全部关闭；补充「status-only 不重刷概览」「total Number 归一」「3 条 URL 测试」。

独立 QA 终验（`551f722`）：**#8 修复有效** —— chip/翻页 summary=0、`.tasks-page` 节点未被销毁；类型变更 summary=5+oldest=1；详情 path 变化正常切换；深链分享、replace 历史、后退行为均 PASS；console 0。

### 测试

- `vitest run`：62 files / 1022 tests 通过（pagination 9 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：首轮 1 major（chip 漏同步）+ 1 major（筛选未进 URL）+ 7 minor；复核确认全部关闭，无阻塞。
- 独立 QA：首轮深链/刷新/chip/后退验证；`484156c` 终验发现 #8（fullPath key 导致重挂载），修复后 `551f722` 复跑全部 PASS。
- 未执行：390 视口下的 URL 深链（分页形态已在 #20 验证）。

### 版本组合

- 前端：`551f722`（合并为 `58357f3`）。
- 后端：`main@1bdd3af`（无接口改动）。

### 后续建议

1. **P3 翻页历史语义**：当前 `router.replace` 不产生历史项，返回键离开页面；若产品希望逐步回退页码需改 `push` 并接受历史膨胀。
2. **P3 查询条件全量同步**：其它页面（资产/日志）筛选仍不写 URL，可复用本模式。
3. **P1 仍待办**：任务生命周期治理（pending 残留执行态、超时回收、持久化去重）、采集 running 超时、菜单死链（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-revert-key（2026-10-28）

**状态**：**已合并**（`e-cam-web#22`，merge commit `148df85`，2026-10-28 09:57 +08）
**代码 PR**：`e-cam-web#22`（commit `f288da0`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 背景与证据

- #21 为消除「query 变化整页重挂载导致的重复概览请求」，把 `MainLayout` 的 `<router-view :key="route.fullPath" />` 改为 `:key="route.path"`。
- 回归：**21 个列表页**（compute/disk、ecs、image、security-group、snapshot；databases/mongodb、rds、redis；middleware/elasticsearch、kafka；network/cdn、ddos、eip、eni、lb、vpc、waf；storage/nas、oss；vswitch 的 `vpc_id`；cert/ledger 的 `daysLeft`；ecs 的 `uid`）在 `onMounted` 读取 `route.query`，依赖「同 path、query 变化 → 重挂载」预填搜索/深链参数。
- 实测（磁盘页 SPA `pushState` + `popstate` 模拟全局搜索跳转）：
  - `route.path`：URL 变为 `?search=probe-123`，**remounted=false、关键字为空**（搜索失效）；
  - 回滚后：**remounted=true、关键字 `probe-123`**（恢复）。

### 需求卡

- 目标用户：使用全局搜索、资源详情「查看资产」等带 `?search/?uid` 跳转的用户。
- 目标：恢复重挂载语义，保证 query 深链被页面读取；任务页 URL 深链/筛选/后退不回退。
- 非目标：消除重复概览请求（重挂载架构的代价，另做页面内 watch query 才能根治）。
- 规则：`router-view :key` 恢复 `route.fullPath`。
- API 契约：无。
- 风险：重复概览（QA #8）现象回归，属已知取舍。

### 变更内容

- `src/layouts/MainLayout.vue`：`:key="route.path"` → `:key="route.fullPath"`（与 #21 之前逐字节一致）。

### 验收（真实浏览器 + 真实后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | 磁盘页同 path 改 `?search=probe-123` | ✅ 重挂载真、关键字预填 |
| 2 | 任务页深链 type+page+size | ✅ 第 2 页/50 卡/类型正确 |
| 3 | chip「失败」 | ✅ 去 page、带 status |
| 4 | `?status=completed&page=2` 刷新 | ✅ 保持 |
| 5 | 非法 type/size | ✅ 清理回退 |
| 6 | A→B→后退 | ✅ 恢复 A |
| 7 | 默认 `/cam/tasks` | ✅ 无参数 |
| 8 | console | ✅ 0 error |

### 已知取舍

- 回滚后，任务页每次写 URL（翻页/筛选）都会重挂载：状态 chip/翻页触发 5 次概览请求，类型切换 10 次；`oldest` 60s 节流被重置；滚动回顶；cert/dashboard 筛选会重跑 `refresh()+loadProbes()`。
- 这是 **既有重挂载架构**的代价，本轮优先修复用户可见的搜索失效；根治方向：为 21 页抽 `useQuerySearchPrefill()`（watch query），或在路由 meta 标记 `remountOnQuery` 折中。

### 测试

- `vitest run`：62 files / 1022 tests 通过。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：Approve（0 blocker）；确认回滚与 #21 之前逐字节一致、21 页清单无遗漏、4 处已响应式页面属多余重挂载；建议补 `?search` 预填回归测试与 follow-up 记录。
- 独立 QA：本轮启动但在合并时仍在运行，未及返回；搜索预填与任务页 URL 流程由总控以真实浏览器（:5194）验证。
- 未执行：21 页逐一搜索预填回归（以磁盘页 + 代码清单推断）。

### 版本组合

- 前端：`f288da0`（合并为 `148df85`）。
- 后端：`main@5ab9499`（无接口改动）。

### 后续建议

1. **P2 概览去重**：tasks 概览 5 个 status 各 limit=1，可换后端聚合端点或短 TTL 缓存。
2. **P2 query 预填 composable**：抽 `useQuerySearchPrefill()`，21 页改为 watch query，之后 key 才能安全改回 path。
3. **P2 回归测试**：补「列表页 mount 带 ?search 预填」用例，锁住本次行为。
4. **P3 空深链**：`AccountCard` 跳 `/iam/users?cloud_account_id=` 但用户页不读 query，属独立问题。
5. **P1 仍待办**：任务生命周期治理、采集 running 超时、菜单死链。

---

## cam-iteration-20261028-dedupe-fetch（2026-10-28）

**状态**：**已合并**（`e-cam-web#23`，merge commit `f303d27`，2026-10-28 12:32 +08）
**代码 PR**：`e-cam-web#23`（commits `522e45b` → `ba10d47`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- QA 在 #21/#22 观测：任务页一次翻页出现 **2 条列表请求**（1 条完全重复）；类型切换 **10 次概览请求**（处理器 5 + 重挂载 5）。
- 根因：`router-view :key="route.fullPath"`（#22 保留）使 URL 变更必然重挂载并由新实例从 URL 取数；处理器在 `router.replace` 后仍主动 fetch。

### 需求卡

- 目标用户：任务页高频翻页/筛选的运维。
- 目标：URL 变化时只由重挂载取数；URL 未变（点已激活「全部」chip 等）仍本地取数。
- 非目标：改变重挂载策略、概览后端聚合、其它页面。
- 规则：`syncPaginationQuery()` 用 `router.resolve({query}).fullPath === route.fullPath` 判等；变化则处理器直接 return，未变才 fetch。
- API 契约：无。
- 风险：逻辑依赖「URL 变化→重挂载」；已在 `MainLayout` key 处加注释固化不变量。

### 变更内容

- `src/views/tasks/index.vue`：`syncPaginationQuery` 返回 boolean；翻页/改 size/类型/状态/chip/清空/越界收敛在 URL 变化时跳过本地 fetch。
- `src/layouts/MainLayout.vue`：router-view key 处补注释（key 必须包含 query）。

### 验收（真实浏览器 + 真实后端）

| 场景 | 修复前 | 修复后 |
|---|---|---|
| 翻页 → 第 2 页 | 列表 2 次 | **列表 1 次、概览 5 次**，20 卡 |
| 改每页 50 | — | 列表 1 次、50 卡、`size=50` |
| 类型切换 | 概览 10 次、列表 2 次 | **概览 5 次、列表 1 次** |
| chip「失败」 | 列表 2 次 | **列表 1 次、概览 5 次**，4 卡 |
| 点已激活「全部」chip（URL 不变） | — | 列表 1 次（验证「未变才 fetch」分支） |
| 越界 `?page=9999` | — | 列表 2 次（越界+收敛）、URL 收敛末页、有数据 |
| 后退 | — | 列表 1 次、恢复上一状态 |
| console | — | 0 error |

> 口径修正：概览真实为 **5 次**（全部+待执行/执行中/已完成/失败）；此前把 `limit=100` 的最老等待请求误计为 `limit=1`，PR 中「12→6」应订正为「10→5」。

### 测试

- `vitest run`：62 files / 1022 tests 通过。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：无 blocker；1 major（刷新完全依赖 fullPath key，注释/测试不足）+ 3 minor（JSON 顺序敏感、replace 竞态、无单测）；已修 JSON → `resolve`、补 MainLayout 注释。
- 独立 QA：7 PASS + 1 条「点当前页」不可达（Element 不发事件，非回归）；确认去重生效、URL 未变分支仍刷新、后退仅 1 次列表。
- 总控：修正统计口径、复跑矩阵（1 列表 + 5 概览）、越界 2 次属预期。

### 版本组合

- 前端：`ba10d47`（合并为 `f303d27`）。
- 后端：`main@5be3746`（无接口改动）。

### 后续建议

1. **P2 watcher 兜底**：把 `watch(route.query)` 改为对比「本实例已加载的 query 快照」，即使 key 改回 path 也不会静默不刷新（审查 M1 建议）。
2. **P2 契约测试**：mount 任务页 + memory router，断言「URL 变→1 次请求、URL 不变→仍请求」。
3. **P2 概览聚合**：5 个 status 各 limit=1 → 后端单端点。
4. **P3 首屏双倍**：深链含默认/非法参数时，规范化 replace 会让首屏多一次挂载。
5. **P1 仍待办**：任务生命周期治理、采集 running 超时、菜单死链。

---

## cam-iteration-20261028-cert-ledger-redirect（2026-10-28）

**状态**：**已合并**（`e-cam-web#24`，merge commit `cc7ce13`，2026-10-28 13:56 +08）
**代码 PR**：`e-cam-web#24`（commit `0626c53`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据（含一次自我纠正）

- 本轮只读健康审计新覆盖 5 页（告警中心/操作审计/变更历史/用户与权限/证书台账），发现 `/cam/certs/ledger` 请求返回 **400 `CERT_INVALID_ID`**、页面「证书详情加载失败」。
- **纠正**：审查用全仓与 20 个 worktree grep 证明仓库内**没有任何菜单指向 `/certs/ledger`**（`cert-nav.ts`/`menu-config.ts` 均为 `/certs`）；该 400 来自历史书签/外部链接，或本轮审计脚本硬编码的 URL。原「菜单死链」结论不成立，PR 描述已改为「旧链接防御性兼容」。
- 真实风险仍存在：`GET /api/v1/certs/ledger` → 400，`/certs/:id` 会把 `ledger` 当证书 id；任意旧书签会落到错误页。

### 需求卡

- 目标用户：持旧书签/外部链接的运维。
- 目标：`/certs/ledger` 落到台账列表，不再请求详情接口；`/certs/:id` 详情与权限守卫不受影响。
- 非目标：修改菜单（无此错误）、通用无效 id 的 404 处理（列为后续）。
- 规则：新增 `{ path: '/certs/ledger', redirect: '/certs' }`，静态段优先于 `/certs/:id`。
- API 契约：无。
- 风险/回滚：单条重定向；测试排除 redirect 不计入「表外页面」。

### 变更内容

- `src/router/routes.ts`：新增 `/certs/ledger → /certs` 重定向。
- `src/router/routes.test.ts`：组合校验排除 redirect，并断言该重定向存在。

### 验收（真实浏览器 + 真实后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | 直接访问 `/cam/certs/ledger` | ✅ 落到 `/cam/certs`，20 行，`/api/v1/certs/ledger` 请求 0 次，4xx 0 |
| 2 | 台账 → 详情 `/certs/:id` | ✅ 详情/引用/变更均 200，reload 稳定 |
| 3 | 看板/变更 | ✅ 正常 |
| 4 | 只读角色（前端模拟） | ✅ 落看板并提示，无 400 |
| 5 | console / 4xx | ✅ 0 / 0 |
| 6 | 390 页面级横向滚动 | ✅ 0（内容区 28px 为既有布局，见下） |
| 7 | replace 语义 | ✅ 不留 `/certs/ledger` 历史项，后退回前页 |

独立 QA：**7/7 PASS**；审查：无 blocker（确认静态段优先、守卫与详情不受影响、redirect 为替换式并保留 query/hash）。

### 已知问题（既有，非本轮）

- 390 下证书台账 `.page-actions` 右边界 418 > 视口 390，内容区横向溢出 28px；直接访问 `/certs` 与重定向后截图 SHA256 相同，属既有布局。
- 任意其它无效 id（如 `/certs/setting`）仍会被当详情并返回 400，建议后续做「详情页 404/不存在」处理。

### 测试

- `vitest run`：62 files / 1022 tests 通过（router 6 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：无 blocker；major 为「描述证据不成立」，已按审查改正为防御性兼容，并据此把结论写入记录。
- 独立 QA：7/7 PASS，含 390、权限模拟、replace、query/hash 保留、400 对照实验。
- 总控：确认审计脚本 URL 写错导致自造证据；保留 redirect（真实旧链接仍有 400 风险）。

### 版本组合

- 前端：`0626c53`（合并为 `cc7ce13`）。
- 后端：`main@f499095`（无接口改动）。

### 后续建议

1. **P2 详情页无效 id**：把后端 `CERT_INVALID_ID`（400）映射为「证书不存在」页并提供「返回台账」，覆盖任意脏链接。
2. **P2 审计脚本修正**：`cam-audit-20261028-health/audit.cjs` 中 `/cam/certs/ledger` 改为 `/cam/certs`，避免再次自造证据。
3. **P3 台账 390 头部按钮**：`.page-actions` 不换行导致 28px 溢出。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、菜单死链（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-cert-notfound（2026-10-28）

**状态**：**已合并**（`e-cam-web#25`，merge commit `df0437c`，2026-10-28 13:57 +08）
**代码 PR**：`e-cam-web#25`（commits `c999618` → `e6f99a5`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 本轮健康审计与上一轮重定向验证共同确认：`/certs/:id` 对无效 id 返回 400 `CERT_INVALID_ID`（1b：合法格式但不存在的 id 返回 404 `CERT_NOT_FOUND`），页面只显示「证书详情加载失败 + 重试」，无返回出口。
- 上一轮的 `/certs/ledger` 只是其中一个特例；任意脏链接（旧书签、手误 id、看板中已删除证书）都会命中。

### 需求卡

- 目标用户：持旧链接、或从看板点进已删除证书的用户（含只读角色）。
- 目标：无效/不存在 id 显示「证书不存在」并提供返回出口；服务端错误仍为「加载失败 + 重试」；有效 id 正常。
- 规则：按 `CertRequestError.code` 区分 `CERT_INVALID_ID` / `CERT_NOT_FOUND`；只读角色返回 `/certs/dashboard`，管理角色返回 `/certs`。
- 非目标：后端把 400 映射 404、台账筛选保持（见后续）。
- API 契约：无（复用现有错误码，后端已定义）。

### 变更内容

- `src/views/cert/detail/index.vue`：新增「证书不存在」态与 CTA；按 readonly 分流；`loadDetail` 改为请求令牌（旧响应丢弃）；in-flight 布尔守卫移除。
- `src/views/cert/detail/format.ts`：新增 `isCertNotFoundCode` 纯函数。
- `src/views/cert/detail/format.test.ts`：+2 例。

### 验收（真实浏览器 + 真实后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | 无效 id `/certs/not-a-valid-id` | ✅「证书不存在」、无「加载失败」、CTA 可见、仅 1 次 400 |
| 1b | 合法格式不存在 id（24 个 0） | ✅ 404 `CERT_NOT_FOUND` → 同样态、1 次请求 |
| 2 | 点 CTA（管理角色） | ✅ `/cam/certs`、20 行 |
| 3 | 有效 id 详情 | ✅ h1=证书 CN、detail/references 各 1 次 200 |
| 4 | `/certs/ledger` | ✅ 仍走既有重定向到 `/certs`，0 次 ledger 请求 |
| 5 | 模拟 500 | ✅「加载失败 + 重试」，不误判为不存在 |
| 6 | 390×844 | ✅ 无溢出，CTA 在视口内 |
| 7 | console | ✅ 应用错误 0、pageerror 0；无效 id 6s 后仍 1 次请求（无重试风暴） |
| 8 | **只读角色 CTA** | ✅「返回到期看板」→ `/certs/dashboard`，无「无权限」toast（对照：直连 `/certs` 仍被拦） |
| 9 | **同组件 A→B 切换** | ✅ 标题/ID/有效期/指纹全部换新，B 仅 1 次请求；B→无效正确进入不存在态 |

独立 QA：**10/10 PASS**（快照 `e6f99a5`）。审查：2 major（只读 CTA 死路、布尔守卫吞参数切换）均已修复。

### 已知遗留（非本轮）

- 从详情返回台账不保留筛选：`goLedger()` 固定 `/certs`，`?daysLeft=le30` 场景返回后变全量 20 行；且 URL 带 `daysLeft` 时筛选控件仍显示「请选择」（既有）。
- 390 顶部外壳被 `overflow-x:hidden` 裁剪（全站既有）。
- 只读角色仅以前端 profile 改写模拟，后端 viewer 鉴权未实测。

### 测试

- `vitest run`：62 files / 1024 tests 通过（format 2 例新增）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：确认错误码真实（后端 `CERT_INVALID_ID`/`CERT_NOT_FOUND`）、状态互斥正确；提出只读 CTA 与 in-flight 布尔两个 major，均已修复并自测。
- 独立 QA：`e6f99a5` 10/10 PASS，含 M1 对照实验（直连 `/certs` 仍被拦）、M2 同组件切换、500 与不存在区分、390。
- 总控：复跑无效/有效 id、1 次 400、令牌守卫、全量测试。

### 版本组合

- 前端：`e6f99a5`（合并为 `df0437c`）。
- 后端：`main@54fae75`（错误码既有，无接口改动）。

### 后续建议

1. **P3 返回保留台账筛选**：`goLedger()` 回退到进入详情前的台账 query（需记录来源 query）。
2. **P3 台账 URL 筛选控件不同步**：`?daysLeft=le30` 时控件显示「请选择」。
3. **P2 后端错误码统一**：无效 id 可由后端返回 404 语义（或前端把 400/404 统一为不存在），减少前端码值依赖。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-cert-back（2026-10-28）

**状态**：**已合并**（`e-cam-web#26`，merge commit `efee595`，2026-10-28 15:12 +08）
**代码 PR**：`e-cam-web#26`（commits `32e67ee` → `cc9f711`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮 QA 观察：从筛选台账（`?daysLeft=le30`，14 行）进入证书详情后，面包屑与「证书不存在」CTA 都固定 `push('/certs')`，返回变全量 20 行，筛选上下文丢失。
- 实测确认：筛选态 14 行 → 详情 → 面包屑 → `/certs`（20 行）。

### 需求卡

- 目标用户：带筛选查看台账后进入详情的运维。
- 目标：从台账/看板进入详情后，返回时保留筛选 query；直接打开详情（无来源）回落全量台账；只读角色回落看板。
- 非目标：台账 URL 与筛选控件同步（独立问题）、跨 tab 历史合并。
- 规则：`history.state.back` 为台账/看板 → `router.back()`；否则按角色 push；面包屑/CTA 共用逻辑。
- API 契约：无。
- 风险：依赖 `history.state.back`（fullPath、不含 base，vue-router 4 行为）；单提交回滚。

### 变更内容

- `src/views/cert/detail/format.ts`：`isCertListBackTarget`（容错尾斜杠、hash）。
- `src/views/cert/detail/index.vue`：`goLedger()` 回退逻辑；面包屑由 `router-link` 改为原生 `<a :href>` + `onLedgerClick`（修饰键/中键保持新标签，普通点击 preventDefault 后只走 goLedger）；只读仅接受看板回退目标。
- `format.test.ts`：+2 例及边界（`/certs/`、`/certs/?x`、`/certs#top`、`/certs/changes/new`、`/certs/probes`、null）。

### 验收（真实浏览器 + 真实后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | 筛选台账 14 行 → 详情 → 面包屑 | ✅ 仍 14 行、URL 保留；`history.length 6→6`（无额外条目） |
| 2 | 同流程浏览器后退 | ✅ 14→14 行、URL 保留 |
| 3 | 直接打开有效详情 → 面包屑 | ✅ 回落 `/certs` 20 行（history 2→3，push 一次，预期） |
| 4 | 直接打开无效详情 → CTA | ✅ `/certs` 20 行 |
| 5 | 筛选台账进入无效 id → CTA | ✅ 回 `?daysLeft=le30`、14 行（两种入口） |
| 6 | 390×844 | ✅ 三条关键路径正常，文档级无横向滚动 |
| 7 | console | ✅ 应用 error 0、pageerror 0（仅网络级 400/404 桩） |
| 8 | 面包屑可访问性 | ✅ 真 `<a href>`、tabIndex=0、focus ring、Enter 生效、中键新标签打开 `/certs` |
| 9 | **单击只一次导航** | ✅ push=0 / pop=1 / replace=0、导航次数=1（修掉 RouterLink 双跳） |

独立 QA：**8/8 PASS**（快照 `cc9f711`，含 3 轮对比：`32e67ee`、中间态、`cc9f711`）。审查：1 blocker（RouterLink 双跳）+ 2 major（接线无测试、谓词边界）均已修复。

### 过程中的环境事件

- 14:46–15:42 本地后端 `e-cam-service:8001` 宕机（非本轮改动导致，进程消失），导致 QA 与总控验收中断；15:42 用 `dev.ps1 restart e-cam-service` 恢复后完成复跑。QA 报告明确标注了这一窗口。

### 测试

- `vitest run`：62 files / 1026 tests 通过（format 新增 2 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：发现 **blocker**（`router-link` 自带 navigate 与 `@click.prevent` 的 `back()` 叠加 → push+back 双跳）、谓词漏尾斜杠/hash、接线零测试；全部修复并补边界测试。
- 独立 QA：`cc9f711` 8/8 PASS，专项验证「面包屑单击只一次导航」（history.length 不变、push=0/pop=1）。
- 总控：复跑筛选/直接打开/无效 CTA，确认单次导航与筛选保留。

### 版本组合

- 前端：`cc9f711`（合并为 `efee595`）。
- 后端：`main@c7f4276`（无接口改动；本地服务期间重启过一次）。

### 后续建议

1. **P3 台账筛选控件与 URL 不同步**：`?daysLeft=le30` 时控件仍显示「请选择」。
2. **P3 台账 390 头部**：`.page-actions` 不换行导致 28px 内容区溢出（全量台账同样存在）。
3. **P2 组件级测试**：`goLedger`/面包屑接线目前仅浏览器验证，建议按仓库既有 mount 范式补一条（可捕获本次 blocker 类问题）。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-server-window-limit（服务端强制 max_window_days,超窗 400）

**需求来源**：口径确认(2026-10-28 维护者答"超窗 400 拒绝,S2S 不豁免")。此前实测 cdn 30d 窗口 200 通过 —— 窗口约束仅前端 clampWindow 单侧钳制。

**改动**：e-cam-service#39 —— 四入口(search/aggregate/diagnose/cache-analyze)统一 checkWindow,超 /types 字典 max_window_days 一律 400 + 结构化拒因日志;S2S 路由复用同 handler 同样生效;恰等于上限放行(与前端钳制语义一致)。

**验收**：本地 `go test ./internal/logquery/...` 全绿(含新增超窗 2 例精确消息 + 边界 2 例:恰 7d 放行走通完整链路/超 1ms 拒绝)、vet/gofmt 干净。**边界**：运行实例未含本改动,live 验证待实例重建后核查(参照 #11 模式)。

**配套说明**：8888 主检出拉取被安全阻断 —— 并行工作区 24 个未提交文件中 2 个与 incoming main 重叠(AppSidebar.vue/CommandPalette.test.ts),不覆盖他人进行中的修改,保持等待。

---

## cam-iteration-20261028-cert-actions-wrap（2026-10-28）

**状态**：**已合并**（`e-cam-web#27`，merge commit `d959087`，2026-10-28 16:18 +08）
**代码 PR**：`e-cam-web#27`（commit `dba1f1c`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- QA 两轮量化：390×844 下证书台账 `.page-actions` 三个导入按钮不换行，右边界 418 > 视口 390，`.main-content` 横向溢出 **28px**、「导入证书」被裁（直接访问与重定向后截图 SHA256 相同，属既有布局问题）。
- 本轮 A/B 实测（运行时移除/恢复新增样式）：390 **28→0**、360 **58→0**、320 **98→0**；`.table-pagination` 全程无溢出（252/252），确认单一越界源为 `.page-actions`。

### 需求卡

- 目标用户：窄屏查看证书台账的运维。
- 目标：头部 3 个导入按钮在窄屏换行、不撑破页面、全部可见可点；宽屏布局不变。
- 非目标：改成下拉/图标化（产品取舍）、其它页面同类头部、表格列宽问题。
- 规则：`.page-actions` 增加 `flex-wrap: wrap; justify-content: flex-end`。
- API 契约：无。
- 风险/回滚：仅本页 scoped 样式；1440/768 零影响；单提交回滚。

### 变更内容

- `src/views/cert/ledger/index.vue`：`.page-actions` +3 行（换行与右对齐 + 注释）。

### 验收（真实浏览器 + 真实后端）

| 视口 | 修复前 main 溢出 | 修复后 | 说明 |
|---|---|---|---|
| 390 | 28px | **0** | 3 按钮两行、全部在视口内、可点击 |
| 360 | 58px | **0** | 同上（QA 额外验证） |
| 320 | 98px | **0** | 3 行仍全部可见 |
| 768 | 0 | 0 | 单行，右缘对齐 header |
| 1440 | 0 | 0 | 像素级零变化（反事实 rect 一致） |

- 「导入证书」对话框可正常打开/取消（未提交）。
- 390 表格横向滚动仍在表格容器内（1150/252），不撑破页面；console 应用错误 0。

独立 QA：**7/7 PASS**，并用「页面内临时改回修复前样式」复现 28px 对照。审查：无 blocker，建议合入。

### 已知遗留（既有，非本轮）

- 390 表格 `hide-sm` 隐藏列仍占 colgroup 宽，横滚末端约 410px 空档、sticky「操作」列在最右滚动时移出、中途与「托管状态」叠字（`CertTable.vue` 最后改动 2026-09-30）。
- 390 `.navbar` 被 `.main-area{overflow:hidden}` 裁切（右上「平台导…」被截），全站外壳既有。
- `row-gap` 仍为 8px，换行后行距略紧（审查 minor，未改）。

### 测试

- `vitest run`：62 files / 1026 tests 通过。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：无 blocker；提出「28px 与静态推算 ~8px 不符」要求实测，并用 A/B 补齐证据；minor（row-gap、右对齐取舍、各页重复）已记录。
- 独立 QA：7/7 PASS，含反事实复现、320/360/768/1440、对话框开合、表格横滚。
- 总控：A/B 实测三档溢出归零。

### 版本组合

- 前端：`dba1f1c`（合并为 `d959087`）。
- 后端：`main@787a808`（无接口改动）。

### 后续建议

1. **P3 表格隐藏列仍占宽**：`hide-sm` 与 colgroup 不同步，建议另开需求。
2. **P3 窄屏头部统一**：`.page-actions` 各页各自定义，建议抽公共头部组件并内置 wrap。
3. **P3 行距**：`row-gap: 12px` 或容器内归一 `.el-button + .el-button` 的 12px margin。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-cert-cols-responsive（2026-10-28）

**状态**：**已合并**（`e-cam-web#29`，merge commit `6f60368`，2026-10-28 17:18 +08）
**代码 PR**：`e-cam-web#29`（commits `bcd9c99` → `035250a` → `6a49f3f`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- QA 在 #27 验收中量化：390 下证书台账用 `.hide-sm` **仅 CSS 隐藏**列，`el-table` 的 colgroup 仍按 8 列占宽——表格横滚 **1150px**、末端约 **410px 空白**，sticky「操作」列在最右滚动时移出、中途与「托管状态」叠字。
- 复验进一步发现：sticky 操作列 200px 在 390 占表宽 **79%**，覆盖域名列（仅剩 52px），且固定列背景透明导致表头渲染成「域名 / S操作」。

### 需求卡

- 目标用户：窄屏查看证书台账的运维。
- 目标：窄屏真正移除次要列（释放 colgroup 宽度）、操作列不覆盖内容、固定列不透明；宽屏列齐全。
- 非目标：其它表格组件同类问题、操作列改图标化、1440 内部 28px 横滚。
- 规则：`matchMedia('(max-width:768px)')` + `v-if` 移除签发者/有效期至/引用数；`fixed` 仅 ≥1024 启用；固定列背景 `--bg-surface`，hover 补不透明 `#212223`。
- API 契约：无。
- 风险/回滚：仅本页表格组件；1440/769 列数不变；单提交回滚。

### 变更内容

- `src/views/cert/ledger/components/CertTable.vue`：
  - `v-if` 真正移除三列（替代 `.hide-sm`），删除失效样式与空 `@media`；
  - `fixed="right"` 仅 ≥1024（`stickyActions`）；
  - 固定列背景 `--bg-surface`（与行一致）+ hover 不透明 `#212223`。

### 验收（真实浏览器 + 真实后端）

| 视口 | 列数 | 表格 scrollWidth | 操作列 | 页面溢出 |
|---|---|---|---|---|
| 390 | **8→5** | **1150→840**（−310=120+110+80） | relative（不覆盖） | 0 |
| 768 | 5 | 840 | relative | 0 |
| 769 | 8 | 1150 | relative | 0 |
| 1023 | 8 | 1150 | relative | 0 |
| 1024 | 8 | 1150 | sticky，bg rgb(25,26,27) α=1 | 0 |
| 1440 | 8 | 1150 | sticky，α=1 | 0 |

- 390 最右：`scrollLeft=588=max`、操作列右缘 353 < 390、末端空白 0；
- 1024 hover：固定列 td `rgb(33,34,35)` α=1，红色徽章区域像素 **0/2600**（基线 403/2600 证明检测有效）；
- 固定列与行底色一致（Δ=1，竖带消失）；console 0。

独立 QA：**3 轮**（发现 F1 叠字 → 修复；F8 hover 透字 → 修复；F9 竖带 → 修复），最终 F10（表头 4% 色差）为低、记录。

### 已知遗留（既有/低）

- **F10**：表头固定列缺少 header 的 5% 白叠加，Δ≈4%，3× 放大才可辨。
- **F4**：1440 表格仍有 28px 内部横滚（列宽总和 1150 > 1122）。
- **F5**：「✗ 已过期 920 天」徽章 108px > 单元格 96px，所有视口右侧被裁。
- 其它 4 个表格组件（Dashboard/Change/ReportCards/Step5Progress）仍用 1023 的 `.hide-sm`，同带 colgroup 占宽问题。

### 测试

- `vitest run`：62 files / 1026 tests 通过。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：Approve（0 blocker）；确认 v-if 动态列在 EP 2.11.7 安全（insert/remove 列、sticky 偏移重算）、`matchMedia` 仅跨断点触发；minor：首帧先 8 列后 5 列、断点与兄弟表不一致、无测试锁定。
- 独立 QA：3 轮，最终 pass；用像素计数验证 hover 无透字（0 vs 403 基线），并用列宽差值 (−310) 证明 colgroup 释放。
- 总控：实现 + 两轮修复（sticky 门控、hover 不透明），复跑矩阵与像素断言。

### 版本组合

- 前端：`6a49f3f`（合并为 `6f60368`）。
- 后端：`main@b705768`（无接口改动）。

### 后续建议

1. **P2 其它表格统一**：`DashboardTable`/`ChangeTable`/`ReportCards`/`Step5Progress` 的 1023 `.hide-sm` 迁移为 `v-if` + 统一 `useNarrowViewport()`。
2. **P3 表头色差 F10**：给 sticky `th` 叠 header 背景色。
3. **P3 列宽 F4/F5**：1440 列宽总和 1150 超出 1122；过期徽章 108 > 96。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-app-kpis(app 页专属 KPI,RCA 语义)

**需求来源**:口径确认(2026-10-28 维护者选定"条数+错误占比+涉及服务数+exception 计数")。此前 app 页误用 cdnKpis 显示缓存命中率。

**改动**:e-cam-web#28 —— buildKpis 增加 app 分支:条数(全窗精确)/错误占比(level=error 采样,≥5% danger)/涉及服务数(去重 service)/exception 条数(非空堆栈)。

**验收**:vue-tsc 0/vitest 142/142/eslint 0;浏览器 UI(真实链路,worktree :3333+后端 :8888):t=app 联邦检索完成后 DOM 结构化断言四标签齐且配对正确(日志条数 3,000/错误占比 11% tone-danger 实数据触发/服务数 42/exception 33),旧 CDN 指标消失,0 页面错误。数值随窗口波动(两轮 17/5.6%/9 与 3,000/11%/42/33,语义一致)。

**配套**:同日交付 e-cam-service#39(服务端超窗 400)+#40(记录);e-cam-web#30(账号级筛选)与本切片同为日志域口径确认需求。

**环境备注**:当日 GitHub api.github.com TLS 握手持续被阻断(SNI 层),改用 curl + --resolve 备用 IP 完成 PR 创建/合并;github.com git 通道正常。

---

## cam-iteration-20261028-account-filter(聚合/诊断补账号级筛选 account_ids)

**需求来源**:口径确认(2026-10-28 维护者答"做,复用现有筛选模式补账号维度")。`account_ids` 后端四入口(search/aggregate/diagnose/cache-analyze)早已支持,前端从未下发。

**改动**:e-cam-web#30 —— useLogSources 新增 accountOptions(源清单去重推导,number 平台 ID,标签 云·账号,CLOUD_ORDER 组序);useLogSearch 的 search/aggregate/翻页请求统一携带 account_ids;工具栏新增账号多选;diagnoseContext 同步(诊断/缓存分析卡生效);watch+onCloudsChange 清理失效选择;类型补 LogAggregateRequest/LogDiagnoseRequest.account_ids。

**验收**:vue-tsc 0 / vitest 150/150(新增 6 例)/ eslint 0;CI 四项全绿后合并。浏览器 UI(真实链路 A 类,:3333+后端 :8888,1440×900):真实源清单 809 源去重为 2 账号(去重语义正确);单选 → search 与 aggregate 均带 account_ids:[1];云扩至 aliyun+volcengine → 账号联动 2 项,全选后 account_ids:[5,1];0 页面错误。

**未执行(如实)**:app 数据仅 2 账号,无法构造 3+ 账号真实验证;账号选择未做 URL 持久化(口径外)。

**环境备注**:api.github.com TLS 握手被持续阻断,改用 curl --resolve 备用 IP 完成 PR 建/合并。

---

## cam-iteration-20261028-cert-tables-narrow（2026-10-28）

**状态**：**已合并**（`e-cam-web#31`，merge commit `e19f250`，2026-10-28 18:03 +08）
**代码 PR**：`e-cam-web#31`（commits `9e06d7f` → `011578f`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- #29 修复台账后，审查指出另有 4 个证书表格（`DashboardTable`/`ChangeTable`/`ReportCards`/`Step5Progress`）仍用 1023 的 `.hide-sm` **仅 CSS 隐藏**，`el-table` colgroup 仍占宽，窄屏出现末端空白与横滚范围过大——与台账同类问题。
- 实测基线：看板/变更 1440 内宽 1122px，1023 下列仍在 DOM、宽度不释放。

### 需求卡

- 目标用户：窄屏查看到期看板、变更管理、变更报告的用户。
- 目标：≤1023 真正移除次级列（释放 colgroup），≥1024 列齐全；断点切换无闪烁。
- 非目标：台账断点（768）统一、回滚表错误列策略、Step5 不可达状态的强测。
- 规则：抽 `useMediaQuery`（同步初值、跨断点触发、卸载清理、旧浏览器 `addListener` 兜底），4 个组件改 `v-if="!isNarrow"` 并删除 `.hide-sm`。
- API 契约：无。
- 风险：动态列增删依赖 EP 的 insert/removeColumn（已验证）；单提交回滚。

### 变更内容

- `src/composables/useMediaQuery.ts`（新增）+ `useMediaQuery.test.ts`（2 例）。
- `DashboardTable.vue`：托管类型/签发者/豁免 → `v-if`。
- `ChangeTable.vue`：进度/发起人/时间 → `v-if`。
- `ReportCards.vue`：原证书 ID/错误信息/批次 → `v-if`；回滚表的错误列保留并加注释（失败线索）。
- `Step5Progress.vue`：错误信息 → `v-if`。

### 验收（真实浏览器 + 真实后端）

| 页面 | 1440 | 1023 | 1024 | 内宽变化 |
|---|---|---|---|---|
| 到期看板 | 7 列 | **4 列** | 7 列 | **1122→705**（−417） |
| 变更管理 | 7 列 | **4 列** | 7 列 | **1122→705** |
| 变更报告（直达 id） | 13 列 | **10 列**（无「错误信息」） | 恢复 | plan 810→670 |
| 步骤 5 进度 | — | **无法验证** | — | 需 executing 状态，不可达 |

- 动态 resize 1440↔1023：列 7↔4、行数 88 不变、CLS=0（0 条 entry）；
- **M1 同步初值验证**：1023 冷加载 rAF 采样只有 4 列（无 7→4 翻转）；
- 看板点证书抽屉、变更点报告均正常；页面溢出 0；console error/pageerror/5xx/失败 0。

独立 QA：**31 PASS / 0 FAIL / 1 无法验证**（Step5 需 `POST /confirm`+`/execute`，按约束未触发）。审查：0 blocker/0 major，M1/M4/M6 已修。

### 未验证

- **Step5Progress 的窄屏列**：当前无 executing 状态变更单，stepbar 5/6/7 disabled；静态代码与其它三表同机制。
- **旧 Safari addListener 分支**：Chromium 走新 API，仅代码审阅。
- 1024 恢复全列后表格内部横滚（内容 960/1090 vs 容器 706）属必然结果，页面级仍 0。

### 测试

- `vitest run`：63 files / 1028 tests 通过（composable 2 例）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：无 blocker/major；确认 EP 动态列安全（4 表无 fixed/sortable/colspan 坑）、`readonly` 解包正常；提出 M1 双布局、M4 旧浏览器兜底、M5 组件级测试缺口、M6 卡3 策略需确认。
- 独立 QA：31 PASS，含 resize CLS=0、M1 rAF 采样、报告页三表、抽屉/详情导航。
- 总控：实现 + M1/M4/M6 修复，复跑矩阵。

### 版本组合

- 前端：`011578f`（合并为 `e19f250`）。
- 后端：`main@5b81978`（无接口改动）。

### 后续建议

1. **P3 统一断点**：台账 768 与四表 1023 两套；建议把 `CertTable` 切到 `useMediaQuery` 并导出 `NARROW_QUERY` 常量。
2. **P3 组件级测试（M5）**：补 DashboardTable 窄屏断言（stub matchMedia → 表头 7→4），锁住列接线。
3. **P3 窄屏错误信息可达性（M3）**：报告/步骤 5 的「错误信息」窄屏移除后失败原因不可达，建议折入「资源」单元格或 tooltip。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## 2026-10-28 部署验证轮:服务端超窗 400(#39)真机确认

**背景**:#39 合并时运行实例未含该改动,live 验证留待实例重建。

**本轮状态**:service 检出(f19688a,main)已含 #39 的 squash 提交 99b90c4;后端进程 18:20:53 重启(晚于合并时刻),具备验证条件。

**验证方法(零真实云调用)**:候选云取不存在的 `nonexistent-cloud-xyz` —— 守卫生效则立即 400;若未生效则为空结果 200,两种结果都不会触达真实云源。

**结果(A 类:真实后端 :8001)**:
| 用例 | 结果 |
|---|---|
| cdn 30d 窗口 | **400** `time window 2592000000ms exceeds max_window_days 7 for cdn` ✓ |
| cdn 1h 窗口(对照,同候选云) | 200 `ok` ✓(证 400 归因于窗口而非云) |
| cdn 恰 7d(边界) | 200 `ok` ✓(与前端钳制 `end-start <= max` 语义一致) |
| 用户路由无凭据 | 401 `认证失败：会话无效或已过期`(鉴权链完好) |

**服务端可观测性**:拒因日志落盘完整 —— `logquery request rejected` WARN,字段 `stage=validate / path=/api/v1/cam/logs/search / tenant_id=3 / client_ip / reason=time window 2592000000ms exceeds max_window_days 7 for cdn`。

**S2S 路由(如实:未 live 验证)**:代码证据 `ioc/gin.go:307` 显示 `/api/s2s/logs` 复用同一 `logQueryModule.RegisterRoutes`(仅外层换 S2SAuth),守卫按设计同样生效;但本机无 eiam 签发的 S2S 服务令牌,直连 8001 得 401 `缺少服务令牌`,故 **S2S 路径未做 live 断言,仅代码级覆盖**(手工审核重点)。

**探测陷阱(记录备查)**:经前端 dev server(:8888)请求 `/api/s2s/...` 返回 **200 + text/html**(vite 未代理该前缀,回退 index.html),不可用于 API 断言;API 探测须直连后端端口并校验 content-type。

**环境观察**::8888 前端实例运行的是并行会话分支 `fix/logs-k8s-tab-ux`(其 WIP 提交),非 main —— 我合并的前端改动(#28/#30)不在该实例服务范围内;我的前端验收一直在自有 worktree 实例(:3333)+ 同源真实后端上完成(A 类)。

---

## cam-iteration-20261028-search-prefill（2026-10-28）

**状态**：**已合并**（`e-cam-web#32`，merge commit `3d12837`，2026-10-28 19:03 +08）
**代码 PR**：`e-cam-web#32`（commits `b9602bc` → `e208495`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 早前 QA 观察：全局搜索/详情「查看资产」带 `?search=` 跳转时，仅 7 个列表页把关键字回显到输入框；其余页面只把值写进 `filters.keyword` 过滤数据，输入框为空。
- 本轮复测（RDS）：`/cam/databases/rds?search=zz-probe` 列表请求带 `name=zz-probe`、返回 0 行，但搜索框为空——用户看不到当前过滤条件。
- 审查进一步发现 `compute/ecs` 同根因但机制不同（搜索框在 `InstanceSearchBar` 子组件内，页面层无法直接写 input）。

### 需求卡

- 目标用户：从全局搜索、资源详情跳转到列表页的运维。
- 目标：深链后页面上能看到当前关键字（输入框或条件 chip）；无 query 时保持为空；过滤行为不变。
- 非目标：`asset_id` 与 `name` 的语义统一（见后续）、抽公共 composable、其它模块。
- 规则：8 页在 onMounted 预填 `filters.keyword` 时同步 `searchKeyword.value`；ecs 向 `searchConditions` 注入 `asset_name` 条件。
- API 契约：无。
- 风险：`searchKeyword` 在回车后会被清空（既有语义），回显后仅作可见提示；单提交回滚。

### 变更内容

- 8 个列表页（compute/security-group、databases/{mongodb,rds,redis}、middleware/{elasticsearch,kafka}、storage/{nas,oss}）：`searchKeyword.value = s`。
- `compute/ecs/index.vue`：`searchConditions.value.push({ field:'asset_name', value:s, displayValue:s })`。

### 验收（真实浏览器 + 真实后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | 8 页 `?search=zz-probe` | ✅ 输入框 value=zz-probe，行数 0、请求带 `name=zz-probe` |
| 2 | 8 页无 query | ✅ 输入框为空、行数 20、总数正常（RDS 50 / Redis 327 / MongoDB 53 / 安全组 936 / Kafka 21 / ES 37 / NAS 95 / OSS 896） |
| 3 | 过滤真实生效 | ✅ 每页恰 1 次 `name=` 请求；行数 0 vs 20 |
| 4 | RDS 手输回车 | ✅ `ve-jlc` → 20→2 行，输入框按既有逻辑清空 |
| 5 | disk 对照页 | ✅ 回显正常（既有实现未回归） |
| 6 | ecs 深链 | ✅ chip「名称: zz-probe-ecs」、0 行；无 query chips=[] |
| 7 | console / 溢出 | ✅ error 0、pageerror 0、API 4xx/5xx 0；1440/390 页面级溢出 0 |
| 8 | 回显额外请求 | ✅ 带/不带 search 均恰 1 次列表请求 |

独立 QA：**31/31 PASS**（快照 `e208495`）。审查：approve，无 blocker；M1（ecs 漏修）已在本轮修复。

### 已知遗留（既有/范围外）

- **`asset_id` vs `name` 语义**：全局搜索传的是 `asset_id`（CommandPalette），8 页把 keyword 映射到后端 `name`；回显后输入框会显示 ID，需后端确认 `name` 能否匹配 ID（或与 vswitch 一样用 `asset_id` 条件）。
- **`searchKeyword` 是待提交缓冲**：回车后清空；回显后 chips 为空，改动任一条件会让列表回全量而输入框仍留旧文本（既有 7 页同取舍）。
- **15 处重复块**：建议抽 `useSearchQueryPrefill()`，但因存在三种形态（keyword/name/condition）未在本轮做。

### 测试

- `vitest run`：64 files / 1040 tests 通过。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：approve；M1（ecs 同根因）已修；minor：ID/名称语义、缓冲语义、composable 建议。
- 独立 QA：31/31 PASS，含 8 页回显/无 query/过滤生效/手输不回归/ecs chip/请求计数。
- 总控：实现 + ecs 补修，浏览器复验 chip 与 0 行。

### 版本组合

- 前端：`e208495`（合并为 `3d12837`）。
- 后端：`main@2e87ac4`（无接口改动）。

### 后续建议

1. **P2 `asset_id` 深链语义**：与后端确认 `name` 是否支持 ID 精确匹配；否则改为 `asset_id` 条件（同 vswitch）。
2. **P3 抽 `useSearchQueryPrefill()`**：统一 15 处重复块，减少漂移（本轮 ecs 就是漂移产物）。
3. **P3 源码级防漂移断言**：仿 `MainLayout.test.ts` 的 grep 断言，要求读取 `route.query.search` 的页面必须同时设置显示态。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## 2026-10-28 验证轮:账号筛选在缓存分析/诊断卡的下发(#30 验证缺口闭环)

**背景**:#30 的 UI 验证当时只断言了 search/aggregate 载荷;诊断卡与缓存分析卡走同一 `diagnoseContext` computed,属代码级覆盖,未做真机断言。

**验证(A 类:自有 worktree :3333 + 真实后端 :8001,:1440×900)**:
| 路径 | 结果 |
|---|---|
| CDN Tab 选账号 → search | `account_ids:[1]` ✓ |
| CDN Tab 选账号 → **缓存分析** | `account_ids:[1]` ✓(与 search 同值;载荷键 `log_type/start_time/end_time/clouds/account_ids`) |
| WAF Tab(诊断卡) | **不适用** —— 该环境 WAF 无日志源(账号选项 0),无法选账号;诊断请求已按预期发出(`account_ids` 缺省即不发送),但 WAF 路径**未取得 live 断言**,仅同源 computed 代码级覆盖 |

**结论**:#30 的"聚合/诊断请求补账号级筛选"在可验证路径(CDN 缓存分析)已闭环;WAF 诊断路径列为人工审核重点(环境无 WAF 源)。

**附:并行会话协同核查** —— 核对我已合并改动在当前 main(386dbc3)上完好(appKpis/accountOptions/selectedAccounts/filter-accounts 均在),未被并行会话的 stats/url-state 改动覆盖;k8s-event 的 KPI 专项已由并行会话(#34)补齐,与本轮无重叠。

---

## cam-iteration-20261028-search-prefill-guard（2026-10-28）

**状态**：**已合并**（`e-cam-web#35`，merge commit `d3cf094`，2026-10-28 22:47 +08）
**代码 PR**：`e-cam-web#35`（commits `79b4f30` → `bb1bbd6`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮（#32）修复「深链 `?search` 不回显」时暴露出**同一 bug 漂移了三次**：7 页先修、8 页后补、ecs 再漏。审查建议加源码级守卫。
- 独立审查复核：`src/views` 共 208 个 `.vue`，其中 **20 个**读取 `route.query.search`；读取处到最近可见写入的距离 59–135 字符。
- 审查用 `git show <commit>^:<path>` **重放历史版本**：`13cb13c^`（6 页）、`b9602bc^`（8 页）、`e208495^`（ecs）喂给该测试全部变红并列出文件名——证明守卫能抓住三次真实漂移。

### 需求卡

- 目标：任何读取 `?search` 的页面，必须在读取点附近把关键字写入用户可见位置（输入框或条件 chip），否则 CI 失败。
- 非目标：AST 语义分析、校验 chip 内容是否搜索条件、覆盖模板 first 绑定的死模式。
- 规则：遍历**每个**读取点；500 字符窗口；`MIN_READERS=20` 下限；失败输出 `文件:行号`。

### 变更内容

- 新增 `src/views/__tests__/searchPrefill.contract.test.ts`（+73/-0，纯测试）。
- 覆盖 3 种可见态写法：`searchKeyword.value = <var>`、`searchConditions.value.push`、`conditions.value.push`。

### 验收

| # | 场景 | 结果 |
|---|---|---|
| 1 | 干净树 | ✅ 1 passed（20 readers 全部有回显） |
| 2 | 变异：删除 rds 的 `searchKeyword.value = s` | ✅ 失败并列出 `databases/rds/index.vue` |
| 3 | 变异：追加一个无回显的 `watch(() => route.query.search)` | ✅ 失败并列出 `文件:行号`（第一版只看首个出现点会漏） |
| 4 | 历史漂移重放（审查独立执行） | ✅ `13cb13c^`/`b9602bc^`/`e208495^` 全红并列出文件 |
| 5 | 浏览器回归（独立 QA） | ✅ **6/6 PASS**：RDS `?search=ve-jlc` 2 行、ECS `?search=<asset_id>` chip 1 行、无 query 空输入、OSS 0 行、console 0、1440/390 页面溢出 0 |

### 已知局限

- 500 字符是硬预算：若在读取与赋值之间插入 >500 字符注释会假红（当前余量 3.7x）。
- 纯文本匹配：`= typeof s === 'string' ? s : ''`、`= null` 会通过；`push` 不校验是否为搜索条件。
- 不校验 ref 是否真的绑到模板（`v-model`）；`v-model="filters.keyword"` 模式已删除（本仓库 template-first，窗口取不到模板）。
- 未上 AST（`typescript` 已是 devDep，约 70–90 行可覆盖更多语义，当前收益不足）。

### 测试

- 定向：1 passed；全量：66 files / **1053 tests** passed。
- `vue-tsc -b` / `eslint src/views/__tests__` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：无 blocker；**major 1**（只看首个读取点 → 已改为遍历全部）、**major 2**（可空跑变绿 → 已加 `MIN_READERS`）；minor：500 硬预算、纯文本语义、死模式、未校验 v-model 绑定（记录）。
- 独立浏览器 QA：6/6 PASS，两轮独立运行数值一致；确认 `git diff` 仅新增测试文件、无行为变化。
- 总控：实现 + 两处增强，变异测试双向验证（删赋值 / 加二次读取均能变红）。

### 版本组合

- 前端：`bb1bbd6`（合并为 `d3cf094`）。
- 后端：`main@05dcdc0`（无接口改动）。

### 后续建议

1. **P3 语义化守卫**：捕获 `const s = route.query.search` 的变量名，要求 RHS 使用同一变量；并校验 `v-model` 绑定，减少假红/假绿。
2. **P3 CI 注释同步**：CI 文案「69 个测试文件」已陈旧（实际 66→67）。
3. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## cam-iteration-20261028-account-regression(账号筛选诊断路径回归测试 + loading 绑定实测证伪)

**需求来源**:协议优先级 4(补齐当前功能相关回归测试)。闭环 #30 的验证缺口 —— 诊断/缓存分析路径此前仅代码级覆盖,且本环境 WAF 无日志源,无法取得 live 断言。

**改动(e-cam-web#37,纯测试)**:LogDiagnoseCard.test.ts 的 context fixture 增加 account_ids(现有全量相等断言即覆盖 卡片→请求 透传);index.test.ts 新增 WAF Tab 用例,断言诊断请求携带所选账号。

**重要实测结论(避免重复踩坑)**:**el-select 的 loading 属性在本项目 EP 版本下不产生任何可见反馈** —— 人为延迟源清单响应 3-4s 后,账号选择器(加 :loading)与源选择器(原本有 :loading)渲染的 DOM **完全一致**(均为 `el-select__suffix > i.el-select__caret`,无 spinner/is-loading)。原计划的 `:loading="sourcesLoading"` 属**视觉空操作**,已撤掉,不以 UI 改进名义交付。若确需加载反馈,应走 placeholder 文案或禁用态等真正可见方案(需产品口径)。

**验收**:vue-tsc 0 / vitest 163/163(12 files) / eslint 0;CI 四项全绿后合并(web main=14c0c29)。浏览器回归(真实链路 :3333+真实后端,:1440×900):工具栏三选择器齐、账号筛选生效(search 携带 account_ids:[1])、0 页面错误。

**未执行(如实)**:WAF 诊断 live 断言(环境无 WAF 源),本轮以单测覆盖该缺口。

---

## cam-iteration-20261028-cert-filter-sync（2026-10-28）

**状态**：**已合并**（`e-cam-web#36`，merge commit `7c48ae7`，2026-10-28 23:42 +08）
**代码 PR**：`e-cam-web#36`（commits `44dadd8` → `e1703fd`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 台账页 `onMounted` 解析 `route.query.daysLeft` 并据此请求，但筛选控件在 `CertTable` 内部、以 `ref('')` 自持状态——`/certs?daysLeft=le30` 显示 14 行（已过滤）而下拉仍显示「请选择」，用户看不到当前档位。
- 该深链用于外部链接/收藏/手输 URL（到期看板自 `619726f` 起改为页内 `?level=` 筛选，不再跳转台账）。

### 需求卡

- 目标用户：通过链接进入带筛选台账的运维。
- 目标：深链档位在「剩余天数」下拉回显；后续手动切换仍生效；无 query 时为默认态。
- 非目标：URL 回写（受控化）、hostingStatus/search 深链（页面未解析）、看板下钻口径。
- 规则：`CertTable` 新增可选 `initialHostingStatus/initialDaysLeft/initialSearch`；refs 从 props 初始化，并加单向幂等 watch（父级值变化时同步、判等防回环）。
- API 契约：无。
- 风险：依赖 `router-view :key="route.fullPath"` 重挂载；watch 为单向（本组件仍是用户操作源）。

### 变更内容

- `CertTable.vue`：3 个可选 props + 3 个单向 watch + 卸载清理 `searchTimer`。
- `ledger/index.vue`：透传 `:initial-hosting-status/:initial-days-left/:initial-search`。
- `ledger/index.test.ts`：AC4 断言 `initialDaysLeft === 'expired'` 透传。

### 验收（真实浏览器 + 真实后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | `?daysLeft=le30` | ✅ 下拉「≤30 天」、14 行、共 14 条 |
| 2 | `le7` / `le14` / `expired` | ✅ 「≤7 天」4 行 / 「≤14 天」5 行 / 「已过期」首页 20 行（共 27） |
| 3 | 无 query | ✅ 两个下拉均「请选择」、20 行、共 88 条 |
| 4 | 深链后手动切档 | ✅ 「≤30 天」14 → 「≤7 天」4 → 「≤14 天」5 |
| 5 | 清除（选「剩余天数：全部」） | ✅ 回 20 行、下拉回占位、请求去掉 daysLeft |
| 6 | 看板分级卡 → 台账 | **无法验证**：入口已被移除（页内 `?level=` 筛选），点击期间台账接口 0 请求 |
| 7 | 390×844 | ✅ 回显正常、`scrollWidth=clientWidth=390`、无横向溢出 |
| 8 | console | ✅ error 0 / pageerror 0 |
| 9 | watch 是否重复请求 | ✅ 进入/切档/清除各 1 次请求，无回环 |

独立 QA：**8/8 PASS**（另 1 项入口不存在，如实标注），两轮数值一致。审查：0 blocker；major（仅初始化语义）已用单向幂等 watch 处置。

### 已知遗留

- **URL 不回写（S3）**：深链 le30 后手动切 le7，地址栏仍 `?daysLeft=le30`，刷新会回 le30；分享当前筛选亦不可用。需按 `tasks/index.vue` 的「URL 单一真值 + `router.replace`」受控化（约 30–50 行），下一轮候选。
- **无专用重置按钮**：清空依赖下拉「剩余天数：全部」。
- **390 首屏下拉底边 852 > 844**：需下滚约 8px 才完整可见（无横向溢出）。
- **深链入口缺失**：看板已改页内筛选，该深链目前只能外部链接触达；若产品仍需下钻需确认口径。

### 测试

- `vitest run`：66 files / **1053 tests** passed（AC4 新增透传断言）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：0 blocker；major（`initial*` 与父级活状态语义冲突、依赖重挂载兜底）→ 加单向幂等 watch；minor（`searchTimer` 未清理、注释不符）已修；指出 `daysLeft` 深链在仓内已无生产者。
- 独立 QA：8/8 PASS，含请求计数（watch 无额外请求）、390 命中测试、清除路径。
- 总控：实现 + 评审修复，浏览器 6 项复跑一致。

### 版本组合

- 前端：`e1703fd`（合并为 `7c48ae7`）。
- 后端：`main@9db6c4e`（无接口改动）。

### 后续建议

1. **P2 台账筛选受控化**：URL 单一真值 + `router.replace` 回写 + `watch(route.query)`（同 tasks 范式），可一次解决回显/分享/刷新三类问题。
2. **P3 重置入口**：筛选区加「清除筛选」按钮（当前需逐项选回「全部」）。
3. **P3 390 首屏**：压缩统计卡/工具栏高度，避免下拉被视口裁切 8px。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。

---

## 2026-10-28 巡检:main 分支 CI 红灯归因(非代码缺陷,避免误判与重复排查)

**背景**:web/service 两侧 main 上 `Build & Deploy` 长期红灯,易被误读为代码回归。

**排查(逐步骤定位)**:
| 仓库 | 工作流 | 失败步骤 | 性质 |
|---|---|---|---|
| e-cam-web | Build & Deploy(如 run 40691202263 @7c48ae7) | **`Login to Aliyun Registry`** | 基础设施/凭据(阿里云镜像仓库登录),非代码 |
| e-cam-service | Build & Deploy(如 run 40691591408 @bd9b141) | **`Login to Aliyun Registry`** | 同上 |
| e-cam-service | CI/CD Pipeline | Test(未发布 e-cloudx-sdk/e-iam 版本) | 既知系统性依赖问题 |

**关键结论**:
1. 两侧 `Build & Deploy` 的红灯**均止于阿里云镜像仓库登录步骤**,与代码无关 —— 在 386dbc3/d3cf094/14c0c29/7c48ae7 等多个 main 提交上稳定复现,属 CI 凭据/网络环境问题,应由维护者处理(本轮不伪造修复)。
2. **PR 门禁工作流(web `CI`)在 main 上全绿**(Gzip Budget/ESLint/Vitest/Typecheck & Build 四项 success),本日合并的 #28/#30/#37 均由该门禁放行 —— 门禁有效。
3. e-cam-service 的 `Test` 失败为既知系统性(依赖版本未发布),与各在途 PR 表现一致,非本轮引入。

**方法备注**:`check-runs` 报出的失败项若在 `actions/runs?head_sha=` 查不到运行记录,需改用 `actions/runs?per_page=N` 按时间列举,再取 `runs/{id}/jobs` 逐步骤定位失败步骤(本次即如此定位到 Registry 登录步骤)。

---

## cam-iteration-20261028-cert-filter-reset（2026-10-28）

**状态**：**已合并**（`e-cam-web#38`，merge commit `b79f2f9`，2026-10-28 00:57 +08）
**代码 PR**：`e-cam-web#38`（commits `d279842` → `8a0d69c` → `9136b24`；CI 4/4 绿）
**记录 PR**：本 PR（无后端代码改动）

### 需求来源与证据

- 上一轮 QA 观察：台账筛选只能逐个把下拉选回「全部」、手动清空搜索框；深链（`?daysLeft=le30`）或组合筛选后要清三次，且无专用重置入口。
- 审查发现更深的问题：清空后 URL 仍带旧 query，刷新或「进详情→面包屑返回」会按旧 query **复活筛选**（`detail` 返回走 `router.back()`，`isCertListBackTarget` 接受带 query 的 `/certs?...`）。
- QA 旧树实测：`?daysLeft=le30` 点清除后 URL 仍为 `?daysLeft=le30`，F5 回到 14 条并重现按钮；旧树还偶发清除时 2 次列表请求。

### 需求卡

- 目标用户：带筛选查看台账、想一键回到全量的运维。
- 目标：有筛选时出现「清除筛选」，一次清空搜索+托管状态+剩余天数、只发一次查询；清空后 URL 去掉旧 query（刷新/返回不复活）；无筛选时不显示。
- 非目标：筛选变更的完整 URL 写回（受控化，需 `router.replace` 每次重挂载，另评估）；`hiddenExpanded` 语义。
- 规则：`resetFilters` 清三个 ref + 取消未触发防抖 + 单次 emit；父级 `onQueryChange` 检测「已清空且有旧 query」→ `router.replace({path:'/certs'})`（重挂载后由 onMounted 取数）。
- API 契约：无。

### 变更内容

- `CertTable.vue`：按钮（`v-if="filterActive"`、`text type="primary"`、`aria-label`）、`resetFilters`（重入守卫）、`clearSearchTimer`（防抖回调置 null）。
- `ledger/index.vue`：清空时 `router.replace('/certs')`；旧 query 判断改读 `route.query` 对象（避免新增 `route.query.search` 读取点触发上一轮契约测试）。

### 验收（真实浏览器 + 真实后端，基线 `9136b24`）

| # | 场景 | 结果 |
|---|---|---|
| 1 | `?daysLeft=le30` → 清除 | ✅ 14→88 条、下拉回「请选择」、按钮消失、请求 1 次、URL→`/cam/certs` |
| 2 | 搜索 `jlcpcb` → 清除 | ✅ 88→5→88、输入框空 |
| 3 | 仅指纹（25）→ 清除 | ✅ 25→88 |
| 4 | 无筛选 | ✅ 按钮不存在（DOM/outerHTML 均无） |
| 5 | 组合（le7+仅指纹）→ 清除 | ✅ 2→88、请求 1 次 |
| 6 | 390×844 | ✅ 搜索框 204=204（Δ0 未挤压）、换行数不变、溢出 0、点击生效 |
| 7 | console | ✅ error 0 / pageerror 0 / API≥400 0 |
| 8 | 位置/样式/误触 | ✅ 纯 Element 样式、Tab+Enter 可触发、双击仅 1 次请求 |
| 9 | 清空后返回/刷新 | ✅ 进详情→面包屑返回、浏览器后退、F5 均 88 条、按钮隐藏 |
| 10 | 防抖取消 | ✅ 输入 `jlc` 后 250ms 内点清除 → 1 次请求、无 `search=` |

独立 QA：**8/8 PASS**（冻结基线后重跑；旧树缺陷已消除，单请求连测 10 次恒为 1）。

### 已知遗留

- **筛选变更仍不回写 URL**：在 `/certs` 里筛到某一档后地址栏无 query，分享/收藏仍是全量；受控化需配合 `router-view :key=fullPath` 的重挂载代价评估。
- **焦点**：点击后按钮被卸载，焦点落回 body；键盘用户需重新 Tab（审查 minor）。
- **触控高度 32px**：低于 44px 建议值，但与同栏控件一致。
- **390 工具栏换行**：「托管状态」下拉因 `toolbar-spacer` 右移 34px（无按钮时同样存在）。

### 测试

- `vitest run`：66 files / **1054 tests** passed（含上一轮 `searchPrefill` 契约）。
- `vue-tsc -b` / `eslint` / `vite build` 均 exit 0。
- PR CI：Typecheck & Build、Vitest、ESLint、Gzip Budget 全 SUCCESS。

### 子 Agent 与复核

- 独立审查：0 blocker；**major**（清空不回写 URL → 返回复活）→ 已修（`router.replace`）；minor（焦点丢失、测试缺口、390 布局）与 nit（死类名、`searchTimer` 语义、双击守卫）分别修复或记录。
- 独立浏览器 QA：8/8 PASS；期间发现工作树被并发修改（我方应用评审意见），改用冻结基线复跑并对比旧树缺陷。
- 总控：实现 + major 修复 + 全量 1054 复跑 + 浏览器 7 项复验。

### 版本组合

- 前端：`9136b24`（合并为 `b79f2f9`）。
- 后端：`main@bd9b141`（无接口改动）。

### 后续建议

1. **P2 台账筛选受控化**：URL 单一真值（`router.replace` + `watch(route.query)`），一次性解决回显/分享/刷新/返回一致；需评估 fullPath key 重挂载的滚动/焦点代价（或改为不依赖重挂载的 key 策略）。
2. **P3 焦点管理**：清除后把焦点交回搜索框（`nextTick` + ref），或用常驻 `:disabled` 替代 `v-if`。
3. **P3 触控尺寸**：工具栏控件统一到 36–40px（移动端）。
4. **P1 仍待办**：任务生命周期治理、采集 running 超时、真实菜单死链排查（`menu-config.ts` 仍被并行会话占用）。
