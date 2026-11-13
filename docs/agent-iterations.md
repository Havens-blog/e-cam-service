
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

---

## cam-dns-probe-hostname-20261113（探测主机名拼接去重：252 例重复后缀畸形域名根治）

**状态**：**已合并**（e-cam-service#634，CI Test + Build×5 全绿）。kicked off by 全平台探测数据盘点（3029 域）。

### 一、证据（真实数据，非推断）

认证后拉取 `GET /api/v1/certs/probes?limit=5000`：**3029 个探测域名**，状态分布：

| 状态 | 数量 |
|---|---|
| consistent | 1567 |
| unreachable | 1212 |
| diff | 189 |
| wildcard_skipped | 61 |

进一步分桶发现两类噪声：
- **重复后缀畸形域名 252 个（8.3%）**：`api.jlcpcb.com.jlcpcb.com`、`3d.jlcpcb.com.jlcpcb.com`、`account.forface3d.net.forface3d.net` —— **100% 落成 unreachable**（必然拨不通），既污染域名清单又消耗拨测并发；
- 随机短标签主机名 81 个（2.7%）：`23ecpui2h5t7.jlc3dp.com` 等（疑似 AAD/CDN 自动别名；其中 17 个 consistent，说明部分真实存在，暂不动）。

### 二、根因

`internal/cam/dns/probe_port.go` `buildProbeHostname` 无条件 `rr + "." + domain`。部分 DNS 源的 rr 字段返回**完整域名**而非相对标签（云厂商 API 实现差异），拼接后即产生重复后缀。

### 三、修复与验收

- rr 已含根域（相等，或以 `.<根域>` 结尾，大小写不敏感）→ 原样返回；
- 顺带归一 FQDN 尾部点（避免 `api.example.com..`）；
- 相对标签路径行为完全不变（既有 7 用例回归锁定），新增 9 用例：完整 rr / apex rr / 大小写 / 尾部点 / 多级根域 / **易误伤边界**（`xxxexample.com` 非 `example.com` 子域，必须继续拼接）；
- 门禁：`go build ./...` 0、`go test ./internal/cam/dns/` ok、gofmt/goimports(-local) clean、CI 全绿。

### 四、未验证项

存量探测记录不回溯；需下一轮 DNS 采集后对比 252 → 0 验证生效。

### 五、教训

1. **8.3% 的探测目标必然是噪声**——这类系统性浪费只有做「全量分布盘点」才看得见，逐域名排查永远发现不了；
2. 修「数据噪声」要找**生成点**（拼接函数）而不是在消费端过滤——生成点修复让存量与增量一并收敛；
3. 防御性归一必须配「易误伤边界」用例（`xxxexample.com` vs `example.com`），否则去重逻辑本身会制造新 bug。

---

## 2026-11-13 **周期回归(14:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7695)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(15:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7695)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **周期回归(16:35)**:全部稳定,无可执行需求
v021fix.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7695)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
本轮判定:**无可执行需求** ✓(口径 124 ✓);剩余外部待办:检出更新 / DBA 索引 / 云账号权限

---

## 2026-11-13 **云账号接入：禁止主账号 AK + 权限体检**（需求确认）
**需求来源**：用户 17:25 确认三条规则 —— ①禁止接入主账号 AK；②暂不支持 RAM 角色 AssumeRole；③权限不足必须提示出来。

**范围（本轮）**
- 后端：接入/更新时用 STS `GetCallerIdentity` 识别身份，`PrimaryAccount` 直接拒绝；新增 `POST /cam/cloud-accounts/:id/permission-check`，按 10 类资源做最小列举探测（ECS/RDS/Redis/SLB/VPC/NAS/CDN/DDoS/DNS/OSS），快照落 `ecam_account_permission_check`（按 account_id upsert），详情与列表接口返回 `permission_check`。
- 前端：接入表单提示 RAM 子账号+只读策略；详情抽屉「权限体检」面板（开始体检/汇总/状态表/体检时间/主账号告警）；卡片与列表在 denied>0 时显示「权限不足 N 项」。

