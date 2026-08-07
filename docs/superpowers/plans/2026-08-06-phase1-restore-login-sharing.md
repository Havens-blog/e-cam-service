# Phase 1：恢复 e-cam-service 登录共享 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 e-cam-web 的身份调用改指 eiam，恢复与 ecmdb 之间的登录态共享，并消除「一次 401 就清掉全平台凭证」的破坏性副作用。

**Architecture:** 不引入 eiam Go 依赖、不改后端鉴权模型。前端把三个已被删除的 ecmdb 端点改指 eiam，映射逻辑抽成纯函数模块以便测试；nginx 补一条显式 location；后端只对 `check_policy` 做最小的 cookie 回退修复。四项改动彼此独立、可单独回滚。

**Tech Stack:** Vue 3 + Pinia + axios + vitest 4.1.5（前端）；Go 1.25.5 + gin 1.11.0 + testify 1.11.1（后端）；nginx 1.25.4。

## Global Constraints

- 设计依据：`e-cam-service/docs/superpowers/specs/2026-08-06-e-cam-service-eiam-integration-design.md`。本计划只实现该文档的 Phase 1（§4.1–4.4），**不做 Phase 2 的任何改动**（不引入 `github.com/Duke1616/eiam`、不删除 `TenantMiddleware`、不接 capability）。
- 三个 eiam 端点固定为：`GET /api/user/profile`、`POST /api/user/logout`、`GET /api/permission/menus`。经 nginx 时前缀为 `/api/iam/`（nginx 会剥掉 `/api/iam` 换成 `/api`）。
- eiam 响应统一是 ginx 信封 `{code, msg, data}`，成功时 `code === 0`。**注意字段名是 `msg` 而非 `message`**。
- eiam 侧字段一律 snake_case（`job_title`、`current_tenant_id`、`is_admin`），前端 `UserInfo` 一律 camelCase，映射层负责转换。
- 前端测试环境是 `environment: 'node'`（`vitest.config.ts:12`），`localStorage` / `document` / `window` **全部为 `undefined`**（已实测）。因此测试只允许覆盖不触碰这些全局对象的纯逻辑；**不得为此新增 happy-dom / jsdom 依赖**。
- `package.json` 中**没有 `test` 脚本**，跑测试用 `npx vitest run <path>`。类型检查用 `pnpm typecheck`（即 `vue-tsc -b`）；**不要用 `pnpm build`**，它是纯 `vite build`，不做类型检查。
- 测试文件必须放在 `src/` 下并以 `.test.ts` 结尾，否则不被 `include: ['src/**/*.test.ts']` 匹配。
- `e-cam-web` 与 `e-cam-service` 是两个独立 git 仓库，各自提交。`D:\Haven\nginx-1.25.4\` **不是 git 仓库**，其中的改动无法提交，需同步改动 `e-cam-web/nginx.dev.conf`（该文件已被 git 跟踪）。
- 提交信息用 conventional commits（`feat:` / `fix:` / `refactor:` / `test:`）。

---

## File Structure

| 文件 | 动作 | 职责 |
|---|---|---|
| `e-cam-web/src/stores/user-mapper.ts` | 创建 | 纯函数：把 eiam profile 响应映射为前端 `UserInfo` 等结构。无任何 DOM / axios / pinia 依赖，因此可在 `node` 环境测试 |
| `e-cam-web/src/stores/user-mapper.test.ts` | 创建 | 上述映射的单元测试 |
| `e-cam-web/src/stores/user.ts` | 修改 | 端点改指 eiam、接入映射层、移除 `roleCodes` / `hasRole`、合并权限请求 |
| `e-cam-web/src/api/request/index.ts` | 修改 `:16-23` | 从 `redirectToLogin()` 移除 `removeEcmdbToken()` |
| `e-cam-web/nginx.dev.conf` | 修改 | 版本控制中的 nginx 配置副本，补 `/api/iam/` location |
| `nginx-1.25.4/conf/vhost/nginx.dev.conf` | 修改 | 实际生效的 nginx 配置，同步同一条 location |
| `e-cam-service/internal/shared/middleware/check_policy.go` | 修改 | 抽出 `extractToken`，补 cookie 回退 |
| `e-cam-service/internal/shared/middleware/check_policy_test.go` | 创建 | `extractToken` 的单元测试 |
| `e-cam-service/ioc/ecmdb.go` | 修改 `:110-113` | 读 `session.cookie.name` 并注入中间件 |

**为什么把映射抽成独立模块**：`user.ts` 在 import 期就执行 `persist: { storage: localStorage }`，而测试环境下 `localStorage === undefined`，该文件根本无法被测试导入。抽出纯函数是在既有约束下唯一能拿到测试覆盖的办法，同时映射逻辑本身也确实该独立于 store 的副作用。

---

## Task 1: nginx 新增 `/api/iam/` location

先做这一项，后续任务的联调验证才有路由可走。

**Files:**
- Modify: `e-cam-web/nginx.dev.conf`（git 跟踪的副本）
- Modify: `D:\Haven\nginx-1.25.4\conf\vhost\nginx.dev.conf`（实际生效，非 git 仓库）

**Interfaces:**
- Consumes: 无
- Produces: `http://localhost:8888/api/iam/*` → `http://127.0.0.1:9000/api/*`，供 Task 3 使用

> **注意**：两份配置文件内容已经不一致（生效的那份多了 `/api/v1/cmdb/` location、少了 `http{}` 外层包装）。**不要用互相覆盖的方式同步**，只在各自文件里插入同一段 location。

- [ ] **Step 1: 确认 eiam 在监听**

```bash
netstat -ano | grep ":9000" | grep LISTENING
```

Expected: 有一行输出。若为空，说明 eiam 未启动，后续验证无法进行 —— 先启动 eiam。

- [ ] **Step 2: 在生效配置中插入 location**

编辑 `D:\Haven\nginx-1.25.4\conf\vhost\nginx.dev.conf`，在 `location ^~ /api/cmdb {` 这个 block 之前插入：

```nginx
        # eiam 统一身份服务: /api/iam/* -> eiam /api/*
        location ^~ /api/iam/ {
            proxy_pass http://127.0.0.1:9000/api/;
            proxy_set_header Host $host;
            proxy_set_header Authorization $http_authorization;
            proxy_set_header Cookie $http_cookie;
            proxy_set_header X-Active-Tenant-ID $http_x_active_tenant_id;
        }

```

