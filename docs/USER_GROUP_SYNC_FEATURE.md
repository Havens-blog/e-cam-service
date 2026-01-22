# 用户组同步功能

## 概述

新增了从云平台同步用户组和权限策略的功能，支持将云平台（阿里云、AWS、腾讯云、华为云、火山云）的用户组自动同步到本地数据库。

## 功能特性

### 1. 用户组同步

**接口**: `POST /api/v1/cam/iam/groups/sync`

**功能**: 从指定云账号同步用户组列表及其权限策略

**请求参数**:

- `cloud_account_id` (query, int, 必需) - 云账号 ID

**响应示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "total_groups": 10,
    "created_groups": 5,
    "updated_groups": 4,
    "failed_groups": 1
  }
}
```

### 2. 同步逻辑

1. **获取云平台用户组**: 通过云平台适配器获取用户组列表
2. **智能同步**:
   - 如果用户组不存在，创建新用户组
   - 如果用户组已存在，更新用户组信息（包括权限策略）
3. **保留本地数据**: 同步时保留本地的自定义名称和租户信息
4. **权限策略同步**: 自动同步用户组关联的权限策略列表

### 3. 数据映射

同步时会映射以下字段：

- `group_name`: 云端用户组名称
- `display_name`: 显示名称
- `description`: 描述信息
- `policies`: 权限策略列表（包含策略 ID、名称、文档、类型）
- `cloud_group_id`: 云端用户组 ID
- `member_count`: 成员数量

### 4. 支持的云平台

- ✅ 阿里云 (Aliyun)
- ✅ AWS
- ✅ 腾讯云 (Tencent)
- ✅ 华为云 (Huawei)
- ✅ 火山云 (Volcano)

## 使用示例

### 同步用户组

```bash
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1"
```

### 响应说明

- `total_groups`: 云平台返回的用户组总数
- `created_groups`: 新创建的用户组数量
- `updated_groups`: 更新的用户组数量
- `failed_groups`: 同步失败的用户组数量

## 技术实现

### 服务层

**文件**: `internal/cam/iam/service/group.go`

**核心方法**:

- `SyncGroups`: 主同步方法，协调整个同步流程
- `syncSingleGroup`: 同步单个用户组
- `createSyncedGroup`: 创建新的同步用户组
- `updateSyncedGroup`: 更新已存在的用户组

### 适配器层

每个云平台适配器实现了 `ListGroups` 方法，用于获取云平台的用户组列表：

- `internal/shared/cloudx/iam/aliyun/group.go`
- `internal/shared/cloudx/iam/aws/group.go`
- `internal/shared/cloudx/iam/tencent/group.go`
- `internal/shared/cloudx/iam/huawei/group.go`
- `internal/shared/cloudx/iam/volcano/group.go`

### Web 层

**文件**: `internal/cam/iam/web/group_handler.go`

**方法**: `SyncGroups` - HTTP 处理器，处理同步请求

## 注意事项

1. **权限要求**: 确保云账号具有读取用户组和权限策略的权限
2. **数据一致性**: 同步会覆盖云端字段，但保留本地自定义字段
3. **错误处理**: 单个用户组同步失败不会影响其他用户组的同步
4. **日志记录**: 所有同步操作都会记录详细日志

## 后续优化建议

1. **增量同步**: 支持只同步变更的用户组
2. **定时同步**: 添加定时任务自动同步
3. **同步策略**: 支持配置同步策略（覆盖/合并）
4. **批量操作**: 支持批量同步多个云账号
5. **同步历史**: 记录同步历史和变更日志

## 相关文档

- [IAM 用户组管理 API](./IAM_GROUP_API_STATUS.md)
- [用户组术语统一规范](../.kiro/specs/user-group-terminology-update/requirements.md)
