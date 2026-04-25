# 数据库资源 API 文档

本文档描述了云数据库资源（RDS、Redis、MongoDB）的 API 接口。

## 基础信息

- 基础路径: `/api/v1/cam/databases`
- 认证方式: Bearer Token
- 响应格式: JSON

## 通用响应格式

```json
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

## 通用查询参数

| 参数       | 类型   | 必填 | 描述                 |
| ---------- | ------ | ---- | -------------------- |
| account_id | int    | 是   | 云账号ID             |
| region     | string | 是   | 地域，如 cn-hangzhou |

---

## RDS 接口

### 获取 RDS 实例列表

获取指定云账号和地域的 RDS 实例列表。

**请求**

```
GET /api/v1/cam/databases/rds
```

**查询参数**

| 参数       | 类型   | 必填 | 描述                                              |
| ---------- | ------ | ---- | ------------------------------------------------- |
| account_id | int    | 是   | 云账号ID                                          |
| region     | string | 是   | 地域                                              |
| engine     | string | 否   | 数据库引擎: mysql, postgresql, mariadb, sqlserver |
| status     | string | 否   | 实例状态: running, stopped, creating 等           |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "instances": [
      {
        "instance_id": "rm-bp1xxxxx",
        "instance_name": "prod-mysql-01",
        "engine": "mysql",
        "engine_version": "8.0",
        "instance_class": "rds.mysql.s2.large",
        "instance_status": "running",
        "connection_string": "rm-bp1xxxxx.mysql.rds.aliyuncs.com",
        "port": 3306,
        "vpc_id": "vpc-bp1xxxxx",
        "vswitch_id": "vsw-bp1xxxxx",
        "zone_id": "cn-hangzhou-h",
        "region_id": "cn-hangzhou",
        "storage_type": "cloud_essd",
        "storage_size": 100,
        "max_connections": 2000,
        "max_iops": 10000,
        "create_time": "2024-01-15T10:30:00Z",
        "expire_time": "2025-01-15T10:30:00Z",
        "pay_type": "PrePaid",
        "tags": {
          "env": "production",
          "team": "backend"
        }
      }
    ],
    "total": 1
  }
}
```

### 获取 RDS 实例详情

获取指定 RDS 实例的详细信息。

**请求**

```
GET /api/v1/cam/databases/rds/:instance_id
```

**路径参数**

| 参数        | 类型   | 必填 | 描述       |
| ----------- | ------ | ---- | ---------- |
| instance_id | string | 是   | RDS 实例ID |

**查询参数**

| 参数       | 类型   | 必填 | 描述     |
| ---------- | ------ | ---- | -------- |
| account_id | int    | 是   | 云账号ID |
| region     | string | 是   | 地域     |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "instance_id": "rm-bp1xxxxx",
    "instance_name": "prod-mysql-01",
    "engine": "mysql",
    "engine_version": "8.0",
    "instance_class": "rds.mysql.s2.large",
    "instance_status": "running",
    "connection_string": "rm-bp1xxxxx.mysql.rds.aliyuncs.com",
    "port": 3306,
    "vpc_id": "vpc-bp1xxxxx",
    "vswitch_id": "vsw-bp1xxxxx",
    "zone_id": "cn-hangzhou-h",
    "region_id": "cn-hangzhou",
    "storage_type": "cloud_essd",
    "storage_size": 100,
    "max_connections": 2000,
    "max_iops": 10000,
    "create_time": "2024-01-15T10:30:00Z",
    "expire_time": "2025-01-15T10:30:00Z",
    "pay_type": "PrePaid",
    "tags": {}
  }
}
```

---

## Redis 接口

### 获取 Redis 实例列表

获取指定云账号和地域的 Redis 实例列表。

**请求**

```
GET /api/v1/cam/databases/redis
```

**查询参数**

| 参数         | 类型   | 必填 | 描述                                 |
| ------------ | ------ | ---- | ------------------------------------ |
| account_id   | int    | 是   | 云账号ID                             |
| region       | string | 是   | 地域                                 |
| architecture | string | 否   | 架构类型: standard, cluster, rwsplit |
| status       | string | 否   | 实例状态                             |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "instances": [
      {
        "instance_id": "r-bp1xxxxx",
        "instance_name": "prod-redis-01",
        "instance_class": "redis.master.small.default",
        "instance_status": "running",
        "engine_version": "6.0",
        "architecture": "cluster",
        "node_type": "double",
        "shard_count": 4,
        "connection_domain": "r-bp1xxxxx.redis.rds.aliyuncs.com",
        "port": 6379,
        "vpc_id": "vpc-bp1xxxxx",
        "vswitch_id": "vsw-bp1xxxxx",
        "zone_id": "cn-hangzhou-h",
        "region_id": "cn-hangzhou",
        "capacity": 4096,
        "bandwidth": 96,
        "connections": 20000,
        "qps": 100000,
        "create_time": "2024-01-15T10:30:00Z",
        "expire_time": "2025-01-15T10:30:00Z",
        "pay_type": "PrePaid",
        "tags": {}
      }
    ],
    "total": 1
  }
}
```

