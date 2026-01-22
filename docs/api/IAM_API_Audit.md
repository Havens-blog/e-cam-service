# 审计日志 API

## 1. 查询审计日志列表

分页查询审计日志，支持多条件筛选。

**接口**: `GET /api/v1/cam/iam/audit/logs`

### 查询参数

| 参数           | 类型   | 必填 | 说明                                        |
| -------------- | ------ | ---- | ------------------------------------------- |
| operation_type | string | 否   | 操作类型 (create/update/delete/sync/assign) |
| operator_id    | string | 否   | 操作人 ID                                   |
| target_type    | string | 否   | 目标类型 (user/group/policy)                |
| cloud_platform | string | 否   | 云平台                                      |
| tenant_id      | string | 否   | 租户 ID                                     |
| start_time     | string | 否   | 开始时间 (RFC3339)                          |
| end_time       | string | 否   | 结束时间 (RFC3339)                          |
| page           | int    | 否   | 页码，默认 1                                |
| size           | int    | 否   | 每页数量，默认 20                           |

### 请求示例

```
GET /api/v1/cam/iam/audit/logs?operation_type=create&cloud_platform=aliyun&start_time=2024-01-01T00:00:00Z&end_time=2024-01-31T23:59:59Z&page=1&size=20
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
        "operation_type": "create",
        "operator_id": "admin-001",
        "operator_name": "管理员",
        "target_type": "user",
        "target_id": "1001",
        "target_name": "test-user",
        "cloud_platform": "aliyun",
        "operation_details": {
          "action": "创建用户",
          "changes": {
            "username": "test-user",
            "email": "test@example.com"
          }
        },
        "ip_address": "192.168.1.100",
        "user_agent": "Mozilla/5.0...",
        "status": "success",
        "error_message": "",
        "tenant_id": "tenant-001",
        "create_time": "2024-01-01T10:00:00Z"
      }
    ],
    "total": 500,
    "page": 1,
    "size": 20
  }
}
```

## 2. 获取审计日志详情

获取指定审计日志的详细信息。

**接口**: `GET /api/v1/cam/iam/audit/logs/{id}`

### 路径参数

| 参数 | 类型  | 说明    |
| ---- | ----- | ------- |
| id   | int64 | 日志 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "operation_type": "create",
    "operator_id": "admin-001",
    "operator_name": "管理员",
    "target_type": "user",
    "target_id": "1001",
    "target_name": "test-user",
    "cloud_platform": "aliyun",
    "operation_details": {
      "action": "创建用户",
      "changes": {
        "username": "test-user",
        "email": "test@example.com",
        "permission_groups": [1, 2]
      },
      "before": null,
      "after": {
        "id": 1001,
        "username": "test-user",
        "status": "active"
      }
    },
    "ip_address": "192.168.1.100",
    "user_agent": "Mozilla/5.0...",
    "status": "success",
    "error_message": "",
    "tenant_id": "tenant-001",
    "create_time": "2024-01-01T10:00:00Z"
  }
}
```

## 3. 导出审计日志

导出审计日志为文件。

**接口**: `POST /api/v1/cam/iam/audit/logs/export`

### 请求参数

```json
{
  "operation_type": "create",
  "cloud_platform": "aliyun",
  "tenant_id": "tenant-001",
  "start_time": "2024-01-01T00:00:00Z",
  "end_time": "2024-01-31T23:59:59Z",
  "format": "csv"
}
```

| 字段           | 类型   | 必填 | 说明                |
| -------------- | ------ | ---- | ------------------- |
| operation_type | string | 否   | 操作类型            |
| operator_id    | string | 否   | 操作人 ID           |
| target_type    | string | 否   | 目标类型            |
| cloud_platform | string | 否   | 云平台              |
| tenant_id      | string | 否   | 租户 ID             |
| start_time     | string | 否   | 开始时间            |
| end_time       | string | 否   | 结束时间            |
| format         | string | 是   | 导出格式 (csv/json) |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "download_url": "https://example.com/exports/audit-logs-20240101.csv",
    "file_name": "audit-logs-20240101.csv",
    "file_size": 1024000,
    "expire_time": "2024-01-02T00:00:00Z"
  }
}
```

## 4. 生成审计报告

生成指定时间范围的审计报告。

**接口**: `POST /api/v1/cam/iam/audit/reports`

### 请求参数

```json
{
  "start_time": "2024-01-01T00:00:00Z",
  "end_time": "2024-01-31T23:59:59Z",
  "tenant_id": "tenant-001"
}
```

| 字段       | 类型   | 必填 | 说明               |
| ---------- | ------ | ---- | ------------------ |
| start_time | string | 是   | 开始时间 (RFC3339) |
| end_time   | string | 是   | 结束时间 (RFC3339) |
| tenant_id  | string | 是   | 租户 ID            |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "report_id": "report-001",
    "status": "generating",
    "create_time": "2024-02-01T00:00:00Z"
  }
}
```

## 5. 获取审计报告

获取已生成的审计报告。

**接口**: `GET /api/v1/cam/iam/audit/reports/{id}`

### 路径参数

| 参数 | 类型   | 说明    |
| ---- | ------ | ------- |
| id   | string | 报告 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "report_id": "report-001",
    "status": "completed",
    "summary": {
      "total_operations": 1000,
      "create_operations": 300,
      "update_operations": 400,
      "delete_operations": 200,
      "sync_operations": 100,
      "success_rate": 0.95,
      "top_operators": [
        {
          "operator_id": "admin-001",
          "operator_name": "管理员",
          "operation_count": 500
        }
      ],
      "platform_distribution": {
        "aliyun": 600,
        "aws": 400
      }
    },
    "download_url": "https://example.com/reports/audit-report-001.pdf",
    "create_time": "2024-02-01T00:00:00Z",
    "complete_time": "2024-02-01T00:10:00Z"
  }
}
```

## 6. 获取审计统计信息

获取审计日志的统计信息。

**接口**: `GET /api/v1/cam/iam/audit/statistics`

### 查询参数

| 参数       | 类型   | 必填 | 说明     |
| ---------- | ------ | ---- | -------- |
| tenant_id  | string | 否   | 租户 ID  |
| start_time | string | 否   | 开始时间 |
| end_time   | string | 否   | 结束时间 |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_operations": 1000,
    "operation_type_distribution": {
      "create": 300,
      "update": 400,
      "delete": 200,
      "sync": 100
    },
    "platform_distribution": {
      "aliyun": 600,
      "aws": 400
    },
    "success_rate": 0.95,
    "daily_trend": [
      {
        "date": "2024-01-01",
        "count": 50
      },
      {
        "date": "2024-01-02",
        "count": 60
      }
    ]
  }
}
```
