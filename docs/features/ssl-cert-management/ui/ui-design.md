---
created: "2026-08-14"
source: prd/prd-ui-functions.md
status: Draft
---

# UI Design: SSL 证书统一托管与更换（证书管理功能域）

## Design System

**Vercel** — Black and white precision, developer-tool aesthetic. Monochrome precision; every element earns its place. Dark surfaces, stark text, negative space as a first-class element.

### Color Palette

| Role | Value | Usage |
|------|-------|-------|
| Background | #000000 | 页面底色 |
| Surface | #111111 | 卡片、表格容器 |
| Surface Alt | #171717 | 次级背景、表头、hover 行 |
| Border | #262626 | 分隔线 |
| Text Primary | #ededed | 标题、正文 |
| Text Secondary | #888888 | 描述、元数据 |
| Accent | #0070f3 | 链接、CTA、交互态 |
| Accent Hover | #3291ff | hover 态 |
| Success | #50e3c2 | 成功、达标、健康 |
| Error | #ee0000 | 破坏性操作、失败、差异告警 |
| Warning（补充） | #f5a623 | 临期分级（14 天）、限流重试中、部分完成（状态语义色，非装饰） |
| Verifying（补充） | #8b5cf6 | 验证中状态徽章（仅验证窗口态，非装饰） |

> 状态语义色仅用于状态标记（徽章/角标/进度），不得作装饰用途。Verifying 紫色仅用于"验证中"单据徽章，不扩展到其他视觉元素。

### Typography & Components

- 字体：Geist Sans（回退 Inter/system-ui），代码/指纹/证书 ID 用 Geist Mono 14px
- 按钮：Primary 实心 accent / Secondary ghost（transparent + border #333）；圆角 6px；150ms 过渡
- 卡片：#111111 + 1px #262626 圆角 12px，padding 24px；hover 边框 #444
- 输入：#111111 + border #333 圆角 8px；聚焦 border accent
- 导航：顶栏 bg #000/80 backdrop-blur；激活项白字 + 1px accent 下划线
- 深度：无投影，靠表面亮度分层（#000 > #111 > #171717）；层级 base(0)/card(10)/nav(20)/modal(30)/toast(40)
- 布局：内容最大宽 1200px；页面留白 24px；表格行高 48px

### 全局模式

- **危险/不可逆操作**（删除证书、执行回滚）：Modal 二次确认 + 明确后果说明
- **变更单状态徽章**：统一色映射 — 草稿/待确认=Text Secondary；执行中=Accent；验证中=Verifying（#8b5cf6，见 Color Palette）；已完成=Success；部分完成=Warning；已回滚=Text Secondary；回滚失败=Error。徽章除色觉通道外附加图标或文字（见"无障碍规范"）。
- **证书状态色**：剩余 >30 天=Success；≤30=Warning；≤14/#f5a623 加深；≤7/已过期=Error
- **空态**：图标 + 一句话 + 主 CTA（与 PRD UF 空态引导一致）
- **骨架屏**：表格 5 行骨架 + 卡片骨架，800ms 内返回则直接渲染
- **长文本规则**：SAN chips 超过 3 个折叠为「+N」chip，悬浮/键盘焦点展开全部；表格中指纹/证书 ID/资源 ID 列固定 mono 截断（首 8 + … + 末 8 字符），行内复制按钮取全文；变更列表「旧证书→新证书」列域名超长省略号截断 + tooltip 显示全文

### 无障碍规范

- **键盘导航与焦点**：所有交互元素可 Tab 到达并显示可见 focus ring（2px accent outline + 2px offset）；Modal/Drawer 打开时焦点陷阱（焦点首入首个可交互元素，Tab 不溢出到背景），Esc 关闭并焦点归位到触发元素；Modal/Drawer 内 Tab 顺序按视觉顺序自上而下；步骤条、表格行、筛选器均支持键盘操作（Enter/Space 触发、方向键在 Tab/表格内移动）。
- **表单标签**：所有输入控件必须有关联 `<label>`（或 `aria-label`），错误提示通过 `aria-describedby` 关联到控件并设 `aria-invalid`；占位符不得替代标签。
- **状态非色觉通道**：所有状态徽章/角标在颜色之外附加图标或文字（成功=✓+「成功」、失败=✗+「失败」、执行中=spinner+「执行中」、验证中=◐+「验证中」、回滚失败=⚠+「回滚失败」）；差异行角标附文字「差异」+图标，不依赖纯色识别。
- **对比度**：Text Primary #ededed 对 Surface #111111 ≈ 13.6:1（达标 AAA）。Text Secondary #888888 对 Surface #111111 ≈ 4.6:1（达标 AA 正文），但对 Surface Alt #171717 ≈ 3.9:1（不达标 AA 正文）。规则：Text Secondary 仅用于 ≥14px 的非关键元数据（如时间戳、计数字）；置于 Surface Alt #171717（表头、hover 行）上的正文或 <14px 关键信息不得使用 #888888，需提升为 Text Primary 或使用 #a1a1a1（#a1a1a1 对 #171717 ≈ 5.4:1）。
- **动效偏好**：`prefers-reduced-motion: reduce` 时禁用 150ms 过渡与手风琴动画，改为即时切换；轮询刷新不产生布局位移。
- **表格语义**：表格使用 `<th scope>` 标注列/行头；分页、筛选器状态变化通过 `aria-live="polite"` 通告。
- **Landmark 与 skip link**：每页含 `header`（顶栏）/ `nav`（主菜单）/ `main`（内容区，每页一个 `<main id="main">`）landmark 角色；页面顶部首个可聚焦元素为「跳转到主内容」skip link（`<a href="#main" class="skip-link">`，默认视觉隐藏，Tab 聚焦时显形并置顶）；长报告页（变更报告 5 卡）以 `<h2>` 分卡标题建立标题层级供读屏标题导航，卡内表头用 `<h3>`。
- **高频轮询与倒计时读屏策略**：执行进度 2s 轮询、验证窗口秒级倒计时属高频变更，不以每次 tick 触发 `aria-live` 通告（避免刷屏）。规则——（1）倒计时以静态 `aria-label` 描述总时长（如 `aria-label="验证窗口剩余 24 小时"`），仅在粒度跨小时档（如 24h→23h、1h→0h）或归零时发一次 `aria-live="polite"` 摘要通告「验证窗口剩余约 N 小时」/「验证窗口已关闭」；（2）执行进度仅在终态事件（项进入成功/失败/限流重试）时通告 `aria-live="polite"` 摘要「已完成 N/总数，失败 M」，运行中 spinner 不通告；（3）`prefers-reduced-motion: reduce` 下倒计时隐藏秒位仅显时分，轮询进度条改为静态文本「处理中：N/总数」。

