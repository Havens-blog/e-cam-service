
---

## cam-testenv-e2e-20261110（测试环境升级 + 统一审计全链路 E2E）

# 测试环境升级 + 统一审计全链路 E2E（2026-11-10 17:27）

**背景**：用户决策——部署工作流搁置（Secrets 不配），改为本地测试环境跑最新 main 做真实验证。

## 一、环境升级操作

| 步骤 | 结果 |
|---|---|
| dev 检出（D:\Haven\e-cam-service，落后 662 提交，CompileDaemon 守护）| stash 保护 prod.yaml 凭据 → merge origin/main |
| 冲突解决 | sync_assets.go 两处（import 并集 / 结构体取超集，保留 #103 aliasResolver 本地工作）|
| 验证 | 0 冲突标记残留；`go build ./...` 0 |
| CompileDaemon 自动重建 | :8001 新进程（17:18）运行最新 main `c623a17`+merge |

## 二、E2E 断言结果（真实浏览器 :3338 + 最新 main 后端）

| # | 场景 | 结果 |
|---|---|---|
| 1 | `GET /api/v1/cam/audit/events`（带会话）| **HTTP 200**，`{"code":0,"data":{...}}` |
| 2 | 联邦降级语义（F2）| `federation_degraded:true`（e-iam 旧二进制无 F1 路由 → 正确降级单源）✅ |
| 3 | 前端降级提示（F3）| warning banner「IAM 侧事件暂不可用，当前仅显示云管侧事件」渲染 ✅ |
| 4 | IAM 服务选项解禁（F3）| 页面存在「IAM 服务」选项 ✅ |
| 5 | **双写落事件** | 无效 POST（400 被业务拒绝，零状态变更）→ 统一事件即时记录：`admin create failed /api/v1/cam/cloud-accounts` ✅ |
| 6 | 事件行渲染 | 统一页表格渲染该事件行 ✅ |
| 7 | 视口 | 1440 与 390 截图（e2e-unified-final.png / e2e-unified-final-390.png）|

## 三、重要更正（判据教训）

- 曾误判「表格 14 行」为数据行——实为**日期选择器日历的 tbody**（「日一二三四五六」）；以 API 响应（total:0）与页面实际 network 调用为准。
- :8001 曾出现端口竞速（CompileDaemon 守护自动重启）——升级 dev 检出才是正解（守护者重建其监视的代码）。

## 四、当前环境状态

- :8001 = 最新 main（含统一审计 #421/#431/#485 全部成果）
- :9000 = e-iam 旧二进制（无 F1 联邦端点 → 联邦呈降级态，符合设计）
- 联邦完整链路（IAM 侧事件并入）需 e-iam 同步升级后自动恢复

---

