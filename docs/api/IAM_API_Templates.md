# 策略模板 API

## 1. 创建策略模板

创建新的策略模板。

**接口**: `POST /api/v1/cam/iam/templates`

### 请求参数

```json
{
  "name": "开发者标准权限模板",
  "description": "适用于开发人员的标准权限配置",
  "category": "readwrite",
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

| 字段            | 类型     | 必填 | 说明                                   |
| --------------- | -------- | ---- | -------------------------------------- |
| name            | string   | 是   | 模板名称，1-100 字符                   |
| description     | string   | 否   | 描述，最多 500 字符                    |
| category        | string   | 是   | 分类 (readonly/readwrite/admin/custom) |
| policies        | []Policy | 否   | 权限策略列表                           |
| cloud_platforms | []string | 是   | 支持的云平台，至少 1 个                |
| tenant_id       | string   | 是   | 租户 ID                                |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "开发者标准权限模板",
    "description": "适用于开发人员的标准权限配置",
    "category": "readwrite",
    "policies": [ ... ],
    "cloud_platforms": ["aliyun", "aws"],
    "is_built_in": false,
    "usage_count": 0,
    "tenant_id": "tenant-001",
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z"
  }
}
```

## 2. 获取策略模板详情

获取指定策略模板的详细信息。

**接口**: `GET /api/v1/cam/iam/templates/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 模板 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "开发者标准权限模板",
    "description": "适用于开发人员的标准权限配置",
    "category": "readwrite",
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
    "is_built_in": false,
    "usage_count": 15,
    "tenant_id": "tenant-001",
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z"
  }
}
```

## 3. 查询策略模板列表

分页查询策略模板列表。

**接口**: `GET /api/v1/cam/iam/templates`

### 查询参数

| 参数        | 类型   | 必填 | 说明              |
| ----------- | ------ | ---- | ----------------- |
| category    | string | 否   | 分类              |
| is_built_in | bool   | 否   | 是否内置模板      |
| tenant_id   | string | 否   | 租户 ID           |
| keyword     | string | 否   | 关键词搜索        |
| page        | int    | 否   | 页码，默认 1      |
| size        | int    | 否   | 每页数量，默认 20 |

### 请求示例

```
GET /api/v1/cam/iam/templates?category=readwrite&is_built_in=false&page=1&size=20
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
        "name": "开发者标准权限模板",
        "description": "适用于开发人员的标准权限配置",
        "category": "readwrite",
        "cloud_platforms": ["aliyun", "aws"],
        "is_built_in": false,
        "usage_count": 15,
        "create_time": "2024-01-01T00:00:00Z"
      }
    ],
    "total": 20,
    "page": 1,
    "size": 20
  }
}
```

## 4. 更新策略模板

更新策略模板信息。

**接口**: `PUT /api/v1/cam/iam/templates/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 模板 ID |

### 请求参数

```json
{
  "name": "新的模板名称",
  "description": "新的描述",
  "policies": [ ... ],
  "cloud_platforms": ["aliyun", "aws", "huawei"]
}
```

| 字段            | 类型     | 必填 | 说明         |
| --------------- | -------- | ---- | ------------ |
| name            | string   | 否   | 模板名称     |
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
    "name": "新的模板名称",
    "description": "新的描述",
    "update_time": "2024-01-02T00:00:00Z"
  }
}
```

## 5. 删除策略模板

删除指定策略模板。

**接口**: `DELETE /api/v1/cam/iam/templates/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 模板 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

## 6. 从权限组创建模板

从现有权限组创建策略模板。

**接口**: `POST /api/v1/cam/iam/templates/from-group`

### 请求参数

```json
{
  "group_id": 1,
  "template_name": "基于开发组的模板",
  "template_description": "从开发权限组创建的模板"
}
```

| 字段                 | 类型   | 必填 | 说明      |
| -------------------- | ------ | ---- | --------- |
| group_id             | int64  | 是   | 权限组 ID |
| template_name        | string | 是   | 模板名称  |
| template_description | string | 否   | 模板描述  |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 2,
    "name": "基于开发组的模板",
    "description": "从开发权限组创建的模板",
    "category": "custom",
    "policies": [ ... ],
    "cloud_platforms": ["aliyun", "aws"],
    "is_built_in": false,
    "create_time": "2024-01-01T00:00:00Z"
  }
}
```

## 7. 应用模板到权限组

将策略模板应用到指定权限组。

**接口**: `POST /api/v1/cam/iam/templates/{id}/apply`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 模板 ID |

### 请求参数

```json
{
  "group_id": 1
}
```

| 字段     | 类型  | 必填 | 说明      |
| -------- | ----- | ---- | --------- |
| group_id | int64 | 是   | 权限组 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "group_id": 1,
    "applied_policies_count": 5
  }
}
```

## 8. 获取内置模板列表

获取系统内置的策略模板列表。

**接口**: `GET /api/v1/cam/iam/templates/built-in`

### 查询参数

| 参数           | 类型   | 必填 | 说明   |
| -------------- | ------ | ---- | ------ |
| category       | string | 否   | 分类   |
| cloud_platform | string | 否   | 云平台 |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 100,
      "name": "只读权限模板",
      "description": "系统内置的只读权限模板",
      "category": "readonly",
      "cloud_platforms": ["aliyun", "aws"],
      "is_built_in": true,
      "usage_count": 50
    },
    {
      "id": 101,
      "name": "管理员权限模板",
      "description": "系统内置的管理员权限模板",
      "category": "admin",
      "cloud_platforms": ["aliyun", "aws"],
      "is_built_in": true,
      "usage_count": 10
    }
  ]
}
```
