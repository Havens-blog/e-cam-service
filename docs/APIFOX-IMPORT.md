# Apifox 导入指南

## 方式一：导入 OpenAPI (Swagger) 文件

### 步骤

1. 打开 Apifox 客户端
2. 选择项目 -> 导入数据
3. 选择 "OpenAPI/Swagger"
4. 选择文件：`docs/swagger.yaml`
5. 点击"确认导入"

### 优点

- 标准格式，兼容性好
- 包含完整的 API 定义
- 支持自动更新

## 方式二：导入 Apifox 格式文件

### 步骤

1. 打开 Apifox 客户端
2. 选择项目 -> 导入数据
3. 选择 "Apifox 格式"
4. 选择文件：`docs/apifox-export.json`
5. 点击"确认导入"

### 优点

- Apifox 原生格式
- 包含示例数据
- 分组清晰

## 方式三：在线访问 Swagger UI

访问：http://localhost:8001/docs

可以直接在浏览器中查看和测试 API

## API 接口分组

### 1. 云账号管理

- POST /api/v1/cam/cloud-accounts - 创建云账号
- GET /api/v1/cam/cloud-accounts - 获取云账号列表
- GET /api/v1/cam/cloud-accounts/:id - 获取云账号详情
- PUT /api/v1/cam/cloud-accounts/:id - 更新云账号
- DELETE /api/v1/cam/cloud-accounts/:id - 删除云账号
- POST /api/v1/cam/cloud-accounts/:id/test-connection - 测试连接
- POST /api/v1/cam/cloud-accounts/:id/enable - 启用账号
- POST /api/v1/cam/cloud-accounts/:id/disable - 禁用账号
- POST /api/v1/cam/cloud-accounts/:id/sync - 同步资产

### 2. 资源同步

- POST /api/v1/cam/assets/discover - 发现云资产
- POST /api/v1/cam/assets/sync - 同步云资产

### 3. 云资产管理

- GET /api/v1/cam/assets - 获取资产列表
- GET /api/v1/cam/assets/:id - 获取资产详情
- POST /api/v1/cam/assets - 创建资产
- PUT /api/v1/cam/assets/:id - 更新资产
- DELETE /api/v1/cam/assets/:id - 删除资产
- GET /api/v1/cam/assets/statistics - 获取资产统计
- GET /api/v1/cam/assets/cost-analysis - 成本分析

### 4. 模型管理

- GET /api/v1/cam/models - 获取模型列表
- GET /api/v1/cam/models/:uid - 获取模型详情
- POST /api/v1/cam/models - 创建模型
- PUT /api/v1/cam/models/:uid - 更新模型
- DELETE /api/v1/cam/models/:uid - 删除模型

### 5. 字段管理

- GET /api/v1/cam/models/:uid/fields - 获取模型字段
- POST /api/v1/cam/models/:uid/fields - 添加字段
- PUT /api/v1/cam/fields/:field_uid - 更新字段
- DELETE /api/v1/cam/fields/:field_uid - 删除字段

### 6. 字段分组管理

- GET /api/v1/cam/models/:uid/field-groups - 获取字段分组
- POST /api/v1/cam/models/:uid/field-groups - 添加分组
- PUT /api/v1/cam/field-groups/:id - 更新分组
- DELETE /api/v1/cam/field-groups/:id - 删除分组

## 请求示例

### 创建云账号

```bash
curl -X POST http://localhost:8001/api/v1/cam/cloud-accounts \
  -H "Content-Type: application/json" \
  -d '{
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
      "supported_regions": ["cn-beijing", "cn-shanghai"],
      "supported_asset_types": ["ecs", "rds", "oss"]
    }
  }'
```

### 测试云账号连接

```bash
curl -X POST http://localhost:8001/api/v1/cam/cloud-accounts/1/test-connection
```

### 同步云账号资产

```bash
curl -X POST http://localhost:8001/api/v1/cam/cloud-accounts/1/sync \
  -H "Content-Type: application/json" \
  -d '{
    "asset_types": ["ecs", "rds"],
    "regions": ["cn-beijing", "cn-shanghai"]
  }'
```

### 发现云资产

```bash
curl -X POST http://localhost:8001/api/v1/cam/assets/discover \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "aliyun",
    "region": "cn-beijing"
  }'
```

### 获取资产列表

```bash
curl "http://localhost:8001/api/v1/cam/assets?provider=aliyun&asset_type=ecs&offset=0&limit=20"
```

### 获取资产统计

```bash
curl http://localhost:8001/api/v1/cam/assets/statistics
```

## 响应格式

所有接口统一返回格式：

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    // 具体数据
  }
}
```

### 错误码

- 0: 成功
- 1001: 参数错误
- 1002: 资产不存在
- 1003: 账号不存在
- 1004: 发现失败
- 5000: 系统错误

## 环境变量

### 开发环境

- Base URL: http://localhost:8001
- MongoDB: mongodb://localhost:27017/ecmdb

### 生产环境

- Base URL: https://api.example.com
- MongoDB: mongodb://prod-server:27017/ecmdb

## 认证

目前接口暂未启用认证，后续会添加 JWT Token 认证。

认证方式：

```
Authorization: Bearer <token>
```

## 更新日志

### v1.0.0 (2025-10-30)

- ✅ 云账号管理接口
- ✅ 资源同步接口
- ✅ 云资产管理接口
- ✅ 模型管理接口
- ✅ 字段管理接口
- ✅ 字段分组管理接口

## 联系方式

- 技术支持: support@example.com
- 文档地址: http://localhost:8001/docs
