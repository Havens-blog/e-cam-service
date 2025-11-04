# 阿里云资产同步功能使用指南 v2

## 功能概述

本功能实现了阿里云资产的自动发现和同步，支持：

- 从阿里云 API 发现云资产（支持多种资源类型）
- 将云资产信息同步到数据库
- 支持多地域并发同步
- 支持增量更新
- **支持指定资源类型，默认全量同步**

## 支持的资源类型

当前支持的资源类型：

- ✅ `ecs` - 云主机实例

计划支持的资源类型：

- 🚧 `rds` - 云数据库
- 🚧 `oss` - 对象存储
- 🚧 `slb` - 负载均衡
- 🚧 `cdn` - CDN 加速
- 🚧 `waf` - Web 应用防火墙

## API 接口

### 1. 发现云资产（不保存）

发现指定地域的云资产，但不保存到数据库。支持指定资源类型。

**请求:**

```http
POST /api/v1/cam/assets/discover
Content-Type: application/json

{
  "provider": "aliyun",
  "region": "cn-shenzhen",
  "asset_types": ["ecs"]  // 可选，不指定则发现所有支持的类型
}
```

**参数说明:**

- `provider` (必填): 云厂商，目前支持 `aliyun`
- `region` (可选): 地域，如 `cn-shenzhen`、`cn-beijing`
- `asset_types` (可选): 要发现的资源类型数组，不指定则发现所有支持的类型

**示例 - 发现所有类型:**

```json
{
  "provider": "aliyun",
  "region": "cn-shenzhen"
}
```

**示例 - 只发现 ECS:**

```json
{
  "provider": "aliyun",
  "region": "cn-shenzhen",
  "asset_types": ["ecs"]
}
```

**示例 - 发现多种类型:**

```json
{
  "provider": "aliyun",
  "region": "cn-shenzhen",
  "asset_types": ["ecs", "rds", "oss"]
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

同步指定云厂商的云资产到数据库。支持指定资源类型。

**请求:**

```http
POST /api/v1/cam/assets/sync
Content-Type: application/json

{
  "provider": "aliyun",
  "asset_types": ["ecs"]  // 可选，不指定则同步所有支持的类型
}
```

**参数说明:**

- `provider` (必填): 云厂商，目前支持 `aliyun`
- `asset_types` (可选): 要同步的资源类型数组，不指定则同步所有支持的类型

**示例 - 全量同步:**

```json
{
  "provider": "aliyun"
}
```

**示例 - 只同步 ECS:**

```json
{
  "provider": "aliyun",
  "asset_types": ["ecs"]
}
```

**示例 - 同步多种类型:**

```json
{
  "provider": "aliyun",
  "asset_types": ["ecs", "rds"]
}
```

**响应:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "asset_types": ["ecs"]
  }
}
```

### 3. 查询已同步的资产

查询数据库中已同步的资产列表。

**请求:**

```http
GET /api/v1/cam/assets?provider=aliyun&asset_type=ecs&region=cn-shenzhen&limit=20&offset=0
```

**参数说明:**

- `provider` (可选): 云厂商筛选
- `asset_type` (可选): 资产类型筛选
- `region` (可选): 地域筛选
- `status` (可选): 状态筛选
- `asset_name` (可选): 资产名称筛选
- `limit` (可选): 每页数量，默认 20
- `offset` (可选): 偏移量，默认 0

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

## 使用场景

### 场景 1: 全量同步所有资源

适用于首次同步或需要完整资产清单的场景。

```bash
curl -X POST http://localhost:8001/api/v1/cam/assets/sync \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aliyun"
  }'
```

### 场景 2: 只同步特定类型资源

适用于只关注某些资源类型的场景，可以减少同步时间和 API 调用。

```bash
# 只同步 ECS 实例
curl -X POST http://localhost:8001/api/v1/cam/assets/sync \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aliyun",
    "asset_types": ["ecs"]
  }'
```

### 场景 3: 同步多种资源类型

适用于需要同步多种但不是全部资源类型的场景。

```bash
# 同步 ECS 和 RDS
curl -X POST http://localhost:8001/api/v1/cam/assets/sync \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aliyun",
    "asset_types": ["ecs", "rds"]
  }'
```

### 场景 4: 先发现后同步

适用于需要先预览资产再决定是否同步的场景。

```bash
# 1. 先发现资产
curl -X POST http://localhost:8001/api/v1/cam/assets/discover \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aliyun",
    "region": "cn-shenzhen",
    "asset_types": ["ecs"]
  }'

# 2. 确认无误后再同步
curl -X POST http://localhost:8001/api/v1/cam/assets/sync \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aliyun",
    "asset_types": ["ecs"]
  }'
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
    "supported_asset_types": ["ecs", "rds", "oss"]
  }
}
```

