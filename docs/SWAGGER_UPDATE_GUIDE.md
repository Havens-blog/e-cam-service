# Swagger 文档更新指南

## ✅ Swagger 文档已更新

Swagger 文档已经重新生成，包含了最新的 IAM 管理 API。

## 📍 访问 Swagger 文档

### 本地开发环境

启动服务后，访问以下地址：

```
http://localhost:8080/docs
```

或者：

```
http://localhost:8080/api-docs
```

### Swagger 文件位置

- **Swagger UI**: `docs/swagger-ui.html`
- **Swagger YAML**: `docs/swagger.yaml`
- **Swagger JSON**: `docs/swagger.json`
- **Swagger Go**: `docs/docs.go`

## 📚 已包含的 API 模块

### 1. 用户管理 (User Management)

- `POST /api/v1/cam/iam/users` - 创建用户
- `GET /api/v1/cam/iam/users/{id}` - 获取用户详情
- `GET /api/v1/cam/iam/users` - 查询用户列表
- `PUT /api/v1/cam/iam/users/{id}` - 更新用户
- `DELETE /api/v1/cam/iam/users/{id}` - 删除用户
- `POST /api/v1/cam/iam/users/sync` - 同步用户
- `POST /api/v1/cam/iam/users/batch-assign` - 批量分配权限组

### 2. 权限组管理 (Group Management)

- `POST /api/v1/cam/iam/groups` - 创建权限组
- `GET /api/v1/cam/iam/groups/{id}` - 获取权限组详情
- `GET /api/v1/cam/iam/groups` - 查询权限组列表
- `PUT /api/v1/cam/iam/groups/{id}` - 更新权限组
- `DELETE /api/v1/cam/iam/groups/{id}` - 删除权限组
- `PUT /api/v1/cam/iam/groups/{id}/policies` - 更新权限策略

### 3. 同步任务管理 (Sync Task Management)

- `POST /api/v1/cam/iam/sync/tasks` - 创建同步任务
- `GET /api/v1/cam/iam/sync/tasks/{id}` - 获取同步任务状态
- `GET /api/v1/cam/iam/sync/tasks` - 查询同步任务列表
- `POST /api/v1/cam/iam/sync/tasks/{id}/retry` - 重试同步任务

### 4. 审计日志管理 (Audit Log Management)

- `GET /api/v1/cam/iam/audit/logs` - 查询审计日志列表
- `POST /api/v1/cam/iam/audit/logs/export` - 导出审计日志
- `POST /api/v1/cam/iam/audit/reports` - 生成审计报告

### 5. 策略模板管理 (Template Management)

- `POST /api/v1/cam/iam/templates` - 创建策略模板
- `GET /api/v1/cam/iam/templates/{id}` - 获取策略模板详情
- `GET /api/v1/cam/iam/templates` - 查询策略模板列表
- `PUT /api/v1/cam/iam/templates/{id}` - 更新策略模板
- `DELETE /api/v1/cam/iam/templates/{id}` - 删除策略模板
- `POST /api/v1/cam/iam/templates/from-group` - 从权限组创建模板

## 🔄 如何重新生成 Swagger 文档

当你修改了 API 注释后，需要重新生成 Swagger 文档：

```bash
# 生成 Swagger 文档
swag init -g main.go -o docs --parseDependency --parseInternal
```

### Swagger 注释格式

在 Handler 方法上添加注释：

```go
// CreateUser 创建用户
// @Summary 创建云用户
// @Description 创建新的云平台用户
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param body body CreateUserVO true "创建用户请求"
// @Success 200 {object} Result{data=domain.CloudUser} "成功"
// @Failure 400 {object} Result "请求参数错误"
// @Failure 500 {object} Result "服务器内部错误"
// @Router /api/v1/cam/iam/users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
    // 实现代码
}
```

### 注释说明

- `@Summary`: 简短描述
- `@Description`: 详细描述
- `@Tags`: API 分组标签
- `@Accept`: 接受的内容类型
- `@Produce`: 返回的内容类型
- `@Param`: 参数定义
- `@Success`: 成功响应
- `@Failure`: 失败响应
- `@Router`: 路由路径和方法

## 📖 文档对比

### Swagger 文档 vs Markdown 文档

| 特性       | Swagger | Markdown  |
| ---------- | ------- | --------- |
| 交互式测试 | ✅ 支持 | ❌ 不支持 |
| 在线调试   | ✅ 支持 | ❌ 不支持 |
| 代码生成   | ✅ 支持 | ❌ 不支持 |
| 详细说明   | ⚠️ 有限 | ✅ 详细   |
| 示例代码   | ⚠️ 有限 | ✅ 丰富   |
| 开发指南   | ❌ 无   | ✅ 完整   |

**建议**:

- **Swagger**: 用于 API 测试和快速查看接口
- **Markdown**: 用于详细了解接口和开发指南

## 🚀 使用 Swagger UI

### 1. 启动服务

```bash
go run main.go start
```

### 2. 访问 Swagger UI

打开浏览器访问：`http://localhost:8080/docs`

### 3. 测试 API

1. 点击要测试的 API 接口
2. 点击 "Try it out" 按钮
3. 填写请求参数
4. 点击 "Execute" 执行请求
5. 查看响应结果

### 4. 认证设置

如果 API 需要认证，点击右上角的 "Authorize" 按钮，输入 Token：

```
Bearer <your_token>
```

## 📝 维护建议

### 1. 保持注释同步

每次修改 API 时，同时更新：

- Swagger 注释（代码中）
- Markdown 文档（docs/api/）

### 2. 定期重新生成

在以下情况重新生成 Swagger 文档：

- 添加新的 API 接口
- 修改现有接口的参数或响应
- 更新接口描述

### 3. 版本管理

- 将生成的 Swagger 文件提交到 Git
- 在 CHANGELOG 中记录 API 变更

## 🔗 相关资源

- **Swagger 官方文档**: https://swagger.io/docs/
- **Swag 工具文档**: https://github.com/swaggo/swag
- **Markdown API 文档**: [docs/api/README.md](./api/README.md)

## ✅ 检查清单

- [x] Swagger 文档已生成
- [x] Swagger UI 可访问
- [x] API 注释已添加
- [x] Markdown 文档已创建
- [x] 两种文档格式都可用

## 📞 问题反馈

如遇到 Swagger 相关问题：

1. 检查 Swagger 注释格式是否正确
2. 重新运行 `swag init` 命令
3. 查看生成日志中的错误信息
4. 联系后端团队

---

**最后更新**: 2024-01-01  
**Swagger 版本**: OpenAPI 2.0  
**工具版本**: swag v1.16.4
