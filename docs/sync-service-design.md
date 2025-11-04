# 云资源同步服务设计文档

## 概述

云资源同步服务负责从云厂商 API 获取资源信息，并与本地数据库进行同步，支持全量同步和增量同步。

## 核心功能

### 1. 同步任务管理

#### SyncTask - 同步任务

记录每次同步操作的详细信息：

```go
type SyncTask struct {
    ID              int64          // 任务ID
    AccountID       int64          // 云账号ID
    Provider        CloudProvider  // 云厂商
    ResourceType    string         // 资源类型
    Region          string         // 地域
    Status          SyncTaskStatus // 任务状态
    StartTime       int64          // 开始时间
    EndTime         int64          // 结束时间
    Duration        int64          // 执行时长(秒)
    TotalCount      int            // 总资源数
    AddedCount      int            // 新增数量
    UpdatedCount    int            // 更新数量
    DeletedCount    int            // 删除数量
    UnchangedCount  int            // 未变化数量
    ErrorCount      int            // 错误数量
    ErrorMessage    string         // 错误信息
}
```

#### 任务状态

- **pending**: 待执行
- **running**: 执行中
- **success**: 成功
- **failed**: 失败
- **cancelled**: 已取消

#### 任务生命周期

```go
// 创建任务
task := &SyncTask{
    AccountID:    1,
    Provider:     "aliyun",
    ResourceType: "ecs",
    Region:       "cn-hangzhou",
    Status:       TaskStatusPending,
}

// 开始任务
task.Start()

// 完成任务
task.Complete(result)

// 或失败
task.Fail(err)

// 或取消
task.Cancel()
```

### 2. 同步服务

#### SyncService

核心同步服务，提供以下功能：

**全量同步**

```go
result, err := syncService.SyncECSInstances(ctx, account, regions)
```

**增量同步**

```go
result, err := syncService.SyncECSInstancesIncremental(ctx, account, regions, lastSyncTime)
```

**状态变化检测**

```go
added, updated, deleted, unchanged := syncService.DetectInstanceChanges(existingInstances, newInstances)
```

**获取状态变化**

```go
statusMap, err := syncService.GetInstanceStatusChanges(ctx, account, region, instanceIDs)
```

### 3. 变化检测

#### 检测逻辑

系统会比较以下字段来判断实例是否有变化：

1. **状态变化**: Status
2. **名称变化**: InstanceName
3. **IP 变化**: PublicIP, PrivateIP
4. **规格变化**: InstanceType
5. **安全组变化**: SecurityGroups
6. **磁盘变化**: DataDisks
7. **标签变化**: Tags

#### 变化类型

- **新增 (Added)**: 云厂商有，数据库没有
- **更新 (Updated)**: 两边都有，但内容不同
- **删除 (Deleted)**: 数据库有，云厂商没有
- **未变化 (Unchanged)**: 两边都有，内容相同

### 4. 并发控制

#### 多地域并发同步

```go
// 使用 worker pool 控制并发数
semaphore := make(chan struct{}, 5) // 限制并发数为5

for _, region := range regions {
    wg.Add(1)
    go func(r string) {
        defer wg.Done()
        semaphore <- struct{}{}
        defer func() { <-semaphore }()

        // 同步单个地域
        regionResult, err := syncRegionECSInstances(ctx, adapter, account, r)
        // ...
    }(region)
}

wg.Wait()
```

#### 并发安全

- 使用 `sync.Mutex` 保护共享数据
- 使用 channel 控制并发数
- 使用 `sync.WaitGroup` 等待所有任务完成

## 同步流程

### 全量同步流程

```
1. 创建适配器
   ↓
2. 获取地域列表（如果未指定）
   ↓
3. 并发同步多个地域
   ├─ 地域1: 获取实例列表 → 比对差异 → 更新数据库
   ├─ 地域2: 获取实例列表 → 比对差异 → 更新数据库
   └─ 地域N: 获取实例列表 → 比对差异 → 更新数据库
   ↓
4. 合并结果
   ↓
5. 返回同步结果
```

### 增量同步流程

```
1. 查询上次同步时间
   ↓
2. 获取最新实例列表
   ↓
3. 与数据库比对
   ├─ 检测新增实例
   ├─ 检测更新实例
   ├─ 检测删除实例
   └─ 跳过未变化实例
   ↓
4. 只更新有变化的数据
   ↓
5. 返回同步结果
```

### 单个地域同步流程

```
1. 调用适配器获取实例列表
   ↓
2. 查询数据库中已存在的实例
   ↓
3. 比对差异
   ├─ 新增: 插入数据库
   ├─ 更新: 更新数据库
   ├─ 删除: 标记删除或物理删除
   └─ 未变化: 跳过
   ↓
4. 记录同步结果
```

## 数据结构

### SyncResult - 同步结果

