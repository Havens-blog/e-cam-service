# 审计日志与变更历史 API 文档

## 概述

审计模块提供两大功能：

1. API 操作审计日志 - 记录所有写操作（POST/PUT/PATCH/DELETE）的审计日志
2. 资产变更历史 - 追踪云资产同步过程中的字段级变更

## 通用说明

### 请求头

| 参数         | 类型   | 必填 | 说明                       |
| ------------ | ------ | ---- | -------------------------- |
| X-Tenant-ID  | string | 是   | 租户ID                     |
| X-Request-ID | string | 否   | 请求追踪ID，不传则自动生成 |

### 响应格式

```json
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

### 错误响应

```json
{
  "code": 400,
  "msg": "参数错误: tenant_id 不能为空"
}
```

---

## 一、审计日志 API

基础路径: `/api/v1/cam/audit`

### 1.1 查询审计日志列表

`GET /api/v1/cam/audit/logs`

#### 查询参数

| 参数           | 类型   | 必填 | 说明                              |
| -------------- | ------ | ---- | --------------------------------- |
| operation_type | string | 否   | 操作类型，如 `api_account_create` |
| operator_id    | string | 否   | 操作人ID                          |
| http_method    | string | 否   | HTTP方法 (POST/PUT/PATCH/DELETE)  |
| api_path       | string | 否   | API路径前缀匹配                   |
| request_id     | string | 否   | 请求ID精确匹配                    |
| status_code    | int    | 否   | HTTP状态码                        |
| start_time     | int64  | 否   | 开始时间（毫秒时间戳）            |
| end_time       | int64  | 否   | 结束时间（毫秒时间戳）            |
| offset         | int    | 否   | 偏移量，默认 0                    |
| limit          | int    | 否   | 每页数量，默认 20                 |

#### 请求示例

```bash
curl -X GET "http://localhost:8080/api/v1/cam/audit/logs?http_method=POST&offset=0&limit=10" \
  -H "X-Tenant-ID: tenant-001"
```

#### 响应示例

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "operation_type": "api_account_create",
        "operator_id": "100",
        "operator_name": "admin",
        "tenant_id": "tenant-001",
        "http_method": "POST",
        "api_path": "/api/v1/cam/accounts",
        "request_body": "{\"name\":\"aliyun-prod\",\"provider\":\"aliyun\",\"access_key\":\"***\"}",
        "status_code": 200,
        "result": "success",
        "request_id": "550e8400-e29b-41d4-a716-446655440000",
        "duration_ms": 125,
        "client_ip": "192.168.1.100",
        "user_agent": "Mozilla/5.0",
        "ctime": 1706000000000
      }
    ],
    "total": 156
  }
}
```

### 1.2 导出审计日志

`GET /api/v1/cam/audit/logs/export`

支持 CSV 和 JSON 两种导出格式。

#### 查询参数

与查询列表接口相同，额外支持：

| 参数   | 类型   | 必填 | 说明                             |
| ------ | ------ | ---- | -------------------------------- |
| format | string | 否   | 导出格式: `csv`（默认）或 `json` |

#### 请求示例

```bash
# 导出 CSV
curl -X GET "http://localhost:8080/api/v1/cam/audit/logs/export?format=csv&start_time=1706000000000" \
  -H "X-Tenant-ID: tenant-001" \
  -o audit_logs.csv

# 导出 JSON
curl -X GET "http://localhost:8080/api/v1/cam/audit/logs/export?format=json" \
  -H "X-Tenant-ID: tenant-001" \
  -o audit_logs.json
```

#### 响应说明

- CSV 格式: `Content-Type: text/csv`，`Content-Disposition: attachment; filename=audit_logs.csv`
- JSON 格式: `Content-Type: application/json`，`Content-Disposition: attachment; filename=audit_logs.json`

### 1.3 生成审计报告

`POST /api/v1/cam/audit/reports`

生成指定时间范围内的审计统计报告。

#### 请求体

```json
{
  "start_time": 1706000000000,
  "end_time": 1706100000000
}
```

#### 请求示例

```bash
curl -X POST "http://localhost:8080/api/v1/cam/audit/reports" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{"start_time": 1706000000000, "end_time": 1706100000000}'
```

