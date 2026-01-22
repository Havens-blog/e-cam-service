# 多租户架构实施指南

## 已完成的工作

### 1. 基础设施层

- ✅ 创建 `internal/shared/domain/base.go` - 租户资源基础结构
- ✅ 创建 `internal/cam/middleware/tenant.go` - 租户中间件
- ✅ 创建 `internal/cam/iam/repository/tenant_filter.go` - 租户过滤器辅助函数
- ✅ 更新 `internal/cam/iam/module.go` - 集成租户中间件到路由

### 2. 中间件集成

```go
// 在 module.go 中的路由配置
iamGroup := r.Group("/api/v1/cam/iam")
iamGroup.Use(middleware.TenantMiddleware(m.Logger))  // 提取租户ID

// 需要租户ID的路由
tenantRequired := iamGroup.Group("")
tenantRequired.Use(middleware.RequireTenant(m.Logger))  // 验证租户ID必填
```

### 3. Handler 层示例

已更新 `UserHandler.CreateUser` 方法作为示例：

```go
func (h *UserHandler) CreateUser(c *gin.Context) {
    // 1. 从上下文获取租户ID（由中间件注入）
    tenantID := middleware.GetTenantID(c)

    var req CreateUserVO
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, Error(err))
        return
    }

    // 2. 使用中间件提取的租户ID，忽略请求体中的租户ID（安全考虑）
    user, err := h.userService.CreateUser(c.Request.Context(), &domain.CreateCloudUserRequest{
        Username:       req.Username,
        UserType:       req.UserType,
        CloudAccountID: req.CloudAccountID,
        DisplayName:    req.DisplayName,
        Email:          req.Email,
        UserGroups:     req.UserGroups,
        TenantID:       tenantID, // 使用中间件提取的租户ID
    })

    if err != nil {
        h.logger.Error("创建用户失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
        c.JSON(500, Error(err))
        return
    }

    c.JSON(200, Success(user))
}
```

## 待完成的工作

### 1. 更新所有 Handler 方法

需要更新以下 Handler 中的所有方法：

#### UserHandler (`internal/cam/iam/web/user_handler.go`)

- [x] CreateUser - 已完成
- [ ] GetUser - 需要添加租户过滤
- [ ] UpdateUser - 需要添加租户过滤
- [ ] DeleteUser - 需要添加租户过滤
- [ ] ListUsers - 需要添加租户过滤
- [ ] SyncUser - 需要添加租户过滤

#### UserGroupHandler (`internal/cam/iam/web/group_handler.go`)

- [ ] CreateGroup
- [ ] GetGroup
- [ ] UpdateGroup
- [ ] DeleteGroup
- [ ] ListGroups
- [ ] AddUsersToGroup
- [ ] RemoveUsersFromGroup
- [ ] SyncGroup

#### TemplateHandler (`internal/cam/iam/web/template_handler.go`)

- [ ] CreateTemplate
- [ ] GetTemplate
- [ ] UpdateTemplate
- [ ] DeleteTemplate
- [ ] ListTemplates

#### SyncHandler (`internal/cam/iam/web/sync_handler.go`)

- [ ] CreateSyncTask
- [ ] GetSyncTask
- [ ] ListSyncTasks
- [ ] CancelSyncTask

#### AuditHandler (`internal/cam/iam/web/audit_handler.go`)

- [ ] ListAuditLogs
- [ ] GetAuditLog
- [ ] GenerateReport

#### PermissionHandler (`internal/cam/iam/web/permission_handler.go`)

- [ ] AssignPermissions
- [ ] RevokePermissions
- [ ] ListPermissions

### 2. 更新 Service 层

需要确保所有 Service 方法都接受并使用 `tenantID` 参数：

```go
// 示例：更新 Service 方法签名
func (s *CloudUserService) GetUser(ctx context.Context, id int64, tenantID string) (*domain.CloudUser, error) {
    return s.repo.FindByID(ctx, id, tenantID)
}

func (s *CloudUserService) ListUsers(ctx context.Context, filter *domain.CloudUserFilter, tenantID string) ([]*domain.CloudUser, int64, error) {
    filter.TenantID = tenantID  // 强制设置租户ID
    return s.repo.List(ctx, filter)
}
```

### 3. 更新 Repository 层

需要在所有查询中添加租户过滤：

```go
// 示例：Repository 方法
func (r *CloudUserRepository) FindByID(ctx context.Context, id int64, tenantID string) (*domain.CloudUser, error) {
    filter := bson.M{"id": id}
    filter = WithTenantID(filter, tenantID)  // 添加租户过滤

    var user domain.CloudUser
    err := r.col.FindOne(ctx, filter).Decode(&user)
    return &user, err
}

func (r *CloudUserRepository) List(ctx context.Context, filter *domain.CloudUserFilter) ([]*domain.CloudUser, int64, error) {
    // filter.TenantID 已经在 Service 层设置
    mongoFilter := bson.M{}
    if filter.TenantID != "" {
        mongoFilter["tenant_id"] = filter.TenantID
    }
    // ... 其他过滤条件
}
```

### 4. 更新 VO 结构体

移除 VO 中的 `TenantID` 字段（因为从中间件获取）：

```go
// 修改前
type CreateUserVO struct {
    Username       string `json:"username" binding:"required"`
    TenantID       string `json:"tenant_id" binding:"required"`  // 移除这个
}

// 修改后
type CreateUserVO struct {
    Username       string `json:"username" binding:"required"`
    // TenantID 从中间件获取，不需要在请求体中
}
```

## 实施步骤