**API**
`POST /api/v1/cam/cloud-accounts/{id}/permission-check` → `{account_id,provider,identity_type,is_primary_account,checked_at,summary{ok,denied,error,unsupported},items[{asset_type,asset_name,status,code,message}]}`

**验收（真实前后端 + 真实测试库，A 类）**
- 浏览器：14/14 断言通过（登录→云账号管理→添加账号提示→详情体检面板→开始体检 loading→结果表 10 行→体检时间→窄屏 390px→无控制台错误）；截图 perm-form-alert.png / perm-panel-loaded.png / perm-panel-checked.png / perm-panel-narrow.png。
- 非阿里云（华为云）体检：10 项「未支持」，汇总显示「未支持 10 项」（perm-panel-unsupported.png）。
- 阿里云真实账号体检：ok=10 / denied=0；`identity_type=RAMUser`、`is_primary_account=false`。
- 单测：错误归类（Forbidden/AccessDenied→denied；超时/限流→error）、message 脱敏（不含 AK/Secret/Signature 查询串）、地域解析、非阿里云跳过、主账号判定拒绝 + 文案。

**已修复的真实缺陷（本轮发现）**
- 阿里云 SDK 报错含完整签名 URL，会把 `AccessKeyId` 打进体检结果与日志 → 统一脱敏（`?<redacted>` + 凭证替换）。
- DDoS 高防探测缺 `PageNo` → 补 `PageNo/PageSize`。
- 瞬时网络错误被误判为「异常」→ 单次重试。
- STS 端点在本机解析到不可达 IP（106.11.232.4）→ 身份识别加重试；仍失败时在体检结果显式列出「AK 身份识别 / IdentityUnavailable」，避免主账号拦截静默失效。

**风险/未覆盖**
- 本机 `sts.aliyuncs.com` 网络不稳定：创建账号时若 STS 不可达，主账号拦截会退化为「放行 + 提示」，是否改为 fail-closed 待确认。
- `denied` 分支：当前 5 个账号权限齐全，未构造真实受限 AK，denied 状态仅由单测覆盖归类逻辑与前端 tag 映射。
- 预存在的 `sync_assets` 日志同样打印含 AK 的 SDK 错误（本轮未改，建议后续统一脱敏）。

---

## 2026-11-13 **周期回归(18:35)**:全部稳定 + 新功能复验
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7695)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
**新功能复验**：`POST /cloud-accounts/1/permission-check` → ok=10 denied=0 error=0，响应不含 AccessKeyId（脱敏生效）✓
本轮判定:**无可执行需求** ✓（上一轮 #644/#374 已合并并部署）
待确认：①STS 不可达时是否 fail-closed ②denied 分支需受限 AK 演练
## 2026-11-13 **周期回归(18:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7694)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-13 **周期回归(19:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7694)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-13 **周期回归(20:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7694)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-13 **周期回归(21:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7694)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-13 **周期回归(22:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7694)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## cam-probe-classification-20261113（探测分类治理 T1-T3：内网域 / 协议记录 / 新口径指标）

**状态**：T1 分类 + T2 降频 + T3 指标全部**已合并**（#643 / #645 / #650，CI 全绿）。T4 僵尸打标、T5 前端展示待做。

### 一、证据链（连续三轮真实数据驱动）

| 轮次 | 证据 | 结论 |
|---|---|---|
| 盘点 | `GET /api/v1/certs/probes?limit=5000` 全量 3029 域 | consistent 1567 / diff 189 / unreachable 1212 / wildcard 61 |
| 样本 | 对非畸形 unreachable（960）等距抽 150 做 A 记录解析 | **79% 解析到内网 IP**（外推 ≈755）、15% NXDOMAIN、6% 公网但 TLS 失败 |
| 补充 | 域名结构扫描 | 32 个下划线标签协议记录（ACME `_hash.` / DKIM `_domainkey.`）漏过既有「仅查 rr 前缀」口径 |

