# CAM (Cloud Asset Management) API 设计

## 概述
CAM 模块提供多云资产统一管理功能，支持资产发现、管理、监控和成本分析。

## API 端点设计

### 1. 资产管理 API

#### 1.1 创建资产
```
POST /api/v1/cam/assets
Content-Type: application/json

{
  "asset_id": "i-1234567890abcdef0",
  "asset_name": "web-server-01",
  "asset_type": "ecs",
  "provider": "aliyun",
  "region": "cn-hangzhou",
  "zone": "cn-hangzhou-a",
  "status": "running",
  "tags": [
    {"key": "env", "value": "prod"},
    {"key": "team", "value": "backend"}
  ],
  "metadata": "{\"instance_type\":\"ecs.t5-lc1m1.small\",\"cpu\":1,\"memory\":1}",
  "cost": 0.045
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1
  }
}
```

#### 1.2 批量创建资产
```
POST /api/v1/cam/assets/batch
Content-Type: application/json

{
  "assets": [
    {
      "asset_id": "i-1234567890abcdef0",
      "asset_name": "web-server-01",
      "asset_type": "ecs",
      "provider": "aliyun",
      "region": "cn-hangzhou",
      "status": "running"
    }
  ]
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "count": 1
  }
}
```

#### 1.3 更新资产
```
PUT /api/v1/cam/assets
Content-Type: application/json

{
  "id": 1,
  "asset_name": "web-server-01-updated",
  "status": "stopped",
  "cost": 0.0
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": null
}
```

#### 1.4 获取资产详情
```
GET /api/v1/cam/assets/{id}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "asset_id": "i-1234567890abcdef0",
    "asset_name": "web-server-01",
    "asset_type": "ecs",
    "provider": "aliyun",
    "region": "cn-hangzhou",
    "zone": "cn-hangzhou-a",
    "status": "running",
    "tags": [
      {"key": "env", "value": "prod"}
    ],
    "metadata": "{}",
    "cost": 0.045,
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z",
    "discover_time": "2024-01-01T00:00:00Z"
  }
}
```

#### 1.5 获取资产列表
```
GET /api/v1/cam/assets?provider=aliyun&asset_type=ecs&region=cn-hangzhou&status=running&asset_name=web&offset=0&limit=20

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "assets": [
      {
        "id": 1,
        "asset_id": "i-1234567890abcdef0",
        "asset_name": "web-server-01",
        "asset_type": "ecs",
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "cost": 0.045
      }
    ],
    "total": 1
  }
}
```

#### 1.6 删除资产
```
DELETE /api/v1/cam/assets/{id}

Response:
{
  "code": 200,
  "msg": "success",
  "data": null
}
```

### 2. 资产发现 API

#### 2.1 发现资产
```
POST /api/v1/cam/discover
Content-Type: application/json

{
  "provider": "aliyun",
  "region": "cn-hangzhou"
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "assets": [...],
    "count": 10
  }
}
```

#### 2.2 同步资产
```
POST /api/v1/cam/sync
Content-Type: application/json

{
  "provider": "aliyun"
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": null
}
```

### 3. 统计分析 API

#### 3.1 获取资产统计
```
GET /api/v1/cam/statistics

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "total_assets": 100,
    "provider_stats": {
      "aliyun": 60,
      "aws": 30,
      "azure": 10
    },
    "asset_type_stats": {
      "ecs": 50,
      "rds": 30,
      "oss": 20
    },
    "region_stats": {
      "cn-hangzhou": 40,
      "cn-beijing": 30,
      "us-west-1": 30
    },
    "status_stats": {
      "running": 80,
      "stopped": 15,
      "terminated": 5
    },
    "total_cost": 1500.50,
    "last_discover_time": "2024-01-01T00:00:00Z"
  }
}
```

#### 3.2 获取成本分析
```
POST /api/v1/cam/cost-analysis
Content-Type: application/json

{
  "provider": "aliyun",
  "days": 30
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "provider": "aliyun",
    "total_cost": 900.30,
    "daily_costs": [
      {"date": "2024-01-01", "cost": 30.50},
      {"date": "2024-01-02", "cost": 29.80}
    ],
    "asset_costs": [
      {
        "asset_id": "i-1234567890abcdef0",
        "asset_name": "web-server-01",
        "asset_type": "ecs",
        "cost": 45.60
      }
    ],
    "region_costs": {
      "cn-hangzhou": 500.20,
      "cn-beijing": 400.10
    }
  }
}
```

### 4. 云平台账号管理