```go
type SyncResult struct {
    TaskID         int64
    Success        bool
    TotalCount     int
    AddedCount     int
    UpdatedCount   int
    DeletedCount   int
    UnchangedCount int
    ErrorCount     int
    Errors         []SyncError
    Duration       time.Duration
}
```

### SyncError - 同步错误

```go
type SyncError struct {
    ResourceID string
    Error      string
    Timestamp  time.Time
}
```

## 使用示例

### 示例 1: 同步指定地域

```go
// 创建服务
factory := adapters.NewAdapterFactory(logger)
syncService := service.NewSyncService(factory, logger)

// 创建账号配置
account := &domain.CloudAccount{
    ID:              1,
    Name:            "生产环境阿里云账号",
    Provider:        domain.ProviderAliyun,
    AccessKeyID:     "LTAI...",
    AccessKeySecret: "...",
    DefaultRegion:   "cn-shenzhen",
    Enabled:         true,
}

// 同步指定地域
regions := []string{"cn-beijing", "cn-shanghai"}
result, err := syncService.SyncECSInstances(ctx, account, regions)

// 处理结果
if err != nil {
    log.Error("同步失败", err)
    return
}

log.Info("同步完成",
    "total", result.TotalCount,
    "added", result.AddedCount,
    "updated", result.UpdatedCount,
    "deleted", result.DeletedCount,
    "duration", result.Duration)
```

### 示例 2: 同步所有地域

```go
// 传入 nil 表示同步所有地域
result, err := syncService.SyncECSInstances(ctx, account, nil)
```

### 示例 3: 增量同步

```go
// 获取上次同步时间
lastSyncTime := time.Now().Add(-1 * time.Hour)

// 执行增量同步
result, err := syncService.SyncECSInstancesIncremental(
    ctx,
    account,
    regions,
    lastSyncTime,
)
```

### 示例 4: 检测变化

```go
// 获取已存在的实例
existingInstances := getExistingInstancesFromDB()

// 获取最新的实例
newInstances, err := adapter.GetECSInstances(ctx, region)

// 检测变化
added, updated, deleted, unchanged := syncService.DetectInstanceChanges(
    existingInstances,
    newInstances,
)

// 处理变化
for _, inst := range added {
    insertInstance(inst)
}
for _, inst := range updated {
    updateInstance(inst)
}
for _, inst := range deleted {
    deleteInstance(inst)
}
```

## 性能优化

### 1. 并发控制

- 使用 worker pool 限制并发数
- 避免过多并发请求导致 API 限流
- 建议并发数: 3-5

### 2. 批量操作

- 批量插入数据库
- 批量更新数据库
- 减少数据库往返次数

### 3. 增量同步

- 只同步有变化的资源
- 记录上次同步时间
- 减少不必要的 API 调用

### 4. 缓存策略

- 缓存地域列表
- 缓存实例规格信息
- 减少重复查询

## 错误处理

### 1. 错误分类

- **网络错误**: 重试
- **认证错误**: 停止同步，通知管理员
- **限流错误**: 延迟重试
- **数据错误**: 记录日志，继续处理其他资源

### 2. 错误恢复

```go
// 单个地域失败不影响其他地域
for _, region := range regions {
    go func(r string) {
        defer func() {
            if err := recover(); err != nil {
                log.Error("同步地域panic", "region", r, "error", err)
            }
        }()

        // 同步逻辑
    }(region)
}
```

### 3. 错误记录

- 记录详细的错误信息
- 记录错误发生的时间
- 记录相关的资源 ID
- 便于问题排查

## 监控指标

### 1. 同步指标

- 同步任务数量
- 同步成功率
- 同步耗时
- 资源变化数量

### 2. 错误指标

- 错误数量
- 错误类型分布
- 错误率

### 3. 性能指标

- API 调用次数
- API 响应时间
- 数据库操作耗时
- 并发数

## 最佳实践

### 1. 定时同步

```go
// 使用 cron 定时执行同步
ticker := time.NewTicker(1 * time.Hour)
for range ticker.C {
    result, err := syncService.SyncECSInstances(ctx, account, nil)
    // 处理结果
}
```

### 2. 失败重试

```go
maxRetries := 3
for i := 0; i < maxRetries; i++ {
    result, err := syncService.SyncECSInstances(ctx, account, regions)
    if err == nil {
        break
    }

    if i < maxRetries-1 {
        time.Sleep(time.Duration(i+1) * time.Minute)
    }
}
```

### 3. 优雅关闭

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// 监听信号
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

go func() {
    <-sigCh
    cancel() // 取消所有正在进行的同步
}()

// 执行同步
result, err := syncService.SyncECSInstances(ctx, account, regions)
```

## 未来扩展

1. **支持更多资源类型**: RDS, Redis, OSS 等
2. **实时同步**: 基于云厂商事件通知
3. **智能调度**: 根据资源变化频率动态调整同步间隔
4. **成本优化**: 减少不必要的 API 调用
5. **数据分析**: 资源使用趋势分析
