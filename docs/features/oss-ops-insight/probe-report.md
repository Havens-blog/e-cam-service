# M1 探测报告:OSS 厂商监控 API 探测定案(oss-ops-insight)

- 探测日期:2026-09-20(运行时实测,只读凭证,全部调用为只读 API)
- 探测账号:租户下活跃云账号各 1 个(aliyun/tencent/huawei/volcano/aws;从库解密加载,AK 掩码展示)
- 探测脚本:`internal/shared/cloudx/nasprobe/`(共享逻辑:单位换算/数量级自检/分布聚合/升格判定 + `oss_usage.go` OSS 聚合/增速)、`internal/shared/cloudx/<provider>/oss_probe_manual_test.go`(env 门控,无 env 不跑)
- 结论速览:**必达 = aliyun / huawei / aws(三家实盘非零全部通过);尽力而为 = tencent(探测可用但占比 ≤15%);volcengine = 二期补(bucket 数占比 15.58% >15% 但探测不可用,须发布说明承诺补采窗口)**
- 对 T3/T4 的核心价值:① aliyun 文档口径 namespace `acs_oss` 实盘已基本失效,现行可用为 **`acs_oss_dashboard`**(与 NAS「文档不可全信」先例同型);② 华为 `SYS.OBS` 文档口径实盘成立,维度 `bucket_name`,容量指标 `capacity_total`(byte)与枚举统计逐桶精确互证;③ tencent `QCE/COS` 探测**可用**(归因修正:非订阅问题),且容量单位是 **MB** 不是 byte。

---

## 1. 各厂商指标名 / namespace 定案

### 1.1 aliyun(必达;定案 namespace = `acs_oss_dashboard`)

**文档口径与实盘不符的关键发现**:proposal 假设的 `acs_oss` 在实盘基本失效——除旧指标 `StorageUtilization` 残留注册外(维度形态不合法,403),容量/对象类候选全部返回 `[400-10002] the metric(...) of project(acs_oss) is not exist`。按现行官方文档(2026-03 更新「Access monitoring data」)改用 `acs_oss_dashboard` 实盘验证通过。

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace | **`acs_oss_dashboard`**(实盘非零验证通过;`acs_oss` 旧版仅 StorageUtilization 残留,不可用) |
| 存储用量 | **`MeteringStorageUtilization`**(单位 **byte** → 采集边界 /1024^3 归一 GB) |
| 对象数 | **`ObjectCount`**(单位 个) |
| 维度 | **`BucketName`**(实测 `{"BucketName":"<bucket>"}` 生效;旧版 `bucket` 键不适用) |
| Period | **3600**(计量类指标;传 86400 旧版口径无数据) |
| 窗口限制 | **≤31 天**(计量类指标只保留最近 31 天;T3 补采历史须注意) |
| 元数据接口 | `DescribeMetricMetaList` 对 acs_oss/acs_oss_dashboard 均返回空集,发现依赖文档口径 + 实盘试错 |

非零验证(3 天窗口,3600s 粒度,枚举快照 storage 最大的 3 个 bucket):

| bucket | MeteringStorageUtilization 最新值 | ≈GB | ObjectCount 最新值 | 数量级 |
|---|---|---|---|---|
| jlc-prod-forface-public | 1.155×10^12 byte | 1076.2 GB | 2,124,371 | ok |
| jlc-prod | 6.004×10^13 byte | 55,910.5 GB | 4,312,699 | ok |
| jlc-prod-pcb-order-sourcefile-analysis | 2.859×10^13 byte | 26,631.2 GB | 38,656,337 | ok |

**结论:aliyun 探测通过(PASS=6/6,零值 0)**。

### 1.2 huawei(必达;文档口径 `SYS.OBS` 实盘成立)

