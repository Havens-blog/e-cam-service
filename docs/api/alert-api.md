# 告警通知 API 文档

## 概述

告警通知模块提供资源变更通知、同步失败告警、资源过期提醒等能力，支持钉钉、企业微信、飞书、邮件四种通知渠道。

所有接口需要 `X-Tenant-ID` Header。

基础路径: `/api/v1/cam/alert`

---

## 一、通知渠道管理

> 通知渠道是告警的发送目标，需要先创建渠道，再在告警规则中引用。

### 1.1 创建通知渠道

`POST /api/v1/cam/alert/channels`

**请求体:**

```json
{
  "name": "运维钉钉群",
  "type": "dingtalk",
  "config": {
    "webhook": "https://oapi.dingtalk.com/robot/send?access_token=xxx",
    "secret": "SECxxx"
  }
}
```

**type 枚举值及对应 config:**

| type       | 说明     | config 字段                                                             |
| ---------- | -------- | ----------------------------------------------------------------------- |
| `dingtalk` | 钉钉     | `webhook` (必填), `secret` (选填, 加签密钥)                             |
| `wecom`    | 企业微信 | `webhook` (必填)                                                        |
| `feishu`   | 飞书     | `webhook` (必填), `secret` (选填, 签名密钥)                             |
| `email`    | 邮件     | `smtp_host`, `smtp_port`, `smtp_user`, `smtp_pass`, `from`, `to` (数组) |

**钉钉示例:**

```json
{
  "name": "钉钉告警群",
  "type": "dingtalk",
  "config": {
    "webhook": "https://oapi.dingtalk.com/robot/send?access_token=your_token",
    "secret": "SECyour_secret"
  }
}
```

**企业微信示例:**

```json
{
  "name": "企微告警群",
  "type": "wecom",
  "config": {
    "webhook": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=your_key"
  }
}
```

**飞书示例:**

```json
{
  "name": "飞书告警群",
  "type": "feishu",
  "config": {
    "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/your_hook_id",
    "secret": "your_secret"
  }
}
```

**邮件示例:**

```json
{
  "name": "邮件通知",
  "type": "email",
  "config": {
    "smtp_host": "smtp.example.com",
    "smtp_port": 465,
    "smtp_user": "alert@example.com",
    "smtp_pass": "password",
    "from": "alert@example.com",
    "to": ["ops@example.com", "admin@example.com"]
  }
}
```

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": { "id": 1 }
}
```

### 1.2 查询通知渠道列表

`GET /api/v1/cam/alert/channels`

| 参数   | 类型   | 必填 | 说明                                   |
| ------ | ------ | ---- | -------------------------------------- |
| type   | string | 否   | 渠道类型 (dingtalk/wecom/feishu/email) |
| offset | int    | 否   | 偏移量，默认 0                         |
| limit  | int    | 否   | 限制数量，默认 20                      |

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "name": "运维钉钉群",
        "type": "dingtalk",
        "config": { "webhook": "https://..." },
        "tenant_id": "tenant-001",
        "enabled": true,
        "create_time": "2026-02-25T10:00:00Z",
        "update_time": "2026-02-25T10:00:00Z"
      }
    ],
    "total": 1
  }
}
```

### 1.3 获取通知渠道详情

`GET /api/v1/cam/alert/channels/:id`

### 1.4 更新通知渠道

`PUT /api/v1/cam/alert/channels/:id`

请求体同创建。

### 1.5 删除通知渠道

`DELETE /api/v1/cam/alert/channels/:id`

### 1.6 测试通知渠道

`POST /api/v1/cam/alert/channels/:id/test`

发送一条测试消息到该渠道，验证配置是否正确。无请求体。

**响应:**

```json
{ "code": 0, "msg": "success" }
```

失败时:

```json
{ "code": 500, "msg": "send to dingtalk failed: unexpected status: 400" }
```

---

## 二、告警规则管理

> 告警规则定义了什么条件下触发告警，以及通过哪些渠道发送。

### 2.1 创建告警规则

`POST /api/v1/cam/alert/rules`

**请求体:**

```json
{
  "name": "ECS资源变更通知",
  "type": "resource_change",
  "channel_ids": [1, 2],
  "account_ids": [],
  "resource_types": ["ecs"],
  "regions": ["cn-hangzhou", "cn-beijing"],
  "silence_duration": 30,
  "escalate_after": 3,
  "escalate_channels": [3],
  "condition": {}
}
```

