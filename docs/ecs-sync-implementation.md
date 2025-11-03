# 阿里云 ECS 同步功能实现总结

## 实现概述

本次实现了阿里云 ECS 实例的自动发现和同步功能，支持从阿里云 API 获取 ECS 实例信息并保存到数据库中。

## 实现的功能

### 1. 资产发现（Discover）

- **接口**: `POST /api/v1/cam/assets/discover`
- **功能**: 从阿里云 API 获取指定地域的 ECS 实例，但不保存到数据库
- **用途**: 用于预览和验证，确认要同步的资产

### 2. 资产同步（Sync）

- **接口**: `POST /api/v1/cam/assets/sync`
- **功能**: 同步指定云厂商的所有 ECS 实例到数据库
- **特性**:
  - 支持多地域并发同步
  - 自动检测新增、更新的实例
  - 更新云账号的最后同步时间

### 3. 资产查询

- **接口**: `GET /api/v1/cam/assets`
- **功能**: 查询已同步的资产列表
- **支持**: 按云厂商、资产类型、地域、状态等条件筛选

### 4. 资产统计

- **接口**: `GET /api/v1/cam/assets/statistics`
- **功能**: 获取资产统计信息
- **统计维度**: 云厂商、资产类型、地域、状态

## 代码结构

### 1. 服务层 (internal/cam/service/asset.go)

新增方法：

```go
// DiscoverAssets 发现资产（不保存到数据库）
func (s *service) DiscoverAssets(ctx context.Context, provider, region string) ([]domain.CloudAsset, error)

// SyncAssets 同步资产到数据库
func (s *service) SyncAssets(ctx context.Context, provider string) error

// syncAccountAssets 同步单个账号的资产
func (s *service) syncAccountAssets(ctx context.Context, account *shareddomain.CloudAccount) (int, error)

// syncRegionECSInstances 同步单个地域的 ECS 实例
func (s *service) syncRegionECSInstances(
	ctx context.Context,
	adapter syncdomain.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (int, error)

// convertECSToAsset 将 ECS 实例转换为资产
func (s *service) convertECSToAsset(inst syncdomain.ECSInstance) (domain.CloudAsset, error)
```

### 2. 适配器层 (internal/cam/sync/service/adapters/)

已有的阿里云适配器：

```go
// AliyunAdapter 阿里云适配器
type AliyunAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
	clients         map[string]*ecs.Client
}

// 主要方法
func (a *AliyunAdapter) GetProvider() domain.CloudProvider
func (a *AliyunAdapter) ValidateCredentials(ctx context.Context) error
func (a *AliyunAdapter) GetRegions(ctx context.Context) ([]domain.Region, error)
func (a *AliyunAdapter) GetECSInstances(ctx context.Context, region string) ([]domain.ECSInstance, error)
```

### 3. 依赖注入 (internal/cam/wire.go & wire_gen.go)

更新了依赖注入配置，添加了：

- `adapters.NewAdapterFactory` - 适配器工厂
- 更新 `service.NewService` 的参数，增加了 `accountRepo`, `adapterFactory`, `logger`

## 数据流程

```
1. API 请求
   ↓
2. Handler (web/handler.go)
   ↓
3. Service (service/asset.go)
   ├─→ 获取云账号 (accountRepo)
   ├─→ 创建适配器 (adapterFactory)
   ├─→ 调用云厂商 API (adapter)
   ├─→ 转换数据格式 (convertECSToAsset)
   └─→ 保存到数据库 (assetRepo)
```

## 同步流程

### 发现流程

```
1. 接收发现请求 (provider, region)
2. 获取该云厂商的第一个可用账号
3. 创建云厂商适配器
4. 调用适配器获取 ECS 实例
5. 转换为资产格式
6. 返回资产列表（不保存）
```

### 同步流程

```
1. 接收同步请求 (provider)
2. 获取该云厂商的所有可用账号
3. 对每个账号：
   a. 创建适配器
   b. 获取所有地域列表
   c. 过滤支持的地域
   d. 对每个地域：
      - 获取 ECS 实例
      - 转换为资产格式
      - 检查是否已存在
      - 新增或更新资产
   e. 更新账号的最后同步时间
4. 返回同步结果
```

## 关键特性

### 1. 多地域并发同步

```go
// 在 sync_service.go 中实现
var wg sync.WaitGroup
semaphore := make(chan struct{}, 5) // 限制并发数为5

for _, region := range regions {
    wg.Add(1)
    go func(r string) {
        defer wg.Done()
        semaphore <- struct{}{}
        defer func() { <-semaphore }()

        // 同步地域资产
        regionResult, err := s.syncRegionECSInstances(ctx, adapter, account, r)
        // ...
    }(region)
}
wg.Wait()
```