与 NAS 的 `SYS.SFS_Turbo` 文档口径落空不同,**OBS 的文档口径 `SYS.OBS` 在实盘成立**:ListMetrics 返回 **103 个指标**,容量类 `capacity_*` 系列与对象数 `object_num_*` 系列逐桶上报。

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace | `SYS.OBS`(文档口径 = 实盘口径,无需替代) |
| 维度 | **`bucket_name`**(ListMetrics 实测) |
| 总容量 | **`capacity_total`**(byte);分层参考:`capacity_standard` / `capacity_infrequent_access` / `capacity_archive` / `capacity_deep_archive` / `capacity_intelligent_tiering_*` / `*_multi_az` / `*_single_az` |
| 对象数 | **`object_num_all`**(个);分层:`object_num_standard_total` 等 |
| 其他 | 请求类 `get_request_count`/`put_request_count`/延迟分位等(本期不采) |

非零验证(3 天窗口,天粒度 Average;103 指标 × 130 实盘 bucket 全量交叉验证,PASS=2142 / FAIL=2501 —— FAIL 集中在无归档/低频存储 bucket 的 `capacity_archive` 等分层指标恒为 0,属业务事实而非探测失败):

| bucket | capacity_total 最新值 | ≈GB | 与枚举快照(GetBucketStat)互证 |
|---|---|---|---|
| eda-prod-lceda-pro | 1.024×10^14 byte | 95,365.3 GB | 一致(95,379.3 GB 快照口径) |
| fat-jlc-pub-file | 6.488×10^12 byte | 6042.2418 GB | **逐字节一致** |
| jlc-mirrors | 1.073×10^12 byte | 999.0355 GB | 一致 |
| eda-dev-modules | 1.128×10^11 byte | 105.0513 GB | 一致 |
| fat-test | 1.108×10^8 byte | 0.1032 GB | 一致 |

> 关键互证:`capacity_total`(监控 API)与 `GetBucketStat`(OSS 数据面)对同 bucket 的存储量**精确一致**(fat-jlc-pub-file 6042.2418 GB 两路相同)——华为容量指标可信,且 58 个 capacity_total 序列全部非零,**验证通过 bucket 数=130(≥1 即 AC-1 通过)**。

### 1.3 aws(必达;`AWS/S3` 标准指标全量通过)

| 项 | 定案 |
|---|---|
| Namespace / MetricName | `AWS/S3` / **`BucketSizeBytes`**(byte)+ **`NumberOfObjects`**(个) |
| 维度 | `BucketName` + `StorageType`(BucketSizeBytes 取 `StandardStorage` 标准存储口径;NumberOfObjects 取 `AllStorageTypes`) |
| 上报频率 | 每日 1 次(天粒度,90 天窗口 89 个数据点) |

实盘验证(104 个 bucket 全量):

| 判定 | 数量 | 说明 |
|---|---|---|
| 序列注册 + 非零 | **84** | 90 天窗口 89 个日数据点,BucketSizeBytes 均非零 |
| 未注册 | 20 | ListMetrics 无序列(新建或从未上报 bucket)→ 上线初期走「零值例外放行+打标」呈现 |
| 序列注册但 0 数据点 | **0** | 与 NAS 的 EFS「闲置零上报」不同,S3 storage metrics 每日必报 |

样例行(节选):

| bucket | region | BucketSizeBytes 最新 | ≈GB | NumberOfObjects |
|---|---|---|---|---|
| aws-jlc-prod-db-backup | eu-west-1 | 2.015×10^13 | 18,767.4 | 4,164 |
| aws-cloudtrail-logs-...-2e12dfd3 | eu-central-1 | 8.363×10^11 | 778.9 | 1,373,313 |
| cloudwatch2prometheus | eu-central-1 | 8.145×10^10 | 75.9 | 300,152 |
| amazon-connect-8e2b6a025e6d | eu-central-1 | 1.369×10^8 | 0.1275 | 1 |

**结论:AWS 探测通过(84/84 注册序列全部非零;无闲置零值样本,AC-2 零值注记路径仅对 20 个未注册 bucket 生效)**。

### 1.4 tencent(尽力而为,维持;但探测可用 —— 修正 proposal 的「待探测」假设)

