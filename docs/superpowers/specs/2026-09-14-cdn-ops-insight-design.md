# 多云 CDN 经营视图(成本 + 性能指标)设计

日期:2026-09-14
状态:待评审
关联:CDN 台账功能(归一化/缓存配置/功能配置已上线)

## 1. 背景与目标

### 1.1 第一性原理

CDN 的本质是**花钱买流量分发能力**。按此拆解,多云 CDN 管理平台的价值轴四条:
成本(花了多少钱)、性能(加速效果)、配置/健康(已有)、操作闭环(远期)。

当前实现重「管理台账」轻「价值洞察」——实测:
- **成本**:`cost_unified_bill` 已采 280 条 CDN/DCDN 账单(阿里云单月 ¥2~4 万),但
  页面零展示、无法按域名归因(`resource_name` 空)。
- **性能**:586 个域名的 `bandwidth`/`traffic_total` 字段恒为 0(无适配器填充),
  无任何指标采集/时序能力,前端零展示。

### 1.2 目标

把 CDN 从「台账」升级为「经营视图」:
1. **一期(成本洞察)**:展示 CDN 月度成本(账号×产品真实粒度)+ 域名级分摊成本(流量占比估算)。
2. **二期(性能指标)**:每日按域名采集带宽/流量/命中率,存指标表,展示趋势与排行。

### 1.3 用户已确认的决策

- 方向:成本归因 + 带宽/流量/命中率指标(两者都做)。
- 二期指标粒度:**天粒度**(每域名每天一行;小时粒度被否,采集量 24 倍不值)。
- 域名级成本分摊:**一期就做**(用流量占比分摊,标注"估算")。

## 2. 现状盘点(实测依据)

- 后端 5 厂商 CDN 适配器已建:`aliyun/tencent/huawei/aws/volcano`;`CDNCacheQuerier`
  全实现,`CDNSettingsQuerier` 仅 aliyun/volcano。
- 归一化基建 `cdn_normalize.go` + `types/cdn.go` 已就绪。
- 成本 DAO(`internal/cam/cost/repository/dao/bill.go`)已有
  `AggregateByField` / `AggregateDailyAmount` / `SumAmount`,可复用。
- 任务执行器模式:`task/executor/sync_*.go`,经 `taskx.Queue` 注册,账号级互斥已实现。
- 前端:列表页 3 统计卡(总数/在线/HTTPS)、详情抽屉 5 Tab;`utils/cdn.ts` 共享标签。
- 监控 API 可行性:阿里(`DescribeDomainFlowData/BpsData/HitRateData`)、
  腾讯(`DescribeCdnData/OriginData/BillingData`)SDK 确认存在;华为/火山/AWS 同类 API 存在。

## 3. 一期:成本洞察

### 3.1 后端

**接口** `GET /api/v1/cam/cost/cdn`(挂到现有成本路由或 CDN 资产路由):
查询参数:`start_month`(默认近 6 个月)、`account_id`(可选)。

**响应**:
```json
{
  "monthly": [{"month": "2026-09", "cdn_amount": 26500.0, "dcdn_amount": 21000.0}],
  "by_account": [{"account_id": 1, "account_name": "阿里云-主账号", "amount": 47500.0, "share": 0.9}],
  "domain_cost": [
    {"domain": "static.example.com", "amount_est": 8200.0, "bytes": 5.2e11, "hit_rate": 0.93, "is_estimate": true}
  ]
}
```

**实现**:
- 复用 `BillDAO`:按 `service_type_name ∈ {cdn, dcdn}` + tenant + 月份过滤,
  `AggregateByField("service_type_name", ...)` 得产品级月成本,
  `AggregateByField("account_id", ...)` 得账号级月成本。
- 域名级分摊:二期指标表就绪前用「当前域名单日/近N天字节占比 × 账号月成本」估算;
  标注 `is_estimate: true`。零流量域名不计入分摊,未分摊部分留在账号级。

### 3.2 前端

- CDN 列表页统计卡区新增「本月成本」卡(取 `monthly` 最新月 CDN+DCDN 合计)。
- 新增小型成本面板(下拉/抽屉):月度成本趋势、按账号占比、域名成本 Top N 排行
  (域名行标注「估算」)。复用 echarts + 现有暗色 token。

## 4. 二期:性能指标采集

### 4.1 统一指标模型

