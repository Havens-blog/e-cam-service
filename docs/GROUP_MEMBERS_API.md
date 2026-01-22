# 用户组成员查询 API

## 功能说明

前端查询用户组时，可以通过专门的 API 获取该用户组的所有成员列表。

## 设计方案

### 方案选择

我们选择了**关联查询**的方案，而不是修改集合结构：

| 方案                                   | 优点                   | 缺点                             | 选择 |
| -------------------------------------- | ---------------------- | -------------------------------- | ---- |
| 修改集合结构（在用户组中存储用户列表） | 查询快                 | 数据冗余、同步复杂、一致性难保证 | ❌   |
| 关联查询（查询用户集合）               | 数据一致、灵活、易维护 | 需要额外查询                     | ✅   |

### 数据结构

**用户组集合** (cloud_iam_groups):

```javascript
{
  "id": 1,
  "name": "开发组",
  "user_count": 5,  // 成员数量
  // 不存储用户列表
}
```

**用户集合** (cloud_iam_users):

```javascript
{
  "id": 1,
  "username": "test-user",
  "user_groups": [1, 2, 3],  // 所属用户组ID列表
}
```

## API 接口

### 获取用户组成员列表

**请求**:

```http
GET /api/v1/cam/iam/groups/{id}/members
X-Tenant-ID: tenant-001
```

**路径参数**:

- `id`: 用户组 ID

**响应**:

```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 1,
      "username": "zhang.san",
      "display_name": "张三",
      "email": "zhang.san@example.com",
      "provider": "aliyun",
      "cloud_user_id": "u-123456",
      "user_groups": [1, 2],
      "status": "active",
      "tenant_id": "tenant-001"
    },
    {
      "id": 2,
      "username": "li.si",
      "display_name": "李四",
      "email": "li.si@example.com",
      "provider": "aliyun",
      "cloud_user_id": "u-789012",
      "user_groups": [1],
      "status": "active",
      "tenant_id": "tenant-001"
    }
  ]
}
```

## 使用示例

### 示例 1: 查询用户组成员

```bash
# 获取用户组 ID 为 1 的所有成员
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups/1/members" \
  -H "X-Tenant-ID: tenant-001"
```

### 示例 2: 前端使用

```javascript
// 查询用户组列表
async function getGroups() {
  const response = await fetch("/api/v1/cam/iam/groups", {
    headers: {
      "X-Tenant-ID": "tenant-001",
    },
  });
  const data = await response.json();
  return data.data.list;
}

// 查询指定用户组的成员
async function getGroupMembers(groupId) {
  const response = await fetch(`/api/v1/cam/iam/groups/${groupId}/members`, {
    headers: {
      "X-Tenant-ID": "tenant-001",
    },
  });
  const data = await response.json();
  return data.data;
}

// 使用示例
const groups = await getGroups();
for (const group of groups) {
  console.log(`用户组: ${group.name}, 成员数: ${group.user_count}`);

  // 获取成员详情
  const members = await getGroupMembers(group.id);
  console.log("成员列表:", members);
}
```

### 示例 3: 带成员信息的用户组列表

如果前端需要一次性获取所有用户组及其成员，可以这样做：

```javascript
async function getGroupsWithMembers() {
  // 1. 获取用户组列表
  const groupsResponse = await fetch("/api/v1/cam/iam/groups", {
    headers: { "X-Tenant-ID": "tenant-001" },
  });
  const groupsData = await groupsResponse.json();
  const groups = groupsData.data.list;

  // 2. 并发获取所有用户组的成员
  const groupsWithMembers = await Promise.all(
    groups.map(async (group) => {
      const membersResponse = await fetch(
        `/api/v1/cam/iam/groups/${group.id}/members`,
        { headers: { "X-Tenant-ID": "tenant-001" } }
      );
      const membersData = await membersResponse.json();

      return {
        ...group,
        members: membersData.data,
      };
    })
  );

  return groupsWithMembers;
}

// 使用
const groupsWithMembers = await getGroupsWithMembers();
console.log(groupsWithMembers);
// [
//   {
//     id: 1,
//     name: "开发组",
//     user_count: 5,
//     members: [...]
//   },
//   ...
// ]
```

## 性能优化

### 当前实现

```go
// 查询所有用户，然后筛选
func GetGroupMembers(groupID) {
    // 1. 查询该租户的所有用户
    users := userRepo.List(filter)

    // 2. 筛选出属于该用户组的用户
    members := []
    for user in users {
        if user.UserGroups contains groupID {
            members.append(user)
        }
    }

    return members
}
```

**性能**:

- 时间复杂度: O(n)，n 为用户总数
- 适用场景: 用户数量 < 10,000

### 优化方案 1: MongoDB 聚合查询

如果用户数量很大，可以使用 MongoDB 聚合查询：

```go
// 使用 MongoDB 聚合直接筛选
func GetGroupMembers(groupID) {
    pipeline := []bson.M{
        {"$match": bson.M{
            "tenant_id": tenantID,
            "permission_groups": groupID,
        }},
        {"$limit": 1000},
    }

    users := collection.Aggregate(pipeline)
    return users
}
```

