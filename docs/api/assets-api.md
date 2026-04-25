# 云资产查询 API 文档

> 版本: 2.2.0 | 更新时间: 2026-01-30

## 概述

统一的云资产查询接口，从本地数据库读取已同步的资产数据。支持 ECS、RDS、Redis、MongoDB、VPC、EIP 六种资产类型。

## Base URL

```
/api/v1/cam/assets
```

## 多租户认证

所有资产 API 都需要通过 `X-Tenant-ID` Header 传递租户ID，实现多租户数据隔离。

### 请求头

| Header 名称  | 类型   | 必填 | 说明             |
| ------------ | ------ | ---- | ---------------- |
| X-Tenant-ID  | string | 是   | 租户ID           |
| Content-Type | string | 否   | application/json |

### 认证方式优先级

1. **请求头** `X-Tenant-ID`（推荐）
2. **JWT Token** `claims.tenant_id`
3. **查询参数** `tenant_id`（已废弃，仅用于开发测试）

### 请求示例

```bash
# 正确方式：通过 Header 传递租户ID
curl -X GET "http://localhost:8080/api/v1/cam/assets/ecs" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json"

# 带查询参数
curl -X GET "http://localhost:8080/api/v1/cam/assets/ecs?provider=aliyun&region=cn-hangzhou" \
  -H "X-Tenant-ID: tenant-001"
```

### 认证失败响应

缺少租户ID时返回：

```json
{
  "code": 400,
  "message": "租户ID不能为空"
}
```

---

## 统一搜索 (推荐)

跨资产类型的全文搜索接口，支持按关键词匹配资产ID、名称、IP地址等，返回匹配信息供前端高亮显示。

### 搜索资产

```http
GET /api/v1/cam/assets/search
```

**查询参数:**

| 参数       | 类型    | 必填 | 默认值 | 说明                                              |
| ---------- | ------- | ---- | ------ | ------------------------------------------------- |
| keyword    | string  | 是   | -      | 搜索关键词，匹配资产ID、名称、IP地址、连接串等    |
| types      | string  | 否   | 全部   | 资产类型，逗号分隔: ecs,rds,redis,mongodb,vpc,eip |
| provider   | string  | 否   | -      | 云厂商: aliyun, aws, huawei, tencent, volcano     |
| account_id | integer | 否   | -      | 云账号ID                                          |
| region     | string  | 否   | -      | 地域                                              |
| offset     | integer | 否   | 0      | 分页偏移量                                        |
| limit      | integer | 否   | 20     | 每页数量                                          |

**搜索匹配的字段:**

| 字段              | 说明              |
| ----------------- | ----------------- |
| asset_id          | 云厂商实例ID      |
| asset_name        | 资产名称          |
| private_ip        | 内网IP (ECS)      |
| public_ip         | 公网IP (ECS)      |
| ip_address        | IP地址 (EIP)      |
| connection_string | 连接地址 (数据库) |
| cidr_block        | CIDR块 (VPC)      |

**请求示例:**

```bash
# 搜索包含 "192.168" 的所有资产
curl "http://localhost:8080/api/v1/cam/assets/search?keyword=192.168" \
  -H "X-Tenant-ID: tenant-001"

# 只搜索 ECS 和 EIP
curl "http://localhost:8080/api/v1/cam/assets/search?keyword=web-server&types=ecs,eip" \
  -H "X-Tenant-ID: tenant-001"

# 搜索阿里云的资产
curl "http://localhost:8080/api/v1/cam/assets/search?keyword=i-bp1&provider=aliyun" \
  -H "X-Tenant-ID: tenant-001"
```

