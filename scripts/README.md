# 初始化脚本

## 模型初始化

### 功能说明

`init_models.go` 脚本用于初始化云资源模型数据到 MongoDB。

### 初始化的模型

脚本会自动创建以下云资源模型：

**计算资源：**
- cloud_ecs - 云主机（包含完整的字段定义和分组）
- cloud_image - 镜像
- cloud_snapshot - 快照

**存储资源：**
- cloud_disk - 云盘
- cloud_oss - 对象存储

**网络资源：**
- cloud_cdn - CDN

**数据库资源：**
- cloud_redis - Redis
- cloud_mysql - MySQL

**安全资源：**
- cloud_waf - WAF
- cloud_security_group - 安全组
- cloud_ssl - SSL证书

**域名资源：**
- cloud_domain - 域名

### 使用方法

#### 方式一：使用 go run 执行

```bash
# 在项目根目录执行
go run scripts/init_models.go
```

#### 方式二：编译后执行

```bash
# 编译
go build -o bin/init_models.exe scripts/init_models.go

# 执行
.\bin\init_models.exe
```

### 注意事项

1. **幂等性**：脚本支持重复执行，已存在的模型会被跳过，不会重复创建
2. **配置文件**：确保 `config/prod.yaml` 文件存在且配置正确
3. **数据库连接**：确保 MongoDB 服务正在运行且可访问
4. **执行位置**：必须在项目根目录执行，因为需要读取配置文件

### 执行结果

成功执行后会看到类似输出：

```
开始初始化云资源模型...
创建模型 cloud_ecs 成功
创建字段分组 cloud_ecs.基本信息 成功
创建字段分组 cloud_ecs.配置信息 成功
创建字段分组 cloud_ecs.网络信息 成功
创建字段 ecs_instance_id 成功
创建字段 ecs_instance_name 成功
...
模型 cloud_disk 已存在，跳过创建
✅ 云资源模型初始化完成！
```

### 验证初始化结果

可以通过以下 API 验证模型是否创建成功：

```bash
# 获取所有模型列表
curl http://localhost:8080/api/v1/cam/models

# 获取云主机模型详情（包含字段和分组）
curl http://localhost:8080/api/v1/cam/models/cloud_ecs
```

### 故障排查

**问题：连接数据库失败**
- 检查 MongoDB 服务是否启动
- 检查 `config/prod.yaml` 中的数据库配置

**问题：配置文件找不到**
- 确保在项目根目录执行脚本
- 检查 `config/prod.yaml` 文件是否存在

**问题：权限不足**
- 确保数据库用户有创建集合和索引的权限
