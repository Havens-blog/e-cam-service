# 用户个人权限同步功能

## 功能概述

在同步云平台用户时，不仅同步用户的基本信息和用户组关系，还要同步用户的**个人权限策略**（直接附加到用户的权限，而非通过用户组继承的权限）。

## 数据模型变更

### 1. CloudUser 领域模型

添加 `Policies` 字段存储用户的个人权限策略：

```go
// CloudUser 云平台用户领域模型
type CloudUser struct {
    ID             int64              `json:"id" bson:"id"`
    Username       string             `json:"username" bson:"username"`
    // ... 其他字段
    UserGroups     []int64            `json:"user_groups" bson:"permission_groups"` // 用户所属的用户组ID列表
    Policies       []PermissionPolicy `json:"policies" bson:"policies"`             // 用户的个人权限策略列表 🆕
    // ... 其他字段
}
```

### 2. 字段说明

| 字段          | 类型                 | 说明                                           |
| ------------- | -------------------- | ---------------------------------------------- |
| `user_groups` | `[]int64`            | 用户所属的用户组 ID 列表（通过用户组继承权限） |
| `policies`    | `[]PermissionPolicy` | 用户的个人权限策略列表（直接附加到用户的权限） |

## 权限类型

### 1. 用户组权限（Group Policies）

- 通过 `user_groups` 字段关联
- 用户加入用户组后自动继承该用户组的所有权限
- 修改用户组权限会影响该用户组的所有成员

### 2. 个人权限（Personal Policies）

- 通过 `policies` 字段存储
- 直接附加到用户的权限策略
- 只影响该用户，不影响其他用户

### 3. 有效权限（Effective Policies）

用户的有效权限 = 个人权限 + 所有用户组权限（合并去重）

## 云平台适配器接口

### 新增方法

```go
// CloudIAMAdapter 云平台IAM适配器接口
type CloudIAMAdapter interface {
    // GetUserPolicies 获取用户的个人权限策略
    GetUserPolicies(ctx context.Context, account *domain.CloudAccount, userID string) ([]domain.PermissionPolicy, error)

    // ... 其他方法
}
```

## 实现步骤

### 1. 阿里云 RAM 适配器

```go
// GetUserPolicies 获取RAM用户的个人权限策略
func (a *Adapter) GetUserPolicies(ctx context.Context, account *domain.CloudAccount, userID string) ([]domain.PermissionPolicy, error) {
    client, err := aliyuncommon.CreateRAMClient(account)
    if err != nil {
        return nil, err
    }

    // 获取用户的策略列表
    request := ram.CreateListPoliciesForUserRequest()
    request.Scheme = "https"
    request.UserName = userID

    response, err := client.ListPoliciesForUser(request)
    if err != nil {
        return nil, fmt.Errorf("failed to list policies for user: %w", err)
    }

    // 转换为领域模型
    policies := make([]domain.PermissionPolicy, 0, len(response.Policies.Policy))
    for _, policy := range response.Policies.Policy {
        policies = append(policies, domain.PermissionPolicy{
            PolicyID:   policy.PolicyName,
            PolicyName: policy.PolicyName,
            PolicyType: domain.PolicyType(policy.PolicyType),
            Provider:   domain.CloudProviderAliyun,
        })
    }

    return policies, nil
}
```

### 2. 腾讯云 CAM 适配器

```go
// GetUserPolicies 获取CAM用户的个人权限策略
func (a *Adapter) GetUserPolicies(ctx context.Context, account *domain.CloudAccount, userID string) ([]domain.PermissionPolicy, error) {
    client, err := tencentcommon.CreateCAMClient(account)
    if err != nil {
        return nil, err
    }

    // 获取用户的策略列表
    request := cam.NewListAttachedUserPoliciesRequest()
    uin, _ := strconv.ParseUint(userID, 10, 64)
    request.TargetUin = &uin

    response, err := client.ListAttachedUserPolicies(request)
    if err != nil {
        return nil, fmt.Errorf("failed to list policies for user: %w", err)
    }

    // 转换为领域模型
    policies := make([]domain.PermissionPolicy, 0, len(response.Response.List))
    for _, policy := range response.Response.List {
        policies = append(policies, domain.PermissionPolicy{
            PolicyID:   fmt.Sprintf("%d", *policy.PolicyId),
            PolicyName: *policy.PolicyName,
            PolicyType: domain.PolicyTypeSystem, // 根据实际情况判断
            Provider:   domain.CloudProviderTencent,
        })
    }

    return policies, nil
}
```

### 3. 用户同步服务

修改用户同步逻辑，在同步用户时获取并保存个人权限：

```go
// syncSingleUser 同步单个用户
func (s *userService) syncSingleUser(ctx context.Context, cloudUser *domain.CloudUser, account *domain.CloudAccount, adapter iam.CloudIAMAdapter) error {
    // 1. 获取用户的个人权限策略
    policies, err := adapter.GetUserPolicies(ctx, account, cloudUser.CloudUserID)
    if err != nil {
        s.logger.Warn("获取用户个人权限失败",
            elog.String("user_id", cloudUser.CloudUserID),
            elog.FieldErr(err))
        // 权限获取失败不影响用户同步，继续执行
        policies = []domain.PermissionPolicy{}
    }
    cloudUser.Policies = policies

    // 2. 检查用户是否已存在
    existingUser, err := s.userRepo.GetByCloudUserID(ctx, cloudUser.CloudUserID, account.Provider)
    if err != nil && err != mongo.ErrNoDocuments {
        return fmt.Errorf("查询用户失败: %w", err)
    }

    if err == mongo.ErrNoDocuments {
        // 创建新用户
        return s.createSyncedUser(ctx, cloudUser, account)
    }

    // 更新现有用户
    return s.updateSyncedUser(ctx, &existingUser, cloudUser)
}
```