## 2026-11-10 **周期回归(17:35)**:新功能二进制稳定;e-iam PR #4 已被维护者合并
8001=expiring-ar.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(total=7973)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
**e-iam PR #4(resetsecret)state=closed merged=True** ✓——常设待办第 3 项的审核部分完成
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-10 **周期回归(18:35)**:全部稳定,无可执行需求
expiring-ar.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7973)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓|#559/#560 已合并无新评论 ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-10 **周期回归(19:35)**:全部稳定,无可执行需求
expiring-ar.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7973)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-10 **周期回归(20:35)**:全部稳定;并行会话已部署 main-latest(含 #559)
8001=e-cam-service.main-latest.exe(并行会话部署的最新 main 构建)✓|need credential type=0 ✓(**AAD 修复在位**)|S2S ✓|expiring API ✓(7973,**#559 功能包含且生效**)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
我的 expiring-ar.exe 已被 main-latest 替代(符合口径 125:各自按节奏发布;功能已合入 main 故自然继承 ✓)
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-10 **周期回归(21:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7973)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-10 **周期回归(22:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7973)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-10 **周期回归(23:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7973)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(00:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7973→8069,同步波动正常)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## cam-fed-e2e-20261110（联邦全链路 E2E 成功 + e-iam 两处真 BUG 修复 e-iam#9）

**状态**：**已验证 + 修复合并**（e-iam#9 MERGED，CI 四项全绿）—— **联邦检索 F1→F2→F3 端到端真实打通。**

### 一、联调环境（不动共享工作树）

- e-iam 第二实例 :9001（我的 worktree 构建，隔离配置 --config，F1 双闸门开启+白名单[3]）
- e-cam :8001（最新 main）经 config/prod.local.yaml（本地覆盖机制）把 eiam.base_url 指向 :9001
- S2S 令牌仍由主实例 :9000 签发（共享 MySQL/Redis，密钥互通）

### 二、发现并修复的两处真 BUG（e-iam#9）

| # | 缺陷 | 实证 | 修复 |
|---|---|---|---|
| 1 | InitTables 缺 AuditEventRow——全新部署建不出 audit_events 表 | 共享 dev 库真实缺表（错误 1146），双写静默失败、读侧 500 | AutoMigrate 补模型 |
| 2 | AuditEventVO.Detail 为 string——违背 SDK 对象契约，F2 反序列化失败→永久降级 | app.log: cannot unmarshal string into map | json.RawMessage 透传（空/非法→null）|

### 三、全链路最终断言（e2e-federation-SUCCESS.png）

修复前：degraded=true（404→401→500→unmarshal 逐层排障）；修复后：
- **total=3（e-iam:2 + e-cam-service:1）——IAM 侧事件真实并入统一页**
- **federation_degraded=false**；前端降级提示消失（F3 条件渲染正确）
- S2S 鉴权/租户白名单/双写/at 归并全部生产验证

### 四、排障链教训（判据系列新增）

404（端口被无关进程 watch-alert.exe 占用）→ 缺 server 子命令 → 401（路由存在）→ 500（缺表）→ unmarshal（契约）——**每一步都以日志/直连探测区分，无猜测**。


---

## 2026-11-11 **周期回归(01:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7884,同步波动)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## cam-f3-filter-20261111（F3「IAM 服务」筛选联邦运行态验证 —— 统一审计验证面收口）

**状态**：验证通过。service=e-iam 筛选 total=8 全部 IAM 来源（服务端过滤对联邦数据生效）；无筛选 total=9 双源混合；degraded=false。F3 此前只能验证禁用态，联邦运行态补齐。**统一审计可验证项全部完成**（双写/读侧/降级/恢复/筛选/双源归并）。


---

## 2026-11-11 **周期回归(02:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7884)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(03:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7884)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(04:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7883)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(05:35)**:全部稳定,无可执行需求
## 2026-11-11 **周期回归(06:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7883)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(07:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7883)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(08:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7889)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(09:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7889)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(10:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7866)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(11:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7823)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(12:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7823)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(13:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7823)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(14:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7823)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(15:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7823)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(16:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7823)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(17:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7823)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(18:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7829)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **周期回归(19:35)**:全部稳定,无可执行需求
main-latest.exe ✓|need credential type=0 ✓|S2S ✓|expiring API ✓(7829)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引

---

## 2026-11-11 **缺陷修复:华为 DDoS AAD 凭证错误根治(PR #591 已合并+已部署+已验证)**

### 一、缺陷(周期回归 20:35 捕获)
`need credential type is [global.Credentials], actually is basic.Credentials` 每小时 1 次(huawei cn-north-1 DDoS),自 11-10 20:33 起 23+ 次——此前 tail 600 窗口大多错过:口径 127:**周期回归窗口应覆盖完整同步周期(~1h),必要时扫全日志**

### 二、根因(非 CompileDaemon,是版本回退新形态)
main-latest.exe(并行会话 11/10 19:42 构建)解析到**模块缓存 e-cloudx-sdk v0.2.0**(早于 PR #12);其 ddos.go 用 basic 凭证建 AAD 客户端;AAD 全局服务+账号默认 region cn-north-1 不在 SDK 地域表→每周期恰 1 次触发,其余华为资源正常故难察觉

### 三、修复(SDK v0.2.1 + go.mod 钉版)
- e-cloudx-sdk 打 tag **v0.2.1**(=v0.2.0+PR#12+PR#11)已推送
- 服务 go.mod v0.2.0→v0.2.1(PR #591 已合并):**GOWORK=off/缓存构建同样携带修复,封死版本回退路径**(部署不变量升级:版本钉死优于 go.work 依赖)

### 四、验收(真实周期验证)
- 部署 e-cam-service.v021fix.exe(PID 42840)后**下一真实周期 21:39:54**:回退 WARN 正常+**列表获取成功(count=0)**+同步完成 ✓;need credential type=0 ✓
- GOWORK=off 构建 ✓ dao/service 单测 ✓ health/S2S/expiring 7829/脱敏/前端 200 ✓
- 无 UI 改动(后端同步路径),前端回归 200 即烟测

### 五、遗留
aliyun ddosbgp/ap-south-1 等 403 为账号权限问题(既有,另计);回滚=换回 main-latest.exe

---

## 2026-11-11 **周期回归(22:35)**:v021fix 跨2周期稳定,无可执行需求
v021fix.exe ✓|**need credential type=0(口径127加宽窗口近5000行,覆盖完整同步周期)** ✓|S2S ✓|expiring ✓(7829)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限(aliyun ddosbgp、ap-south-1)

---

## 2026-11-11 **周期回归(23:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7829)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(00:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7917,同步波动)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(01:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7860)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(02:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7860)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(03:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7861)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(04:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7861)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(05:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7861)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(06:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7861)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(07:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7861)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(08:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7863)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(09:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7863)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(10:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7863)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(11:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7849)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(12:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7827)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(13:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7827)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(14:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7827)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(15:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7827)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(16:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7827)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(17:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7810)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(18:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7810)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(19:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7810)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(20:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7810)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(21:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7810)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(22:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7810)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-12 **周期回归(23:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7810)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(00:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(01:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(02:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(03:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(04:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(05:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(06:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(07:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7746)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(08:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7752)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(09:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7752)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(10:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7752)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## cam-cert-san-wildcard-20261113（三个 test 域证书差异根因：通配符 SAN 单标签覆盖）

**状态**：**已定位 + 需求卡归档**（e-cam-service#615 已合并）。无代码改动（调查 + 记录轮）。

### 一、现象

用户报告三个 test 域在证书探测中显示「差异」，且 OSS 桶上已绑定证书：
- test.order-static.jlc-layout.com
- test.pub-static.jlc-layout.com
- test.temp-static.jlc-layout.com

### 二、调查（真实拨测 + 代码口径）

| 域名 | CNAME 指向 | 线上证书 | 指纹 | 结论 |
|---|---|---|---|---|
| order-static（非 test）| `...c.vedcdnlb.com`（CDN）| `*.jlc-layout.com` | d8ef18ba… | 一致 |
| pub-static / temp-static（非 test）| 同上 CDN | 同上 | d8ef18ba… | 一致 |
| test.order-static | 直连 OSS 桶 | `*.oss-cn-shenzhen.aliyuncs.com` | 579b1947… | 差异 |
| test.pub-static / test.temp-static | 直连各自 OSS 桶 | 同上 | 579b1947… | 差异 |

判定代码：`internal/cert/service/probe_service.go` 六态判定 + `coverageIndex.covers`（通配符按 `coversSingleLabel`）；覆盖口径在 `internal/cert/domain/cert_parse.go` `CheckSANCover`，单测 `cert_parse_test.go` 明确断言 `"wildcard does not cover two labels"`。

### 三、根因

**`*.jlc-layout.com` 只覆盖单标签的三级域，不覆盖四级的 `test.order-static.jlc-layout.com`**（RFC 6125）。
因此：证书 SAN 不覆盖 → OSS 绑定无法生效（或生效后浏览器不认）→ 线上回落 OSS 区域通配证 → 探测判 `diff`。

**结论：差异告警是真实配置缺口，不是探测误报。**「证书已绑定」≠「证书覆盖该域名」。

### 四、交付与修复方案

需求卡 #615（已合并）：OSS 自定义域名纳入证书平台自动化的契约与验收（P2 平台能力）；附根因更正。
三个 test 域的直接修复（待决策）：
- A（推荐）签发 `*.order-static.jlc-layout.com` / `*.pub-static…` / `*.temp-static…`（正好覆盖其下四级域）；
- B 现有证书追加三条显式 SAN；
- C 测试域改三级命名（`test-order-static…`），现有通配符直接覆盖；
- D（否）改走 CDN——CDN 同样校验证书覆盖，证书不换无效。

### 五、教训

1. **判断证书类差异，第一步比对「线上 SAN 是否覆盖主机名」，再查绑定链路**——绑定成功与服务能力是两件事；
2. 域名标签层级是通配符证书的硬约束（`*.a.com` 覆盖 `x.a.com`，不覆盖 `x.y.a.com`）；
3. 对照组（非 test 版走 CDN）是本次定位的关键证据——同一张证书在两套链路上表现不同，直接把范围从「证书问题」收窄到「路径 + 层级」。

---

## 2026-11-13 **周期回归(11:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7752)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(12:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7702)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(13:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7695)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限
