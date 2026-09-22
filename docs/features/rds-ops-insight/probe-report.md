# M1 探测报告:RDS 厂商监控 API 探测定案(rds-ops-insight)

- 探测日期:2026-09-22(运行时实测,只读凭证,全部调用为只读 API)
- 探测账号:租户下活跃云账号各 1 个(aliyun/tencent/huawei/volcano/aws;从库解密加载,AK 掩码展示)
- 探测脚本:`internal/shared/cloudx/nasprobe/`(共享:RDSToUsage 分布聚合/MemoryPercentFromFreeable AWS 换算/MemoryPercentFromUsed 直接口径换算/CheckUsagePercentRange 归一门禁)、`internal/shared/cloudx/<provider>/rds_probe_manual_test.go`(env 门控,无 env 不跑)、`nasprobe/rds_distribution_manual_test.go`(分布统计)
- 结论速览:**必达 = aliyun / huawei / aws(三家实盘非零全部通过);尽力而为 = tencent(实盘 0 实例、占比 0% ≤15%,QCE/CDB 指标已确认实盘注册名,二期路径清晰);volcengine = 二期补(占比 2.08% ≤15%,且探测不可用——订阅未开通,与前四案同根因)**
- 对 T3/T4 的核心价值:① huawei `SYS.RDS` 实盘确认(172 指标),但**维度键按引擎分键**(mysql=`rds_cluster_id`、pg=`postgresql_cluster_id`),适配器必须按 engine 分派维度;② aws 无直接内存/磁盘使用率,FreeableMemory/FreeStorageSpace 字节换算公式实盘成立(内存换算 5/5 PASS,90~96% 合理);③ aliyun `acs_rds_dashboard` 四指标 %直给全通过,但**指标名按引擎前缀分派**(MySQL 无前缀 / SQLServer 用 `SQLServer_*`);④ 实盘 RDS 规模很小(48 实例),且 aliyun 枚举快照 Storage 全 0(资产表字段未映射,见遗留行动)。

---

## 1. 各厂商指标名 / namespace 定案

### 1.1 aliyun(必达;定案 namespace = `acs_rds_dashboard`,四指标 %直给)

文档口径 `acs_rds_dashboard` 实盘成立(与 proposal 假设一致)。DescribeMetricMetaList 发现 CPU/内存/磁盘/连接类指标 **79 条**,逐格非零验证:

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace | **`acs_rds_dashboard`**(实盘验证通过) |
| CPU | **`CpuUsage`**(%,实例级;SQLServer 另有 `SQLServer_CpuUsage` 同值口径) |
| 内存 | **`MemoryUsage`**(%,**直给,无需换算**;MySQL 实盘样本 39.43~81.74%) |
| 磁盘 | **`DiskUsage`**(%,MySQL 实盘样本 23.86~72.16%) |
| 连接数 | **`ConnectionUsage`**(%,**连接使用率口径**;另有 `MySQL_Connections` 类 count 指标,Group* 为分组聚合维度 `userId,groupId`) |
| 引擎分派 | **指标名按引擎前缀分派**:MySQL 用无前缀通用名;SQLServer 用 `SQLServer_CpuUsage` / `SQLServer_Memory_Usage` / `SQLServer_DiskUsage` / `SQLServer_IOPS`(另有 `_Cluster_Secondary` 副本变体) |
| 单位注意 | `MySQL_DataDiskSize`/`MySQL_InstanceDiskSize` 等容量类单位 **MiB**(非 GB),如需容量水位须换算;IOPS 类 `MySQL_IOPS`(count)/`SQLServer_IOPS`(count/s)单位不一致 |

非零验证(3 天窗口天粒度,5 实例覆盖 MySQL+SQLServer 双引擎):

| 判定 | 数量 |
|---|---|
| 指标非零 PASS | **84**(零值 13、空窗口 298 为低负载/无该引擎指标的正常业务事实) |

样例行:`rm-wz900ae…(MySQL) CpuUsage 0.043% / MemoryUsage 81.738% / DiskUsage 64.562% / ConnectionUsage 2.334%`;`rm-wz91lwc7…(SQLServer) SQLServer_CpuUsage 11.696% / SQLServer_Memory_Usage 85.363%`。

**结论:aliyun 探测通过(AC-3)**;四指标均有 %直给实例级指标,归一成本最低。

### 1.2 huawei(必达;定案 SYS.RDS + 引擎分键维度)

