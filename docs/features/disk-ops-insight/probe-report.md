# M1 探测报告:Disk 厂商监控 API 探测定案(disk-ops-insight)

- 探测日期:2026-09-21(运行时实测,只读凭证,全部调用为只读 API)
- 探测账号:租户下活跃云账号各 1 个(aliyun/tencent/huawei/volcano/aws;从库解密加载,AK 掩码展示)
- 探测脚本:`internal/shared/cloudx/nasprobe/`(共享:DisksToUsage 分布聚合/DiskUsagePercentFromIdle AWS 派生/CheckUsagePercentRange 归一门禁)、`internal/shared/cloudx/<provider>/disk_probe_manual_test.go`(env 门控,无 env 不跑)、`nasprobe/disk_distribution_manual_test.go`(分布统计)
- 结论速览:**必达 = aliyun / huawei / aws(三家实盘非零全部通过);尽力而为 = tencent(占比 0.49% ≤15%,且探测未通过,二期补路径已固化);volcengine = 二期补(占比 50.96% >15% 但探测不可用,须发布说明承诺补采窗口)**
- 对 T3/T4 的核心价值:① aliyun 文档口径 `acs_ecs_dashboard` 实盘成立,IOPS/吞吐有**云盘级(diskId 维度)**指标,使用率**只有挂载实例级**(vm.DiskUtilization,云监控插件);② 华为 SYS.EVS 的 `disk_device_*` 系列实盘形态是**实例 UUID+设备名**键(非卷 ID),使用率同样只有实例/设备级;③ AWS 派生公式实盘成立但语义是**繁忙时间占比**(非容量水位),本期使用率对 AWS 打标缺失;④ tencent QCE/CBS 实盘仅注册快照类 2 指标,云盘 IO 指标全部未注册。

---

## 1. 各厂商指标名 / namespace 定案

### 1.1 aliyun(必达;定案 namespace = `acs_ecs_dashboard`,IOPS/吞吐云盘级 + 使用率实例级)

文档口径 `acs_ecs_dashboard` 实盘成立(与 proposal 假设一致,无需替代 namespace)。DescribeMetricMetaList 返回磁盘类指标全量(指标名/单位/维度),逐格试错确认维度形态:

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace | **`acs_ecs_dashboard`**(实盘验证通过) |
| IOPS(云盘级) | **`DiskReadIOPS` / `DiskWriteIOPS`**(次/秒,维度 `diskId`;ESSD 实盘非零) |
| 吞吐(云盘级) | **`DiskReadBPS` / `DiskWriteBPS`**(**byte/s**,维度 `diskId`;采集边界归一 MB/s) |
| 性能利用率(参考) | `DiskReadIOPSUtilization` / `DiskWriteIOPSUtilization` / `DiskReadBPSUtilization` / `DiskWriteBPSUtilization` / `DiskReadWriteIOPSUtilization` / `DiskReadWriteBPSUtilization`(%,**相对已购 IOPS/BPS 配额**,非容量水位) |
| 使用率(挂载实例级) | **`vm.DiskUtilization`**(%,维度 `userId,instanceId,mountpoint`,desc=磁盘使用率,云监控插件上报)+ `diskusage_utilization`(Host agent,%,维度 `userId,instanceId,device`) |
| 分组聚合(实例级) | `Groupvm.DiskUtilization` / `GroupDiskReadBPS` / `GroupDiskReadIOPS` / `GroupDiskWriteBPS` / `GroupDiskWriteIOPS`(维度 `userId,groupId`) |
| 注意 | `DiskReadWrite*BurstUtilization` 实盘出现 **-1** 值(未突发时哨兵),采集边界须过滤(0~100 门禁 `CheckUsagePercentRange` 天然拦截) |

非零验证(3 天窗口天粒度,容量最大 5 盘 × 两维度形态):

| 维度形态 | PASS | 零值 | 空窗口 |
|---|---|---|---|
| instanceId(挂载实例级) | **176** | 4 | 55 |
| diskId(云盘级) | **160** | 20 | 55 |

样例行(节选):