### 响应式策略

- **断点**：>1200px / 1024~1200px / <1024px / <768px 四档。
- **>1200px**：内容最大宽 1200px 居中，双侧负空间；统计卡/总览卡按设计列数原样排列。
- **1024~1200px**：表格容器横向滚动（首列与操作列粘性 sticky），台账 3 列统计卡不变、看板 5 列总览卡折行为 3+2；向导步骤条压缩为横向滚动。
- **<1024px**：表格转为卡片式列表（每行一个卡片，字段标签前置）；统计卡/总览卡单列堆叠；侧栏导航折叠为抽屉（汉堡按钮触发，Drawer z30，宽 320px）。
- **<768px**：单列布局；Modal/Drawer 优先全宽抽屉化。
- **Modal 宽度**：两档——480px（单表单/确认）/ 720px（多步表单、批量导入、清单确认），小屏（<768px）改为全宽底部抽屉。
- **Drawer 宽度**：480px（引用资源抽屉、看板探测详情抽屉、报告侧栏），小屏（<768px）全宽。
- **表格列优先级**（小屏隐藏低优先列）：台账页保留 域名/SAN · 剩余 · 托管状态 · 操作，隐藏 签发者/引用数；看板页保留 子域名 · 剩余 · 状态，隐藏 托管类型/豁免；变更列表保留 单号 · 旧→新 · 状态 · 操作，隐藏 进度/发起人/时间。

### 只读角色路由拦截

- 只读查看者可访问 `/certs/dashboard`（到期看板，默认页与唯一菜单入口）与 `/certs/:id`（证书详情，只读模式：仅查看要素与引用关系，隐藏「立即扫描」等操作入口，从看板抽屉「查看证书详情」链接进入）。
- 下列路由对只读角色前端拦截并提示「无权限访问，仅到期看板可见」，同时 EIAM 接口同步拦截：`/certs`（台账）、`/certs/changes`（变更管理，含 `/certs/changes/new`、`/certs/changes/:id`）、`/certs/settings`（全局配置）。
- 一级菜单对只读角色仅展示「到期看板」，其余菜单项不渲染；证书详情不经菜单，经看板链接可达。

---

## Component: 证书台账页

### Placement

- **Mode**: new-page
- **Target**: /certs
- **Position**: 证书管理功能域主页（一级菜单"证书台账"）

### Layout Structure

```
[顶栏：平台导航 | 证书管理 › 证书台账]
[页面头] 证书台账  …… [导入证书 Primary] [批量导入 Secondary]
[统计卡行: 3 列]
  完整托管 (N) | 仅指纹登记 (N · 占比 X%) | 覆盖率：登记 ≥90% 目标 / 可更换托管
[表格卡]
  搜索框 | 筛选: 托管状态▾ 剩余天数▾
  ┌ 域名/SAN │ 签发者 │ 有效期 │ 剩余 │ 托管状态 │ 保护期 │ 引用数 │ 操作 ┐
  │ *.example.com │ DigiCert │ 2026-11-30 │ 74天[Success] │ 完整托管 │ 🔒保护期 5天 │ 12 │ 详情 发起更换 ⋯ │
  └──────────────────────────────────────────────┘
  [分页]
```

表格行点击 → 证书详情；操作列：详情 / 发起更换（完整托管证书显示）/ ⋯ 菜单（删除、补传私钥[仅指纹登记行]）。

**保护期标记（行级）**：证书处于回滚保护期内（protectDaysLeft>0）时，"保护期"列显示徽章 = 锁图标 + 「保护期 X 天」（X=protectDaysLeft），色 Text Secondary（置于 #111111 表体行上，对比度达标；hover 行 #171717 上提升为 #a1a1a1，见无障碍规范）；非保护期该列显示 "—"。

### States

