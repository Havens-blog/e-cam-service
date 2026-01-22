# CMDB API 文档

> 配置管理数据库 API - 提供资源模型和实例的统一管理接口

## 基础信息

- **Base URL**: `/api/v1`
- **Content-Type**: `application/json`

## 数据库设计

### MongoDB 集合

所有 CMDB 集合统一使用 `c_` 前缀：

| 集合名                  | 用途         | 说明                 |
| ----------------------- | ------------ | -------------------- |
| `c_model`               | 模型定义     | 存储资源模型元数据   |
| `c_model_group`         | 模型分组     | 模型的分类管理       |
| `c_model_relation_type` | 模型关系类型 | 定义模型间的关系类型 |
| `c_attribute`           | 模型属性     | 模型的字段定义       |
| `c_attribute_group`     | 属性分组     | 属性的分组管理       |
| `c_instance`            | 资源实例     | 存储实际的资源数据   |
| `c_instance_relation`   | 实例关系     | 实例间的关联关系     |

## 响应格式

```json
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

### 错误码

| Code   | 说明       |
| ------ | ---------- |
| 0      | 成功       |
| 400001 | 参数错误   |
| 404001 | 模型不存在 |
| 404002 | 实例不存在 |
| 409001 | 模型已存在 |
| 409002 | 实例已存在 |
| 500001 | 系统错误   |

---

## 模型管理

### 创建模型

```
POST /cmdb/models
```

**请求参数**

| 字段           | 类型   | 必填 | 说明                                                    |
| -------------- | ------ | ---- | ------------------------------------------------------- |
| uid            | string | ✓    | 模型唯一标识，如 `aliyun_ecs`                           |
| name           | string | ✓    | 模型名称                                                |
| category       | string | ✓    | 资源类别: compute/storage/network/database/security/iam |
| model_group_id | int64  |      | 模型分组 ID                                             |
| parent_uid     | string |      | 父模型 UID                                              |
| level          | int    |      | 层级 (1=主资源, 2=子资源)                               |
| icon           | string |      | 图标                                                    |
| description    | string |      | 描述                                                    |
| provider       | string |      | 云厂商: aliyun/aws/azure/all                            |
| extensible     | bool   |      | 是否可扩展，默认 true                                   |

**请求示例**

```json
{
  "uid": "aliyun_ecs",
  "name": "阿里云ECS",
  "category": "compute",
  "provider": "aliyun",
  "level": 1,
  "extensible": true
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 1
  }
}
```

### 获取模型列表

```
GET /cmdb/models
```

**查询参数**

| 参数       | 类型   | 说明              |
| ---------- | ------ | ----------------- |
| provider   | string | 云厂商过滤        |
| category   | string | 资源类别过滤      |
| parent_uid | string | 父模型 UID        |
| level      | int    | 层级过滤          |
| offset     | int    | 偏移量，默认 0    |
| limit      | int    | 每页数量，默认 20 |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "models": [
      {
        "id": 1,
        "uid": "aliyun_ecs",
        "name": "阿里云ECS",
        "category": "compute",
        "provider": "aliyun",
        "level": 1,
        "extensible": true,
        "create_time": 1705305600000,
        "update_time": 1705305600000
      }
    ],
    "total": 1
  }
}
```

### 获取模型详情

```
GET /cmdb/models/{uid}
```

**路径参数**

| 参数 | 说明         |
| ---- | ------------ |
| uid  | 模型唯一标识 |

### 更新模型

```
PUT /cmdb/models/{uid}
```

**请求参数**

| 字段           | 类型   | 说明        |
| -------------- | ------ | ----------- |
| name           | string | 模型名称    |
| model_group_id | int64  | 模型分组 ID |
| icon           | string | 图标        |
| description    | string | 描述        |
| extensible     | bool   | 是否可扩展  |

### 删除模型

```
DELETE /cmdb/models/{uid}
```

---

## 实例管理

### 创建实例

```
POST /cmdb/instances
```

**请求参数**

| 字段       | 类型   | 必填 | 说明                            |
| ---------- | ------ | ---- | ------------------------------- |
| model_uid  | string | ✓    | 模型 UID，如 `aliyun_ecs`       |
| asset_id   | string | ✓    | 云厂商资产 ID，如 `i-bp1234xxx` |
| tenant_id  | string | ✓    | 租户 ID                         |
| asset_name | string |      | 资产名称                        |
| account_id | int64  |      | 云账号 ID                       |
| attributes | object |      | 动态属性                        |

