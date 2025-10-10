# CAM Service 单元测试运行指南

## 测试文件说明

### account_test.go
- 包含 CloudAccountService 的完整单元测试
- 使用 testify/suite 框架进行测试组织
- 使用 testify/mock 进行依赖模拟
- 覆盖所有主要业务场景和边界条件

## 运行测试

### 1. 运行所有测试
```bash
# 在项目根目录运行
go test ./internal/cam/internal/service/...

# 或者在 service 目录运行
cd internal/cam/internal/service
go test .
```

### 2. 运行特定测试文件
```bash
go test ./internal/cam/internal/service/account_test.go ./internal/cam/internal/service/account.go
```

### 3. 运行特定测试用例
```bash
# 运行 CreateAccount 相关测试
go test -run TestCloudAccountServiceSuite/TestCreateAccount ./internal/cam/internal/service/

# 运行所有 CloudAccountService 测试
go test -run TestCloudAccountServiceSuite ./internal/cam/internal/service/
```

### 4. 查看测试覆盖率
```bash
# 生成覆盖率报告
go test -cover ./internal/cam/internal/service/

# 生成详细覆盖率报告
go test -coverprofile=coverage.out ./internal/cam/internal/service/
go tool cover -html=coverage.out -o coverage.html
```

### 5. 运行基准测试
```bash
go test -bench=. ./internal/cam/internal/service/
```

### 6. 详细输出模式
```bash
# 显示详细测试输出
go test -v ./internal/cam/internal/service/

# 显示测试进度
go test -v -count=1 ./internal/cam/internal/service/
```

## 测试覆盖的场景

### CreateAccount 测试
- ✅ 成功创建云账号
- ✅ 账号名称已存在
- ✅ 数据库创建失败
- ✅ 数据验证失败

### GetAccount 测试
- ✅ 成功获取云账号
- ✅ 账号不存在
- ✅ 敏感数据脱敏验证

### ListAccounts 测试
- ✅ 成功获取账号列表
- ✅ 分页参数处理
- ✅ 敏感数据脱敏验证

### UpdateAccount 测试
- ✅ 成功更新账号信息
- ✅ 账号不存在
- ✅ 部分字段更新

### DeleteAccount 测试
- ✅ 成功删除账号
- ✅ 账号不存在
- ✅ 账号状态检查（测试中不允许删除）

### TestConnection 测试
- ✅ 连接测试成功
- ✅ 状态更新验证
- ✅ 测试结果记录

### EnableAccount/DisableAccount 测试
- ✅ 成功启用/禁用账号
- ✅ 账号不存在处理
- ✅ 状态更新验证

### SyncAccount 测试
- ✅ 成功启动同步
- ✅ 账号已禁用检查
- ✅ 只读账号检查
- ✅ 同步状态记录

## 测试数据准备

测试使用 Mock 对象，不需要真实数据库连接：

```go
// 示例：模拟成功创建
suite.repo.On("GetByName", suite.ctx, "test-account", "tenant-123").
    Return(domain.CloudAccount{}, errors.New("not found"))

suite.repo.On("Create", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).
    Return(int64(123), nil)
```

## 持续集成

在 CI/CD 流水线中运行测试：

```yaml
# GitHub Actions 示例
- name: Run Tests
  run: |
    go test -v -cover ./internal/cam/internal/service/...
    
- name: Generate Coverage Report
  run: |
    go test -coverprofile=coverage.out ./internal/cam/internal/service/...
    go tool cover -html=coverage.out -o coverage.html
```

## 测试最佳实践

1. **使用表驱动测试** - 覆盖多种场景
2. **Mock 外部依赖** - 隔离测试单元
3. **验证业务逻辑** - 不仅测试成功路径，也测试失败路径
4. **敏感数据处理** - 验证数据脱敏功能
5. **错误处理** - 确保错误码正确返回
6. **日志验证** - 重要操作需要有日志记录

## 故障排除

### 常见问题

1. **Import 错误**
   ```bash
   # 确保依赖已安装
   go mod tidy
   ```

2. **Mock 断言失败**
   ```bash
   # 检查 mock 期望设置是否正确
   # 确保所有 mock 调用都有对应的期望
   ```

3. **测试超时**
   ```bash
   # 增加测试超时时间
   go test -timeout 30s ./internal/cam/internal/service/
   ```

## 性能测试

基准测试结果示例：
```
BenchmarkCreateAccount-8    1000000    1234 ns/op    456 B/op    7 allocs/op
```

这表示：
- 每次操作耗时 1234 纳秒
- 每次操作分配 456 字节内存
- 每次操作进行 7 次内存分配