# IAM 用户组成员同步 - 快速开始

## 🚀 5 分钟快速上手

### 步骤 1：准备云账号

确保你已经配置了云账号信息：

```bash
# 查看云账号列表
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts \
  -H "X-Tenant-ID: tenant-001"

# 如果没有，先创建云账号
curl -X POST http://localhost:8080/api/v1/cam/cloud-accounts \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "阿里云账号",
    "provider": "aliyun",
    "access_key": "your-access-key",
    "secret_key": "your-secret-key",
    "region": "cn-hangzhou"
  }'
```

### 步骤 2：执行同步

```bash
# 同步用户组及成员（假设云账号 ID 为 1）
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001"
```

### 步骤 3：查看结果

```bash
# 查看用户组列表
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups" \
  -H "X-Tenant-ID: tenant-001"

# 查看用户列表
curl -X GET "http://localhost:8080/api/v1/cam/iam/users" \
  -H "X-Tenant-ID: tenant-001"
```

## 📊 同步结果示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_groups": 5, // 发现 5 个用户组
    "created_groups": 2, // 新创建 2 个
    "updated_groups": 3, // 更新 3 个
    "failed_groups": 0, // 0 个失败
    "total_members": 15, // 发现 15 个成员
    "synced_members": 14, // 成功同步 14 个
    "failed_members": 1 // 1 个失败
  }
}
```

## 🧪 使用测试脚本

```bash
# 运行自动化测试
cd scripts
go run test_group_member_sync.go

# 使用自定义配置
export API_BASE_URL="http://localhost:8080"
export TENANT_ID="tenant-001"
export CLOUD_ACCOUNT_ID="1"
go run test_group_member_sync.go
```

## 🔄 定时同步

建议配置定时任务，每天自动同步：

```bash
# Linux crontab
0 2 * * * curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" -H "X-Tenant-ID: tenant-001"
```

## 📖 更多文档

- [完整功能文档](USER_GROUP_MEMBER_SYNC.md)
- [使用示例](examples/sync_user_groups_example.md)
- [测试脚本说明](../scripts/README_GROUP_SYNC_TEST.md)

## ❓ 常见问题

**Q: 同步需要多长时间？**
A: 取决于用户组和成员数量，通常 5-10 个用户组需要 10-30 秒。

**Q: 会删除本地已有的数据吗？**
A: 不会，同步只会创建和更新，不会删除。

**Q: 支持哪些云平台？**
A: 目前支持阿里云 RAM 和腾讯云 CAM，其他平台正在开发中。

**Q: 同步失败怎么办？**
A: 查看日志了解具体原因，通常是权限不足或网络问题。