**性能**:

- 使用索引，时间复杂度: O(log n)
- 需要在 `permission_groups` 字段上创建索引

### 优化方案 2: 缓存

对于频繁查询的用户组，可以使用 Redis 缓存：

```go
func GetGroupMembers(groupID) {
    // 1. 尝试从缓存获取
    cacheKey := fmt.Sprintf("group:%d:members", groupID)
    if cached := redis.Get(cacheKey); cached != nil {
        return cached
    }

    // 2. 从数据库查询
    members := queryFromDB(groupID)

    // 3. 写入缓存（5分钟过期）
    redis.Set(cacheKey, members, 5*time.Minute)

    return members
}
```

## 索引建议

为了提高查询性能，建议创建以下索引：

```javascript
// MongoDB 索引
db.cloud_iam_users.createIndex(
  { tenant_id: 1, permission_groups: 1 },
  { name: "idx_tenant_groups" }
);
```

## 错误处理

### 错误码

| 错误码 | 说明         | HTTP 状态码 |
| ------ | ------------ | ----------- |
| 0      | 成功         | 200         |
| 400    | 参数错误     | 400         |
| 404    | 用户组不存在 | 500         |
| 500    | 服务器错误   | 500         |

### 错误示例

**用户组不存在**:

```json
{
  "code": 500,
  "message": "用户组不存在"
}
```

**参数错误**:

```json
{
  "code": 400,
  "message": "invalid syntax"
}
```

## 前端展示建议

### 用户组列表页

```
┌─────────────────────────────────────────┐
│ 用户组管理                               │
├─────────────────────────────────────────┤
│ 用户组名称    成员数    操作             │
├─────────────────────────────────────────┤
│ 开发组        5        [查看成员] [编辑] │
│ 测试组        3        [查看成员] [编辑] │
│ 运维组        8        [查看成员] [编辑] │
└─────────────────────────────────────────┘
```

### 用户组详情页

```
┌─────────────────────────────────────────┐
│ 用户组详情 - 开发组                      │
├─────────────────────────────────────────┤
│ 基本信息:                                │
│   名称: 开发组                           │
│   描述: 开发人员用户组                   │
│   成员数: 5                              │
│                                          │
│ 成员列表:                                │
│ ┌─────────────────────────────────────┐ │
│ │ 用户名      姓名    邮箱      操作   │ │
│ ├─────────────────────────────────────┤ │
│ │ zhang.san   张三    zhang@...  [移除]│ │
│ │ li.si       李四    li@...     [移除]│ │
│ │ wang.wu     王五    wang@...   [移除]│ │
│ └─────────────────────────────────────┘ │
│                                          │
│ [添加成员] [返回]                        │
└─────────────────────────────────────────┘
```

## 相关 API

### 1. 查询用户组列表

```
GET /api/v1/cam/iam/groups
```

### 2. 查询用户组详情

```
GET /api/v1/cam/iam/groups/{id}
```

### 3. 查询用户组成员（新增）

```
GET /api/v1/cam/iam/groups/{id}/members
```

### 4. 查询用户列表

```
GET /api/v1/cam/iam/users
```

## 测试

### 测试脚本

```bash
#!/bin/bash

TENANT_ID="tenant-001"
GROUP_ID=1

echo "=== 测试用户组成员查询 ==="

# 1. 查询用户组列表
echo "1. 查询用户组列表"
curl -s -X GET "http://localhost:8080/api/v1/cam/iam/groups" \
  -H "X-Tenant-ID: $TENANT_ID" | jq '.'

# 2. 查询指定用户组的成员
echo "2. 查询用户组成员"
curl -s -X GET "http://localhost:8080/api/v1/cam/iam/groups/$GROUP_ID/members" \
  -H "X-Tenant-ID: $TENANT_ID" | jq '.'

# 3. 验证成员数量
echo "3. 验证成员数量"
MEMBER_COUNT=$(curl -s -X GET "http://localhost:8080/api/v1/cam/iam/groups/$GROUP_ID/members" \
  -H "X-Tenant-ID: $TENANT_ID" | jq '.data | length')
echo "成员数量: $MEMBER_COUNT"
```

## 总结

### 优势

1. ✅ **数据一致性**: 用户组和用户数据分离，避免冗余
2. ✅ **灵活性**: 可以按需查询，不影响其他接口
3. ✅ **易维护**: 不需要修改现有数据结构
4. ✅ **性能可控**: 可以通过索引和缓存优化

### 使用建议

1. **列表页**: 只显示用户组基本信息和成员数量
2. **详情页**: 点击"查看成员"时再调用成员查询 API
3. **批量查询**: 如果需要一次性获取多个用户组的成员，使用并发请求
4. **性能优化**: 对于大量用户的场景，考虑添加索引和缓存

---

**更新日期**: 2025-11-23  
**版本**: v1.0
