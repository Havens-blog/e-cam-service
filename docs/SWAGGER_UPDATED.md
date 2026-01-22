# Swagger 文档更新说明

## 更新内容

已添加新的 API 接口到 Swagger 文档：

### 新增接口

#### 获取用户组成员列表

**路径**: `GET /api/v1/cam/iam/groups/{id}/members`

**描述**: 获取指定用户组的所有成员列表

**参数**:

- `X-Tenant-ID` (header, required): 租户 ID
- `id` (path, required): 用户组 ID

**响应**:

- `200 OK`: 返回成员列表

## 访问 Swagger UI

### 本地开发环境

启动服务后，访问以下地址：

```
http://localhost:8080/swagger/index.html
```

### 使用步骤

1. **启动服务**

   ```bash
   go run main.go
   ```

2. **打开浏览器**
   访问: http://localhost:8080/swagger/index.html

3. **查找新接口**

   - 在页面中搜索 "用户组管理"
   - 找到 `GET /api/v1/cam/iam/groups/{id}/members`

4. **测试接口**
   - 点击接口展开详情
   - 点击 "Try it out"
   - 填写参数:
     - `X-Tenant-ID`: tenant-001
     - `id`: 1
   - 点击 "Execute"
   - 查看响应结果

## Swagger 文档文件

生成的文档文件位于 `docs/` 目录：

- `docs/swagger.json` - JSON 格式
- `docs/swagger.yaml` - YAML 格式
- `docs/docs.go` - Go 代码

## 重新生成文档

如果修改了 API 注释，需要重新生成 Swagger 文档：

```bash
# 基本生成
swag init -g main.go -o docs

# 完整生成（包含依赖和内部包）
swag init -g main.go -o docs --parseDependency --parseInternal
```

## API 文档示例

### 请求示例

```bash
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups/1/members" \
  -H "X-Tenant-ID: tenant-001"
```

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 1,
      "username": "zhang.san",
      "display_name": "张三",
      "email": "zhang.san@example.com",
      "provider": "aliyun",
      "cloud_user_id": "u-123456",
      "user_groups": [1, 2],
      "status": "active",
      "tenant_id": "tenant-001"
    }
  ]
}
```

## 完整的用户组管理 API 列表

在 Swagger UI 中，"用户组管理" 标签下包含以下接口：

1. `POST /api/v1/cam/iam/groups` - 创建用户组
2. `GET /api/v1/cam/iam/groups/{id}` - 获取用户组详情
3. `GET /api/v1/cam/iam/groups` - 查询用户组列表
4. `PUT /api/v1/cam/iam/groups/{id}` - 更新用户组
5. `DELETE /api/v1/cam/iam/groups/{id}` - 删除用户组
6. `PUT /api/v1/cam/iam/groups/{id}/policies` - 更新权限策略
7. `POST /api/v1/cam/iam/groups/sync` - 同步用户组
8. `GET /api/v1/cam/iam/groups/{id}/members` - 获取用户组成员 🆕

## Swagger 注释规范

如果需要添加新的 API，请遵循以下注释格式：

```go
// GetGroupMembers 获取用户组成员列表
// @Summary 获取用户组成员列表
// @Tags 用户组管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户组ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id}/members [get]
func (h *UserGroupHandler) GetGroupMembers(c *gin.Context) {
    // 实现代码
}
```

### 注释说明

- `@Summary`: 接口简短描述
- `@Tags`: 接口分组标签
- `@Produce`: 响应内容类型
- `@Param`: 参数定义
  - 格式: `名称 位置 类型 是否必需 描述`
  - 位置: `header`, `path`, `query`, `body`
- `@Success`: 成功响应
  - 格式: `状态码 {类型} 结构体`
- `@Router`: 路由定义
  - 格式: `路径 [HTTP方法]`

## 常见问题

### Q1: Swagger UI 无法访问

**检查**:

- 服务是否已启动
- 端口是否正确（默认 8080）
- 路径是否正确（/swagger/index.html）

### Q2: 新接口没有显示

**解决**:

1. 检查 Swagger 注释是否正确
2. 重新生成文档: `swag init -g main.go -o docs`
3. 重启服务
4. 刷新浏览器（Ctrl+F5 强制刷新）

### Q3: 接口测试失败

**检查**:

- 请求头是否包含 `X-Tenant-ID`
- 参数是否正确
- 服务是否正常运行
- 数据库是否有数据

## 相关文档

- [用户组成员查询 API](GROUP_MEMBERS_API.md)
- [API 文档](API-DOCUMENTATION.md)
- [Swagger 官方文档](https://swagger.io/docs/)

---

**更新日期**: 2025-11-25  
**Swagger 版本**: 2.0
