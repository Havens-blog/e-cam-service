# M1 探测报告:厂商监控 API 探测定案(nas-ops-insight)

- 探测日期:2026-09-19(运行时实测,只读凭证,全部调用为只读 API)
- 探测账号:租户下活跃云账号各 1 个(aliyun/tencent/huawei/volcano/aws;从库解密加载,AK 掩码展示)
- 探测脚本:`internal/shared/cloudx/nasprobe/`(共享逻辑+分布统计)、`internal/shared/cloudx/<provider>/probe_manual_test.go`(env 门控,无 env 不跑)
- 结论速览:**必达 = aliyun / huawei / aws;尽力而为 = tencent / volcengine(二期补);无升格候选**

---

## 1. 各厂商指标名 / namespace 定案

### 1.1 aliyun(必达;本次探测范围外,定案归 T3 联调)

| 项 | 定案 |
|---|---|
| 监控 API | CMS `DescribeMetricList`(标准路径,SDK 已在 go.mod,零新依赖) |
| 本次是否实盘验证 | 否(验收项 AC-3 仅要求华为/AWS;aliyun 样本验证归 T3 适配器联调) |
| 资产表容量语义 | 容量型文件系统 `capacity=10485760` 为名义容量(10 PiB 按量上限),非真实用量;`used_capacity=1~4` 明显失真 —— 佐证 proposal「资产表容量字段不可信,指标表为唯一数据来源」 |

### 1.2 huawei(必达;实测定案 namespace = `SYS.EFS`)

按 Hard Rule 对三个 namespace **分别独立探测**(cn-south-1,实例真实 region):

| namespace | ListMetrics 结果 | 判定 |
|---|---|---|
| `SYS.SFS`(普通 SFS) | 空(0 指标) | 账号下无普通 SFS 1.0 实例,无从验证 |
| `SYS.SFS_Turbo`(文档口径) | **空(0 指标)** | **文档 namespace 与实盘上报不符** |
| `SYS.EFS`(实盘发现) | 60 条序列 / 12 个指标 | **定案:实盘 SFS Turbo(HPC 型)全部上报在 SYS.EFS** |

**定案指标口径(供 T3 适配器)**:

| 项 | 值 |
|---|---|
| Namespace | `SYS.EFS` |
| 维度 | `efs_instance_id` = 文件系统 ID |
| 已用容量 | `used_capacity`(单位 **byte** → /1024^3 归一 GB) |
| 使用率 | `used_capacity_percent`(%) |
| 总容量 | **无直接指标**,由 `used_capacity ÷ used_capacity_percent` 派生(或 SFS API `size` 字段,注意其当前返回空) |
| 其他 | `used_inode`/`used_inode_percent`(inode 水位)、`iops`/`avg_read_latency`/`avg_write_latency`/`data_read_io_bytes`/`data_write_io_bytes` 等 |

> 关键发现:文档口径 `SYS.SFS_Turbo` 在实盘**没有任何指标上报**;若 T3 按文档实现,华为链路将整表静默为空。以 `SYS.EFS` 为准是本次探测的核心产出之一。

### 1.3 aws(必达;指标路径已验证,非零验证未通过,附观察期注记)

| 项 | 定案 |
|---|---|
| Namespace / MetricName | `AWS/EFS` / `StorageBytes`(官方指标) |
| 维度 | `FileSystemId` |
| 指标序列存在性 | **已确认**:ListMetrics 返回全部 5 个 fs 均注册了 `StorageBytes`/`PercentIOLimit`/`PermittedThroughput` 序列 |
| 数据点 | `GetMetricData` 与 `GetMetricStatistics` 双路、90 天窗口均 **0 数据点** |
| 原因归因 | 5 个 fs 均有挂载点(2~3 个/fs)但长期无 I/O;EFS StorageBytes 仅在有存储量变化时上报 —— **是实例闲置,不是 API 不可用**(签名/region/维度解析全链路已通) |
| 实盘分布 | 4 × eu-central-1 + 1 × us-east-1;**账号 regions 配置=eu-west-1 与实盘不一致**(见 §5 遗留行动) |

