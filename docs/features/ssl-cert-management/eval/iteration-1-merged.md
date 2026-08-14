# Iteration 1 — Merged Report (PM + QA)

- PM: 823/1000（eval/iteration-1-pm.md）
- QA: 842/1000（eval/iteration-1-qa.md）
- **Gate Score（平均）: 833/1000**（target 900）

## Merged ATTACK_POINTS

1. [Flow Diagrams] 部分失败路径绕过验证窗口与回滚保护期（图与正文/Story 3 AC5 三方矛盾）；图亦缺"验证窗口内失败 → 回滚"分支 — Mermaid 图补齐部分失败分支的验证窗口与保护期节点及窗口内回滚分支
2. [User Stories/Functional Specs] 回滚对象错位：失败项旧引用从未被改，"对失败项执行回滚→恢复旧引用"自相矛盾；验证中/部分完成状态缺回滚入口 — 回滚语义改为作用于成功项（或精确定义"验证窗口内发现失败项"含义），UF-4 补验证中状态回滚入口，Story 3 补成功项回滚 AC
3. [Scope Clarity] "ALBConfig 等 CRD"开放枚举：哪些算"网关 CRD"直接决定 ≥95% 成功率分母 — 枚举首期扫描的 CRD 类型清单或给出判定规则
4. [Functional Specs] In Scope→UI 传导缺口：①补传私钥升级完整托管无入口；②"不可自动变更"目标单列清单在 ui-functions 与 user-stories 缺失；③孤儿证书补偿清理在变更报告字段中不可见 — 三处补 UI 交互与故事
5. [Functional Specs] 状态表标准不一：UF-2/UF-3 缺 loading/error，UF-4 缺 loading/empty/error，UF-5 缺空态 — 每个 UI Function 补齐完整状态机
6. [User Stories] AC 可验证性：①导入时 SAN 校验基准不可构造（新证书无引用，"预期域名"来源未定义）；②"不可篡改地留存审计记录"无验收手段；③分批执行与 GitOps 拦截两个核心安全机制无 AC；④Story 3 缺全部成功正向终态 AC
7. [Scenario Completeness] 管理权探测判定依据未说明（annotation/label/dry-run 等信号）及不可变更时的用户出路 — 给出判定依据与运营出路
8. [Edge Case Coverage] 并发与边界：①同一旧证书并发变更单无互斥语义；②执行中变更与天级扫描并发产生混合状态；③bootstrap 批量导入部分文件失败处理；④云 API 限流退避的用户可见行为；⑤验证窗口关闭仍未达标项的告警去向 — 补并发控制与边界规则
9. [Background & Goals] 登记覆盖率 ≥ 90% 分母口径缺失（扫描覆盖率有独立分母定义，登记覆盖率没有）— 定义独立分母
10. [Scenario Completeness] 豁免清单维护角色三处不一致：spec 用户表"运维工程师维护探测豁免清单" vs Story 5/UF-5"仅运维主管/审计" — 三处统一
11. [Functional Specs] 只读查看者可见范围矛盾：导航规则"仅可见到期看板一级入口" vs Story 6"证书管理页面仅可见看板与证书状态"、UF-1 页面对只读者行为 — 明确 /certs 路由对只读角色的可见性
12. [Scenario Completeness] 华为云/AWS/Azure 引用点进入变更清单的行为未定义（排除/单列/阻断三选一）及对 ≥95% 成功率指标分母的影响 — 明确处理方式
13. [blindspot] 分批灰度剩余项生命周期无主：剩余 50% 是同单续批还是另开新单未定义 — 补分批执行完整生命周期
