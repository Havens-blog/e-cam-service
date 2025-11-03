# 阿里云 ECS 同步功能使用指南

## 功能概述

本功能实现了阿里云 ECS 实例的自动发现和同步，支持：

- 从阿里云 API 发现 ECS 实例
- 将 ECS 实例信息同步到数据库
- 支持多地域并发同步
- 支持增量更新

## API 接口

### 1. 发现云资产（不保存）

发现指定地域的 ECS 实例，但不保存到数据库。

**请求:**

```http
POST /api/v1/cam/assets/discover
Content-Type: application/json

{
  "provider": "aliyun",
  "region": "cn-shenzhen"
}
```

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "assets": [
      {
        "id": 0,
        "asset_id": "i-wz9xxxxx",
        "asset_name": "test-ecs-01",
        "asset_type": "ecs",
        "provider": "aliyun",
        "region": "cn-shenzhen",
        "zone": "cn-shenzhen-a",
        "status": "Running",
        "tags": [{ "key": "env", "value": "prod" }],
        "metadata": "{...}",
        "cost": 0,
        "create_time": "2025-01-01T00:00:00Z",
        "update_time": "2025-01-01T00:00:00Z",
        "discover_time": "2025-10-30T17:00:00Z"
      }
    ],
    "count": 10
  }
}
```

### 2. 同步云资产（保存到数据库）

同步指定云厂商的所有 ECS 实例到数据库。

**请求:**

```http
POST /api/v1/cam/assets/sync
Content-Type: application/json

{
  "provider": "aliyun"
}
```

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

### 3. 查询已同步的资产

查询数据库中已同步的资产列表。

**请求:**

```http
GET /api/v1/cam/assets?provider=aliyun&asset_type=ecs&region=cn-shenzhen&limit=20&offset=0
```

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "assets": [...],
    "total": 100
  }
}
```

### 4. 获取资产统计

获取资产的统计信息。

**请求:**

```http
GET /api/v1/cam/assets/statistics
```

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "total_assets": 100,
    "provider_stats": {
      "aliyun": 100
    },
    "asset_type_stats": {
      "ecs": 100
    },
    "region_stats": {
      "cn-shenzhen": 50,
      "cn-beijing": 50
    },
    "status_stats": {
      "Running": 80,
      "Stopped": 20
    },
    "total_cost": 0,
    "last_discover_time": "2025-10-30T17:00:00Z"
  }
}
```

## 使用步骤

### 1. 创建云账号

首先需要创建一个阿里云账号配置：

```http
POST /api/v1/cam/cloud-accounts
Content-Type: application/json

{
  "name": "生产环境阿里云账号",
  "provider": "aliyun",
  "environment": "production",
  "access_key_id": "LTAI...",
  "access_key_secret": "xxx",
  "region": "cn-shenzhen",
  "description": "生产环境主账号",
  "config": {
    "enable_auto_sync": true,
    "sync_interval": 3600,
    "supported_regions": ["cn-beijing", "cn-shanghai", "cn-shenzhen"],
    "supported_asset_types": ["ecs"]
  }
}
```

### 2. 测试连接

测试云账号连接是否正常：

```http
POST /api/v1/cam/cloud-accounts/{id}/test-connection
```

### 3. 发现资产

先使用发现接口查看有哪些 ECS 实例：

```http
POST /api/v1/cam/assets/discover
Content-Type: application/json

{
  "provider": "aliyun",
  "region": "cn-shenzhen"
}
```

### 4. 同步资产

确认无误后，执行同步操作：

```http
POST /api/v1/cam/assets/sync
Content-Type: application/json

{
  "provider": "aliyun"
}
```

### 5. 查询资产

查询已同步的资产：

```http
GET /api/v1/cam/assets?provider=aliyun&asset_type=ecs
```

## 测试脚本

项目提供了测试脚本 `scripts/test_ecs_sync.go`，可以快速测试同步功能：

```bash
# 设置环境变量
export ALIYUN_ACCESS_KEY_ID="your_access_key_id"
export ALIYUN_ACCESS_KEY_SECRET="your_access_key_secret"
export MONGO_URI="mongodb://localhost:27017"

