# 用户组同步问题修复

## 问题描述

在同步用户组时发现两个问题：

### 问题 1: 云账号 ID 不一致

**现象**: 同步用户组后，返回的用户组中的 `cloud_account_id` 与请求的云账号 ID 不一致

**原因**: 在 `updateSyncedGroup` 方法中，更新已存在的用户组时，没有更新 `CloudAccountID` 和 `Provider` 字段，导致这些字段保留了旧值。

### 问题 2: 用户数量为 0

**现象**: 同步用户组后，用户组的 `user_count` 字段始终为 0

**原因**:

1. 创建用户组时 `UserCount` 初始化为 0
2. 同步成员后没有更新 `UserCount` 字段
3. 更新用户组时保留了旧的 `UserCount` 值

## 修复方案

### 修复 1: 保持云账号信息一致

**文件**: `internal/cam/iam/service/group.go`

**修改**: 在 `updateSyncedGroup` 方法中保留 `UserCount`，但允许更新 `CloudAccountID` 和 `Provider`

```go
// updateSyncedGroup 更新同步的用户组
func (s *userGroupService) updateSyncedGroup(ctx context.Context, existingGroup, cloudGroup *domain.UserGroup) error {
	// 保留本地数据
	cloudGroup.ID = existingGroup.ID
	cloudGroup.Name = existingGroup.Name
	cloudGroup.TenantID = existingGroup.TenantID
	cloudGroup.CreateTime = existingGroup.CreateTime
	cloudGroup.CTime = existingGroup.CTime

	// 保留用户数量（不从云端同步，由本地维护）
	cloudGroup.UserCount = existingGroup.UserCount

	// 更新时间
	now := time.Now()
	cloudGroup.UpdateTime = now
	cloudGroup.UTime = now.Unix()

	if err := s.groupRepo.Update(ctx, *cloudGroup); err != nil {
		return fmt.Errorf("更新用户组失败: %w", err)
	}

	s.logger.Info("更新同步用户组成功",
		elog.Int64("group_id", existingGroup.ID),
		elog.String("group_name", cloudGroup.GroupName),
		elog.Int64("cloud_account_id", cloudGroup.CloudAccountID))

	return nil
}
```

**说明**:

- `CloudAccountID` 和 `Provider` 会从 `cloudGroup` 中获取（在 `createSyncedGroup` 中已设置）
- `UserCount` 保留本地值，由后续的 `updateGroupUserCount` 方法更新

### 修复 2: 同步后更新用户数量

**文件**: `internal/cam/iam/service/group.go`

**修改 1**: 在同步成员后调用更新用户数量的方法

```go
// 同步用户组成员
memberResult := s.syncGroupMembers(ctx, adapter, &account, cloudGroup.CloudGroupID, groupID)
result.TotalMembers += memberResult.Total
result.SyncedMembers += memberResult.Synced
result.FailedMembers += memberResult.Failed

// 更新用户组的用户数量
if memberResult.Synced > 0 {
	if err := s.updateGroupUserCount(ctx, groupID); err != nil {
		s.logger.Warn("更新用户组用户数量失败",
			elog.Int64("group_id", groupID),
			elog.FieldErr(err))
	}
}
```

**修改 2**: 添加 `updateGroupUserCount` 方法

```go
// updateGroupUserCount 更新用户组的用户数量
func (s *userGroupService) updateGroupUserCount(ctx context.Context, groupID int64) error {
	// 查询该用户组的用户数量
	filter := domain.CloudUserFilter{
		Limit: 1000, // 设置一个较大的限制
	}

	users, _, err := s.userRepo.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("查询用户列表失败: %w", err)
	}

	// 统计包含该用户组的用户数量
	count := 0
	for _, user := range users {
		for _, gid := range user.UserGroups {
			if gid == groupID {
				count++
				break
			}
		}
	}

	// 获取用户组
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("获取用户组失败: %w", err)
	}

	// 更新用户数量
	group.UserCount = count
	now := time.Now()
	group.UpdateTime = now
	group.UTime = now.Unix()

	if err := s.groupRepo.Update(ctx, group); err != nil {
		return fmt.Errorf("更新用户组失败: %w", err)
	}

	s.logger.Info("更新用户组用户数量成功",
		elog.Int64("group_id", groupID),
		elog.Int("user_count", count))

	return nil
}
```

## 验证修复

### 步骤 1: 同步用户组

```bash
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json"
```

**预期结果**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_groups": 5,
    "created_groups": 2,
    "updated_groups": 3,
    "failed_groups": 0,
    "total_members": 15,
    "synced_members": 14,
    "failed_members": 1
  }
}
```

### 步骤 2: 查询用户组列表

```bash
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups?page=1&size=10" \
  -H "X-Tenant-ID: tenant-001"
```

**验证点**:

1. ✅ `cloud_account_id` 应该与同步时使用的云账号 ID 一致
2. ✅ `user_count` 应该显示正确的用户数量（不为 0）
3. ✅ `provider` 应该与云账号的 provider 一致

**预期结果示例**:

```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 1,
        "name": "开发组",
        "cloud_account_id": 1, // ✓ 与请求一致
        "provider": "aliyun", // ✓ 正确
        "user_count": 5, // ✓ 不为 0
        "member_count": 5
      }
    ],
    "total": 5
  }
}
```

### 步骤 3: 查询用户列表

```bash
curl -X GET "http://localhost:8080/api/v1/cam/iam/users?page=1&size=10" \
  -H "X-Tenant-ID: tenant-001"
```

**验证点**:

1. ✅ 应该能看到同步的用户
2. ✅ 用户的 `user_groups` 字段应该包含用户组 ID

## 注意事项

### 1. 用户数量统计的性能

当前实现会查询所有用户并遍历统计，如果用户数量很大（>1000），可能会有性能问题。

**优化建议**:

- 使用 MongoDB 聚合查询直接统计
- 或者在用户加入/离开用户组时增量更新计数

### 2. 并发同步

如果多个云账号同时同步相同名称的用户组，可能会有冲突。

**建议**:

- 使用 `cloud_account_id + group_name` 作为唯一标识
- 或者在用户组表中添加唯一索引

### 3. 用户组迁移

如果用户组从一个云账号迁移到另一个云账号，当前实现会更新 `cloud_account_id`。

**注意**:

- 确保这是预期行为
- 如果不希望迁移，需要修改 `updateSyncedGroup` 逻辑

## 性能优化建议

### 优化 1: 使用聚合查询统计用户数量

```go
// 使用 MongoDB 聚合查询
func (s *userGroupService) updateGroupUserCount(ctx context.Context, groupID int64) error {
	// 使用聚合查询统计
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"permission_groups": groupID}}},
		{{Key: "$count", Value: "count"}},
	}

	// ... 执行聚合查询
}
```

### 优化 2: 批量更新用户数量

```go
// 同步完所有用户组后，批量更新用户数量
func (s *userGroupService) batchUpdateGroupUserCounts(ctx context.Context, groupIDs []int64) error {
	// 一次性查询所有用户
	// 统计每个用户组的用户数量
	// 批量更新
}
```

## 相关文档

- [用户组成员同步功能](USER_GROUP_MEMBER_SYNC.md)
- [Tenant ID 问题排查](TROUBLESHOOTING_TENANT_ID.md)
- [云账号 Tenant ID 更新](CLOUD_ACCOUNT_TENANT_ID_FIX.md)

## 更新日志

- **2025-11-23**: 修复云账号 ID 不一致问题
- **2025-11-23**: 添加用户数量自动更新功能
- **2025-11-23**: 优化 updateSyncedGroup 逻辑
