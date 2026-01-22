# 测试云账号 Tenant ID 更新功能

## 测试目的

验证云账号更新时 `tenant_id` 字段能够正确传递和保存。

## 修复内容

### 1. Web 层 VO

**文件**: `internal/cam/web/vo.go`

添加 `TenantID` 字段：

```go
type UpdateCloudAccountReq struct {
    // ... 其他字段
    TenantID *string `json:"tenant_id,omitempty"` // 新增
}
```

### 2. Handler 层

**文件**: `internal/cam/web/handler.go`

传递 `TenantID` 到 domain 层：

```go
domainReq := &domain.UpdateCloudAccountRequest{
    // ... 其他字段
    TenantID: req.TenantID, // 新增
}
```

### 3. Service 层

**文件**: `internal/cam/service/account.go`

更新 `TenantID`：

```go
if req.TenantID != nil {
    account.TenantID = *req.TenantID
}
```

### 4. Domain 层

**文件**: `internal/shared/domain/account.go`

添加 `TenantID` 字段：

```go
type UpdateCloudAccountRequest struct {
    // ... 其他字段
    TenantID *string `json:"tenant_id,omitempty"` // 新增
}
```

## 测试步骤

### 步骤 1: 查看当前云账号信息

```bash
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json"
```

**预期响应**:

```json
{
  "code": 0,
  "data": {
    "id": 1,
    "name": "阿里云账号",
    "tenant_id": "old-tenant-id",
    ...
  }
}
```

### 步骤 2: 更新 tenant_id

```bash
curl -X PUT http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "JLC"
  }'
```

**预期响应**:

```json
{
  "code": 0,
  "message": "success"
}
```

### 步骤 3: 验证更新结果

```bash
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: JLC" \
  -H "Content-Type: application/json"
```

**预期响应**:

```json
{
  "code": 0,
  "data": {
    "id": 1,
    "name": "阿里云账号",
    "tenant_id": "JLC",  // 已更新
    ...
  }
}
```

### 步骤 4: 同时更新多个字段

```bash
curl -X PUT http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: JLC" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "阿里云生产账号",
    "description": "生产环境",
    "tenant_id": "JLC",
    "regions": ["cn-hangzhou", "cn-beijing"]
  }'
```

**预期响应**:

```json
{
  "code": 0,
  "message": "success"
}
```

## 调试方法

### 1. 检查请求体是否正确

在 handler 中添加日志：

```go
func (h *Handler) UpdateCloudAccount(ctx *gin.Context, req UpdateCloudAccountReq) (ginx.Result, error) {
    h.logger.Info("收到更新请求",
        elog.Any("tenant_id", req.TenantID),
        elog.Any("name", req.Name))
    // ... 其他代码
}
```

### 2. 检查 domain 层是否接收到

在 service 中添加日志：

```go
func (s *cloudAccountService) UpdateAccount(ctx context.Context, id int64, req *domain.UpdateCloudAccountRequest) error {
    s.logger.Info("更新云账号",
        elog.Int64("id", id),
        elog.Any("tenant_id", req.TenantID))
    // ... 其他代码
}
```

### 3. 检查数据库是否更新

```bash
# 连接 MongoDB
mongo mongodb://admin:password@localhost:27017/e_cam_service

# 查询云账号
db.cloud_accounts.findOne({id: 1}, {id: 1, name: 1, tenant_id: 1})
```

## 常见问题

### Q1: 请求体中有 tenant_id，但 req.TenantID 为 nil

**原因**: JSON 标签不匹配

**检查**:

```go
// VO 定义
TenantID *string `json:"tenant_id,omitempty"` // 确保是 tenant_id

// 请求体
{
  "tenant_id": "JLC"  // 确保是 tenant_id，不是 tenantId
}
```

### Q2: Service 层收到了 TenantID，但数据库没更新

**原因**: Service 层没有处理该字段

**检查**:

```go
// 确保有这段代码
if req.TenantID != nil {
    account.TenantID = *req.TenantID
}
```

### Q3: 更新后查询不到数据

**原因**: 查询时使用的 X-Tenant-ID 与更新后的 tenant_id 不匹配

**解决**:

```bash
# 使用新的 tenant_id 查询
curl -X GET http://localhost:8080/api/v1/cam/cloud-accounts/1 \
  -H "X-Tenant-ID: JLC"  # 使用更新后的值
```

## 验证清单

- [ ] Web 层 VO 添加了 `TenantID` 字段
- [ ] Handler 层传递了 `TenantID` 到 domain 层
- [ ] Service 层处理了 `TenantID` 更新
- [ ] Domain 层定义了 `TenantID` 字段
- [ ] 请求体中的 JSON 字段名正确（`tenant_id`）
- [ ] 更新后能够查询到正确的数据
- [ ] 日志中能看到 `TenantID` 的值

## 完整测试脚本

```bash
#!/bin/bash

API_BASE="http://localhost:8080/api/v1/cam"
ACCOUNT_ID=1
OLD_TENANT="tenant-001"
NEW_TENANT="JLC"

echo "=== 测试云账号 Tenant ID 更新 ==="
echo ""

# 1. 查看当前信息
echo "1. 查看当前云账号信息..."
curl -s -X GET "$API_BASE/cloud-accounts/$ACCOUNT_ID" \
  -H "X-Tenant-ID: $OLD_TENANT" | jq '.data.tenant_id'
echo ""

# 2. 更新 tenant_id
echo "2. 更新 tenant_id 为 $NEW_TENANT..."
curl -s -X PUT "$API_BASE/cloud-accounts/$ACCOUNT_ID" \
  -H "X-Tenant-ID: $OLD_TENANT" \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\": \"$NEW_TENANT\"}" | jq '.'
echo ""

# 3. 验证更新
echo "3. 验证更新结果..."
curl -s -X GET "$API_BASE/cloud-accounts/$ACCOUNT_ID" \
  -H "X-Tenant-ID: $NEW_TENANT" | jq '.data.tenant_id'
echo ""

echo "=== 测试完成 ==="
```

## 总结

通过以上修复，现在可以正确更新云账号的 `tenant_id` 字段了。修复涉及 4 个层次：

1. ✅ Web 层 VO
2. ✅ Handler 层
3. ✅ Service 层
4. ✅ Domain 层

所有层次都已正确传递和处理 `tenant_id` 字段。
