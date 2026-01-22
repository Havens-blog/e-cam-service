# IAM 用户组同步功能实现文档

## 概述

本文档说明多云 IAM 用户组（权限组）和用户组权限的同步功能实现情况。

## 功能清单

### 1. 接口定义

已在 `internal/shared/cloudx/iam/adapter.go` 中扩展了 `CloudIAMAdapter` 接口，新增以下方法：

#### 用户组管理

- ✅ `ListGroups`: 获取用户组列表
- ✅ `GetGroup`: 获取用户组详情
- ✅ `CreateGroup`: 创建用户组
- ✅ `UpdateGroupPolicies`: 更新用户组权限策略
- ✅ `DeleteGroup`: 删除用户组

#### 用户组成员管理

- ✅ `ListGroupUsers`: 获取用户组成员列表
- ✅ `AddUserToGroup`: 将用户添加到用户组
- ✅ `RemoveUserFromGroup`: 将用户从用户组移除

#### 策略管理

- ✅ `GetPolicy`: 获取策略详情（新增）

### 2. 阿里云 RAM 实现

#### 文件结构

```
internal/shared/cloudx/iam/aliyun/
├── adapter.go      # 用户和策略管理
├── group.go        # 用户组管理（新增）
├── converter.go    # 数据转换
├── types.go        # 类型定义
└── wrapper.go      # SDK 封装
```

#### 已实现功能

**用户组管理** (`group.go`)

- ✅ `ListGroups`: 分页获取所有用户组，包含策略信息
- ✅ `GetGroup`: 获取用户组详情，包含策略和成员数量
- ✅ `CreateGroup`: 创建新用户组
- ✅ `UpdateGroupPolicies`: 智能更新用户组策略（自动对比并附加/分离）
- ✅ `DeleteGroup`: 删除用户组（自动清理成员和策略）
- ✅ `ListGroupUsers`: 获取用户组成员列表
- ✅ `AddUserToGroup`: 添加用户到用户组
- ✅ `RemoveUserFromGroup`: 从用户组移除用户

**策略管理** (`adapter.go`)

- ✅ `GetPolicy`: 获取策略详情，包含策略文档
- ✅ `ListPolicies`: 获取策略列表

**数据转换** (`converter.go`)

- ✅ `ConvertRAMGroupToPermissionGroup`: RAM 用户组转换为领域模型

#### 核心特性

1. **智能策略更新**

   - 自动对比当前策略和目标策略
   - 只附加新增的策略
   - 只分离移除的策略
   - 避免不必要的 API 调用

2. **安全删除**

   - 删除用户组前自动移除所有成员
   - 删除用户组前自动分离所有策略
   - 确保资源清理完整

3. **完整信息**

   - 获取用户组时包含策略列表
   - 获取用户组时包含成员数量
   - 获取策略时包含完整策略文档

4. **错误处理**
   - 使用指数退避重试机制
   - 限流保护（20 QPS）
   - 详细的错误日志

### 3. AWS IAM 实现

#### 文件结构

```
internal/shared/cloudx/iam/aws/
├── adapter.go      # 用户和策略管理
├── group.go        # 用户组管理（已实现）
├── converter.go    # 数据转换
├── types.go        # 类型定义
└── wrapper.go      # SDK 封装
```

#### 实现状态

**用户组管理** (`group.go`)

- ✅ `ListGroups`: 分页获取所有用户组，包含策略信息
- ✅ `GetGroup`: 获取用户组详情，包含策略和成员数量
- ✅ `CreateGroup`: 创建新用户组
- ✅ `UpdateGroupPolicies`: 智能更新用户组策略（自动对比并附加/分离）
- ✅ `DeleteGroup`: 删除用户组（自动清理成员和策略）
- ✅ `ListGroupUsers`: 获取用户组成员列表
- ✅ `AddUserToGroup`: 添加用户到用户组
- ✅ `RemoveUserFromGroup`: 从用户组移除用户
- ✅ `GetPolicy`: 获取策略详情，包含策略文档

**数据转换** (`converter.go`)

- ✅ `ConvertIAMGroupToPermissionGroup`: IAM 用户组转换为领域模型

#### 核心特性

1. **智能策略更新**

   - 自动对比当前策略和目标策略
   - 只附加新增的策略
   - 只分离移除的策略
   - 避免不必要的 API 调用

