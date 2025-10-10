# CAM 数据库表设计

## 1. 云资产表 (cloud_assets)

### 表结构
```sql
-- MongoDB Collection: cloud_assets
{
  "_id": ObjectId,
  "id": NumberLong,              // 自增ID
  "asset_id": String,            // 云厂商资产ID (唯一)
  "asset_name": String,          // 资产名称
  "asset_type": String,          // 资产类型 (ecs, rds, oss, etc.)
  "provider": String,            // 云厂商 (aliyun, aws, azure)
  "region": String,              // 地域
  "zone": String,                // 可用区
  "status": String,              // 资产状态
  "tags": [                      // 标签数组
    {
      "key": String,
      "value": String
    }
  ],
  "metadata": String,            // 元数据 (JSON字符串)
  "cost": Double,                // 成本
  "create_time": Date,           // 创建时间
  "update_time": Date,           // 更新时间
  "discover_time": Date,         // 发现时间
  "ctime": NumberLong,           // 创建时间戳
  "utime": NumberLong            // 更新时间戳
}
```

## 2. 云账号表 (cloud_accounts)

### 表结构
```sql
-- MongoDB Collection: cloud_accounts
{
  "_id": ObjectId,
  "id": NumberLong,              // 自增ID
  "name": String,                // 账号名称
  "provider": String,            // 云厂商 (aliyun, aws, azure)
  "environment": String,         // 环境 (production, staging, development)
  "access_key_id": String,       // 访问密钥ID
  "access_key_secret": String,   // 访问密钥Secret (加密存储)
  "region": String,              // 默认地域
  "description": String,         // 描述信息
  "status": String,              // 账号状态 (active, disabled, error)
  "config": {                    // 配置信息
    "enable_auto_sync": Boolean,     // 是否启用自动同步
    "sync_interval": NumberLong,     // 同步间隔(秒)
    "read_only": Boolean,            // 只读权限
    "show_sub_accounts": Boolean,    // 显示子账号
    "enable_cost_monitoring": Boolean, // 启用成本监控
    "supported_regions": [String],   // 支持的地域列表
    "supported_asset_types": [String] // 支持的资产类型
  },
  "tenant_id": String,           // 租户ID
  "last_sync_time": Date,        // 最后同步时间
  "last_test_time": Date,        // 最后测试时间
  "asset_count": NumberLong,     // 资产数量
  "error_message": String,       // 错误信息
  "create_time": Date,           // 创建时间
  "update_time": Date,           // 更新时间
}
```

### 索引设计
```javascript
// 1. 唯一索引 - 账号名称和租户ID
db.cloud_accounts.createIndex(
  { "name": 1, "tenant_id": 1 }, 
  { unique: true, name: "idx_name_tenant" }
)

// 2. 复合索引 - 云厂商和环境
db.cloud_accounts.createIndex(
  { "provider": 1, "environment": 1 }, 
  { name: "idx_provider_env" }
)

// 3. 单字段索引 - 状态
db.cloud_accounts.createIndex(
  { "status": 1 }, 
  { name: "idx_status" }
)

// 4. 单字段索引 - 租户ID
db.cloud_accounts.createIndex(
  { "tenant_id": 1 }, 
  { name: "idx_tenant_id" }
)

// 5. 时间索引 - 创建时间倒序
db.cloud_accounts.createIndex(
  { "ctime": -1 }, 
  { name: "idx_ctime_desc" }
)

// 6. 复合索引 - 启用状态和自动同步
db.cloud_accounts.createIndex(
  { "status": 1, "config.enable_auto_sync": 1 }, 
  { name: "idx_status_auto_sync" }
)
```