### 二、用户决策（2026-11-13）

1. 内网域**降频，非停拨**；
2. 指标**新口径 + 旧字段保留一版**；
3. 僵尸阈值 **连续 3 轮 NXDOMAIN**；
4. 三个 test 域**保持原状**（不做证书变更）。

### 三、实现（三个 PR）

**T1 #643 分类核心**
- `ProbeStatus` 新增 `internal` / `protocol_skipped`（validator 枚举 6→8）；
- `dialResult.RemoteIP`（握手后对端 IP）；
- 任一下划线标签 → `protocol_skipped`（不拨测）；对端命中 RFC1918/100.64/10/169.254/127/8/IPv6 私网 → `internal`（指纹照记供到期监控）；
- 徽标优先序把两态归「跳过类」，不让内网域把整证徽标拉成告警。

**T2 #645 降频**
- 日轮 `dropInternalTargets` 剔除上一轮 `internal` 目标（省 ≈755 域/天）；
- 新增 `cert:internal-probe` 周轮（`0 6 * * 0`），只拨内网域，保留到期监控；
- **自愈**：内网域若改公网解析，周轮改判回常规状态并自动回到日轮；
- 读取失败保守退化（宁可多拨不漏拨）。

**T3 #650 新口径**
- 新增 `probeTotalCount` / `internalCount` / `protocolSkippedCount` / `probeableCount` / `reachableRateOnProbeable` / `diffRateOnProbeable`；
- 分母 = 总数 − internal − protocol_skipped − wildcard_skipped；`computeProbeRates` 纯函数，分母 ≤0 时取 0（不产生 NaN）；
- 典型场景：3000 域 → 分母 2152，拨通率 52.2% → **72.8%**。

### 四、测试

新增 15 个用例（5 组）：`hasUnderscoreLabel` 7（含中段 `_domainkey` 漏网形态）、`isPrivateProbeIP` 16（各私网段边界 + 公网对照 + 空/非法保守）、`probeOne`/`probeOneDNS` 分类 5、`internalDomainSet`/`dropInternalTargets`/`ProbeInternalDomains` 4 组（含不改动入参底层数组）、`computeProbeRates` 6（含 NaN 防御与「新口径必须高于旧口径」验收）。

### 五、三轮 CI 往返（教训）

| 失败 | 原因 | 修法 |
|---|---|---|
| #645 trips 编译失败 | `tests/multicloudtest/StubProbeRunner` 未实现新接口方法 | 补桩（复用全量桩）|
| #645 gocritic | `rangeValCopy` 每次迭代拷贝 184B | 改索引遍历 |
| #650 exhaustive | switch 未覆盖新增枚举值 | 补 `unreachable`/`wildcard_skipped` 空分支 + 注释 |
| #643/#650 flaky | `TestTaskxDispatch_ExecutesViaQueue`（本地 6/6 通过、与改动无关）| 重跑 CI；**未改动/未跳过测试** |

**教训**：给枚举加值时，`exhaustive` 与 `gocritic` 会在**所有**既有 switch/loop 上同时触发；给接口加方法时，全仓 fake/stub（含 `tests/` 下的 Trips 桩）都会编译失败。下一次同类改动应先在本地 `go vet ./...`（含 tests）与 grep 枚举用量，再提交，可省 1-2 轮 CI。

### 六、未验证项

- 降频与分类的**线上效果**：需下一轮真实探测数据确认（日轮拨测量 ≈3029 → ≈2274、unreachable 1212 → ≈58 真不可达）；
- T4 僵尸打标（连续 3 轮 NXDOMAIN）、T5 前端看板新字段均未开始。

---

## 2026-11-13 **周期回归(23:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7694)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-14 **周期回归(00:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7624)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-14 **周期回归(01:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7624)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-14 **周期回归(02:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7624)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## cam-probe-nxdomain-stale-20261114（T4 僵尸记录打标）

**状态**：**已合并**（e-cam-service#656，CI Test + Build×5 全绿）。

### 一、证据与决策