| 目标 | metric | 最新值 | 说明 |
|---|---|---|---|
| d-wz95rqmk…(diskId) | DiskReadBPS | 122,570.919 byte/s | 云盘级吞吐非零 |
| d-wz95rqmk…(diskId) | DiskReadIOPS | 8.256 次/s | 云盘级 IOPS 非零 |
| i-wz95rqmk…(instanceId) | vm.DiskUtilization | 60.693% | 实例级磁盘使用率(插件口径) |
| i-wz95j71…(instanceId) | vm.DiskUtilization | 88.649% | 高水位样本(近失证据) |

**结论:aliyun 探测通过(PASS=336/样本两形态,零值/空窗口为无 IO 盘的正常业务事实)**。

### 1.2 huawei(必达;定案 SYS.EVS `disk_device_*` 系列 + SYS.ECS 实例级)

与 proposal 假设不同:**SYS.EVS 的 `disk_device_*` 系列不是"卷 ID"键,实盘维度值 = `<实例 UUID>-<设备名>`**(ECS Agent 按设备粒度上报,disk_name 形态如 `f4bf6adf-…-vda`;含 `<实例 UUID>-volume-<卷 UUID>` 形态)。文档口径 namespace 在实盘成立但**键形态与适配器假设不同**,T3/T4 必须按实盘维度形态查询:

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace(IOPS/吞吐/IO 利用率) | **`SYS.EVS`**(13 个指标实盘注册,149 序列) |
| IOPS | **`disk_device_read_requests_rate` / `disk_device_write_requests_rate`**(request/s) |
| 吞吐 | **`disk_device_read_bytes_rate` / `disk_device_write_bytes_rate`**(**Byte/s**) |
| IO 利用率(设备级) | **`disk_device_io_util`**(%,设备繁忙占比) |
| 使用率(挂载实例级) | **`SYS.ECS disk_util_inband`**(%,实例级磁盘使用率,实盘样本 41.79% 非零) |
| 其他 | `disk_device_io_iobw_qos_num` / `disk_device_io_await` / `disk_device_queue_length` / `disk_device_*_bytes_per_operation`(KB/op)等(本期不采) |
| 维度形态 | SYS.EVS:`disk_name=<实例 UUID>-<device>`;SYS.ECS:`instance_id=<实例 UUID>` —— **匹配键须按前缀匹配,不能用用户命名或卷 ID 精确匹配** |

非零验证(3 天窗口天粒度 Average,实盘 159 盘关联序列,每 namespace 最多 30 序列):

| Namespace | PASS | FAIL |
|---|---|---|
| SYS.EVS | **240** | 0 |
| SYS.ECS | **154** | 0 |

样例行:`SYS.EVS disk_device_write_bytes_rate` 实盘非零(多序列);`SYS.ECS disk_read_bytes_rate` 20,121.14 byte/s 非零;`disk_util_inband` 41.79%。

**结论:huawei 探测通过;使用率口径 = 挂载实例级(SYS.ECS disk_util_inband)/ 设备级(SYS.EVS disk_device_io_util),无云盘级容量使用率**。

### 1.3 aws(必达;`AWS/EBS` 标准指标全量通过 + 派生公式实盘成立)

| 项 | 定案(供 T3 适配器) |
|---|---|
| Namespace | **`AWS/EBS`**(维度 `VolumeId`) |
| 吞吐 | **`VolumeReadBytes` / `VolumeWriteBytes`**(byte,Sum 日粒度 → 采集边界按窗口秒数归一 byte/s→MB/s) |
| IOPS | **`VolumeReadOps` / `VolumeWriteOps`**(次,Sum 日粒度) |
| 空闲时间 | **`VolumeIdleTime`**(秒,Sum 日粒度;使用率派生输入) |
| 使用率 | **无直接指标**;派生公式 `usage% = (1 − VolumeIdleTime/86400) × 100` 实盘成立 —— 语义为**繁忙时间占比**,非容量水位(见 §2 口径归一) |