#### 4.1 创建云账号
```
POST /api/v1/cam/cloud-accounts
Content-Type: application/json

{
  "name": "aliyun-prod-account",
  "provider": "aliyun",
  "environment": "production",
  "access_key_id": "LTAI5tETYYVqFtAx1VBELjka",
  "access_key_secret": "your-secret-key",
  "region": "cn-hangzhou",
  "description": "生产环境阿里云账号",
  "config": {
    "enable_auto_sync": true,
    "sync_interval": 300,
    "read_only": false,
    "show_sub_accounts": true,
    "enable_cost_monitoring": true
  },
  "tenant_id": "d26ba0fc9a11426a89b3a9f15a9de1a1"
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "aliyun-prod-account",
    "provider": "aliyun",
    "environment": "production",
    "access_key_id": "LTAI5t***VqFtAx1VBELjka",
    "status": "active",
    "create_time": "2024-01-01T00:00:00Z"
  }
}
```

#### 4.2 获取云账号列表
```
GET /api/v1/cam/cloud-accounts?provider=aliyun&environment=production&status=active&offset=0&limit=20

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "accounts": [
      {
        "id": 1,
        "name": "aliyun-prod-account",
        "provider": "aliyun",
        "environment": "production",
        "access_key_id": "LTAI5t***VqFtAx1VBELjka",
        "region": "cn-hangzhou",
        "status": "active",
        "last_sync_time": "2024-01-01T12:00:00Z",
        "asset_count": 150,
        "create_time": "2024-01-01T00:00:00Z"
      }
    ],
    "total": 1
  }
}
```

#### 4.3 获取云账号详情
```
GET /api/v1/cam/cloud-accounts/{id}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "aliyun-prod-account",
    "provider": "aliyun",
    "environment": "production",
    "access_key_id": "LTAI5t***VqFtAx1VBELjka",
    "region": "cn-hangzhou",
    "description": "生产环境阿里云账号",
    "status": "active",
    "config": {
      "enable_auto_sync": true,
      "sync_interval": 300,
      "read_only": false,
      "show_sub_accounts": true,
      "enable_cost_monitoring": true
    },
    "last_sync_time": "2024-01-01T12:00:00Z",
    "asset_count": 150,
    "create_time": "2024-01-01T00:00:00Z",
    "update_time": "2024-01-01T00:00:00Z"
  }
}
```

#### 4.4 更新云账号
```
PUT /api/v1/cam/cloud-accounts/{id}
Content-Type: application/json

{
  "name": "aliyun-prod-account-updated",
  "description": "更新后的生产环境阿里云账号",
  "config": {
    "enable_auto_sync": false,
    "sync_interval": 600
  }
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": null
}
```

#### 4.5 测试云账号连接
```
POST /api/v1/cam/cloud-accounts/{id}/test-connection

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "status": "success",
    "message": "连接测试成功",
    "regions": ["cn-hangzhou", "cn-beijing", "cn-shanghai"],
    "test_time": "2024-01-01T12:00:00Z"
  }
}
```

#### 4.6 启用/禁用云账号
```
POST /api/v1/cam/cloud-accounts/{id}/enable
POST /api/v1/cam/cloud-accounts/{id}/disable

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "status": "active" // or "disabled"
  }
}
```

#### 4.7 删除云账号
```
DELETE /api/v1/cam/cloud-accounts/{id}

Response:
{
  "code": 200,
  "msg": "success",
  "data": null
}
```

#### 4.8 同步云账号资产
```
POST /api/v1/cam/cloud-accounts/{id}/sync
Content-Type: application/json

{
  "asset_types": ["ecs", "rds", "oss"],
  "regions": ["cn-hangzhou", "cn-beijing"]
}

Response:
{
  "code": 200,
  "msg": "success",
  "data": {
    "sync_id": "sync-123456",
    "status": "running",
    "start_time": "2024-01-01T12:00:00Z"
  }
}
```

## 错误码设计

| 错误码 | 错误信息 | 说明 |
|--------|----------|------|
| 200 | success | 成功 |
| 400 | params error | 参数错误 |
| 404001 | asset not found | 资产不存在 |
| 409001 | asset already exist | 资产已存在 |
| 400001 | provider not support | 云厂商不支持 |
| 400002 | asset type invalid | 资产类型无效 |
| 404002 | cloud account not found | 云账号不存在 |
| 409002 | cloud account already exist | 云账号已存在 |
| 400003 | cloud account config invalid | 云账号配置无效 |
| 401001 | cloud account auth failed | 云账号认证失败 |
| 500001 | asset discovery failed | 资产发现失败 |
| 500002 | cloud account connection failed | 云账号连接失败 |
| 500 | system error | 系统错误 |

## 支持的云厂商和资产类型

### 云厂商 (provider)
- `aliyun`: 阿里云
- `aws`: Amazon Web Services
- `azure`: Microsoft Azure
- `tencent`: 腾讯云
- `huawei`: 华为云

### 资产类型 (asset_type)
- `ecs`: 弹性计算服务
- `rds`: 关系型数据库
- `oss`: 对象存储
- `slb`: 负载均衡
- `vpc`: 虚拟私有云
- `eip`: 弹性公网IP
- `disk`: 云盘

### 资产状态 (status)
- `running`: 运行中
- `stopped`: 已停止
- `starting`: 启动中
- `stopping`: 停止中
- `terminated`: 已终止
- `unknown`: 未知状态