- 证据：150 样本 DNS 实测中 **15% 的 unreachable 是 NXDOMAIN**（外推 ≈147 个）——云上已删、平台仍有记录的僵尸域名，长期占用清单；
- 决策（`docs/req-probe-classification.md`）：**连续 3 轮 NXDOMAIN 才打「疑似僵尸」标**，前两轮只计数不告警，恢复解析后清零；**只打标不删记录**（清理需人工确认）。

### 二、实现要点

1. **区分 DNS 失败与 TLS 失败**：`net.DNSError.IsNotFound` → 包装 `errProbeNXDomain`。这是关键设计——TLS 握手超时不是僵尸证据，若混为一类会造成误标；
2. **连续轮次计数**：`ProbeResult.nxdomainCount`，NXDOMAIN 时上一轮 +1；非 NXDOMAIN 失败与解析成功**一律重置 0**；
3. **阈值单点**：`staleNXDomainThreshold = 3` + 导出 `StaleNXDomainThreshold`（展示层判定 `staleMarked` 不重复定义魔数）；
4. **展示**：探测列表 VO 下发 `nxDomainCount` / `staleMarked`；
5. **schema 同步**：`schema.sql` 补 `internal`/`protocol_skipped` 枚举（修 T1 遗留滞后）+ `nxdomainCount: int ≥ 0`；`models_test` 的预期清单同步（该测试强制 bson ↔ schema 1:1，是本次能发现滞后的原因）。

### 三、测试

新增 8 例：NXDOMAIN 首轮计数 1、连续第 3 轮达阈值、**普通失败重置 0**、解析成功重置 0、大小写归一命中、DNS 源路径累加且租赁/链路字段不丢、`nxDomainCounts` 只收 >0 且小写归一、读取失败空表不阻断拨测。

### 四、CI 往返与教训（累计四轮）

| PR | 失败 | 修法 |
|---|---|---|
| #645 | trips 桩缺接口方法 | 补桩 |
| #645 | gocritic rangeValCopy 184B | 索引遍历 |
| #650 | exhaustive 未覆盖新枚举 | 补空分支 |
| #656 | lll 144>140 + 未用字段 leaf | 拆行 / 删字段 |

**新增教训**：本仓 CI 的 lint 面（lll=140 / unused / exhaustive / gocritic / goimports-local）在本地未装 golangci-lint 时不可见。可行做法是提交前跑固定预检脚本：① 全文件行宽 ≤140；② `go vet ./...`（含 tests）；③ gofmt/goimports 对 LF 副本校验。本轮已按此自查，但首次仍漏行宽——因为脚本只校验了改动文件的行数，未覆盖测试文件全文。

### 五、后端四件套收口

| 阶段 | PR | 内容 |
|---|---|---|
| T1 | #643 | 分类：internal / protocol_skipped |
| T2 | #645 | 降频：日轮剔除 + 周轮 |
| T3 | #650 | 指标：可拨测域分母 + 新两率 |
| T4 | #656 | 打标：NXDOMAIN 连续 3 轮 |

### 六、未验证项 / 待做

- **线上效果未验证**：日轮拨测量 ≈3029→≈2274、unreachable 1212→≈58 需下一轮真实探测数据；
- **T5 前端看板未开始**（展示新分类计数、新口径两率、staleCount），需真实浏览器双视口验收。

---

## 2026-11-14 **周期回归(03:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7611)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-14 **周期回归(04:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7611)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## 2026-11-14 **周期回归(05:35)**:全部稳定
permcheck.exe ✓|need credential type=0(口径127) ✓|S2S ✓|expiring ✓(7611)|脱敏 ✓|health ✓|前端 200 ✓|5 端口 UP ✓
权限体检复验：ok=10 denied=0 error=0，无 AK 泄露 ✓
本轮判定:**无可执行需求** ✓；待确认：STS 不可达时 fail-open/fail-closed、denied 分支需受限 AK 演练

---

## cam-cert-probe-ui-20261114（T5 前端：探测口径卡 + 疑似僵尸标签，含真实浏览器验收）

