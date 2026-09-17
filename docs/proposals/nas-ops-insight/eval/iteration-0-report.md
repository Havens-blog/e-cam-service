# Iteration 0 Report — Pre-Revision (Freeform Review)

**提案**:docs/proposals/nas-ops-insight/proposal.md
**审查专家**:多云存储监控指标架构师
**日期**:2026-09-17

## Freeform Findings Triage

freeform 审查产出 13 风险/问题(8 high, 5 medium)+ 7 建议。pre-revision reviser 逐条处理:

| 攻击点 | 严重度 | 处理结果 |
|--------|--------|---------|
| 跨厂商单位归一化未定义 | high | ✅ 接受:新增「单位归一化与字段语义」专节 + 5 厂商换算表 + 数量级自检 |
| utilization 除零/越界 | high | ✅ 接受:capacity=0 记 null、used>capacity 收敛、utilization 不落库读取派生 |
| 日快照采集口径 | medium | ✅ 接受:日末态口径 + days=2 采集窗口 + 峰值由 MAX 派生 |
| 尽力而为可观测性 | high | ✅ 接受:新增「失败可观测性」节(执行器计数/适配器分路径/前端空态/不继承全零跳过) |
| 日闸写失败降级 | high | ✅ 接受:原子认领 + 写失败指数退避重试/告警 |
| 日闸读失败洪泛 | high | ✅ 接受:读失败 ≥5 分钟退避 |
| 日闸并发触发 | medium | ✅ 接受:原子认领兜底多副本/手动+自动重叠 |
| aws 取舍矛盾 | high | ✅ 接受:aws CloudWatch 升为必达项,删 CloudFront 套用 |
| 跨账号同 fs 聚合口径 | medium | ✅ 接受:新增「聚合口径」节,按 fs_id 去重取最新/容量最大行 |
| 读取接口越权 | high | ✅ 接受:服务端校验 account_id∈租户账号集合,越权 404 |
| 探测未进任务清单 | medium | ✅ 接受:Next Steps 加独立探测任务(T-2 天最高优先) |
| 华为单 region 回退污染 | medium | ✅ 接受:声明指标路径用实例真实 region |
| 状态型重采覆盖 | medium | ✅ 接受:同日首写生效,区别于 CDN 覆盖语义 |

**Triage 率**:13/13 accepted(100%),≥80% 门槛通过。
**Baseline Score**:freeform 审查未给分数(rubric 评分在本轮之后)。

## 备注

- 全部 Edit 定向修改,未触碰 DOC_DIR 外文件与 eval/ 审查文件。
- 一致性修订(Key Scenarios/In Scope/Key Risks/Success Criteria)同步更新,无自相矛盾。
- 进入 iteration 1 rubric 评分。