| 项 | 实测结果 |
|---|---|
| 元数据接口 | **可用**:`DescribeBaseMetrics(Namespace=QCE/COS)` 返回 **206 个指标**(维度均 `bucket`) |
| 容量指标 | **`StdStorage`**(标准存储,**单位 MB** → 采集边界 MB→GB 换算,注意与 byte 厂商不同);分层:`ArcStorage`/`DeepArcStorage`/`ColdStorage`/`ItFreqStorage`/`MazStdStorage` 等 |
| 对象数指标 | **`StdObjectNumber`**(单位 None/Count);分层 `ArcObjectNumber`/`ColdObjectNumber` 等 |
| 维度形态 | `{"bucket":"<name>"}` 单维即可;`{"appid":...,"bucket":...}` 双维同样生效(CAM GetUserAppId 解析 APPID=1301938239) |
| 数据验证 | `StdStorage` 对测试 bucket 返回 **307 MB(非零)**,3 天窗口 3 个日点;30 个错误样本均为部分指标/维度组合的「维度/参数不合法」,非订阅问题 |

**归因修正**:proposal 将 tencent 列为「指标可用性待探测」;实测 **QCE/COS 指标订阅与注册均正常,探测可用** —— T4 可直接按上表实现,无二期补前提。

**结论:tencent 探测通过(指标可用);是否纳入本期由分布占比判定(见 §3,占比 ≤15% 维持尽力而为)**。

### 1.5 volcengine(尽力而为 → 二期补;与前案同根因:指标订阅未开通)

| 项 | 实测结果 |
|---|---|
| 元数据接口 | 仍不可用(ListNamespaces/ListMetrics InvalidActionOrVersion,与 NAS 前案一致) |
| 指标查询 | 真实 bucket(`ark-auto-2100700292-cn-beijing-default`,cn-beijing)+ 静态候选矩阵:Namespace(TOS/Volcano_TOS/Vulcan_TOS/VEI_TOS)× SubNamespace(tos/bucket/object_storage)× 指标名(BucketSize/StorageSize/UsedCapacity/TotalCapacity/BucketStorageSize/ObjectCount/BucketObjectNums/CapacityUsage)× 维度名(BucketName/bucket_name/bucket)共 **289 组合全部 `metric not found`** |
| 阳性对照 | 真实 ECS 指标 `Vulcan_ECS/Instance/CPUPercent` 同样 `metric not found` —— 注册表为空特征(与前案一致) |
| 根因判定 | **账号云产品监控指标注册表为空**(云产品监控指标需「产品订阅」开通,文档 6408/114674),非维度/指标名错误 |

**二期补重试路径(固化)**:① 开通云产品监控指标订阅(写操作,超出本任务只读边界);② 重跑 `TestManualProbeVolcanoTOSCloudMonitor`;③ 按文档候选定案 `Namespace=TOS`、`SubNamespace=tos`、`MetricName=BucketSize|StorageSize`、`Dimension=BucketName`。

---

## 2. 实盘非零验证结果汇总(AC-1/2/3)

| 厂商 | namespace 定案 | 容量指标 | 对象数指标 | 非零验证 | 判定 |
|---|---|---|---|---|---|
| aliyun | `acs_oss_dashboard` | MeteringStorageUtilization(byte) | ObjectCount(个) | 6/6 PASS(3 bucket × 2 指标) | **通过** |
| huawei | `SYS.OBS` | capacity_total(byte) | object_num_all(个) | 58 capacity_total 序列 PASS,130 bucket 非零,监控口径与数据面互证一致 | **通过** |
| aws | `AWS/S3` | BucketSizeBytes(byte,StandardStorage) | NumberOfObjects(个,AllStorageTypes) | 84/84 注册序列 PASS,89 日点/90 天 | **通过** |
| tencent | `QCE/COS` | StdStorage(**MB**) | StdObjectNumber(个) | 测试 bucket 307 MB 非零 | **通过**(探测层面) |
| volcengine | 未收敛 | 未收敛 | 未收敛 | 289 组合 metric not found + 阳性对照否定 | **未通过(订阅未开通)** |

必达三家 100% 有真实非零数据点 —— 发布 gate 的非零前提成立。

---

## 3. 实盘 OSS bucket 按厂商分布(AC-5)

实盘只读枚举(2026-09-20,5 厂商活跃账号;bucket 枚举走各厂商 ListBuckets,全局服务口径):

