# 多云 CDN 经营视图(成本+性能指标)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 CDN 从台账升级为经营视图——一期展示 CDN 月度成本(账号×产品真实粒度 + 域名级流量占比分摊),二期按天采集带宽/流量/命中率并展示趋势。

**Architecture:** 复用现有成本 DAO(`cost_unified_bill`)与任务队列(`taskx`)。一期加 CDN 成本聚合查询接口 + 前端成本面板;二期给 5 厂商 CDN 适配器加 `CDNMetricQuerier` 可选能力,新增 `cdn:collect_metrics` 定时任务写 `ecam_cdn_metric` 表,前端加流量/命中率趋势 Tab。

**Tech Stack:** Go (ego/ginx/mongox/mongo-driver)、Vue3 + Element Plus + echarts、既有 `taskx.Queue`。

**Spec:** `docs/superpowers/specs/2026-09-14-cdn-ops-insight-design.md`

## Global Constraints

- 并行会话共享工作区,`git add -A` 禁用——所有提交用显式文件清单。
- 采集失败一律容忍(单厂商/域名缺省,不阻塞整体);成本分摊必标 `is_estimate`。
- 无密钥泄漏:`sensitiveArgs`/`maskIfSet` 脱敏模式已有,接口不得返回凭证类参数。
- 采集受账号级互斥 + 日频限制;不在详情页实时拉历史。
- 天粒度;(域名,date) 唯一索引,upsert 幂等。
- CDN 账单按 `service_type_name ∈ {cdn, dcdn, p_cdn}`(前缀 cdn/dcdn)过滤——**不可用 `service_type=network`**(280 条中仅 94 条是 network,186 条金额落入 other)。

---

### Task 1: 一期后端 CDN 成本聚合

**Files:**
- Create: `internal/cam/cost/repository/dao/bill_cdn.go`(仅为 CDN 专用聚合,复用 `billDAO` 底层集合)
- Create: `internal/cam/service/cdn_cost.go`(CDN 成本服务)
- Create: `internal/cam/web/handler_cost_cdn.go`(web handler)
- Modify: `internal/cam/cost/repository/types.go`(加 CDN 聚合返回类型,如感可并入)
- Modify: `internal/cam/init.go`(wire 注入 CDN 成本服务 + 路由)

**Interfaces:**
- Consumes: `repository.BillDAO`(`AggregateByField(ctx, tenantID, field, startDate, endDate, filter) ([]AggregateResult, error)`)、`UnifiedBillFilter{ServiceType, StartDate, EndDate, ...}`
- Produces: `CDN cost` 聚合接口 `GET /api/v1/cam/cost/cdn?start_month=YYYY-MM&months=6&account_id=N`:
  ```json
  {"monthly": [{"month":"2026-09","cdn_amount":26500.0,"dcdn_amount":21000.0}],
   "by_account": [{"account_id":1,"account_name":"...","amount":47500.0,"share":0.9}],
   "domain_cost": [{"domain":"...","amount_est":8200.0,"bytes":5.2e11,"is_estimate":true}]}
  ```

**实现要点(填充 bill_cdn.go):**
- 关键:现有 `billDAO` 的 `AggregateByField` 只支持 `service_type` 等值过滤,而 CDN 账单 64% 金额 `service_type=other`。**必须直接查 `ecam_cost_unified_bill` 集合并按 `service_type_name` 正则** `^cdn|^p_cdn|^dcdn` 过滤。
- 复制既有 `aggregatePipeline` 模式,新增:
  ```go
  type CDNBillDAO struct { coll *mongo.Collection } // 或复用 billDAO 加方法
  func AggregateByServiceTypeName(ctx, tenantID, field, startDate, endDate string) ([]AggregateResult, error)
  ```
  pipeline: `{$match: {tenant_id, billing_date: {$gte, $lte}, service_type_name: {$regex: "^(cdn|p_cdn|dcdn)"}}}`,按 field 分组求和 `amount_cny`。
- CDN 成本服务 `cdnCostService`:
  - `GetMonthly(tenantID, months int)` → `[{month, cdn_amount, dcdn_amount}]`:月粒度用 `billing_date` 前 7 位 `YYYY-MM` 分组。
  - `GetByAccount(tenantID, month)` → 按账号聚合。
  - `GetDomainCost(tenantID, month)` → 从 `ecam_cdn_metric` 取该账号近 30 天域名流量字节,按账号占比分摊得 `amount_est`(二期接入;一期返回空数组 + `is_estimate:false`,服务端不报错)。

