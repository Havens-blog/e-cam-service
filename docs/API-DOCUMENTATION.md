# API 文档说明

## 概述

E-CAM Service 提供完整的 RESTful API 文档，支持自动生成和在线查看。

## 访问 API 文档

### 方式 1: Swagger UI（推荐）

启动服务后，访问：

```
http://localhost:8001/docs
```

或

```
http://localhost:8001/api-docs
```

### 方式 2: 查看 YAML 文件

直接查看 OpenAPI 规范文件：

```
docs/swagger.yaml
```

## 文档特性

### ✅ 已实现

1. **完整的 API 定义**

   - 所有接口的请求和响应格式
   - 参数说明和类型定义
   - 错误码说明

2. **交互式文档**

   - 在线测试 API
   - 查看请求示例
   - 查看响应示例

3. **数据模型定义**

   - 完整的数据结构
   - 字段类型和约束
   - 示例数据

4. **认证说明**
   - JWT Token 认证
   - 请求头格式

### 🔄 自动生成（可选）

使用 `swag` 工具可以从代码注释自动生成文档：

#### 安装 swag

```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

#### 添加注释

在 Handler 方法上添加 Swagger 注释：

```go
// CreateModel 创建模型
// @Summary 创建模型
// @Description 创建新的资源模型
// @Tags 模型管理
// @Accept json
// @Produce json
// @Param request body CreateModelReq true "创建模型请求"
// @Success 200 {object} ginx.Result{data=ModelVO}
// @Failure 400 {object} ginx.Result
// @Router /api/v1/cam/models [post]
// @Security BearerAuth
func (h *Handler) CreateModel(ctx *gin.Context, req CreateModelReq) (ginx.Result, error) {
    // ...
}
```

#### 生成文档

```bash
swag init -g docs/docs.go -o docs/swagger
```

## API 模块

### 1. 云资产管理

#### 获取资产列表

```http
GET /api/v1/cam/assets?provider=aliyun&page=1&page_size=20
```

**查询参数：**

- `provider`: 云厂商（aliyun/aws/azure）
- `model_uid`: 模型 UID
- `region`: 地域
- `page`: 页码（默认 1）
- `page_size`: 每页数量（默认 20）

**响应示例：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "list": [
      {
        "id": 1,
        "uid": "i-xxx",
        "name": "web-server-01",
        "model_uid": "cloud_ecs",
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "properties": {
          "instance_type": "ecs.g6.large",
          "cpu": 2,
          "memory": 8192
        },
        "tags": {
          "env": "production"
        }
      }
    ],
    "total": 100
  }
}
```

#### 创建资产

```http
POST /api/v1/cam/assets
Content-Type: application/json

{
  "uid": "i-xxx",
  "name": "web-server-01",
  "model_uid": "cloud_ecs",
  "provider": "aliyun",
  "region": "cn-hangzhou",
  "properties": {
    "instance_type": "ecs.g6.large",
    "cpu": 2,
    "memory": 8192
  },
  "tags": {
    "env": "production"
  }
}
```

#### 获取资产详情

```http
GET /api/v1/cam/assets/{id}
```

#### 更新资产

```http
PUT /api/v1/cam/assets/{id}
Content-Type: application/json

{
  "name": "web-server-01-updated",
  "properties": {
    "cpu": 4
  }
}
```

#### 删除资产

```http
DELETE /api/v1/cam/assets/{id}
```

#### 批量创建资产

```http
POST /api/v1/cam/assets/batch
Content-Type: application/json

{
  "assets": [
    {
      "uid": "i-xxx1",
      "name": "server-01",
      "model_uid": "cloud_ecs",
      "provider": "aliyun"
    },
    {
      "uid": "i-xxx2",
      "name": "server-02",
      "model_uid": "cloud_ecs",
      "provider": "aliyun"
    }
  ]
}
```

#### 获取资产统计

```http
GET /api/v1/cam/assets/statistics
```

