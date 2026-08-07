# e-cam-service 接入 ecmdb/eiam 统一鉴权平台

日期：2026-08-06
状态：设计待评审

## 1. 背景

用户现象：ecmdb 登录正常，进入 e-cam-service 页面提示登录过期。

排查后确认这不是会话过期，而是两件被混淆的事：

1. **登录共享已断裂** —— ecmdb 已把 user/permission 模块整体迁往 eiam，e-cam-web 仍在调用被删除的端点。
2. **e-cam-service 从未接入平台鉴权** —— 它自建 JWT 验签，且其策略校验链路指向一个从未被实现的 gRPC 服务。

本设计分两阶段：Phase 1 恢复登录可用，Phase 2 完整对齐 ecmdb 的接入形态。

## 2. 已确证的事实

### 2.1 凭证共享机制

平台的「共享凭证」不依赖任何服务间调用，而是**同一把 HS256 密钥各自独立验签**：

| | eiam | ecmdb | e-cam-service |
|---|---|---|---|
| `session_encrypted_key` | `1234567890Key` | `1234567890Key` | `1234567890Key` |
| `cookie.name` | `ecmdb-token-key` | `ecmdb-token-key` | `ecmdb-token-key` |
| TTL | 30 天 | 30 天 | 30 天 |
| `cookie.domain` | `localhost` | `106.52.187.69` | `.example.com` |

签发方是 eiam：`issueSession()`（`eiam/internal/web/user/user.go:402`）用 `NewSessionBuilder` 签 JWT，jwtData 携带 `tenant_id` 与 `username`；`SwitchTenant`（`eiam/internal/web/tenant/handler.go:215`）切租户时销毁旧 session 并重签。

**关键机制**：ginx 的 `SessionProvider.Get()` 只调用 `VerifyAccessToken(token)`，纯 JWT 验签，**完全不读 Redis**。Redis 仅用于 `sess.Get(key)` 读取 session 数据和 `Destroy()`。

> 更正一处既有注释：`e-cam-service/config/prod.yaml` 中「redis db 与 eiam 不一致会导致会话无效或已过期」的因果不成立。db 不参与登录判定。统一 db 本身是对的，但它不能修 401。

### 2.2 实测证据

用已知密钥自签合法 token 打向 e-cam-service（:8001），请求不存在的路径，因此 401 = 认证被拒、404 = 认证通过：

| 请求 | 结果 | 含义 |
|---|---|---|
| 无凭证 | 401 `会话无效或已过期` | 基线 |
| 自签 token 放 `Authorization: Bearer` | **404** | 认证通过 |
| 同一 token 放 cookie `ecmdb-token-key` | **403 权限不足** | 认证通过，死在 `check_policy` |
| 垃圾 token | 401 | 对照 |

结论：**e-cam-service 的会话验签完全正常**，密钥、claims 结构、TTL 判定、时钟均无问题。前端与后端的两句「过期」文案均为硬编码，与真实原因无关。

### 2.3 ecmdb 已迁移

`ecmdb/internal/user/` 已在 commit `2ec3854 chore: 删除已经迁移的模块，重构目录结构` 中删除。当前 HEAD 中 ecmdb 仅剩 7 个业务模块（attribute、dataio、model、plugin、relation、resource、tools），**无任何 user / permission / login 路由**。

### 2.4 ecmdb 的接入形态（Phase 2 的对齐目标）

| 维度 | 实现 |
|---|---|
| 依赖 | `go.mod` 引 `github.com/Duke1616/eiam v0.0.20`（eiam 最新 tag `v0.0.21`） |
| 鉴权 | `ecmdb/ioc/web.go:40-43` 使用 `sdk.CheckLogin()` + `sdk.CheckPolicy()` |
| 装配 | `ecmdb/ioc/policy.go`：`sdk.NewSDK()`、`capability.NewSyncer(capability.NewHttpReporter())` |
| 权限资产 | 各 handler 用 `capability.NewRegistry(...)` 声明，`syncer` 上报 eiam |
| 上下文 | `server.Engine.ContextWithFallback = true`，身份经 `ctxutil` 注入 `context.Context` |
| 租户隔离 | `pkg/mongox/plugin/tenant_plugin.go` 在 DAO 层自动隔离 |
| 指向 | `policy.auth_url` → eiam（dev `127.0.0.1:9000`，prod `http://eiam:8000`） |