文档口径 namespace `SYS.RDS` **实盘成立**(172 指标注册,ListMetrics 全量发现)。与 proposal 假设的关键差异在**维度键形态**与**指标名**:

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace | **`SYS.RDS`**(实盘 172 指标;文档口径与实盘一致,无需替代 namespace) |
| CPU | **`rds001_cpu_util`**(%,MySQL+PG 双引擎同指标名;实盘样本 1.33~15.7%) |
| 内存 | **`rds002_mem_util`**(%,**直给**;实盘样本 27.41~82.06%)——文档口径 `mem_usedPercent` 未注册,以实盘为准 |
| 磁盘 | **`rds039_disk_util`**(%,实盘样本 5.39~89.32%;另有 `rds040_transaction_logs_usage` 事务日志用量) |
| 连接数 | **`rds006_conn_count`(count,总连接)/ `rds007_conn_active_count`(count,活跃)/ `rds042_database_connections`(count,PG)/ `rds072_conn_usage`/`rds083_conn_usage`(%,连接使用率)** |
| **维度键(关键差异)** | **按引擎分键:mysql=`rds_cluster_id`、postgresql=`postgresql_cluster_id`** —— 同一指标两条维度族,T3 必须按 engine 选择维度键,不能假设统一 `instance_id` |
| 内存字节口径(备选) | `os_mem_size_used`(MiB,已用)+ `os_mem_size_spec`(MiB,总量)可换算使用率——`rds002_mem_util` 直给优先,字节口径作兜底 |
| 引擎专属指标 | PG 系(active_connections/long_transactions_*/os_cpu_process_postmaster 等)与 MySQL 系(rds008_qps/rds010~038 innodb/myisam 等)各成一族,佐证「engine 元数据分派」必要性 |

非零验证(3 天窗口天粒度 Average,13 实例全量覆盖 mysql=8/pg=5,每指标最多 30 序列):

| 判定 | 数量 |
|---|---|
| 指标非零 PASS | **383** |
| FAIL | **0** |

样例行:`rds001_cpu_util 4.34%(mysql)/ 4.28%(pg)`;`rds002_mem_util 79.62%(mysql)`;`rds039_disk_util 89.32%(mysql,高水位样本=近失证据)`;`rds042_database_connections 99.6(pg)`。

**结论:huawei 探测通过(AC-1);SYS.RDS 四指标齐备,但适配器须按 engine 分派维度键(实盘定案,AC-5 多引擎结论的核心证据)。**

### 1.3 aws(必达;`AWS/RDS` 标准指标全量通过 + FreeableMemory 换算公式实盘成立)

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace | **`AWS/RDS`**(维度 `DBInstanceIdentifier`) |
| CPU | **`CPUUtilization`**(%,直给;实盘样本 3.06~8.67%) |
| 内存 | **`FreeableMemory`**(byte,Average;**无直接使用率**,换算公式见下) |
| 磁盘 | **`FreeStorageSpace`**(byte,Average;**无直接使用率**,换算公式见下) |
| 连接数 | **`DatabaseConnections`**(个,绝对值直给;实盘样本 17.4~118.05) |
| 内存换算公式 | **memory% = (1 − FreeableMemory/TotalMemory) × 100**,TotalMemory 由 `DBInstanceClass` 规格映射(探测用常见规格表:db.m7g.large=8GiB 等,实盘 5 实例全部 db.m7g.large);实盘验证 **5/5 PASS**(90.52~95.75%,与 MySQL buffer pool 常驻特征吻合,公式数值合理) |
| 磁盘换算公式 | **disk% = (1 − FreeStorageSpace/(AllocatedStorage×GiB)) × 100**(AllocatedStorage 取枚举快照 Storage 字段;实盘 5 实例换算 82.08~85.41%,合理) |
| 规格表注意 | db.m7g 系(Graviton)规格表必须收录(实盘全部为 m7g);T3 固化完整规格表或改用实例元数据 API 取总内存 |

实盘验证(5 实例,eu-central-1;账号 regions 配 eu-west-1 与实盘不一致,`NAS_PROBE_AWS_REGIONS` 扩展后收敛——与 Disk 前案同型,探测脚本已留 region 覆盖机制):

| 判定 | 数量 |
|---|---|
| 四指标非零 PASS | **20**(FAIL=0,3 天窗口全部有数据点) |
| 内存换算 PASS | **5**(SKIP/FAIL=0) |

