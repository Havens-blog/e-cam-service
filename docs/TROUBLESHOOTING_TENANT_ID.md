# Tenant ID 问题排查和修复指南

## 问题描述

查询用户列表时返回空数据，可能的原因是：

1. 数据库中用户的 `tenant_id` 字段与租户集合中的 `_id` 不匹配
2. 云账号的 `tenant_id` 设置不正确，导致同步时使用了错误的租户 ID
3. 请求头中的 `X-Tenant-ID` 与数据库中的 `tenant_id` 不匹配

## 租户 ID 的正确使用

### 租户表结构

```javascript
// MongoDB collection: tenants
{
  "_id": "tenant-001",        // 这是租户的唯一标识
  "name": "测试租户",
  "display_name": "测试租户",
  // ... 其他字段
}
```

### 云账号表结构

```javascript
// MongoDB collection: cloud_accounts
{
  "id": 1,
  "name": "阿里云账号",
  "tenant_id": "tenant-001",  // 必须与租户的 _id 匹配
  // ... 其他字段
}
```

### 用户表结构

```javascript
// MongoDB collection: cloud_iam_users
{
  "id": 1,
  "username": "test-user",
  "tenant_id": "tenant-001",  // 必须与租户的 _id 匹配
  // ... 其他字段
}
```

## 排查步骤

### 步骤 1：检查租户数据

```bash
# 连接 MongoDB
mongo mongodb://admin:password@localhost:27017/e_cam_service

# 查看租户列表
db.tenants.find({}, {_id: 1, name: 1})

# 输出示例：
# { "_id" : "tenant-001", "name" : "测试租户" }
```

### 步骤 2：检查云账号的 tenant_id

```bash
# 查看云账号的 tenant_id
db.cloud_accounts.find({}, {id: 1, name: 1, tenant_id: 1})

# 输出示例：
# { "id" : 1, "name" : "阿里云账号", "tenant_id" : "tenant-001" }
```

**检查点**：

- `tenant_id` 是否与租户的 `_id` 匹配？
- 如果不匹配，需要修复

### 步骤 3：检查用户的 tenant_id

```bash
# 统计各个 tenant_id 的用户数
db.cloud_iam_users.aggregate([
  { $group: { _id: "$tenant_id", count: { $sum: 1 } } },
  { $sort: { count: -1 } }
])

# 输出示例：
# { "_id" : "tenant-001", "count" : 10 }
# { "_id" : "", "count" : 5 }  // 空的 tenant_id，需要修复
```

### 步骤 4：检查 API 请求

```bash
# 测试用户列表 API
curl -X GET "http://localhost:8080/api/v1/cam/iam/users?page=1&size=10" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json"
```

**检查点**：

- 请求头中的 `X-Tenant-ID` 是否与数据库中的 `tenant_id` 匹配？
- 如果不匹配，修改请求头或修复数据库数据

## 自动修复

### 使用修复脚本

我们提供了一个自动修复脚本，可以检查并修复所有 tenant_id 问题：

```bash
# 运行修复脚本
go run scripts/fix_tenant_id.go

# 使用自定义 MongoDB 连接
export MONGO_URI="mongodb://admin:password@localhost:27017"
export MONGO_DATABASE="e_cam_service"
go run scripts/fix_tenant_id.go
```

### 脚本功能

1. **检查租户数据**：列出所有租户及其 ID
2. **检查云账号**：验证云账号的 tenant_id 是否有效
3. **修复云账号**：将无效的 tenant_id 更新为默认租户
4. **检查用户**：统计各个 tenant_id 的用户数
5. **修复用户**：批量更新无效的 tenant_id
6. **检查用户组**：验证用户组的 tenant_id
7. **修复用户组**：批量更新无效的 tenant_id

### 输出示例

```
=== 修复 Tenant ID 问题 ===
MongoDB URI: mongodb://admin:password@localhost:27017
Database: e_cam_service

✓ MongoDB 连接成功

步骤 1: 检查租户集合...
  总租户数: 1
  租户列表:
    1. ID: tenant-001, 名称: 测试租户

步骤 2: 检查云账号的 tenant_id...
  总云账号数: 2
  云账号列表:
    1. ID: 1, 名称: 阿里云账号, TenantID: tenant-001 ✓
    2. ID: 2, 名称: 腾讯云账号, TenantID:  ❌ (无效)

步骤 3: 修复 1 个无效的 tenant_id...
  使用默认租户: tenant-001 (测试租户)
  ✓ 更新云账号 2 的 tenant_id:  -> tenant-001

步骤 4: 检查用户的 tenant_id...
  总用户数: 15
  用户按 tenant_id 分布:
    1. TenantID: tenant-001 - 用户数: 10 ✓
    2. TenantID: <空> - 用户数: 5 ❌ (无效)

步骤 5: 修复 5 个用户的 tenant_id...
  使用默认租户: tenant-001 (测试租户)
  ✓ 成功更新 5 个用户的 tenant_id

=== 修复完成 ===

建议:
  1. 重新查询用户列表，应该能看到数据了
  2. 如果还有问题，检查请求头中的 X-Tenant-ID 是否正确
```