`proxy_pass` 末尾的 `/` 是必须的 —— 它让 nginx 把 `/api/iam/` 前缀替换为 `/api/`，而不是拼接。

- [ ] **Step 3: 在 git 跟踪的副本中插入同一段**

编辑 `e-cam-web/nginx.dev.conf`，在 `location ^~ /api/cmdb/ {` 之前插入 Step 2 中完全相同的 location 块（缩进对齐该文件既有风格）。

- [ ] **Step 4: 校验 nginx 配置语法**

```bash
cd /d/Haven/nginx-1.25.4 && ./nginx.exe -t
```

Expected: `syntax is ok` 与 `test is successful`。

实测说明（Task 1 执行时确认）：本机不存在 `conf/nginx.dev.conf`，真正的入口是默认的 `conf/nginx.conf` —— 它第 45 行 `include vhost/*.conf` 加载被改的那份 vhost 配置，第 18 行的 `include mime.types` 是相对路径。因此直接 `./nginx.exe -t`（不带 `-c`）即可校验到插入的 location，也不会触发绝对路径问题。

- [ ] **Step 5: 重载 nginx**

```bash
cd /d/Haven/nginx-1.25.4 && ./nginx.exe -s reload
```

Expected: 无输出即成功。

- [ ] **Step 6: 验证路由已生效**

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8888/api/iam/user/profile
```

Expected: `401`。

关键在于**排除 HTML**（那意味着请求落到了 `location /` → ecmdb-web:3333）。

实测修正（Task 1 执行时确认）：**未认证的 401 响应 body 是空的（`Content-Length: 0`）**，直连 eiam 与经 nginx 一致，对所有路径都如此。所以不能用「body 是 JSON」作为判据 —— 那永远不成立。改用负向对照：

```bash
# 目标路由：应为 401 且 body 长度 0
printf "iam:  code=%s len=%s\n" \
  "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8888/api/iam/user/profile)" \
  "$(curl -s http://localhost:8888/api/iam/user/profile | wc -c)"

# 负向对照：未被任何 API location 匹配的路径，会落到前端，返回 200 + 大段 HTML
printf "ctrl: code=%s len=%s\n" \
  "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8888/api/iam__notmatched)" \
  "$(curl -s http://localhost:8888/api/iam__notmatched | wc -c)"
```

Expected: 第一行 `code=401 len=0`；第二行 `code=200` 且 len 为数万（HTML）。两者不同即证明请求确实进了 eiam 而非落到前端。

> 说明：eiam 对不存在的路径同样返回 401（它在路由之前就 abort），所以 401 只能证明「请求到了 eiam」，不能证明「该路由存在」。三个端点的存在性依据是源码 `eiam/internal/web/user/user.go:88-89` 与 `eiam/internal/web/permission/handler.go:42`，不依赖本步探测。
>
> 注意此结论只适用于**未认证**请求。Task 3 / Task 6 在浏览器中带凭证访问时，profile 返回的是 200 + 完整 JSON body，那里的「响应是 JSON」判据仍然有效。

- [ ] **Step 7: 提交**

只有 `e-cam-web` 那份能提交。

```bash
cd /d/Haven/e-cam-web
git add nginx.dev.conf
git commit -m "feat: add /api/iam/ nginx location for eiam identity service"
```

---

## Task 2: 抽出 eiam profile 映射层（TDD）

**Files:**
- Create: `e-cam-web/src/stores/user-mapper.ts`
- Test: `e-cam-web/src/stores/user-mapper.test.ts`

**Interfaces:**
- Consumes: 无
- Produces: 供 Task 3 使用 ——
  - `interface UserInfo { id: number; username: string; displayName?: string; email?: string; title?: string }`
  - `interface EiamTenant { id: number; name: string; code: string; domain: string }`
  - `interface MappedProfile { userInfo: UserInfo; permissions: string[]; tenants: EiamTenant[]; currentTenantId: number; isAdmin: boolean; mustSelectTenant: boolean }`
  - `function mapEiamProfile(rawData: unknown): MappedProfile | null`

- [ ] **Step 1: 写失败的测试**

创建 `e-cam-web/src/stores/user-mapper.test.ts`：

```ts
import { describe, expect, it } from 'vitest'
import { mapEiamProfile } from './user-mapper'

/** eiam GET /api/user/profile 的 data 部分（真实字段名，snake_case） */
function makeRawProfile(overrides: Record<string, unknown> = {}) {
    return {
        user: {
            id: 42,
            username: 'zhangsan',
            email: 'zhangsan@example.com',
            nickname: '张三',
            job_title: '运维工程师',
            avatar: '',
            phone: '',
            status: 'enabled',
            source: 'ldap',
        },
        tenants: [{ id: 1, name: '默认租户', code: 'default', domain: '' }],
        current_tenant_id: 1,
        must_select_tenant: false,
        is_admin: true,
        permissions: ['cam:asset:view', 'cam:cost:view'],
        ...overrides,
    }
}

