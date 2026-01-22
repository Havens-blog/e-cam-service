# 同步任务 API

## 1. 创建同步任务

创建新的同步任务。

**接口**: `POST /api/v1/cam/iam/sync/tasks`

### 请求参数

```json
{
  "task_type": "full",
  "target_type": "user",
  "target_id": 1001,
  "cloud_account_id": 1,
  "provider": "aliyun"
}
```

| 字段             | 类型   | 必填 | 说明                        |
| ---------------- | ------ | ---- | --------------------------- |
| task_type        | string | 是   | 任务类型 (full/incremental) |
| target_type      | string | 是   | 目标类型 (user/group)       |
| target_id        | int64  | 是   | 目标 ID                     |
| cloud_account_id | int64  | 是   | 云账号 ID                   |
| provider         | string | 是   | 云厂商                      |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "task_type": "full",
    "target_type": "user",
    "target_id": 1001,
    "cloud_account_id": 1,
    "provider": "aliyun",
    "status": "pending",
    "create_time": "2024-01-01T00:00:00Z"
  }
}
```

## 2. 获取同步任务详情

获取指定同步任务的详细信息。

**接口**: `GET /api/v1/cam/iam/sync/tasks/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 任务 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "task_type": "full",
    "target_type": "user",
    "target_id": 1001,
    "cloud_account_id": 1,
    "provider": "aliyun",
    "status": "success",
    "result": {
      "success_count": 10,
      "failed_count": 0,
      "details": "同步完成"
    },
    "error_message": "",
    "start_time": "2024-01-01T00:00:00Z",
    "end_time": "2024-01-01T00:05:00Z",
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:05:00Z"
  }
}
```

## 3. 查询同步任务列表

分页查询同步任务列表。

**接口**: `GET /api/v1/cam/iam/sync/tasks`

### 查询参数

| 参数             | 类型   | 必填 | 说明              |
| ---------------- | ------ | ---- | ----------------- |
| task_type        | string | 否   | 任务类型          |
| status           | string | 否   | 任务状态          |
| cloud_account_id | int64  | 否   | 云账号 ID         |
| provider         | string | 否   | 云厂商            |
| page             | int    | 否   | 页码，默认 1      |
| size             | int    | 否   | 每页数量，默认 20 |

### 请求示例

```
GET /api/v1/cam/iam/sync/tasks?provider=aliyun&status=success&page=1&size=20
```

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1,
        "task_type": "full",
        "target_type": "user",
        "provider": "aliyun",
        "status": "success",
        "start_time": "2024-01-01T00:00:00Z",
        "end_time": "2024-01-01T00:05:00Z"
      }
    ],
    "total": 50,
    "page": 1,
    "size": 20
  }
}
```

## 4. 取消同步任务

取消正在运行的同步任务。

**接口**: `POST /api/v1/cam/iam/sync/tasks/{id}/cancel`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 任务 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "status": "cancelled"
  }
}
```

## 5. 重试失败的同步任务

重试失败的同步任务。

**接口**: `POST /api/v1/cam/iam/sync/tasks/{id}/retry`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 任务 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "status": "pending"
  }
}
```

## 6. 批量同步用户

批量同步多个用户到云平台。

**接口**: `POST /api/v1/cam/iam/sync/batch-users`

### 请求参数

```json
{
  "user_ids": [1001, 1002, 1003],
  "cloud_account_id": 1,
  "provider": "aliyun"
}
```

| 字段             | 类型    | 必填 | 说明         |
| ---------------- | ------- | ---- | ------------ |
| user_ids         | []int64 | 是   | 用户 ID 列表 |
| cloud_account_id | int64   | 是   | 云账号 ID    |
| provider         | string  | 是   | 云厂商       |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "task_ids": [1, 2, 3],
    "total_count": 3
  }
}
```

## 7. 获取同步统计信息

获取同步任务的统计信息。

**接口**: `GET /api/v1/cam/iam/sync/statistics`

### 查询参数

| 参数       | 类型   | 必填 | 说明               |
| ---------- | ------ | ---- | ------------------ |
| provider   | string | 否   | 云厂商             |
| start_time | string | 否   | 开始时间 (RFC3339) |
| end_time   | string | 否   | 结束时间 (RFC3339) |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_tasks": 100,
    "success_tasks": 85,
    "failed_tasks": 10,
    "running_tasks": 5,
    "success_rate": 0.85,
    "avg_duration": 300
  }
}
```
