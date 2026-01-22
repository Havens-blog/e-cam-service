# 云账号区域字段更新测试文档

## 更新内容

已成功将云账号的 `region` 字段从单个字符串更新为 `regions` 数组，支持多个区域。

## 更新的文件

### 1. 领域模型层

- `internal/shared/domain/account.go`
  - `CloudAccount.Region` → `CloudAccount.Regions []string`
  - `CreateCloudAccountRequest.Region` → `CreateCloudAccountRequest.Regions []string`

### 2. DAO 层

- `internal/cam/repository/dao/account.go`
  - `CloudAccount.Region` → `CloudAccount.Regions []string`

### 3. Repository 层

- `internal/cam/repository/account.go`
  - 更新了 `toDomain()` 和 `toEntity()` 方法中的字段映射

### 4. Service 层

- `internal/cam/service/account.go`
  - 更新了创建账号时的字段赋值
- `internal/cam/service/asset.go`
  - 使用 `account.Regions[0]` 作为默认区域

### 5. Web 层

- `internal/cam/web/vo.go`
  - `CreateCloudAccountReq.Region` → `CreateCloudAccountReq.Regions []string`
  - `CloudAccount.Region` → `CloudAccount.Regions []string`
- `internal/cam/web/handler.go`
  - 更新了请求处理和响应转换逻辑

### 6. 云厂商验证器

- `internal/shared/cloudx/aliyun_validator.go`
- `internal/shared/cloudx/aws_validator.go`
- `internal/shared/cloudx/azure_validator.go`
- `internal/shared/cloudx/huawei_validator.go`
- `internal/shared/cloudx/tencent_validator.go`
  - 使用 `account.Regions[0]` 作为默认区域
  - 降级处理时使用 `account.Regions` 而不是单个区域

## API 变更

### 1. 创建云账号 API

**请求示例（旧）：**

```json
{
  "name": "测试账号",
  "provider": "aliyun",
  "environment": "production",
  "access_key_id": "LTAI5tTestAccessKey123456",
  "access_key_secret": "TestSecretKey1234567890abcdef",
  "region": "cn-hangzhou",
  "tenant_id": "tenant_001"
}
```

**请求示例（新）：**

```json
{
  "name": "测试账号",
  "provider": "aliyun",
  "environment": "production",
  "access_key_id": "LTAI5tTestAccessKey123456",
  "access_key_secret": "TestSecretKey1234567890abcdef",
  "regions": ["cn-hangzhou", "cn-beijing", "cn-shanghai"],
  "tenant_id": "tenant_001"
}
```

**响应示例（新）：**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "测试账号",
    "provider": "aliyun",
    "environment": "production",
    "access_key_id": "LTAI5t***3456",
    "regions": ["cn-hangzhou", "cn-beijing", "cn-shanghai"],
    "status": "active",
    "create_time": "2025-11-10T16:00:00Z",
    "update_time": "2025-11-10T16:00:00Z"
  }
}
```

### 2. 更新云账号 API

**请求示例（旧）：**

```json
{
  "name": "更新后的账号名称",
  "description": "更新后的描述"
}
```

**请求示例（新）：**

```json
{
  "name": "集团-阿里云",
  "environment": "production",
  "access_key_id": "LTAI5tNewAccessKey123456",
  "access_key_secret": "NewSecretKey1234567890abcdef",
  "regions": ["eu-west-1", "cn-hangzhou", "cn-shenzhen"],
  "description": "集团-阿里云账号",
  "config": {
    "enable_auto_sync": true,
    "sync_interval": 300,
    "read_only": false,
    "show_sub_accounts": true,
    "enable_cost_monitoring": true
  }
}
```

**支持的更新字段：**

- `name`: 账号名称
- `environment`: 环境（production/staging/development）
- `access_key_id`: 访问密钥 ID
- `access_key_secret`: 访问密钥 Secret
- `regions`: 支持的区域列表
- `description`: 描述信息
- `config`: 配置信息

**注意：** 所有字段都是可选的，只需要传入需要更新的字段即可。

## Swagger 文档更新

Swagger 文档已自动更新，`regions` 字段定义如下：

```json
{
  "regions": {
    "type": "array",
    "minItems": 1,
    "items": {
      "type": "string"
    }
  }
}
```

## 测试脚本

### 1. 创建账号测试

已创建测试脚本 `scripts/test_cloud_account_regions.go`，包含以下测试场景：

1. 创建支持多个区域的云账号
2. 创建单个区域的云账号
3. 获取云账号详情验证 regions 字段
4. 列出所有云账号

### 2. 更新账号测试

已创建测试脚本 `scripts/test_update_cloud_account.go`，包含以下测试场景：

1. 更新账号名称和描述
2. 更新区域列表
3. 更新环境
4. 更新 AccessKey
5. 更新配置
6. 批量更新多个字段

### 运行测试

```bash
# 确保服务已启动
go run main.go start

# 在另一个终端运行创建测试
go run scripts/test_cloud_account_regions.go

# 运行更新测试（需要先修改脚本中的 accountID）
go run scripts/test_update_cloud_account.go
```

## 兼容性说明

### 数据库迁移

如果数据库中已有旧数据（使用 `region` 字段），需要进行数据迁移：

```javascript
// MongoDB 迁移脚本
db.cloud_accounts.find({ region: { $exists: true } }).forEach(function (doc) {
  db.cloud_accounts.updateOne(
    { _id: doc._id },
    {
      $set: { regions: [doc.region] },
      $unset: { region: "" },
    }
  );
});
```

### 默认区域处理

在需要单个区域的场景中（如创建云厂商客户端），系统会自动使用 `regions[0]` 作为默认区域。

## 验证清单

- [x] 领域模型更新
- [x] DAO 层更新
- [x] Repository 层更新
- [x] Service 层更新
- [x] Web 层更新
- [x] 云厂商验证器更新
- [x] Swagger 文档生成
- [x] 代码编译通过
- [x] 创建测试脚本

## 注意事项

1. **必填验证**：`regions` 字段必须至少包含一个区域（`binding:"required,min=1"`）
2. **默认区域**：系统会使用 `regions[0]` 作为默认区域
3. **向后兼容**：建议在部署前进行数据库迁移
4. **API 文档**：确保前端团队了解 API 变更

## 后续工作

1. 通知前端团队更新 API 调用
2. 执行数据库迁移脚本
3. 更新相关文档和示例
4. 进行集成测试