#### 响应示例

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "tenant_id": "tenant-001",
    "start_time": 1706000000000,
    "end_time": 1706100000000,
    "total_operations": 1250,
    "success_count": 1200,
    "failed_count": 50,
    "by_operation_type": {
      "api_account_create": 15,
      "api_account_update": 30,
      "api_asset_sync": 200,
      "api_task_create": 50
    },
    "by_http_method": {
      "POST": 500,
      "PUT": 400,
      "DELETE": 100,
      "PATCH": 250
    },
    "top_endpoints": [
      { "path": "/api/v1/cam/accounts", "method": "POST", "count": 150 },
      { "path": "/api/v1/cam/tasks", "method": "POST", "count": 120 }
    ],
    "top_operators": [
      { "operator_id": "100", "operator_name": "admin", "count": 800 },
      { "operator_id": "101", "operator_name": "ops-user", "count": 450 }
    ]
  }
}
```

---

## 二、资产变更历史 API

基础路径: `/api/v1/cam/audit`

### 2.1 查询资产变更历史

`GET /api/v1/cam/audit/changes`

查询指定资产的字段级变更记录。

#### 查询参数

| 参数       | 类型   | 必填 | 说明                    |
| ---------- | ------ | ---- | ----------------------- |
| asset_id   | string | 是   | 资产ID（如 `i-bp1xxx`） |
| field_name | string | 否   | 变更字段名过滤          |
| start_time | int64  | 否   | 开始时间（毫秒时间戳）  |
| end_time   | int64  | 否   | 结束时间（毫秒时间戳）  |
| offset     | int    | 否   | 偏移量，默认 0          |
| limit      | int    | 否   | 每页数量，默认 20       |

#### 请求示例

```bash
curl -X GET "http://localhost:8080/api/v1/cam/audit/changes?asset_id=i-bp1xxx&offset=0&limit=10" \
  -H "X-Tenant-ID: tenant-001"
```

#### 响应示例

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "asset_id": "i-bp1xxx",
        "asset_name": "web-server-01",
        "model_uid": "cloud_vm",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "field_name": "status",
        "old_value": "\"stopped\"",
        "new_value": "\"running\"",
        "change_source": "sync_task",
        "change_task_id": "",
        "ctime": 1706000000000
      },
      {
        "id": 2,
        "asset_id": "i-bp1xxx",
        "asset_name": "web-server-01",
        "model_uid": "cloud_vm",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "field_name": "cpu",
        "old_value": "2",
        "new_value": "4",
        "change_source": "sync_task",
        "change_task_id": "",
        "ctime": 1706000000000
      }
    ],
    "total": 25
  }
}
```

### 2.2 获取变更统计汇总

`GET /api/v1/cam/audit/changes/summary`

获取资产变更的统计汇总信息。

#### 查询参数

| 参数       | 类型   | 必填 | 说明                                       |
| ---------- | ------ | ---- | ------------------------------------------ |
| model_uid  | string | 否   | 资产模型UID（如 `cloud_vm`、`cloud_rds`）  |
| provider   | string | 否   | 云厂商 (aliyun/aws/huawei/tencent/volcano) |
| start_time | int64  | 否   | 开始时间（毫秒时间戳）                     |
| end_time   | int64  | 否   | 结束时间（毫秒时间戳）                     |

#### 请求示例

```bash
curl -X GET "http://localhost:8080/api/v1/cam/audit/changes/summary?start_time=1706000000000" \
  -H "X-Tenant-ID: tenant-001"
```

#### 响应示例

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "total": 580,
    "by_resource_type": {
      "cloud_vm": 200,
      "cloud_rds": 150,
      "cloud_redis": 80,
      "cloud_vpc": 50,
      "cloud_eip": 100
    },
    "by_field": {
      "status": 180,
      "cpu": 50,
      "memory": 45,
      "private_ip": 30,
      "tags": 120
    },
    "by_provider": {
      "aliyun": 300,
      "aws": 150,
      "huawei": 80,
      "tencent": 50
    }
  }
}
```

---

## 三、操作类型说明

审计中间件根据 API 路径和 HTTP 方法自动推断操作类型，格式为 `api_{resource}_{action}`：

| 操作类型             | 说明                       |
| -------------------- | -------------------------- |
| `api_account_create` | 创建云账号                 |
| `api_account_update` | 更新云账号                 |
| `api_account_delete` | 删除云账号                 |
| `api_asset_sync`     | 同步资产（路径包含 /sync） |
| `api_task_create`    | 创建任务                   |
| `api_generic`        | 其他通用操作               |

## 四、敏感字段脱敏

审计日志中的请求体会自动对以下字段进行脱敏处理（值替换为 `***`）：

- `password`
- `secret_key`
- `access_key`
- `secret_id`
- `access_key_secret`

## 五、变更追踪忽略字段

以下字段在资产同步时不会记录变更（每次同步都会变化的瞬态字段）：

- `sync_time`
- `update_time`
- `utime`
