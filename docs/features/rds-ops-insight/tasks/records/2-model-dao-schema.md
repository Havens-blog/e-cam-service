---
status: "completed"
started: "2026-09-22 10:39"
completed: "2026-09-22 11:15"
time_spent: "~36m"
---

# Task Record: 2 RDSMetric 模型 + RDSMetricQuerier 接口 + ecam_rds_metric DAO 建表

## Summary
RDSMetric 模型 + RDSMetricQuerier 接口 + ecam_rds_metric DAO 建表完成。① types/rds_metric.go 定义 RDSMetric(rds_id/instance_name/date/cpu_percent/memory_percent/disk_percent(三使用率 float64 0~100)/connections(int64 绝对值)/engine/qc_status/account_id/provider),engine 元数据字段按 T1 探测定案附上(aliyun 指标名前缀分派/huawei 维度键分派是查询必经依据,probe-report §2 修正 proposal「engine 仅透传」假设);qc_status 取值常量复用 NASMetricQc*(RDSMetricQcOK=""/RDSMetricQcZeroException="zero_exception")单一来源;不落库 QPS/IOPS 吞吐(Hard Rule);bson+json 11 字段精确往返测试拦截 tag 漂移。② interfaces.go 在 RDSAdapter 后注册可选接口 RDSMetricQuerier,签名精确 GetRDSMetrics(ctx, rdsID, instanceName, region, engine, startDate, endDate)——带 region(RDS 地域性资源,与 NAS/Disk 同型,不做全局推断)+ engine(多引擎分派);var _ 编译期断言测试锚定参数次序。③ dao/rds_metric.go + rds_metric_query.go 实现 RDSMetricDAO 六方法(UpsertMetric/BulkUpsertMetrics/BulkInsertIfAbsent/CountMetricsByProviders/ListByRDS/ListByAccounts,仿 DiskMetricDAO 平移),构造器创建唯一索引 (account_id, rds_id, date)(Hard Rule 唯一键含 account_id,3cea6d7 多账号修复经验;rds_id 地域内唯一类比 disk_id);写入路径 rdsMetricQC 门禁:三使用率越出 [0,100] 拒绝(错误携带 rds_id/date)、connections 负值拒绝、四指标全 0 例外放行强制打 zero_exception(T5/T8 契约「四指标全 0」,单指标 0 低负载不触发——probe-report 零值 13 行正常业务事实)。④ 装配位(module.go/wire.go)按 AC 留待 T5/T7 未接线。⑤ 测试:17 个顶层测试全绿(dao 单测 9 含 QC 10 子用例 + MONGO_DSN 门控 live 5 + types 2 + cloudx 签名 1);live 对可达实例实跑通过——唯一索引 spec+裸 InsertOne dup-key 拦截、跨账号同 rds_id 同日 3 行并存、首写批重放不覆盖/覆盖批次日补采语义、全 0 行落库可见、ListByRDS 窗口升序/ListByAccounts 跨实例、CountMetricsByProviders 健康口径;独立测试库 ecam_dao_rds_test 整库 Drop 零残留(库清单核对)。

## Changes

### Files Created
- internal/shared/cloudx/types/rds_metric.go
- internal/shared/cloudx/types/rds_metric_test.go
- internal/shared/cloudx/rds_metric_querier_test.go
- internal/cam/repository/dao/rds_metric.go
- internal/cam/repository/dao/rds_metric_query.go
- internal/cam/repository/dao/rds_metric_test.go
- internal/cam/repository/dao/rds_metric_live_test.go

### Files Modified
- internal/shared/cloudx/interfaces.go

### Key Decisions
- engine 落库为模型字段而非仅前端展示:T1 探测定案 proposal「engine 作为元数据透传不参与指标口径分支」假设不成立——aliyun 指标名按引擎前缀分派(SQLServer_*)、huawei CES 维度键按引擎分键(rds_cluster_id/postgresql_cluster_id),engine 是 T3 适配器查询分派的必经依据(probe-report §2)
- zero_exception 触发口径定为「四指标全 0」(CPU/内存/磁盘/连接)而非 Disk 的单指标 0:依据 proposal Key Scenarios + T5「不继承 CDN 全零过滤(四指标全 0 落库打 zero_exception)」+ T8「四指标全 0 原样暴露」三方一致契约;单指标 0(如低负载 CPU=0)是正常业务事实不触发打标
- instance_name 冗余落库(与 DiskMetric.DiskName/NASMetric.FsName 同理由):Top/趋势展示免联资产表,指标行不随实例删除丢失
- gate 复用 disk_metric.go 的 usagePercentMax=100 常量(同值门禁),memory/disk/cpu 三使用率共用一个循环校验,错误消息携带字段名+rds_id+date 供执行器归因
- live 测试读取窗口用 days=30 而非 3:metricDateFloor 相对当日计算,fixture 固定日期(2026-09-12~14)须落在窗口内(首跑 days=3 教训已修正)
- DAO 含读取方法 ListByRDS/ListByAccounts(T8 读取接口依赖),完整平移 Disk 四件套模式;T8 之前不被消费属 DAO 契约完整性

## Test Results
- **Tests Executed**: Yes
- **Passed**: 17
- **Failed**: 0
- **Coverage**: 98.0%

## Acceptance Criteria
- [x] RDSMetric 模型落库字段 rds_id/date/cpu_percent/memory_percent/disk_percent(float64 0~100)/connections(int64)/qc_status(空=正常,zero_exception=全 0 异常行);engine 作为元数据字段
- [x] RDSMetricQuerier 可选接口签名带 region + engine 并在 interfaces.go 注册
- [x] ecam_rds_metric DAO 唯一索引 (account_id, rds_id, date);UpsertMetric/BulkInsertIfAbsent 首写生效语义(今日行不覆盖,昨日行补采覆盖)
- [x] 写入门禁:三使用率限 0~100 越界拒绝,zero_exception 放行打标;connections 非负校验
- [x] 单测:模型序列化、DAO 唯一键隔离(多账号同 rds_id 同日各留一行)/幂等/门禁;live 测试 MONGO_DSN 门控实跑通过
- [x] 装配位(module.go/wire.go)留待 T5/T7 接线,本任务不提前接

## Notes
覆盖率口径:rds_metric.go 8/9 函数 100%(CountMetricsByProviders 85.7% 错误分支未覆盖),rds_metric_query.go normalizeRDSReadDays 100%/ListByRDS 80%/ListByAccounts 83.3%,rds_metric.go 加权约 98%;包级数字被既有无关文件稀释(dao 6.1%),如实记录。静态检查:go build ./... 通过;gofmt 对 8 个触碰文件全净(修复 1 处自建文件对齐);go vet 三包通过;golangci-lint 未安装(仓内既有环境状态,按 NAS/OSS/Disk T2 先例以 go vet 替代);-race 未启用(宿主无 gcc,仓内既有约束)。live 凭证经 env 注入(config/prod.yaml 可达实例),凭证未落任何文件;测试库 ecam_dao_rds_test Cleanup 整库 Drop 后经库清单核对零残留。树上并行会话 WIP(internal/logquery/* 与 index.json)非本任务产物,提交时逐文件显式 add。
