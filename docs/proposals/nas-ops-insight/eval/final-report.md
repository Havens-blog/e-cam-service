# Eval-Proposal Final Report

**提案**: docs/proposals/nas-ops-insight/proposal.md — 多云文件存储 NAS 经营洞察(容量/使用率指标)
**日期**: 2026-09-19
**类型**: proposal | **标尺**: 1000 | **目标**: 900 | **最大迭代**: 3

## Eval-Proposal Complete

**Final Score**: 864/1000 (target: 900)
**Iterations Used**: 3/3

### Score Progression

| Iteration | Score | Delta |
|-----------|-------|-------|
| Pre-Revision Baseline | (freeform 未评分) | — |
| 1 | 695 | — |
| 2 | 819 | +124 |
| 3 | 864 | +45 |

### Dimension Breakdown (final)

| Dimension | Score | Max |
|-----------|-------|-----|
| 1. Problem Definition | 96 | 110 |
| 2. Solution Clarity | 114 | 120 |
| 3. Industry Benchmarking | 103 | 120 |
| 4. Requirements Completeness | 100 | 110 |
| 5. Solution Creativity | 68 | 100 |
| 6. Feasibility | 86 | 100 |
| 7. Scope Definition | 72 | 80 |
| 8. Risk Assessment | 79 | 90 |
| 9. Success Criteria | 71 | 80 |
| 10. Logical Consistency | 75 | 90 |

### Outcome

**Target NOT reached — 3 iterations exhausted.**

最终 864/1000,距 900 target 差 36 分。提案质量已从初始大幅提升(695→864,+169),remaining gaps 集中在:Solution Creativity(68,中规中矩平铺 CDN 模式)、Success Criteria(71)、Logical Consistency(75)。未达标主因是 iteration 3 修订后残留/引入的 4 个未闭合逻辑点 + 2 个 blindspot(详见下方 Residual Issues)。

---

## Pre-Revision Section (Phase 0 freeform review)

Freeform 审查产出 **13 风险/问题(8 high, 5 medium)+ 7 建议**。Pre-Revision(iteration 0)全量接受:

| 处置 | 数量 |
|------|------|
| Accepted(fully) | 13 |
| Partially accepted | 0 |
| Deferred | 0 |
| Skipped | 0 |
| **Triage rate** | **13/13 = 100%**(门槛 ≥80% ✅) |
| **Accepted + partially** | **13/13 = 100%**(门槛 ≥60% ✅) |

**HIT_RATE**: freeform 审查 13 findings 全部被 extraction 捕获并进入修订 = 1.0(≥0.5,无 low hit-rate annotation)。

### Baseline Comparison

- BASELINE_SCORE: 无(freeform 按协议不评分;基线仅存档于 `eval/baseline-snapshot/proposal.md`,145 行 pre-revision 原稿)
- 基线漂移告警: 不适用(无 BASELINE_SCORE 可比)
- INITIAL_SCORE(iter-1): 695

### Iteration Summary

| 轮次 | 分数 | 攻击点 | 处置 |
|------|------|--------|------|
| iter-1 | 695 | 16(含 3 blindspot) | reviser 全部 address |
| iter-2 | 819 | 12(含 2 blindspot) | 13/16 resolved,2 partially,2 resolved-but-flawed;引入 5 新问题 |
| iter-3 | 864 | 7(含 2 blindspot) | 12/12 resolved;残留 4 未闭合 + 2 blindspot |

### Bias Detection Report (annotated blind review)

| 轮次 | Annotated density | Unannotated density | Ratio |
|------|-------------------|---------------------|-------|
| iter-2 | — | — | ~19.4 |
| iter-3 | 0.16 | 0.02 | ~7.0 |

annotated/unannotated 密度比随迭代收窄(19.4→7.0),无 `conflict-with-pre-revision` 标记——修订方向与 CTO 评分判断一致。

---

## Residual Issues (未达 900 的原因)

### Iteration 3 残留(4 未闭合逻辑点)

1. **[Requirements] 补昨日失败无重试路径**:若次日补采失败,该行永久停在 00:10 初态,「日末态快照」口径在个别日期不成立。需定义:后续日重试补采 / 打标 `qc_status=initial-state-stale` / 冻结初态并记录。
2. **[Solution Clarity] SC-2 未随 M1 分组条件化**:SC-1 已内联「以 M1 探测分组为准」,但 SC-2(tencent/volcengine 尽力而为)未同步标注,升格/二期补状态下语义悬空。需预写三态(升格必达/维持尽力而为/>15% 但探测不可用→二期补)。
3. **[Feasibility] 探测任务 2 天承载 5 条工作流过载**:探测(指标名/namespace/非零验证)与统计(容量分布/高水位数)塞进 2 天单任务。需拆分「探测」与「统计」两条,或明示并行方式与样本上限。
4. **[Benchmarking] AWS 探测失败无预声明处置路径**:仅华为有降级路径,AWS 零值结果无预声明(降级为尽力而为 / SC-1 改写为「aliyun 必达」)。

### Iteration 3 blindspot(2)

5. **[blindspot] fs_id 撞号**:Top/运营卡按 `fs_id` 去重假设全局唯一,若厂商 fs_id 为 region-scoped,跨区域撞号会撞键/错并。需确认各厂商 fs_id 唯一域;若 region-scoped,唯一键加 region、去重键改 `(region, fs_id)`。
6. **[blindspot] 每日采集无节流/分片**:回填节流详尽,但 `nas:collect_metrics` 每日遍历无批大小/退避,大租户可撞 CloudWatch GetMetricData 配额。需复用回填的「厂商×账号」分片 + 批间退避参数。

---

## Rollback

- **Inner rollback**(Step 3b gate):未触发(iter-2 的 819 > iter-1 的 695,无分数回退)。
- **Outer rollback**(post-report):最终 864 < BASELINE_SCORE(无评分基线)→ 由用户决定。pre-revision 原稿存档于 `eval/baseline-snapshot/proposal.md`,如需回退到原始构思可恢复。

## 结论

提案方向正确、规格已相当扎实(单位语义、日快照口径、失败可观测性、聚合口径、原子认领日闸、回滚开关均已写死)。864/900 的差距是可修复的细节残留,非方向性问题。若接受 864 进入任务拆分,建议把上述 6 个 residual issues 作为任务级约束带上;或再补一轮迭代(新增修订)冲击 900。

## Next Step

用户决定:接受当前提案进入任务拆分(携带 6 residual 为任务约束),或继续修订。
