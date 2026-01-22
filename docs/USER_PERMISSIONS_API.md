# 用户权限查询 API

## 功能说明

系统提供了完整的用户权限查询功能，可以获取用户的个人权限、用户组权限和有效权限。

## API 接口

### 1. 获取用户权限

获取用户的基本权限信息，包括所属用户组及其权限。

**请求**:

```http
GET /api/v1/cam/iam/permissions/users/{user_id}
X-Tenant-ID: tenant-001
```

**路径参数**:

- `user_id`: 用户 ID

**响应**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "user_id": 1,
    "username": "zhang.san",
    "display_name": "张三",
    "provider": "aliyun",
    "direct_policies": [],
    "user_groups": [
      {
        "group_id": 1,
        "group_name": "developers",
        "display_name": "开发组",
        "policies": [
          {
            "policy_id": "AliyunECSFullAccess",
            "policy_name": "AliyunECSFullAccess",
            "policy_document": "ECS完全访问权限",
            "provider": "aliyun",
            "policy_type": "system"
          }
        ]
      }
    ]
  }
}
```

### 2. 获取用户有效权限

获取用户的所有有效权限，包括直接权限和从用户组继承的权限（合并去重）。

**请求**:

```http
GET /api/v1/cam/iam/permissions/users/{user_id}/effective
X-Tenant-ID: tenant-001
```

**路径参数**:

- `user_id`: 用户 ID

**响应**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "user_id": 1,
    "username": "zhang.san",
    "display_name": "张三",
    "provider": "aliyun",
    "effective_policies": [
      {
        "policy_id": "AliyunECSFullAccess",
        "policy_name": "AliyunECSFullAccess",
        "policy_document": "ECS完全访问权限",
        "provider": "aliyun",
        "policy_type": "system",
        "source": "group",
        "source_id": 1,
        "source_name": "开发组"
      },
      {
        "policy_id": "AliyunRDSReadOnlyAccess",
        "policy_name": "AliyunRDSReadOnlyAccess",
        "policy_document": "RDS只读权限",
        "provider": "aliyun",
        "policy_type": "system",
        "source": "group",
        "source_id": 2,
        "source_name": "测试组"
      }
    ],
    "user_groups": [
      {
        "group_id": 1,
        "group_name": "developers",
        "display_name": "开发组"
      },
      {
        "group_id": 2,
        "group_name": "testers",
        "display_name": "测试组"
      }
    ]
  }
}
```

### 3. 获取用户组权限

获取指定用户组的权限策略。

**请求**:

```http
GET /api/v1/cam/iam/permissions/groups/{group_id}
X-Tenant-ID: tenant-001
```

**路径参数**:

- `group_id`: 用户组 ID

**响应**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "group_id": 1,
    "group_name": "developers",
    "display_name": "开发组",
    "policies": [
      {
        "policy_id": "AliyunECSFullAccess",
        "policy_name": "AliyunECSFullAccess",
        "policy_document": "ECS完全访问权限",
        "provider": "aliyun",
        "policy_type": "system"
      }
    ]
  }
}
```

### 4. 查询云平台权限策略

查询指定云账号可用的权限策略列表。

**请求**:

```http
GET /api/v1/cam/iam/permissions/policies?cloud_account_id=1
X-Tenant-ID: tenant-001
```

**查询参数**:

- `cloud_account_id`: 云账号 ID

**响应**:

```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "policy_id": "AliyunECSFullAccess",
      "policy_name": "AliyunECSFullAccess",
      "policy_document": "ECS完全访问权限",
      "provider": "aliyun",
      "policy_type": "system"
    },
    {
      "policy_id": "AliyunRDSFullAccess",
      "policy_name": "AliyunRDSFullAccess",
      "policy_document": "RDS完全访问权限",
      "provider": "aliyun",
      "policy_type": "system"
    }
  ]
}
```

## 使用示例

### 示例 1: 查询用户权限

```bash
# 获取用户 ID 为 1 的权限
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/1" \
  -H "X-Tenant-ID: tenant-001"
```

### 示例 2: 查询用户有效权限

```bash
# 获取用户 ID 为 1 的所有有效权限
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/1/effective" \
  -H "X-Tenant-ID: tenant-001"
```

### 示例 3: 前端使用

```javascript
// 获取用户权限
async function getUserPermissions(userId) {
  const response = await fetch(`/api/v1/cam/iam/permissions/users/${userId}`, {
    headers: {
      "X-Tenant-ID": "tenant-001",
    },
  });
  const data = await response.json();
  return data.data;
}

// 获取用户有效权限
async function getUserEffectivePermissions(userId) {
  const response = await fetch(
    `/api/v1/cam/iam/permissions/users/${userId}/effective`,
    {
      headers: {
        "X-Tenant-ID": "tenant-001",
      },
    }
  );
  const data = await response.json();
  return data.data;
}

// 使用示例
const permissions = await getUserPermissions(1);
console.log("用户权限:", permissions);