**状态**：**已合并**（e-cam-web#385，CI ESLint / Typecheck&Build / Vitest / Gzip 全绿 + 浏览器 18 项断言全 PASS）。

### 一、来源与契约

后端 T1–T4 已合并（#643 分类 / #645 降频 / #650 指标 / #656 打标）。前端消费两个已在线的契约：

| 接口 | 新增字段 |
|---|---|
| `GET /certs/dashboard` → `summary` | `probeTotalCount` / `internalCount` / `protocolSkippedCount` / `probeableCount` / `reachableRateOnProbeable` / `diffRateOnProbeable` |
| `GET /certs/probes` → 每行 | `nxDomainCount` / `staleMarked` |

### 二、变更

1. `src/api/cert.ts`：两处类型补齐；
2. `OverviewCards.vue`：新增「探测口径」三卡——可拨测域数（副文案列明三类剔除：内网域 / 协议记录 / 通配符）、拨通率、差异率；比率经 `percent()` 格式化（null/NaN → 「—」）；三卡为 `.static` 纯展示（无筛选语义、默认光标）；
3. `probes/index.vue`：状态徽标旁「疑似僵尸」琥珀标签，仅 `staleMarked` 时出现，title 含连续失败轮次与处置建议。

### 三、真实浏览器验收（D:\Haven\.browser-verify\cam-t5\）

环境：独立后端 :8012（本次构建 `-f config/verify8012.yaml`，含 T1–T4）+ 前端 :3339（`vite.verify3339.config.ts`，`VITE_BACKEND_URL=http://localhost:8012`）。

| 断言 | 结果 |
|---|---|
| 入口导航 `/cam/certs/dashboard` | PASS |
| 三卡渲染 | PASS（3）|
| 真实数值 | **3064 域 / 57.0% / 5.8%**（非 mock）|
| 无 NaN / undefined | PASS |
| 既有 5 分级卡 + 2 特殊卡未破坏 | PASS |
| 三卡纯展示（`.static`×3）| PASS |
| 390 视口单列折叠 + 无横向溢出 | PASS（columns=230px）|
| 刷新后三卡仍在 | PASS |
| **后端缺字段 → 降级「—」且无 NaN** | PASS（拦截真实响应删除 6 字段的负向测试）|
| 无僵尸数据不显示标签 | PASS（tags=0，表体 78 行）|
| console errors / failed requests | **0 / 0** |

### 四、环境合规处理

- 探测发现 **:8002 被他人 `e-cmdb-service` 占用** → 未触碰，改用 :8012；
- 验收后停止 :8012/:3339，删除临时后端配置（含 local 覆盖）、临时 vite 配置与临时二进制；
- 他人在 `src/components.d.ts` 的未提交改动全程未触碰（只提交自己改的 4 个文件）。

### 五、几个踩坑（可复用）

1. **路由前缀**：该前端 `base=/cam/`，证书域真实路径是 `/cam/certs/dashboard`，`/certs/dashboard` 会被 Vite 提示 base 不匹配；`/cam/dashboard` 是多云概览，不是证书看板；
2. **跨源被 CORS 拦截**：页面内 `fetch('http://localhost:8012/...')` 失败，改用 Playwright `context.request`（共享 cookie 容器、不受 CORS 限制）；
3. **PowerShell 读写 UTF-8 脚本会乱码**：`Get-Content/Set-Content` 默认编码破坏了中文断言文案 → 改用 .NET `ReadAllText/WriteAllText` + `UTF8Encoding($false)`；
4. **登录后会被重定向到门户首页** → 登录完成后再显式 `goto` 目标页，否则断言对象不存在。

### 六、未验证项

分类/降频的**线上生效情况**仍未验证：验收时 API 返回 `internal=0`、`protocol=0`，说明尚未跑过含新代码的全量探测轮。需观察下一轮（每日 05:00）后 `internal`/`protocol_skipped` 是否出现、`unreachable` 是否从 1212 显著下降。