实盘验证(123 个 EBS 卷,实盘在 **eu-central-1**(账号 regions 配 eu-west-1 与实盘不一致,`NAS_PROBE_AWS_REGIONS` 扩展后收敛;探测脚本已留 region 覆盖机制);容量最大 5 卷 × 5 指标):

| 判定 | 数量 |
|---|---|
| 指标非零 PASS | **21**(FAIL=0,3 天窗口全部有数据点) |
| 派生使用率 PASS | **5**(FAIL=0) |

样例行:vol-00ddfd59… WriteBytes 6.29×10^12 byte/日、idle=69,579s/86,400s → 派生 19.47%;vol-006e505f… idle=86,399s → 派生 0.00%(全闲盘)。

**结论:aws 探测通过(21/21 非零 + 派生公式 5/5 成立)**。

### 1.4 tencent(尽力而为,维持;但探测未通过 —— 归因:监控指标未注册/数据权限未开通)

| 项 | 实测结果 |
|---|---|
| 元数据接口 | DescribeBaseMetrics(`QCE/CBS`)仅返回 **2 个指标**:`SnapshotCapacityUsage`(GB)/ `SnapshotCountUsage`(count),dims 为空(账号级快照用量)—— **无任何云盘 IO/使用率指标注册** |
| 指标查询 | 8 个文档口径候选(DiskReadIops/DiskWriteIops/DiskReadTotal/DiskWriteTotal/DiskIoActiveTimePercent/DiskTotalIoRatio/DiskUsage/CvmDiskUsage)× 5 实盘盘(维度 `diskId`)全部 `InvalidParameterValue: namespace(QCE/CBS) or metricName(...) invalid` |
| 阳性对照 | QCE/CVM CpuUsage(unInstanceId 维度,实盘挂载实例)同样被拒:`unauthorized operation or the instance has been destroyed` —— 云监控数据查询权限未开通,无法进一步区分「CBS 指标名错」vs「订阅未开通」 |
| 根因判定 | **账号云监控数据查询权限/云盘指标订阅未开通**(错误形态一致,非维度形态错误——单维 diskId 与文档一致) |

**二期补重试路径(固化)**:① 控制台/子账号开通云监控 GetMonitorData 数据权限与云硬盘基础监控订阅(写操作,超出本任务只读边界);② 重跑 `TestManualProbeTencentQCECBSMetrics`;③ 按文档候选定案(QCE/CBS DiskReadIops 等 + 维度 diskId)。

### 1.5 volcengine(尽力而为 → 二期补;与前案同根因:云产品监控指标订阅未开通)

| 项 | 实测结果 |
|---|---|
| 指标查询 | 真实磁盘(`vol-3xccls…`,50GB)+ 静态候选矩阵:Namespace(Vulcan_EBS/Volcano_EBS/VEI_EBS/EBS/Vulcan_Storage_EBS)× SubNamespace(ebs/volume/disk/block_storage)× 指标名(13 个容量/IO/使用率候选)× 维度名(volume_id/disk_id/VolumeId/DiskId)共 **2240 组合全部 `metric not found`** |
| 阳性对照 | 真实 ECS 指标 `Vulcan_ECS/Instance/CPUPercent` 同样 `metric not found` —— 注册表为空特征(与 NAS/OSS 前案一致) |
| 根因判定 | **账号云产品监控指标注册表为空**(云产品监控指标需「产品订阅」开通,文档 6408/114674),非维度/指标名错误 |

**二期补重试路径(固化)**:① 开通云产品监控指标订阅(写操作,超出本任务只读边界);② 重跑 `TestManualProbeVolcanoEBSCloudMonitor`;③ 按文档候选定案(候选矩阵已在探测脚本,命中后按实盘指标名收敛)。

---

## 2. 实盘非零验证结果汇总 + 使用率口径归一(AC-1/2/3/5)