**结论:aws 探测通过(AC-2);四指标实盘非零 + 内存换算公式实盘成立,AC-2 关键决策点(内存口径)定案为「FreeableMemory 换算」。**

### 1.4 tencent(尽力而为,维持;实盘 0 RDS 实例 —— 无实例可验证,元数据已归因)

| 项 | 实测结果 |
|---|---|
| 实盘枚举 | 6 个主流 region(ap-guangzhou/shanghai/beijing/chengdu/nanjing/hongkong)全部 **0 RDS 实例**——非枚举失败(ListInstances 正常返回空) |
| 元数据接口 | DescribeBaseMetrics(`QCE/CDB`)**正常返回 206 条指标注册**——云监控数据权限与 namespace 订阅均正常 |
| 指标注册名(实盘确认) | **`CpuUseRate`(%)/ `MemoryUseRate`(%)/ `CapacityRate`(%)/ `ConnectionUseRate`(%)**(均为百分比直给);`MemoryUse`(MBytes,已用字节口径兜底);维度 `instanceid,insttype`——**文档候选名 `CpuUsage`/`MemoryUsage`/`DiskUsage` 与实盘注册名不符**,T4 若落地须用实盘名 |
| 根因判定 | **账号无 RDS 实例**(非权限/订阅问题,元数据接口健康)——「指标订阅未开通 vs 指标不存在」归因结论:指标存在且已注册,仅无实例 |

**二期补路径(固化)**:① 账号购入/纳管 RDS 实例后重跑 `TestManualProbeTencentQCEDBMetrics`;② 直接按实盘注册名定案(`CpuUseRate`/`MemoryUseRate`/`CapacityRate`/`ConnectionUseRate` + 维度 `instanceid`,候选矩阵已在脚本);③ QCE/CDB 仅覆盖 MySQL 主实例,其他引擎(TDSQL 等)须另核 namespace。

### 1.5 volcengine(尽力而为 → 二期补;与前四案同根因:云产品监控指标订阅未开通)

| 项 | 实测结果 |
|---|---|
| 实盘枚举 | 1 个 RDS MySQL 实例(`mysql-a6ff2274eff8`,345GB,running) |
| 指标查询 | 真实实例维度值 + 静态候选矩阵:Namespace(Volcano_RDS/Vulcan_RDS/RDS/RDS_MySQL/Volcano_MySQL/MySQL)× SubNamespace(rds/mysql/instance/db_instance)× 指标名(15 个 CPU/内存/磁盘/连接候选)× 维度名(instance_id/InstanceId/rds_id/db_instance_id/ResourceID)共 **1800 组合全部无数据** |
| 元数据接口 | ListNamespaces 在 v2(2021-06-30)与 v1(2018-01-01)两版本均 `InvalidActionOrVersion`(网关未开放该元数据动作) |
| 阳性对照 | 真实 ECS 指标 `Vulcan_ECS/Instance/CPUPercent` 同样 `metric not found` —— 注册表为空特征(与 NAS/OSS/Disk 前案一致) |
| 根因判定 | **账号云产品监控指标注册表为空**(云产品监控指标需「产品订阅」开通),非维度/指标名错误 |

**二期补重试路径(固化)**:① 开通云产品监控指标订阅(写操作,超出本任务只读边界);② 重跑 `TestManualProbeVolcanoRDSMetrics`;③ 命中后按实盘指标名收敛定案(候选矩阵已在脚本)。**占比裁定:实盘 1 实例(2.08%)/345GB(1.38%)均 ≤15% → 不触发「>15% 且探测不可用」的显式降级承诺条款,按普通「尽力而为→二期补」记录。**

---

## 2. 实盘非零验证结果汇总 + 口径归一(AC-3/AC-5)

| 厂商 | namespace 定案 | CPU | 内存 | 磁盘 | 连接数 | 非零验证 | 判定 |
|---|---|---|---|---|---|---|---|
| aliyun | `acs_rds_dashboard` | CpuUsage(%) | MemoryUsage(**%直给**) | DiskUsage(%) | ConnectionUsage(% 使用率) | PASS=84 | **通过** |
| huawei | `SYS.RDS` | rds001_cpu_util(%) | rds002_mem_util(**%直给**) | rds039_disk_util(%) | rds006/007/042(count)+rds083(%) | PASS=383 | **通过** |
| aws | `AWS/RDS` | CPUUtilization(%) | FreeableMemory(byte→**换算**) | FreeStorageSpace(byte→**换算**) | DatabaseConnections(个) | PASS=20+换算 5 | **通过** |
| tencent | `QCE/CDB`(206 指标注册) | CpuUseRate(%) | MemoryUseRate(%) | CapacityRate(%) | ConnectionUseRate(%) | 无实例可验证 | **未验证(无实例)** |
| volcengine | 未收敛 | 未收敛 | 未收敛 | 未收敛 | 未收敛 | 0(1800 组合) | **未通过(订阅未开通)** |