### 索引设计
```javascript
// 1. 唯一索引 - 资产ID
db.cloud_assets.createIndex(
  { "asset_id": 1 }, 
  { unique: true, name: "idx_asset_id" }
)

// 2. 复合索引 - 云厂商和资产类型
db.cloud_assets.createIndex(
  { "provider": 1, "asset_type": 1 }, 
  { name: "idx_provider_type" }
)

// 3. 单字段索引 - 地域
db.cloud_assets.createIndex(
  { "region": 1 }, 
  { name: "idx_region" }
)

// 4. 单字段索引 - 状态
db.cloud_assets.createIndex(
  { "status": 1 }, 
  { name: "idx_status" }
)

// 5. 文本索引 - 资产名称搜索
db.cloud_assets.createIndex(
  { "asset_name": "text" }, 
  { name: "idx_asset_name_text" }
)

// 6. 时间索引 - 创建时间倒序
db.cloud_assets.createIndex(
  { "ctime": -1 }, 
  { name: "idx_ctime_desc" }
)

// 7. 复合索引 - 云厂商和地域
db.cloud_assets.createIndex(
  { "provider": 1, "region": 1 }, 
  { name: "idx_provider_region" }
)

// 8. 复合索引 - 资产类型和状态
db.cloud_assets.createIndex(
  { "asset_type": 1, "status": 1 }, 
  { name: "idx_type_status" }
)
```

## 3. 账号同步历史表 (account_sync_history)

### 表结构
```sql
-- MongoDB Collection: account_sync_history
{
  "_id": ObjectId,
  "id": NumberLong,              // 自增ID
  "account_id": NumberLong,      // 云账号ID
  "sync_id": String,             // 同步任务ID
  "provider": String,            // 云厂商
  "sync_type": String,           // 同步类型 (manual, scheduled, auto)
  "status": String,              // 同步状态 (running, success, failed, partial)
  "asset_types": [String],       // 同步的资产类型
  "regions": [String],           // 同步的地域
  "total_found": NumberLong,     // 发现总数
  "total_new": NumberLong,       // 新增数量
  "total_updated": NumberLong,   // 更新数量
  "total_deleted": NumberLong,   // 删除数量
  "error_message": String,       // 错误信息
  "start_time": Date,            // 开始时间
  "end_time": Date,              // 结束时间
  "duration": NumberLong,        // 持续时间(秒)
  "ctime": NumberLong,           // 创建时间戳
  "utime": NumberLong            // 更新时间戳
}
```

### 索引设计
```javascript
// 1. 复合索引 - 账号ID和开始时间
db.account_sync_history.createIndex(
  { "account_id": 1, "start_time": -1 }, 
  { name: "idx_account_start_time" }
)

// 2. 单字段索引 - 同步ID
db.account_sync_history.createIndex(
  { "sync_id": 1 }, 
  { name: "idx_sync_id" }
)

// 3. 单字段索引 - 状态
db.account_sync_history.createIndex(
  { "status": 1 }, 
  { name: "idx_status" }
)
```

## 4. 资产发现历史表 (asset_discovery_history)

### 表结构
```sql
-- MongoDB Collection: asset_discovery_history
{
  "_id": ObjectId,
  "id": NumberLong,              // 自增ID
  "provider": String,            // 云厂商
  "region": String,              // 地域
  "discovery_type": String,      // 发现类型 (manual, scheduled)
  "status": String,              // 发现状态 (running, success, failed)
  "total_found": NumberLong,     // 发现总数
  "total_new": NumberLong,       // 新增数量
  "total_updated": NumberLong,   // 更新数量
  "error_message": String,       // 错误信息
  "start_time": Date,            // 开始时间
  "end_time": Date,              // 结束时间
  "duration": NumberLong,        // 持续时间(秒)
  "ctime": NumberLong,           // 创建时间戳
  "utime": NumberLong            // 更新时间戳
}
```

### 索引设计
```javascript
// 1. 复合索引 - 云厂商和开始时间
db.asset_discovery_history.createIndex(
  { "provider": 1, "start_time": -1 }, 
  { name: "idx_provider_start_time" }
)

// 2. 单字段索引 - 状态
db.asset_discovery_history.createIndex(
  { "status": 1 }, 
  { name: "idx_status" }
)

// 3. 时间索引 - 创建时间倒序
db.asset_discovery_history.createIndex(
  { "ctime": -1 }, 
  { name: "idx_ctime_desc" }
)
```

## 5. 资产成本历史表 (asset_cost_history)