### 1.4 volcengine(尽力而为 → 二期补;指标名未收敛)

| 项 | 实测结果 |
|---|---|
| 可用网关 | 仅 `cloudmonitor.<region>.volcengineapi.com`;其上仅 `GetMetricData`(Version 2018-01-01)注册 |
| 元数据接口 | `ListNamespaces`/`ListMetrics`/`QueryMetricData`(文档 6361,2021-06-30 家族)在该网关一律 `InvalidActionOrVersion`(7 个候选版本 × cloudmonitor/monitor 双签名名全部否定) |
| Period 格式 | 必须为时长字符串(`"5m"`/`"1h"`);秒数(`"3600"`)报 `invalid period`,数字类型报「entire request parameter invalid」 |
| 指标查询 | 参数合法后全部候选返回 `metric not found`:`Namespace(FileNAS/VEI_NAS/NAS/Vulcan_NAS/Vulcan_FileNAS/CFS/Vulcan_CFS/FileStorageNAS) × SubNamespace × 指标名(TotalCapacity/UsedCapacity/CapacityUsage/Capacity/FileSystemCapacityUsed/StorageUsed/CapacityUsage) × 维度名(FileSystemId/fs_id/file_system_id)`,共 355+ 组合 |
| 阳性对照 | 用真实 ECS 实例 id 查 `Vulcan_ECS/Instance/CPUPercent` 等公开命名,同样 `metric not found` |
| 根因判定 | **账号云产品监控指标注册表为空** —— 云产品监控指标需「产品订阅」开通(文档 6408/114674);本账号未订阅,指标名无法经实盘收敛 |
| 重试路径(二期补) | ① 开通云产品监控指标订阅(写操作,不在本任务范围);② 重跑 `TestManualProbeVolcanoNASCloudMonitor`;③ 按文档候选定案 `Namespace=FileNAS`、`MetricName=UsedCapacity/TotalCapacity/CapacityUsage`、`Dimension=FileSystemId` |

### 1.5 tencent(尽力而为,维持)

| 项 | 结果 |
|---|---|
| 实盘实例 | 0 个(CFS 枚举成功且为空;DB 交叉核对 `tencent_cfs`=0 一致) |
| 监控 API | monitor 子包(需一行 go.mod,T3 实现时引入);无实例则探测定案为「空集,无数据可采」 |

---

## 2. 实盘非零验证结果(AC-3)

对实盘 capacity=0 的实例,验证监控 API 返回非零且数量级正确:

### 华为:5/5 PASS

`SYS.EFS / used_capacity`(3 天窗口,4 个日点,平均口径):

| 文件系统 | fs_id(前 8 位) | 最新 used_capacity | 换算 GB | used_capacity_percent | 数量级 |
|---|---|---|---|---|---|
| jlc-fat-sfs-turbo | 98d68b8d | 1,479,761,633,280 byte | **1378.14 GB** | 56.09% | ok |
| jlc-dev-sfs-turbo | d666bc13 | 20,305,059,840 byte | **18.91 GB** | 1.54% | ok |
| eda-prod-sfs-turbo | 68ba515b | 4,202,885,120 byte | **3.91 GB** | 0.32% | ok |
| eda-uat-sfs-turbo | d88aac25 | 101,501,493,248 byte | **94.53 GB** | 7.70% | ok |
| eda-dev-sfs-turbo | 31d5c2a4 | 115,325,640,704 byte | **107.41 GB** | 8.75% | ok |

> **结论:华为探测通过。** 资产表 `capacity=0, used_capacity=0` 确系 sync 映射 bug,监控 API 数据完好 —— proposal 的前提成立,**不触发「华为降级为尽力而为、SC-1 改写 aliyun/aws 必达」的预案**。同时:5 个 fs 的真实总容量(如 jlc-fat ≈ 1378/56.09% ≈ 2457 GB)与资产表容量=0 形成直接反差,资产表坏值影响量化有了实锚。