eiam 侧提供 `POST /api/permission/check_login` 与 `POST /api/permission/check_policy`（`eiam/internal/web/permission/handler.go:36-37`，公开路由，SDK 自带 token 由其内部校验）。

### 2.5 e-cam-service 的四项差距

1. **零依赖、自建验签** —— go.mod 无任何 eiam/ecmdb 依赖，靠 `ioc/session.go` 自建 provider。
2. **鉴权实际失效** —— 所调用的 `ecmdb.policy.v1.PolicyService` proto 只存在于 e-cam-service 自身目录，ecmdb 与 eiam 均无 `RegisterPolicyServiceServer`。运行日志为 `Unimplemented`，配合 `fail_mode: fail_open`，**所有携带 Authorization 头的请求一律放行**。2.2 中自签假 token 走到 404 即由此。
3. **gRPC target 指向自己** —— `grpc.client.ecmdb.target: etcd:///service/e-cam-service`。
4. **仍用已被取代的旧模型，且同样已失效** —— `RegisterEndpointsToEcmdb` 走 ecmdb 旧的 endpoint/Casbin 注册。运行日志证实该链路也是死的：`unknown service ecmdb.endpoint.v1.EndpointService`，`count: 310`。即 policy 与 endpoint **两条 gRPC 链路均指向不存在的服务**，e-cam-service 与平台之间目前没有任何有效的 gRPC 通路。

## 3. 根因链路

### 3.1 当前断裂链路

```
ecmdb 删除 internal/user (commit 2ec3854)
  │
  └─ e-cam-web fetchUserInfo()  (src/stores/user.ts:70)
       POST /api/cmdb/user/info
         → nginx  location ^~ /api/cmdb  → 127.0.0.1:8000/api/...
           → ecmdb 已无该路由 → 404
             │
             └─ catch → redirectToLogin()  (src/api/request/index.ts:16)
                  ├─ ElMessage "登录已过期，请重新登录"      ← 用户看到的文案
                  └─ removeEcmdbToken()                      ← 破坏性副作用
                       document.cookie ecmdb-token-key
                         path=/    ← ecmdb-web 用 js-cookie 写在此处的平台共用凭证
                         path=/cam
                         │
                         └─ ecmdb 登录态一并被清除 → 故障自我扩散
```

三个失效调用：

| 位置 | 当前调用（已不存在） | eiam 对应端点 |
|---|---|---|
| `src/stores/user.ts:70` | `POST /api/cmdb/user/info` | `GET /api/user/profile` |
| `src/stores/user.ts:87` | `POST /api/cmdb/permission/get_user_menu` | 不映射到 menus 端点，改由 profile 的 `permissions` 字段提供，见 4.1 |
| `src/stores/user.ts:102` | `POST /api/cmdb/user/logout` | `POST /api/user/logout` |

所用端点在 eiam 中均属 `IdentityRoutes` 层级（`eiam/ioc/web.go`：`PublicRoutes` → `session.CheckLoginMiddleware()` → `tenancyBuilder.Build()` → `IdentityRoutes` → `CheckPermission` → `PrivateRoutes`），即只需登录态，不需额外权限。

### 3.2 后端 403 链路

```
请求携带 cookie（无 Authorization 头）
  │
  ├─ EcmdbAuthMiddleware  → sp.Get() → mixin carrier
  │    header carrier 取空 → cookie carrier 命中 → 验签通过 ✓
  │
  └─ CheckPolicyMiddleware  (internal/shared/middleware/check_policy.go:59)
       token := c.GetHeader("Authorization")   ← 只读 header，无 cookie 回退
       token == "" → 403 "权限不足"
```

认证层接受 header 或 cookie 两种载体，策略层却只认 header，导致纯 cookie 携带的请求必然 403。日志中 `策略检查: Token为空` 即此。

### 3.3 目标链路（Phase 2 完成后）