必达三家 100% 有真实非零数据点 —— 发布 gate 的非零前提成立。

### 内存口径归一(AC-5 定案,本任务关键决策点)

| 厂商 | 内存口径 | 归一方案 |
|---|---|---|
| aliyun | `MemoryUsage` **%直给**(MySQL);SQLServer 用 `SQLServer_Memory_Usage`(%,引擎分派指标名) | 直接入库 `memory_percent`,0~100 门禁 |
| huawei | `rds002_mem_util` **%直给**(双引擎同名);`os_mem_size_used/spec`(MiB)字节口径作兜底 | 直接入库;换算函数 `MemoryPercentFromUsed` 已备 |
| aws | `FreeableMemory`(byte)→ **换算公式 `(1−Freeable/Total)×100` 实盘成立**(5/5 PASS) | 换算函数 `nasprobe.MemoryPercentFromFreeable` 固化(T3 引用),TotalMemory 由 DBInstanceClass 规格表映射 |
| tencent | `MemoryUseRate` %直给(实盘注册名,未验证数据点) | 二期落地时直给入库 |
| volcengine | 未收敛(订阅未开通) | 二期补后定案 |

**结论:归一口径 = 百分比 0~100;仅 aws 需要换算(公式已实盘验证),其余厂商直给。换算公式单测覆盖(`rds_usage_test.go`,3 函数 100% 分支覆盖)。**

### 多引擎口径确认(AC-5 定案)

实盘引擎分布:aliyun mysql=23/sqlserver=5/postgresql=1;huawei mysql=8/postgresql=5;volcano mysql=1。**差异大,适配器必须按 engine 分派**,证据:

| 厂商 | 引擎间差异 | 分派方式 |
|---|---|---|
| aliyun | 指标名前缀不同:MySQL 无前缀 / SQLServer 用 `SQLServer_*` | 按 engine 前缀分派指标名 |
| huawei | **维度键不同:mysql=`rds_cluster_id` / pg=`postgresql_cluster_id`**(同名指标两条维度族) | 按 engine 分派维度键 |
| aws | 单引擎实盘(mysql),指标名无引擎前缀 | 无需分派(规格表按 class) |
| tencent | QCE/CDB 仅覆盖 MySQL 主实例 | 其他引擎二期另核 |

**对 proposal 的口径修正(实盘为准)**:proposal「engine 作为元数据透传不参与指标口径分支」的默认假设**不成立**——aliyun(指标名)与 huawei(维度键)的引擎差异是查询必经路径,T3/T4 必须实现按 engine 分派(探测报告 §2 证据支撑)。

---

## 3. 实盘 RDS 按厂商分布(AC-6)

实盘只读枚举(2026-09-22,5 厂商活跃账号;地域性资源逐账号逐 region 枚举;aws 账号 regions 配 eu-west-1 与实盘 eu-central-1 不一致,以 NAS_PROBE_AWS_REGIONS 扩展覆盖;存储取枚举快照 `RDSInstance.Storage`,单位 GB 直加):

| 厂商 | 实例数 | 实例数占比 | 存储合计(GB) | 存储占比 | 引擎分布 | storage=0 数 |
|---|---|---|---|---|---|---|
| aliyun | 29 | **60.42%** | 0.00(注 1) | 0.00(注 1) | mysql=23, sqlserver=5, postgresql=1 | 29 |
| huawei | 13 | **27.08%** | 2,000.00 | 7.98% | mysql=8, postgresql=5 | 0 |
| aws | 5 | 10.42% | **22,730.00** | **90.65%** | mysql=5(全部 db.m7g.large) | 0 |
| volcano | 1 | 2.08% | 345.00 | 1.38% | mysql=1 | 0 |
| tencent | 0 | 0.00% | 0.00 | 0.00% | — | 0 |
| **合计** | **48** | 100% | **25,075.00** | 100% | mysql=37, pg=6, sqlserver=5 | 29 |