### 获取 Redis 实例详情

获取指定 Redis 实例的详细信息。

**请求**

```
GET /api/v1/cam/databases/redis/:instance_id
```

**路径参数**

| 参数        | 类型   | 必填 | 描述         |
| ----------- | ------ | ---- | ------------ |
| instance_id | string | 是   | Redis 实例ID |

**查询参数**

| 参数       | 类型   | 必填 | 描述     |
| ---------- | ------ | ---- | -------- |
| account_id | int    | 是   | 云账号ID |
| region     | string | 是   | 地域     |

---

## MongoDB 接口

### 获取 MongoDB 实例列表

获取指定云账号和地域的 MongoDB 实例列表。

**请求**

```
GET /api/v1/cam/databases/mongodb
```

**查询参数**

| 参数       | 类型   | 必填 | 描述                                        |
| ---------- | ------ | ---- | ------------------------------------------- |
| account_id | int    | 是   | 云账号ID                                    |
| region     | string | 是   | 地域                                        |
| db_type    | string | 否   | 数据库类型: replicate, sharding, serverless |
| status     | string | 否   | 实例状态                                    |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "instances": [
      {
        "instance_id": "dds-bp1xxxxx",
        "instance_name": "prod-mongodb-01",
        "db_type": "replicate",
        "engine_version": "5.0",
        "instance_class": "dds.mongo.standard",
        "instance_status": "running",
        "storage_engine": "WiredTiger",
        "replica_set_name": "mgset-xxxxx",
        "connection_string": "dds-bp1xxxxx.mongodb.rds.aliyuncs.com:3717",
        "port": 3717,
        "vpc_id": "vpc-bp1xxxxx",
        "vswitch_id": "vsw-bp1xxxxx",
        "zone_id": "cn-hangzhou-h",
        "region_id": "cn-hangzhou",
        "storage_type": "cloud_essd",
        "storage_size": 50,
        "max_connections": 1000,
        "max_iops": 5000,
        "create_time": "2024-01-15T10:30:00Z",
        "expire_time": "2025-01-15T10:30:00Z",
        "pay_type": "PrePaid",
        "tags": {}
      }
    ],
    "total": 1
  }
}
```

### 获取 MongoDB 实例详情

获取指定 MongoDB 实例的详细信息。

**请求**

```
GET /api/v1/cam/databases/mongodb/:instance_id
```

**路径参数**

| 参数        | 类型   | 必填 | 描述           |
| ----------- | ------ | ---- | -------------- |
| instance_id | string | 是   | MongoDB 实例ID |

**查询参数**

| 参数       | 类型   | 必填 | 描述     |
| ---------- | ------ | ---- | -------- |
| account_id | int    | 是   | 云账号ID |
| region     | string | 是   | 地域     |

---

## 数据模型

### RDSInstanceVO

| 字段              | 类型   | 描述         |
| ----------------- | ------ | ------------ |
| instance_id       | string | 实例ID       |
| instance_name     | string | 实例名称     |
| engine            | string | 数据库引擎   |
| engine_version    | string | 引擎版本     |
| instance_class    | string | 实例规格     |
| instance_status   | string | 实例状态     |
| connection_string | string | 连接地址     |
| port              | int    | 端口         |
| vpc_id            | string | VPC ID       |
| vswitch_id        | string | 交换机ID     |
| zone_id           | string | 可用区ID     |
| region_id         | string | 地域ID       |
| storage_type      | string | 存储类型     |
| storage_size      | int    | 存储大小(GB) |
| max_connections   | int    | 最大连接数   |
| max_iops          | int    | 最大IOPS     |
| create_time       | string | 创建时间     |
| expire_time       | string | 过期时间     |
| pay_type          | string | 付费类型     |
| tags              | object | 标签         |

### RedisInstanceVO

| 字段              | 类型   | 描述       |
| ----------------- | ------ | ---------- |
| instance_id       | string | 实例ID     |
| instance_name     | string | 实例名称   |
| instance_class    | string | 实例规格   |
| instance_status   | string | 实例状态   |
| engine_version    | string | 引擎版本   |
| architecture      | string | 架构类型   |
| node_type         | string | 节点类型   |
| shard_count       | int    | 分片数     |
| connection_domain | string | 连接地址   |
| port              | int    | 端口       |
| vpc_id            | string | VPC ID     |
| vswitch_id        | string | 交换机ID   |
| zone_id           | string | 可用区ID   |
| region_id         | string | 地域ID     |
| capacity          | int    | 容量(MB)   |
| bandwidth         | int    | 带宽(Mbps) |
| connections       | int    | 最大连接数 |
| qps               | int    | QPS        |
| create_time       | string | 创建时间   |
| expire_time       | string | 过期时间   |
| pay_type          | string | 付费类型   |
| tags              | object | 标签       |

### MongoDBInstanceVO

| 字段              | 类型   | 描述         |
| ----------------- | ------ | ------------ |
| instance_id       | string | 实例ID       |
| instance_name     | string | 实例名称     |
| db_type           | string | 数据库类型   |
| engine_version    | string | 引擎版本     |
| instance_class    | string | 实例规格     |
| instance_status   | string | 实例状态     |
| storage_engine    | string | 存储引擎     |
| replica_set_name  | string | 副本集名称   |
| connection_string | string | 连接地址     |
| port              | int    | 端口         |
| vpc_id            | string | VPC ID       |
| vswitch_id        | string | 交换机ID     |
| zone_id           | string | 可用区ID     |
| region_id         | string | 地域ID       |
| storage_type      | string | 存储类型     |
| storage_size      | int    | 存储大小(GB) |
| max_connections   | int    | 最大连接数   |
| max_iops          | int    | 最大IOPS     |
| create_time       | string | 创建时间     |
| expire_time       | string | 过期时间     |
| pay_type          | string | 付费类型     |
| tags              | object | 标签         |

---

## 错误码

| 错误码 | 描述           |
| ------ | -------------- |
| 400    | 请求参数错误   |
| 404    | 资源不存在     |
| 500    | 服务器内部错误 |

---

## 支持的云厂商

| 云厂商             | RDS 服务  | Redis 服务  | MongoDB 服务 |
| ------------------ | --------- | ----------- | ------------ |
| 阿里云 (aliyun)    | RDS       | Redis       | MongoDB      |
| AWS                | RDS       | ElastiCache | DocumentDB   |
| 华为云 (huawei)    | RDS       | DCS         | DDS          |
| 腾讯云 (tencent)   | CDB       | Redis       | MongoDB      |
| 火山引擎 (volcano) | RDS MySQL | Redis       | MongoDB      |