| State | Visual | Behavior |
|-------|--------|----------|
| Default | 统计卡 + 表格 | 行 hover 背景变 #171717 |
| Loading | 骨架屏（5 行表格 + 3 卡） | 自动 |
| Empty | 居中图标 + "暂无证书" + "批量导入存量证书" Primary 按钮（点击打开批量导入 Modal，见 Interactions） | 引导 bootstrap |
| Error | 卡片内错误提示 + 重试按钮 | 服务异常 |

### Interactions

| Trigger | Action | Feedback |
|---------|--------|----------|
| 点击"导入证书" | 打开导入 Modal（拖拽上传 PEM + 私钥[可选] + 预期域名[可选]） | Modal z30 |
| 导入提交（处理中） | 合并 loading 态：提交按钮 disabled + spinner + 文案"上传中/解析中/校验中"（按当前阶段切换，阶段间无中间态闪烁），Modal 不可关闭 | 终态（成功/失败）到达前按钮保持 disabled，防重复提交 |
| 导入提交（校验失败） | 表单顶部 Error 横幅，逐项列出具体错误（私钥不匹配/证书链缺失/SAN 结构无法解析/已过期） | 不入库；错误逐条对应文件 |
| 导入提交（成功） | 关闭 Modal + Toast 成功 | 列表刷新，新证书置顶 |
| 预期域名比对不一致 | 表单内 Warning 提示"SAN 未覆盖预期域名（提示性，不拦截）" | 允许继续提交 |
| 点击"删除"（有活跃引用/保护期内） | 拦截 Modal：说明原因（N 个活跃引用 / 回滚保护期至 X 日） | 仅提供"知道了"，无删除按钮 |
| 点击"删除"（可删） | 二次确认 Modal（红色 Error 描述后果） | 确认后删除 + Toast |
| 点击"补传私钥"（仅指纹行） | 上传私钥 Modal → 匹配校验 | 通过后托管状态即时变为"完整托管" |
| 点击"发起更换" | 路由跳转 /certs/changes/new?certId= | — |
| 带 ?certId= 进入向导 | Step1 自动预选该旧证书（须为完整托管且有引用；预选后仍可手动更换，旧证书选择器不锁）；若该证书非完整托管或无活跃引用 → 旧证书选择器空并置顶提示"该证书无活跃引用或不可更换"，其余交互不变 | 预选不跳步，用户仍在 Step1 |
| 点击"批量导入"（页面头按钮，空态 CTA 同指此 Modal） | 打开批量导入 Modal（720px）：多文件选择/拖拽上传 PEM，逐文件可选择性附加私钥（含/不含私钥混合上传）；支持 zip 解包 | Modal z30，焦点陷阱见无障碍规范 |
| 批量导入提交 | 服务端逐文件校验（同单张导入四项规则），Modal 内展示逐文件结果列表：成功（完整托管）/ 成功（仅指纹登记）/ 失败 + 具体原因（私钥不匹配/证书链缺失/SAN 无法解析/已过期/重复指纹） | 全部完成前进度条 + 已完成计数；不得中途关闭（关闭需二次确认"终止导入"） |
| 批量导入部分失败 | 结果列表中失败行提供"重试"按钮（单文件重试），成功文件已入库不受影响 | 重试后该行状态更新；关闭 Modal 后列表刷新 |
| 批量导入完成 | 全部文件处理完成 | 关闭 Modal + Toast「导入完成：N 成功 / M 失败」，列表刷新 |
| 批量导入中断（浏览器崩溃/断网/确认终止） | 服务端逐文件即时处理并落库：已成功文件不回滚、保留入库；未处理文件不保留（无服务端暂存） | 重入页面后再开批量导入 Modal 展示上次会话结果列表（成功/失败状态留存），失败文件可单独重试；未处理文件需重新选择上传 |

### Data Binding

| UI Element | Data Field | Source |
|------------|-----------|--------|
| 统计卡 | completeCount / fingerprintOnlyCount / fingerprintOnlyRatio / registrationRate / replaceableRate | 台账统计接口 |
| 表格列 | cert.commonName / sans[] / issuer / notAfter / daysLeft / hostingStatus / protectDaysLeft / refCount | 证书列表接口 |
| 搜索框 | searchKeyword（域名/SAN/指纹片段匹配） | 客户端状态 + 服务端分页（过滤在当前页数据上即时执行，分页由服务端返回） |
| 筛选器（托管状态/剩余天数） | filterState{hostingStatus, daysLeftLevel} | 客户端状态（同上，与搜索合并过滤） |
| 分页 | page / totalPages（服务端分页，每页固定 20 行） | 证书列表接口 |
| 导入表单 | certFile / keyFile(可选) / expectedDomain(可选) | 用户输入 |
| 私钥字段（详情） | 状态"已加密托管" | 永不返回明文 |
| 批量导入结果列表 | files[]{fileName, keyAttached(bool), result: complete/fingerprintOnly/failed, errorReason, certId?} | 批量导入接口 batchImport |
| 批量导入进度 | processed / total / succeeded / failed | 批量导入接口（轮询或 SSE） |

---

## Component: 证书详情与引用关系页

### Placement

- **Mode**: new-page
- **Target**: /certs/:id
- **Position**: 从台账行进入；面包屑"证书台账 / *.example.com"

### Layout Structure