**响应示例：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "total": 150,
    "by_provider": {
      "aliyun": 100,
      "aws": 30,
      "azure": 20
    },
    "by_model": {
      "cloud_ecs": 80,
      "cloud_rds": 40,
      "cloud_oss": 30
    }
  }
}
```

### 2. 模型管理

#### 获取模型列表

```http
GET /api/v1/cam/models?provider=aliyun&category=compute
```

**查询参数：**

- `provider`: 云厂商
- `category`: 分类

**响应示例：**

```json
{
  "code": 0,
  "msg": "success",
  "data": [
    {
      "id": 1,
      "uid": "cloud_ecs",
      "name": "云主机",
      "model_group_id": 1,
      "category": "compute",
      "icon": "server",
      "description": "云服务器实例",
      "provider": "all",
      "extensible": true
    }
  ]
}
```

#### 创建模型

```http
POST /api/v1/cam/models
Content-Type: application/json

{
  "uid": "cloud_ecs",
  "name": "云主机",
  "model_group_id": 1,
  "category": "compute",
  "icon": "server",
  "description": "云服务器实例",
  "provider": "all",
  "extensible": true
}
```

#### 获取模型详情

```http
GET /api/v1/cam/models/{uid}
```

**响应示例：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "model": {
      "id": 1,
      "uid": "cloud_ecs",
      "name": "云主机"
    },
    "field_groups": [
      {
        "group": {
          "id": 1,
          "name": "基本信息",
          "index": 1
        },
        "fields": [
          {
            "id": 1,
            "field_uid": "ecs_instance_id",
            "field_name": "instance_id",
            "field_type": "string",
            "display_name": "实例ID",
            "display": true,
            "required": true
          }
        ]
      }
    ]
  }
}
```

#### 更新模型

```http
PUT /api/v1/cam/models/{uid}
Content-Type: application/json

{
  "name": "云主机（更新）",
  "description": "更新后的描述"
}
```

#### 删除模型

```http
DELETE /api/v1/cam/models/{uid}
```

### 3. 字段管理

#### 添加字段

```http
POST /api/v1/cam/models/{uid}/fields
Content-Type: application/json

{
  "field_uid": "ecs_cpu",
  "field_name": "cpu",
  "field_type": "int",
  "model_uid": "cloud_ecs",
  "group_id": 1,
  "display_name": "CPU核数",
  "display": true,
  "required": false
}
```

#### 获取模型字段

```http
GET /api/v1/cam/models/{uid}/fields
```

#### 更新字段

```http
PUT /api/v1/cam/models/{uid}/fields/{field_uid}
Content-Type: application/json

{
  "display_name": "CPU核数（更新）",
  "required": true
}
```

#### 删除字段

```http
DELETE /api/v1/cam/models/{uid}/fields/{field_uid}
```

### 4. 字段分组管理

#### 添加分组

```http
POST /api/v1/cam/models/{uid}/groups
Content-Type: application/json

{
  "model_uid": "cloud_ecs",
  "name": "基本信息",
  "index": 1
}
```

#### 获取模型分组

```http
GET /api/v1/cam/models/{uid}/groups
```

#### 更新分组

```http
PUT /api/v1/cam/models/{uid}/groups/{id}
Content-Type: application/json

{
  "name": "基本信息（更新）",
  "index": 2
}
```

#### 删除分组

```http
DELETE /api/v1/cam/models/{uid}/groups/{id}
```

## 认证

所有 API 请求都需要在请求头中包含 JWT Token：

```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

### 获取 Token

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "password"
}
```

**响应：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "expires_at": 1698765432
  }
}
```

## 错误码

| 错误码 | 说明       | HTTP 状态码 |
| ------ | ---------- | ----------- |
| 0      | 成功       | 200         |
| 1      | 参数错误   | 400         |
| 2      | 未授权     | 401         |
| 3      | 禁止访问   | 403         |
| 4      | 资源不存在 | 404         |
| 5      | 系统错误   | 500         |

## 响应格式

所有 API 响应都遵循统一格式：

```json
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

