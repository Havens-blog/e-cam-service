# 权限组管理 API

## 1. 创建权限组

创建新的权限组。

**接口**: `POST /api/v1/cam/iam/groups`

### 请求参数

```json
{
  "name": "开发者权限组",
  "description": "开发人员的标准权限",
  "policies": [
    {
      "policy_id": "AliyunECSReadOnlyAccess",
      "policy_name": "AliyunECSReadOnlyAccess",
      "policy_document": "ECS只读权限",
      "provider": "aliyun",
      "policy_type": "system"
    }
  ],
  "cloud_platforms": ["aliyun", "aws"],
  "tenant_id": "tenant-001"
}
```

| 字段            | 类型     | 必填 | 说明                    |
| --------------- | -------- | ---- | ----------------------- |
| name            | string   | 是   | 权限组名称，1-100 字符  |
| description     | string   | 否   | 描述，最多 500 字符     |
| policies        | []Policy | 否   | 权限策略列表            |
| cloud_platforms | []string | 是   | 支持的云平台，至少 1 个 |
| tenant_id       | string   | 是   | 租户 ID                 |

### Policy 对象

| 字段            | 类型   | 说明                     |
| --------------- | ------ | ------------------------ |
| policy_id       | string | 策略 ID                  |
| policy_name     | string | 策略名称                 |
| policy_document | string | 策略文档/描述            |
| provider        | string | 云厂商                   |
| policy_type     | string | 策略类型 (system/custom) |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "开发者权限组",
    "description": "开发人员的标准权限",
    "policies": [ ... ],
    "cloud_platforms": ["aliyun", "aws"],
    "user_count": 0,
    "tenant_id": "tenant-001",
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z"
  }
}
```

## 2. 获取权限组详情

获取指定权限组的详细信息。

**接口**: `GET /api/v1/cam/iam/groups/{id}`

### 路径参数

| 参数 | 类型  | 说明      |
| ---- | ----- | --------- |
| id   | int64 | 权限组 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "开发者权限组",
    "description": "开发人员的标准权限",
    "policies": [
      {
        "policy_id": "AliyunECSReadOnlyAccess",
        "policy_name": "AliyunECSReadOnlyAccess",
        "policy_document": "ECS只读权限",
        "provider": "aliyun",
        "policy_type": "system"
      }
    ],
    "cloud_platforms": ["aliyun", "aws"],
    "user_count": 15,
    "tenant_id": "tenant-001",
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z"
  }
}
```

## 3. 查询权限组列表

分页查询权限组列表。

**接口**: `GET /api/v1/cam/iam/groups`

### 查询参数

| 参数      | 类型   | 必填 | 说明                    |
| --------- | ------ | ---- | ----------------------- |
| tenant_id | string | 否   | 租户 ID                 |
| keyword   | string | 否   | 关键词搜索（名称/描述） |
| page      | int    | 否   | 页码，默认 1            |
| size      | int    | 否   | 每页数量，默认 20       |

### 请求示例

```
GET /api/v1/cam/iam/groups?tenant_id=tenant-001&page=1&size=20
```

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1,
        "name": "开发者权限组",
        "description": "开发人员的标准权限",
        "cloud_platforms": ["aliyun", "aws"],
        "user_count": 15,
        "create_time": "2024-01-01T00:00:00Z"
      }
    ],
    "total": 10,
    "page": 1,
    "size": 20
  }
}
```

## 4. 更新权限组

更新权限组信息。

**接口**: `PUT /api/v1/cam/iam/groups/{id}`

### 路径参数

| 参数 | 类型  | 说明      |
| ---- | ----- | --------- |
| id   | int64 | 权限组 ID |

### 请求参数

```json
{
  "name": "新的权限组名称",
  "description": "新的描述",
  "policies": [ ... ],
  "cloud_platforms": ["aliyun", "aws", "huawei"]
}
```

| 字段            | 类型     | 必填 | 说明         |
| --------------- | -------- | ---- | ------------ |
| name            | string   | 否   | 权限组名称   |
| description     | string   | 否   | 描述         |
| policies        | []Policy | 否   | 权限策略列表 |
| cloud_platforms | []string | 否   | 支持的云平台 |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "新的权限组名称",
    "description": "新的描述",
    "update_time": "2024-01-02T00:00:00Z"
  }
}
```

## 5. 删除权限组

删除指定权限组。

**接口**: `DELETE /api/v1/cam/iam/groups/{id}`

### 路径参数

| 参数 | 类型  | 说明      |
| ---- | ----- | --------- |
| id   | int64 | 权限组 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

## 6. 获取权限组的用户列表

获取指定权限组下的所有用户。

**接口**: `GET /api/v1/cam/iam/groups/{id}/users`

### 路径参数

| 参数 | 类型  | 说明      |
| ---- | ----- | --------- |
| id   | int64 | 权限组 ID |

### 查询参数

| 参数 | 类型 | 必填 | 说明              |
| ---- | ---- | ---- | ----------------- |
| page | int  | 否   | 页码，默认 1      |
| size | int  | 否   | 每页数量，默认 20 |

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
        "display_name": "用户1",
        "email": "user1@example.com",
        "status": "active"
      }
    ],
    "total": 15,
    "page": 1,
    "size": 20
  }
}
```

## 7. 获取可用策略列表

获取指定云平台的可用权限策略列表。

**接口**: `GET /api/v1/cam/iam/policies`

### 查询参数

| 参数             | 类型   | 必填 | 说明                     |
| ---------------- | ------ | ---- | ------------------------ |
| provider         | string | 是   | 云厂商 (aliyun/aws)      |
| cloud_account_id | int64  | 是   | 云账号 ID                |
| policy_type      | string | 否   | 策略类型 (system/custom) |
| keyword          | string | 否   | 关键词搜索               |

### 请求示例

```
GET /api/v1/cam/iam/policies?provider=aliyun&cloud_account_id=1
```

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "policy_id": "AliyunECSReadOnlyAccess",
      "policy_name": "AliyunECSReadOnlyAccess",
      "policy_document": "ECS只读权限",
      "provider": "aliyun",
      "policy_type": "system"
    },
    {
      "policy_id": "AliyunECSFullAccess",
      "policy_name": "AliyunECSFullAccess",
      "policy_document": "ECS完全权限",
      "provider": "aliyun",
      "policy_type": "system"
    }
  ]
}
```