> 注 1:**aliyun 枚举快照 Storage 字段全 0**(资产表 `DBInstanceStorage` 字段未映射进 `RDSInstance.Storage`,适配器既有缺陷,见 §5 遗留行动)——存储占比口径对 aliyun 失真,双口径判定以**实例数占比为主口径**,存储占比作参考;修复后重算不影响必达分组结论(aliyun 实例数占比 60.42% 已远超阈值)。

### 升格判定(tencent / volcengine,>15% 且探测可用 → 升格;否则维持尽力而为并记录理由)

| 厂商 | 实例数占比 | 存储占比 | 探测可用 | 判定 |
|---|---|---|---|---|
| tencent | 0.00% | 0.00% | 指标已注册但无实例可验证 | **不触发升格**(占比 ≤15% 双口径均远低于阈):维持尽力而为;二期路径=纳管实例后重跑(实盘指标名已确认,见 §1.4) |
| volcengine | 2.08% | 1.38% | **否**(订阅未开通,§1.5) | **不触发升格**(占比 ≤15%):维持尽力而为→二期补;与前四案同根因,二期补路径已固化 |

**与 Disk 前案的差异说明**:Disk 实盘 volcengine 占比 50.96%(>15%)触发了「显式降级+发布说明承诺」条款;RDS 实盘 volcengine 仅 1 实例(2.08%),双口径均 ≤15%,按规格维持「尽力而为并记录理由」,**无需发布说明承诺补采窗口**(无用户可感知的覆盖缺口)。

---

## 4. 必达 vs 尽力而为:最终分组(发布 gate 定案)

| 分组 | 厂商 | 依据 |
|---|---|---|
| **必达** | **aliyun** | 实测定案 `acs_rds_dashboard`:四指标 %直给全通过(PASS=84);实盘 29 实例(60.42%,**最大 RDS 资产**);适配注意:指标名按引擎前缀分派(SQLServer_*),ConnectionUsage 是使用率口径非绝对值,容量类单位 MiB |
| **必达** | **huawei** | 实测定案 `SYS.RDS`(172 指标,PASS=383 FAIL=0);实盘 13 实例(27.08%,第二);适配注意:**维度键按引擎分键(rds_cluster_id/postgresql_cluster_id)**,内存 rds002_mem_util 直给(文档 mem_usedPercent 未注册),rds006/007/042 连接数为绝对值 |
| **必达** | **aws** | `AWS/RDS` 标准指标全量通过(20/20 非零 + 内存换算 5/5);实盘 5 实例(10.42%)但**存储 22,730GB(90.65%,最大存储资产)**;适配注意:内存/磁盘使用率均为字节换算(公式实盘验证),DBInstanceClass 规格表必须含 db.m7g 系,账号 regions 配置漂移(实盘 eu-central-1)→ 采集执行器按实例真实 region 查询 |
| **尽力而为** | **tencent** | 实盘 0 RDS 实例(0.00% 双口径)——无用户可感知覆盖需求;指标已注册(CpuUseRate/MemoryUseRate 等,二期落地成本已探明);**T4 不纳入本期**,二期路径=实例纳管后按实盘注册名直采 |
| **尽力而为 → 二期补** | **volcengine** | 实盘 1 实例(2.08%/1.38% 均 ≤15%)且探测不可用(订阅未开通,与前四案同根因)→ 二期补;不触发显式降级承诺条款(占比不足 15%) |
| **升格候选** | 无 | 「>15% 且探测可用」双条件无一满足(tencent/volcengine 占比均远低于阈) |

**高危实例数(近失证据,Urgency 量化口径)**:样本级证据——aliyun `DiskUsage` 实盘最高 **72.16%**(rm-wz9lgp2b1bn3q68d5)、`MemoryUsage` 最高 **81.74%**(rm-wz900ae5358c3h8ek);huawei `rds039_disk_util` 最高 **89.32%**(rds_cluster_id=624bcf2d…,>80% 高水位);aws 磁盘换算 82.08~85.41%(>80%)。**观测期后由 `ecam_rds_metric` 指标表按 memory/disk_percent >80% 阈值统计全量高危实例数**。

---

## 5. 遗留行动(不阻塞本定案,移交项)

