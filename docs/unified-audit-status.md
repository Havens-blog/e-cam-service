# 统一审计日志 — 交付总账与恢复计划

> 本文档是统一审计专项的**权威状态总账**（截至 2026-11-07）。逐轮过程记录见
> `docs/agent-iterations.md`；本文只保留结论、验证矩阵与恢复路径。

## 一、交付全景（4 仓库，全部已合并）

| 仓库 | PR | 内容 | merge |
|---|---|---|---|
| e-common-go | #1 | auditlog SDK（envelope 模型/MongoWriter/NonBlocking/Sanitize）| `7b4f2bf`（v0.2.0）|
| e-common-go | #2 | SDK 使用文档 | `db9ebec` |
| e-cam-service | #421 | 操作审计双写 + `GET /api/v1/cam/audit/events` 统一查询 API | `8730e18` |
| e-cam-service | #431 | ChangeRecord + 变更单审计接入统一 envelope | `19847f4` |
| e-cam-web | #287 | 统一审计事件检索页 `/audit/events` | `180ed18` |
| e-cam-web | #289 | 域选项补「资产变更」+ resource_type 筛选 | `332266a` |
| e-cam-web | #291 | 「IAM 服务」选项诚实化（联邦待接入，禁用）| `090ef5c` |
| e-cam-web | #292 | total=-1 降级契约的前端消费（分页器损坏修复）| `3bc55c8` |
| e-cam-web | #300 | 统一页 + 变更页组件测试（7 例）| `3bc9d59` |
| e-iam | #5 | OperationLog 双写（MySQL audit_events 表）| `3f062ec` |
| e-iam | #6 | 统一事件读侧端点 + #5 空去重键数据丢失修复 | `c50ff0c` |
| e-iam | #7 | permgen 工程名与检出位置解耦（worktree 路径泄漏治本）| `6838afa` |

过程文档：#416（SDK）、#425/#427/#429/#433/#435/#438/#440/#443/#445/#446/#448 附近（逐轮记录）、
#441（联邦设计）、#451（教训汇编）。

## 二、验证状态矩阵

| 能力 | 单测 | CI | 浏览器实测 | 待验证 |
|---|---|---|---|---|
| SDK（模型/写入/脱敏/幂等）| 24 例 ✅ | — | — | — |
| e-cam 双写 + 统一查询 API | 12 例 ✅ | ✅ | ❌（旧后端无路由）| 部署后真数据 |
| 字段变更/变更单双写 | 10 例 ✅ | ✅ | — | 部署后 |
| e-iam 双写 | 8 例 ✅ | ✅（-race）| — | — |
| e-iam 读侧端点 | 8 例 ✅ | ✅（-race）| ❌（:9000 二进制 11-02 构建）| 部署后 |
| 统一检索页（渲染/筛选/URL/错误态）| 4 例 ✅ | ✅ | ✅（:3338 + 旧后端错误态）| 部署后真数据 |
| 变更页筛选（field_name/model_uid/change_source）| — | ✅ | ✅（field_name API 级真实生效）| change_source/model_uid 服务端过滤待 #418 部署 |
| total=-1 降级分支 | 3 例 ✅ | ✅ | ❌（需大表 30s+ 触发）| 部署后大数据 |

## 三、唯一阻塞：部署管道（用户侧）

三仓 Actions Secrets **全部未配置** → 每次 main push 的部署 job 红在
「Login to Aliyun Registry」（代码 CI 全绿）。修复清单见 `docs/agent-iterations.md`
的「cam-secrets-checklist-20261106」节（逐仓逐 Secret 名）。

## 四、待评审：联邦检索设计

`docs/unified-audit-federation-design.md`（#441 已合并）—— 推荐方案 A（S2S 后端
联邦）+ 三个待决策点：方案选择 / S2S 租户授权口径 / 降级策略。
**评审通过后按 F1（e-iam S2S 读端点）→ F2（e-cam 联邦客户端）→ F3（前端解禁
IAM 选项）分独立 PR 实施。**

## 五、恢复执行计划（部署管道修复后）

1. **全量 E2E**（积压清单）：
   - 统一页真数据渲染 + 筛选（含 change 域、resource_type）；
   - 触发写操作 → 统一事件出现（audit_events 集合）；
   - 变更页 change_source/model_uid 服务端过滤生效（#418）；
   - e-iam 读侧端点权限流（管理员实调）；
   - field_name 大表真实超时降级路径（大数据实测）。
2. **联邦 F1-F3**（按评审结论）。
3. **旧页退役评估**（统一页稳定运行后）。

## 六、已知残留

- e-cam-service CI 的 Docker job 缺 `DOCKER_USERNAME/PASSWORD`（#445 清单内）。
- 统一页无导航菜单入口（eiam 控制台菜单系统，待协调）。
- e-iam 主工作树 tenant-quota WIP（86 条目）仍在，等待其作者落地。
