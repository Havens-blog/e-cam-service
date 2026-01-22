# 多租户架构设计方案

## 1. 设计原则

### 1.1 最小侵入原则

- 不是所有模型都需要 `tenant_id`
- 只有需要租户隔离的业务资源才添加
- 系统级配置和全局数据不需要

### 1.2 分层隔离原则

```
┌─────────────────────────────────────┐
│  API Layer (Middleware)             │  ← 提取租户ID
├─────────────────────────────────────┤
│  Service Layer                      │  ← 传递租户ID
├─────────────────────────────────────┤
│  Repository Layer                   │  ← 自动添加租户过滤
├─────────────────────────────────────┤
│  Database (MongoDB)                 │  ← 租户数据隔离
└─────────────────────────────────────┘
```

## 2. 实现方案

### 2.1 模型层 - 使用组合模式

```go
// 基础租户资源
type TenantResource struct {
    TenantID string `json:"tenant_id" bson:"tenant_id"`
}

// 业务模型嵌入
type CloudAccount struct {
    ID       int64  `json:"id" bson:"id"`
    Name     string `json:"name" bson:"name"`
    TenantResource  // 嵌入租户资源
    TimeStamps      // 嵌入时间戳
}
```

**优点**：

- 减少代码重复
- 统一验证逻辑
- 便于后续扩展

### 2.2 中间件层 - 自动提取租户 ID

```go
// 从请求头/JWT/查询参数中提取租户ID
router.Use(middleware.TenantMiddleware(logger))

// 需要租户ID的路由组
tenantGroup := router.Group("/api/v1")
tenantGroup.Use(middleware.RequireTenant(logger))
{
    tenantGroup.GET("/accounts", handler.ListAccounts)
}
```

**优点**：

- 统一处理，避免每个接口重复代码
- 集中验证，提高安全性
- 便于日志追踪

### 2.3 Repository 层 - 自动过滤

```go
// 所有查询自动添加租户过滤
func (r *CloudAccountDAO) FindByID(ctx context.Context, id int64, tenantID string) (*domain.CloudAccount, error) {
    filter := bson.M{"id": id}
    filter = WithTenantID(filter, tenantID) // 自动添加租户过滤

    var account domain.CloudAccount
    err := r.col.FindOne(ctx, filter).Decode(&account)
    return &account, err
}
```

**优点**：

- 防止跨租户数据访问
- 统一过滤逻辑
- 减少安全漏洞

## 3. 需要 TenantID 的模型清单

### 3.1 核心业务资源（必须）

- ✅ CloudAccount - 云账号
- ✅ CloudUser - IAM 用户
- ✅ UserGroup - 用户组
- ✅ PolicyTemplate - 策略模板
- ✅ SyncTask - 同步任务
- ✅ AuditLog - 审计日志

### 3.2 云资源（建议）

- ECS 实例
- RDS 数据库
- OSS 存储桶
- VPC 网络
- 安全组

### 3.3 不需要 TenantID 的模型

- 系统配置（全局）
- 云厂商元数据（全局）
- 地域信息（全局）
- 系统日志（全局）

## 4. 数据库索引优化

### 4.1 复合索引策略

```javascript
// 所有包含 tenant_id 的集合都应该创建复合索引
db.cloud_accounts.createIndex({ tenant_id: 1, id: 1 });
db.cloud_accounts.createIndex({ tenant_id: 1, provider: 1, status: 1 });
db.cloud_iam_users.createIndex({ tenant_id: 1, username: 1 });
```

**优点**：

- 提高查询性能
- 确保租户隔离
- 支持高效分页

### 4.2 唯一索引注意事项

```javascript
// 错误：全局唯一
db.cloud_accounts.createIndex({ name: 1 }, { unique: true });

// 正确：租户内唯一
db.cloud_accounts.createIndex({ tenant_id: 1, name: 1 }, { unique: true });
```

## 5. 性能优化建议

### 5.1 避免全表扫描

