---
status: "completed"
started: "2026-09-19 12:17"
completed: "2026-09-19 14:09"
time_spent: "~1h 52m"
---

# Task Record: 2 NASMetric 模型 + NASMetricQuerier 接口 + ecam_nas_metric DAO 建表

## Summary
NASMetric 模型 + NASMetricQuerier 接口 + ecam_nas_metric DAO 建表完成。① types/nas_metric.go 定义 NASMetric(fs_id/fs_name/date/capacity(GB 二进制 GiB, float64)/used_capacity/qc_status/account_id/provider),utilization 不落库(模型层无该字段,bson 形状测试拦截),qc_status 取值常量冻结(NASMetricQcOK=""/NASMetricQcZeroException="zero_exception");② interfaces.go 在 NASAdapter 后新增可选接口 NASMetricQuerier(单方法 GetNASMetrics(ctx, fsID, fsName, region, startDate, endDate),签名带 region 区别于 CDN 全局签名,var _ 编译期断言+反射形状测试双锚定);③ dao/nas_metric.go 实现 NASMetricDAO:UpsertMetric + BulkUpsertMetrics(mongo BulkWrite, 仿 CDNMetricDAO),唯一键 (account_id, fs_id, date) 索引在构造器创建,写入路径 nasMetricQC 数量级自检——capacity=0 例外放行强制打 zero_exception(不拦截不跳过),非零越出 [1MB,1PB] 整批拒绝并携带 fs_id/date;④ 装配位(module.go:55/wire.go:135 的 NewCDNMetricDAO 位点)已勘察,实际接线按任务注记留给 T4/T7,本任务不产生无消费者装配;⑤ 12 个顶层测试全绿:非 live(QC 8 子用例含边界/写入前门禁/空批/错误透传)+ 4 个 MONGO_DSN 门控 live 测试(唯一索引 spec+裸 InsertOne dup-key 拦截/同账号同日幂等/跨账号同 fs 三行并存仿 TestCDNMetricPerAccountLive/批量重放幂等+零值行打标)对可达实例实跑通过,独立测试库 ecam_dao_test 整库 Drop 零残留(服务器库清单核对)。

## Changes

### Files Created
- internal/shared/cloudx/types/nas_metric.go
- internal/shared/cloudx/types/nas_metric_test.go
- internal/shared/cloudx/nas_metric_querier_test.go
- internal/cam/repository/dao/nas_metric.go
- internal/cam/repository/dao/nas_metric_test.go
- internal/cam/repository/dao/nas_metric_live_test.go

### Files Modified
- internal/shared/cloudx/interfaces.go

### Key Decisions
- capacity/used_capacity 用 float64 而非 NASInstance 的 int64:数量级自检下界 1MB(=1/1024 GiB)与探测实测值(1378.14 GB 等)需要小数精度,int64 会把亚 GB 值截断成 0 与 zero_exception 混淆
- 数量级自检落位 DAO 写入路径(UpsertMetric/BulkUpsertMetrics 共用 nasMetricQC),保证执行器/回填/任何后续写入方统一过门禁;非零越界行语义取『门禁拒绝并报错(错误携带 fs_id/date)』而非『审查标注放行』——proposal『只对非零行做门禁/审查标注』中取门禁读法,防字节直写 GB 的单位 bug 行污染读取侧;零值行不拦截不跳过照 AC
- NASMetric 增配 fs_name 冗余字段:Top/运营卡展示需要(提案 Top items 含 fs_name),指标行生命周期长于实例行(实例删除后历史指标仍可显示),避免读取侧联资产表;region 未落库(实例元数据可查,保持行精简)
- live 测试用独立测试库 ecam_dao_test + Cleanup 整库 Drop,不复刻 CDN live 测试直写 ecam 库的模式:本机可直达的是 prod 配置实例,测试写入(含索引创建)与真实业务集合零接触,已核对服务器库清单零残留
- 模型落新文件 types/nas_metric.go 而非改 nas.go:后者工作树为 CRLF(Edit 匹配风险),且与 cdn.go 承载 CDNMetric 的组织方式一致、文件聚焦
- BulkUpsertMetrics 与 UpsertMetric 一并提供:CDN 执行器实测写入口是 BulkUpsertMetrics(每域名一批),NAS 执行器(T5)同为每 fs 一批,属参照 DAO 的写路径面而非超scope;T5 的『今日首写生效』DAO 方法留待其自行扩展

## Test Results
- **Tests Executed**: Yes
- **Passed**: 12
- **Failed**: 0
- **Coverage**: 100.0%

## Acceptance Criteria
- [x] types.NASMetric 落库字段 fs_id/date/capacity(GB)/used_capacity(GB)/qc_status;utilization 不落库;qc_status 含 zero_exception 取值
- [x] cloudx.NASMetricQuerier 接口 GetNASMetrics(ctx, fsID, fsName, region, startDate, endDate) ([]types.NASMetric, error),签名带 region
- [x] ecam_nas_metric 唯一索引 (account_id, fs_id, date)(Unique);DAO 提供 UpsertMetric 保证键唯一(首写生效+昨日覆盖语义留给执行器)
- [x] 数量级自检:写入前 capacity 落在 [1MB,1PB] 校验;capacity=0 例外放行打 qc_status=zero_exception(不拦截不跳过)
- [x] 单测:DAO upsert 同账号同日幂等、跨账号同 fs 并存各一行(仿 TestCDNMetricPerAccountLive)

## Notes
coverage=100.0 为 dao/nas_metric.go 全部 6 个函数的 per-function 覆盖(go tool cover -func,含 MONGO_DSN live 路径);包级数字被包内既有无关文件稀释(dao 7.6%/cloudx 29.0%/types 96.2%),如实记录。测试计数:12 个顶层测试函数(dao 非 live 5 + live 4 + cloudx 签名 1 + types 2),其中 TestNASMetricQC 展开为 8 子用例。-race 未启用(宿主无 gcc,既有既录约束);live 测试凭证仅经 env 注入未落任何文件。硬规则逐条:GB 二进制 GiB(float64,禁止字节直写由门禁与模型注释双保险)/utilization 不落库(模型无字段+测试拦截)/唯一键含 account_id(live 实证跨账号三行并存)。fmt 门禁:自建 7 文件 gofmt -l 全净(修过 var 块与 struct 注释对齐);全仓既有 CRLF 噪声非本任务文件未触碰。并行会话 WIP(internal/cert/deployer/* 与 cert-volcano index.json)在树但绝不纳入本次提交,显式逐文件 add。
