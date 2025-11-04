# API 文档

## 快速开始

### 方式 1: 通过服务访问（推荐）

启动 E-CAM Service 后，直接访问：

```
http://localhost:8001/docs
```

### 方式 2: 独立文档服务器

#### Linux/Mac

```bash
chmod +x scripts/serve_docs.sh
./scripts/serve_docs.sh
```

#### Windows

```cmd
scripts\serve_docs.bat
```

#### 使用 Make

```bash
make -f Makefile.docs docs-serve
```

然后访问：

```
http://localhost:8080/swagger-ui.html
```

## 文档文件

- `swagger.yaml` - OpenAPI 3.0 规范文件
- `swagger-ui.html` - Swagger UI 界面
- `docs.go` - Swagger 注释主文件
- `API-DOCUMENTATION.md` - 详细的 API 使用文档

## 自动生成文档

### 安装 swag 工具

```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

或使用 Make：

```bash
make -f Makefile.docs docs-install
```

### 生成文档

```bash
swag init -g docs/docs.go -o docs/swagger
```

或使用 Make：

```bash
make -f Makefile.docs docs-gen
```

### 添加 API 注释

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

## 文档结构

```
docs/
├── swagger.yaml          # OpenAPI 规范文件（手动维护）
├── swagger-ui.html       # Swagger UI 页面
├── docs.go              # Swagger 注释主文件
├── API-DOCUMENTATION.md  # 详细文档
├── README.md            # 本文件
└── swagger/             # 自动生成的文档（可选）
    ├── docs.go
    ├── swagger.json
    └── swagger.yaml
```

## 验证文档

### 使用 Swagger CLI

```bash
npm install -g @apidevtools/swagger-cli
swagger-cli validate docs/swagger.yaml
```

### 使用 OpenAPI Generator

```bash
npm install -g @openapitools/openapi-generator-cli
openapi-generator-cli validate -i docs/swagger.yaml
```

### 使用 Make

```bash
make -f Makefile.docs docs-validate
```

## 导出文档

### 导出为 JSON

```bash
# 使用 yq 工具
yq eval -o=json docs/swagger.yaml > docs/swagger.json
```

### 导出为 Postman Collection

```bash
openapi-generator-cli generate \
  -i docs/swagger.yaml \
  -g postman-collection \
  -o docs/postman
```

### 导出为 HTML

```bash
npx redoc-cli bundle docs/swagger.yaml -o docs/api.html
```

## 集成到项目

### 在 Gin 中注册路由

```go
import "github.com/Havens-blog/e-cam-service/internal/cam/web"

func main() {
    router := gin.Default()

    // 注册 Swagger 路由
    web.RegisterSwaggerRoutes(router)

    router.Run(":8001")
}
```

### 使用 gin-swagger（可选）

```go
import (
    ginSwagger "github.com/swaggo/gin-swagger"
    "github.com/swaggo/files"
)

func main() {
    router := gin.Default()

    // 注册 Swagger 路由
    router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

    router.Run(":8001")
}
```

## 前端集成

### JavaScript/TypeScript

```typescript
// 使用 openapi-typescript-codegen 生成客户端
npm install openapi-typescript-codegen --save-dev

npx openapi-typescript-codegen \
  --input docs/swagger.yaml \
  --output src/api \
  --client axios
```

### React

```typescript
import { ECAMClient } from "./api";

const client = new ECAMClient({
  BASE: "http://localhost:8001",
  TOKEN: "your-jwt-token",
});

// 使用
const assets = await client.assets.getAssets({
  provider: "aliyun",
  page: 1,
  pageSize: 20,
});
```

### Vue

```typescript
import { createClient } from "./api";

const api = createClient({
  baseURL: "http://localhost:8001",
  headers: {
    Authorization: `Bearer ${token}`,
  },
});

// 使用
const { data } = await api.getAssets({ provider: "aliyun" });
```

## 最佳实践

### 1. 保持文档同步

- 代码变更时同时更新文档
- 使用自动生成工具减少手动维护
- 定期验证文档的准确性

### 2. 添加示例

- 为每个接口添加请求示例
- 提供常见场景的使用示例
- 包含错误处理示例

### 3. 详细的描述

- 清晰描述每个参数的用途
- 说明参数的约束和默认值
- 列出可能的错误码和原因

### 4. 版本管理

- 使用语义化版本号
- 记录 API 变更历史
- 保持向后兼容

## 常见问题

### Q: 文档无法访问？

A: 检查：

1. 服务是否正常启动
2. 端口是否被占用
3. 防火墙设置

### Q: 文档内容不更新？

A: 尝试：

1. 清除浏览器缓存
2. 重新生成文档
3. 重启服务

### Q: 如何添加认证？

A: 在 Swagger UI 中：

1. 点击右上角 "Authorize" 按钮
2. 输入 JWT Token
3. 点击 "Authorize"

### Q: 如何测试 API？

A: 在 Swagger UI 中：

1. 展开要测试的接口
2. 点击 "Try it out"
3. 填写参数
4. 点击 "Execute"

## 相关资源

- [OpenAPI 规范](https://swagger.io/specification/)
- [Swagger UI](https://swagger.io/tools/swagger-ui/)
- [Swag 文档](https://github.com/swaggo/swag)
- [Gin Swagger](https://github.com/swaggo/gin-swagger)

## 更新日志

### v1.0.0 (2025-10-30)

- ✅ 初始版本
- ✅ OpenAPI 3.0 规范
- ✅ Swagger UI 集成
- ✅ 自动生成支持
- ✅ 详细的 API 文档