### 表结构
```sql
-- MongoDB Collection: asset_cost_history
{
  "_id": ObjectId,
  "id": NumberLong,              // 自增ID
  "asset_id": String,            // 云厂商资产ID
  "provider": String,            // 云厂商
  "region": String,              // 地域
  "asset_type": String,          // 资产类型
  "cost": Double,                // 成本
  "currency": String,            // 货币单位 (CNY, USD)
  "billing_date": Date,          // 计费日期
  "billing_cycle": String,       // 计费周期 (daily, monthly)
  "ctime": NumberLong,           // 创建时间戳
  "utime": NumberLong            // 更新时间戳
}
```

### 索引设计
```javascript
// 1. 复合索引 - 资产ID和计费日期
db.asset_cost_history.createIndex(
  { "asset_id": 1, "billing_date": -1 }, 
  { name: "idx_asset_billing_date" }
)

// 2. 复合索引 - 云厂商和计费日期
db.asset_cost_history.createIndex(
  { "provider": 1, "billing_date": -1 }, 
  { name: "idx_provider_billing_date" }
)

// 3. 复合索引 - 地域和计费日期
db.asset_cost_history.createIndex(
  { "region": 1, "billing_date": -1 }, 
  { name: "idx_region_billing_date" }
)
```

## 6. 数据字典

### 云厂商 (provider)
| 值 | 说明 |
|----|------|
| aliyun | 阿里云 |
| aws | Amazon Web Services |
| azure | Microsoft Azure |
| tencent | 腾讯云 |
| huawei | 华为云 |

### 资产类型 (asset_type)
| 值 | 说明 |
|----|------|
| ecs | 弹性计算服务 |
| rds | 关系型数据库 |
| oss | 对象存储 |
| slb | 负载均衡 |
| vpc | 虚拟私有云 |
| eip | 弹性公网IP |
| disk | 云盘 |

### 资产状态 (status)
| 值 | 说明 |
|----|------|
| running | 运行中 |
| stopped | 已停止 |
| starting | 启动中 |
| stopping | 停止中 |
| terminated | 已终止 |
| unknown | 未知状态 |

### 发现类型 (discovery_type)
| 值 | 说明 |
|----|------|
| manual | 手动发现 |
| scheduled | 定时发现 |

### 发现状态 (discovery_status)
| 值 | 说明 |
|----|------|
| running | 运行中 |
| success | 成功 |
| failed | 失败 |

### 计费周期 (billing_cycle)
| 值 | 说明 |
|----|------|
| daily | 按日计费 |
| monthly | 按月计费 |

### 账号状态 (account_status)
| 值 | 说明 |
|----|------|
| active | 活跃状态 |
| disabled | 已禁用 |
| error | 错误状态 |
| testing | 测试中 |

### 同步类型 (sync_type)
| 值 | 说明 |
|----|------|
| manual | 手动同步 |
| scheduled | 定时同步 |
| auto | 自动同步 |

### 同步状态 (sync_status)
| 值 | 说明 |
|----|------|
| running | 运行中 |
| success | 成功 |
| failed | 失败 |
| partial | 部分成功 |

## 7. 查询优化建议

### 常用查询模式
1. **按云厂商和资产类型查询**: 使用 `idx_provider_type` 索引
2. **按地域查询**: 使用 `idx_region` 索引
3. **按状态查询**: 使用 `idx_status` 索引
4. **资产名称模糊搜索**: 使用 `idx_asset_name_text` 文本索引
5. **时间范围查询**: 使用 `idx_ctime_desc` 索引

### 分页查询优化
```javascript
// 推荐使用基于时间的分页
db.cloud_assets.find({
  "provider": "aliyun",
  "ctime": { $lt: last_ctime }
}).sort({ "ctime": -1 }).limit(20)
```

### 聚合查询示例
```javascript
// 统计各云厂商资产数量
db.cloud_assets.aggregate([
  {
    $group: {
      _id: "$provider",
      count: { $sum: 1 },
      total_cost: { $sum: "$cost" }
    }
  }
])

// 统计各地域资产分布
db.cloud_assets.aggregate([
  {
    $group: {
      _id: { provider: "$provider", region: "$region" },
      count: { $sum: 1 }
    }
  }
])
```