**响应示例:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "asset_id": "i-bp1xxxxx",
        "asset_name": "web-server-01",
        "asset_type": "ecs",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "attributes": {
          "private_ip": "192.168.1.10",
          "public_ip": "47.xxx.xxx.xxx"
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000,
        "matches": [
          {
            "field": "private_ip",
            "value": "192.168.1.10",
            "label": "内网IP"
          }
        ]
      },
      {
        "id": 5,
        "asset_id": "vpc-bp1xxxxx",
        "asset_name": "prod-vpc",
        "asset_type": "vpc",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "Available",
        "attributes": {
          "cidr_block": "192.168.0.0/16"
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000,
        "matches": [
          {
            "field": "cidr_block",
            "value": "192.168.0.0/16",
            "label": "CIDR块"
          }
        ]
      }
    ],
    "total": 2,
    "keyword": "192.168"
  }
}
```

### 前端高亮实现

响应中的 `matches` 数组包含匹配信息，前端可以据此实现高亮：

```typescript
// 匹配信息类型
interface MatchInfo {
  field: string; // 匹配的字段名
  value: string; // 匹配的字段值
  label: string; // 字段显示名称
}

// 高亮函数
function highlightText(text: string, keyword: string): string {
  if (!keyword) return text;
  const regex = new RegExp(`(${keyword})`, "gi");
  return text.replace(regex, "<mark>$1</mark>");
}

// 渲染搜索结果
function renderSearchResult(item: SearchResultItem, keyword: string) {
  return {
    ...item,
    // 高亮资产名称
    highlightedName: highlightText(item.asset_name, keyword),
    // 高亮匹配的字段
    highlightedMatches: item.matches.map((m) => ({
      ...m,
      highlightedValue: highlightText(m.value, keyword),
    })),
  };
}
```

### React 组件示例

```tsx
import React from "react";

interface SearchResultProps {
  item: SearchResultItem;
  keyword: string;
}

const HighlightText: React.FC<{ text: string; keyword: string }> = ({
  text,
  keyword,
}) => {
  if (!keyword) return <>{text}</>;

  const parts = text.split(new RegExp(`(${keyword})`, "gi"));
  return (
    <>
      {parts.map((part, i) =>
        part.toLowerCase() === keyword.toLowerCase() ? (
          <mark key={i} className="bg-yellow-200">
            {part}
          </mark>
        ) : (
          part
        ),
      )}
    </>
  );
};

const SearchResultCard: React.FC<SearchResultProps> = ({ item, keyword }) => {
  return (
    <div className="p-4 border rounded-lg">
      <div className="flex items-center gap-2">
        <span className="px-2 py-1 text-xs bg-blue-100 rounded">
          {item.asset_type}
        </span>
        <span className="font-medium">
          <HighlightText text={item.asset_name} keyword={keyword} />
        </span>
      </div>
      <div className="text-sm text-gray-500 mt-1">
        <HighlightText text={item.asset_id} keyword={keyword} />
      </div>
      {item.matches.length > 0 && (
        <div className="mt-2 text-sm">
          <span className="text-gray-400">匹配: </span>
          {item.matches.map((match, i) => (
            <span key={i} className="mr-2">
              {match.label}:{" "}
              <HighlightText text={match.value} keyword={keyword} />
            </span>
          ))}
        </div>
      )}
    </div>
  );
};
```

---

## 通用查询参数

所有列表接口支持以下查询参数：

| 参数       | 类型    | 必填 | 默认值 | 说明                                          |
| ---------- | ------- | ---- | ------ | --------------------------------------------- |
| account_id | integer | 否   | -      | 云账号ID                                      |
| provider   | string  | 否   | -      | 云厂商: aliyun, aws, huawei, tencent, volcano |
| region     | string  | 否   | -      | 地域，如 cn-hangzhou                          |
| status     | string  | 否   | -      | 实例状态，如 running, stopped                 |
| name       | string  | 否   | -      | 实例名称（模糊搜索）                          |
| offset     | integer | 否   | 0      | 分页偏移量                                    |
| limit      | integer | 否   | 20     | 每页数量                                      |

> ⚠️ 注意：火山引擎的 provider 值是 `volcano`，不是 `volcengine`

---

## ECS 云虚拟机

### 获取 ECS 列表

```http
GET /api/v1/cam/assets/ecs
```

**ECS 专用查询参数:**

| 参数       | 类型   | 必填 | 说明   |
| ---------- | ------ | ---- | ------ |
| private_ip | string | 否   | 内网IP |
| public_ip  | string | 否   | 公网IP |
| vpc_id     | string | 否   | VPC ID |

**请求示例:**

```bash
# 获取所有 ECS
curl "http://localhost:8080/api/v1/cam/assets/ecs?provider=aliyun&region=cn-hangzhou&limit=10" \
  -H "X-Tenant-ID: tenant-001"