```
浏览器  ecmdb-token-key (JWT, eiam 签发)
  │
  ├─ Authorization: Bearer <jwt>  或  Cookie
  ▼
nginx  localhost:8888
  ├─ /            → ecmdb-web  :3333
  ├─ /cam/        → e-cam-web   :5173
  ├─ /api/cmdb/   → ecmdb       :8000
  ├─ /api/v1/cam/ → e-cam-service :8001
  └─ /api/iam/    → eiam        :9000/api/      ← Phase 1 新增
  ▼
e-cam-service
  ├─ sdk.CheckLogin()   ── HTTP ──▶ eiam POST /api/permission/check_login
  │    └─ ctxutil.WithUserID / WithTenantID / WithOriginTenantID → request context
  ├─ sdk.CheckPolicy()  ── HTTP ──▶ eiam POST /api/permission/check_policy
  │    └─ 经 capability.GetResourceInfo(handler ptr) 自动识别 service/path
  ├─ capability syncer  ── HTTP ──▶ eiam 上报权限资产 manifest
  └─ DAO 层 tenant_plugin 依 ctxutil.GetTenantID 自动隔离
```

## 4. Phase 1：恢复登录共享

目标：让登录态在 ecmdb 与 e-cam-service 之间重新可用，且不再自我破坏。不引入 eiam 依赖，不改后端鉴权模型。

### 4.1 e-cam-web 身份调用改指 eiam

修改 `src/stores/user.ts` 的 `ecmdbAxios` 调用，路径前缀改为 `/api/iam/`。

响应结构变化需处理。eiam `Profile` 返回 `RetrieveUser`：

```
user{id,username,email,nickname,avatar,job_title,phone,status,source,
     ctime,utime,last_login_at,mfa_type,mfa_bound,identities}
tenants[] / current_tenant_id / must_select_tenant / must_bind
is_admin / permissions[] / mfa_required
```

现有 `UserInfo` 为 `{id, username, displayName?, email?, title?, departmentId?, roleCodes?, createType?}`。

**字段消费情况实测**：组件层实际只用到 `username`。`roleCodes` 仅被 store 内的 `hasRole()` 读取，而 `hasRole` 在 store 之外**没有任何调用点**；`hasPermission` 同样没有调用点。二者都是当前未被使用的预留接口。

**决策：采用薄适配层**，在 store 内将 eiam 响应映射为 `UserInfo`，而非全局替换类型定义。理由：被真正消费的字段极少，映射层把改动收敛在一个文件内，避免波及组件层；待 Phase 2 引入租户模型时再统一重构类型。

| `UserInfo` | eiam 来源 |
|---|---|
| `id` | `user.id` |
| `username` | `user.username` |
| `displayName` | `user.nickname` |
| `email` | `user.email` |
| `title` | `user.job_title` |
| `departmentId` / `createType` / `roleCodes` | eiam 无对应字段，一并移除 |

移除 `roleCodes` 需同时删除 `hasRole()`（否则 TS 编译失败）。因其无调用点，删除无影响；将来若需角色判断，应基于 eiam 的 `is_admin` + `permissions` 口径重写，而非恢复旧字段。

`permissions` 的来源同时简化：不再单独调 menus 端点，改由 profile 的 `permissions` 字段提供。已验证 eiam `Profile` 中该字段来自 `permSvc.GetAuthorizedCodes(...)`，类型为 `[]string`，与 `hasPermission` 的 `includes(字符串)` 语义匹配。`fetchPermissions()` 因此可以合并进 `fetchUserInfo()`，`initUserState` 从两次请求减为一次。

> 不可照搬 menus 端点：eiam 的 `GetAuthorizedMenus` 返回 `Data` 直接是 `[]Menu` 对象数组（无 `.menus` 包装，元素含 `id`/`path`/`meta` 等），与现有 `data.data.menus || []` 的取法和 `includes(字符串)` 的用法均不兼容，照搬会静默失效。

同时把 `current_tenant_id` 与 `is_admin` 存入 store，供 Phase 2 的租户切换使用。

### 4.2 nginx 新增 eiam location

`D:\Haven\nginx-1.25.4\conf\vhost\nginx.dev.conf` 追加：

```nginx
location ^~ /api/iam/ {
    proxy_pass http://127.0.0.1:9000/api/;
    proxy_set_header Host $host;
    proxy_set_header Authorization $http_authorization;
    proxy_set_header Cookie $http_cookie;
    proxy_set_header X-Active-Tenant-ID $http_x_active_tenant_id;
}
```