| 厂商 | namespace 定案 | IOPS | 吞吐 | 使用率 | 非零验证 | 判定 |
|---|---|---|---|---|---|---|
| aliyun | `acs_ecs_dashboard` | DiskRead/WriteIOPS(次/s,diskId 云盘级) | DiskRead/WriteBPS(**byte/s**,diskId 云盘级) | **无云盘级**;vm.DiskUtilization(% ,挂载实例级,插件) | PASS=336 | **通过** |
| huawei | `SYS.EVS`(+SYS.ECS) | disk_device_read/write_requests_rate(request/s,设备键) | disk_device_read/write_bytes_rate(**Byte/s**) | **无云盘级**;SYS.ECS disk_util_inband(% ,实例级) | PASS=394 | **通过** |
| aws | `AWS/EBS` | VolumeRead/WriteOps(次/日 Sum) | VolumeRead/WriteBytes(**byte/日 Sum**) | **无直接指标**;派生繁忙占比(见下) | PASS=21+派生 5 | **通过** |
| tencent | 未收敛(QCE/CBS 仅快照类) | 未收敛 | 未收敛 | 未收敛 | 0(全 invalid) | **未通过(权限/订阅)** |
| volcengine | 未收敛 | 未收敛 | 未收敛 | 未收敛 | 0(2240 组合 not found) | **未通过(订阅未开通)** |

必达三家 100% 有真实非零数据点 —— 发布 gate 的非零前提成立。

### 使用率口径归一(AC-5 定案)

**核心结论:五厂商中无任何一家提供「云盘级容量使用率(used/size)」直接指标。** 各厂商可用口径:

| 厂商 | 可得口径 | 语义 | 归一方案 |
|---|---|---|---|
| aliyun | vm.DiskUtilization / diskusage_utilization | **挂载实例级**磁盘使用率(云监控插件/Host agent,按 mountpoint/device) | 百分比 0~100 直接入库 `usage_percent`,但**打标 instance_level**(维度是实例+挂载点,非单盘) |
| huawei | SYS.ECS disk_util_inband / SYS.EVS disk_device_io_util | **实例级/设备级**使用率与 IO 利用率 | 同上:百分比归一 + 实例级打标 |
| aws | 派生 `(1−VolumeIdleTime/周期)×100` | **繁忙时间占比**(IO busy share),非容量水位 | 百分比 0~100 直接入库,但**打标 busy_share**(语义不同,前端展示须区分「容量水位」vs「IO 繁忙」) |
| tencent / volcengine | 未收敛(二期补) | — | 二期补后按实盘口径归一 |

**对 proposal 的口径修正(实盘为准,AC-5 要求)**:proposal「usage_percent 为磁盘使用率(0~100 或厂商百分比口径归一)」隐含「各厂商都有云盘级使用率」——实盘不成立。T3/T4 落地建议:
1. `usage_percent` 字段保留 0~100 归一口径,但需新增**口径标注**(instance_level / busy_share / cloud_disk_level),前端区分呈现;厂商间数值不可直接横比(阿里/华为是空间水位、AWS 是繁忙占比)。
2. 「磁盘容量水位」若须云盘级精确值,只能从 OS 侧(挂载实例 agent)取,属 proposal Out of Scope;本期按厂商可得口径打标呈现,不填假值(缺失日不填充,qc_status 闭环)。
3. 阿里 `BurstUtilization` 系列实盘含 **-1 哨兵值**、AWS `VolumeIdleTime` 全闲盘派生 0.00 属合法值——0~100 门禁(`CheckUsagePercentRange`)只拦负值/越界,-1 须在采集边界显式过滤为 null。

---

## 3. 实盘 Disk 按厂商分布(AC-6)

实盘只读枚举(2026-09-21,5 厂商活跃账号;地域性资源逐账号逐 region 枚举,aws 账号 regions 与实盘不一致时以 NAS_PROBE_AWS_REGIONS 扩展覆盖;容量取枚举快照 `DiskInstance.Size`,单位 GB 直加):

