# E-CAM Service API 文档总结

## 📊 API 统计

### 总体统计

- **总 API 数量**: 25+ 个
- **API 分组**: 6 个主要分类
- **支持方法**: GET, POST, PUT, DELETE

### 📋 API 分类统计

#### 1. 资产管理 (Asset Management) - 7 个 API

- `POST /cam/assets` - 创建资产
- `POST /cam/assets/batch` - 批量创建资产
- `GET /cam/assets` - 获取资产列表
- `GET /cam/assets/{id}` - 获取资产详情
- `PUT /cam/assets/{id}` - 更新资产
- `DELETE /cam/assets/{id}` - 删除资产

#### 2. 资产发现 (Asset Discovery) - 2 个 API

- `POST /cam/assets/discover` - 发现云资产
- `POST /cam/assets/sync` - 同步云资产

#### 3. 统计分析 (Analytics) - 2 个 API

- `GET /cam/assets/statistics` - 获取资产统计
- `GET /cam/assets/cost-analysis` - 获取成本分析

#### 4. 云账号管理 (Cloud Account Management) - 6 个 API

- `POST /cam/cloud-accounts` - 创建云账号
- `GET /cam/cloud-accounts` - 获取云账号列表
- `GET /cam/cloud-accounts/{id}` - 获取云账号详情
- `PUT /cam/cloud-accounts/{id}` - 更新云账号
- `DELETE /cam/cloud-accounts/{id}` - 删除云账号

#### 5. 云账号操作 (Cloud Account Operations) - 4 个 API

- `POST /cam/cloud-accounts/{id}/test-connection` - 测试连接
- `POST /cam/cloud-accounts/{id}/enable` - 启用云账号
- `POST /cam/cloud-accounts/{id}/disable` - 禁用云账号
- `POST /cam/cloud-accounts/{id}/sync` - 同步云账号资产

#### 6. 模型管理 (Model Management) - 5 个 API

- `POST /cam/models` - 创建模型
- `GET /cam/models` - 获取模型列表
- `GET /cam/models/{uid}` - 获取模型详情
- `PUT /cam/models/{uid}` - 更新模型
- `DELETE /cam/models/{uid}` - 删除模型

#### 7. 字段管理 (Field Management) - 4 个 API

- `POST /cam/models/{uid}/fields` - 添加字段
- `GET /cam/models/{uid}/fields` - 获取字段列表
- `PUT /cam/fields/{field_uid}` - 更新字段
- `DELETE /cam/fields/{field_uid}` - 删除字段

#### 8. 字段分组管理 (Field Group Management) - 4 个 API

- `POST /cam/models/{uid}/field-groups` - 添加字段分组
- `GET /cam/models/{uid}/field-groups` - 获取分组列表
- `PUT /cam/field-groups/{id}` - 更新字段分组
- `DELETE /cam/field-groups/{id}` - 删除字段分组

## 🏷️ 支持的云厂商

所有 API 都支持以下云厂商：

- **阿里云** (aliyun)
- **AWS** (aws)
- **Azure** (azure)

## 📝 API 文档特性

### ✅ 完整的 Swagger 注释

- 详细的接口描述
- 完整的参数说明
- 响应状态码定义
- 请求/响应示例

### 🔍 支持的查询参数

- **分页**: offset, limit
- **过滤**: provider, environment, status
- **搜索**: 按名称、类型等条件

### 📊 响应格式

所有 API 都使用统一的响应格式：

```json
{
  "code": 200,
  "msg": "success",
  "data": {...}
}
```

## 🌐 访问方式

### Swagger UI

- **URL**: http://localhost:8001/swagger/index.html
- **功能**: 交互式 API 文档，支持在线测试

### API 文档

- **JSON**: http://localhost:8001/swagger/doc.json
- **YAML**: http://localhost:8001/swagger/swagger.yaml

## 🚀 使用建议

1. **开发测试**: 使用 Swagger UI 进行 API 测试
2. **集成开发**: 参考 JSON/YAML 文档生成客户端代码
3. **API 版本**: 当前版本 v1，基础路径 `/api/v1`
4. **认证**: 根据实际部署配置认证方式

## 📈 后续扩展

当前 API 覆盖了云资产管理的核心功能，后续可以考虑添加：

- 资产监控告警 API
- 成本优化建议 API
- 资产合规检查 API
- 批量操作 API
- 导入导出 API

---

**生成时间**: 2025-11-05  
**API 版本**: v1.0  
**文档版本**: 自动生成