# 按内网IP过滤
curl "http://localhost:8080/api/v1/cam/assets/ecs?private_ip=172.16.0.10" \
  -H "X-Tenant-ID: tenant-001"

# 按公网IP过滤
curl "http://localhost:8080/api/v1/cam/assets/ecs?public_ip=47.xxx.xxx.xxx" \
  -H "X-Tenant-ID: tenant-001"

# 按VPC过滤
curl "http://localhost:8080/api/v1/cam/assets/ecs?vpc_id=vpc-bp1xxxxx" \
  -H "X-Tenant-ID: tenant-001"
```

**响应示例:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "asset_id": "i-bp1xxxxx",
        "asset_name": "web-server-01",
        "asset_type": "ecs",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "attributes": {
          "cpu": 4,
          "memory": 8192,
          "os_type": "linux",
          "instance_type": "ecs.c6.xlarge",
          "private_ip": "172.16.0.10",
          "public_ip": "47.xxx.xxx.xxx"
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000
      }
    ],
    "total": 100
  }
}
```

### 获取 ECS 详情

```http
GET /api/v1/cam/assets/ecs/{asset_id}
```

**路径参数:**
| 参数 | 类型 | 必填 | 说明 |
| -------- | ------ | ---- | ------------------ |
| asset_id | string | 是 | 云厂商实例ID |

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/ecs/i-bp1xxxxx?provider=aliyun" \
  -H "X-Tenant-ID: tenant-001"
```

---

## RDS 关系型数据库

### 获取 RDS 列表

```http
GET /api/v1/cam/assets/rds
```

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/rds?provider=aliyun&region=cn-hangzhou" \
  -H "X-Tenant-ID: tenant-001"
```

**响应示例:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 2,
        "asset_id": "rm-bp1xxxxx",
        "asset_name": "prod-mysql-01",
        "asset_type": "rds",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "attributes": {
          "engine": "mysql",
          "engine_version": "8.0",
          "instance_class": "rds.mysql.s2.large",
          "storage_size": 100,
          "connection_string": "rm-bp1xxxxx.mysql.rds.aliyuncs.com",
          "port": 3306
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000
      }
    ],
    "total": 50
  }
}
```

### 获取 RDS 详情

```http
GET /api/v1/cam/assets/rds/{asset_id}
```

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/rds/rm-bp1xxxxx?provider=aliyun" \
  -H "X-Tenant-ID: tenant-001"
```

---

## Redis 缓存

### 获取 Redis 列表

```http
GET /api/v1/cam/assets/redis
```

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/redis?provider=aliyun&region=cn-hangzhou" \
  -H "X-Tenant-ID: tenant-001"
```

**响应示例:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 3,
        "asset_id": "r-bp1xxxxx",
        "asset_name": "cache-redis-01",
        "asset_type": "redis",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "attributes": {
          "engine_version": "6.0",
          "architecture": "cluster",
          "capacity": 4096,
          "shard_count": 4,
          "connection_domain": "r-bp1xxxxx.redis.rds.aliyuncs.com",
          "port": 6379
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000
      }
    ],
    "total": 30
  }
}
```

### 获取 Redis 详情

```http
GET /api/v1/cam/assets/redis/{asset_id}
```

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/redis/r-bp1xxxxx?provider=aliyun" \
  -H "X-Tenant-ID: tenant-001"
```

---

## MongoDB 文档数据库

### 获取 MongoDB 列表

```http
GET /api/v1/cam/assets/mongodb
```

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/mongodb?provider=aliyun&region=cn-hangzhou" \
  -H "X-Tenant-ID: tenant-001"
```