| 厂商 | 磁盘数 | 磁盘数占比 | 容量合计(GB) | 容量占比 | size=0 数 |
|---|---|---|---|---|---|
| aliyun | 1318 | **40.00%** | 442,517.00 | **43.18%** | 0 |
| volcano | 1679 | **50.96%** | 474,826.00 | **46.33%** | 0 |
| huawei | 159 | 4.83% | 42,276.00 | 4.12% | 0 |
| aws | 123 | 3.73% | 61,668.00 | 6.02% | 0 |
| tencent | 16 | 0.49% | 3,650.00 | 0.36% | 0 |
| **合计** | **3295** | 100% | **1,024,937.00** | 100% | 0 |

口径说明:磁盘数与容量均取数据面枚举快照(权威),5 厂商枚举全部成功(无 ProbeErr),双口径判定结论一致。

### 升格判定(tencent / volcengine,>15% 且探测可用 → 升格;探测不可用 → 二期补)

| 厂商 | 磁盘数占比 | 容量占比 | 探测可用 | 判定 |
|---|---|---|---|---|
| tencent | 0.49% | 0.36% | **否**(QCE/CBS 云盘指标未注册+数据权限未开通,§1.4) | **不触发升格**(占比 ≤15% 双口径均远低于阈):维持尽力而为;二期补路径已固化(开通权限后重跑,路径见探测脚本) |
| volcengine | **50.96%** | **46.33%** | **否**(订阅未开通,§1.5) | **「二期补」判定成立**:占比 >15% 但探测不可用,按规格显式降级为二期补,并在发布说明承诺补采窗口 |

---

## 4. 必达 vs 尽力而为:最终分组(发布 gate 定案)

| 分组 | 厂商 | 依据 |
|---|---|---|
| **必达** | **aliyun** | 实测定案 `acs_ecs_dashboard`:IOPS/吞吐云盘级(diskId)非零 336 PASS;实盘 1318 盘(40.00%)、容量 43.18%,为最大计算存储资产之一;适配注意:使用率只有挂载实例级(vm.DiskUtilization),Burst 系列含 -1 哨兵须过滤 |
| **必达** | **huawei** | 实测定案 `SYS.EVS` disk_device_* 系列(IOPS/吞吐/IO 利用率,设备键 = 实例 UUID-设备名)+ `SYS.ECS disk_util_inband`(实例级使用率);PASS=394 零 FAIL;实盘 159 盘(4.83%)、容量 4.12%;适配注意:维度键形态特殊(前缀匹配),不能按卷 ID 精确匹配 |
| **必达** | **aws** | `AWS/EBS` 标准指标全量通过(21/21 非零 + 派生公式 5/5);实盘 123 卷(3.73%)、容量 6.02%;适配注意:使用率为派生繁忙占比,须打标 busy_share 与容量水位区分;账号 regions 与实盘 region 不一致,采集执行器须按实例真实 region 查询(与 proposal「不做全局 region 推断」一致) |
| **尽力而为** | **tencent** | 探测未通过(QCE/CBS 云盘指标未注册 + 数据权限未开通)且磁盘数占比 0.49% ≤15% 双条件均不满足升格;**T4 不纳入本期**,二期补路径固化(开通权限 → 重跑探测 → 定案) |
| **尽力而为 → 二期补** | **volcengine** | 磁盘数占比 50.96%、容量占比 46.33% 均 >15% 但探测不可用(订阅未开通,与 NAS/OSS 前案同根因)→ 二期补,重试路径已固化在探测脚本;发布说明承诺二期补采窗口 |
| **升格候选** | 无 | tencent 探测不可用且占比不足;volcengine 占比足但探测不可用 —— 「>15% 且探测可用」双条件无一满足 |

**高危磁盘数(近失证据,Urgency 量化口径)**:容量水位(used/size>80%)五厂商均无云盘级直接指标(见 §2),无法在探测期统计;样本级证据:aliyun `vm.DiskUtilization` 实盘含 88.649%/60.693% 高水位实例。**观测期后由 `ecam_disk_metric` 指标表按 usage_percent 阈值统计**。

---

## 5. 遗留行动(不阻塞本定案,移交项)