2. **安全删除**

   - 删除用户组前自动移除所有成员
   - 删除用户组前自动分离所有策略
   - 确保资源清理完整

3. **完整信息**

   - 获取用户组时包含策略列表
   - 获取用户组时包含成员数量
   - 获取策略时包含完整策略文档（包括策略版本）

4. **错误处理**
   - 使用指数退避重试机制
   - 限流保护（10 QPS）
   - 详细的错误日志

### 4. 其他云厂商

- ⏳ **腾讯云 CAM**: 待实现
- ⏳ **华为云 IAM**: 待实现
- ⏳ **Azure AD**: 待实现

## API 使用示例

### 同步用户组

```bash
# 同步指定云账号的用户组
POST /api/v1/cam/iam/sync/tasks
{
  "cloud_account_id": 1,
  "task_type": "sync_groups",
  "provider": "aliyun"
}
```

### 获取用户组列表

```bash
# 获取用户组列表
GET /api/v1/cam/iam/groups?cloud_account_id=1&provider=aliyun
```

### 更新用户组权限

```bash
# 更新用户组权限策略
PUT /api/v1/cam/iam/groups/{id}/policies
{
  "policy_ids": [1, 2, 3]
}
```

### 管理用户组成员

```bash
# 批量分配用户到权限组
POST /api/v1/cam/iam/users/batch-assign
{
  "user_ids": [1, 2, 3],
  "group_ids": [10, 20]
}
```

## 数据模型

### PermissionGroup

```go
type PermissionGroup struct {
    ID             int64               // 本地ID
    GroupName      string              // 用户组名称
    DisplayName    string              // 显示名称
    Description    string              // 描述
    CloudAccountID int64               // 云账号ID
    Provider       CloudProvider       // 云厂商
    CloudGroupID   string              // 云端用户组ID
    TenantID       string              // 租户ID
    Policies       []PermissionPolicy  // 权限策略列表
    MemberCount    int                 // 成员数量
    CreateTime     time.Time           // 创建时间
    UpdateTime     time.Time           // 更新时间
}
```

### PermissionPolicy

```go
type PermissionPolicy struct {
    PolicyID       string       // 策略ID
    PolicyName     string       // 策略名称
    PolicyDocument string       // 策略文档
    Provider       CloudProvider // 云厂商
    PolicyType     PolicyType   // 策略类型（系统/自定义）
}
```

## 同步流程

### 用户组同步流程

```
1. 调用云厂商 API 获取用户组列表
   ├─ 分页获取所有用户组
   ├─ 获取每个用户组的策略列表
   └─ 获取每个用户组的成员数量

2. 转换为领域模型
   ├─ 转换用户组基本信息
   ├─ 转换策略信息
   └─ 设置成员数量

3. 保存到本地数据库
   ├─ 新增不存在的用户组
   ├─ 更新已存在的用户组
   └─ 标记已删除的用户组

4. 同步用户组策略
   ├─ 对比本地和云端策略
   ├─ 更新策略关联关系
   └─ 记录变更日志

5. 同步用户组成员
   ├─ 获取用户组成员列表
   ├─ 更新用户-用户组关联
   └─ 记录变更日志
```

### 策略更新流程

```
1. 获取当前策略列表
   └─ 调用 ListPoliciesForGroup API

2. 对比目标策略
   ├─ 找出需要附加的策略
   └─ 找出需要分离的策略

3. 分离不需要的策略
   └─ 调用 DetachPolicyFromGroup API

4. 附加新策略
   └─ 调用 AttachPolicyToGroup API

5. 记录操作日志
   └─ 记录附加和分离的策略数量
```

## 错误处理

### 常见错误

1. **EntityAlreadyExists**: 用户组已存在

   - 处理: 跳过创建，更新现有用户组

2. **NoSuchEntity**: 用户组不存在

   - 处理: 从本地数据库标记为已删除

3. **DeleteConflict**: 用户组有成员或策略

   - 处理: 先清理成员和策略，再删除

4. **Throttling**: API 限流
   - 处理: 使用指数退避重试

### 重试策略

- 最大重试次数: 3 次
- 退避策略: 指数退避
- 可重试错误: 限流错误、网络错误
- 不可重试错误: 权限错误、参数错误