**响应示例:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 4,
        "asset_id": "dds-bp1xxxxx",
        "asset_name": "mongo-cluster-01",
        "asset_type": "mongodb",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "attributes": {
          "engine_version": "5.0",
          "db_type": "sharding",
          "storage_size": 200,
          "shard_count": 3,
          "connection_string": "dds-bp1xxxxx.mongodb.rds.aliyuncs.com:3717",
          "port": 3717
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000
      }
    ],
    "total": 20
  }
}
```

### 获取 MongoDB 详情

```http
GET /api/v1/cam/assets/mongodb/{asset_id}
```

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/mongodb/dds-bp1xxxxx?provider=aliyun" \
  -H "X-Tenant-ID: tenant-001"
```

---

## VPC 虚拟私有云

### 获取 VPC 列表

```http
GET /api/v1/cam/assets/vpc
```

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/vpc?provider=aliyun&region=cn-hangzhou" \
  -H "X-Tenant-ID: tenant-001"
```

**响应示例:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 5,
        "asset_id": "vpc-bp1xxxxx",
        "asset_name": "prod-vpc-01",
        "asset_type": "vpc",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "Available",
        "attributes": {
          "cidr_block": "172.16.0.0/12",
          "secondary_cidrs": ["10.0.0.0/8"],
          "ipv6_cidr_block": "",
          "enable_ipv6": false,
          "is_default": false,
          "vswitch_count": 5,
          "route_table_count": 2,
          "nat_gateway_count": 1,
          "security_group_count": 10,
          "description": "生产环境VPC"
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000
      }
    ],
    "total": 10
  }
}
```

### 获取 VPC 详情

```http
GET /api/v1/cam/assets/vpc/{asset_id}
```

**路径参数:**
| 参数 | 类型 | 必填 | 说明 |
| -------- | ------ | ---- | ------ |
| asset_id | string | 是 | VPC ID |

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/vpc/vpc-bp1xxxxx?provider=aliyun" \
  -H "X-Tenant-ID: tenant-001"
```

---

## EIP 弹性公网IP

### 获取 EIP 列表

```http
GET /api/v1/cam/assets/eip
```

**EIP 专用查询参数:**

| 参数          | 类型   | 必填 | 说明                                                                   |
| ------------- | ------ | ---- | ---------------------------------------------------------------------- |
| ip_address    | string | 否   | IP地址（精确匹配）                                                     |
| instance_id   | string | 否   | 绑定的实例ID                                                           |
| instance_type | string | 否   | 绑定的实例类型: EcsInstance, SlbInstance, Nat, HaVip, NetworkInterface |
| vpc_id        | string | 否   | VPC ID                                                                 |
| isp           | string | 否   | 线路类型: BGP, BGP_PRO, ChinaTelecom, ChinaUnicom, ChinaMobile         |
| bindable      | string | 否   | 绑定状态: bound(已绑定), unbound(未绑定)                               |

**instance_type 可选值:**

| 值               | 说明         |
| ---------------- | ------------ |
| EcsInstance      | ECS实例      |
| SlbInstance      | 负载均衡     |
| Nat              | NAT网关      |
| HaVip            | 高可用虚拟IP |
| NetworkInterface | 弹性网卡     |

**请求示例:**

```bash
# 获取所有 EIP
curl "http://localhost:8080/api/v1/cam/assets/eip?provider=aliyun&region=cn-hangzhou" \
  -H "X-Tenant-ID: tenant-001"

# 查询绑定到 ECS 的 EIP
curl "http://localhost:8080/api/v1/cam/assets/eip?instance_type=EcsInstance" \
  -H "X-Tenant-ID: tenant-001"

# 查询绑定到指定实例的 EIP
curl "http://localhost:8080/api/v1/cam/assets/eip?instance_id=i-bp1xxxxx" \
  -H "X-Tenant-ID: tenant-001"

# 查询未绑定的 EIP
curl "http://localhost:8080/api/v1/cam/assets/eip?bindable=unbound" \
  -H "X-Tenant-ID: tenant-001"