**字段说明:**

| 字段              | 类型     | 必填 | 说明                                                          |
| ----------------- | -------- | ---- | ------------------------------------------------------------- |
| name              | string   | 是   | 规则名称                                                      |
| type              | string   | 是   | 告警类型，见下方枚举                                          |
| channel_ids       | int64[]  | 是   | 通知渠道ID列表                                                |
| account_ids       | int64[]  | 否   | 限定云账号，空数组=全部账号                                   |
| resource_types    | string[] | 否   | 限定资源类型 (ecs/rds/redis/mongodb/vpc/eip/...)，空数组=全部 |
| regions           | string[] | 否   | 限定地域，空数组=全部地域                                     |
| silence_duration  | int      | 否   | 静默期(分钟)，同一规则在静默期内不重复告警                    |
| escalate_after    | int      | 否   | 连续触发N次后升级通知渠道                                     |
| escalate_channels | int64[]  | 否   | 升级后使用的渠道ID列表                                        |
| condition         | object   | 否   | 额外条件 (预留扩展)                                           |

**type 枚举值:**

| 值                | 说明         | 触发时机                               |
| ----------------- | ------------ | -------------------------------------- |
| `resource_change` | 资源变更     | 同步完成后检测到资源新增/删除/状态变更 |
| `sync_failure`    | 同步失败     | 同步任务执行失败                       |
| `expiration`      | 资源过期提醒 | 包年包月资源即将到期 (7天/3天/1天)     |
| `security_group`  | 安全组变更   | 安全组规则发生变更                     |

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": { "id": 1 }
}
```

### 2.2 查询告警规则列表

`GET /api/v1/cam/alert/rules`

| 参数   | 类型   | 必填 | 说明                                                              |
| ------ | ------ | ---- | ----------------------------------------------------------------- |
| type   | string | 否   | 告警类型 (resource_change/sync_failure/expiration/security_group) |
| offset | int    | 否   | 偏移量，默认 0                                                    |
| limit  | int    | 否   | 限制数量，默认 20                                                 |

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "name": "ECS资源变更通知",
        "type": "resource_change",
        "channel_ids": [1, 2],
        "account_ids": [],
        "resource_types": ["ecs"],
        "regions": ["cn-hangzhou"],
        "silence_duration": 30,
        "escalate_after": 3,
        "escalate_channels": [3],
        "condition": {},
        "tenant_id": "tenant-001",
        "enabled": true,
        "create_time": "2026-02-25T10:00:00Z",
        "update_time": "2026-02-25T10:00:00Z"
      }
    ],
    "total": 1
  }
}
```

### 2.3 获取告警规则详情

`GET /api/v1/cam/alert/rules/:id`

### 2.4 更新告警规则

`PUT /api/v1/cam/alert/rules/:id`

请求体同创建。

### 2.5 删除告警规则

`DELETE /api/v1/cam/alert/rules/:id`

### 2.6 启用/禁用告警规则

`PUT /api/v1/cam/alert/rules/:id/toggle`

**请求体:**

```json
{ "enabled": false }
```

---

## 三、告警事件查询

> 告警事件由系统自动生成，前端只需查询展示。

### 3.1 查询告警事件列表

`GET /api/v1/cam/alert/events`

