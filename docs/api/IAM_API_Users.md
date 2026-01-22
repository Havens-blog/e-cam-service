# 用户管理 API

## 1. 创建用户

创建云平台用户。

**接口**: `POST /api/v1/cam/iam/users`

### 请求参数

```json
{
  "username": "test-user",
  "user_type": "ram_user",
  "cloud_account_id": 1,
  "display_name": "测试用户",
  "email": "test@example.com",
  "permission_groups": [1, 2],
  "tenant_id": "tenant-001"
}
```

| 字段              | 类型    | 必填 | 说明                           |
| ----------------- | ------- | ---- | ------------------------------ |
| username          | string  | 是   | 用户名，1-100 字符             |
| user_type         | string  | 是   | 用户类型，见枚举 CloudUserType |
| cloud_account_id  | int64   | 是   | 云账号 ID                      |
| display_name      | string  | 否   | 显示名称，最多 200 字符        |
| email             | string  | 否   | 邮箱地址                       |
| permission_groups | []int64 | 否   | 权限组 ID 列表                 |
| tenant_id         | string  | 是   | 租户 ID                        |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1001,
    "username": "test-user",
    "user_type": "ram_user",
    "cloud_account_id": 1,
    "provider": "aliyun",
    "cloud_user_id": "ram-user-123",
    "display_name": "测试用户",
    "email": "test@example.com",
    "permission_groups": [1, 2],
    "status": "active",
    "tenant_id": "tenant-001",
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z"
  }
}
```

## 2. 获取用户详情

获取指定用户的详细信息。

**接口**: `GET /api/v1/cam/iam/users/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 用户 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1001,
    "username": "test-user",
    "user_type": "ram_user",
    "cloud_account_id": 1,
    "provider": "aliyun",
    "cloud_user_id": "ram-user-123",
    "display_name": "测试用户",
    "email": "test@example.com",
    "permission_groups": [1, 2],
    "metadata": {
      "last_login_time": "2024-01-01T10:00:00Z",
      "last_sync_time": "2024-01-01T12:00:00Z",
      "access_key_count": 2,
      "mfa_enabled": true,
      "tags": {
        "department": "IT",
        "role": "developer"
      }
    },
    "status": "active",
    "tenant_id": "tenant-001",
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z"
  }
}
```

## 3. 查询用户列表

分页查询用户列表，支持多条件筛选。

**接口**: `GET /api/v1/cam/iam/users`

### 查询参数

| 参数             | 类型   | 必填 | 说明                      |
| ---------------- | ------ | ---- | ------------------------- |
| provider         | string | 否   | 云厂商，如 aliyun, aws    |
| user_type        | string | 否   | 用户类型                  |
| status           | string | 否   | 用户状态                  |
| cloud_account_id | int64  | 否   | 云账号 ID                 |
| tenant_id        | string | 否   | 租户 ID                   |
| keyword          | string | 否   | 关键词搜索（用户名/邮箱） |
| page             | int    | 否   | 页码，默认 1              |
| size             | int    | 否   | 每页数量，默认 20         |

### 请求示例

```
GET /api/v1/cam/iam/users?provider=aliyun&status=active&page=1&size=20
```

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1001,
        "username": "user1",
        "user_type": "ram_user",
        "provider": "aliyun",
        "display_name": "用户1",
        "email": "user1@example.com",
        "status": "active",
        "create_time": "2024-01-01T00:00:00Z"
      }
    ],
    "total": 100,
    "page": 1,
    "size": 20
  }
}
```

## 4. 更新用户

更新用户信息。

**接口**: `PUT /api/v1/cam/iam/users/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 用户 ID |

### 请求参数

```json
{
  "display_name": "新的显示名称",
  "email": "newemail@example.com",
  "permission_groups": [1, 2, 3],
  "status": "inactive"
}
```

| 字段              | 类型    | 必填 | 说明           |
| ----------------- | ------- | ---- | -------------- |
| display_name      | string  | 否   | 显示名称       |
| email             | string  | 否   | 邮箱地址       |
| permission_groups | []int64 | 否   | 权限组 ID 列表 |
| status            | string  | 否   | 用户状态       |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1001,
    "username": "test-user",
    "display_name": "新的显示名称",
    "email": "newemail@example.com",
    "permission_groups": [1, 2, 3],
    "status": "inactive",
    "update_time": "2024-01-02T00:00:00Z"
  }
}
```

## 5. 删除用户

删除指定用户。

**接口**: `DELETE /api/v1/cam/iam/users/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 用户 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

## 6. 批量分配权限组

为多个用户批量分配权限组。

**接口**: `POST /api/v1/cam/iam/users/batch-assign`

### 请求参数

```json
{
  "user_ids": [1001, 1002, 1003],
  "group_ids": [1, 2]
}
```

| 字段      | 类型    | 必填 | 说明                      |
| --------- | ------- | ---- | ------------------------- |
| user_ids  | []int64 | 是   | 用户 ID 列表，至少 1 个   |
| group_ids | []int64 | 是   | 权限组 ID 列表，至少 1 个 |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "success_count": 3,
    "failed_count": 0
  }
}
```

## 7. 同步用户到云平台

将本地用户同步到云平台。

**接口**: `POST /api/v1/cam/iam/users/{id}/sync`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 用户 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "task_id": "sync-task-001",
    "status": "running"
  }
}
```