```
[面包屑: 证书台账 / *.example.com]
[要素卡: 2 列]
  左：SAN 列表(mono chips) / 签发者 / 指纹(mono, 可复制) / 托管状态徽章
  右：有效期进度条(剩余天数着状态色) / 私钥状态"已加密托管" / 关联变更历史列表
[引用关系卡]
  [正向引用 | 反向查询] Tab
  Tab 正向：[盲区声明横幅: "本视图不含 VM Nginx 配置级引用" 常驻 Warning 底色]
  [扫描元数据行: 最近扫描 2h 前 · 覆盖率: 阿里 99% · 腾讯 98% · K8s 5 集群 | 立即扫描 Secondary(超期时显示)]
  [筛选行: 云▾ 产品▾ 集群▾ | 资源名搜索框（输入即过滤，300ms 防抖，空查询恢复全量）]
  分组折叠列表: ▾ 阿里云 · DCDN (8)
    ┌ 资源 ID(mono) │ 域名 │ 证书 ID(mono) │ 账号 ┐
  Tab 反向：搜索框(域名/资源名，输入即过滤 + "查询"按钮) → 结果: 该域名/资源引用的证书卡片列表
```

### States

| State | Visual | Behavior |
|-------|--------|----------|
| Default | 要素卡 + 引用分组 | 分组默认展开前 2 组 |
| Loading | 要素骨架 + 引用区骨架 | — |
| Error | 引用卡内错误 + 重试 | 要素卡独立加载不受影响 |
| 未发现引用 | "未发现引用（≠ 无引用）· 最近扫描 X 前" | 区别于空态文案 |
| 扫描超期 | 元数据行变 Warning + "立即扫描"按钮 | 点击触发扫描任务，完成后刷新 |

### Interactions

| Trigger | Action | Feedback |
|---------|--------|----------|
| 点击"立即扫描" | 触发引用扫描任务 | 按钮转 loading"扫描中"；完成 Toast + 刷新元数据。防重：扫描进行中按钮 disabled + 文案"扫描中"；服务端同证书同时仅允许一个扫描任务，重复触发不新建任务、返回进行中任务状态；扫描中重入/刷新页面，按钮直接恢复"扫描中"态并轮询任务，完成后自动刷新元数据 |
| 分组折叠/展开 | 手风琴 | 150ms 过渡 |
| 点击引用资源行 | 右侧抽屉展示资源要素（资源 ID/云账号/证书 ID 字段原文） | Drawer z30 |
| 复制指纹 | 点击复制图标 | Toast"已复制" |
| 反向查询搜索 | 输入域名/资源名回车或点"查询"按钮 | 结果列表（按证书分组，指纹区分并存证书）；无匹配 → 空态文案"未查询到引用该域名/资源的证书"（区别于未执行查询的初始态） |
| 正向筛选器（云/产品/集群）变更 | 组内过滤引用分组与行（云与产品为级联，集群仅 K8s 组显示） | 即时；无匹配分组显示"无匹配引用"空态文案 |
| 正向资源名搜索 | 输入资源名/资源 ID 片段，300ms 防抖过滤 | 即时过滤当前分组；清空恢复全量 |

### Data Binding

| UI Element | Data Field | Source |
|------------|-----------|--------|
| 要素卡 | cert 全要素 | 证书详情接口 |
| 扫描元数据 | lastScanAt / coverageByCloud[] | 扫描任务记录 |
| 引用分组 | refs[](cloud/product/resourceId/certId/accountId) | 引用扫描结果 |
| 正向筛选器 | filterState{cloud,product,cluster}（云端）+ resourceKeyword（搜索词） | 客户端状态（前端过滤 refs[]） |
| 关联变更历史列表 | changeOrders[]{id,oldCert,newCert,state,progress,creator,createdAt}（按 certId=当前证书过滤，倒序） | 变更服务 |
| 盲区横幅 | 静态常驻 | — |

---

## Component: 到期看板页

### Placement

- **Mode**: new-page
- **Target**: /certs/dashboard
- **Position**: 一级菜单"到期看板"；只读查看者默认页与唯一可见页

### Layout Structure

```
[总览卡行: 5 列]
  >30天(N) | ≤30天(N) | ≤14天(N) | ≤7天(N) | 已过期(N)
  [次行: 差异告警数卡(N, 可点) · 探测豁免数卡(N, 可点)]（与状态卡同等卡片形态与交互）
[筛选行: 状态分级▾ 云▾ 托管类型▾ | 最近巡检: 3h 前]
[表格卡]
  ┌ 子域名(下行附云 chips) │ 剩余天数(状态色徽章) │ 托管类型 │ 线上探测 │ 豁免 ┐
  │ api.example.com + [阿里云][腾讯云] │ 12天[Warning] │ 完整托管 │ 一致[Success] │ — │
  │ intranet.example.com + [阿里云] │ 45天[Success] │ 完整托管 │ 豁免[Secondary] │ ✓ │
  │ legacy.example.com + [阿里云] │ 30天[Success] │ 仅指纹登记 │ 不可达[Secondary ⚠] │ — │
```

### States

| State | Visual | Behavior |
|-------|--------|----------|
| Default | 5 总览卡 + 表格 | 徽章按剩余天数着色 |
| Loading | 卡骨架 + 表骨架 | — |
| Empty | "暂无证书，等待导入" | 只读者看到亦同 |
| Error | 错误提示 + 重试 | — |
| 差异行 | 行内 Error 角标 + 最近探测时间 tooltip | 线上≠台账 |

### Interactions

