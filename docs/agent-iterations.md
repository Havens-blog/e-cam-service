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

**状态：实现和验收完成，待审核**（火山 2026-03~08 历史账期为云侧 API 限制，已记录为用户侧遗留动作，见上）

- **PR**：
  - e-cam-service: https://github.com/Havens-blog/e-cam-service/pull/5 （分支 `cam-iteration-20261027-multicloud-cost`，commit `5ffb731`）
  - e-cloudx-sdk: https://github.com/Havens-blog/e-cloudx-sdk/pull/2 （同名分支，commit `8dc4029`）
  - 交叉引用已写入 PR 描述；建议先合 SDK 仓（go.work 本地联调，无阻塞依赖）
  - e-cam-web 本轮无改动，无 PR（不制造空提交）
- **联调版本组合**：e-cam-service `5ffb731` + e-cloudx-sdk `8dc4029`（生产 dev 环境经 CompileDaemon 热部署，二进制 10:10:50 起为修复后版本）
- **浏览器验收**（Playwright chromium 141 headless，视口 1440x900 + 375x812，经 nginx:8888 实链路）：
  1. 未登录访问 `/cam/finops/cost` → 跳 `/console/login?redirect=...` ✅
  2. admin 登录（仓库迁移初始凭证）→ 直达成本页 ✅
  3. 成本页渲染、默认窗与宽窗（2025-12-01~2026-10-27）全部 cost API 200 ✅
  4. 宽窗口页面出现四云标签：阿里云/华为云/腾讯云/火山 ✅
  5. 控制台错误仅 2 条，均来自未登录阶段（401+路由守卫），属预期行为；无新增错误
  - 已知坑（记录供后续会话复用）：登录态 cookie `ecmdb-token-key` 的 Domain 绑定 `localhost`——脚本/测试必须全程使用 `http://localhost:8888`，用 `127.0.0.1` 会被浏览器拒收 cookie（表现为"登录成功但永远 401"）
- **证据**：`docs/agent-iterations-evidence/cam-iteration-20261027-multicloud-cost/`（01 未登录跳转、02 登录后、03 默认窗、04 宽窗、05 窄屏截图 + results.json）
- **历史缺口补采**（生产 `CollectAccount` 路径，`backfill_range_collect.go`）：
  - ✅ tencent 2026-03~10 一次成功（32m34s）：4~9 月缺口全部入库（4月 ¥57,832 / 5月 ¥67,243 / 6月 ¥66,177 / 7月 ¥78,789 / 8月 ¥98,963 / 9月 ¥189,751），汇总表按范围自动重建
  - ✅ volcengine 2026-01 缺口补齐（¥222,959）
  - ❌ volcengine 2026-03~08 六个账期月：分段（4 段）与单月粒度均被火山服务端拒绝——`InternalServiceTimeout`（3~6 月）与 `RequestInvalid 查询数据错误`（4~8 月交替出现），而 2025-12/01/02/09/10 正常。参数错误应全月一致失败，故判定为**火山账单明细 API 对该批历史账期不可用**（云侧限制）。已留存各月 request id 于采集日志；如需补齐需走火山账单导出/工单渠道（用户侧动作，非代码）
  - 幂等性：脚本按月先删后插，云侧恢复后可安全重跑同一命令
- **终态账期覆盖**（ecam_cost_unified_bill 按 provider×账期月，2026-10-27 10:45）：
  - aliyun：2025-12 ~ 2026-10 共 11 月完整（¥1.72M~2.56M/月）
  - tencent：2026-03 ~ 2026-10 共 8 月完整（本轮补齐 4~9 月）
  - volcengine：2025-12、01（新补）、02、09、10 共 5 月；缺 3~8 月（云侧限制，见上）
  - huawei：仅 2026-02（¥30.6M）；更多账期受 `bss:billDetail:view` 权限缺口阻塞（云侧运维动作）
- **缺口窗口复验**（2026-04-01 ~ 2026-09-30，补采前腾讯在此窗口零数据）：
  - distribution API 精确返回 3 个 provider：aliyun ¥13,126,954.41 (85.7%) / volcano ¥1,623,924.84 (10.6%) / **tencent ¥558,758.28 (3.65%，补采数据)**，无 huawei（4~9 月确无华为账期，精确正确）
  - API 金额与库内聚合逐分吻合（腾讯 6 月合计=各月之和）→ 页面与数据库一致
  - 注：页面 DOM 文本含"华为云"来自**云厂商筛选下拉的静态选项**（常驻），数据级断言以 distribution API 响应为准（gap-months-distribution.json 已存证）
- **模拟/真实边界声明**：本轮所有云调用均为真实云厂商账单**只读** API（与定时采集同类，用户配置的真实账号、用户明确要求的实盘数据场景）；未做任何云资源写操作；浏览器链路为本地真实前后端 + 远程测试库（106.52.187.69），无前端 Mock

### 后续建议

1. `internal/account/repository/account.go toDomain`：解密失败静默回退密文是高危隐患（本轮补采首次运行即踩中：脚本未初始化加密组件 → 签名错误）。建议改为解密失败返回错误，或至少告警日志。
2. 采集日志 `bill_start/bill_end` 记录的是"请求范围"而非"实际返回账期范围"，排查历史缺口时可读性差，可考虑记录适配器实际覆盖的账期月清单。
3. e-cam-web 无 E2E 框架，成本页核心流程（筛选→分布→趋势）值得补 Playwright 冒烟（需先确认登录测试账号约定）。
4. 华为云 `bss:billDetail:view`、AWS Cost Explorer 授权仍是云侧权限缺口（运维动作，非代码）。