`/api/iam/` 前缀沿用平台既有约定（ecmdb-web 的 vite proxy 已用 `/api/iam` → `:9000` 并 rewrite 去掉前缀）。

注意一条隐式链路：当前 nginx 未命中的路径会落到 `location /` → ecmdb-web:3333，再被其 vite proxy 转发至 eiam。因此 dev 环境下 `/api/iam/*` 可能「偶然可用」，但该路径依赖 ecmdb-web dev server 存活，且生产环境不成立。显式 location 是必要的。

另需补 `X-Active-Tenant-ID` 透传——现有配置只透传 `X-Tenant-ID`，而 eiam 的约定 header 是 `X-Active-Tenant-ID`（`eiam/pkg/web/middleware/tenant.go:15`），ecmdb-web 已改为发送后者。

### 4.3 停止破坏性 cookie 清除

从 `redirectToLogin()`（`src/api/request/index.ts:16`）中移除 `removeEcmdbToken()` 调用。

理由：注销全局凭证是 eiam 的职责（其 `Destroy()` 会正确清理），单个服务的一次 401 不应代替平台注销整个平台的登录态。函数本身保留，供真正的登出流程使用。

### 4.4 check_policy 补 cookie 回退

`internal/shared/middleware/check_policy.go:59` 改为与认证层 carrier 一致的取值顺序：先 `Authorization` 头（去 `Bearer ` 前缀），为空则回退读 cookie `ecmdb-token-key`。

该中间件在 Phase 2 将被整体删除，故此处仅作最小修复，不重构。

Phase 2 后该缺口结构性消失：eiam SDK 的 `callAPI` 会同时透传 `Authorization` 与 `Cookie` 两个头（`eiam/pkg/web/sdk/permission.go`），不存在只认一种载体的问题。

### 4.5 本阶段范围外

- **租户未选择的情形**：eiam 的 `RetrieveUser` 含 `must_select_tenant`，但**该字段在 profile 响应中恒为 `false`**（最终评审核实）：`eiam/internal/web/user/user.go:378-386` 的 `Profile` handler 只赋值 `User` / `Tenants` / `CurrentTenantID` / `IsAdmin` / `Permissions`，`MustSelectTenant` 仅在登录 handler 中赋值。故 Phase 2 判断"尚未选择租户"**必须用 `current_tenant_id === 0`，不能用 `must_select_tenant`** —— 后者结构性地永远为假。
  同理恒为零值的还有 `must_bind` / `bind_token` / `mfa_required` / `mfa_token`（均只在登录 handler 赋值）。前端映射层保留 `mustSelectTenant` 字段无害，但它是一个永远不会为真的 ref。
  Phase 1 只需把租户上下文存入 store 不作处理；租户选择流程属 Phase 2 范围。
- **`cookie.domain` 三份不一致 + `Secure: true`**：localhost 下浏览器对 Secure 破例，且 cookie 实际由 ecmdb-web 的 js-cookie 写入（host-only、无 Secure），故当前不影响 dev。但这是**部署至 IP 或域名时必然触发的故障**：`Secure` cookie 在裸 HTTP 下会被浏览器直接丢弃，且 ecmdb 的 `domain: 106.52.187.69` 在 localhost 访问时域不匹配、服务端 `Set-Cookie` 会被整条丢弃。见 5.6。

### 4.6 验证

按 `http://localhost:8888` 走完整路径：

1. `pnpm typecheck`（即 `vue-tsc -b`）通过 —— 验证移除 `roleCodes` / `hasRole` 后无类型残留。注意不能用 `pnpm build`：它是纯 `vite build`，不做类型检查，查不出残留引用
2. ecmdb 登录 → 确认 `document.cookie` 中存在 `ecmdb-token-key`
3. 进入 `/cam/` → 不再弹「登录已过期」，用户名正常显示
4. Network 中确认 `/api/iam/user/profile` 返回 200 且命中 nginx 新 location（非 ecmdb-web 的 HTML），`permissions` 为字符串数组
5. 再查 `document.cookie` → token 仍在（验证 4.3 生效）
6. `/api/v1/cam/*` 返回 200
7. 刷新页面 → 登录态保持；点击登出 → cookie 被清除且跳转登录页（验证 4.3 未误删真正的登出路径）
8. 复跑 2.2 的自签 token 探测，确认后端认证行为无退化