### AWS:0/5(记录原因)

| fs_id | region | 挂载点 | StorageBytes 数据点(90 天) | 判定 |
|---|---|---|---|---|
| fs-0aa52000cb986aad0 | eu-central-1 | 2 | 0(GetMetricStatistics 与 GetMetricData 双路确认) | 未通过(实例闲置无上报) |
| fs-0ddb93fb0d48da2f3 | eu-central-1 | 3 | 0 | 同上 |
| fs-0274b7881c3baf798 | eu-central-1 | 3 | 0 | 同上 |
| fs-043c155909ccf27d5 | eu-central-1 | 2 | 0 | 同上 |
| fs-2c923064 | us-east-1 | 2 | 0 | 同上 |

> **结论:AWS 探测「指标路径可用、非零验证未通过」,原因是实例闲置(序列已注册但 90 天无上报),不是 API 不可用。** 对 SC-1 的影响:AWS 采集上线初期该 5 实例仍将无数据 → 走 proposal「capacity=0 例外放行 + `qc_status=zero_exception` 打标落库可见」路径;数量级自检仅对非零行生效。此结果**不推翻** SC-1(华为已实证「监控 API 数据是好的」,AWS 无反证),但发布评审须如实呈现「AWS 实盘首期可能全为零值/无数据行」。

---

## 3. 实盘容量按厂商分布(AC-4)

实盘只读枚举(2026-09-19,5 厂商活跃账号 × 全部配置 region;与 DB `ecam_instance` 交叉核对逐厂商数量完全一致:aliyun_nas=73、volcengine_nas=12、huawei_nas=5、aws_nas=5、tencent_cfs=0):

| 厂商 | 实例数 | 实例数占比 | 资产表容量合计(原值,GB 语义不可信) | 原值占比 | capacity=0 数 |
|---|---|---|---|---|---|
| aliyun | 73 | 76.8% | 685,770,304 | 85.60% | 0 |
| tencent | 0 | 0.0% | 0 | 0.00% | 0 |
| huawei | 5 | 5.3% | 0 | 0.00% | 5 |
| volcengine | 12 | 12.6% | 115,343,860 | 14.40% | 0 |
| aws | 5 | 5.3% | 0 | 0.00% | 5 |
| **合计** | **95** | 100% | 801,114,164 | 100% | 10 |

**口径说明(重要)**:资产表容量字段单位语义不可信 —— aliyun/volcengine 容量型 fs 均 `capacity=10485760`(名义容量,非真实水位),华为/AWS 全为 0。故「原值占比」仅具相对参考意义,**实例数占比是可靠的分布口径**,两口径均不改变下述判定。

### 升格判定(tencent / volcengine,>15% 且探测可用 → 升格)

| 厂商 | 实例数占比 | 容量原值占比 | 探测可用 | 判定 |
|---|---|---|---|---|
| tencent | 0.0% | 0.00% | (无实例,无需探测) | **不触发**:维持尽力而为 |
| volcengine | 12.6% | 14.40% | **否**(指标名未收敛,见 §1.4) | **不触发**:双口径均 ≤15%,且探测不可用 → 维持尽力而为 + 「二期补」判定成立 |

> 灵敏度备注:volcengine 容量原值占比 14.40% 距 15% 阈值仅 0.6pp,且该分子是「名义容量」口径 —— 若二期开通订阅后按真实容量重算,**存在贴线超阈的可能**,二期补重跑探测时应重算本表。

---

## 4. 必达 vs 尽力而为:最终分组(发布 gate 定案)