| Trigger | Action | Feedback |
|---------|--------|----------|
| 点击子域名行 | 右侧抽屉：证书要素 + 最近探测详情（探测时间/线上指纹/差异说明）+ 底部"查看证书详情"链接（跳转 /certs/:id，全角色可用，不受只读拦截——只读者路由规则见下方说明） | Drawer |
| 点击状态卡 | 表格按该分级过滤 | 筛选联动；选中卡高亮（Accent 边框），再点取消过滤 |
| 点击"差异告警数"卡 | 表格过滤 探测状态=差异 | 与状态卡同等交互形态（选中高亮，再点取消） |
| 点击"探测豁免数"卡 | 表格过滤 豁免=✓ | 同上 |
| 点击"复制差异摘要"（差异行抽屉内） | 复制 域名/探测时间/线上指纹/差异说明 文本 | Toast"已复制"；只读者在本抽屉的两个操作入口之一（另一为"查看证书详情"），无告警权限 |
| 筛选器变更 | 表格过滤 | 即时 |

### Data Binding

| UI Element | Data Field | Source |
|------------|-----------|--------|
| 总览卡 | countsByLevel / diffAlertCount / exemptCount | 看板统计接口 |
| 表格列 | domain / daysLeft / level / hostingType / probeStatus(consistent/diff/unreachable/exempt) | 台账+探测结果 |
| 表格子域名行云 chips | referencedClouds[]{cloud}（当前证书所引用资源的所属云去重集合，多值） | 引用扫描结果 |
| 云筛选器 | filterState.cloud（多选） | 客户端状态（按 referencedClouds[] 过滤行，命中其一即显示） |
| 最近巡检 | lastInspectionAt | 巡检任务记录 |
| 探测详情抽屉 | lastProbeAt / onlineFingerprint / diffReason | TLS 探测记录 |

> **probeStatus 渲染定义**：一致 = Success 绿 + ✓；差异 = Error 角标 + 「差异」+ 最近探测时间 tooltip（线上≠台账）；不可达 = Text Secondary + ⚠ + 「不可达」（探测失败，如端口不通/DNS 无法解析/超时），**不参与差异告警**——区别于"豁免"：豁免是人工排除（✓ + 「豁免」，Secondary），不可达是探测失败需关注，巡检侧仍重试。

> 只读查看者：本页无变更类操作入口，仅保留差异行抽屉内「复制差异摘要」（纯文本复制，无告警权限）与「查看证书详情」链接（跳转 /certs/:id 只读模式）；差异告警由巡检自动触达全局告警接收人（配置见全局配置页"告警接收"），只读者无需人工上报。路由拦截规则见全局模式"只读角色路由拦截"（/certs、/certs/changes 及其子路由、/certs/settings 前端拦截 + EIAM 接口拦截；/certs/dashboard 与 /certs/:id 对只读开放）。

---

## Component: 变更管理列表 + 变更向导

### Placement

- **Mode**: new-page
- **Target**: /certs/changes（列表）+ /certs/changes/new（向导）；"配置"入口 → /certs/settings
- **Position**: 一级菜单"变更管理"

### Layout Structure — 列表页

```
[页面头] 变更管理 …… [新建变更 Primary] [配置 Secondary(仅主管)]
[状态 Tab: 全部 | 待确认 | 执行中 | 验证中 | 部分完成 | 已完成 | 已回滚/回滚失败]
  （Tab 与变更单状态机全集对齐：待确认含草稿/待确认；"已回滚/回滚失败"为合并 Tab，
   回滚失败行徽章为 Error + ⚠，Tab 内可再按徽章区分）
[表格卡]
  ┌ 变更单号 │ 旧证书→新证书(旧证书侧附保护期徽章) │ 状态徽章 │ 进度(成功/失败/总数) │ 发起人 │ 时间 │ 操作 ┐
  │ CHG-0042 │ *.example.com 🔒保护期 5天 → shop.example.com │ 执行中[Accent] │ 8/10 · 2 失败 │ ops@… │ … │ 详情 │
```

**保护期标记（行级）**：变更单旧证书处于回滚保护期内（changeOrder.protectDaysLeft>0）时，"旧证书→新证书"列旧证书侧显示徽章 = 锁图标 + 「保护期 X 天」（X=protectDaysLeft），色 Text Secondary（表体 #111111 上；hover 行 #171717 上提升为 #a1a1a1）；过期该徽章消失。

列表行点击 → 变更报告详情页（/certs/changes/:id，见下节）；执行中/验证中单据行点击 → 同路由进入只读恢复视图（见 Interactions）。

### States — 列表页

| State | Visual | Behavior |
|-------|--------|----------|
| Default | 状态 Tab + 表格 | 行 hover 背景 #171717；执行中/验证中行徽章实时刷新（10s 轮询） |
| Loading | 表格 5 行骨架 + Tab 骨架 | 自动 |
| Empty | 居中图标 + "暂无变更单" + "新建变更" Primary 按钮 | 引导创建 |
| Error | 表格卡内错误提示 + 重试按钮 | 变更服务异常 |

### Layout Structure — 向导（分步，顶部步骤条）

