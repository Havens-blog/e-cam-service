# 异步任务框架设计文档

## 概述

为了处理耗时的资产同步操作，我们设计并实现了一个成熟的异步任务框架。该框架基于 Go 的 channel 和 goroutine，提供了任务队列、任务执行、状态跟踪等完整功能。

## 架构设计

### 核心组件

```
┌─────────────┐
│   Web API   │  提交任务
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Task Service│  任务服务层
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Task Queue  │  任务队列（channel）
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Workers   │  工作协程池
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Executors  │  任务执行器
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  MongoDB    │  任务状态持久化
└─────────────┘
```

### 目录结构

```
internal/cam/task/
├── domain/
│   └── task.go              # 任务领域模型
├── repository/
│   ├── dao/
│   │   └── task.go          # 任务DAO
│   └── task.go              # 任务仓储
├── queue/
│   └── queue.go             # 任务队列
├── executor/
│   └── sync_assets.go       # 同步资产执行器
├── service/
│   └── task_service.go      # 任务服务
├── web/
│   ├── handler.go           # HTTP处理器
│   └── vo.go                # VO对象
└── module.go                # 模块初始化
```

## 核心功能

### 1. 任务类型

当前支持的任务类型：

- `sync_assets` - 同步资产
- `discover_assets` - 发现资产

### 2. 任务状态

- `pending` - 待执行
- `running` - 执行中
- `completed` - 已完成
- `failed` - 失败
- `cancelled` - 已取消

### 3. 任务队列

**特性：**

- 基于 channel 的任务队列
- 支持配置 worker 数量
- 支持配置队列缓冲大小
- 自动任务分发
- 优雅关闭

**配置：**

```go
queueConfig := queue.Config{
    WorkerNum:  5,   // 5个worker并发执行
    BufferSize: 100, // 队列最多缓冲100个任务
}
```

### 4. 任务执行器

**接口定义：**

```go
type TaskExecutor interface {
    Execute(ctx context.Context, task *Task) error
    GetType() TaskType
}
```

**实现示例：**

```go
type SyncAssetsExecutor struct {
    assetService service.Service
    taskRepo     repository.TaskRepository
    logger       *elog.Component
}

func (e *SyncAssetsExecutor) Execute(ctx context.Context, task *Task) error {
    // 1. 解析任务参数
    // 2. 执行业务逻辑
    // 3. 更新任务进度
    // 4. 保存任务结果
    return nil
}
```

### 5. 任务持久化

所有任务状态都持久化到 MongoDB，包括：

- 任务基本信息
- 任务参数
- 执行进度
- 执行结果
- 错误信息
- 时间戳

## API 接口

### 1. 提交同步资产任务

**请求：**

```http
POST /api/v1/cam/tasks/sync-assets
Content-Type: application/json

{
  "provider": "aliyun",
  "asset_types": ["ecs"],
  "regions": ["cn-shenzhen"],
  "account_id": 123
}
```

**响应：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "task_id": "550e8400-e29b-41d4-a716-446655440000",
    "message": "任务已提交，正在执行中"
  }
}
```

### 2. 查询任务状态

**请求：**

```http
GET /api/v1/cam/tasks/{task_id}
```

**响应：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "type": "sync_assets",
    "status": "running",
    "params": {
      "provider": "aliyun",
      "asset_types": ["ecs"]
    },
    "result": null,
    "error": "",
    "progress": 45,
    "message": "正在同步地域 cn-shenzhen",
    "created_by": "system",
    "created_at": "2025-10-30T17:00:00Z",
    "started_at": "2025-10-30T17:00:01Z",
    "completed_at": null,
    "duration": 0
  }
}
```

### 3. 获取任务列表

**请求：**

```http
GET /api/v1/cam/tasks?type=sync_assets&status=completed&offset=0&limit=20
```

**响应：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "tasks": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "type": "sync_assets",
        "status": "completed",
        "progress": 100,
        "message": "任务执行完成",
        "created_at": "2025-10-30T17:00:00Z",
        "completed_at": "2025-10-30T17:05:00Z",
        "duration": 300
      }
    ],
    "total": 1
  }
}
```

### 4. 取消任务

**请求：**

```http
POST /api/v1/cam/tasks/{task_id}/cancel
```

**响应：**

```json
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

### 5. 删除任务

**请求：**

```http
DELETE /api/v1/cam/tasks/{task_id}
```

**响应：**

```json
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

## 使用示例

### 1. 提交同步任务

```bash
# 提交任务
curl -X POST http://localhost:8001/api/v1/cam/tasks/sync-assets \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aliyun",
    "asset_types": ["ecs"]
  }'

