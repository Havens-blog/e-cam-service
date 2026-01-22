# 剩余更新工作清单

## ✅ 已完成 Handler（3/7）

1. ✅ **UserHandler** - 7 个方法全部完成
2. ✅ **GroupHandler** - 7 个方法全部完成
3. ✅ **TemplateHandler** - 6 个方法全部完成

## ⏳ 待完成 Handler（4/7）

### 1. SyncHandler (`internal/cam/iam/web/sync_handler.go`)

需要在每个方法开头添加：

```go
tenantID := middleware.GetTenantID(c)
```

需要更新的方法：

- CreateSyncTask
- GetSyncTask
- ListSyncTasks
- CancelSyncTask

### 2. AuditHandler (`internal/cam/iam/web/audit_handler.go`)

需要更新的方法：

- ListAuditLogs
- GetAuditLog
- GenerateReport
- GetStatistics

### 3. PermissionHandler (`internal/cam/iam/web/permission_handler.go`)

需要更新的方法：

- AssignPermissions
- RevokePermissions
- ListPermissions
- GetUserPermissions

### 4. TenantHandler (`internal/cam/iam/web/tenant_handler.go`)

**特殊处理**：不需要修改（已在 module.go 中配置为不需要 RequireTenant 中间件）

## 📝 Handler 层更新模式

每个方法都按照以下模式更新：

### 步骤 1：添加 import

```go
import (
    // ... 其他 imports
    "github.com/Havens-blog/e-cam-service/internal/cam/middleware"
)
```

### 步骤 2：在方法开头获取租户 ID

```go
func (h *Handler) Method(c *gin.Context) {
    tenantID := middleware.GetTenantID(c)  // 添加这行

    // ... 其他代码
}
```

### 步骤 3：传递租户 ID 到 Service

```go
// 创建操作
result, err := h.service.Create(ctx, &domain.CreateRequest{
    Name:     req.Name,
    TenantID: tenantID,  // 使用中间件提取的租户ID
})

// 查询操作
result, err := h.service.Get(ctx, id, tenantID)  // 添加 tenantID 参数

// 列表查询
results, total, err := h.service.List(ctx, domain.Filter{
    TenantID: tenantID,  // 强制设置租户ID
    // ... 其他过滤条件
})

// 更新操作
err := h.service.Update(ctx, id, tenantID, req)  // 添加 tenantID 参数

// 删除操作
err := h.service.Delete(ctx, id, tenantID)  // 添加 tenantID 参数
```

### 步骤 4：在日志中记录租户 ID

```go
h.logger.Error("操作失败",
    elog.String("tenant_id", tenantID),  // 添加租户ID
    elog.FieldErr(err))
```

### 步骤 5：更新 Swagger 注释

```go
// @Param X-Tenant-ID header string true "租户ID"  // 添加这行
```

## 🔄 下一步：Service 层更新

Handler 层完成后，需要更新 Service 层。

### Service 层更新模式

#### 1. 查询单个资源

```go
// 修改前
func (s *Service) Get(ctx context.Context, id int64) (*Entity, error) {
    return s.repo.FindByID(ctx, id)
}

// 修改后
func (s *Service) Get(ctx context.Context, id int64, tenantID string) (*Entity, error) {
    return s.repo.FindByID(ctx, id, tenantID)
}
```

#### 2. 列表查询

```go
// 修改前
func (s *Service) List(ctx context.Context, filter Filter) ([]*Entity, int64, error) {
    return s.repo.List(ctx, filter)
}

// 修改后
func (s *Service) List(ctx context.Context, filter Filter) ([]*Entity, int64, error) {
    // filter.TenantID 已经在 Handler 层设置
    return s.repo.List(ctx, filter)
}
```

#### 3. 创建资源

```go
// 修改前
func (s *Service) Create(ctx context.Context, req *CreateRequest) (*Entity, error) {
    // req.TenantID 已经在 Handler 层设置
    return s.repo.Create(ctx, req)
}

// 修改后 - 不需要改变签名，因为 TenantID 已经在 req 中
func (s *Service) Create(ctx context.Context, req *CreateRequest) (*Entity, error) {
    // req.TenantID 已经在 Handler 层设置
    return s.repo.Create(ctx, req)
}
```

#### 4. 更新资源

```go
// 修改前
func (s *Service) Update(ctx context.Context, id int64, req *UpdateRequest) error {
    return s.repo.Update(ctx, id, req)
}

// 修改后
func (s *Service) Update(ctx context.Context, id int64, tenantID string, req *UpdateRequest) error {
    // 先验证资源属于该租户
    existing, err := s.repo.FindByID(ctx, id, tenantID)
    if err != nil {
        return err
    }

    return s.repo.Update(ctx, id, req)
}
```

#### 5. 删除资源

```go
// 修改前
func (s *Service) Delete(ctx context.Context, id int64) error {
    return s.repo.Delete(ctx, id)
}

// 修改后
func (s *Service) Delete(ctx context.Context, id int64, tenantID string) error {
    // 先验证资源属于该租户
    _, err := s.repo.FindByID(ctx, id, tenantID)
    if err != nil {
        return err
    }

    return s.repo.Delete(ctx, id, tenantID)
}
```

