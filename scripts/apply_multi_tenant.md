# 多租户架构应用脚本

## 快速参考：需要修改的模式

### 1. Handler 层修改模式

#### 修改前：

```go
func (h *UserHandler) CreateUser(c *gin.Context) {
    var req CreateUserVO
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, Error(err))
        return
    }

    user, err := h.userService.CreateUser(c.Request.Context(), &domain.CreateCloudUserRequest{
        Username:   req.Username,
        TenantID:   req.TenantID,  // 从请求体获取
    })
}
```

#### 修改后：

```go
func (h *UserHandler) CreateUser(c *gin.Context) {
    tenantID := middleware.GetTenantID(c)  // 从中间件获取

    var req CreateUserVO
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, Error(err))
        return
    }

    user, err := h.userService.CreateUser(c.Request.Context(), &domain.CreateCloudUserRequest{
        Username:   req.Username,
        TenantID:   tenantID,  // 使用中间件提取的
    })
}
```

### 2. 查询方法修改模式

#### 修改前：

```go
func (h *UserHandler) GetUser(c *gin.Context) {
    id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
    user, err := h.userService.GetUser(c.Request.Context(), id)
}
```

#### 修改后：

```go
func (h *UserHandler) GetUser(c *gin.Context) {
    tenantID := middleware.GetTenantID(c)  // 添加这行
    id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
    user, err := h.userService.GetUser(c.Request.Context(), id, tenantID)  // 传递租户ID
}
```

### 3. 列表查询修改模式

#### 修改前：

```go
func (h *UserHandler) ListUsers(c *gin.Context) {
    var filter domain.CloudUserFilter
    // ... 解析过滤条件
    users, total, err := h.userService.ListUsers(c.Request.Context(), &filter)
}
```

#### 修改后：

```go
func (h *UserHandler) ListUsers(c *gin.Context) {
    tenantID := middleware.GetTenantID(c)  // 添加这行

    var filter domain.CloudUserFilter
    // ... 解析过滤条件
    filter.TenantID = tenantID  // 强制设置租户ID

    users, total, err := h.userService.ListUsers(c.Request.Context(), &filter)
}
```

## 需要修改的文件清单

### Handler 文件（7 个）

1. `internal/cam/iam/web/user_handler.go` - ✅ 部分完成
2. `internal/cam/iam/web/group_handler.go`
3. `internal/cam/iam/web/template_handler.go`
4. `internal/cam/iam/web/sync_handler.go`
5. `internal/cam/iam/web/audit_handler.go`
6. `internal/cam/iam/web/permission_handler.go`
7. `internal/cam/iam/web/tenant_handler.go` - 特殊处理（管理租户本身）

### Service 文件（7 个）

1. `internal/cam/iam/service/user.go`
2. `internal/cam/iam/service/group.go`
3. `internal/cam/iam/service/template.go`
4. `internal/cam/iam/service/sync.go`
5. `internal/cam/iam/service/audit.go`
6. `internal/cam/iam/service/permission.go`
7. `internal/cam/iam/service/tenant.go`

### Repository 文件（7 个）

1. `internal/cam/iam/repository/user.go`
2. `internal/cam/iam/repository/group.go`
3. `internal/cam/iam/repository/template.go`
4. `internal/cam/iam/repository/sync.go`
5. `internal/cam/iam/repository/audit.go`
6. `internal/cam/iam/repository/tenant.go`

### DAO 文件（7 个）

1. `internal/cam/iam/repository/dao/user.go`
2. `internal/cam/iam/repository/dao/group.go`
3. `internal/cam/iam/repository/dao/template.go`
4. `internal/cam/iam/repository/dao/sync.go`
5. `internal/cam/iam/repository/dao/audit.go`
6. `internal/cam/iam/repository/dao/tenant.go`

## 修改优先级

### 高优先级（核心功能）

1. UserHandler - 用户管理
2. GroupHandler - 用户组管理
3. PermissionHandler - 权限管理

### 中优先级（支持功能）

4. TemplateHandler - 策略模板
5. SyncHandler - 同步任务
6. AuditHandler - 审计日志

### 低优先级（管理功能）

7. TenantHandler - 租户管理（特殊处理）

## 测试命令

### 1. 测试租户隔离

```bash
# 创建租户1的用户
curl -X POST http://localhost:8098/api/v1/cam/iam/users \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: tenant-001" \
  -d '{
    "username": "user1",
    "user_type": "iam_user",
    "cloud_account_id": 1
  }'

# 创建租户2的用户
curl -X POST http://localhost:8098/api/v1/cam/iam/users \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: tenant-002" \
  -d '{
    "username": "user2",
    "user_type": "iam_user",
    "cloud_account_id": 2
  }'

# 租户1查询用户（应该只看到 user1）
curl -X GET http://localhost:8098/api/v1/cam/iam/users \
  -H "X-Tenant-ID: tenant-001"

# 租户2查询用户（应该只看到 user2）
curl -X GET http://localhost:8098/api/v1/cam/iam/users \
  -H "X-Tenant-ID: tenant-002"
```