# 响应
{
  "code": 0,
  "msg": "success",
  "data": {
    "task_id": "550e8400-e29b-41d4-a716-446655440000",
    "message": "任务已提交，正在执行中"
  }
}
```

### 2. 轮询任务状态

```bash
# 查询任务状态
curl http://localhost:8001/api/v1/cam/tasks/550e8400-e29b-41d4-a716-446655440000

# 响应（执行中）
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "running",
    "progress": 45,
    "message": "正在同步地域 cn-shenzhen"
  }
}

# 响应（已完成）
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "completed",
    "progress": 100,
    "message": "任务执行完成",
    "result": {
      "total_count": 100,
      "added_count": 10,
      "updated_count": 90
    },
    "duration": 300
  }
}
```

### 3. 查询任务历史

```bash
# 查询所有已完成的同步任务
curl "http://localhost:8001/api/v1/cam/tasks?type=sync_assets&status=completed&limit=10"
```

## 扩展开发

### 添加新的任务类型

1. **定义任务类型**

```go
// internal/cam/task/domain/task.go
const (
    TaskTypeExportAssets TaskType = "export_assets"
)

type ExportAssetsParams struct {
    Provider string   `json:"provider"`
    Format   string   `json:"format"` // csv, json, excel
}
```

2. **实现任务执行器**

```go
// internal/cam/task/executor/export_assets.go
type ExportAssetsExecutor struct {
    assetService service.Service
    taskRepo     repository.TaskRepository
    logger       *elog.Component
}

func (e *ExportAssetsExecutor) GetType() domain.TaskType {
    return domain.TaskTypeExportAssets
}

func (e *ExportAssetsExecutor) Execute(ctx context.Context, task *domain.Task) error {
    // 实现导出逻辑
    return nil
}
```

3. **注册执行器**

```go
// internal/cam/task/module.go
exportAssetsExecutor := executor.NewExportAssetsExecutor(assetService, taskRepo, logger)
taskQueue.RegisterExecutor(exportAssetsExecutor)
```

4. **添加 API 接口**

```go
// internal/cam/task/web/handler.go
func (h *TaskHandler) SubmitExportAssetsTask(ctx *gin.Context, req SubmitExportAssetsTaskReq) (ginx.Result, error) {
    // 提交任务
}
```

## 性能优化

### 1. Worker 数量调优

根据任务类型和系统资源调整 worker 数量：

```go
// CPU 密集型任务
queueConfig := queue.Config{
    WorkerNum: runtime.NumCPU(),
}

// IO 密集型任务
queueConfig := queue.Config{
    WorkerNum: runtime.NumCPU() * 2,
}
```

### 2. 队列缓冲大小

根据任务提交频率调整缓冲大小：

```go
queueConfig := queue.Config{
    BufferSize: 1000, // 高频提交场景
}
```

### 3. 任务优先级

可以扩展支持任务优先级：

```go
type Task struct {
    Priority int // 优先级，数字越大优先级越高
    // ...
}
```

## 监控指标

建议监控以下指标：

1. **队列指标**

   - 队列长度
   - 待执行任务数
   - 执行中任务数

2. **执行指标**

   - 任务执行成功率
   - 任务平均执行时间
   - 任务失败率

3. **系统指标**
   - Worker 使用率
   - 内存使用
   - CPU 使用

## 故障处理

### 1. 任务失败重试

可以扩展支持自动重试：

```go
type Task struct {
    RetryCount int // 已重试次数
    MaxRetry   int // 最大重试次数
    // ...
}
```

### 2. 死信队列

对于多次失败的任务，可以移入死信队列：

```go
type DeadLetterQueue struct {
    tasks chan *Task
}
```

### 3. 任务超时

可以为任务设置超时时间：

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
defer cancel()

err := executor.Execute(ctx, task)
```

## 最佳实践

1. **任务幂等性**

   - 确保任务可以安全重试
   - 使用唯一标识避免重复执行

2. **进度更新**

   - 定期更新任务进度
   - 提供有意义的进度消息

3. **错误处理**

   - 详细记录错误信息
   - 区分可重试和不可重试错误

4. **资源清理**

   - 任务完成后清理临时资源
   - 使用 defer 确保资源释放

5. **日志记录**
   - 记录任务关键节点
   - 便于问题排查

## 总结

异步任务框架提供了完整的任务管理能力，包括：

- ✅ 任务提交和调度
- ✅ 任务状态跟踪
- ✅ 任务进度更新
- ✅ 任务结果持久化
- ✅ 任务取消和删除
- ✅ 可扩展的执行器机制
- ✅ 优雅的启动和关闭

该框架可以轻松扩展支持更多类型的异步任务，为系统提供强大的异步处理能力。