## 🔄 Repository 层更新

### Repository 层更新模式

```go
// 修改前
func (r *Repository) FindByID(ctx context.Context, id int64) (*Entity, error) {
    return r.dao.FindByID(ctx, id)
}

// 修改后
func (r *Repository) FindByID(ctx context.Context, id int64, tenantID string) (*Entity, error) {
    return r.dao.FindByID(ctx, id, tenantID)
}
```

## 🔄 DAO 层更新

### DAO 层更新模式

```go
// 修改前
func (d *DAO) FindByID(ctx context.Context, id int64) (*Entity, error) {
    filter := bson.M{"id": id}

    var entity Entity
    err := d.col.FindOne(ctx, filter).Decode(&entity)
    return &entity, err
}

// 修改后
func (d *DAO) FindByID(ctx context.Context, id int64, tenantID string) (*Entity, error) {
    filter := bson.M{"id": id}
    filter = WithTenantID(filter, tenantID)  // 添加租户过滤

    var entity Entity
    err := d.col.FindOne(ctx, filter).Decode(&entity)
    return &entity, err
}
```

## 📊 工作量估算

| 层级            | 文件数 | 方法数（估算） | 预计时间      |
| --------------- | ------ | -------------- | ------------- |
| Handler（剩余） | 3      | ~15            | 20 分钟       |
| Service         | 6      | ~40            | 45 分钟       |
| Repository      | 6      | ~30            | 30 分钟       |
| DAO             | 6      | ~30            | 45 分钟       |
| **总计**        | **21** | **~115**       | **~2.5 小时** |

## 🎯 推荐策略

### 策略 A：逐层完成（推荐）

1. 完成所有 Handler 层
2. 完成所有 Service 层
3. 完成所有 Repository 层
4. 完成所有 DAO 层
5. 统一测试

**优点**：

- 每层完成后可以统一测试
- 容易发现模式问题
- 便于批量修改

### 策略 B：垂直完成

1. 完成 User 模块的所有层
2. 完成 Group 模块的所有层
3. 依次完成其他模块

**优点**：

- 每个模块完成后可以立即测试
- 可以逐步上线

## 🚀 快速完成脚本

由于剩余工作量较大，建议使用以下方法加速：

### 方法 1：使用查找替换

在 IDE 中使用正则表达式批量替换：

#### Handler 层

查找：`func \(h \*(\w+)Handler\) (\w+)\(c \*gin\.Context\) \{`
替换：

```go
func (h *$1Handler) $2(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
```

### 方法 2：使用代码生成

创建一个 Go 脚本来自动生成更新后的代码

### 方法 3：继续让 AI 批量更新

继续当前的批量更新流程

## 📝 检查清单

完成每一层后，使用以下清单检查：

### Handler 层检查

- [ ] 所有方法都添加了 `tenantID := middleware.GetTenantID(c)`
- [ ] 所有 Service 调用都传递了 tenantID
- [ ] 所有日志都记录了 tenantID
- [ ] 所有 Swagger 注释都添加了 X-Tenant-ID header

### Service 层检查

- [ ] 所有查询方法都添加了 tenantID 参数
- [ ] 所有更新/删除操作都先验证租户权限
- [ ] 所有创建操作的 req 中都包含 tenantID

### Repository 层检查

- [ ] 所有方法签名都添加了 tenantID 参数
- [ ] 所有调用都传递了 tenantID 到 DAO 层

### DAO 层检查

- [ ] 所有查询都使用了 `WithTenantID(filter, tenantID)`
- [ ] 所有索引都包含 tenant_id 字段

## 🔍 测试验证

完成所有更新后，进行以下测试：

### 1. 编译测试

```bash
go build ./...
```

### 2. 单元测试

```bash
go test ./internal/cam/iam/...
```

### 3. 集成测试

创建两个租户的数据，验证租户隔离

### 4. API 测试

使用不同的租户 ID 调用 API，验证数据隔离

## 📌 注意事项

1. **不要遗漏任何查询**

   - 所有数据库查询都必须包含租户过滤
   - 包括关联查询、统计查询等

2. **批量操作要特别注意**

   - 批量删除、批量更新都要包含租户过滤
   - 防止误操作其他租户的数据

3. **缓存要考虑租户维度**

   - 如果使用了缓存，缓存 key 要包含租户 ID

4. **测试要覆盖租户隔离场景**
   - 测试跨租户访问被拒绝
   - 测试租户数据不会泄露

## 🎉 完成标志

当以下所有项都完成时，多租户架构实施完成：

- [ ] 所有 Handler 方法都已更新
- [ ] 所有 Service 方法都已更新
- [ ] 所有 Repository 方法都已更新
- [ ] 所有 DAO 方法都已更新
- [ ] 所有测试都通过
- [ ] API 测试验证租户隔离
- [ ] 文档已更新
- [ ] 代码已提交