```go
// ❌ 错误：没有租户过滤
filter := bson.M{"status": "active"}

// ✅ 正确：始终包含租户过滤
filter := bson.M{
    "tenant_id": tenantID,
    "status": "active",
}
```

### 5.2 使用投影减少数据传输

```go
// 只查询需要的字段
projection := bson.M{
    "id": 1,
    "name": 1,
    "status": 1,
    "tenant_id": 1,
}
```

### 5.3 分页查询优化

```go
// 使用 tenant_id + 其他字段的复合索引
filter := bson.M{
    "tenant_id": tenantID,
    "status": "active",
}
opts := options.Find().
    SetSort(bson.D{{Key: "tenant_id", Value: 1}, {Key: "ctime", Value: -1}}).
    SetLimit(limit).
    SetSkip(offset)
```

## 6. 安全性考虑

### 6.1 防止租户数据泄露

```go
// 1. 中间件层验证租户ID
// 2. Service 层传递租户ID
// 3. Repository 层强制过滤
// 4. 数据库层索引约束
```

### 6.2 审计日志

```go
// 所有操作记录租户ID
type AuditLog struct {
    OperatorID string `json:"operator_id"`
    TenantID   string `json:"tenant_id"`  // 必须记录
    Action     string `json:"action"`
    Resource   string `json:"resource"`
}
```

### 6.3 跨租户操作控制

```go
// 超级管理员可以跨租户操作
func (s *Service) GetAccount(ctx context.Context, id int64, tenantID string, isSuperAdmin bool) (*domain.CloudAccount, error) {
    if !isSuperAdmin {
        // 普通用户必须过滤租户
        return s.repo.FindByID(ctx, id, tenantID)
    }
    // 超级管理员可以不过滤
    return s.repo.FindByID(ctx, id, "")
}
```

## 7. 迁移策略

### 7.1 现有数据迁移

```javascript
// 为现有数据添加默认租户ID
db.cloud_accounts.updateMany(
  { tenant_id: { $exists: false } },
  { $set: { tenant_id: "default" } }
);
```

### 7.2 渐进式迁移

1. 第一阶段：添加 `tenant_id` 字段（可选）
2. 第二阶段：中间件提取租户 ID
3. 第三阶段：Repository 层添加过滤
4. 第四阶段：将 `tenant_id` 改为必填

## 8. 测试建议

### 8.1 单元测试

```go
func TestTenantIsolation(t *testing.T) {
    // 创建两个租户的数据
    tenant1Account := createAccount("tenant1")
    tenant2Account := createAccount("tenant2")

    // 验证租户1只能看到自己的数据
    accounts := repo.List(ctx, "tenant1")
    assert.NotContains(t, accounts, tenant2Account)
}
```

### 8.2 集成测试

```go
func TestCrossTenantAccess(t *testing.T) {
    // 尝试跨租户访问
    _, err := service.GetAccount(ctx, tenant2AccountID, "tenant1", false)
    assert.Error(t, err)
    assert.Equal(t, errs.ErrAccountNotFound, err)
}
```

## 9. 监控指标

### 9.1 租户级别监控

- 每个租户的资源数量
- 每个租户的 API 调用量
- 每个租户的存储使用量
- 每个租户的错误率

### 9.2 告警规则

- 租户资源超过配额
- 跨租户访问尝试
- 租户数据异常增长

## 10. 总结

### 优点

- ✅ 数据隔离安全
- ✅ 代码复用性高
- ✅ 性能影响小
- ✅ 易于维护扩展

### 注意事项

- ⚠️ 所有查询必须包含租户过滤
- ⚠️ 索引设计要考虑租户维度
- ⚠️ 唯一约束要在租户内
- ⚠️ 审计日志必须记录租户 ID

### 最佳实践

1. 使用中间件统一提取租户 ID
2. 使用组合模式减少代码重复
3. Repository 层自动添加租户过滤
4. 数据库索引优先考虑租户维度
5. 完善的测试覆盖租户隔离场景