# 查询已绑定的 EIP
curl "http://localhost:8080/api/v1/cam/assets/eip?bindable=bound" \
  -H "X-Tenant-ID: tenant-001"

# 按 IP 地址查询
curl "http://localhost:8080/api/v1/cam/assets/eip?ip_address=47.xxx.xxx.xxx" \
  -H "X-Tenant-ID: tenant-001"

# 按 VPC 过滤
curl "http://localhost:8080/api/v1/cam/assets/eip?vpc_id=vpc-bp1xxxxx" \
  -H "X-Tenant-ID: tenant-001"

# 按线路类型过滤
curl "http://localhost:8080/api/v1/cam/assets/eip?isp=BGP" \
  -H "X-Tenant-ID: tenant-001"
```

**响应示例:**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 6,
        "asset_id": "eip-bp1xxxxx",
        "asset_name": "web-eip-01",
        "asset_type": "eip",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "InUse",
        "attributes": {
          "ip_address": "47.xxx.xxx.xxx",
          "bandwidth": 100,
          "internet_charge_type": "PayByTraffic",
          "isp": "BGP",
          "instance_id": "i-bp1xxxxx",
          "instance_type": "EcsInstance",
          "instance_name": "web-server-01",
          "vpc_id": "vpc-bp1xxxxx",
          "charge_type": "PostPaid",
          "creation_time": "2024-01-15T10:00:00Z",
          "expired_time": ""
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000
      }
    ],
    "total": 25
  }
}
```

### 获取 EIP 详情

```http
GET /api/v1/cam/assets/eip/{asset_id}
```

**路径参数:**
| 参数 | 类型 | 必填 | 说明 |
| -------- | ------ | ---- | --------------- |
| asset_id | string | 是 | EIP Allocation ID |

**请求示例:**

```bash
curl "http://localhost:8080/api/v1/cam/assets/eip/eip-bp1xxxxx?provider=aliyun" \
  -H "X-Tenant-ID: tenant-001"
```

---

## 响应结构

### 统一资产对象 (Asset)

| 字段        | 类型    | 说明                                          |
| ----------- | ------- | --------------------------------------------- |
| id          | integer | 数据库ID                                      |
| asset_id    | string  | 云厂商实例ID                                  |
| asset_name  | string  | 实例名称                                      |
| asset_type  | string  | 资产类型: ecs, rds, redis, mongodb, vpc, eip  |
| tenant_id   | string  | 租户ID                                        |
| account_id  | integer | 云账号ID                                      |
| provider    | string  | 云厂商: aliyun, aws, huawei, tencent, volcano |
| region      | string  | 地域                                          |
| status      | string  | 实例状态                                      |
| attributes  | object  | 扩展属性（不同资产类型有不同字段）            |
| create_time | integer | 创建时间（毫秒时间戳）                        |
| update_time | integer | 更新时间（毫秒时间戳）                        |

### 错误响应

```json
{
  "code": 404,
  "msg": "实例不存在"
}
```

| 错误码 | 说明           |
| ------ | -------------- |
| 0      | 成功           |
| 400    | 请求参数错误   |
| 400    | 租户ID不能为空 |
| 404    | 资源不存在     |
| 500    | 服务器内部错误 |

---

## 前端集成示例

### TypeScript 类型定义