## 5. Phase 2：完整对齐 ecmdb

目标：e-cam-service 与 ecmdb 形成同构接入，权限可在 eiam 控制台统一治理。

### 5.1 引入依赖与鉴权替换

- `go.mod` 引入 `github.com/Duke1616/eiam`，版本与 ecmdb 对齐（`v0.0.20`，或与 ecmdb 一同升至 `v0.0.21`）
- 新增 `ioc/policy.go`，照 ecmdb 形态提供 `sdk.NewSDK()` 与 `capability.NewSyncer(capability.NewHttpReporter())`
- 配置新增 `policy.auth_url` 指向 eiam
- `ioc/gin.go` 中间件链改为 `sdk.CheckLogin()` + `sdk.CheckPolicy()`，并设 `Engine.ContextWithFallback = true`
- 删除 `EcmdbAuthMiddlewareWithConfig`、`CheckPolicyMiddleware`、自建 `ioc/session.go`
- `TenantMiddleware` 的删除有前置未决项，见下

**租户来源与 `X-Active-Tenant-ID` 的未决问题。** 现有 `TenantMiddleware` 从 `X-Tenant-ID` 头读租户。接入后租户应改由 `sdk.CheckLogin()` 经 `ctxutil` 注入（取自 eiam 会话）。但两点尚未澄清：

1. eiam SDK 的 `callAPI` 只透传 `Authorization` 与 `Cookie`，**不透传 `X-Active-Tenant-ID`**。因此前端发送的活跃租户头不会到达 eiam 的 `check_login`，注入的将是会话租户而非前端请求的租户。
2. eiam 自身通过 `TenancyBuilder` 与 `WithTenantOverride` / `WithTenantSwitch`（`eiam/pkg/web/middleware/tenant.go`）处理该头，但这些依赖本地 `session.Provider`；ecmdb 并未使用它们（只用了同包的 `AccessLogger` 与 `NewCorsBuilder`）。

即 ecmdb 当前如何支持租户切换、e-cam-web 的租户选择器接入后是否仍生效，需在 5.1 实施前实测确认。**在澄清之前不要删除 `TenantMiddleware`**——否则租户切换可能静默失效。这是 Phase 2 的第一个待验证项，优先于其他改动。

### 5.2 清理死链路

- 删除 `api/proto/ecmdb/policy/v1/`、`InitEcmdbPolicyClient`、`InitCheckPolicyMiddleware`
- 删除 `RegisterEndpointsToEcmdb` 及 endpoint gRPC 客户端与 `api/proto/ecmdb/endpoint/v1/`（已被 capability manifest 取代，且该链路本身已失效，见 2.5）
- 修正或移除 `grpc.client.ecmdb.target` 的自指配置

### 5.3 身份读取方式迁移（本阶段主要工作量）

eiam SDK 将身份注入 `context.Context`（经 `ctxutil`），而非 gin key。现状有两类读取方式，处理方式不同：

**第一类：经 helper 读取**（`GetUid` / `GetUsername` / `GetTenantID`，定义于 `internal/shared/middleware/ecmdb_auth.go`）。

跨包限定调用（`middleware.GetXxx(`）实测 **102 处**，分布于 alert、audit、cam 各 handler。此数为下限——同包内的非限定调用不含在内（如 `audit.go:92` 直接调 `GetUsername(c)`），实施时以 `go build` 结果为准，不以此数为完成判据。

采用**保留函数签名、替换内部实现**的方式：函数体改为读 `ctxutil`，调用点无需修改。`ContextWithFallback = true` 保证回退生效。

| helper | 现返回 | 新实现 |
|---|---|---|
| `GetUid(c) int64` | `int64` | `ctxutil.GetUserID(c.Request.Context()).Int64()` |
| `GetTenantID(c) string` | `string` | `ctxutil.GetTenantID(c.Request.Context()).String()` |
| `GetUsername(c) string` | `string` | 见下，需改变取值来源 |

