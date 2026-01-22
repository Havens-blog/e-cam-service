# 云账号 Tenant ID 更新功能修复

## 问题描述

在更新云账号时，`tenant_id` 字段无法被更新，导致：

1. 云账号的租户归属无法修改
2. 同步用户时会使用错误的 `tenant_id`
3. 多租户环境下数据隔离出现问题

## 修复内容

### 1. 添加 TenantID 字段到更新请求

**文件**: `internal/shared/domain/account.go`

```go
type UpdateCloudAccountRequest struct {
	Name            *string             `json:"name,omitempty"`
	Environment     *Environment        `json:"environment,omitempty"`
	AccessKeyID     *string             `json:"access_key_id,omitempty"`
	AccessKeySecret *string             `json:"access_key_secret,omitempty"`
	Regions         []string            `json:"regions,omitempty"`
	Description     *string             `json:"description,omitempty"`
	Config          *CloudAccountConfig `json:"config,omitempty"`
	TenantID        *string             `json:"tenant_id,omitempty"` // 新增
}
```

### 2. 更新服务层逻辑

**文件**: `internal/cam/service/account.go`

```go
// UpdateAccount 方法中添加
if req.TenantID != nil {
	account.TenantID = *req.TenantID
}
```

## 使用方法

### API 调用示例

```bash
# 更新云账号的 tenant_id
curl -X PUT http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-001"
  }'
```

### 完整更新示例

```bash
curl -X PUT http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "阿里云生产账号",
    "description": "生产环境阿里云账号",
    "tenant_id": "tenant-001",
    "regions": ["cn-hangzhou", "cn-beijing"]
  }'
```

## 验证修复

### 步骤 1: 查看云账号当前的 tenant_id

```bash
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001"
```

### 步骤 2: 更新 tenant_id

```bash
curl -X PUT http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-002"
  }'
```

### 步骤 3: 验证更新结果

```bash
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-002"
```

## 注意事项

### 1. 租户 ID 必须存在

更新前确保目标租户 ID 在租户表中存在：

```bash
# 查询租户列表
curl -X GET http://localhost:8080/api/v1/cam/iam/tenants
```

### 2. 影响范围

更新云账号的 `tenant_id` 后：

- **不会自动更新**已同步的用户和用户组的 `tenant_id`
- 需要手动修复或重新同步

### 3. 修复已同步的数据

如果已经同步了用户和用户组，需要运行修复脚本：

```bash
# 方法 1: 使用自动修复脚本
go run scripts/fix_tenant_id.go

# 方法 2: 手动更新 MongoDB
mongo mongodb://admin:password@localhost:27017/e_cam_service
db.cloud_iam_users.updateMany(
  { cloud_account_id: 1 },
  { $set: { tenant_id: "tenant-001" } }
)
db.cloud_iam_groups.updateMany(
  { cloud_account_id: 1 },
  { $set: { tenant_id: "tenant-001" } }
)
```

### 4. 重新同步

更新 `tenant_id` 后，建议重新同步用户组：

```bash
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001"
```

## 最佳实践

### 1. 创建云账号时指定正确的 tenant_id

```bash
curl -X POST http://localhost:8080/api/v1/cam/cloud-accounts \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "阿里云账号",
    "provider": "aliyun",
    "access_key": "your-access-key",
    "secret_key": "your-secret-key",
    "region": "cn-hangzhou",
    "tenant_id": "tenant-001"
  }'
```

### 2. 定期检查 tenant_id 一致性

```bash
# 使用快速检查脚本
bash scripts/quick_check_tenant.sh
```

### 3. 同步前验证 tenant_id

```bash
# 1. 查看云账号的 tenant_id
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001"

# 2. 确认 tenant_id 正确后再同步
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001"
```

## 相关文档

- [Tenant ID 问题排查指南](TROUBLESHOOTING_TENANT_ID.md)
- [用户组成员同步功能](USER_GROUP_MEMBER_SYNC.md)
- [API 文档](API-DOCUMENTATION.md)

## 更新日志

- **2025-11-23**: 添加 `tenant_id` 字段到云账号更新请求
- **2025-11-23**: 更新服务层逻辑支持 `tenant_id` 更新