```typescript
interface Asset {
  id: number;
  asset_id: string;
  asset_name: string;
  asset_type: "ecs" | "rds" | "redis" | "mongodb" | "vpc" | "eip";
  tenant_id: string;
  account_id: number;
  provider: "aliyun" | "aws" | "huawei" | "tencent" | "volcano";
  region: string;
  status: string;
  attributes: Record<string, any>;
  create_time: number;
  update_time: number;
}

interface AssetListResponse {
  code: number;
  msg: string;
  data: {
    items: Asset[];
    total: number;
  };
}

interface AssetQueryParams {
  account_id?: number;
  provider?: string;
  region?: string;
  status?: string;
  name?: string;
  offset?: number;
  limit?: number;
}

// ECS 专用查询参数
interface ECSQueryParams extends AssetQueryParams {
  private_ip?: string; // 内网IP
  public_ip?: string; // 公网IP
  vpc_id?: string; // VPC ID
}

// EIP 专用查询参数
interface EIPQueryParams extends AssetQueryParams {
  ip_address?: string; // IP地址（精确匹配）
  instance_id?: string; // 绑定的实例ID
  instance_type?: string; // 绑定的实例类型
  vpc_id?: string; // VPC ID
  isp?: string; // 线路类型
  bindable?: "bound" | "unbound"; // 绑定状态
}

// 搜索相关类型
interface MatchInfo {
  field: string; // 匹配的字段名
  value: string; // 匹配的字段值
  label: string; // 字段显示名称
}

interface SearchResultItem extends Asset {
  matches: MatchInfo[]; // 匹配信息，用于前端高亮
}

interface SearchResponse {
  code: number;
  msg: string;
  data: {
    items: SearchResultItem[];
    total: number;
    keyword: string; // 返回搜索关键词
  };
}

interface SearchParams {
  keyword: string; // 搜索关键词 (必填)
  types?: string; // 资产类型，逗号分隔
  provider?: string;
  account_id?: number;
  region?: string;
  offset?: number;
  limit?: number;
}

// VPC 特有属性
interface VPCAttributes {
  cidr_block: string;
  secondary_cidrs: string[];
  ipv6_cidr_block: string;
  enable_ipv6: boolean;
  is_default: boolean;
  vswitch_count: number;
  route_table_count: number;
  nat_gateway_count: number;
  security_group_count: number;
  description: string;
}

// EIP 特有属性
interface EIPAttributes {
  ip_address: string;
  bandwidth: number;
  internet_charge_type: string;
  isp: string;
  instance_id: string;
  instance_type: string;
  instance_name: string;
  vpc_id: string;
  charge_type: string;
  creation_time: string;
  expired_time: string;
}
```

### API 调用示例

```typescript
// 创建带租户ID的请求头
function createHeaders(tenantId: string): HeadersInit {
  return {
    "X-Tenant-ID": tenantId,
    "Content-Type": "application/json",
  };
}

// 获取 ECS 列表
async function listECS(
  tenantId: string,
  params: AssetQueryParams,
): Promise<AssetListResponse> {
  const query = new URLSearchParams(params as any).toString();
  const response = await fetch(`/api/v1/cam/assets/ecs?${query}`, {
    headers: createHeaders(tenantId),
  });
  return response.json();
}

// 获取 RDS 列表
async function listRDS(
  tenantId: string,
  params: AssetQueryParams,
): Promise<AssetListResponse> {
  const query = new URLSearchParams(params as any).toString();
  const response = await fetch(`/api/v1/cam/assets/rds?${query}`, {
    headers: createHeaders(tenantId),
  });
  return response.json();
}

// 获取 VPC 列表
async function listVPC(
  tenantId: string,
  params: AssetQueryParams,
): Promise<AssetListResponse> {
  const query = new URLSearchParams(params as any).toString();
  const response = await fetch(`/api/v1/cam/assets/vpc?${query}`, {
    headers: createHeaders(tenantId),
  });
  return response.json();
}

// 获取 EIP 列表
async function listEIP(
  tenantId: string,
  params: AssetQueryParams,
): Promise<AssetListResponse> {
  const query = new URLSearchParams(params as any).toString();
  const response = await fetch(`/api/v1/cam/assets/eip?${query}`, {
    headers: createHeaders(tenantId),
  });
  return response.json();
}

// 使用示例
const tenantId = "tenant-001";
const ecsData = await listECS(tenantId, {
  provider: "aliyun",
  region: "cn-hangzhou",
  status: "running",
  limit: 20,
});
```

### Axios 封装示例