```
Step1 选择证书: 旧证书选择器(按域名/指纹搜索台账) + 新证书选择器(台账「完整托管」证书；向导内无上传入口，缺新证书→「返回台账导入」)
Step2 前置校验: 扫描新鲜度(超期→阻断卡: 原因+「立即扫描」) / SAN 预检(不满足→阻断卡)
Step3 变更清单:
  [可执行项表格: 资源│云/产品│计划动作│原证书 ID│
  [不可执行项分区(Warning 底色): 华为云/AWS/Azure 引用点 — "首期无部署器，二期或手工"
   不可自动变更 — "GitOps/控制器管理，走其管理链路"]
  [盲区声明横幅常驻] [清单绑定扫描: 2h 前快照]
Step4 确认执行: 清单摘要 + [同单分批勾选: 首批 ≤50%(floor)] + 确认按钮(工程师角色)
Step5 执行进度: 逐项状态行(成功✓/失败✗/执行中spinner/限流重试中[Warning])
  出现失败项后 → 顶部横幅"已成功项可回滚" + [回滚成功项 Secondary]
Step6 验证窗口: 倒计时(如 22:31:05) + 逐项验证状态(达标/差异-变更关联告警) + 成功项回滚入口
Step7 变更报告: 清单/逐项结果/回滚状态/验证结论/孤儿证书补偿清理结果(逐项清理成功/失败) + 导出
```

### 向导步间导航

- **步骤条行为**：当前步高亮（Accent）；已完成步可回点查看，进入**只读回看模式**（页头显示"只读回看"徽章 + "返回当前步骤"按钮，不可修改已提交内容）；未到达步不可点。键盘：方向键在步骤间移动，Enter 进入。
- **前进**：「下一步」按钮仅在当前步校验通过后可用（Step1 需旧/新证书均已选定；Step2 需新鲜度与 SAN 预检均通过；Step3 清单生成完成；Step4 需确认勾选）。校验失败时按钮 disabled + tooltip 说明缺失项。
- **存为草稿**：任意步（Step1~4）可通过页头「存为草稿」Secondary 按钮保存当前进度，变更单落状态机"草稿"态；列表中草稿单可从"详情"进入向导恢复编辑。Step5 起不可存草稿（已进入执行，无回退语义）。
- **取消**：任意步点「取消」→ 二次确认 Modal（"未保存的选择将丢弃；已存草稿不受影响"）。确认后返回入口页（台账或变更管理列表）。
- **返回导航**：向导页面包屑指向入口页（从台账"发起更换"进入 → 返回台账；从变更管理进入 → 返回变更管理）。

### 向导状态转换语义

| 转换 | 触发 | 说明 |
|------|------|------|
| Step4 → Step5 | 确认执行 Modal 确认 | 立即进入，变更单转"执行中"，审计留痕 |
| Step5 → Step6 | 全部清单项达到终态（成功/失败），无剩余批 | 自动进入，无需人工点击；存在剩余批且首批验证未开始 → 停留 Step5 显示"等待首批验证"提示 |
| Step6 → Step7 | 验证窗口关闭（倒计时归零）且无剩余批 | 自动进入；窗口内全部达标也可提前点「提前完成」（二次确认）进入报告 |
| 执行中 → 部分完成/已完成 | 窗口关闭时存在未达标/失败项 → 部分完成；全部达标 → 已完成 | 报告页徽章与保护期倒计时随之更新 |

### States（向导关键态）

| State | Visual | Behavior |
|-------|--------|----------|
| Step1 选择器空态 | 台账中无完整托管证书可选：空态卡"暂无完整托管证书" + 「返回台账导入」 | 跳转 /certs；已选旧证书暂存草稿 |
| Step2 预检执行中 | 整步骨架 + "预检执行中" | 预检自动执行，完成自动出结果 |
| Step3 清单生成中 | 清单表格骨架 + "清单生成中" | 自动生成，完成自动展示 |
| Step3 清单生成失败 | 错误提示 + 「重试」 | 重试重新触发生成；可返回上一步 |
| 清单被阻断 | 阻断卡（原因 + 引导动作） | "上一步"或"立即扫描" |
| 执行中 | 逐项状态实时刷新（2s 轮询） | 失败不中断其他项 |
| 验证中 | 窗口倒计时 + 逐项验证徽章 | 关闭仍未达标 → 转常规差异告警提示 |
| 部分完成 | 报告入口 + 成功项回滚入口 + 保护期剩余 | — |
| 回滚失败 | Error 横幅 + "已转人工处理"提示 | 告警同步触发 |
| 已完成 | 报告入口 + 保护期剩余天数 | — |
| 在途互斥 | 对同一旧证书再次发起 → Error 提示"存在在途变更单 CHG-XXXX" + 跳转链接 | 不允许重复发起 |

### Interactions