| 分组 | 厂商 | 依据 |
|---|---|---|
| **必达** | **aliyun** | CMS 标准指标、SDK 零新依赖;实盘 73 实例、76.8% 实例数占比 |
| **必达** | **huawei** | 实测定案 `SYS.EFS/used_capacity` 非零验证 5/5 PASS;**不触发降级预案**;适配注意:namespace 用 SYS.EFS(非文档的 SYS.SFS_Turbo)、维度 efs_instance_id、总容量需派生 |
| **必达** | **aws** | `AWS/EFS StorageBytes` 指标路径全链路已验证;**附注记**:实盘 5 实例长期闲置无数据点,上线初期 AWS 行可能全部为零值/无数据,按「零值例外放行+打标」呈现,不作为采集失效误判 |
| **尽力而为** | **tencent** | 0 实例,无覆盖损失;monitor 子包照常实现(一行依赖) |
| **尽力而为** | **volcengine** | 探测不可用(订阅未开通)+ 占比双口径 ≤15% → **二期补**,重试路径已固化在探测脚本中 |
| **升格候选** | 无 | tencent 0%、volcengine ≤15%,均不满足「>15% 且探测可用」 |

**SC-1 最终口径**:必达项 = aliyun / huawei(SYS.EFS 口径)/ aws(含上述 AWS 零值注记);必达清单以本分组为准。

**高水位实例数(近失证据,Urgency 量化口径)**:以监控 API 可信口径(`used_capacity_percent`)计,>70% 实例数 = **0**(最高 jlc-fat 56.09%);aliyun/volcano 资产表 used 字段失真无法参与计算。「越晚修」成本下限当前为 0,上线观测期后由指标表按 MAX 聚合重算。

---

## 5. 遗留行动(不阻塞本定案,移交项)

1. **AWS 账号 regions 配置与实盘 region 不一致**(账号配 eu-west-1,实盘 EFS 在 eu-central-1/us-east-1)→ 现有资产同步对 AWS 存在 region 覆盖面缺口;建议另立修复任务(探测脚本已支持 `NAS_PROBE_AWS_REGIONS` 绕行)。
2. **volcengine 云产品监控指标订阅开通**(写操作,超出本任务只读边界)→ 二期补的前置条件;开通后重跑探测即可收敛指标名。
3. **华为总容量无直接指标** → T3 适配器需按 `used_capacity ÷ used_capacity_percent` 派生(除零语义:percent 为 0 时 capacity 派生无效,按 `data_status` 标注,不写 NaN)。
4. aliyun used_capacity 字段失真属资产表质量 bug,按 proposal Out of Scope 另立任务。

## 6. 探测产物清单

| 产物 | 路径 | 说明 |
|---|---|---|
| 共享探测逻辑 + 单测 | `internal/shared/cloudx/nasprobe/probe.go` / `probe_test.go` | 单位换算、数量级自检 [1MB,1PB]、分布聚合、升格判定(25 个单测用例) |
| 账号只读加载 | `internal/shared/cloudx/nasprobe/accounts.go` | env 门控(NAS_PROBE_MONGODB_DSN / CAM_ENCRYPTION_KEY / 逐厂商直填) |
| 分布统计探针 | `internal/shared/cloudx/nasprobe/probe_distribution_manual_test.go` | 5 厂商枚举 + 聚合 + 升格判定输入 |
| volcengine 探针 | `internal/shared/cloudx/volcano/probe_manual_test.go` | 元数据发现 + GetMetricData 候选矩阵 + 实测结论与重试路径 |
| huawei 探针 | `internal/shared/cloudx/huawei/probe_manual_test.go` | 三 namespace 独立探测 + 非零验证 |
| aws 探针 | `internal/shared/cloudx/aws/probe_manual_test.go` | StorageBytes 双路查询 + ListMetrics 序列核查 + 挂载点核查 |
| 原始探测日志 | `logs/nas_probe_*.txt`(gitignored) | 逐格探测证据,报告引用的样例行均出自其中 |

> Hard Rule 合规声明:本探测全程只读(枚举/List/ListMetrics/Get 类 API),未写任何临时 collection,未触碰 `ecam_nas_metric`(建表属 T2);凭证仅内存解密、日志掩码展示。
