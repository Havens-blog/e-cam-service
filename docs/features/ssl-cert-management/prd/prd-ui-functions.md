---
feature: "SSL 证书统一托管与更换（证书管理功能域）"
---

# SSL 证书统一托管与更换 — UI Functions

> Requirements layer: defines WHAT the UI must do. Not HOW it looks (that's ui-design.md).

## UI Scope

e-cam-web 新增"证书管理"功能域，覆盖四类页面：证书台账、引用关系、到期看板、变更管理（向导与报告），外加全局配置（告警/探测豁免）。全部为新页面，挂在平台一级导航下。

## Navigation Architecture

- **Platform**: web

### Primary Navigation (shared across pages)

| # | Label | Target Page | Icon Keyword |
|---|-------|-------------|-------------|
| 1 | 证书台账 | /certs（证书管理 › 证书台账） | certificate |
| 2 | 到期看板 | /certs/dashboard（证书管理 › 到期看板） | clock-alert |
| 3 | 变更管理 | /certs/changes（证书管理 › 变更管理） | swap |

### Secondary Pages (navigated from a parent page)

| Page | Entry Point (UF# or action) | Return Target |
|------|-----------------------------|---------------|
| 证书详情（含引用关系） | UF-1 台账行点击 | 证书台账 |
| 变更向导 | UF-1 "发起更换" / UF-4 变更管理"新建变更" | 变更管理 |
| 变更报告详情 | UF-4 变更管理列表行点击 | 变更管理 |
| 全局配置 | UF-4 变更管理页"配置"入口（仅主管/审计可见） | 变更管理 |

### Navigation Rules

- Primary navigation is shared across pages（沿用 e-cam-web 现有一级菜单机制，按 EIAM 角色控制可见性）
- Every secondary page has back navigation targeting its entry point page
- 只读查看者仅可见"到期看板"一级入口；台账对只读者隐藏操作按钮

## UI Function 1: 证书台账管理

### Placement

- **Mode**: new-page
- **Target Page**: /certs（证书台账列表页）
- **Position**: 证书管理功能域主页，含列表、导入入口、统计概览

### Description

托管证书的列表与入口：展示证书要素（域名/SAN、签发者、有效期、指纹、托管状态），提供单张/批量导入、查看详情、删除（带拦截）操作。

### User Interaction Flow

1. 用户进入页面 → 看到统计概览（完整托管数 / 仅指纹登记数 / 覆盖率两口径）与证书列表
2. 点击"导入证书" → 上传 PEM + 私钥（私钥可选项，缺省走仅指纹登记）→ 提交 → 系统校验
3. 校验失败 → 行内/弹窗展示具体错误（不匹配/链缺失/SAN 不含/已过期），不入库
4. 点击证书行 → 进入证书详情（要素 + 引用关系入口 + 相关变更历史）
5. 点击"删除" → 系统校验：存在活跃引用或处于回滚保护期 → 拦截并说明原因；否则二次确认后删除
6. 点击"发起更换"（完整托管证书）→ 进入变更向导（UF-4）

### Data Requirements

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| 证书列表 | List | 证书台账服务 | 域名/SAN、签发者、有效期、指纹、托管状态（完整/仅指纹）、引用数 |
| 统计概览 | Stats | 台账统计 | 登记覆盖率、可更换托管覆盖率、仅指纹登记占比 |
| 导入表单 | Form | 用户输入 | PEM 文件 + 私钥文件（可选） |

### States

| State | Display | Trigger |
|-------|---------|---------|
| loading | 骨架屏 | 首次加载 |
| empty | 空态引导（指向批量导入/bootstrap） | 无证书 |
| error | 错误提示 + 重试 | 服务异常 |
| populated | 列表 + 概览 | 有数据 |

### Validation Rules

- 导入校验：证书/私钥匹配、证书链完整、有效期、SAN 解析；失败 100% 拦截并给出具体原因
- 任何接口/页面不展示明文私钥（详情页私钥字段仅显示"已加密托管"状态）
- 删除拦截：活跃引用 > 0 或回滚保护期内 → 禁止并提示先解除引用或等待保护期结束

---

## UI Function 2: 引用关系视图

### Placement

- **Mode**: new-page（证书详情的子区块）
- **Target Page**: /certs/:id（证书详情页）
- **Position**: 证书详情页核心区块：正向（本证书 → 引用资源）+ 反向（域名/资源 → 证书）查询

### Description

展示证书与资源的引用关系：按云/产品/集群分组列出引用资源；显示扫描时间与覆盖率元数据；显式声明覆盖边界。

### User Interaction Flow

1. 从台账进入证书详情 → 默认展示引用关系区块
2. 用户按云/产品/集群筛选、搜索资源名
3. 点击引用资源 → 展示资源要素（资源 ID、所属云账号、引用的证书 ID/名字段）
4. 用户切换"反向查询"标签 → 按域名或资源搜索其所引用的证书

### Data Requirements

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| 引用资源列表 | List | 引用扫描结果 | ���/产品/集群分组、资源 ID、证书 ID |
| 扫描元数据 | Meta | 扫描任务记录 | 最近扫描时间、各云各产品覆盖率（分母=资产同步独立盘点） |
| 覆盖边界声明 | Static | — | "本视图不含 VM Nginx 配置级引用"常驻显示 |

### States

| State | Display | Trigger |
|-------|---------|---------|
| populated | 分组引用列表 | 有引用 |
| 未发现引用 | "未发现引用（区别于无引用）"+ 最近扫描时间 | 扫描无匹配 |
| 扫描超期 | 新鲜度提示 + "立即扫描"入口 | 超过新鲜度阈值 |

### Validation Rules

- 同域名多证书并存时按指纹严格区分，不合并展示
- 覆盖边界声明不可关闭

---

## UI Function 3: 到期看板

### Placement

- **Mode**: new-page
- **Target Page**: /certs/dashboard
- **Position**: 证书管理第二主页面，面向全部角色（含只读查看者）

### Description

按子域名展示证书到期时间与健康状态：剩余有效期、分级状态（30/14/7 天/已过期）、TLS 探测结果（线上生效证书 vs 台账）、探测豁免标记。

### User Interaction Flow

1. 用户进入看板 → 总览卡片（各级数量、已过期数、差异告警数、豁免数）
2. 列表按子域名展示：剩余天数、状态色、托管类型、线上探测状态
3. 点击子域名 → 抽屉/详情展示证书要素与最近探测结果
4. 筛选：按状态分级、云、托管类型

### Data Requirements

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| 子域名列表 | List | 台账 + 探测结果 | 剩余天数、分级状态、探测一致性 |
| 探测状态 | Enum | TLS 探测任务 | 一致 / 差异 / 不可达 / 豁免 |

### States

| State | Display | Trigger |
|-------|---------|---------|
| populated | 看板卡片 + 列表 | 有数据 |
| empty | 空态引导 | 无证书 |
| 差异告警标记 | 行内差异角标 + 最近探测时间 | 线上≠台账 |

### Validation Rules

- 已豁免端点不参与常规差异状态渲染（单独标记，不告警）
- 剩余天数按天级刷新，展示最近巡检时间

---

## UI Function 4: 变更向导与变更管理

### Placement

- **Mode**: new-page
- **Target Page**: /certs/changes（变更管理列表）+ /certs/changes/new（变更向导）
- **Position**: 证书管理第三主页面，含向导（生成清单→确认→执行→进度→验证）与报告

### Description

变更单全生命周期入口：向导引导生成按指纹聚合的变更清单（含新鲜度校验、SAN 预检、盲区声明），人工确认（可选分批 ≤50%），执行进度逐项展示，验证窗口状态，变更报告查看。

### User Interaction Flow

1. 从台账"发起更换"或变更管理"新建变更"进入向导
2. 选择旧证书 + 新证书 → 系统校验扫描新鲜度（超期阻断并引导扫描）→ 生成清单（逐项资源、计划动作、原证书 ID、覆盖边界声明、SAN 预检结果）
3. 用户确认清单（可勾选分批，单批 ≤ 总量一半）→ 执行
4. 执行中：逐项状态实时刷新（成功/失败/执行中）；失败项出现"回滚"操作
5. 执行完成进入验证窗口：展示窗口倒计时与逐项验证状态（达标/差异-变更关联告警）
6. 查看变更报告：清单、逐项结果、回滚状态、验证结论
7. 回滚保护期内旧证书行显示保护期标记

### Data Requirements

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| 变更单列表 | List | 变更服务 | 状态机（草稿/待确认/执行中/验证中/已完成/部分完成/已回滚/回滚失败） |
| 变更清单 | List | 清单生成服务 | 资源、计划动作、原证书 ID、预检结果 |
| 执行进度 | Stream/Poll | 执行通道 | 逐项状态与错误信息 |
| 验证状态 | List | TLS 探测 | 窗口内预期终态比对结果 |

### States

| State | Display | Trigger |
|-------|---------|---------|
| 清单被阻断 | 阻断原因（扫描超期/SAN 不满足）+ 引导动作 | 新鲜度/预检失败 |
| 执行中 | 逐项进度 + 失败项可回滚 | 变更执行 |
| 验证中 | 窗口倒计时 + 逐项验证状态 | 进入验证窗口 |
| 回滚失败 | 告警横幅 + 转人工提示 | 回滚执行失败 |
| 已完成 | 报告入口 + 回滚保护期剩余 | 窗口关闭且达标 |

### Validation Rules

- 清单生成前置校验：扫描新鲜度、SAN ⊇ 目标域名，任一不满足阻断
- 分批确认单批不超过清单总量 50%
- 确认操作仅运维工程师角色可用；全程留审计

---

## UI Function 5: 全局配置（主管）

### Placement

- **Mode**: new-page（从变更管理页"配置"进入）
- **Target Page**: /certs/settings
- **Position**: 仅运维主管/审计可见

### Description

配置告警接收人（webhook + 邮件）、探测豁免清单、新鲜度/验证窗口/回滚保护期阈值参数。

### User Interaction Flow

1. 主管进入配置页 → 配置/编辑 webhook 地址与邮件接收组
2. 维护探测豁免清单（子域名增删，变更留审计）
3. 查看并可调阈值参数（扫描新鲜度、验证窗口、回滚保护期——在既定量级范围内）

### Data Requirements

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| 告警接收配置 | Form | 配置服务 | webhook URL、邮件组 |
| 豁免清单 | List | 配置服务 | 子域名、加入原因、操作人 |
| 阈值参数 | Form | 配置服务 | 新鲜度（默认 24h 量级）、窗口（2~24h）、保护期（7~14 天） |

### States

| State | Display | Trigger |
|-------|---------|---------|
| populated | 配置表单 | 有配置 |
| 渠道未确认提示 | 醒目提示"告警渠道未确认，相关 SC 不在验收范围" | 渠道未就绪 |

### Validation Rules

- 阈值参数调整限定在 PRD 既定量级范围（新鲜度小时级、窗口 2~24h、保护期 7~14 天且 ≥ 验证窗口）
- 所有配置变更留审计记录

---

## Page Composition

| Page | Type | UI Functions | Position Notes |
|------|------|-------------|----------------|
| /certs | new | UF-1 | 证书台账主页 |
| /certs/:id | new | UF-2（+ UF-1 详情要素） | 证书详情与引用关系 |
| /certs/dashboard | new | UF-3 | 到期看板（全角色，只读者默认页） |
| /certs/changes | new | UF-4 | 变更管理列表 |
| /certs/changes/new | new | UF-4 向导 | 变更向导（分步） |
| /certs/settings | new | UF-5 | 全局配置（主管） |