## 手动修复

如果不想使用脚本，可以手动修复：

### 修复云账号

```javascript
// 查找无效的云账号
db.cloud_accounts.find({ tenant_id: { $nin: ["tenant-001"] } });

// 批量更新
db.cloud_accounts.updateMany(
  { tenant_id: { $nin: ["tenant-001"] } },
  { $set: { tenant_id: "tenant-001" } }
);
```

### 修复用户

```javascript
// 查找无效的用户
db.cloud_iam_users.find({ tenant_id: { $nin: ["tenant-001"] } });

// 批量更新
db.cloud_iam_users.updateMany(
  { tenant_id: { $nin: ["tenant-001"] } },
  { $set: { tenant_id: "tenant-001" } }
);
```

### 修复用户组

```javascript
// 查找无效的用户组
db.cloud_iam_groups.find({ tenant_id: { $nin: ["tenant-001"] } });

// 批量更新
db.cloud_iam_groups.updateMany(
  { tenant_id: { $nin: ["tenant-001"] } },
  { $set: { tenant_id: "tenant-001" } }
);
```

## 预防措施

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

### 2. 同步前确认云账号的 tenant_id

```bash
# 查询云账号详情
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001"

# 确认返回的 tenant_id 是否正确
```

### 3. 使用正确的请求头

所有 IAM 相关的 API 请求都必须包含 `X-Tenant-ID` 请求头：

```bash
# 正确的请求
curl -X GET "http://localhost:8080/api/v1/cam/iam/users" \
  -H "X-Tenant-ID: tenant-001"

# 错误的请求（缺少 X-Tenant-ID）
curl -X GET "http://localhost:8080/api/v1/cam/iam/users"
```

## 常见问题

### Q1: 为什么查询用户列表返回空？

**A**: 可能的原因：

1. 数据库中没有用户数据（还没有执行同步）
2. 用户的 `tenant_id` 与请求头中的 `X-Tenant-ID` 不匹配
3. 云账号的 `tenant_id` 设置不正确

**解决方案**：

1. 运行修复脚本：`go run scripts/fix_tenant_id.go`
2. 确认请求头中的 `X-Tenant-ID` 正确
3. 重新执行用户组同步

### Q2: 同步后用户的 tenant_id 为什么不对？

**A**: 同步时使用的是云账号的 `tenant_id`，如果云账号的 `tenant_id` 不正确，同步的用户也会有问题。

**解决方案**：

1. 先修复云账号的 `tenant_id`
2. 运行修复脚本修复已同步的用户
3. 或者重新执行同步

### Q3: 如何查看当前使用的 tenant_id？

**A**:

```bash
# 方法 1：查看日志
# 日志中会显示：tenant context set tenant_id=tenant-001

# 方法 2：在 API 响应中添加调试信息
# 可以临时修改代码，在响应中返回 tenant_id
```

### Q4: 多租户环境下如何管理？

**A**:

1. 每个租户使用独立的 `tenant_id`（租户的 `_id`）
2. 云账号必须关联到正确的租户
3. 所有 API 请求必须携带正确的 `X-Tenant-ID`
4. 定期检查数据一致性

## 验证修复结果

修复完成后，执行以下验证：

```bash
# 1. 查询用户列表
curl -X GET "http://localhost:8080/api/v1/cam/iam/users?page=1&size=10" \
  -H "X-Tenant-ID: tenant-001" | jq '.'

# 2. 查询用户组列表
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups?page=1&size=10" \
  -H "X-Tenant-ID: tenant-001" | jq '.'

# 3. 检查返回的数据是否正确
# 应该能看到用户和用户组数据
```

## 联系支持

如果问题仍未解决，请提供以下信息：

1. MongoDB 中租户、云账号、用户的数据示例
2. API 请求的完整 curl 命令
3. 服务日志中的相关错误信息
4. 修复脚本的输出结果