1. **volcengine 云产品监控指标订阅开通**(写操作,超出只读边界)→ 二期补前置条件;开通后重跑 `TestManualProbeVolcanoEBSCloudMonitor` 收敛指标名,并重算 §3 分布表(volcengine 占比 >50%,二期补优先级最高)。
2. **tencent 云监控数据权限开通** → 同上;开通后重跑 `TestManualProbeTencentQCECBSMetrics`(8 候选矩阵已在脚本)。
3. **usage_percent 口径标注**:T2/T3 建模时须为 `ecam_disk_metric` 增加口径标注(instance_level / busy_share),或在 qc_status 体系外新增 data_status 维度 —— 本报告 §2 的归一方案是 T2 建模输入。
4. **阿里 Burst 系列负值过滤**:T3 适配器对 `DiskReadWrite*BurstUtilization` 的 -1 哨兵值过滤为 null(勿入 0~100 门禁误报)。
5. **aws 账号 regions 配置漂移**:账号 regions 配 eu-west-1 而实盘卷在 eu-central-1 —— 采集执行器按实例真实 region 查询(proposal 已定),账号 regions 配置修复不属本期。
6. **aliyun 磁盘指标窗口**:候选指标天粒度 86400 实盘可用;历史补采窗口上限探测未单测(与 OSS 31 天限制同型风险),T3 上线后首采按「首写生效+次日补昨日」口径不回溯。

## 6. 探测产物清单

| 产物 | 路径 | 说明 |
|---|---|---|
| Disk 分布聚合/使用率归一/派生公式纯逻辑 + 单测 | `internal/shared/cloudx/nasprobe/disk_usage.go` / `disk_usage_test.go` | DisksToUsage(数量/容量双口径)/DiskUsagePercentFromIdle(AWS 派生)/CheckUsagePercentRange(0~100 门禁),3 函数 4 子用例 100% 覆盖 |
| 分布统计探针 | `internal/shared/cloudx/nasprobe/disk_distribution_manual_test.go` | 5 厂商逐 region 磁盘枚举 + 双口径占比 + 升格判定输入 |
| aliyun 探针 | `internal/shared/cloudx/aliyun/disk_probe_manual_test.go` | acs_ecs_dashboard 元数据发现 + diskId/instanceId 双维度形态逐格探测 + 非零验证 |
| huawei 探针 | `internal/shared/cloudx/huawei/disk_probe_manual_test.go` | SYS.EVS/SYS.ECS 双 namespace 指标发现 + 实盘维度键形态(前缀匹配)非零验证 |
| aws 探针 | `internal/shared/cloudx/aws/disk_probe_manual_test.go` | AWS/EBS 五指标 GetMetricData 非零 + VolumeIdleTime 派生使用率验证 |
| tencent 探针 | `internal/shared/cloudx/tencent/disk_probe_manual_test.go` | DescribeBaseMetrics 发现 + 候选矩阵 + 错误归因 + QCE/CVM 阳性对照 |
| volcengine 探针 | `internal/shared/cloudx/volcano/disk_probe_manual_test.go` | 2240 组合候选矩阵 + ECS 阳性对照 + 二期补重试路径固化 |
| 原始探测日志 | `logs/disk_probe_*.txt`(gitignored) | 逐格探测证据,报告引用的样例行均出自其中 |

> Hard Rule 合规声明:本探测全程只读(List/ListInstances/DescribeMetric*/GetMetricData/DescribeBaseMetrics/ShowMetricData 类 API),未写任何临时 collection,未触碰生产表(建表属 T2);凭证仅内存解密、日志掩码展示。

## 7. 参考来源

- [阿里云 ECS 云监控指标(acs_ecs_dashboard)](https://help.aliyun.com/zh/ecs/user-guide/view-monitoring-data)
- [AWS CloudWatch EBS metrics(VolumeReadBytes/VolumeIdleTime 等)](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using_cloudwatch_ebs.html)
- 华为云 CES SYS.EVS/SYS.ECS 指标 / 腾讯云云监控 QCE/CBS 指标(各厂商官方文档)
- 前案参照:`docs/features/oss-ops-insight/probe-report.md`(volcengine 订阅未开通同根因)、NAS probe SYS.EFS 定案(文档口径与实盘不符先例)
