# 多云权限管理功能

## 概述

多云权限管理功能提供了统一的权限查询和管理接口，支持查询用户权限、用户组权限、有效权限以及云平台可用的权限策略。

## 功能特性

### 1. 查询用户权限

**接口**: `GET /api/v1/cam/iam/permissions/users/{user_id}`

**功能**: 获取指定用户的所有权限信息，包括直接权限和所属用户组

**请求参数**:

- `user_id` (path, int, 必需) - 用户 ID

**响应示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "user_id": 1,
    "username": "admin",
    "display_name": "管理员",
    "provider": "aliyun",
    "direct_policies": [],
    "user_groups": [
      {
        "group_id": 1,
        "group_name": "administrators",
        "display_name": "管理员组",
        "policies": [
          {
            "policy_id": "AdministratorAccess",
            "policy_name": "管理员权限",
            "policy_document": "...",
            "provider": "aliyun",
            "policy_type": "system"
          }
        ]
      }
    ]
  }
}
```

### 2. 查询用户组权限

**接口**: `GET /api/v1/cam/iam/permissions/groups/{group_id}`

**功能**: 获取指定用户组的所有权限策略

**请求参数**:

- `group_id` (path, int, 必需) - 用户组 ID

**响应示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "group_id": 1,
    "group_name": "administrators",
    "display_name": "管理员组",
    "provider": "aliyun",
    "policies": [
      {
        "policy_id": "AdministratorAccess",
        "policy_name": "管理员权限",
        "policy_document": "...",
        "provider": "aliyun",
        "policy_type": "system"
      }
    ],
    "member_count": 5
  }
}
```

### 3. 查询用户有效权限

**接口**: `GET /api/v1/cam/iam/permissions/users/{user_id}/effective`

**功能**: 获取用户的有效权限，包括直接权限和从用户组继承的权限（自动去重）

**请求参数**:

- `user_id` (path, int, 必需) - 用户 ID

**响应示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "user_id": 1,
    "username": "admin",
    "display_name": "管理员",
    "provider": "aliyun",
    "all_policies": [
      {
        "policy_id": "AdministratorAccess",
        "policy_name": "管理员权限",
        "provider": "aliyun",
        "policy_type": "system"
      },
      {
        "policy_id": "ReadOnlyAccess",
        "policy_name": "只读权限",
        "provider": "aliyun",
        "policy_type": "system"
      }
    ],
    "direct_policies": [],
    "inherited_policies": [
      {
        "policy_id": "AdministratorAccess",
        "policy_name": "管理员权限",
        "provider": "aliyun",
        "policy_type": "system"
      }
    ],
    "user_groups": [
      {
        "group_id": 1,
        "group_name": "administrators",
        "display_name": "管理员组",
        "policies": [...]
      }
    ]
  }
}
```

**字段说明**:

- `all_policies`: 所有权限（去重后）
- `direct_policies`: 直接分配给用户的权限
- `inherited_policies`: 从用户组继承的权限
- `user_groups`: 用户所属的用户组列表

### 4. 查询云平台权限策略

**接口**: `GET /api/v1/cam/iam/permissions/policies`

**功能**: 查询指定云账号对应云平台的所有可用权限策略

**请求参数**:

- `cloud_account_id` (query, int, 必需) - 云账号 ID

**响应示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": [
    {
      "policy_id": "AdministratorAccess",
      "policy_name": "管理员权限",
      "policy_document": "...",
      "provider": "aliyun",
      "policy_type": "system"
    },
    {
      "policy_id": "ReadOnlyAccess",
      "policy_name": "只读权限",
      "policy_document": "...",
      "provider": "aliyun",
      "policy_type": "system"
    }
  ]
}
```

## 使用场景

### 场景 1: 权限审计

查询用户的有效权限，了解用户实际拥有的所有权限：

```bash
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/1/effective"
```

### 场景 2: 用户组权限管理

查看用户组包含哪些权限策略：

```bash
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/groups/1"
```

### 场景 3: 权限分配

查询云平台可用的权限策略，用于分配给用户或用户组：

```bash
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/policies?cloud_account_id=1"
```

### 场景 4: 权限溯源

查询用户权限来源，了解权限是直接分配还是从用户组继承：

```bash
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/1"
```

## 技术实现

### 服务层

**文件**: `internal/cam/iam/service/permission.go`

**核心方法**:

- `GetUserPermissions`: 获取用户权限和所属用户组
- `GetUserGroupPermissions`: 获取用户组的权限策略
- `GetUserEffectivePermissions`: 计算用户的有效权限（合并去重）
- `ListPoliciesByProvider`: 从云平台获取可用的权限策略

**数据结构**:

- `UserPermissions`: 用户权限信息
- `GroupPermissions`: 用户组权限信息
- `EffectivePermissions`: 有效权限（包含继承关系）
- `UserGroupInfo`: 用户组简要信息

### Web 层

**文件**: `internal/cam/iam/web/permission_handler.go`

**HTTP 处理器**:

- `GetUserPermissions`: 处理用户权限查询
- `GetUserGroupPermissions`: 处理用户组权限查询
- `GetUserEffectivePermissions`: 处理有效权限查询
- `ListPoliciesByProvider`: 处理云平台策略查询

### 路由注册

所有权限相关的路由都在 `/api/v1/cam/iam/permissions` 路径下：

```
GET  /api/v1/cam/iam/permissions/users/:user_id           - 查询用户权限
GET  /api/v1/cam/iam/permissions/users/:user_id/effective - 查询用户有效权限
GET  /api/v1/cam/iam/permissions/groups/:group_id         - 查询用户组权限
GET  /api/v1/cam/iam/permissions/policies                 - 查询云平台权限策略
```

## 权限计算逻辑

### 有效权限计算

1. **收集直接权限**: 用户直接分配的权限（当前版本为空，权限通过用户组管理）
2. **收集继承权限**: 遍历用户所属的所有用户组，收集每个用户组的权限策略
3. **去重处理**: 使用 `provider:policy_id` 作为唯一键进行去重
4. **合并结果**: 将直接权限和继承权限合并为最终的有效权限列表

### 权限优先级

当前实现中，权限采用合并策略：

- 用户拥有所有用户组的权限并集
- 不同用户组的相同权限会自动去重
- 未来可扩展支持权限冲突解决策略

## 多云支持

### 支持的云平台

- ✅ **阿里云 (Aliyun)**: 支持 RAM 权限策略
- ✅ **AWS**: 支持 IAM 权限策略
- ✅ **腾讯云 (Tencent)**: 支持 CAM 权限策略
- ✅ **华为云 (Huawei)**: 支持 IAM 权限策略
- ✅ **火山云 (Volcano)**: 支持 IAM 权限策略

### 云平台适配器

每个云平台适配器实现了 `ListPolicies` 方法，用于获取该平台的权限策略列表：

- `internal/shared/cloudx/iam/aliyun/policy.go`
- `internal/shared/cloudx/iam/aws/policy.go`
- `internal/shared/cloudx/iam/tencent/policy.go`
- `internal/shared/cloudx/iam/huawei/policy.go`
- `internal/shared/cloudx/iam/volcano/policy.go`

## 安全考虑

1. **权限验证**: 确保调用者有权限查询目标用户或用户组的权限
2. **敏感信息**: 权限策略文档可能包含敏感信息，需要控制访问权限
3. **审计日志**: 建议记录所有权限查询操作
4. **数据缓存**: 可以考虑缓存云平台策略列表以提高性能

## 后续优化建议

1. **权限比较**: 支持比较两个用户或用户组的权限差异
2. **权限推荐**: 基于角色推荐合适的权限策略
3. **权限分析**: 分析权限使用情况，识别过度授权
4. **权限模板**: 支持创建权限模板快速分配
5. **权限变更历史**: 记录权限变更历史
6. **权限影响分析**: 分析权限变更的影响范围

## API 列表

| 方法 | 路径                                                    | 功能               | 标签     |
| ---- | ------------------------------------------------------- | ------------------ | -------- |
| GET  | `/api/v1/cam/iam/permissions/users/{user_id}`           | 查询用户权限       | 权限管理 |
| GET  | `/api/v1/cam/iam/permissions/users/{user_id}/effective` | 查询用户有效权限   | 权限管理 |
| GET  | `/api/v1/cam/iam/permissions/groups/{group_id}`         | 查询用户组权限     | 权限管理 |
| GET  | `/api/v1/cam/iam/permissions/policies`                  | 查询云平台权限策略 | 权限管理 |

## 相关文档

- [IAM 用户组管理 API](./IAM_GROUP_API_STATUS.md)
- [用户组同步功能](./USER_GROUP_SYNC_FEATURE.md)
- [Swagger 文档](./swagger.json)