**请求示例**

```json
{
  "model_uid": "aliyun_ecs",
  "asset_id": "i-bp1234567890abcdef",
  "asset_name": "web-server-01",
  "tenant_id": "tenant-001",
  "account_id": 1,
  "attributes": {
    "status": "Running",
    "region": "cn-hangzhou",
    "cpu": 4,
    "memory": 8192,
    "private_ip": "192.168.1.100"
  }
}
```

### 批量创建实例

```
POST /cmdb/instances/batch
```

**请求示例**

```json
{
  "instances": [
    {
      "model_uid": "aliyun_ecs",
      "asset_id": "i-bp001",
      "tenant_id": "tenant-001",
      "asset_name": "server-01",
      "attributes": { "status": "Running" }
    },
    {
      "model_uid": "aliyun_ecs",
      "asset_id": "i-bp002",
      "tenant_id": "tenant-001",
      "asset_name": "server-02",
      "attributes": { "status": "Running" }
    }
  ]
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "count": 2
  }
}
```

### 更新或插入实例 (Upsert)

> 根据 `tenant_id + model_uid + asset_id` 判断，存在则更新，不存在则创建。适用于资源同步场景。

```
POST /cmdb/instances/upsert
```

**请求参数** 同创建实例

### 批量更新或插入实例

```
POST /cmdb/instances/upsert-batch
```

**请求参数** 同批量创建实例

### 获取实例列表

```
GET /cmdb/instances
```

**查询参数**