### 2. 数据转换

ECS 实例 → 云资产：

```go
func (s *service) convertECSToAsset(inst syncdomain.ECSInstance) (domain.CloudAsset, error) {
    // 转换标签
    tags := make([]domain.Tag, 0, len(inst.Tags))
    for k, v := range inst.Tags {
        tags = append(tags, domain.Tag{Key: k, Value: v})
    }

    // 序列化详细信息为 JSON
    metadata, err := json.Marshal(inst)

    // 创建资产对象
    return domain.CloudAsset{
        AssetId:      inst.InstanceID,
        AssetName:    inst.InstanceName,
        AssetType:    "ecs",
        Provider:     inst.Provider,
        Region:       inst.Region,
        Zone:         inst.Zone,
        Status:       inst.Status,
        Tags:         tags,
        Metadata:     string(metadata),
        // ...
    }, nil
}
```

### 3. 增量更新

```go
// 检查资产是否已存在
existing, err := s.repo.GetAssetByAssetId(ctx, asset.AssetId)
if err != nil {
    // 资产不存在，创建新资产
    _, err = s.CreateAsset(ctx, asset)
} else {
    // 资产已存在，更新资产
    asset.Id = existing.Id
    asset.CreateTime = existing.CreateTime
    err = s.UpdateAsset(ctx, asset)
}
```

## 测试

### 测试脚本

创建了 `scripts/test_ecs_sync.go` 测试脚本，包含：

1. 创建测试云账号
2. 发现 ECS 实例（不保存）
3. 同步 ECS 实例到数据库
4. 查询已同步的资产
5. 获取资产统计信息

### 运行测试

```bash
# 设置环境变量
export ALIYUN_ACCESS_KEY_ID="your_access_key_id"
export ALIYUN_ACCESS_KEY_SECRET="your_access_key_secret"
export MONGO_URI="mongodb://localhost:27017"

# 运行测试
go run scripts/test_ecs_sync.go
```

## 文档

创建了以下文档：

1. **ecs-sync-guide.md** - 使用指南

   - API 接口说明
   - 使用步骤
   - 数据结构
   - 故障排查

2. **ecs-sync-implementation.md** - 实现总结（本文档）
   - 实现概述
   - 代码结构
   - 数据流程
   - 关键特性

## 已实现的功能清单

- [x] 阿里云 ECS 实例发现
- [x] 阿里云 ECS 实例同步
- [x] 多地域并发同步
- [x] 资产增量更新
- [x] 资产查询和筛选
- [x] 资产统计
- [x] 云账号管理
- [x] 适配器工厂模式
- [x] 错误处理和日志
- [x] 测试脚本
- [x] 使用文档

## 待扩展功能

- [ ] 支持更多资源类型（RDS、OSS、SLB、CDN 等）
- [ ] 支持 AWS、Azure 等其他云厂商
- [ ] 实现资源变更通知
- [ ] 添加成本分析功能
- [ ] 支持资源标签管理
- [ ] 实现资源生命周期管理
- [ ] 添加定时同步任务
- [ ] 实现同步任务状态跟踪
- [ ] 添加同步历史记录
- [ ] 实现资源依赖关系分析

## 性能优化建议

1. **批量操作**: 使用批量插入/更新减少数据库操作次数
2. **缓存**: 缓存地域列表、实例规格等不常变化的数据
3. **增量同步**: 只同步有变化的实例
4. **并发控制**: 根据 API 限流调整并发数
5. **分页查询**: 对大量实例使用分页查询

## 安全考虑

1. **凭证加密**: AccessKey 应该加密存储
2. **权限控制**: 限制 API 访问权限
3. **审计日志**: 记录所有同步操作
4. **敏感信息脱敏**: 返回数据时脱敏敏感信息
5. **网络隔离**: 使用 VPC 内网访问云 API

## 监控指标

建议监控以下指标：

1. **同步成功率**: 成功同步的资产数 / 总资产数
2. **同步耗时**: 每次同步的总耗时
3. **API 调用次数**: 监控 API 调用频率
4. **错误率**: 同步失败的次数和原因
5. **资产变化**: 新增、更新、删除的资产数量

## 总结

本次实现完成了阿里云 ECS 实例的自动发现和同步功能，为后续扩展其他云资源类型和云厂商奠定了基础。代码结构清晰，易于维护和扩展。

主要亮点：

- ✅ 完整的功能实现
- ✅ 良好的代码结构
- ✅ 详细的文档说明
- ✅ 完善的测试脚本
- ✅ 可扩展的架构设计