### 步骤 1: 更新 Handler 层（批量）

对于每个 Handler 方法，按照以下模式更新：

```go
func (h *Handler) Method(c *gin.Context) {
    // 1. 获取租户ID
    tenantID := middleware.GetTenantID(c)

    // 2. 解析请求参数
    var req RequestVO
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, Error(err))
        return
    }

    // 3. 调用 Service，传入租户ID
    result, err := h.service.Method(c.Request.Context(), req, tenantID)
    if err != nil {
        h.logger.Error("操作失败",
            elog.String("tenant_id", tenantID),
            elog.FieldErr(err))
        c.JSON(500, Error(err))
        return
    }

    c.JSON(200, Success(result))
}
```

### 步骤 2: 更新 Service 层（批量）

```go
// 添加 tenantID 参数到所有方法
func (s *Service) Method(ctx context.Context, req *Request, tenantID string) (*Result, error) {
    // 设置租户ID
    req.TenantID = tenantID

    // 调用 Repository
    return s.repo.Method(ctx, req)
}

// 对于查询方法，强制设置租户过滤
func (s *Service) List(ctx context.Context, filter *Filter, tenantID string) ([]*Result, int64, error) {
    filter.TenantID = tenantID  // 强制设置
    return s.repo.List(ctx, filter)
}
```

### 步骤 3: 更新 Repository 层（批量）

```go
// 在所有查询中使用租户过滤器
func (r *Repository) FindByID(ctx context.Context, id int64, tenantID string) (*Entity, error) {
    filter := bson.M{"id": id}
    filter = WithTenantID(filter, tenantID)  // 使用辅助函数

    var entity Entity
    err := r.col.FindOne(ctx, filter).Decode(&entity)
    return &entity, err
}
```

### 步骤 4: 更新 VO 定义（批量）

```go
// 移除所有 VO 中的 TenantID 字段
// 因为租户ID从中间件获取，不应该由客户端提供

// 修改前
type CreateVO struct {
    Name     string `json:"name" binding:"required"`
    TenantID string `json:"tenant_id" binding:"required"`  // 删除
}

// 修改后
type CreateVO struct {
    Name string `json:"name" binding:"required"`
}
```

## 测试清单

### 1. 单元测试

- [ ] 测试中间件正确提取租户 ID
- [ ] 测试 RequireTenant 中间件拒绝无租户 ID 的请求
- [ ] 测试 Service 层正确传递租户 ID
- [ ] 测试 Repository 层正确过滤租户数据

### 2. 集成测试

- [ ] 测试跨租户数据隔离
- [ ] 测试租户 A 无法访问租户 B 的数据
- [ ] 测试租户 ID 从请求头正确传递到数据库查询

### 3. API 测试

```bash
# 测试创建用户（带租户ID）
curl -X POST http://localhost:8098/api/v1/cam/iam/users \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: tenant-001" \
  -d '{
    "username": "test-user",
    "user_type": "iam_user",
    "cloud_account_id": 1
  }'

# 测试查询用户（验证租户隔离）
curl -X GET http://localhost:8098/api/v1/cam/iam/users/1 \
  -H "X-Tenant-ID: tenant-001"

# 测试跨租户访问（应该失败）
curl -X GET http://localhost:8098/api/v1/cam/iam/users/1 \
  -H "X-Tenant-ID: tenant-002"
```

## 安全注意事项

### 1. 永远不要信任客户端提供的租户 ID

```go
// ❌ 错误：使用请求体中的租户ID
tenantID := req.TenantID

// ✅ 正确：使用中间件提取的租户ID
tenantID := middleware.GetTenantID(c)
```

### 2. 所有查询必须包含租户过滤

```go
// ❌ 错误：没有租户过滤
filter := bson.M{"id": id}

// ✅ 正确：包含租户过滤
filter := bson.M{"id": id, "tenant_id": tenantID}
```

### 3. 审计日志必须记录租户 ID

```go
h.logger.Error("操作失败",
    elog.String("tenant_id", tenantID),  // 必须记录
    elog.Int64("user_id", userID),
    elog.FieldErr(err))
```

## 性能优化建议

### 1. 确保索引包含租户 ID

```javascript
// 所有查询索引都应该以 tenant_id 开头
db.cloud_iam_users.createIndex({ tenant_id: 1, id: 1 });
db.cloud_iam_users.createIndex({ tenant_id: 1, username: 1 });
db.cloud_iam_users.createIndex({ tenant_id: 1, status: 1, ctime: -1 });
```

### 2. 使用复合索引优化查询

```go
// 查询时使用与索引匹配的字段顺序
filter := bson.M{
    "tenant_id": tenantID,  // 第一个字段
    "status": "active",      // 第二个字段
}
opts := options.Find().SetSort(bson.D{
    {Key: "tenant_id", Value: 1},
    {Key: "ctime", Value: -1},
})
```

## 下一步行动

1. **立即执行**：

   - 更新所有 Handler 方法使用 `middleware.GetTenantID(c)`
   - 更新所有 Service 方法签名添加 `tenantID` 参数
   - 更新所有 Repository 查询添加租户过滤

2. **测试验证**：

   - 编写单元测试验证租户隔离
   - 编写集成测试验证跨租户访问被拒绝
   - 手动测试 API 确保租户 ID 正确传递

3. **文档更新**：

   - 更新 API 文档，添加 `X-Tenant-ID` 请求头说明
   - 更新开发文档，说明多租户架构使用方法

4. **监控告警**：
   - 添加租户级别的监控指标
   - 设置跨租户访问尝试的告警