```go
// types/cdn.go
type CDNMetric struct {
    Domain    string `bson:"domain"`    // 归一化域名
    Date      string `bson:"date"`      // YYYY-MM-DD
    Bytes     int64  `bson:"bytes"`     // 当日流量(字节)
    Bandwidth int64  `bson:"bandwidth"` // 当日带宽峰值(bps)
    HitRate   float64 `bson:"hit_rate"` // 命中率 0-1(可空用 -1)
    AccountID int64  `bson:"account_id"`
    Provider  string `bson:"provider"`
    Ctime     int64  `bson:"ctime"`
}
```

**接口扩展**:`CDNMetricQuerier`(可选能力,与 Cache/Settings 同构):
```go
type CDNMetricQuerier interface {
    GetDomainMetrics(ctx context.Context, domainName, domainID string,
        startDate, endDate string) ([]types.CDNMetric, error)
}
```

### 4.2 采集任务

- 新任务类型 `cdn:collect_metrics` + 执行器 `SyncCDNMetricsExecutor`
  (复用账号级互斥模式,可并入现有 executor 或独立)。
- 调度:挂在现有 auto-sync scheduler,每账号每日一次,回看最近 1~2 天补缺。
- 落库:`ecam_cdn_metric` 集合,唯一索引 `{domain, date}`(upsert 幂等)。
- 5 厂商 `GetDomainMetrics`:
  - aliyun:`DescribeDomainFlowData`(流量)+ `DescribeDomainBpsData`(带宽)+
    `DescribeDomainHitRateData`(命中率),按日聚合。
  - tencent:`DescribeCdnData`(metric=flux/bandwidth/hitRate),Interval=day。
  - huawei:CDN v2 `ShowDomainStats` 或日志域;可行性在实现期用 SDK 探测确认,
    失败则该厂商指标缺省,不阻塞整体。
  - volcano:CDN 监控 API;同上尽力而为。
  - aws:CloudFront 无现成"命中率"API,用 `bandwidth`(实时监控 API);
    命中率经 CloudFront 日志域可算,一期指标可先缺省。

### 4.3 读取接口

- `GET /api/v1/cam/assets/cdn/metrics?domain_name=&days=30`:
  返回该域名近 N 天 `[{date, bytes, bandwidth, hit_rate}]`。
- `GET /api/v1/cam/assets/cdn/top?metric=bytes&days=7&limit=10`:
  域名流量/成本 Top 排行(供分摊展示)。

### 4.4 前端

- CDN 列表页:「今日带宽峰值」统计卡(选当日 Bandwidth 最大域名值)。
- 详情抽屉新增「流量/命中率」Tab:echarts 双轴图(柱=流量,线=命中率),
  近 30 天趋势;数据来自 metrics 接口。

## 5. 错误处理与约束

- **采集失败容忍**:单域名/单厂商监控 API 失败仅记录日志、该条目缺省,
  不影响其他域名与账号(与快照基建一致的"尽力而为"原则)。
- **成本估算标注**:一切按流量占比分摊的域名成本必须带 `is_estimate`,
  前端显著标注,避免用户误读为精确账单。
- **无密钥泄漏**:沿用 `sensitiveArgs`/`maskIfSet` 脱敏模式,接口不返回凭证类参数。
- **配额保护**:采集任务受账号级互斥 + 日频限制,不在详情页实时拉历史。

## 6. 测试策略

- 后端:归一化/metrics 汇总/成本分摊纯函数单测;
  `ecam_cdn_metric` DAO 活体测试(MONGO_DSN gated,沿用 stats_snapshot 模式);
  成本聚合复用已有 BillDAO 测试。
- 前端:vue-tsc 0 错误、eslint 0 警告;echarts 组件用 mock 数据渲染检查。

## 7. 分期交付

- 一期提交:成本接口 + 前端成本卡/面板(不依赖二期)。
- 二期提交:指标模型 + 5 厂商采集 + 任务 + metrics/top 接口 + 前端趋势 Tab + 分摊接入。
- 每期独立可验证、可回滚。

## 8. 待实现期确认(非阻塞)

- 华为/火山/AWS 监控 API 精确接口名与返回结构(实现期 SDK 探测,失败则缺省)。
- 域名级分摊的流量数据源:一期用「采集到的近 N 天字节」,指标为空时只展示账号级成本。