- `code`: 错误码，0 表示成功
- `msg`: 消息描述
- `data`: 响应数据

## 分页

列表接口支持分页，使用以下参数：

- `page`: 页码，从 1 开始
- `page_size`: 每页数量，默认 20

响应格式：

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "list": [],
    "total": 100,
    "page": 1,
    "page_size": 20
  }
}
```

## 筛选和排序

### 筛选

使用查询参数进行筛选：

```http
GET /api/v1/cam/assets?provider=aliyun&region=cn-hangzhou
```

### 排序

使用 `sort` 参数：

```http
GET /api/v1/cam/assets?sort=ctime&order=desc
```

- `sort`: 排序字段
- `order`: 排序方向（asc/desc）

## 最佳实践

### 1. 使用 HTTPS

生产环境必须使用 HTTPS 加密传输。

### 2. 处理错误

始终检查响应的 `code` 字段：

```javascript
if (response.code !== 0) {
  console.error("API Error:", response.msg);
  return;
}
```

### 3. 设置超时

设置合理的请求超时时间：

```javascript
fetch("/api/v1/cam/assets", {
  timeout: 30000, // 30秒
});
```

### 4. 重试机制

对于网络错误，实现重试机制：

```javascript
async function fetchWithRetry(url, options, retries = 3) {
  for (let i = 0; i < retries; i++) {
    try {
      return await fetch(url, options);
    } catch (error) {
      if (i === retries - 1) throw error;
      await new Promise((resolve) => setTimeout(resolve, 1000 * (i + 1)));
    }
  }
}
```

### 5. 批量操作

使用批量接口而不是循环调用单个接口：

```javascript
// ✅ 推荐
POST /api/v1/cam/assets/batch
{
  "assets": [...]
}

// ❌ 不推荐
for (const asset of assets) {
  POST /api/v1/cam/assets
}
```

## 开发工具

### Postman

导入 OpenAPI 规范文件到 Postman：

1. 打开 Postman
2. 点击 Import
3. 选择 `docs/swagger.yaml`

### cURL

```bash
# 获取资产列表
curl -X GET "http://localhost:8001/api/v1/cam/assets?page=1&page_size=20" \
  -H "Authorization: Bearer <token>"

# 创建资产
curl -X POST "http://localhost:8001/api/v1/cam/assets" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "uid": "i-xxx",
    "name": "web-server-01",
    "model_uid": "cloud_ecs",
    "provider": "aliyun"
  }'
```

### JavaScript/TypeScript

```typescript
// 封装 API 客户端
class ECAMClient {
  private baseURL: string;
  private token: string;

  constructor(baseURL: string, token: string) {
    this.baseURL = baseURL;
    this.token = token;
  }

  private async request(method: string, path: string, data?: any) {
    const response = await fetch(`${this.baseURL}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${this.token}`,
        "Content-Type": "application/json",
      },
      body: data ? JSON.stringify(data) : undefined,
    });

    const result = await response.json();
    if (result.code !== 0) {
      throw new Error(result.msg);
    }

    return result.data;
  }

  async getAssets(params?: any) {
    const query = new URLSearchParams(params).toString();
    return this.request("GET", `/api/v1/cam/assets?${query}`);
  }

  async createAsset(asset: any) {
    return this.request("POST", "/api/v1/cam/assets", asset);
  }
}

// 使用
const client = new ECAMClient("http://localhost:8001", "your-token");
const assets = await client.getAssets({ provider: "aliyun" });
```

## 更新日志

### v1.0.0 (2025-10-30)

- ✅ 初始版本
- ✅ 云资产管理 API
- ✅ 模型管理 API
- ✅ 字段管理 API
- ✅ Swagger 文档

## 支持

如有问题，请联系：

- Email: support@example.com
- GitHub Issues: https://github.com/your-org/e-cam-service/issues