**测试计划:** 单测覆盖 CDN 账单聚合正则(`p_cdn` 前缀、排除非 CDN);DAO 层活体测试沿用 `MONGO_DSN` gated 模式(参照 `stats_snapshot_live_test.go`)。

- [ ] **Step 1: 写失败测试** — `internal/cam/cost/repository/dao/bill_cdn_test.go`
  ```go
  func TestAggregateByServiceTypeName(t *testing.T) {
      // 用 bson regex 正则 "^(cdn|p_cdn|dcdn)" 断言:
      //  - 能匹配 "cdn"、"p_cdn"、"dcdn"
      //  - 不匹配 "cdnbilling"、"other"
  }
  ```
- [ ] **Step 2: 运行确认失败** — `go test ./internal/cam/cost/repository/dao/ -run TestAggregateByServiceTypeName -count=1`
- [ ] **Step 3: 实现 `bill_cdn.go`** — CDN 正则聚合 + 单元测试通过
- [ ] **Step 4: 实现 `cdn_cost.go` + `handler_cost_cdn.go`** — 月/账号/域名三分支
- [ ] **Step 5: init.go 注入 + 路由注册** — `GET /assets/cost/cdn`
- [ ] **Step 6: `go build ./internal/...` + gofmt + go vet**
- [ ] **Step 7: 提交** — `feat(cost): CDN 经营成本聚合接口(按月/账号/流量分摊)`

---

### Task 2: 一期前端成本卡 + 面板

**Files:**
- Modify: `src/api/asset.ts`(加 `getCdnCostApi`)
- Create: `src/views/network/cdn/components/CdnCostPanel.vue`(成本面板)
- Modify: `src/views/network/cdn/index.vue`(统计卡区加「本月成本」+ 面板触发)

**Interfaces:**
- Consumes: Task 1 的 `GET /api/v1/cam/cost/cdn` API
- Produces: CDN 列表页成本卡 + 下拉面板(月度趋势、账号占比、域名 Top)

**测试:** vue-tsc 0 错误、eslint 0 警告。

- [ ] **Step 1: 加 API 函数** `getCdnCostApi(params?)` → `/assets/cost/cdn`
- [ ] **Step 2: 写统计卡** 「本月成本」卡(`formatNumber` 处理,卡图沿用现有 `StatCard` 模式,`icon="Money"`)
- [ ] **Step 3: CdnCostPanel** echarts 年度成本趋势(柱) + 按账号占比(环) + 域名 Top(列表,标「估算」)
- [ ] **Step 4: vue-tsc + eslint + commit**

---

### Task 3: 二期指标模型 + 采集执行器

**Files:**
- Create: `internal/cam/repository/dao/cdn_metric.go`(或并入 `dao/`)
- Create: `internal/cam/task/executor/sync_cdn_metrics.go`
- Modify: `internal/shared/cloudx/types/cdn.go`(加 `CDNMetric`)
- Modify: `internal/shared/cloudx/interfaces.go`(加 `CDNMetricQuerier`)
- Modify: `internal/cam/task/module.go`(注册执行器)

**Interfaces:**
- Consumes: 5 厂商 `CDNAdapter`(`ListInstances` 已有)、`taskx.Queue.RegisterExecutor`
- Produces: `CDNMetricQuerier.GetDomainMetrics(ctx, domainName, domainID, startDate, endDate) ([]types.CDNMetric, error)`、`ecam_cdn_metric` 唯一索引 `{domain,date}`、任务 `cdn:collect_metrics`

**实现要点:**
- `NewCDNMetricQuerier` 为可选接口,5 厂商适配器 `GetDomainMetrics` 实现:
  - **aliyun**: `DescribeDomainFlowData`(流量,`DataInterval=600`≈10min 合→日)+ `DescribeDomainBpsData`(带宽峰值)+ `DescribeDomainHitRateData`(命中率)。按 `DataModule[]` 累加/取峰值。
  - **tencent**: `DescribeCdnData`(metrics=flux/bandwidth/hitRate,`Interval=day`)。
  - **huawei/volcano/aws**: 实现期用 SDK 探测;仅有流量或带宽之一即可,`HitRate` 缺省用 `-1`。探测失败 → 该厂商 `GetDomainMetrics` 返回空 + 不注册到采集清单。