| Trigger | Action | Feedback |
|---------|--------|----------|
| Step1 旧证书选择 | 搜索框按域名/SAN/指纹（mono，支持指纹前缀匹配）检索台账，下拉行 = 域名 + 指纹（mono 截断）+ 剩余天数；单选 | 选中后卡片回显证书要素；"下一步"闸门见"向导步间导航" |
| Step1 新证书选择 | 同一搜索交互，候选集限定台账「完整托管」证书（前置：新证书须先导入台账；向导内不提供上传入口），排除已选旧证书 | 选中后卡片回显；无完整托管候选 → 空态卡 + 「返回台账导入」链接（跳转 /certs，已选旧证书暂存草稿） |
| Step2 进入 | 自动执行预检（扫描新鲜度 + SAN 覆盖），整步 loading（骨架 + "预检执行中"） | 完成自动展示结果；不通过 → 阻断卡（原因 + 「立即扫描」/返回上一步） |
| Step3 进入（预检通过） | 自动生成变更清单，表格骨架 + "清单生成中" | 完成展示可执行/不可执行分区；失败 → 错误提示 + 「重试」（重新触发生成，不阻断返回上一步） |
| Step4 确认执行 | Modal 二次确认（含影响面摘要 N 项资源） | 确认后进入 Step5，全程留审计 |
| Step4 确认（服务端快照重校验） | 确认时服务端重校验清单快照新鲜度与引用一致性 | 不一致 → 拦截执行，Error 提示"引用清单已变化，请重新预检"，自动回退 Step2 重新预检；一致 → 进入 Step5 |
| 分批勾选 | 首批数量选择器上限 = floor(总量/2) | 超限禁用 |
| 点击"回滚成功项" | Modal：回滚范围（仅本次执行成功项）+ 回滚目标有效性预检结果 | 云侧旧证书无效 → 提示"转人工决策"路径 |
| 执行剩余批（首批验证通过后） | 剩余批清单确认 Modal（同样需人工确认）；入口位于向导 Step6 页头（验证窗口行下方）与报告详情页顶部 | 全部批次完成生成整体报告 |
| 列表行点击 / 点击"详情" | 路由跳转 /certs/changes/:id 变更报告详情页 | 面包屑返回列表 |
| 执行中/验证中单据行点击 | 同路由进入**只读恢复视图**：报告详情页顶部内嵌向导 Step5/Step6 原状态组件（执行逐项状态 / 验证窗口倒计时），轮询自动恢复（2s） | 关闭浏览器/断网后重入不丢进度；用户无需停留原页 |
| 草稿/待确认单据行点击 | 进入向导恢复编辑（定位到已保存步骤，后续步可继续） | 变更单保持草稿态直至确认执行 |

### Data Binding

| UI Element | Data Field | Source |
|------------|-----------|--------|
| 列表列 | changeOrder{id,oldCert,newCert,state,progress(succeeded/failed/total),protectDaysLeft,creator,createdAt} | 变更服务 |
| Step1 选择器 | oldCertCandidates[]{id,commonName,fingerprint,daysLeft}（台账检索，域名/指纹匹配）；newCertCandidates[]{id,commonName,fingerprint,daysLeft}（hostingStatus=完整托管，排除旧证书） | 台账服务 |
| 清单表 | items[]{resource,cloud,product,action,oldCertId,executable,blockReason} | 清单生成服务 |
| 执行进度 | itemStates[]{status: success/failed/running/rateLimited,error} | 执行通道（轮询） |
| 验证状态 | verification[]{item,expected,actual,verdict} | TLS 探测 |
| 报告 | report{list,results,rollback,verification,orphanCleanup[]} | 变更服务 |
| 只读恢复视图（执行/验证中单据） | changeOrder.state + itemStates[] + verification[] + windowDeadline（同向导绑定，进入时按 state 定位 Step5/6 组件） | 变更服务 + 执行通道（轮询恢复） |

---

## Component: 变更报告详情页

### Placement

- **Mode**: new-page
- **Target**: /certs/changes/:id
- **Position**: 从变更管理列表行点击/「详情」进入；面包屑"变更管理 / CHG-XXXX"；执行中/验证中单据以只读恢复视图形式复用本页

### Layout Structure

```
[面包屑: 变更管理 / CHG-0042]
[页面头] CHG-0042 状态徽章 · 旧证书→新证书（指纹 mono, 可复制） …… [导出 Secondary] [执行剩余批 Secondary(存在剩余批且首批验证通过)]
[只读恢复区(执行中/验证中 且窗口未关闭，或部分完成[保护期内])]
  说明：部分完成 = 窗口已关闭且存在未达标/失败项，仍在可回滚保护期内（protectDaysLeft>0）；恢复区对部分完成单据可见，区内含成功项回滚入口。
  执行中 → 内嵌向导 Step5 组件（逐项状态实时刷新 2s 轮询 + 失败横幅 + 回滚成功项入口）
  验证中 → 内嵌向导 Step6 组件（倒计时 + 逐项验证状态 + 成功项回滚入口）
  部分完成 → 内嵌成功项回滚入口 + 保护期剩余天数徽章（无执行/验证实时组件，窗口已关闭）
  草稿/待确认单据 → 头部提示条"该单尚未执行" + [继续编辑 Primary] 跳转向导
[卡1 变更清单] items[] 表格: 资源 │ 云/产品 │ 计划动作 │ 原证书 ID │ 可执行性(不可执行项分区 Warning 底色 + 原因)
[卡2 逐项执行结果] itemStates[] 表格: 资源 │ 结果(成功✓/失败✗/限流重试中◐/跳过—) │ 错误信息 │ 批次
[卡3 回滚状态] rollback{scope[], per-item 结果, 目标有效性预检结论}；未回滚 → "未执行回滚"
[卡4 验证结论] verification[] 表格: 资源 │ 预期终态 │ 实际 │ 判定(达标✓/差异✗+关联告警链接)；未达标清单分区；窗口关闭时间与结论摘要
[卡5 孤儿证书补偿清理] orphanCleanup[] 表格: 云/资源 │ 清理动作 │ 结果(成功/失败+原因)；无可清理项 → "无孤儿证书"
```

### States