## 性能优化

### 1. 批量操作

- 分页获取数据，每页 100 条
- 批量保存到数据库

### 2. 并发控制

- 使用限流器控制 QPS
- 阿里云: 20 QPS
- AWS: 10 QPS

### 3. 缓存策略

- 策略列表缓存 5 分钟
- 用户组列表缓存 5 分钟

### 4. 增量同步

- 记录最后同步时间
- 只同步变更的数据

## 测试

### 单元测试

```bash
# 测试阿里云用户组同步
go test ./internal/shared/cloudx/iam/aliyun/... -v

# 测试 AWS 用户组同步
go test ./internal/shared/cloudx/iam/aws/... -v
```

### 集成测试

```bash
# 测试完整同步流程
go test ./internal/cam/iam/service/... -v -run TestSyncGroups
```

## 后续工作

### 短期（1-2 周）

1. ✅ 完成阿里云 RAM 用户组同步
2. ✅ 实现 AWS IAM 用户组同步
3. ⏳ 添加用户组同步的单元测试
4. ⏳ 添加用户组同步的集成测试

### 中期（1 个月）

1. ⏳ 实现腾讯云 CAM 用户组同步
2. ⏳ 实现华为云 IAM 用户组同步
3. ⏳ 优化同步性能
4. ⏳ 添加同步进度监控

### 长期（2-3 个月）

1. ⏳ 实现 Azure AD 用户组同步
2. ⏳ 实现增量同步
3. ⏳ 添加同步冲突解决
4. ⏳ 实现双向同步

## 注意事项

### 1. 权限要求

**阿里云 RAM**

- `ram:ListGroups`
- `ram:GetGroup`
- `ram:CreateGroup`
- `ram:DeleteGroup`
- `ram:ListPoliciesForGroup`
- `ram:AttachPolicyToGroup`
- `ram:DetachPolicyFromGroup`
- `ram:ListUsersForGroup`
- `ram:AddUserToGroup`
- `ram:RemoveUserFromGroup`

**AWS IAM**

- `iam:ListGroups`
- `iam:GetGroup`
- `iam:CreateGroup`
- `iam:DeleteGroup`
- `iam:ListAttachedGroupPolicies`
- `iam:AttachGroupPolicy`
- `iam:DetachGroupPolicy`
- `iam:GetGroupPolicy`
- `iam:AddUserToGroup`
- `iam:RemoveUserFromGroup`

### 2. 限制

- 阿里云 RAM: 每个账号最多 100 个用户组
- AWS IAM: 每个账号最多 300 个用户组
- 用户组名称长度限制: 1-64 字符
- 每个用户组最多附加 10 个策略

### 3. 最佳实践

1. 定期同步（建议每小时一次）
2. 监控同步失败率
3. 记录详细的审计日志
4. 使用只读权限进行测试
5. 在非生产环境先测试

## 相关文档

- [IAM API 文档](./api/IAM_API.md)
- [IAM 用户管理](./api/IAM_API_Users.md)
- [IAM 权限组管理](./api/IAM_API_Groups.md)
- [IAM 同步任务](./api/IAM_API_Sync.md)
- [阿里云 RAM API 文档](https://help.aliyun.com/document_detail/28672.html)
- [AWS IAM API 文档](https://docs.aws.amazon.com/IAM/latest/APIReference/)

## 更新日志

### 2025-11-17

- ✅ 修复 domain.PermissionGroup 结构体，添加缺失字段（GroupName, DisplayName, CloudAccountID, Provider, CloudGroupID, MemberCount）
- ✅ 完整实现 AWS IAM 用户组管理功能
- ✅ 实现 AWS IAM 用户组的智能策略更新
- ✅ 实现 AWS IAM 用户组的安全删除
- ✅ 添加 AWS IAM 用户组数据转换函数
- ✅ 更新阿里云和 AWS 的 wrapper 以支持用户组管理接口

### 2025-11-11

- ✅ 扩展 CloudIAMAdapter 接口，新增用户组管理方法
- ✅ 实现阿里云 RAM 用户组完整同步功能
- ✅ 添加用户组数据转换函数
- ✅ 创建 AWS IAM 用户组占位符实现
- ✅ 添加 GetPolicy 方法获取策略详情
- ✅ 完善错误处理和重试机制