### 2. 测试连接

测试云账号连接是否正常：

```http
POST /api/v1/cam/cloud-accounts/{id}/test-connection
```

### 3. 发现资产

先使用发现接口查看有哪些资产：

```http
POST /api/v1/cam/assets/discover
Content-Type: application/json

{
  "provider": "aliyun",
  "region": "cn-shenzhen",
  "asset_types": ["ecs"]
}
```

### 4. 同步资产

确认无误后，执行同步操作：

```http
POST /api/v1/cam/assets/sync
Content-Type: application/json

{
  "provider": "aliyun",
  "asset_types": ["ecs"]
}
```

### 5. 查询资产

查询已同步的资产：

```http
GET /api/v1/cam/assets?provider=aliyun&asset_type=ecs
```

## 配置说明

### 云账号配置

在云账号配置中，可以通过 `supported_asset_types` 字段限制该账号支持的资源类型：

```json
{
  "config": {
    "supported_asset_types": ["ecs", "rds"]
  }
}
```

这样即使同步时不指定 `asset_types`，也只会同步配置中指定的类型。

### 地域配置

通过 `supported_regions` 字段限制同步的地域范围：

```json
{
  "config": {
    "supported_regions": ["cn-beijing", "cn-shanghai"]
  }
}
```

## 同步策略

### 默认行为

- **不指定 asset_types**: 同步所有当前支持的资源类型（目前只有 ECS）
- **指定 asset_types**: 只同步指定的资源类型
- **不支持的类型**: 会记录警告日志并跳过

### 全量同步

```json
{
  "provider": "aliyun"
}
```

- 同步所有支持的资源类型
- 同步所有配置的地域
- 适合首次同步或定期全量更新

### 增量同步

```json
{
  "provider": "aliyun",
  "asset_types": ["ecs"]
}
```

- 只同步指定的资源类型
- 减少 API 调用次数
- 适合频繁更新特定资源

## 性能优化

### 1. 指定资源类型

只同步需要的资源类型，减少不必要的 API 调用：

```json
{
  "provider": "aliyun",
  "asset_types": ["ecs"] // 只同步 ECS
}
```

### 2. 限制地域范围

在云账号配置中限制地域：

```json
{
  "config": {
    "supported_regions": ["cn-shenzhen"] // 只同步深圳地域
  }
}
```

### 3. 调整同步间隔

设置合理的自动同步间隔：

```json
{
  "config": {
    "enable_auto_sync": true,
    "sync_interval": 3600 // 每小时同步一次
  }
}
```

## 注意事项

1. **API 限流**: 阿里云 API 有调用频率限制，建议：

   - 设置合理的同步间隔
   - 使用资源类型过滤减少调用次数
   - 限制地域范围

2. **权限要求**: AccessKey 需要有相应资源的只读权限

3. **成本**: 频繁调用 API 可能产生费用

4. **数据一致性**: 同步过程中可能存在短暂的数据不一致

5. **错误处理**:
   - 单个资源类型同步失败不会影响其他类型
   - 单个地域同步失败不会影响其他地域

## 故障排查

### 1. 同步失败

检查：

- 云账号凭证是否正确
- 云账号是否有足够的权限
- 指定的资源类型是否支持
- 网络连接是否正常

### 2. 部分资源未同步

检查：

- 云账号配置的 `supported_asset_types`
- 云账号配置的 `supported_regions`
- 日志中是否有错误信息

### 3. 性能问题

优化：

- 指定具体的资源类型
- 限制地域范围
- 减少同步频率
- 使用增量同步

## 扩展开发

### 添加新的资源类型

1. 在适配器中实现获取资源的方法
2. 在 `DiscoverAssets` 中添加 case 分支
3. 在 `syncRegionAssets` 中添加 case 分支
4. 实现资源到资产的转换方法

示例：

```go
// 在 adapter 中添加方法
func (a *AliyunAdapter) GetRDSInstances(ctx context.Context, region string) ([]RDSInstance, error)

// 在 service 中添加处理
case "rds":
    instances, err := adapter.GetRDSInstances(ctx, region)
    // 转换和保存逻辑
```

## 相关文档

- [API 文档](./swagger.yaml)
- [同步服务设计](./sync-service-design.md)
- [适配器设计](./sync-adapter-design.md)
- [实现总结](./ecs-sync-implementation.md)