| State | Visual | Behavior |
|-------|--------|----------|
| Default（终态单据） | 页头 + 5 卡报告 | 各卡独立滚动锚点导航；卡 2/4 支持按结果筛选 |
| 只读恢复（执行中/验证中/部分完成[保护期内]） | 只读恢复区置顶 + 已完成卡（清单） | 轮询自动恢复，进度实时更新；用户可关闭浏览器后重入；部分完成无轮询（窗口已关闭），仅展示成功项回滚入口与保护期剩余 |
| Loading | 页头 + 5 卡骨架 | 自动 |
| Error | 卡内错误提示 + 重试；恢复区错误整卡重试 | 报告服务异常；恢复态轮询失败自动退避（2s→10s）重试不中断 |

### Interactions

| Trigger | Action | Feedback |
|---------|--------|----------|
| 点击"导出" | 下载报告（PDF/JSON） | Toast |
| 点击"执行剩余批"（存在剩余批且首批验证通过） | 剩余批清单确认 Modal（同向导规则） | 全部批次完成报告整体刷新 |
| 点击失败项/差异项行 | 展开错误详情（错误原文、关联告警链接） | 行内展开 |
| 点击"继续编辑"（草稿/待确认单） | 跳转向导恢复编辑 | 面包屑指向变更管理 |
| 回滚成功项（恢复区内，仅 执行中[出现失败后]/验证中/部分完成 可用） | 同向导回滚 Modal（范围 + 目标有效性预检） | 转人工路径同向导 |

### Data Binding

| UI Element | Data Field | Source |
|------------|-----------|--------|
| 页头 | changeOrder{id,oldCert,newCert,state,creator,createdAt,windowDeadline,protectDaysLeft} | 变更服务 |
| 卡1 变更清单 | items[]{resource,cloud,product,action,oldCertId,executable,blockReason} | 清单生成服务 |
| 卡2 逐项结果 | itemStates[]{status: success/failed/running/rateLimited/skipped,error,batch} | 执行通道（恢复态轮询） |
| 卡3 回滚状态 | rollback{items[],targetValidity,perItemResult} | 变更服务 |
| 卡4 验证结论 | verification[]{item,expected,actual,verdict,alertLink[]} + unmetList[] + windowClosedAt | TLS 探测 |
| 卡5 孤儿清理 | orphanCleanup[]{cloud,resource,action,result,reason} | 变更服务 |

---

## Component: 全局配置页（主管）

### Placement

- **Mode**: new-page
- **Target**: /certs/settings
- **Position**: 从变更管理页"配置"进入；仅运维主管/审计角色可见

### Layout Structure

```
[面包屑: 变更管理 / 配置] [渠道未确认提示横幅(Warning, 未就绪时)]
[卡1 告警接收] webhook URL 输入(mono) + 邮件接收组(tag 输入) + [发送测试] [保存]
[卡2 探测豁免清单] 表格: 子域名 │ 加入原因 │ 操作人 │ 时间 │ 移除
  [添加豁免 Modal: 子域名 + 原因]
[卡3 阈值参数] 扫描新鲜度(小时, 默认 24) │ 验证窗口(2~24h) │ 回滚保护期(7~14 天, ≥ 验证窗口)
  滑块或数字输入, 越界禁用保存; [保存] 全部配置变更留审计
```

### States

| State | Visual | Behavior |
|-------|--------|----------|
| Loading | 三卡骨架 | 读取配置中 |
| 读取失败 | 卡片内错误提示 + 「重试」 | 表单不可编辑，重试成功后恢复 Default |
| 保存异常 | 保存按钮旁行内错误提示（服务端错误信息） | 表单保留用户输入不清空，可修正后重试 |
| Empty | "尚未配置接收人" 空态卡 | 引导首次配置 |
| Default | 三卡表单 | 保存后 Toast |
| 渠道未确认 | 顶部 Warning 横幅"告警渠道未确认，相关验收标准不在范围" | 渠道就绪后消失 |
| 阈值越界 | 输入框 Error 边框 + 提示合法区间 | 保存禁用 |

### Interactions

| Trigger | Action | Feedback |
|---------|--------|----------|
| 保存告警配置 | 校验 URL 格式与邮箱格式 | 失败行内提示；成功 Toast |
| 发送测试 | 触发测试告警 | 结果 Toast（成功/失败原因） |
| 添加/移除豁免 | Modal 确认 | 列表即时更新 + 审计记录 |

### Data Binding

| UI Element | Data Field | Source |
|------------|-----------|--------|
| 告警配置 | webhookUrl / emailGroup[] | 配置服务 |
| 豁免清单 | exemptions[]{domain,reason,operator,at} | 配置服务 |
| 阈值 | scanFreshnessHours / verifyWindowHours / rollbackProtectDays | 配置服务 |

---

## Page Composition

| Page | Type | Components | Notes |
|------|------|-----------|-------|
| /certs | new | 证书台账页 | UF-1 |
| /certs/:id | new | 证书详情与引用关系页 | UF-2 |
| /certs/dashboard | new | 到期看板页 | UF-3；只读者唯一可见页 |
| /certs/changes | new | 变更管理列表 | UF-4 |
| /certs/changes/new | new | 变更向导（7 步） | UF-4 |
| /certs/changes/:id | new | 变更报告详情页 | UF-4；含执行/验证中只读恢复视图 |
| /certs/settings | new | 全局配置页 | UF-5；仅主管/审计 |