| 参数       | 类型   | 说明                |
| ---------- | ------ | ------------------- |
| model_uid  | string | 模型 UID 过滤       |
| tenant_id  | string | 租户 ID 过滤        |
| account_id | int64  | 云账号 ID 过滤      |
| asset_name | string | 资产名称 (模糊搜索) |
| status     | string | 状态过滤            |
| region     | string | 地域过滤            |
| offset     | int    | 偏移量，默认 0      |
| limit      | int    | 每页数量，默认 20   |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "instances": [
      {
        "id": 1001,
        "model_uid": "aliyun_ecs",
        "asset_id": "i-bp1234567890abcdef",
        "asset_name": "web-server-01",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "attributes": {
          "status": "Running",
          "region": "cn-hangzhou",
          "cpu": 4,
          "memory": 8192,
          "private_ip": "192.168.1.100"
        },
        "create_time": 1705305600000,
        "update_time": 1705305600000
      }
    ],
    "total": 1
  }
}
```

### 获取实例详情

```
GET /cmdb/instances/{id}
```

### 更新实例

```
PUT /cmdb/instances/{id}
```

**请求参数**

| 字段       | 类型   | 说明                        |
| ---------- | ------ | --------------------------- |
| asset_name | string | 资产名称                    |
| attributes | object | 动态属性 (会与现有属性合并) |

### 删除实例

```
DELETE /cmdb/instances/{id}
```

---

## 预置模型列表

| UID                   | 名称              | 类别     | 云厂商 |
| --------------------- | ----------------- | -------- | ------ |
| aliyun_ecs            | 阿里云 ECS        | compute  | aliyun |
| aliyun_rds            | 阿里云 RDS        | database | aliyun |
| aliyun_oss            | 阿里云 OSS        | storage  | aliyun |
| aliyun_vpc            | 阿里云 VPC        | network  | aliyun |
| aliyun_slb            | 阿里云 SLB        | network  | aliyun |
| aliyun_security_group | 阿里云安全组      | security | aliyun |
| aliyun_ram_user       | 阿里云 RAM 用户   | iam      | aliyun |
| aliyun_ram_group      | 阿里云 RAM 用户组 | iam      | aliyun |
| aliyun_ram_policy     | 阿里云 RAM 策略   | iam      | aliyun |
| aws_ec2               | AWS EC2           | compute  | aws    |
| aws_rds               | AWS RDS           | database | aws    |
| aws_s3                | AWS S3            | storage  | aws    |
| aws_vpc               | AWS VPC           | network  | aws    |
| aws_iam_user          | AWS IAM 用户      | iam      | aws    |
| aws_iam_group         | AWS IAM 用户组    | iam      | aws    |
| aws_iam_policy        | AWS IAM 策略      | iam      | aws    |

---

## 模型分组管理

> 模型分组用于对资源模型进行分类管理，支持树形展示

### 预置分组

| UID        | 名称     | 图标       | 说明                   |
| ---------- | -------- | ---------- | ---------------------- |
| host       | 主机管理 | server     | 物理机、虚拟机等       |
| cloud      | 云资源   | cloud      | 云厂商资源             |
| network    | 网络设备 | network    | 路由器、交换机、防火墙 |
| database   | 数据库   | database   | MySQL、PostgreSQL 等   |
| middleware | 中间件   | middleware | Redis、Kafka、MQ 等    |
| container  | 容器服务 | container  | K8s、Docker 等         |
| storage    | 存储设备 | storage    | NAS、SAN 等            |
| security   | 安全设备 | security   | WAF、IDS 等            |
| iam        | 身份权限 | user       | 用户、角色、策略       |
| custom     | 自定义   | custom     | 用户自定义模型         |

### 初始化内置分组

```
POST /cmdb/model-groups/init
```

### 创建模型分组

```
POST /cmdb/model-groups
```

**请求参数**

| 字段        | 类型   | 必填 | 说明         |
| ----------- | ------ | ---- | ------------ |
| uid         | string | ✓    | 分组唯一标识 |
| name        | string | ✓    | 分组名称     |
| icon        | string |      | 图标         |
| sort_order  | int    |      | 排序顺序     |
| description | string |      | 描述         |

### 获取分组列表

```
GET /cmdb/model-groups
```

### 获取分组及其模型（树形结构）

> 用于前端左侧分组树 + 右侧模型列表的展示

```
GET /cmdb/model-groups/with-models
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "groups": [
      {
        "id": 1,
        "uid": "cloud",
        "name": "云资源",
        "icon": "cloud",
        "sort_order": 2,
        "is_builtin": true,
        "models": [
          {
            "id": 1,
            "uid": "aliyun_ecs",
            "name": "阿里云ECS",
            "model_group_id": 1,
            "category": "compute",
            "provider": "aliyun"
          },
          {
            "id": 2,
            "uid": "aliyun_rds",
            "name": "阿里云RDS",
            "model_group_id": 1,
            "category": "database",
            "provider": "aliyun"
          }
        ]
      },
      {
        "id": 2,
        "uid": "network",
        "name": "网络设备",
        "icon": "network",
        "sort_order": 3,
        "is_builtin": true,
        "models": []
      }
    ]
  }
}
```

### 获取/更新/删除分组

```
GET    /cmdb/model-groups/{uid}
PUT    /cmdb/model-groups/{uid}
DELETE /cmdb/model-groups/{uid}
```

> 注意：内置分组不可删除，有模型的分组不可删除

---

## 模型关系类型管理

### 创建模型关系类型

> 定义两个模型之间可以建立什么样的关系

```
POST /cmdb/model-relations
```

**请求参数**

| 字段             | 类型   | 必填 | 说明                                                     |
| ---------------- | ------ | ---- | -------------------------------------------------------- |
| uid              | string | ✓    | 关系类型唯一标识，如 `ecs_bindto_eip`                    |
| name             | string | ✓    | 关系名称，如 "ECS 绑定 EIP"                              |
| source_model_uid | string | ✓    | 源模型 UID                                               |
| target_model_uid | string | ✓    | 目标模型 UID                                             |
| relation_type    | string | ✓    | 关系类型: belongs_to/contains/bindto/connects/depends_on |
| direction        | string |      | 方向: one_to_one/one_to_many/many_to_many                |
| source_to_target | string |      | 源到目标的描述，如 "绑定"                                |
| target_to_source | string |      | 目标到源的描述，如 "被绑定"                              |
| description      | string |      | 描述                                                     |

**请求示例**

```json
{
  "uid": "ecs_belongs_to_vpc",
  "name": "ECS属于VPC",
  "source_model_uid": "aliyun_ecs",
  "target_model_uid": "aliyun_vpc",
  "relation_type": "belongs_to",
  "direction": "many_to_one",
  "source_to_target": "属于",
  "target_to_source": "包含"
}
```

### 获取模型关系类型列表

```
GET /cmdb/model-relations
```

**查询参数**

| 参数             | 类型   | 说明              |
| ---------------- | ------ | ----------------- |
| source_model_uid | string | 源模型 UID 过滤   |
| target_model_uid | string | 目标模型 UID 过滤 |
| relation_type    | string | 关系类型过滤      |
| offset           | int    | 偏移量            |
| limit            | int    | 每页数量          |

### 获取/更新/删除模型关系类型

```
GET    /cmdb/model-relations/{uid}
PUT    /cmdb/model-relations/{uid}
DELETE /cmdb/model-relations/{uid}
```

---

## 实例关系管理

### 创建实例关系

```
POST /cmdb/instance-relations
```

**请求参数**

| 字段               | 类型   | 必填 | 说明         |
| ------------------ | ------ | ---- | ------------ |
| source_instance_id | int64  | ✓    | 源实例 ID    |
| target_instance_id | int64  | ✓    | 目标实例 ID  |
| relation_type_uid  | string | ✓    | 关系类型 UID |
| tenant_id          | string | ✓    | 租户 ID      |

**请求示例**

```json
{
  "source_instance_id": 1001,
  "target_instance_id": 2001,
  "relation_type_uid": "ecs_belongs_to_vpc",
  "tenant_id": "tenant-001"
}
```

### 批量创建实例关系

```
POST /cmdb/instance-relations/batch
```

### 获取实例关系列表

```
GET /cmdb/instance-relations
```

**查询参数**

| 参数               | 类型   | 说明         |
| ------------------ | ------ | ------------ |
| source_instance_id | int64  | 源实例 ID    |
| target_instance_id | int64  | 目标实例 ID  |
| relation_type_uid  | string | 关系类型 UID |
| tenant_id          | string | 租户 ID      |

### 删除实例关系

```
DELETE /cmdb/instance-relations/{id}
```

---

## 拓扑视图

### 获取实例拓扑图

> 获取指定实例的关联拓扑图，用于可视化展示

```
GET /cmdb/topology/instance/{id}
```

**查询参数**

| 参数      | 类型   | 默认值 | 说明                             |
| --------- | ------ | ------ | -------------------------------- |
| depth     | int    | 1      | 查询深度                         |
| direction | string | both   | 查询方向: both/outgoing/incoming |
| model_uid | string |        | 按模型过滤                       |
| tenant_id | string |        | 租户 ID                          |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "nodes": [
      {
        "id": 1001,
        "model_uid": "aliyun_ecs",
        "model_name": "阿里云ECS",
        "asset_id": "i-bp001",
        "asset_name": "web-server-01",
        "category": "compute",
        "icon": "ecs"
      },
      {
        "id": 2001,
        "model_uid": "aliyun_vpc",
        "model_name": "阿里云VPC",
        "asset_id": "vpc-001",
        "asset_name": "prod-vpc",
        "category": "network"
      }
    ],
    "edges": [
      {
        "source_id": 1001,
        "target_id": 2001,
        "relation_type_uid": "ecs_belongs_to_vpc",
        "relation_name": "ECS属于VPC",
        "relation_type": "belongs_to"
      }
    ]
  }
}
```

### 获取模型拓扑图

> 获取模型间的关系定义图，用于展示资源模型架构

```
GET /cmdb/topology/model
```

**查询参数**

| 参数     | 类型   | 说明       |
| -------- | ------ | ---------- |
| provider | string | 云厂商过滤 |

### 获取关联实例列表

> 获取指定实例的关联实例列表

```
GET /cmdb/topology/related/{id}
```

**查询参数**

| 参数              | 类型   | 说明              |
| ----------------- | ------ | ----------------- |
| relation_type_uid | string | 关系类型 UID 过滤 |

---

## 关系类型说明

| 类型       | 说明     | 示例            |
| ---------- | -------- | --------------- |
| belongs_to | 从属关系 | ECS 属于 VPC    |
| contains   | 包含关系 | VPC 包含 Subnet |
| bindto     | 绑定关系 | ECS 绑定 EIP    |
| connects   | 连接关系 | ECS 连接 RDS    |
| depends_on | 依赖关系 | App 依赖 RDS    |