`GetTenantID` 刻意保留 `string` 返回类型（`ContextID.String()`），使调用点零改动。后续若需 `int64` 语义再单独收敛，不在本阶段。

**`GetUsername` 的来源变更。** eiam 的 `sdk.CheckLogin()` 只注入 `uid` / `tenant_id` / `origin_tenant_id`，**不注入 username**；其 HTTP 响应（`check_login`）同样只返回 `uid` 与 `tenant_id`。username 虽在 JWT claims 内（eiam `issueSession` 写入），但接入 SDK 后 e-cam-service 不再自行解析 JWT，为取一个字段而保留本地解析会抵消接入的意义。

该 helper 在全仓仅有一个消费者：`audit.go:92`，写入审计记录的 `OperatorName`。而同处第 91 行已经取了 `uid` 作为 `OperatorID`。

**决策：审计以 uid 为操作人主键，展示名在查询时解析。** 理由有两点，与「少改一处」无关：

1. 审计记录应引用不可变身份。username 是可变的展示数据，用户改名会使历史审计记录与当前身份不一致；uid 不会。
2. 替代方案是在中间件内按 uid 反查 eiam。该中间件作用于所有写请求，等于给每次写操作加一次同步 RPC，代价与收益不匹配。

实施时 `OperatorName` 字段保留（避免动审计表结构），置空或回填 uid 字符串；审计查询接口在返回前按 uid 批量解析展示名。审计表的具体字段处理属实施细节，以不改变已有记录的可读性为准。

**第二类：直接读 gin key，共 14 处，集中在 4 个文件**：

- `internal/cam/cost/handler/allocation_handler.go`
- `internal/cam/cost/handler/budget_handler.go`
- `internal/cam/cost/handler/collector_handler.go`
- `internal/cam/cost/handler/cost_handler.go`

这批绕过了 helper，直接 `ctx.GetString("tenant_id")` 读取旧 `TenantMiddleware` 写入的 gin key。该 key 在中间件删除后不再存在，**14 处会静默取到空字符串**（不报错，但租户过滤失效），必须逐一改为调用 `middleware.GetTenantID(c)`。因是静默失败，需逐点核对，不可依赖编译器发现。

### 5.4 权限资产接入

各 handler 引入 `capability.NewRegistry(service, module, group)`，为路由声明 `Capability(name, code)`。服务名建议沿用 `cam`。启动时经 syncer 上报 eiam。

**规模（运行时权威数据）**：`RegisterEndpointsToEcmdb` 实际尝试注册 **310** 个 `/api/v1/cam/` 端点（取自启动日志 `count: 310`，非 grep 估算）。按模块分布：

| 模块 | 路由数 |
|---|---|
| `internal/cam/web` | 98 |
| `internal/cmdb/web` | 44 |
| `internal/cam/iam/web` | 38 |
| `internal/servicetree/web` | 28 |
| `internal/cam/servicetree/web` | 28 |
| `internal/cam/cost/handler` | 23 |
| `internal/cam/tag` | 16 |
| `internal/cam/template` | 15 |
| `internal/alert/web` | 13 |
| `internal/cam/dictionary` | 12 |
| `internal/cam/dns` | 8 |
| `internal/topology/web` | 7 |

310 个端点逐一声明 `Capability` 不适合单批次完成。**采用分模块渐进接入**：eiam SDK 的 `CheckPolicy()` 对未打标接口默认放行（`capability.GetResourceInfo(ptr)` 未命中即 `ctx.Next()`，见 `eiam/pkg/web/sdk/permission.go`），因此已声明与未声明的模块可以共存，无需一次性完成。

建议顺序：先 `cost`（23）验证机制走通，再按业务重要性推进 `cam/web`（98）等大模块。

**注意：SDK 的 fail-closed 不为此兜底。** 5.6 所述的硬编码 fail-closed 只作用于「鉴权中心不可达」，不作用于「接口未声明权限」——后者走的是 `GetResourceInfo` 未命中即放行的分支。因此渐进接入期间，实际受控端点 = 已声明 `Capability` 的比例（起点 0/310）。这一点须在验收时明确，不可因「已接入 SDK」认定鉴权已收口。

### 5.5 租户隔离