# 运行测试
go run scripts/test_ecs_sync.go
```

测试脚本会执行以下操作：

1. 创建测试云账号
2. 发现 ECS 实例（不保存）
3. 同步 ECS 实例到数据库
4. 查询已同步的资产
5. 获取资产统计信息

## 数据结构

### ECS 实例元数据

同步的 ECS 实例会将详细信息存储在 `metadata` 字段中（JSON 格式），包含：

```json
{
  "instance_id": "i-wz9xxxxx",
  "instance_name": "test-ecs-01",
  "status": "Running",
  "region": "cn-shenzhen",
  "zone": "cn-shenzhen-a",
  "instance_type": "ecs.g6.large",
  "instance_type_family": "g6",
  "cpu": 2,
  "memory": 8192,
  "os_type": "linux",
  "os_name": "CentOS 7.9 64位",
  "image_id": "centos_7_9_x64_20G_alibase_20210318.vhd",
  "public_ip": "47.xxx.xxx.xxx",
  "private_ip": "172.16.0.1",
  "vpc_id": "vpc-xxxxx",
  "vswitch_id": "vsw-xxxxx",
  "security_groups": ["sg-xxxxx"],
  "internet_max_bandwidth_in": 100,
  "internet_max_bandwidth_out": 5,
  "system_disk_category": "cloud_essd",
  "system_disk_size": 40,
  "data_disks": [],
  "charge_type": "PostPaid",
  "creation_time": "2025-01-01T00:00:00Z",
  "expired_time": "",
  "auto_renew": false,
  "auto_renew_period": 0,
  "io_optimized": "optimized",
  "network_type": "vpc",
  "instance_network_type": "vpc",
  "tags": {
    "env": "prod",
    "project": "test"
  },
  "description": "测试实例",
  "provider": "aliyun",
  "host_name": "test-ecs-01",
  "key_pair_name": ""
}
```

## 同步策略

### 全量同步

- 获取所有地域的 ECS 实例
- 对比数据库中的现有数据
- 新增不存在的实例
- 更新已存在的实例
- 标记已删除的实例（可选）

### 增量同步

- 只同步有变化的实例
- 基于最后同步时间
- 减少 API 调用次数

### 并发控制

- 支持多地域并发同步
- 默认并发数为 5
- 可通过配置调整

## 注意事项

1. **API 限流**: 阿里云 API 有调用频率限制，建议设置合理的同步间隔
2. **权限要求**: AccessKey 需要有 ECS 的只读权限
3. **成本**: 频繁调用 API 可能产生费用
4. **数据一致性**: 同步过程中可能存在短暂的数据不一致
5. **错误处理**: 单个地域同步失败不会影响其他地域

## 后续扩展

- [ ] 支持更多资源类型（RDS、OSS、SLB 等）
- [ ] 支持 AWS、Azure 等其他云厂商
- [ ] 实现资源变更通知
- [ ] 添加成本分析功能
- [ ] 支持资源标签管理
- [ ] 实现资源生命周期管理

## 故障排查

### 1. 同步失败

检查：

- 云账号凭证是否正确
- 云账号是否有足够的权限
- 网络连接是否正常
- MongoDB 是否正常运行

### 2. 数据不完整

检查：

- 是否所有地域都同步成功
- 日志中是否有错误信息
- API 调用是否被限流

### 3. 性能问题

优化：

- 减少同步频率
- 限制同步的地域范围
- 调整并发数
- 使用增量同步

## 相关文档

- [API 文档](./swagger.yaml)
- [同步服务设计](./sync-service-design.md)
- [适配器设计](./sync-adapter-design.md)
