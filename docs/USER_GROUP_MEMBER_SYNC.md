# 用户组成员同步功能

## 功能概述

在原有的用户组同步功能基础上，新增了用户组成员的自动同步功能。当同步云平台用户组时，系统会自动获取并同步该用户组的所有成员。

## 主要特性

### 1. 自动成员同步

- 在同步用户组时，自动获取云平台用户组的成员列表
- 支持阿里云、腾讯云等多云平台
- 自动创建本地不存在的用户
- 自动将用户关联到对应的用户组

### 2. 增量同步

- 检查用户是否已存在，避免重复创建
- 检查用户是否已在用户组中，避免重复关联
- 保持用户的现有用户组关系

### 3. 同步结果统计

同步完成后返回详细的统计信息：

```json
{
  "total_groups": 10, // 总用户组数
  "created_groups": 3, // 新创建的用户组数
  "updated_groups": 7, // 更新的用户组数
  "failed_groups": 0, // 失败的用户组数
  "total_members": 45, // 总成员数
  "synced_members": 43, // 成功同步的成员数
  "failed_members": 2 // 失败的成员数
}
```

## 技术实现

### 核心方法

#### 1. syncGroupMembers

```go
func (s *userGroupService) syncGroupMembers(
    ctx context.Context,
    adapter CloudIAMAdapter,
    account *domain.CloudAccount,
    cloudGroupID string,
    localGroupID int64
) *GroupMemberSyncResult
```

**功能**：同步单个用户组的所有成员

- 调用云平台适配器获取成员列表
- 逐个同步成员到本地数据库
- 返回同步结果统计

#### 2. syncGroupMember

```go
func (s *userGroupService) syncGroupMember(
    ctx context.Context,
    cloudMember *domain.CloudUser,
    account *domain.CloudAccount,
    groupID int64
) error
```

**功能**：同步单个用户组成员

- 检查用户是否已存在（通过 CloudUserID + Provider）
- 不存在则创建新用户，并直接关联到用户组
- 已存在则检查是否在用户组中，不在则添加

### 数据流程

```
1. 调用 SyncGroups(cloudAccountID)
   ↓
2. 获取云平台用户组列表
   ↓
3. 对每个用户组：
   a. 同步用户组基本信息
   b. 调用 syncGroupMembers
      ↓
      - 获取云平台用户组成员列表
      - 对每个成员调用 syncGroupMember
        ↓
        - 检查用户是否存在
        - 创建或更新用户
        - 关联到用户组
   ↓
4. 返回完整的同步结果统计
```

## API 使用

### 同步用户组及成员

**请求**：

```http
POST /api/v1/cam/iam/groups/sync?cloud_account_id=123
X-Tenant-ID: tenant-001
```

**响应**：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_groups": 10,
    "created_groups": 3,
    "updated_groups": 7,
    "failed_groups": 0,
    "total_members": 45,
    "synced_members": 43,
    "failed_members": 2
  }
}
```

## 云平台支持

### 阿里云 RAM

- 使用 `ListUsersForGroup` API 获取用户组成员
- 支持分页获取大量成员

### 腾讯云 CAM

- 使用 `ListUsersForGroup` API 获取用户组成员
- 支持分页获取大量成员

## 日志记录

系统会记录详细的同步日志：

```
INFO  开始同步用户组成员 cloud_group_id=group-123 local_group_id=456
INFO  创建同步用户成功 user_id=789 username=test-user group_id=456
INFO  将用户添加到用户组 user_id=790 group_id=456
INFO  用户组成员同步完成 cloud_group_id=group-123 total=10 synced=9 failed=1
```

## 错误处理

- 获取成员列表失败：记录错误日志，继续处理其他用户组
- 单个成员同步失败：记录警告日志，继续处理其他成员
- 所有错误都会在同步结果中统计

## 性能优化

1. **批量处理**：一次性获取所有用户组，然后逐个同步
2. **增量更新**：只创建不存在的用户，避免重复操作
3. **并发控制**：通过云平台适配器的限流器控制 API 调用频率
4. **错误隔离**：单个用户或用户组失败不影响其他数据的同步

## 注意事项

1. 首次同步可能需要较长时间，取决于用户组和成员数量
2. 建议在业务低峰期执行大规模同步
3. 同步不会删除本地已有的用户或用户组关系
4. 用户的 CloudUserID 和 Provider 组合是唯一标识