- 执行器 `SyncCDNMetricsExecutor.Execute`:
  - 解析任务参数(账号列表/地域),复用既有 `expandAssetTypes` 不需要。
  - 遍历账号:`cloudxFactory.CreateAdapter` → `adapter.CDN()` → 断言 `CDNMetricQuerier`,不实现则跳过。
  - 对每个活跃域名:`GetDomainMetrics(domain, "", lastNDays)` → 逐条 upsert(按 `{domain,date}`)。
  - 失败日志 `elog.FieldErr`,继续下一域名。
- DAO:`UpsertMetric`,索引 `{domain:1, date:1}` unique。

**测试:** 指标归一化纯函数(aliyun 10min→日聚合)、DAO upsert 活体测试、执行器跳过不实现厂商的分支。

- [ ] **Step 1-3: 类型 + 接口 + DAO + 失败测试**
- [ ] **Step 4-5: aliyun/tencent `GetDomainMetrics` 实现**
- [ ] **Step 6-7: huawei/volcano/aws 尽力实现**
- [ ] **Step 8: 执行器 + 注册**
- [ ] **Step 9: build + vet + commit** `feat(cdn): 每日带宽/流量/命中率指标采集`

---

### Task 4: 二期指标读取接口 + 分摊接入

**Files:**
- Modify: `internal/cam/web/asset_handler_edge.go`(加 metrics/top handler)
- Modify: `internal/cam/web/asset_handler.go`(注册 `/assets/cdn/metrics`、`/assets/cdn/top`)
- Modify: `internal/cam/service/asset_cdn_query.go`(或新 `cdn_metric_service.go` 加 `GetDomainMetrics`、`GetTopDomains`)
- Modify: `internal/cam/service/cdn_cost.go`(域名成本分摊接真数据)

**Interfaces:**
- Consumes: `cdn_metric` DAO(方法 `ListByDomain`, `TopByBytes`)
- Produces:
  - `GET /api/v1/cam/assets/cdn/metrics?domain_name=&days=30` → `[{date, bytes, bandwidth, hit_rate}]`
  - `GET /api/v1/cam/assets/cdn/top?metric=bytes&days=7&limit=10` → Top 域名

- [ ] **Step 1-2: DAO ListByDomain/TopByBytes + 失败测试**
- [ ] **Step 3-5: service + handler + 路由**
- [ ] **Step 6: 分摊接入** — `cdn_cost.GetDomainCost` 用指标表近 30 天流量占比,空则返回 `[]`
- [ ] **Step 7: build + vet + commit** `feat(cdn): 指标读取接口 + 域名成本分摊接真数据`

---

### Task 5: 二期前端趋势 Tab

**Files:**
- Modify: `src/api/asset.ts`(加 `getCdnMetricsApi`、`getCdnTopDomainsApi`)
- Modify: `src/views/network/cdn/components/CdnDetailDrawer.vue`(加「流量/命中率」Tab,echarts 双轴图)
- Modify: `src/views/network/cdn/index.vue`(加「今日带宽」统计卡)

**Interfaces:**
- Consumes: Task 4 的 metrics/top 接口
- Produces: 详情抽屉趋势 Tab(柱=流量,线=命中率,近 30 天)

- [ ] **Step 1-2: API + 抽屉 Tab 骨架**
- [ ] **Step 3: echarts 双轴图**(流量柱 + 命中率线,复用既有 chart-options/色彩 token)
- [ ] **Step 4: 今日带宽统计卡** + 触发刷新
- [ ] **Step 5: vue-tsc + eslint + commit** `feat(cdn): 流量/命中率趋势 Tab + 今日带宽卡`

---

## Self-Review

- **Spec 覆盖:** 一期成本(账款+分摊)→ Task 1/2;二期采集 → Task 3;二期读取+分摊 → Task 4;二期前端 → Task 5;错误处理容忍/估算标注/配额保护 → Global Constraints + 各任务实现要点;测试 → 各任务 Step 1/测试计划。
- **占位符扫描:** 无 TODO/TBD 步骤;每个 Step 对代码任务给出文件与行为。huawei/volcano/aws 有「实现期探测」——这是 spec §8 明确允许的非阻塞缺口,已标注「失败则缺省」,不算占位符。
- **类型一致性:** `CDNMetric`(Task 3 定义)被 Task 4 DAO/接口共用;`AggregateResult`(Task 1 复用既有类型);`CDNMetricQuerier.GetDomainMetrics` 签名在 Task 3 定义、Task 4 消费——一致。