# 仪表盘 API 文档

## 概述

仪表盘 API 提供云资产的统计聚合数据，用于前端 Dashboard 页面展示资产分布、趋势和告警信息。

所有接口需要 `X-Tenant-ID` 请求头。

## 基础信息

- 基础路径: `/api/v1/cam/dashboard`
- 认证方式: `X-Tenant-ID` Header (必填)
- 响应格式: JSON

---

## 1. 资产总览

获取资产总数及按云厂商、类型、状态的分布统计。适合 Dashboard 首屏展示。

```
GET /api/v1/cam/dashboard/overview
```

### 请求示例

```bash
curl -X GET "http://localhost:8080/api/v1/cam/dashboard/overview" \
  -H "X-Tenant-ID: tenant-001"
```

### 响应示例

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "total": 1256,
    "by_provider": [
      { "key": "aliyun", "count": 580 },
      { "key": "aws", "count": 320 },
      { "key": "huawei", "count": 200 },
      { "key": "tencent", "count": 100 },
      { "key": "volcano", "count": 56 }
    ],
    "by_type": [
      { "key": "cloud_vm", "count": 450 },
      { "key": "cloud_rds", "count": 120 },
      { "key": "cloud_redis", "count": 80 },
      { "key": "cloud_vpc", "count": 200 },
      { "key": "cloud_eip", "count": 150 },
      { "key": "cloud_oss", "count": 60 }
    ],
    "by_status": [
      { "key": "running", "count": 900 },
      { "key": "stopped", "count": 200 },
      { "key": "Available", "count": 156 }
    ]
  }
}
```

### 前端建议

- `by_provider`: 饼图或环形图展示云厂商分布
- `by_type`: 柱状图展示资产类型分布
- `by_status`: 状态卡片或标签展示
- `total`: 大数字卡片展示

---

## 2. 按云厂商统计

```
GET /api/v1/cam/dashboard/by-provider
```

### 响应示例

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "items": [
      { "key": "aliyun", "count": 580 },
      { "key": "aws", "count": 320 },
      { "key": "huawei", "count": 200 },
      { "key": "tencent", "count": 100 },
      { "key": "volcano", "count": 56 }
    ]
  }
}
```

### key 值说明

| key     | 云厂商   |
| ------- | -------- |
| aliyun  | 阿里云   |
| aws     | AWS      |
| huawei  | 华为云   |
| tencent | 腾讯云   |
| volcano | 火山引擎 |

---

## 3. 按地域统计

```
GET /api/v1/cam/dashboard/by-region
```

### 响应示例

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "items": [
      { "key": "cn-hangzhou", "count": 300 },
      { "key": "cn-beijing", "count": 250 },
      { "key": "us-east-1", "count": 180 },
      { "key": "cn-shanghai", "count": 150 },
      { "key": "ap-guangzhou", "count": 100 }
    ]
  }
}
```

### 前端建议

- 可用地图组件 (如 ECharts 地图) 展示地域分布
- 地域 key 为云厂商原始地域ID，前端可维护一份地域名称映射表

---

## 4. 按资产类型统计

```
GET /api/v1/cam/dashboard/by-asset-type
```

### 响应示例

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "items": [
      { "key": "cloud_vm", "count": 450 },
      { "key": "cloud_vpc", "count": 200 },
      { "key": "cloud_eip", "count": 150 },
      { "key": "cloud_rds", "count": 120 },
      { "key": "cloud_redis", "count": 80 },
      { "key": "cloud_oss", "count": 60 },
      { "key": "cloud_mongodb", "count": 40 }
    ]
  }
}
```

### key 值说明 (model_uid)

| key                  | 资产类型     |
| -------------------- | ------------ |
| cloud_vm / \*\_ecs   | 云虚拟机     |
| cloud_rds / \*\_rds  | 关系型数据库 |
| cloud_redis          | Redis 缓存   |
| cloud_mongodb        | MongoDB      |
| cloud_vpc / \*\_vpc  | 虚拟私有云   |
| cloud_eip / \*\_eip  | 弹性公网IP   |
| cloud_nas / \*\_nas  | 文件存储     |
| cloud_oss / \*\_oss  | 对象存储     |
| cloud_kafka          | 消息队列     |
| cloud_elasticsearch  | 搜索服务     |
| cloud_disk           | 云盘         |
| cloud_snapshot       | 快照         |
| cloud_security_group | 安全组       |

前端可将 `model_uid` 映射为中文显示名称。

---

## 5. 按云账号统计

```
GET /api/v1/cam/dashboard/by-account
```

### 响应示例

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "items": [
      { "key": "1", "count": 400 },
      { "key": "2", "count": 350 },
      { "key": "3", "count": 200 }
    ]
  }
}
```

> `key` 为云账号ID (字符串格式)。前端可调用 `/api/v1/cam/accounts` 接口获取账号名称进行关联展示。

---

## 6. 即将过期的资源

查询指定天数内即将过期的云资源列表。

```
GET /api/v1/cam/dashboard/expiring
```

### 请求参数

| 参数   | 类型 | 必填 | 默认值 | 说明         |
| ------ | ---- | ---- | ------ | ------------ |
| days   | int  | 否   | 30     | 过期天数范围 |
| offset | int  | 否   | 0      | 偏移量       |
| limit  | int  | 否   | 20     | 每页数量     |

### 请求示例

```bash
curl -X GET "http://localhost:8080/api/v1/cam/dashboard/expiring?days=7&limit=10" \
  -H "X-Tenant-ID: tenant-001"
```

### 响应示例

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 101,
        "asset_id": "i-bp1abc123",
        "asset_name": "web-server-01",
        "asset_type": "ecs",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "attributes": {
          "expire_time": "2026-03-05T00:00:00Z",
          "instance_type": "ecs.c6.xlarge",
          "charge_type": "PrePaid"
        },
        "create_time": 1706000000000,
        "update_time": 1708000000000
      }
    ],
    "total": 5
  }
}
```

### 前端建议

- 用表格展示即将过期资源，按过期时间排序 (最近的在前)
- 高亮 7 天内过期的资源 (红色)，30 天内的用黄色
- `attributes.expire_time` 为 RFC3339 格式的过期时间
- `attributes.charge_type` 为 `PrePaid` 表示包年包月资源

---

## 统一响应格式

### 成功响应

```json
{
  "code": 200,
  "msg": "success",
  "data": { ... }
}
```

### 错误响应

```json
{
  "code": 400,
  "message": "租户ID不能为空"
}
```

---

## 前端页面结构建议

```
Dashboard 页面
├── 顶部统计卡片
│   ├── 资产总数 (overview.total)
│   ├── 云厂商数量 (overview.by_provider.length)
│   └── 即将过期数量 (expiring.total)
├── 云厂商分布 (饼图/环形图)
│   └── GET /dashboard/by-provider
├── 资产类型分布 (柱状图)
│   └── GET /dashboard/by-asset-type
├── 地域分布 (地图/柱状图)
│   └── GET /dashboard/by-region
├── 状态分布 (标签/卡片)
│   └── overview.by_status
└── 即将过期资源 (表格)
    └── GET /dashboard/expiring?days=30
```