describe('mapEiamProfile', () => {
    it('把 snake_case 字段映射为 camelCase 的 UserInfo', () => {
        const result = mapEiamProfile(makeRawProfile())

        expect(result?.userInfo).toEqual({
            id: 42,
            username: 'zhangsan',
            displayName: '张三',
            email: 'zhangsan@example.com',
            title: '运维工程师',
        })
    })

    it('permissions 原样透传为字符串数组', () => {
        const result = mapEiamProfile(makeRawProfile())

        expect(result?.permissions).toEqual(['cam:asset:view', 'cam:cost:view'])
    })

    it('保留租户上下文供后续租户切换使用', () => {
        const result = mapEiamProfile(makeRawProfile())

        expect(result?.currentTenantId).toBe(1)
        expect(result?.isAdmin).toBe(true)
        expect(result?.mustSelectTenant).toBe(false)
        expect(result?.tenants).toEqual([
            { id: 1, name: '默认租户', code: 'default', domain: '' },
        ])
    })

    it('缺失的可选字段降级为安全默认值而非 undefined 传播', () => {
        const result = mapEiamProfile({ user: { id: 7, username: 'lisi' } })

        expect(result?.userInfo).toEqual({
            id: 7,
            username: 'lisi',
            displayName: 'lisi',
            email: undefined,
            title: undefined,
        })
        expect(result?.permissions).toEqual([])
        expect(result?.tenants).toEqual([])
        expect(result?.currentTenantId).toBe(0)
        expect(result?.isAdmin).toBe(false)
    })

    it('nickname 为空串时 displayName 回退到 username', () => {
        const raw = makeRawProfile()
        ;(raw.user as Record<string, unknown>).nickname = ''

        expect(mapEiamProfile(raw)?.userInfo.displayName).toBe('zhangsan')
    })

    it('permissions 含非字符串元素时过滤掉，不污染 hasPermission', () => {
        const result = mapEiamProfile(
            makeRawProfile({ permissions: ['ok', 123, null, 'fine'] })
        )

        expect(result?.permissions).toEqual(['ok', 'fine'])
    })

    it('对无效输入返回 null 而非抛异常', () => {
        expect(mapEiamProfile(null)).toBeNull()
        expect(mapEiamProfile(undefined)).toBeNull()
        expect(mapEiamProfile('not an object')).toBeNull()
        expect(mapEiamProfile({})).toBeNull()
        expect(mapEiamProfile({ user: null })).toBeNull()
        expect(mapEiamProfile({ user: { username: 'no-id' } })).toBeNull()
    })

    it('tenants 不是数组时降级为空数组', () => {
        const result = mapEiamProfile(makeRawProfile({ tenants: 'broken' }))

        expect(result?.tenants).toEqual([])
    })

    it('current_tenant_id 为 0 表示尚未选择租户，如实保留', () => {
        const result = mapEiamProfile(
            makeRawProfile({ current_tenant_id: 0, must_select_tenant: true })
        )

        expect(result?.currentTenantId).toBe(0)
        expect(result?.mustSelectTenant).toBe(true)
    })
})
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /d/Haven/e-cam-web && npx vitest run src/stores/user-mapper.test.ts
```

Expected: FAIL，报无法解析 `./user-mapper`（模块不存在）。

- [ ] **Step 3: 实现映射模块**

创建 `e-cam-web/src/stores/user-mapper.ts`：

```ts
/**
 * eiam profile 响应 → 前端 UserInfo 的映射层。
 *
 * 独立于 store 存在，原因有两点：
 * 1. eiam 侧字段为 snake_case、前端为 camelCase，转换逻辑值得单独收敛与测试。
 * 2. user.ts 在 import 期即访问 localStorage（pinia persist），而测试环境
 *    environment: 'node' 下该全局不存在，故 store 本身无法被测试导入。
 *
 * 本文件不得引入任何 DOM / axios / pinia 依赖，否则将失去可测性。
 */

/** 前端使用的用户信息（camelCase） */
export interface UserInfo {
    id: number
    username: string
    displayName?: string
    email?: string
    title?: string
}

/** eiam 租户（对应 eiam/internal/web/user/vo.go 的 Tenant） */
export interface EiamTenant {
    id: number
    name: string
    code: string
    domain: string
}

/** 映射结果：用户信息 + 权限 + 租户上下文 */
export interface MappedProfile {
    userInfo: UserInfo
    permissions: string[]
    tenants: EiamTenant[]
    /** 0 表示尚未选择租户（临时凭证） */
    currentTenantId: number
    isAdmin: boolean
    mustSelectTenant: boolean
}