## API 响应示例

### 查询用户详情

```bash
GET /api/v1/cam/iam/users/1
```

响应：

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 1,
    "username": "alice",
    "display_name": "Alice Wang",
    "email": "alice@example.com",
    "user_groups": [1, 2],
    "policies": [
      {
        "policy_id": "AliyunOSSReadOnlyAccess",
        "policy_name": "AliyunOSSReadOnlyAccess",
        "policy_type": "system",
        "provider": "aliyun"
      },
      {
        "policy_id": "custom-policy-001",
        "policy_name": "自定义策略",
        "policy_type": "custom",
        "provider": "aliyun"
      }
    ],
    "status": "active",
    "create_time": "2025-11-25T10:00:00Z",
    "update_time": "2025-11-25T12:00:00Z"
  }
}
```

### 查询用户有效权限

```bash
GET /api/v1/cam/iam/permissions/users/1/effective
```

响应：

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "user_id": 1,
    "username": "alice",
    "personal_policies": [
      {
        "policy_id": "AliyunOSSReadOnlyAccess",
        "policy_name": "AliyunOSSReadOnlyAccess",
        "policy_type": "system",
        "provider": "aliyun",
        "source": "personal"
      }
    ],
    "group_policies": [
      {
        "policy_id": "AliyunECSFullAccess",
        "policy_name": "AliyunECSFullAccess",
        "policy_type": "system",
        "provider": "aliyun",
        "source": "group",
        "group_id": 1,
        "group_name": "开发组"
      }
    ],
    "effective_policies": [
      {
        "policy_id": "AliyunOSSReadOnlyAccess",
        "policy_name": "AliyunOSSReadOnlyAccess",
        "policy_type": "system",
        "provider": "aliyun"
      },
      {
        "policy_id": "AliyunECSFullAccess",
        "policy_name": "AliyunECSFullAccess",
        "policy_type": "system",
        "provider": "aliyun"
      }
    ]
  }
}
```

## 前端展示

### 用户详情页

```
用户信息
├── 基本信息
│   ├── 用户名: alice
│   ├── 显示名: Alice Wang
│   └── 邮箱: alice@example.com
├── 用户组 (2)
│   ├── 开发组
│   └── 测试组
└── 个人权限 (2) 🆕
    ├── AliyunOSSReadOnlyAccess (系统策略)
    └── 自定义策略 (自定义策略)
```

### 权限矩阵视图

| 用户  | 个人权限 | 用户组 | 用户组权限   | 有效权限               |
| ----- | -------- | ------ | ------------ | ---------------------- |
| alice | OSS 只读 | 开发组 | ECS 完全访问 | OSS 只读, ECS 完全访问 |
| bob   | -        | 开发组 | ECS 完全访问 | ECS 完全访问           |

## 数据库索引

为了提升查询性能，建议创建以下索引：

```javascript
// 用户查询索引
db.cloud_iam_users.createIndex({
  cloud_user_id: 1,
  provider: 1,
});

// 用户组成员查询索引
db.cloud_iam_users.createIndex({
  permission_groups: 1,
  tenant_id: 1,
});

// 租户用户查询索引
db.cloud_iam_users.createIndex({
  tenant_id: 1,
  status: 1,
});
```

## 注意事项

### 1. 权限合并规则

- 个人权限和用户组权限是**并集**关系（取并集）
- 相同的权限策略只保留一份（去重）
- 不存在权限冲突，只有权限叠加

### 2. 权限同步频率

- 用户同步时自动同步个人权限
- 建议定期同步（如每小时一次）
- 支持手动触发同步

### 3. 错误处理

- 个人权限获取失败不影响用户同步
- 记录警告日志，继续执行
- 下次同步时重试

### 4. 性能考虑

- 批量同步时使用并发控制
- 限制并发数量（如 10 个并发）
- 使用速率限制避免 API 限流

## 相关文档

- [用户权限查询 API](./USER_PERMISSIONS_API.md)
- [用户组成员同步功能](./USER_GROUP_MEMBER_SYNC.md)
- [IAM API 快速参考](./IAM_API_QUICK_REFERENCE.md)

## 实现状态

- [x] 数据模型变更
- [x] Repository 层转换逻辑
- [ ] 阿里云适配器实现
- [ ] 腾讯云适配器实现
- [ ] 用户同步服务修改
- [ ] API 响应更新
- [ ] 前端展示支持

## 下一步

1. 实现各云平台适配器的 `GetUserPolicies` 方法
2. 修改用户同步服务，在同步时获取个人权限
3. 更新权限查询 API，区分个人权限和用户组权限
4. 前端添加个人权限展示
