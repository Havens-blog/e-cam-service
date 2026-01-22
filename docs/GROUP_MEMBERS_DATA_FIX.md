# 用户组成员数据修复指南

## 问题描述

查询用户组成员时，所有用户组都只显示 1 个成员，但实际应该有多个成员。

## 问题原因

可能的原因：

### 1. 用户的 `permission_groups` 字段未正确填充

用户数据中的 `permission_groups` 字段（对应 JSON 的 `user_groups`）可能：

- 不存在
- 为空数组 `[]`
- 数据不完整

### 2. 数据同步问题

- 用户是在用户组同步之前创建的
- 用户同步时没有正确设置用户组关系
- 数据迁移时字段丢失

### 3. `user_count` 字段不准确

用户组的 `user_count` 字段可能与实际成员数不一致。

## 数据结构说明

### CloudUser 模型

```go
type CloudUser struct {
    ID         int64   `json:"id" bson:"id"`
    Username   string  `json:"username" bson:"username"`
    UserGroups []int64 `json:"user_groups" bson:"permission_groups"` // 用户所属的用户组ID列表
    // ... 其他字段
}
```

### 正确的数据示例

```json
{
  "id": 1,
  "username": "alice",
  "permission_groups": [1, 2, 3], // alice 属于用户组 1, 2, 3
  "tenant_id": "tenant-001"
}
```

### 错误的数据示例

```json
{
  "id": 1,
  "username": "alice",
  // permission_groups 字段不存在或为空
  "tenant_id": "tenant-001"
}
```

## 诊断步骤

### 1. 快速检查

```bash
bash scripts/quick_check_members.sh
```

这会显示：

- 用户组数量
- 用户数量
- 有/无 `permission_groups` 的用户数量
- 第一个用户组的实际成员

### 2. 详细诊断

```bash
go run scripts/diagnose_group_members.go
```

这会检查：

- 所有用户组
- 所有用户的 `permission_groups` 字段
- 每个用户组的实际成员数
- `user_count` 与实际成员数的对比

### 3. 手动检查（MongoDB）

```javascript
// 连接数据库
use e-cam-service

// 查看用户组
db.cloud_iam_groups.find().pretty()

// 查看用户
db.cloud_iam_users.find().pretty()

// 查看没有 permission_groups 的用户
db.cloud_iam_users.find({
  $or: [
    {permission_groups: {$exists: false}},
    {permission_groups: []}
  ]
})

// 查询用户组 ID=1 的成员
db.cloud_iam_users.find({
  permission_groups: 1
})
```

## 修复方案

### 方案 1: 自动修复脚本（推荐）

```bash
# 运行修复脚本
go run scripts/fix_group_members.go
```

这会：

1. 修复所有用户组的 `user_count` 字段
2. 为没有 `permission_groups` 的用户初始化空数组
3. 显示修复结果

### 方案 2: 重新同步用户组

```bash
# 通过 API 重新同步用户组
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001"
```

这会：

1. 从云平台获取最新的用户组和成员
2. 自动创建或更新用户
3. 正确设置用户的 `permission_groups`

### 方案 3: 手动修复（MongoDB）

```javascript
// 1. 为所有用户初始化 permission_groups
db.cloud_iam_users.updateMany(
  { permission_groups: { $exists: false } },
  { $set: { permission_groups: [] } }
);

// 2. 修复用户组的 user_count
db.cloud_iam_groups.find().forEach(function (group) {
  var count = db.cloud_iam_users.countDocuments({
    permission_groups: group.id,
  });
  db.cloud_iam_groups.updateOne(
    { id: group.id },
    { $set: { user_count: count } }
  );
  print("用户组 " + group.name + " (ID: " + group.id + ") 成员数: " + count);
});
```

## 验证修复

### 1. 运行诊断脚本

```bash
go run scripts/diagnose_group_members.go
```

应该看到：

- ✅ 所有用户都有 `permission_groups` 字段
- ✅ 用户组的 `user_count` 与实际成员数一致

### 2. 测试 API

```bash
# 查询用户组成员
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups/1/members" \
  -H "X-Tenant-ID: tenant-001"
```

应该返回所有成员。

### 3. 查看用户详情

```bash
# 查询用户详情
curl -X GET "http://localhost:8080/api/v1/cam/iam/users/1" \
  -H "X-Tenant-ID: tenant-001"
```

应该看到 `user_groups` 字段包含用户所属的用户组 ID。

## 预防措施

### 1. 确保同步顺序

正确的同步顺序：

1. 先同步用户组
2. 再同步用户（或在同步用户组时自动同步成员）

### 2. 数据验证

在创建或更新用户时，确保：

- `permission_groups` 字段存在
- 至少初始化为空数组 `[]`

### 3. 定期检查

定期运行诊断脚本检查数据一致性：

```bash
# 添加到定时任务
0 2 * * * cd /path/to/project && go run scripts/diagnose_group_members.go
```

## 常见问题

### Q1: 为什么用户的字段叫 `permission_groups` 而不是 `user_groups`？

**A**: 这是为了兼容旧数据。在代码中：

- Go 结构体字段名：`UserGroups`
- JSON 序列化名：`user_groups`
- MongoDB 存储名：`permission_groups`

### Q2: 同步用户组后，用户的 `permission_groups` 还是空的？

**A**: 可能原因：

1. 用户是在用户组同步之前创建的
2. 需要重新同步用户组（会自动同步成员）
3. 云平台 API 返回的成员列表为空

解决方案：重新同步用户组。

### Q3: `user_count` 显示的数量和实际查询的不一致？

**A**: 运行修复脚本：

```bash
go run scripts/fix_group_members.go
```

### Q4: 如何批量为用户分配用户组？

**A**: 使用批量分配 API：

```bash
curl -X POST "http://localhost:8080/api/v1/cam/iam/users/assign-groups" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "user_ids": [1, 2, 3],
    "group_ids": [1, 2]
  }'
```

## 相关文档

- [用户组成员查询修复](./GROUP_MEMBERS_QUERY_FIX.md)
- [用户组成员同步功能](./USER_GROUP_MEMBER_SYNC.md)
- [用户组成员查询 API](./GROUP_MEMBERS_API.md)

## 总结

用户组成员查询问题通常是由于用户的 `permission_groups` 字段未正确填充导致的。

**快速解决方案**：

1. 运行诊断脚本：`go run scripts/diagnose_group_members.go`
2. 运行修复脚本：`go run scripts/fix_group_members.go`
3. 重新同步用户组：`POST /api/v1/cam/iam/groups/sync`
4. 验证修复结果

---

**创建时间**: 2025-11-25  
**版本**: v1.2.0
