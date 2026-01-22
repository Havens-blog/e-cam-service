# 用户组成员同步测试脚本

## 功能说明

`test_group_member_sync.go` 是一个集成测试脚本，用于验证用户组成员同步功能是否正常工作。

## 测试流程

1. 调用同步 API，同步云平台用户组及成员
2. 查询本地用户组列表，验证同步结果
3. 查询本地用户列表，验证成员同步
4. 验证数据一致性

## 使用方法

### 前置条件

1. 服务已启动并运行在 `http://localhost:8080`
2. 已配置云账号信息
3. 云账号有权限访问 RAM/CAM API

### 运行测试

#### 方式 1：使用默认配置

```bash
cd scripts
go run test_group_member_sync.go
```

默认配置：

- API 地址: `http://localhost:8080`
- 租户 ID: `tenant-001`
- 云账号 ID: `1`

#### 方式 2：使用环境变量

```bash
# Windows PowerShell
$env:API_BASE_URL="http://localhost:8080"
$env:TENANT_ID="tenant-001"
$env:CLOUD_ACCOUNT_ID="123"
go run test_group_member_sync.go

# Linux/Mac
export API_BASE_URL="http://localhost:8080"
export TENANT_ID="tenant-001"
export CLOUD_ACCOUNT_ID="123"
go run test_group_member_sync.go
```

#### 方式 3：编译后运行

```bash
# 编译
go build -o test_group_sync.exe test_group_member_sync.go

# 运行
./test_group_sync.exe
```

## 输出示例

```
=== 用户组成员同步测试 ===
API地址: http://localhost:8080
租户ID: tenant-001
云账号ID: 1

步骤 1: 执行用户组同步...
同步完成！
  用户组统计:
    - 总数: 5
    - 新创建: 2
    - 已更新: 3
    - 失败: 0
  成员统计:
    - 总数: 15
    - 已同步: 14
    - 失败: 1

步骤 2: 查询用户组列表...
共查询到 5 个用户组:
  1. 开发组 (aliyun) - 成员数: 5
  2. 测试组 (aliyun) - 成员数: 3
  3. 运维组 (aliyun) - 成员数: 4
  4. 产品组 (aliyun) - 成员数: 2
  5. 管理组 (aliyun) - 成员数: 1

步骤 3: 查询用户列表...
共查询到 14 个用户:
  1. zhang.san (aliyun) - 所属用户组: [1 5]
  2. li.si (aliyun) - 所属用户组: [1]
  3. wang.wu (aliyun) - 所属用户组: [2]
  4. zhao.liu (aliyun) - 所属用户组: [3]
  ...

步骤 4: 验证数据一致性...
  ✓ 用户组数量一致: 5
  ✓ 用户数量: 14
  ✓ 用户组关联总数: 18
  ⚠️  警告: 1 个成员同步失败

=== 测试完成 ===
```

## 环境变量说明

| 变量名           | 说明         | 默认值                |
| ---------------- | ------------ | --------------------- |
| API_BASE_URL     | API 服务地址 | http://localhost:8080 |
| TENANT_ID        | 租户 ID      | tenant-001            |
| CLOUD_ACCOUNT_ID | 云账号 ID    | 1                     |

## 故障排查

### 1. 连接失败

```
同步失败: Post "http://localhost:8080/api/v1/cam/iam/groups/sync": dial tcp [::1]:8080: connect: connection refused
```

**解决方案**：

- 检查服务是否启动
- 确认端口号是否正确
- 尝试使用 `127.0.0.1` 替代 `localhost`

### 2. 认证失败

```
API返回错误: 租户ID不能为空
```

**解决方案**：

- 检查 TENANT_ID 环境变量是否设置
- 确认租户 ID 是否存在

### 3. 云账号不存在

```
API返回错误: 获取云账号失败: account not found
```

**解决方案**：

- 检查 CLOUD_ACCOUNT_ID 是否正确
- 确认云账号是否已创建

### 4. 权限不足

```
API返回错误: 从云平台获取用户组列表失败: access denied
```

**解决方案**：

- 检查云账号的 AccessKey 权限
- 确保有 RAM/CAM 读取权限

## 性能测试

如果需要测试大量数据的同步性能，可以修改脚本添加计时功能：

```go
startTime := time.Now()
syncResult, err := syncGroups(baseURL, tenantID, cloudAccountID)
duration := time.Since(startTime)

fmt.Printf("同步耗时: %v\n", duration)
fmt.Printf("平均每个用户组: %v\n", duration/time.Duration(syncResult.Data.TotalGroups))
fmt.Printf("平均每个成员: %v\n", duration/time.Duration(syncResult.Data.TotalMembers))
```

## 自动化测试

可以将此脚本集成到 CI/CD 流程中：

```yaml
# .github/workflows/test.yml
name: Integration Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2

      - name: Setup Go
        uses: actions/setup-go@v2
        with:
          go-version: 1.21

      - name: Start Service
        run: |
          go build -o e-cam-service .
          ./e-cam-service &
          sleep 10

      - name: Run Sync Test
        env:
          API_BASE_URL: http://localhost:8080
          TENANT_ID: test-tenant
          CLOUD_ACCOUNT_ID: 1
        run: |
          cd scripts
          go run test_group_member_sync.go
```