| 参数     | 类型   | 必填 | 说明                                    |
| -------- | ------ | ---- | --------------------------------------- |
| type     | string | 否   | 告警类型                                |
| severity | string | 否   | 告警级别 (info/warning/critical)        |
| status   | string | 否   | 事件状态 (pending/sent/failed/silenced) |
| offset   | int    | 否   | 偏移量，默认 0                          |
| limit    | int    | 否   | 限制数量，默认 20                       |

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "rule_id": 1,
        "type": "resource_change",
        "severity": "warning",
        "title": "资源变更: ecs [aliyun/cn-hangzhou]",
        "content": {
          "resource_type": "ecs",
          "account_id": 1,
          "provider": "aliyun",
          "region": "cn-hangzhou",
          "added_count": 2,
          "removed_count": 1,
          "modified_count": 0,
          "changes": [
            {
              "change_type": "added",
              "resource_type": "ecs",
              "asset_id": "i-bp1234xxx",
              "asset_name": "web-server-03",
              "account_id": 1,
              "provider": "aliyun",
              "region": "cn-hangzhou"
            }
          ]
        },
        "source": "change_detector:ecs:cn-hangzhou",
        "tenant_id": "tenant-001",
        "status": "sent",
        "retry_count": 0,
        "create_time": "2026-02-25T10:30:00Z",
        "sent_at": "2026-02-25T10:30:05Z"
      }
    ],
    "total": 1
  }
}
```

**severity 级别说明:**

| 值         | 含义 | 场景                                  |
| ---------- | ---- | ------------------------------------- |
| `info`     | 信息 | 资源新增、状态正常变更、7天过期提醒   |
| `warning`  | 警告 | 资源删除、3天过期提醒                 |
| `critical` | 严重 | 同步失败、1天过期提醒、高危安全组变更 |

**status 状态说明:**

| 值         | 含义     | 说明                 |
| ---------- | -------- | -------------------- |
| `pending`  | 待发送   | 等待后台处理器发送   |
| `sent`     | 已发送   | 通知已成功发送到渠道 |
| `failed`   | 发送失败 | 重试3次后仍失败      |
| `silenced` | 已静默   | 在静默期内，跳过发送 |

---

## 四、告警事件 content 字段结构

不同告警类型的 `content` 字段结构不同:

### resource_change (资源变更)

```json
{
  "resource_type": "ecs",
  "account_id": 1,
  "provider": "aliyun",
  "region": "cn-hangzhou",
  "added_count": 2,
  "removed_count": 1,
  "modified_count": 1,
  "changes": [
    {
      "change_type": "added",
      "resource_type": "ecs",
      "asset_id": "i-xxx",
      "asset_name": "web-03",
      "account_id": 1,
      "provider": "aliyun",
      "region": "cn-hangzhou"
    },
    {
      "change_type": "modified",
      "resource_type": "ecs",
      "asset_id": "i-yyy",
      "asset_name": "web-01",
      "account_id": 1,
      "provider": "aliyun",
      "region": "cn-hangzhou",
      "details": {
        "field": "status",
        "old_value": "running",
        "new_value": "stopped"
      }
    }
  ]
}
```

### sync_failure (同步失败)

```json
{
  "task_id": "task-abc123",
  "account_id": 1,
  "account_name": "阿里云生产账号",
  "reason": "获取地域列表失败: InvalidAccessKeyId"
}
```

### expiration (资源过期)

```json
{
  "resource_type": "ecs",
  "asset_id": "i-xxx",
  "asset_name": "web-server-01",
  "expire_time": "2026-03-01T00:00:00Z",
  "days_left": 3,
  "account_id": 1
}
```

### security_group (安全组变更)

```json
{
  "security_group_id": "sg-xxx",
  "change_type": "rule_added",
  "rule_detail": "入方向: 0.0.0.0/0 -> TCP:22 (accept)"
}
```

---

## 五、前端页面建议

### 5.1 页面结构

```
告警中心
├── 告警事件    (事件列表，按时间倒序，支持按类型/级别/状态筛选)
├── 告警规则    (规则 CRUD，启用/禁用开关)
└── 通知渠道    (渠道 CRUD，测试按钮)
```

### 5.2 告警事件页面

- 列表展示: severity 用颜色标签区分 (info=蓝, warning=橙, critical=红)
- status 用 Tag 展示 (pending=灰, sent=绿, failed=红, silenced=黄)
- 点击展开查看 content 详情
- 支持按 type / severity / status 筛选
- 建议轮询间隔: 30秒

### 5.3 告警规则页面

- 列表展示规则名称、类型、关联渠道数、启用状态
- 启用/禁用用 Switch 组件
- 创建/编辑表单:
  - type 用 Select 选择
  - channel_ids 用多选下拉 (先调用渠道列表接口获取选项)
  - resource_types 用多选 Tag (ecs/rds/redis/mongodb/vpc/eip/nas/oss/kafka/elasticsearch)
  - regions 用多选下拉
  - account_ids 用多选下拉 (调用云账号列表接口)

### 5.4 通知渠道页面

- 列表展示渠道名称、类型、启用状态
- 类型用图标区分 (钉钉/企微/飞书/邮件)
- 创建/编辑时根据 type 动态渲染 config 表单
- 每行提供"测试"按钮，调用 `POST /channels/:id/test`

---

## 六、错误码

| code | 说明       |
| ---- | ---------- |
| 0    | 成功       |
| 400  | 参数错误   |
| 500  | 服务端错误 |