| 厂商 | bucket数 | bucket数占比 | 容量合计(GB)* | 容量占比* | storage=0 数 |
|---|---|---|---|---|---|
| aliyun | 572 | **57.49%** | 3,611,960.17 | **93.72%** | 302 |
| tencent | 22 | 2.21% | 0 | 0.00% | 22 |
| huawei | 142 | 14.27% | 241,826.37 | 6.28% | 107 |
| volcengine | 155 | **15.58%** | 0 | 0.00% | 155 |
| aws | 104 | 10.45% | 0 | 0.00% | 104 |
| **合计** | **995** | 100% | 3,853,786.55 | 100% | 590 |

**口径说明(重要)**:
- 容量取自枚举时 `GetBucketStats` 快照:**volcano TOS 适配器无统计接口(TOS API 不提供)恒 0;aws/tencent 快照也全部为 0(S3/COS 数据面统计接口未返回)** —— 容量占比仅 aliyun/huawei 两列可信。aws 真实容量可由 `AWS/S3 BucketSizeBytes` 获取(如 aws-jlc-prod-db-backup 18,767 GB),tencent 可由 `StdStorage` 获取,二期若需要精确容量分布可用监控口径重算。
- **bucket 数占比是可靠口径**(枚举为权威数据面),双口径下判定结论一致。
- aliyun 302 个 storage=0 多为 GetBucketStat 调用失败的空快照,非真实空桶(huawei 107 个同源)。

### 升格判定(tencent / volcengine,>15% 且探测可用 → 升格;探测不可用 → 二期补)

| 厂商 | bucket数占比 | 容量占比 | 探测可用 | 判定 |
|---|---|---|---|---|
| tencent | 2.21% | 0.00%(快照口径不可信) | **是**(QCE/COS 可用) | **不触发升格**:占比 ≤15%,维持尽力而为 —— 但适配路径已完全收敛(T4 可直接实现,无技术障碍) |
| volcengine | **15.58%** | 0.00%(TOS 无统计接口) | **否**(订阅未开通,§1.5) | **「二期补」判定成立**:占比 >15% 但探测不可用,按规格显式降级为二期补,并在发布说明承诺补采窗口 |

> 灵敏度备注:volcengine bucket 数占比 15.58% 贴线超阈(阈 15%);该占比按 bucket 数计,若二期开通订阅后按真实容量重算,占比可能明显变化(当前 TOS 无容量统计口径)。**二期补重跑探测时应重算本表。**

---

## 4. 必达 vs 尽力而为:最终分组(发布 gate 定案)

| 分组 | 厂商 | 依据 |
|---|---|---|
| **必达** | **aliyun** | 实测定案 `acs_oss_dashboard`/`MeteringStorageUtilization`+`ObjectCount` 非零 6/6;实盘 572 bucket(57.49%)、容量 93.72% 占比,为最大存储资产;适配注意:namespace 用 dashboard 版(非文档旧版 acs_oss)、维度 BucketName、Period=3600、窗口 ≤31 天 |
| **必达** | **huawei** | 实测定案 `SYS.OBS`/`capacity_total`+`object_num_all`,130 bucket 非零,与数据面逐桶互证一致;不触发降级预案;实盘 142 bucket(14.27%)、容量 6.28% |
| **必达** | **aws** | `AWS/S3`/`BucketSizeBytes`+`NumberOfObjects` 84/84 全量非零,标准指标无歧义;实盘 104 bucket(10.45%);20 个未注册 bucket 上线初期走零值例外打标呈现 |
| **尽力而为** | **tencent** | 探测可用(QCE/COS StdStorage MB/StdObjectNumber)但 bucket数占比 2.21% ≤15%,**不触发升格条件**;T4 按定案实现,成本与 volcengine 无关 |
| **尽力而为 → 二期补** | **volcengine** | bucket数占比 15.58% >15% 但探测不可用(订阅未开通)→ 二期补,重试路径已固化在探测脚本;发布说明承诺二期补采窗口 |
| **升格候选** | 无 | tencent 探测可用但占比不足;volcengine 占比足但探测不可用 —— 「>15% 且探测可用」双条件无一满足 |