function isRecord(v: unknown): v is Record<string, unknown> {
    return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function asString(v: unknown): string | undefined {
    return typeof v === 'string' && v !== '' ? v : undefined
}

function asNumber(v: unknown): number {
    return typeof v === 'number' && Number.isFinite(v) ? v : 0
}

function mapTenants(v: unknown): EiamTenant[] {
    if (!Array.isArray(v)) return []
    return v.filter(isRecord).map((t) => ({
        id: asNumber(t.id),
        name: asString(t.name) ?? '',
        code: asString(t.code) ?? '',
        domain: asString(t.domain) ?? '',
    }))
}

/**
 * 把 eiam `GET /api/user/profile` 的 data 部分映射为前端结构。
 * 输入非法（缺 user 或缺 user.id）时返回 null，由调用方决定降级行为。
 */
export function mapEiamProfile(rawData: unknown): MappedProfile | null {
    if (!isRecord(rawData)) return null

    const user = rawData.user
    if (!isRecord(user)) return null
    if (typeof user.id !== 'number') return null

    const username = asString(user.username) ?? ''

    return {
        userInfo: {
            id: user.id,
            username,
            // nickname 缺失或为空串时回退 username，避免界面出现空白用户名
            displayName: asString(user.nickname) ?? username,
            email: asString(user.email),
            title: asString(user.job_title),
        },
        // eiam 的 permissions 来自 permSvc.GetAuthorizedCodes()，是权限码字符串数组；
        // 过滤非字符串元素，保证 hasPermission 的 includes 语义不被污染
        permissions: Array.isArray(rawData.permissions)
            ? rawData.permissions.filter((p): p is string => typeof p === 'string')
            : [],
        tenants: mapTenants(rawData.tenants),
        currentTenantId: asNumber(rawData.current_tenant_id),
        isAdmin: rawData.is_admin === true,
        mustSelectTenant: rawData.must_select_tenant === true,
    }
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /d/Haven/e-cam-web && npx vitest run src/stores/user-mapper.test.ts
```

Expected: PASS，9 个测试全绿。

- [ ] **Step 5: 提交**

```bash
cd /d/Haven/e-cam-web
git add src/stores/user-mapper.ts src/stores/user-mapper.test.ts
git commit -m "feat: add eiam profile mapping layer with unit tests"
```

---

## Task 3: store 端点改指 eiam

**Files:**
- Modify: `e-cam-web/src/stores/user.ts`
- Modify: `e-cam-web/src/router/index.ts:62`（删除一行冗余调用）

**Interfaces:**
- Consumes: Task 2 的 `mapEiamProfile()`、`UserInfo`、`EiamTenant`、`MappedProfile`；Task 1 的 `/api/iam/` 路由
- Produces: store 暴露 `userInfo`、`isLoggedIn`、`permissions`、`tenants`、`currentTenantId`、`isAdmin`、`mustSelectTenant`、`setUserInfo`、`fetchUserInfo`、`logout`、`initUserState`、`hasPermission`。**`fetchPermissions` 与 `hasRole` 被移除**

**外部消费点（Task 3 执行时修正）**：本计划原先断言「只有 `src/router/index.ts:57` 的 `fetchUserInfo()`，故无需改路由」—— **该断言是错的**，漏看了紧邻的第 62 行：

```ts
// src/router/index.ts:56-63 现状
if (!userStore.isLoggedIn) {
    const success = await userStore.fetchUserInfo()
    if (!success) {
        redirectToLogin()
        return
    }
    await userStore.fetchPermissions()   // ← 第 62 行，store 中已被移除
}
```

`fetchUserInfo()` 签名不变（仍返回 `Promise<boolean>`），第 57 行无需改动。但第 62 行必须删除，否则 `fetchPermissions` 为 `undefined`，路由守卫首次导航即抛错、全部路由无法解析。

删除是正确解而非退让：第 57 行的 `fetchUserInfo()` 现已通过 mapper 一并填充 `permissions`，第 62 行纯属冗余 —— 这正是本计划「`initUserState` 从两次请求降为一次」的本意。**不得为消除编译错误而恢复 `fetchPermissions`**，那会直接违反该设计决定。

已核实 `fetchPermissions` 在全仓仅此一处消费点（`grep -rn "fetchPermissions" src`）。

- [ ] **Step 1: 改写 store**

把 `e-cam-web/src/stores/user.ts` 整体替换为：

```ts
import { redirectToLogin } from '@/api/request/index'
import { getEcmdbToken, removeEcmdbToken } from '@/utils/cookie'
import axios from 'axios'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { mapEiamProfile } from './user-mapper'
import type { EiamTenant, UserInfo } from './user-mapper'

export type { UserInfo } from './user-mapper'

/**
 * eiam 统一身份服务专用 axios 实例。
 * 经 nginx: /api/iam/* -> eiam :9000 /api/*
 */
const eiamAxios = axios.create({
    timeout: 15000,
    withCredentials: true,
    headers: { 'Content-Type': 'application/json' },
})

// 请求拦截：注入 session token（从 cookie 读取）
eiamAxios.interceptors.request.use((config) => {
    const token = getEcmdbToken()
    if (token && config.headers) {
        config.headers.Authorization = `Bearer ${token}`
    }
    return config
})

// 响应拦截：401 跳转登录
eiamAxios.interceptors.response.use(
    (response) => response,
    (error) => {
        if (error.response?.status === 401) {
            redirectToLogin()
        }
        return Promise.reject(error)
    }
)

/**
 * eiam 统一鉴权下的用户状态管理
 */
export const useUserStore = defineStore(
    'user',
    () => {
        const userInfo = ref<UserInfo | null>(null)
        const isLoggedIn = ref(false)
        const permissions = ref<string[]>([])
        const tenants = ref<EiamTenant[]>([])
        const currentTenantId = ref<number>(0)
        const isAdmin = ref(false)
        const mustSelectTenant = ref(false)

        const setUserInfo = (info: UserInfo | null) => {
            userInfo.value = info
            isLoggedIn.value = !!info
        }

        const resetState = () => {
            setUserInfo(null)
            permissions.value = []
            tenants.value = []
            currentTenantId.value = 0
            isAdmin.value = false
            mustSelectTenant.value = false
        }

        /**
         * 从 eiam 获取当前登录用户信息。
         * eiam 的 profile 同时返回权限码与租户上下文，故一次请求即可，
         * 无需再单独调权限菜单接口。
         */
        const fetchUserInfo = async (): Promise<boolean> => {
            try {
                const res = await eiamAxios.get('/api/iam/user/profile')
                const body = res.data
                // eiam 使用 ginx 信封：{code, msg, data}，成功时 code === 0
                if (body?.code !== 0) {
                    // 三条失败路径都只向调用方返回 false，而调用方（路由守卫）会据此
                    // 跳登录页。本 store 无自动化测试覆盖（node 环境无法导入），
                    // 控制台是唯一的可观测手段，故三处必须各自留痕、可区分。
                    console.warn('[user] eiam profile 返回非成功信封', {
                        code: body?.code,
                        msg: body?.msg,
                    })
                    return false
                }

                const mapped = mapEiamProfile(body.data)
                if (!mapped) {
                    console.warn('[user] eiam profile 载荷无法解析，已按未登录处理')
                    return false
                }

                setUserInfo(mapped.userInfo)
                permissions.value = mapped.permissions
                tenants.value = mapped.tenants
                currentTenantId.value = mapped.currentTenantId
                isAdmin.value = mapped.isAdmin
                mustSelectTenant.value = mapped.mustSelectTenant
                return true
            } catch (err) {
                // 网络失败 / 超时 / 非 2xx 都落到这里，与上面两条业务失败区分开
                console.warn('[user] 请求 eiam profile 失败', err)
                return false
            }
        }

        /**
         * 登出：通知 eiam 销毁会话，再清理本地状态与 cookie。
         * 这是唯一应当主动清除共享凭证的路径。
         */
        const logout = async () => {
            try {
                await eiamAxios.post('/api/iam/user/logout')
            } catch {
                // 即使登出接口失败也清理本地状态
            }
            resetState()
            removeEcmdbToken()
            const loginUrl = import.meta.env.VITE_ECMDB_LOGIN_URL || '/login'
            window.location.href = loginUrl
        }

        const initUserState = async () => {
            await fetchUserInfo()
        }

        const hasPermission = (permission: string): boolean => {
            return permissions.value.includes(permission)
        }

        return {
            userInfo,
            isLoggedIn,
            permissions,
            tenants,
            currentTenantId,
            isAdmin,
            mustSelectTenant,
            setUserInfo,
            fetchUserInfo,
            logout,
            initUserState,
            hasPermission,
        }
    },
    {
        persist: {
            // key 从 'cam-user' 提升为 v2：本任务把 store 的 ref 从 3 个增至 7 个，
            // 旧 blob 带 isLoggedIn:true 复原后会让路由守卫跳过 profile 拉取（见下方说明）
            key: 'cam-user-v2',
            storage: localStorage,
            // 只持久化 userInfo（仅为冷启动时即时渲染用户名）。
            // 授权派生字段 permissions / isAdmin / tenants / currentTenantId /
            // mustSelectTenant 一律不落盘：它们必须每次从 eiam 取，
            // 否则用户可编辑 localStorage 伪造 isAdmin，且撤权无法生效。
            pick: ['userInfo'],
        },
    }
)
```

**persist 配置的修正（Task 3 评审后，经用户裁定）**

本计划最初逐字给出的是 `persist: { key: 'cam-user', storage: localStorage }`（无 `pick`），与改动前完全相同。该写法有缺陷：

- pinia 无 `pick` 时持久化**全部** state。本任务把 ref 从 3 个（`userInfo`/`isLoggedIn`/`permissions`）增至 7 个，于是 `isAdmin`、`tenants`、`currentTenantId`、`mustSelectTenant` 这些**授权派生数据**在 key 未变的情况下被静默写入 localStorage。
- 更严重的是与路由守卫的交互：`router/index.ts` 仅在 `if (!userStore.isLoggedIn)` 时拉 profile，而 `isLoggedIn` 本身是从 localStorage 复原的。因此任何已存在的 `cam-user` 旧 blob（带 `isLoggedIn: true`）复原后，**profile 根本不会被拉取** —— `tenants` 恒为 `[]`、`currentTenantId` 恒为 `0`，即一个 eiam 永远不会产生的状态。这使 Task 3 对所有老用户失效，正是 Phase 1 要修的那类问题。
- 同一机制的线上表现：cookie 是共享的，若用户在另一标签页以别的账号重新登录 ecmdb，cam 源的 localStorage 仍说 `isLoggedIn: true`，守卫跳过拉取，界面渲染 A 的身份与 `isAdmin` 而请求携带 B 的 token。且权限**撤销**返回 403 而非 401，被撤权的管理员会无限期保留管理员界面。

裁定：改 key 丢弃旧 blob + `pick: ['userInfo']`。`isLoggedIn` 不再入缓存，守卫每次必拉 profile，授权态永远新鲜；用户名仍从缓存瞬时渲染，无白屏。

已核实 `pick` 是本项目 `pinia-plugin-persistedstate@4.7.1` 的有效选项（`dist/index.d.ts:65`）。**注意不要写成 v3 的 `paths`** —— 那会静默回退为持久化全部字段，使本修复失效。

改动要点：
- `POST /api/cmdb/user/info` → `GET /api/iam/user/profile`（注意方法由 POST 变 GET）
- 删除 `fetchPermissions()` 与那次 `/api/cmdb/permission/get_user_menu` 请求，权限改由 profile 提供，`initUserState` 从两次请求降为一次
- 删除 `hasRole()` 与 `UserInfo.roleCodes` / `departmentId` / `createType`（eiam 无对应字段，且 `hasRole` 全仓无调用点）
- `logout` 保留 `removeEcmdbToken()` —— 这是唯一正确的清除时机，与 Task 4 要移除的那处不同

- [ ] **Step 2: 类型检查**

```bash
cd /d/Haven/e-cam-web && pnpm typecheck
```

Expected: 通过，无错误。若报某处引用了 `roleCodes` 或 `hasRole`，说明存在我未发现的消费者 —— 停下来评估：`hasRole` 应基于 eiam 的 `isAdmin` / `permissions` 重写，**不要**恢复 `roleCodes` 字段。

- [ ] **Step 3: 确认映射层测试仍然通过**

```bash
cd /d/Haven/e-cam-web && npx vitest run
```

Expected: PASS。

- [ ] **Step 4: 浏览器联调**

前提：nginx 已 reload（Task 1）、eiam 在 :9000、ecmdb 在 :8000、e-cam-service 在 :8001、两个前端 dev server 均在运行。

1. 访问 `http://localhost:8888/` 并登录 ecmdb
2. Console 执行 `document.cookie`，确认存在 `ecmdb-token-key`
3. 访问 `http://localhost:8888/cam/`
4. Expected: **不再弹「登录已过期，请重新登录」**，用户名正常显示
5. Network 中找到 `/api/iam/user/profile`：状态 200、响应是 JSON（非 HTML）、`data.permissions` 是字符串数组
6. 再次执行 `document.cookie`，确认 `ecmdb-token-key` **仍然存在**
7. 确认 `/api/v1/cam/*` 请求返回 200

- [ ] **Step 5: 提交**

```bash
cd /d/Haven/e-cam-web
git add src/stores/user.ts
git commit -m "feat: point identity calls to eiam, drop removed ecmdb endpoints"
```

---

## Task 4: 移除破坏性 cookie 清除

一次误判的 401 不应代替平台注销全局凭证。这是「故障自我扩散」的根源：e-cam 报一次过期，ecmdb 的登录态也被一起清掉。

**Files:**
- Modify: `e-cam-web/src/api/request/index.ts:16-23`

**Interfaces:**
- Consumes: 无
- Produces: `redirectToLogin()` 行为变更 —— 只提示并跳转，不再删 cookie

- [ ] **Step 1: 修改 `redirectToLogin`**

`e-cam-web/src/api/request/index.ts` 中，把：

```ts
export function redirectToLogin() {
    if (isRedirectingToLogin) return
    isRedirectingToLogin = true
    removeEcmdbToken()
    ElMessage.warning('登录已过期，请重新登录')
    const ecmdbLoginUrl = import.meta.env.VITE_ECMDB_LOGIN_URL || '/login'
    const currentUrl = window.location.href
    window.location.href = `${ecmdbLoginUrl}?redirect=${encodeURIComponent(currentUrl)}`
}
```

改为：

```ts
export function redirectToLogin() {
    if (isRedirectingToLogin) return
    isRedirectingToLogin = true
    // 注意：这里不清除 ecmdb-token-key。
    // 该 cookie 是 ecmdb / eiam / e-cam-service 共用的平台凭证，注销它是 eiam
    // logout 的职责。本函数的触发条件（单次请求 401、或路由守卫判定无 token /
    // 拉取用户信息失败）全都只是本应用的局部判断；若在此删除 cookie，会把其他
    // 服务（尤其 ecmdb）的登录态一并清掉，使局部故障扩散为全平台掉线。
    ElMessage.warning('登录状态已失效，请重新登录')
    const ecmdbLoginUrl = import.meta.env.VITE_ECMDB_LOGIN_URL || '/login'
    const currentUrl = window.location.href
    window.location.href = `${ecmdbLoginUrl}?redirect=${encodeURIComponent(currentUrl)}`
}
```

- [ ] **Step 2: 处理 `removeEcmdbToken` 的 import**

`removeEcmdbToken` 现在在本文件中已无调用点。检查：

```bash
cd /d/Haven/e-cam-web && grep -n "removeEcmdbToken" src/api/request/index.ts
```

若只剩第 1 行的 import，把它从 import 语句中删掉（保留 `getEcmdbToken`）。

**这一步是必须的，不是清洁工作**（Task 4 执行前核实）：`noUnusedLocals: true` 同时配置在 `tsconfig.app.json:13` 与 `tsconfig.node.json:19`，未使用的 import 会让 Step 3 的 `pnpm typecheck` 直接失败并报 TS6133（`'removeEcmdbToken' is declared but its value is never read`）。ESLint 那条 `@typescript-eslint/no-unused-vars` 只是 `warn`（`eslint.config.js:14`），不是真正的关卡 —— 真正拦住的是 typecheck。

**不要删除 `src/utils/cookie.ts` 里的 `removeEcmdbToken` 函数本身** —— Task 3 的 `logout` 仍在用它（已核实全仓 5 处引用，`src/stores/user.ts:107` 是保留方）。

- [ ] **Step 3: 类型检查与 lint**

```bash
cd /d/Haven/e-cam-web && pnpm typecheck
```

Expected: 通过。

- [ ] **Step 4: 验证登出路径未被破坏**

浏览器中：

1. 正常登录后进入 `/cam/`
2. 刷新页面 → 登录态保持（不跳登录页）
3. 点击登出 → Expected: 跳转登录页，且 `document.cookie` 中 `ecmdb-token-key` **已被清除**

第 3 步是关键：它证明移除的只是误触发路径，真正的登出仍然会清 cookie。

- [ ] **Step 5: 提交**

```bash
cd /d/Haven/e-cam-web
git add src/api/request/index.ts
git commit -m "fix: stop clearing shared platform cookie on a single 401"
```

---

## Task 5: `check_policy` 补 cookie 回退（TDD）

认证层的 mixin carrier 接受 header 或 cookie 两种载体，策略层却只读 header，导致纯 cookie 携带的请求必然 403。

**Files:**
- Modify: `e-cam-service/internal/shared/middleware/check_policy.go`
- Create: `e-cam-service/internal/shared/middleware/check_policy_test.go`
- Modify: `e-cam-service/ioc/ecmdb.go:110-113`

**Interfaces:**
- Consumes: 无
- Produces:
  - `func extractToken(c *gin.Context, cookieName string) string`（包内）
  - `NewCheckPolicyMiddleware(policyClient policyv1.PolicyServiceClient, cfg PolicyConfig, cookieName string, logger *elog.Component) *CheckPolicyMiddleware` —— **签名新增 `cookieName` 参数**

该构造函数目前唯一调用点是 `ioc/ecmdb.go:113`（`ioc/wire_gen.go:26` 调的是 `InitCheckPolicyMiddleware`，不受影响）。

> 该中间件在 Phase 2 会被整体删除，因此这里只做最小修复，不重构。

- [ ] **Step 1: 写失败的测试**

创建 `e-cam-service/internal/shared/middleware/check_policy_test.go`：

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// newTestCtx 构造一个带指定 header/cookie 的 gin.Context
func newTestCtx(headers map[string]string, cookies map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/whatever", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	c.Request = req
	return c
}

func TestExtractToken(t *testing.T) {
	const cookieName = "ecmdb-token-key"

	testCases := []struct {
		name    string
		headers map[string]string
		cookies map[string]string
		want    string
	}{
		{
			name:    "Authorization 头存在时直接使用",
			headers: map[string]string{"Authorization": "Bearer header.jwt.token"},
			want:    "Bearer header.jwt.token",
		},
		{
			name:    "无 Authorization 头时回退到 cookie，并补齐 Bearer 前缀",
			cookies: map[string]string{cookieName: "cookie.jwt.token"},
			want:    "Bearer cookie.jwt.token",
		},
		{
			name:    "两者都在时优先 header，与认证层 mixin carrier 顺序一致",
			headers: map[string]string{"Authorization": "Bearer header.jwt.token"},
			cookies: map[string]string{cookieName: "cookie.jwt.token"},
			want:    "Bearer header.jwt.token",
		},
		{
			name: "两者都不存在时返回空串",
			want: "",
		},
		{
			name:    "cookie 名不匹配时不误取",
			cookies: map[string]string{"some-other-cookie": "irrelevant"},
			want:    "",
		},
		{
			name:    "cookie 值为空串时视作不存在",
			cookies: map[string]string{cookieName: ""},
			want:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractToken(newTestCtx(tc.headers, tc.cookies), cookieName)
			assert.Equal(t, tc.want, got)
		})
	}
}

// cookieName 为空时不应回退到 cookie —— 避免配置缺失导致读错 cookie
func TestExtractToken_EmptyCookieName(t *testing.T) {
	c := newTestCtx(nil, map[string]string{"ecmdb-token-key": "cookie.jwt.token"})

	assert.Equal(t, "", extractToken(c, ""))
}

// policyClient 为 nil 时中间件直接放行，携带 cookie 的请求不应被 403
func TestCheckPolicyMiddleware_NilClientPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := NewCheckPolicyMiddleware(nil, PolicyConfig{}, "ecmdb-token-key", nil)

	engine := gin.New()
	engine.Use(m.Build())
	engine.GET("/api/v1/cam/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/ping", nil)
	req.AddCookie(&http.Cookie{Name: "ecmdb-token-key", Value: "cookie.jwt.token"})
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /d/Haven/e-cam-service && go test ./internal/shared/middleware/ -run TestExtractToken -v
```

Expected: 编译失败，`undefined: extractToken`。

- [ ] **Step 3: 实现 `extractToken` 并接入**

编辑 `e-cam-service/internal/shared/middleware/check_policy.go`。

3a. 给 struct 增加字段（在 `whitelist []string` 之后）：

```go
	cookieName string // session cookie 名，用于 Authorization 头缺失时回退取 token
```

3b. 构造函数增加参数并赋值：

```go
func NewCheckPolicyMiddleware(policyClient policyv1.PolicyServiceClient, cfg PolicyConfig, cookieName string, logger *elog.Component) *CheckPolicyMiddleware {
	failMode := cfg.FailMode
	if failMode == "" {
		failMode = "fail_open"
	}
	return &CheckPolicyMiddleware{
		policyClient: policyClient,
		logger:       logger,
		resource:     "CAM",
		failMode:     failMode,
		whitelist:    cfg.Whitelist,
		cookieName:   cookieName,
	}
}
```

3c. 在文件末尾追加取值函数：

```go
// extractToken 取出请求携带的 token，顺序与认证层的 mixin TokenCarrier 保持一致：
// 先 Authorization 头，为空则回退 cookie。
//
// 认证中间件通过 mixin carrier 同时接受 header 与 cookie 两种载体，若此处只读
// header，纯 cookie 携带的请求会在通过认证后被本中间件判为「Token为空」而 403。
//
// cookie 分支补上 "Bearer " 前缀，使下游收到的格式与 header 分支一致（header
// 分支保持原样透传，不改变既有行为）。
func extractToken(c *gin.Context, cookieName string) string {
	if h := c.GetHeader("Authorization"); h != "" {
		return h
	}
	if cookieName == "" {
		return ""
	}
	if v, err := c.Cookie(cookieName); err == nil && v != "" {
		return "Bearer " + v
	}
	return ""
}
```

3d. 把 `Build()` 中的取值改为调用它 —— 将：

```go
		// 从 Authorization 头获取 token
		token := c.GetHeader("Authorization")
		if token == "" {
			m.logger.Warn("策略检查: Token为空")
```

改为：

```go
		// 取 token：Authorization 头优先，回退 cookie（与认证层 carrier 一致）
		token := extractToken(c, m.cookieName)
		if token == "" {
			m.logger.Warn("策略检查: Token为空",
				elog.String("path", c.Request.URL.Path))
```

- [ ] **Step 4: 更新调用方**

`e-cam-service/ioc/ecmdb.go` 中把 `InitCheckPolicyMiddleware` 改为：

```go
// InitCheckPolicyMiddleware 初始化策略检查中间件
func InitCheckPolicyMiddleware(policyClient policyv1.PolicyServiceClient) *middleware.CheckPolicyMiddleware {
	var cfg middleware.PolicyConfig
	_ = viper.UnmarshalKey("policy", &cfg)
	// cookie 名与认证层共用同一份配置，保证两层取 token 的来源一致
	cookieName := viper.GetString("session.cookie.name")
	return middleware.NewCheckPolicyMiddleware(policyClient, cfg, cookieName, elog.DefaultLogger)
}
```

`viper` 与 `elog` 在该文件中已被 import，无需新增。

- [ ] **Step 5: 运行测试确认通过**

```bash
cd /d/Haven/e-cam-service && go test ./internal/shared/middleware/ -v -race
```

Expected: PASS，`TestExtractToken` 的 6 个子测试 + `TestExtractToken_EmptyCookieName` + `TestCheckPolicyMiddleware_NilClientPassesThrough` 全绿。

- [ ] **Step 6: 确认整个项目仍可编译**

```bash
cd /d/Haven/e-cam-service && go build ./...
```

Expected: 无输出。若报 `NewCheckPolicyMiddleware` 参数数量不符，说明存在我未发现的第二个调用点 —— 按同样方式补上 `cookieName` 实参。

- [ ] **Step 7: 提交**

```bash
cd /d/Haven/e-cam-service
git add internal/shared/middleware/check_policy.go internal/shared/middleware/check_policy_test.go ioc/ecmdb.go
git commit -m "fix: fall back to session cookie when Authorization header is absent"
```

---

## Task 6: 端到端回归验证

**Files:** 无改动，仅验证。

**Interfaces:**
- Consumes: Task 1–5 的全部产出
- Produces: Phase 1 可交付的结论

- [ ] **Step 1: 重启 e-cam-service 使 Task 5 生效**

按你既有的启动方式重启（当前是 `go run . start`，配置默认取 `config/prod.yaml`）。

- [ ] **Step 2: 确认四个服务在监听**

```bash
netstat -ano | grep -E ":8000|:8001|:8888|:9000" | grep LISTENING
```

Expected: 四个端口都在。

- [ ] **Step 3: 走完整用户路径**

按设计文档 §4.6 的 8 步逐条执行：

1. `pnpm typecheck` 通过（Task 3 Step 2 已验，此处复核）
2. `http://localhost:8888/` 登录 ecmdb → `document.cookie` 含 `ecmdb-token-key`
3. 进入 `/cam/` → 不弹「登录已过期」，用户名正常显示
4. Network 中 `/api/iam/user/profile` 返回 200 且是 JSON（非 HTML），`data.permissions` 为字符串数组
5. 再查 `document.cookie` → token 仍在
6. `/api/v1/cam/*` 返回 200
7. 刷新页面 → 登录态保持；点击登出 → cookie 被清除且跳转登录页
8. 复跑自签 token 探测（Step 6）

- [ ] **Step 4: 核实持久化行为（Task 3 评审指出的最大缺口）**

Task 3 把 persist 从「全量落盘、key 不变」改为 `key: 'cam-user-v2'` + `pick: ['userInfo']`。该修复的**运行时效果全部是推理得出的，未经观察** —— 而它要修的恰恰是「类型检查通过但行为错误」那一类问题，正是推理最不可靠之处。必须实测。

登录后在 Console 执行：

```js
// 1. 新 key 存在，且只含 userInfo
JSON.parse(localStorage.getItem('cam-user-v2'))
```

Expected: 对象**只有** `userInfo` 一个顶层键。**不得**出现 `isAdmin` / `tenants` / `permissions` / `currentTenantId` / `mustSelectTenant` / `isLoggedIn` —— 若出现任何一个，说明 `pick` 未生效（最可能是被写成了 v3 的 `paths`），授权态又被写进了用户可编辑的 localStorage。

```js
// 2. 旧 key 已不再被写入
localStorage.getItem('cam-user')
```

Expected: `null`（或仅剩改名前的历史残留，不会被读取）。

```js
// 3. 关键：植入一个伪造的旧 blob，确认它被忽略
localStorage.setItem('cam-user', JSON.stringify({
    userInfo: { id: 999, username: 'STALE_USER' },
    isLoggedIn: true,
    isAdmin: true
}))
location.reload()
```

Expected: 页面**不显示** `STALE_USER`，且 Network 中能看到 `/api/iam/user/profile` **被重新请求**。这证明旧 blob 无法再绕过路由守卫 —— 即 Task 3 那个「老用户 profile 永不拉取」的缺陷确实已消除。测完清掉：`localStorage.removeItem('cam-user')`。

- [ ] **Step 5: 重定向循环检查（Task 4 评审指出的新增前置风险）**

Task 4 让 401 时不再清除 cookie，这是本阶段的目的，但它同时移除了旧的自限行为：从前那次删除会打断循环。`isRedirectingToLogin` 帮不上 —— 每一轮都是全新页面加载，模块重新初始化。

```js
// 把 cookie 篡改为无效值（保留 cookie 存在，只让它无效）
document.cookie = 'ecmdb-token-key=invalid.jwt.value; path=/'
```

然后访问 `http://localhost:8888/cam/`。

**判据（已按最终评审修正 —— 原判据是错的，会放过真实缺陷）**

原先写的是「落在登录页并停住，若地址栏反复闪烁即为循环」。评审追踪 ecmdb-web 的守卫后证明实际故障形态**不是**循环：

`ecmdb-web/src/router/guard.ts:25-27` 有 `if (to.path === LOGIN_PATH && getToken()) return "/"`。Task 4 让 cookie 在 401 后存活，于是 `getToken()` 为真 —— 用户被弹到 `/login` 时，ecmdb-web 会把他再重定向到 `/`，**渲染 ecmdb 首页而非登录表单**。全过程无地址栏闪烁、无重复请求对，所以原判据会判为 PASS，而用户实际上被静默停在 ecmdb 首页、无法进入 cam 应用（再输入 `/cam/` 只是重复这一趟）。

Expected（修正后）：**用户必须落在一个能重新认证的地方**。具体确认三件事：

1. 最终停在哪个 URL —— 若是 ecmdb 首页 `/` 而非登录表单，即为缺陷（不是循环，是死胡同）
2. 该页面上是否真的存在登录表单（用户名/密码输入框），而不只是"看起来像登录页"
3. 是否能从该处完成登录并回到 `/cam/`

若落在首页：这是 Phase 1 有意保留 cookie 的副作用，修法在 `redirectToLogin()` 侧（例如带一个 ecmdb-web 会尊重的强制参数，或让 ecmdb-web 在 URL 带 `redirect` 参数时跳过上述 token 短路），不要靠恢复"401 就删 cookie"来绕过 —— 那会退回本阶段修掉的缺陷。

测完重新登录以恢复有效 cookie。

- [ ] **Step 6: 复跑后端认证探测，确认无退化**

用已知密钥自签 token，打不存在的路径（401 = 认证被拒，404 = 认证通过）：

```bash
KEY='1234567890Key'
b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
NOW=$(date +%s); EXP=$((NOW + 3600)); EXPMS=$(( (NOW + 3600) * 1000 ))
HDR=$(printf '%s' '{"alg":"HS256","typ":"JWT"}' | b64url)
PAY=$(printf '{"data":{"Uid":1,"SSID":"probe-ssid-0001","Data":{"username":"probe"},"Expiration":%s},"exp":%s,"iat":%s}' "$EXPMS" "$EXP" "$NOW" | b64url)
SIG=$(printf '%s' "$HDR.$PAY" | openssl dgst -sha256 -hmac "$KEY" -binary | b64url)
TOKEN="$HDR.$PAY.$SIG"

echo -n "A) 无凭证        期望 401 -> "; curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8001/api/v1/cam/__probe_nonexistent
echo -n "B) header token  期望 404 -> "; curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8001/api/v1/cam/__probe_nonexistent
echo -n "C) cookie token  期望 404 -> "; curl -s -o /dev/null -w "%{http_code}\n" -H "Cookie: ecmdb-token-key=$TOKEN" http://127.0.0.1:8001/api/v1/cam/__probe_nonexistent
echo -n "D) 垃圾 token    期望 401 -> "; curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer garbage.garbage.garbage" http://127.0.0.1:8001/api/v1/cam/__probe_nonexistent
```

Expected: A=401、B=404、C=**404**、D=401。

**C 是本次修复的核心判据**：修复前它是 403（认证已过但被策略层判为 Token 为空），修复后应与 B 一致。

- [ ] **Step 7: 确认日志中不再出现「Token为空」**

```bash
cd /d/Haven/e-cam-service && grep -a "策略检查: Token为空" logs/app.log | tail -5
```

Expected: 没有 Step 6 执行之后的新增记录（老记录仍在，按时间戳区分）。

---

## Self-Review

**Spec coverage（对照设计文档 Phase 1）：**

| 设计文档 | 对应任务 |
|---|---|
| §4.1 e-cam-web 身份调用改指 eiam | Task 2（映射层）+ Task 3（store 改写） |
| §4.2 nginx 新增 eiam location | Task 1 |
| §4.3 停止破坏性 cookie 清除 | Task 4 |
| §4.4 check_policy 补 cookie 回退 | Task 5 |
| §4.5 本阶段范围外（租户未选择、cookie domain/Secure） | 不实现；`mustSelectTenant` / `currentTenantId` 仅存入 store 备用，符合「只存不处理」 |
| §4.6 验证 8 步 | Task 6 |

Phase 2（§5）全部不在本计划范围内，符合 Global Constraints。

**类型一致性核对：**
- `mapEiamProfile` 在 Task 2 定义、Task 3 消费，返回 `MappedProfile | null`，字段名与 Task 3 的赋值逐一对应
- `UserInfo` 移至 `user-mapper.ts`，`user.ts` 用 `export type { UserInfo }` 再导出，保证任何既有导入不断
- `extractToken(c *gin.Context, cookieName string) string` 在 Task 5 定义、同任务内被 `Build()` 与测试共同使用
- `NewCheckPolicyMiddleware` 的新签名在 Task 5 Step 3b 定义、Step 4 更新唯一调用点、测试中按新签名调用（`nil` client + `nil` logger）

**已知的实测约束（不是遗漏）：**
- store 本身无单元测试：`environment: 'node'` 下 `localStorage` 不存在，store 在 import 期即访问它。已通过抽出纯函数拿到映射逻辑的覆盖，store 的接线由 Task 3 Step 4 的浏览器联调验证。
- `nginx-1.25.4/` 非 git 仓库，其改动无法提交，Task 1 同步修改了 git 跟踪的 `e-cam-web/nginx.dev.conf`。两文件内容本就不同，故明确要求分别插入而非互相覆盖。
- Task 5 中 cookie 分支补 `"Bearer "` 前缀这一选择无法验证正确性 —— 目标 gRPC 服务 `PolicyService` 并不存在（`Unimplemented`），发给它的 token 格式不可观测。选择它是为了与 header 分支格式一致；该中间件在 Phase 2 整体删除。

---

## Execution Handoff

Plan complete and saved to `e-cam-service/docs/superpowers/plans/2026-08-06-phase1-restore-login-sharing.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