移植 ecmdb 的 `pkg/mongox/plugin/tenant_plugin.go` 到 e-cam-service 的 mongox（当前无 plugin 目录），在 DAO 层自动隔离。需逐集合评估 `SharedConfig`（哪些是共享资源、哪些强制私有、哪些完全豁免）——云资产数据的租户归属规则需业务确认，这是 Phase 2 中唯一需要业务决策的部分。

### 5.6 接入后的行为变化与待核实项

- **eiam 成为请求路径上的硬依赖。** eiam SDK 的 `callAPI` 在鉴权中心不可达时执行 `AbortWithStatus(500)`——**行为硬编码为 fail-closed，无 `fail_mode` 可配**（`fail_mode` 是即将删除的旧中间件的配置项，接入后不再存在）。因此 eiam 宕机等于 e-cam-service 全部接口不可用。这是接入统一鉴权的固有代价，与 ecmdb 现状一致，但部署时须保证 eiam 可用性与 e-cam-service 同级。
- 4.5 中的 `cookie.domain` 与 `Secure` 问题须在部署到 IP/域名前解决
- **token 续期头名待核实**：eiam SDK 回传续期 token 读的是 `x-jwt-token`，而 ginx 的 header carrier 用 `X-Access-Token`。两者是否需要对齐须在 5.1 实施时实测确认，不预设结论。

## 6. 风险与遗留

| 项 | 说明 |
|---|---|
| **当前鉴权等同虚设** | `fail_open` + 两条 gRPC 链路均 `Unimplemented`，生产环境所有带 header 的请求一律放行。Phase 2 修复；若需提前收口，应作为独立变更处理，不要等本设计全部完成 |
| **鉴权覆盖度不等于接入完成** | eiam SDK 对未打标接口默认放行，故 5.4 渐进接入期间，实际受控端点 = 已声明 `Capability` 的比例（起点 0/310）。验收须以覆盖度衡量，不可因「已接 SDK」认定收口 |
| **eiam 成为硬依赖** | SDK 不可达时硬编码 500，无 `fail_mode` 可配。eiam 宕机 = e-cam-service 全量不可用。须保证其可用性与 e-cam-service 同级 |
| eiam 版本 | ecmdb 钉 `v0.0.20`，eiam 仓库已有 `v0.0.21`。两服务应同版本，避免 SDK 与服务端协议漂移 |
| `Secure` cookie + 裸 HTTP | 部署到 IP 时必然故障，见 4.5 |
| 5.3 静默失效风险 | 14 处 `ctx.GetString("tenant_id")` 在旧中间件删除后会取到空串而非报错，**编译器不会发现，租户过滤静默失效**。这是 Phase 2 最需要逐点核对的地方 |
| 5.5 业务决策 | 云资产的租户归属与共享规则需业务确认后方可实施 |
| 审计 `OperatorName` 语义变更 | 见 5.3。新记录不再写入 username，需确认审计查询侧与合规要求可接受 |

## 7. 实施顺序

**Phase 1** — 4.1 至 4.4 四项改动彼此独立、可单独回滚，完成后按 4.6 验证并交付。

**Phase 2** 顺序有硬约束，不是简单的 5.1→5.5：

| 步骤 | 内容 | 约束 |
|---|---|---|
| 0 | 实测澄清租户来源（5.1 末的未决问题） | **前置**。结论决定 `TenantMiddleware` 去留，未澄清前不动它 |
| 1 | 5.1 + 5.2 | **必须同批次**。删除旧中间件与接入新 SDK 不可分离，中间态无法运行 |
| 2 | 5.3 | 完成后服务应可启动并正常通过认证。14 处静默失效点须逐一核对 |
| 3 | 5.4 | **渐进、跨多批次**。建议先 `cost`(23) 验证机制，再推进大模块。每批次独立可验证 |
| 4 | 5.5 | 依赖业务确认租户归属规则，可与 5.4 并行 |

步骤 1 完成后鉴权行为发生实质变化（fail-closed 且 eiam 成为硬依赖），建议在此处设置一个验证关口，确认全部接口仍可正常访问后再继续。

步骤 3 期间系统长期处于「部分端点受控」状态，这是预期形态而非中间缺陷，但须按第 6 节所述以覆盖度衡量进度。