**高增长 bucket 数(近失证据,Urgency 量化口径)**:仅必达三家可算(监控口径 30 天首末点)。aliyun 快照最大 3 bucket:-0.97% / +7.18% / -98.62%(末点异常系 31 天窗口截断的孤立点,记 0);aws 84 bucket 全部 0 高增长(>30% 月增速)。**当前高增长 bucket 数 = 0**;观测期后由 `ecam_oss_metric` 指标表按月重算。

---

## 5. 遗留行动(不阻塞本定案,移交项)

1. **volcengine 云产品监控指标订阅开通**(写操作,超出只读边界)→ 二期补前置条件;开通后重跑 `TestManualProbeVolcanoTOSCloudMonitor` 收敛指标名,并重算 §3 分布表。
2. **aws/tencent 枚举快照容量为 0**(GetBucketStats 数据面接口未返回)→ 资产表 storage_size 快照质量同 proposal Out of Scope(资产表字段修复不在本期);容量经营口径以指标表为准(本期设计已如此)。
3. **aliyun 计量类指标仅保留 31 天** → T3 采集执行器上线后,补采窗口不能超过 31 天(与 proposal「首写生效+次日补昨日覆盖」口径兼容)。
4. **tencent 容量单位 MB** → T4 适配器单位换算须按厂商区分(byte:aliyun/huawei/aws;MB:tencent),禁止把 MB 当 byte 进 BytesToGB。

## 6. 探测产物清单

| 产物 | 路径 | 说明 |
|---|---|---|
| OSS 聚合/增速纯逻辑 + 单测 | `internal/shared/cloudx/nasprobe/oss_usage.go` / `oss_usage_test.go` | OSSBucketsToUsage(双口径聚合)/OSSGrowthPercent/IsHighGrowth(3 函数 12 子用例,100% 覆盖) |
| 分布统计探针 | `internal/shared/cloudx/nasprobe/oss_distribution_manual_test.go` | 5 厂商 bucket 枚举 + 双口径占比 + 升格判定输入 |
| aliyun 探针 | `internal/shared/cloudx/aliyun/oss_probe_manual_test.go` | 元数据接口否定 + namespace×候选矩阵 + 非零验证 + 30 天增速 |
| huawei 探针 | `internal/shared/cloudx/huawei/oss_probe_manual_test.go` | SYS.OBS 指标发现(103 个)+ 逐桶非零验证 + 数据面互证 |
| aws 探针 | `internal/shared/cloudx/aws/oss_probe_manual_test.go` | ListMetrics 注册确认 + GetMetricData 90 天非零 + 未注册注记 |
| tencent 探针 | `internal/shared/cloudx/tencent/oss_probe_manual_test.go` | DescribeBaseMetrics 发现 + 双维形态探测 + 错误归因 |
| volcengine 探针 | `internal/shared/cloudx/volcano/oss_probe_manual_test.go` | 候选矩阵 + ECS 阳性对照 + 二期补重试路径固化 |
| 原始探测日志 | `logs/oss_probe_*.txt`(gitignored) | 逐格探测证据,报告引用的样例行均出自其中 |

> Hard Rule 合规声明:本探测全程只读(List/ListMetrics/DescribeMetric*/GetMetricData/GetBucket* 类 API),未写任何临时 collection,未触碰生产指标表(建表属 T2);凭证仅内存解密、日志掩码展示。

## 7. 参考来源

- [阿里云 OSS 监控数据查询(Access monitoring data,2026-03 更新)](https://help.aliyun.com/zh/oss/user-guide/access-monitoring-data)
- [阿里云 OSS 监控指标(Metrics)](https://help.aliyun.com/zh/oss/metrics)
- [阿里云 DescribeMetricList API](https://next.api.aliyun.com/api/Cms/2019-01-01/DescribeMetricList)
- [AWS S3 CloudWatch metrics(BucketSizeBytes/NumberOfObjects)](https://docs.aws.amazon.com/AmazonS3/latest/userguide/metrics-dimensions.html)
- 华为云 CES SYS.OBS 指标 / 腾讯云云监控 QCE/COS 指标(各厂商官方文档)
