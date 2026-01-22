# 🚀 IAM API 快速参考

## 📍 Swagger UI 访问

```
http://localhost:8080/swagger/index.html
```

## 🔄 重新生成命令

```bash
swag init -g main.go -o docs --parseDependency --parseInternal
```

## 📊 API 概览 (26 个接口)

### 👤 用户管理 (7 个)

```
POST   /api/v1/cam/iam/users                  创建用户
GET    /api/v1/cam/iam/users                  查询用户列表
GET    /api/v1/cam/iam/users/{id}             获取用户详情
PUT    /api/v1/cam/iam/users/{id}             更新用户
DELETE /api/v1/cam/iam/users/{id}             删除用户
POST   /api/v1/cam/iam/users/sync             同步用户
POST   /api/v1/cam/iam/users/batch-assign     批量分配权限组
```

### 👥 权限组管理 (6 个)

```
POST   /api/v1/cam/iam/groups                 创建权限组
GET    /api/v1/cam/iam/groups                 查询权限组列表
GET    /api/v1/cam/iam/groups/{id}            获取权限组详情
PUT    /api/v1/cam/iam/groups/{id}            更新权限组
DELETE /api/v1/cam/iam/groups/{id}            删除权限组
PUT    /api/v1/cam/iam/groups/{id}/policies   更新权限策略
```

### 📋 策略模板管理 (6 个)

```
POST   /api/v1/cam/iam/templates              创建模板
GET    /api/v1/cam/iam/templates              查询模板列表
GET    /api/v1/cam/iam/templates/{id}         获取模板详情
PUT    /api/v1/cam/iam/templates/{id}         更新模板
DELETE /api/v1/cam/iam/templates/{id}         删除模板
POST   /api/v1/cam/iam/templates/from-group   从权限组创建模板
```

### 🔄 同步任务管理 (4 个)

```
POST   /api/v1/cam/iam/sync/tasks             创建同步任务
GET    /api/v1/cam/iam/sync/tasks             查询任务列表
GET    /api/v1/cam/iam/sync/tasks/{id}        获取任务状态
POST   /api/v1/cam/iam/sync/tasks/{id}/retry  重试任务
```

### 📝 审计日志管理 (3 个)

```
GET    /api/v1/cam/iam/audit/logs             查询审计日志
GET    /api/v1/cam/iam/audit/logs/export      导出审计日志
POST   /api/v1/cam/iam/audit/reports          生成审计报告
```

## 🧪 快速测试

### 创建用户

```bash
curl -X POST "http://localhost:8080/api/v1/cam/iam/users" \
  -H "Content-Type: application/json" \
  -d '{"username":"test","user_type":"ram_user","cloud_account_id":1}'
```

### 查询用户

```bash
curl "http://localhost:8080/api/v1/cam/iam/users?page=1&size=20"
```

### 创建权限组

```bash
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups" \
  -H "Content-Type: application/json" \
  -d '{"name":"开发组","cloud_platforms":["aliyun"]}'
```

## 📁 文档位置

- **Swagger YAML**: `docs/swagger.yaml`
- **Swagger JSON**: `docs/swagger.json`
- **Go 代码**: `docs/docs.go`
- **详细文档**: `docs/api/IAM_API_*.md`

## ✅ 状态

- 总接口数: **26 个**
- 生成状态: **✅ 完成**
- 编译状态: **✅ 通过**
- 文档状态: **✅ 可用**
