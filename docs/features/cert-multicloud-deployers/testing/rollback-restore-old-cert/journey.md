---
feature: "cert-multicloud-deployers"
journey: "rollback-restore-old-cert"
risk_level: "High"
golden_path: false
surface_types: ["api"]
surface_keys: ["api"]
sources:
  - docs/proposals/cert-multicloud-deployers/proposal.md
generated: "2026-09-16"
---

# Journey: rollback-restore-old-cert

**Risk Level**: High

<!-- Risk Classification Criteria:
  High   = Workflow involves state mutation, data loss risk, or irreversible operations
  Medium = Workflow involves multi-step interaction without irreversible side effects
  Low    = Workflow is read-only or purely observational
-->

## Overview

替换后验证不通过或运维判定需要回退时，运维人员按旧云证书 ID 反绑完成回滚：触发回滚 → GetCert 校验旧云证书仍有效 → BindResource 将资源恢复引用旧证书 → 验证窗口确认线上指纹回到旧证书，回滚与正向替换共用同一两段式与状态机。(Source: proposal.md "Proposed Solution 创新点（回滚按旧云证书 ID 反绑）" + Key Risks "映射回滚失败"缓解 + Success Criterion 6)

## Setup

- 某三云条目已完成替换（新证书已绑定，映射中保有旧云证书 ID 与新云证书 ID）
- 旧云证书在云证书库中仍存在（华为 SCM / AWS ACM / Azure KV）
- 操作者具备变更回滚权限

## Happy Path

### Step 1: 对已完成替换的条目发起回滚

**User Action**: 运维人员对该条目发起回滚，系统定位映射中记录的旧云证书 ID

**Expected Result**: 回滚请求受理；系统按云解析旧云证书 ID（SCM ID / ACM ARN / KV 引用），准备反绑

### Step 2: GetCert 校验旧云证书仍有效

**User Action**: 系统调用 GetCert 按旧云证书 ID 回读校验

**Expected Result**: 旧证书存在且有效（含证书体/公钥可取回），校验通过后才继续回滚；按云实现各自校验

### Step 3: BindResource 反绑回旧云证书

**User Action**: 系统将目标资源重新绑定到旧云证书 ID

**Expected Result**: 资源恢复引用旧证书（与正向绑定同一绑定 API 与产品分支语义），绑定结果显式成功

### Step 4: 验证窗口确认线上指纹恢复旧证书

**User Action**: 运维人员在验证窗口内等待系统对目标域名拨测

**Expected Result**: ProbeDomains 拨测线上指纹 = 旧证书指纹，回滚闭环完成；资源回到替换前状态

## Edge Cases

### Step 2b: 旧云证书已被删除或已过期

**Precondition**: 云侧旧证书已不存在（人工删除）或 GetCert 判定无效

**User Action**: 系统执行回滚校验

**Expected Result**: 显式失败不猜测：回滚中止并给出明确原因，不盲绑到无效证书、不产生半回滚状态

### Step 2c: 旧 ID 形态按云不同导致解析差异

**Precondition**: 三云映射中的旧 ID 分别为 SCM ID / ACM ARN / KV 引用

**User Action**: 系统对三云条目分别执行回滚校验

**Expected Result**: 每云 GetCert 回滚校验按云实现均能正确解析与回读，无跨云形态混淆（ID 空间互斥）

### Step 3b: 重复回滚幂等

**Precondition**: 回滚已完成后运维人员再次发起同一回滚

**User Action**: 系统处理重复回滚请求

**Expected Result**: 幂等处理：不产生重复绑定副作用，结果与首次回滚一致（资源引用旧证书）

### Step 3c: 新证书已绑定到多个资源

**Precondition**: 替换时新证书被绑定到同一引用条目的多个资源/多地域

**User Action**: 系统执行回滚反绑

**Expected Result**: 所有已绑定资源均反绑回旧证书，无遗漏；不出现部分资源新证书、部分旧证书的混合态

### Step 4b: 回滚与清理队列并发

**Precondition**: 回滚发起时旧证书恰在清理队列中待删（或已被标记 orphan）

**User Action**: 系统协调回滚与清理

**Expected Result**: 回滚目标旧证书不被清理队列删除（或回滚显式失败），状态机不出现"回滚成功但旧证书已删"的矛盾终态

### Step 4c: 回滚后拨测指纹未及时收敛

**Precondition**: 反绑完成但 CDN/边缘节点指纹同步滞后

**User Action**: 系统在验证窗口内继续拨测

**Expected Result**: 窗口内重试拨测直至确认旧指纹或超时显式失败；不因单次拨测不一致误判回滚失败

## Journey Invariants

- 回滚前必须经 GetCert 校验旧云证书有效，禁止在未知状态下盲绑
- 回滚按旧云证书 ID 反绑，与正向替换复用同一 BindResource/绑定 API，不新增云特定回滚机制
- 回滚幂等：重复回滚不产生重复副作用，终态唯一（资源引用旧证书）
- 三云旧 ID 形态（SCM ID / ACM ARN / KV 引用）解析与校验按云实现，映射记录全程可追溯
- 回滚与清理/orphan 状态互斥协调，不允许出现引用已删除证书的终态