1. **aliyun RDS 枚举快照 Storage 全 0**:资产表 `ecam_instance` 的 `storage` 字段未映射(`DBInstanceStorage` 解析缺失)——T2 建模时 `ecam_rds_metric` 磁盘使用率可直接采(`DiskUsage` %直给不受影响);但「存储容量水位=used/size」精确值与容量分布统计依赖该字段修复,**字段修复不在本期范围**(proposal Out of Scope 已声明),登记资产表字段修复移交项。
2. **volcengine 云产品监控指标订阅开通**(写操作,超出只读边界)→ 二期补前置条件;开通后重跑 `TestManualProbeVolcanoRDSMetrics` 收敛指标名。
3. **tencent RDS 实例纳管** → 账号购入 RDS 后重跑 `TestManualProbeTencentQCEDBMetrics`;实盘注册名(`CpuUseRate`/`MemoryUseRate`/`CapacityRate`/`ConnectionUseRate`,维度 `instanceid`)已在脚本候选矩阵,落地成本低。
4. **aws DBInstanceClass 规格表**:T3 适配器固化完整规格表(实盘 db.m7g.large=8GiB 已验证),或改用 RDS 实例元数据 API 取总内存,避免规格表遗漏(Graviton 新系)。
5. **aws 账号 regions 配置漂移**:账号 regions 配 eu-west-1 而实盘 RDS 在 eu-central-1 —— 采集执行器按实例真实 region 查询(proposal 已定),账号 regions 配置修复不属本期。
6. **huawei 引擎分键维度**:T3 适配器按 engine 分派维度键(mysql=`rds_cluster_id`/pg=`postgresql_cluster_id`),并处理未来引擎扩展(mariadb/sqlserver 的维度键形态须在实例纳管后复核)。

## 6. 探测产物清单

| 产物 | 路径 | 说明 |
|---|---|---|
| RDS 分布聚合/内存换算纯逻辑 + 单测 | `internal/shared/cloudx/nasprobe/rds_usage.go` / `rds_usage_test.go` | RDSToUsage(实例数/存储双口径)/MemoryPercentFromFreeable(AWS 换算)/MemoryPercentFromUsed(直接口径),3 函数 100% 分支覆盖 |
| 分布统计探针 | `internal/shared/cloudx/nasprobe/rds_distribution_manual_test.go` | 5 厂商逐 region RDS 枚举 + 双口径占比 + 引擎/规格分布 + 升格判定输入 |
| aliyun 探针 | `internal/shared/cloudx/aliyun/rds_probe_manual_test.go` | acs_rds_dashboard 元数据发现 + 逐引擎采样(每引擎至少 1 实例)+ 非零验证 + 内存口径判定 |
| huawei 探针 | `internal/shared/cloudx/huawei/rds_probe_manual_test.go` | SYS.RDS ListMetrics 发现 + 实盘维度键形态(引擎分键)非零验证 + 内存口径判定 |
| aws 探针 | `internal/shared/cloudx/aws/rds_probe_manual_test.go` | AWS/RDS 四指标 GetMetricData 非零 + FreeableMemory 换算验证 + 规格表映射 |
| tencent 探针 | `internal/shared/cloudx/tencent/rds_probe_manual_test.go` | DescribeBaseMetrics 发现(206 指标注册)+ 实盘注册名确认 + 无实例归因 |
| volcengine 探针 | `internal/shared/cloudx/volcano/rds_probe_manual_test.go` | 1800 组合候选矩阵 + ListNamespaces 元数据归因 + ECS 阳性对照 + 二期补路径固化 |
| 原始探测日志 | `logs/rds_probe_*.txt`(gitignored) | 逐格探测证据,报告引用的样例行均出自其中 |

> Hard Rule 合规声明:本探测全程只读(ListInstances/DescribeMetricMetaList/DescribeMetricList/ListMetrics/ShowMetricData/GetMetricData/DescribeBaseMetrics 类 API),未写任何临时 collection,未触碰生产表(建表属 T2);凭证仅内存解密、日志掩码展示。

## 7. 参考来源

- [阿里云 RDS 云监控指标(acs_rds_dashboard)](https://help.aliyun.com/zh/rds/developer-reference/view-monitoring-data-of-an-apsaradb-rds-instance)
- [AWS CloudWatch RDS metrics(CPUUtilization/FreeableMemory/FreeStorageSpace/DatabaseConnections)](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/metrics.dimensions.html)
- 华为云 CES SYS.RDS 指标(rds001~rds083 系列)/ 腾讯云云监控 QCE/CDB 指标(各厂商官方文档)
- 前案参照:`docs/features/disk-ops-insight/probe-report.md`(aws region 漂移/volcengine 订阅未开通同根因)、NAS probe SYS.EFS 定案(文档口径与实盘不符先例)
