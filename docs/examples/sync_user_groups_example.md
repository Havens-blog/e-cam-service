# 用户组成员同步示例

## 场景说明

假设你有一个阿里云账号，需要将云上的 RAM 用户组及其成员同步到本地系统。

## 前置条件

1. 已配置云账号信息（AccessKey、SecretKey）
2. 云账号 ID 为 `123`
3. 租户 ID 为 `tenant-001`

## 使用步骤

### 1. 调用同步 API

```bash
curl -X POST 'http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=123' \
  -H 'X-Tenant-ID: tenant-001' \
  -H 'Content-Type: application/json'
```

### 2. 查看同步结果

**成功响应示例**：

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

**结果说明**：

- 共发现 5 个用户组
- 新创建了 2 个用户组
- 更新了 3 个已存在的用户组
- 没有失败的用户组
- 共发现 15 个成员
- 成功同步了 14 个成员
- 1 个成员同步失败（可能是权限问题或数据异常）

### 3. 验证同步结果

#### 查询用户组列表

```bash
curl -X GET 'http://localhost:8080/api/v1/cam/iam/groups?page=1&size=20' \
  -H 'X-Tenant-ID: tenant-001'
```

**响应示例**：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1,
        "name": "开发组",
        "group_name": "developers",
        "description": "开发人员用户组",
        "provider": "aliyun",
        "cloud_group_id": "g-123456",
        "user_count": 5,
        "member_count": 5,
        "policies": [
          {
            "policy_id": "AliyunECSFullAccess",
            "policy_name": "AliyunECSFullAccess",
            "provider": "aliyun",
            "policy_type": "system"
          }
        ]
      }
    ],
    "total": 5,
    "page": 1,
    "size": 20
  }
}
```

#### 查询用户列表

```bash
curl -X GET 'http://localhost:8080/api/v1/cam/iam/users?page=1&size=20' \
  -H 'X-Tenant-ID: tenant-001'
```

**响应示例**：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1,
        "username": "zhang.san",
        "display_name": "张三",
        "email": "zhang.san@example.com",
        "provider": "aliyun",
        "cloud_user_id": "u-123456",
        "user_groups": [1, 2],
        "status": "active"
      },
      {
        "id": 2,
        "username": "li.si",
        "display_name": "李四",
        "email": "li.si@example.com",
        "provider": "aliyun",
        "cloud_user_id": "u-789012",
        "user_groups": [1],
        "status": "active"
      }
    ],
    "total": 14,
    "page": 1,
    "size": 20
  }
}
```

## 同步流程详解

### 阶段 1：同步用户组

```
1. 调用阿里云 RAM API 获取用户组列表
2. 对每个用户组：
   - 检查本地是否存在（通过 group_name + tenant_id）
   - 不存在则创建新用户组
   - 已存在则更新用户组信息（描述、策略等）
```

### 阶段 2：同步用户组成员

```
对每个用户组：
1. 调用阿里云 RAM API 获取用户组成员列表
2. 对每个成员：
   - 检查用户是否存在（通过 cloud_user_id + provider）
   - 不存在则创建新用户，并关联到用户组
   - 已存在则检查是否在该用户组中
     - 不在则添加用户组关联
     - 已在则跳过
```

## 多云平台示例

### 腾讯云 CAM

```bash
# 同步腾讯云用户组
curl -X POST 'http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=456' \
  -H 'X-Tenant-ID: tenant-001' \
  -H 'Content-Type: application/json'
```

### 华为云 IAM

```bash
# 同步华为云用户组
curl -X POST 'http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=789' \
  -H 'X-Tenant-ID: tenant-001' \
  -H 'Content-Type: application/json'
```

## 定时同步配置

建议配置定时任务，定期同步云平台数据：

```go
// 示例：每天凌晨 2 点同步
func setupSyncSchedule() {
    c := cron.New()
    c.AddFunc("0 2 * * *", func() {
        ctx := context.Background()

        // 获取所有云账号
        accounts, _ := accountRepo.ListAll(ctx)

        for _, account := range accounts {
            // 同步用户组及成员
            result, err := groupService.SyncGroups(ctx, account.ID)
            if err != nil {
                logger.Error("定时同步失败",
                    elog.Int64("account_id", account.ID),
                    elog.FieldErr(err))
                continue
            }

            logger.Info("定时同步完成",
                elog.Int64("account_id", account.ID),
                elog.Int("total_groups", result.TotalGroups),
                elog.Int("synced_members", result.SyncedMembers))
        }
    })
    c.Start()
}
```

## 错误处理

### 常见错误及解决方案

#### 1. 云账号凭证无效

```json
{
  "code": 500,
  "message": "获取云账号失败: account not found"
}
```

**解决**：检查 cloud_account_id 是否正确

#### 2. 权限不足

```json
{
  "code": 500,
  "message": "从云平台获取用户组列表失败: access denied"
}
```

**解决**：检查云账号的 AccessKey 是否有 RAM/CAM 读取权限

#### 3. 部分成员同步失败

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
    "synced_members": 12,
    "failed_members": 3
  }
}
```

**解决**：查看日志了解具体失败原因，通常是数据格式问题或网络超时

## 性能建议

1. **首次同步**：如果用户组和成员数量较多，建议在业务低峰期执行
2. **增量同步**：后续同步会自动识别已存在的数据，速度较快
3. **并发控制**：系统已内置限流器，自动控制 API 调用频率
4. **超时设置**：建议设置合理的超时时间（如 5 分钟）

## 监控指标

建议监控以下指标：

- 同步成功率：`synced_members / total_members`
- 同步耗时：记录每次同步的时间
- 失败次数：`failed_groups + failed_members`
- API 调用次数：监控云平台 API 配额使用情况