```typescript
import axios from "axios";

const apiClient = axios.create({
  baseURL: "/api/v1/cam",
});

// 请求拦截器：自动添加租户ID
apiClient.interceptors.request.use((config) => {
  const tenantId = localStorage.getItem("tenantId") || "";
  config.headers["X-Tenant-ID"] = tenantId;
  return config;
});

// 资产 API
export const assetApi = {
  // 统一搜索
  search: (params: SearchParams) =>
    apiClient.get<SearchResponse>("/assets/search", { params }),

  listECS: (params: AssetQueryParams | ECSQueryParams) =>
    apiClient.get("/assets/ecs", { params }),
  getECS: (assetId: string, params?: { provider?: string }) =>
    apiClient.get(`/assets/ecs/${assetId}`, { params }),

  listRDS: (params: AssetQueryParams) =>
    apiClient.get("/assets/rds", { params }),
  getRDS: (assetId: string, params?: { provider?: string }) =>
    apiClient.get(`/assets/rds/${assetId}`, { params }),

  listRedis: (params: AssetQueryParams) =>
    apiClient.get("/assets/redis", { params }),
  getRedis: (assetId: string, params?: { provider?: string }) =>
    apiClient.get(`/assets/redis/${assetId}`, { params }),

  listMongoDB: (params: AssetQueryParams) =>
    apiClient.get("/assets/mongodb", { params }),
  getMongoDB: (assetId: string, params?: { provider?: string }) =>
    apiClient.get(`/assets/mongodb/${assetId}`, { params }),

  listVPC: (params: AssetQueryParams) =>
    apiClient.get("/assets/vpc", { params }),
  getVPC: (assetId: string, params?: { provider?: string }) =>
    apiClient.get(`/assets/vpc/${assetId}`, { params }),

  listEIP: (params: AssetQueryParams | EIPQueryParams) =>
    apiClient.get("/assets/eip", { params }),
  getEIP: (assetId: string, params?: { provider?: string }) =>
    apiClient.get(`/assets/eip/${assetId}`, { params }),
};
```

---

## 旧接口兼容

以下旧接口仍然可用，但建议使用新的 `/assets/*` 路由：

| 旧路由                        | 新路由                     | 说明                   |
| ----------------------------- | -------------------------- | ---------------------- |
| /api/v1/cam/databases/rds     | /api/v1/cam/assets/rds     | 旧路由不强制要求租户ID |
| /api/v1/cam/databases/redis   | /api/v1/cam/assets/redis   | 旧路由不强制要求租户ID |
| /api/v1/cam/databases/mongodb | /api/v1/cam/assets/mongodb | 旧路由不强制要求租户ID |
| /api/v1/cam/instances?uid=ecs | /api/v1/cam/assets/ecs     | 旧路由不强制要求租户ID |

> ⚠️ 注意：旧路由不强制要求租户ID，可能返回跨租户数据。建议尽快迁移到新路由。

---

## 资产同步

### 同步资产请求

```http
POST /api/v1/cam/tasks/sync
```

**请求头:**
| Header 名称 | 类型 | 必填 | 说明 |
| ----------- | ------ | ---- | ------ |
| X-Tenant-ID | string | 是 | 租户ID |

**请求体:**

```json
{
  "account_id": 1,
  "asset_types": ["ecs", "database", "network"],
  "regions": ["cn-hangzhou", "cn-shanghai"]
}
```

**asset_types 支持的值:**

| 值       | 说明                                 |
| -------- | ------------------------------------ |
| ecs      | 云虚拟机                             |
| rds      | 关系型数据库                         |
| redis    | Redis 缓存                           |
| mongodb  | MongoDB 文档数据库                   |
| vpc      | 虚拟私有云                           |
| eip      | 弹性公网IP                           |
| database | 聚合类型，展开为 rds, redis, mongodb |
| network  | 聚合类型，展开为 vpc, eip            |

**请求示例:**

```bash
# 同步所有资产类型
curl -X POST "http://localhost:8080/api/v1/cam/tasks/sync" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{"account_id": 1, "asset_types": ["ecs", "database", "network"]}'

# 只同步网络资产
curl -X POST "http://localhost:8080/api/v1/cam/tasks/sync" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{"account_id": 1, "asset_types": ["network"]}'
```
