# 多云 IAM 统一管理 API 文档

## 概述

本文档描述多云 IAM 统一管理系统的 RESTful API 接口，供前端开发使用。

## 基础信息

- **Base URL**: `/api/v1/cam/iam`
- **Content-Type**: `application/json`
- **认证方式**: Bearer Token (通过 Header: `Authorization: Bearer <token>`)

## 支持的云厂商

| 云厂商 | Provider 值 | 状态      |
| ------ | ----------- | --------- |
| 阿里云 | `aliyun`    | ✅ 已实现 |
| AWS    | `aws`       | ✅ 已实现 |
| 华为云 | `huawei`    | ⏳ 待实现 |
| 腾讯云 | `tencent`   | ⏳ 待实现 |
| 火山云 | `volcano`   | ⏳ 待实现 |

## 通用响应格式

### 成功响应

```json
{
  "code": 0,
  "message": "success",
  "data": { ... }
}
```

### 错误响应

```json
{
  "code": 1,
  "message": "错误描述",
  "data": null
}
```

### 分页响应

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [ ... ],
    "total": 100,
    "page": 1,
    "size": 20
  }
}
```

## API 模块

1. [用户管理 API](./IAM_API_Users.md)
2. [权限组管理 API](./IAM_API_Groups.md)
3. [同步任务 API](./IAM_API_Sync.md)
4. [审计日志 API](./IAM_API_Audit.md)
5. [策略模板 API](./IAM_API_Templates.md)

## 错误码说明

| 错误码 | 说明           |
| ------ | -------------- |
| 0      | 成功           |
| 400    | 请求参数错误   |
| 401    | 未授权         |
| 403    | 无权限         |
| 404    | 资源不存在     |
| 500    | 服务器内部错误 |
| 1001   | 云账号不存在   |
| 1002   | 云账号凭证无效 |
| 1003   | 用户已存在     |
| 1004   | 权限组不存在   |
| 1005   | 同步任务失败   |

## 枚举类型

### CloudProvider (云厂商)

```
aliyun   - 阿里云
aws      - AWS
azure    - Azure
tencent  - 腾讯云
huawei   - 华为云
volcano  - 火山云
```

### CloudUserType (用户类型)

```
api_key     - API密钥用户
access_key  - 访问密钥用户
ram_user    - RAM用户(阿里云)
iam_user    - IAM用户(AWS/其他)
```

### CloudUserStatus (用户状态)

```
active   - 活跃
inactive - 未激活
deleted  - 已删除
```

### PolicyType (策略类型)

```
system - 系统策略
custom - 自定义策略
```

### SyncTaskType (同步任务类型)

```
full        - 全量同步
incremental - 增量同步
```

### SyncTaskStatus (同步任务状态)

```
pending    - 等待中
running    - 运行中
success    - 成功
failed     - 失败
cancelled  - 已取消
```

### AuditOperationType (审计操作类型)

```
create - 创建
update - 更新
delete - 删除
sync   - 同步
assign - 分配
```

### TemplateCategory (模板分类)

```
readonly  - 只读权限
readwrite - 读写权限
admin     - 管理员权限
custom    - 自定义
```