const effectivePermissions = await getUserEffectivePermissions(1);
console.log("有效权限:", effectivePermissions.effective_policies);
```

## 权限数据结构

### UserPermissions (用户权限)

```typescript
interface UserPermissions {
  user_id: number;
  username: string;
  display_name: string;
  provider: string;
  direct_policies: PermissionPolicy[]; // 直接分配的权限
  user_groups: UserGroupInfo[]; // 所属用户组及其权限
}
```

### EffectivePermissions (有效权限)

```typescript
interface EffectivePermissions {
  user_id: number;
  username: string;
  display_name: string;
  provider: string;
  effective_policies: EffectivePolicy[]; // 所有有效权限（合并去重）
  user_groups: UserGroupInfo[]; // 所属用户组
}
```

### EffectivePolicy (有效权限策略)

```typescript
interface EffectivePolicy {
  policy_id: string;
  policy_name: string;
  policy_document: string;
  provider: string;
  policy_type: string;
  source: string; // "direct" | "group"
  source_id: number; // 来源ID（用户组ID）
  source_name: string; // 来源名称（用户组名称）
}
```

## 权限来源说明

用户的权限可以来自两个来源：

1. **直接权限** (direct_policies)

   - 直接分配给用户的权限
   - 目前系统主要通过用户组管理权限，直接权限较少使用

2. **用户组权限** (user_groups)
   - 用户通过加入用户组获得的权限
   - 用户可以加入多个用户组
   - 每个用户组可以有多个权限策略

## 前端展示建议

### 用户详情页 - 权限标签

```
┌─────────────────────────────────────────┐
│ 用户详情 - 张三                          │
├─────────────────────────────────────────┤
│ [基本信息] [权限] [操作日志]             │
├─────────────────────────────────────────┤
│ 所属用户组:                              │
│ ┌─────────────────────────────────────┐ │
│ │ 开发组                               │ │
│ │   - AliyunECSFullAccess (系统策略)  │ │
│ │   - AliyunRDSReadOnlyAccess (系统)  │ │
│ │                                      │ │
│ │ 测试组                               │ │
│ │   - AliyunOSSReadOnlyAccess (系统)  │ │
│ └─────────────────────────────────────┘ │
│                                          │
│ 有效权限汇总:                            │
│ ┌─────────────────────────────────────┐ │
│ │ ✓ AliyunECSFullAccess (来自: 开发组) │ │
│ │ ✓ AliyunRDSReadOnlyAccess (开发组)  │ │
│ │ ✓ AliyunOSSReadOnlyAccess (测试组)  │ │
│ └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

### 权限矩阵视图

```
用户 \ 权限    | ECS完全访问 | RDS只读 | OSS只读
-------------|------------|---------|--------
张三 (开发组)  | ✓          | ✓       | ✓
李四 (测试组)  | ✗          | ✓       | ✓
王五 (运维组)  | ✓          | ✓       | ✓
```

## 常见问题

### Q1: 用户权限为空

**可能原因**:

1. 用户没有加入任何用户组
2. 用户组没有配置权限策略
3. 数据同步未完成

**解决方案**:

```bash
# 1. 检查用户是否有用户组
curl -X GET "http://localhost:8080/api/v1/cam/iam/users/1" \
  -H "X-Tenant-ID: tenant-001"

# 2. 检查用户组是否有权限
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups/1" \
  -H "X-Tenant-ID: tenant-001"

# 3. 重新同步用户组
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001"
```

### Q2: 权限策略显示不完整

**原因**: 用户组的权限策略可能没有同步

**解决方案**:

1. 重新同步用户组
2. 手动更新用户组的权限策略

### Q3: 如何判断用户是否有某个权限

```javascript
function hasPermission(effectivePermissions, policyId) {
  return effectivePermissions.effective_policies.some(
    (policy) => policy.policy_id === policyId
  );
}

// 使用
const permissions = await getUserEffectivePermissions(1);
if (hasPermission(permissions, "AliyunECSFullAccess")) {
  console.log("用户有 ECS 完全访问权限");
}
```

## 相关 API

- `GET /api/v1/cam/iam/users/{id}` - 获取用户详情
- `GET /api/v1/cam/iam/groups/{id}` - 获取用户组详情
- `GET /api/v1/cam/iam/groups/{id}/members` - 获取用户组成员
- `GET /api/v1/cam/iam/permissions/users/{user_id}` - 获取用户权限
- `GET /api/v1/cam/iam/permissions/users/{user_id}/effective` - 获取用户有效权限

## 测试

```bash
#!/bin/bash

TENANT_ID="tenant-001"
USER_ID=1

echo "=== 测试用户权限查询 ==="

# 1. 获取用户权限
echo "1. 获取用户权限"
curl -s -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/$USER_ID" \
  -H "X-Tenant-ID: $TENANT_ID" | jq '.'

# 2. 获取用户有效权限
echo "2. 获取用户有效权限"
curl -s -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/$USER_ID/effective" \
  -H "X-Tenant-ID: $TENANT_ID" | jq '.'

# 3. 统计权限数量
echo "3. 统计权限数量"
POLICY_COUNT=$(curl -s -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/$USER_ID/effective" \
  -H "X-Tenant-ID: $TENANT_ID" | jq '.data.effective_policies | length')
echo "用户有效权限数量: $POLICY_COUNT"
```

## 总结

用户权限查询功能已完整实现，包括：

1. ✅ 获取用户基本权限
2. ✅ 获取用户有效权限（合并去重）
3. ✅ 获取用户组权限
4. ✅ 查询云平台权限策略

前端可以通过这些 API 完整展示用户的权限信息！

---

**更新日期**: 2025-11-25  
**版本**: v1.0
