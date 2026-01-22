# 多云 IAM 统一管理 API 文档

## 📚 文档目录

### 1. [API 概述](./IAM_API_Overview.md)

- 基础信息
- 支持的云厂商
- 通用响应格式
- 错误码说明
- 枚举类型定义

### 2. [用户管理 API](./IAM_API_Users.md)

- 创建用户
- 获取用户详情
- 查询用户列表
- 更新用户
- 删除用户
- 批量分配权限组
- 同步用户到云平台

### 3. [权限组管理 API](./IAM_API_Groups.md)

- 创建权限组
- 获取权限组详情
- 查询权限组列表
- 更新权限组
- 删除权限组
- 获取权限组的用户列表
- 获取可用策略列表

### 4. [同步任务 API](./IAM_API_Sync.md)

- 创建同步任务
- 获取同步任务详情
- 查询同步任务列表
- 取消同步任务
- 重试失败的同步任务
- 批量同步用户
- 获取同步统计信息

### 5. [审计日志 API](./IAM_API_Audit.md)

- 查询审计日志列表
- 获取审计日志详情
- 导出审计日志
- 生成审计报告
- 获取审计报告
- 获取审计统计信息

### 6. [策略模板 API](./IAM_API_Templates.md)

- 创建策略模板
- 获取策略模板详情
- 查询策略模板列表
- 更新策略模板
- 删除策略模板
- 从权限组创建模板
- 应用模板到权限组
- 获取内置模板列表

### 7. [前端开发指南](./Frontend_Development_Guide.md)

- 技术栈建议
- 页面结构设计
- 状态管理
- HTTP 请求封装
- 类型定义
- 实时更新方案
- 错误处理
- 性能优化
- 测试建议

## 🚀 快速开始

### 1. 认证

所有 API 请求需要在 Header 中携带 Bearer Token：

```bash
Authorization: Bearer <your_token>
```

### 2. 基础 URL

```
/api/v1/cam/iam
```

### 3. 示例请求

```bash
# 获取用户列表
curl -X GET "https://api.example.com/api/v1/cam/iam/users?page=1&size=20" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json"

# 创建用户
curl -X POST "https://api.example.com/api/v1/cam/iam/users" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "test-user",
    "user_type": "ram_user",
    "cloud_account_id": 1,
    "tenant_id": "tenant-001"
  }'
```

## 🌐 支持的云厂商

| 云厂商 | Provider 值 | 实现状态  |
| ------ | ----------- | --------- |
| 阿里云 | `aliyun`    | ✅ 已实现 |
| AWS    | `aws`       | ✅ 已实现 |
| 华为云 | `huawei`    | ⏳ 待实现 |
| 腾讯云 | `tencent`   | ⏳ 待实现 |
| 火山云 | `volcano`   | ⏳ 待实现 |

## 📊 数据模型

### 用户 (User)

```json
{
  "id": 1001,
  "username": "test-user",
  "user_type": "ram_user",
  "cloud_account_id": 1,
  "provider": "aliyun",
  "cloud_user_id": "ram-user-123",
  "display_name": "测试用户",
  "email": "test@example.com",
  "permission_groups": [1, 2],
  "status": "active",
  "tenant_id": "tenant-001",
  "create_time": "2024-01-01T00:00:00Z",
  "update_time": "2024-01-01T00:00:00Z"
}
```

### 权限组 (Group)

```json
{
  "id": 1,
  "name": "开发者权限组",
  "description": "开发人员的标准权限",
  "policies": [...],
  "cloud_platforms": ["aliyun", "aws"],
  "user_count": 15,
  "tenant_id": "tenant-001",
  "create_time": "2024-01-01T00:00:00Z",
  "update_time": "2024-01-01T00:00:00Z"
}
```

### 同步任务 (SyncTask)

```json
{
  "id": 1,
  "task_type": "full",
  "target_type": "user",
  "target_id": 1001,
  "cloud_account_id": 1,
  "provider": "aliyun",
  "status": "success",
  "result": {...},
  "start_time": "2024-01-01T00:00:00Z",
  "end_time": "2024-01-01T00:05:00Z"
}
```

## 🔧 开发工具

### Postman Collection

导入 Postman Collection 快速测试 API：

```bash
# 下载 Collection
curl -O https://api.example.com/docs/postman-collection.json
```

### Swagger UI

访问 Swagger UI 查看交互式 API 文档：

```
https://api.example.com/swagger-ui
```

## 📝 更新日志

### v1.0.0 (2024-01-01)

- ✅ 用户管理 API
- ✅ 权限组管理 API
- ✅ 同步任务 API
- ✅ 审计日志 API
- ✅ 策略模板 API
- ✅ 阿里云适配器
- ✅ AWS 适配器

### 计划中

- ⏳ 华为云适配器
- ⏳ 腾讯云适配器
- ⏳ 火山云适配器
- ⏳ WebSocket 实时通知
- ⏳ GraphQL API

## 🤝 贡献指南

如发现文档错误或有改进建议，请：

1. 提交 Issue
2. 发起 Pull Request
3. 联系后端团队

## 📧 联系方式

- **技术支持**: support@example.com
- **API 问题**: api@example.com
- **文档反馈**: docs@example.com

## 📄 许可证

Copyright © 2024 E-CAM Service. All rights reserved.