### 2. 测试跨租户访问

```bash
# 租户1尝试访问租户2的用户（应该返回404或403）
curl -X GET http://localhost:8098/api/v1/cam/iam/users/2 \
  -H "X-Tenant-ID: tenant-001"
```

### 3. 测试缺少租户 ID

```bash
# 不提供租户ID（应该返回400）
curl -X GET http://localhost:8098/api/v1/cam/iam/users
```

## 常见问题

### Q1: 如何处理超级管理员？

A: 在 Service 层添加超级管理员判断：

```go
func (s *Service) GetUser(ctx context.Context, id int64, tenantID string, isSuperAdmin bool) (*domain.CloudUser, error) {
    if isSuperAdmin {
        // 超级管理员可以跨租户查询
        return s.repo.FindByID(ctx, id, "")
    }
    return s.repo.FindByID(ctx, id, tenantID)
}
```

### Q2: 租户管理接口如何处理？

A: 租户管理接口不需要租户 ID 验证（因为是管理租户本身）：

```go
// 在 module.go 中
iamGroup := r.Group("/api/v1/cam/iam")
iamGroup.Use(middleware.TenantMiddleware(m.Logger))  // 只提取，不验证

// 租户管理路由（不需要 RequireTenant）
m.TenantHandler.RegisterRoutes(iamGroup)

// 其他路由（需要 RequireTenant）
tenantRequired := iamGroup.Group("")
tenantRequired.Use(middleware.RequireTenant(m.Logger))
{
    m.UserHandler.RegisterRoutes(tenantRequired)
    // ...
}
```

### Q3: 如何处理批量操作？

A: 批量操作也必须包含租户过滤：

```go
func (s *Service) BatchDelete(ctx context.Context, ids []int64, tenantID string) error {
    // 确保所有ID都属于当前租户
    filter := bson.M{
        "id": bson.M{"$in": ids},
        "tenant_id": tenantID,  // 必须包含
    }
    result, err := s.col.DeleteMany(ctx, filter)
    return err
}
```

### Q4: 如何处理关联查询？

A: 关联查询也要确保租户隔离：

```go
// 查询用户及其所属用户组
func (s *Service) GetUserWithGroups(ctx context.Context, userID int64, tenantID string) (*UserWithGroups, error) {
    // 1. 查询用户（带租户过滤）
    user, err := s.userRepo.FindByID(ctx, userID, tenantID)
    if err != nil {
        return nil, err
    }

    // 2. 查询用户组（也要带租户过滤）
    groups, err := s.groupRepo.FindByIDs(ctx, user.UserGroups, tenantID)
    if err != nil {
        return nil, err
    }

    return &UserWithGroups{User: user, Groups: groups}, nil
}
```

## 回滚计划

如果出现问题，可以按以下步骤回滚：

1. 移除中间件：

```go
// 在 module.go 中注释掉中间件
// iamGroup.Use(middleware.TenantMiddleware(m.Logger))
// tenantRequired.Use(middleware.RequireTenant(m.Logger))
```

2. 恢复 Handler 方法：

```go
// 使用请求体中的租户ID
tenantID := req.TenantID
```

3. 数据库索引保持不变（已经优化过）

## 进度跟踪

使用以下清单跟踪进度：

- [x] 基础设施搭建

  - [x] 创建 base.go
  - [x] 创建 tenant.go 中间件
  - [x] 创建 tenant_filter.go
  - [x] 更新 module.go

- [ ] Handler 层更新

  - [x] UserHandler.CreateUser
  - [ ] UserHandler 其他方法
  - [ ] GroupHandler
  - [ ] TemplateHandler
  - [ ] SyncHandler
  - [ ] AuditHandler
  - [ ] PermissionHandler

- [ ] Service 层更新

  - [ ] UserService
  - [ ] GroupService
  - [ ] TemplateService
  - [ ] SyncService
  - [ ] AuditService
  - [ ] PermissionService

- [ ] Repository 层更新

  - [ ] UserRepository
  - [ ] GroupRepository
  - [ ] TemplateRepository
  - [ ] SyncRepository
  - [ ] AuditRepository

- [ ] 测试

  - [ ] 单元测试
  - [ ] 集成测试
  - [ ] API 测试

- [ ] 文档
  - [x] 架构设计文档
  - [x] 实施指南
  - [ ] API 文档更新
