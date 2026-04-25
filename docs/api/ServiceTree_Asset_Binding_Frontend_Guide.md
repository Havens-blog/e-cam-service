# 服务树资产绑定 - 前端对接指南

## 概述

服务树资产绑定功能实现了云资产与业务系统的关联，支持查询某个业务节点下的所有云资源（含 CMDB 资产详情），以及反查某个资产属于哪个业务节点。

## 根节点特殊行为 ⭐

根节点（`parent_id = 0`）作为「待分配资源池」，行为与普通节点不同：

| 场景                                | 返回内容                                   |
| ----------------------------------- | ------------------------------------------ |
| 根节点 + `include_children=false`   | **未绑定**到任何子节点的资产（待分配资源） |
| 根节点 + `include_children=true`    | 租户下**全部**资产                         |
| 普通节点 + `include_children=false` | 直接绑定到该节点的资产                     |
| 普通节点 + `include_children=true`  | 该节点及所有子节点的资产                   |

**前端适配要点：**

1. 根节点返回的资产 `binding_id` 为 0（因为没有真正的绑定记录）
2. 资产被绑定到子节点后，会自动从根节点的列表中「消失」
3. 解绑后，资产会自动「回到」根节点的待分配池
4. 同步任务不需要额外操作，新资产自动出现在根节点

**UI 建议：**

- 根节点可以显示为「待分配资源」或「资源池」
- 根节点的资产列表可以提供「分配到节点」的快捷操作
- 根节点 + `include_children=true` 可用于「全部资产」视图

## 基础路由

```
BASE_URL = /api/v1/cam/service-tree
```

所有请求必须携带 `X-Tenant-ID` Header。

## 新增 API（3 个）

### 1. 查询节点下的云资产列表

```
GET /api/v1/cam/service-tree/nodes/:id/assets
```

返回绑定信息 + CMDB 资产详情的聚合数据。

**请求参数**

| 参数             | 位置   | 类型   | 必填 | 说明                                   |
| ---------------- | ------ | ------ | ---- | -------------------------------------- |
| X-Tenant-ID      | Header | string | 是   | 租户ID                                 |
| id               | Path   | int    | 是   | 服务树节点ID                           |
| env_id           | Query  | int    | 否   | 环境ID，不传则查所有环境               |
| asset_type       | Query  | string | 否   | 资产类型过滤 (ecs/rds/redis/vpc/eip等) |
| include_children | Query  | bool   | 否   | 是否包含子节点的资产，默认 false       |
| offset           | Query  | int    | 否   | 偏移量，默认 0                         |
| limit            | Query  | int    | 否   | 每页数量，默认 20                      |

**响应示例**

```json
{
  "code": 0,
  "msg": "",
  "data": {
    "items": [
      {
        "binding_id": 1001,
        "node_id": 10,
        "env_id": 1,
        "bind_type": "manual",
        "id": 500,
        "asset_id": "i-bp1abc123def",
        "asset_name": "web-server-01",
        "asset_type": "ecs",
        "provider": "aliyun",
        "region": "cn-hangzhou",
        "status": "running",
        "account_id": 1,
        "attributes": {
          "cpu": 4,
          "memory": 8192,
          "os_type": "linux",
          "instance_type": "ecs.c6.xlarge",
          "private_ip": "172.16.0.10",
          "public_ip": "47.100.xx.xx",
          "vpc_id": "vpc-bp1xxx"
        },
        "create_time": 1706000000000,
        "update_time": 1706100000000
      }
    ],
    "total": 25
  }
}
```

### 2. 查询节点资产统计

```
GET /api/v1/cam/service-tree/nodes/:id/assets/stats
```

**请求参数**

| 参数             | 位置   | 类型   | 必填 | 说明           |
| ---------------- | ------ | ------ | ---- | -------------- |
| X-Tenant-ID      | Header | string | 是   | 租户ID         |
| id               | Path   | int    | 是   | 服务树节点ID   |
| include_children | Query  | bool   | 否   | 是否包含子节点 |

**响应示例**

```json
{
  "code": 0,
  "msg": "",
  "data": {
    "total": 42,
    "by_asset_type": {
      "ecs": 15,
      "rds": 8,
      "redis": 5,
      "vpc": 4,
      "eip": 10
    },
    "by_provider": {
      "aliyun": 25,
      "aws": 10,
      "huawei": 7
    }
  }
}
```

### 3. 查询资产所属节点

```
GET /api/v1/cam/service-tree/assets/:id/node
```

**请求参数**

| 参数        | 位置   | 类型   | 必填 | 说明             |
| ----------- | ------ | ------ | ---- | ---------------- |
| X-Tenant-ID | Header | string | 是   | 租户ID           |
| id          | Path   | int    | 是   | CMDB 资产实例 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "",
  "data": {
    "node_id": 10,
    "uid": "order-service",
    "name": "订单服务",
    "path": "/1/5/10",
    "level": 3
  }
}
```

**未绑定时返回**

```json
{
  "code": 404,
  "msg": "资产未绑定到任何节点"
}
```

## TypeScript 类型定义

```typescript
// ========== 请求参数 ==========

interface ListNodeAssetsParams {
  env_id?: number;
  asset_type?: string; // ecs | rds | redis | mongodb | vpc | eip | nas | oss | kafka | elasticsearch
  include_children?: boolean;
  offset?: number;
  limit?: number;
}

interface NodeAssetStatsParams {
  include_children?: boolean;
}

// ========== 响应类型 ==========

interface NodeAssetVO {
  // 绑定信息
  binding_id: number;
  node_id: number;
  env_id: number;
  bind_type: "manual" | "rule";

  // 资产详情 (来自 CMDB)
  id: number;
  asset_id: string; // 云厂商实例ID，如 i-bp1xxx
  asset_name: string;
  asset_type: string; // ecs | rds | redis | mongodb | vpc | eip ...
  provider: string; // aliyun | aws | huawei | tencent | volcano
  region: string;
  status: string; // running | stopped 等
  account_id: number;
  attributes: Record<string, any>; // 资产扩展属性
  create_time: number; // 毫秒时间戳
  update_time: number;
}

interface NodeAssetListResponse {
  items: NodeAssetVO[];
  total: number;
}

interface AssetStatsVO {
  total: number;
  by_asset_type: Record<string, number>; // { ecs: 15, rds: 8 }
  by_provider: Record<string, number>; // { aliyun: 25, aws: 10 }
}

interface AssetNodeVO {
  node_id: number;
  uid: string;
  name: string;
  path: string;
  level: number;
}
```

## API 服务封装

```typescript
import apiClient from "./apiClient";

const SERVICE_TREE_BASE = "/api/v1/cam/service-tree";

export const serviceTreeAssetApi = {
  /** 查询节点下的云资产列表 */
  listNodeAssets(nodeId: number, params?: ListNodeAssetsParams) {
    return apiClient.get<NodeAssetListResponse>(
      `${SERVICE_TREE_BASE}/nodes/${nodeId}/assets`,
      { params },
    );
  },

  /** 查询节点资产统计 */
  getNodeAssetStats(nodeId: number, params?: NodeAssetStatsParams) {
    return apiClient.get<AssetStatsVO>(
      `${SERVICE_TREE_BASE}/nodes/${nodeId}/assets/stats`,
      { params },
    );
  },

  /** 查询资产所属节点 */
  getAssetNode(assetId: number) {
    return apiClient.get<AssetNodeVO>(
      `${SERVICE_TREE_BASE}/assets/${assetId}/node`,
    );
  },
};
```

## 页面集成建议

### 1. 节点详情页 - 资产 Tab

在服务树节点详情页新增「关联资产」Tab，展示该节点绑定的所有云资产。

```
路由: /service-tree/nodes/:id → 资产 Tab
```

**组件结构**

```
<NodeAssetPanel>
  ├── <AssetStatsCards />        ← 调用 stats 接口，展示统计卡片
  ├── <AssetTypeFilter />        ← asset_type 下拉筛选
  ├── <EnvFilter />              ← env_id 环境筛选
  ├── <IncludeChildrenSwitch />  ← include_children 开关
  └── <AssetTable />             ← 调用 list 接口，分页表格
```

**表格列建议**

| 列名     | 字段       | 说明                              |
| -------- | ---------- | --------------------------------- |
| 资产名称 | asset_name | 可点击跳转到资产详情              |
| 资产ID   | asset_id   | 云厂商实例ID                      |
| 类型     | asset_type | 用 Tag 展示，如 ECS / RDS / Redis |
| 云厂商   | provider   | 用图标展示                        |
| 地域     | region     |                                   |
| 状态     | status     | 用 Badge 展示 running/stopped     |
| 环境     | env_id     | 关联环境名称                      |
| 绑定方式 | bind_type  | manual=手动 / rule=规则自动       |

### 2. 资产详情页 - 业务归属

在资产详情页展示该资产所属的服务树节点，调用 `GET /assets/:id/node`。

```
<AssetDetailPage>
  └── <BelongsToNode />  ← 展示节点名称 + 路径面包屑，可点击跳转
```

如果返回 404，显示「未关联业务」并提供绑定入口。

### 3. 统计卡片

调用 stats 接口，用卡片或饼图展示资产分布：

```
┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│ 总资产 42 │ │ ECS  15  │ │ RDS   8  │ │ Redis 5  │
└──────────┘ └──────────┘ └──────────┘ └──────────┘
```

`by_provider` 可用饼图展示云厂商分布。

## asset_type 枚举值

| 值             | 说明         |
| -------------- | ------------ |
| ecs            | 云虚拟机     |
| rds            | 关系型数据库 |
| redis          | Redis 缓存   |
| mongodb        | MongoDB      |
| vpc            | 虚拟私有云   |
| eip            | 弹性公网IP   |
| nas            | 文件存储     |
| oss            | 对象存储     |
| kafka          | 消息队列     |
| elasticsearch  | 搜索服务     |
| security_group | 安全组       |
| image          | 镜像         |
| disk           | 云盘         |
| snapshot       | 快照         |

## provider 枚举值

| 值      | 说明     |
| ------- | -------- |
| aliyun  | 阿里云   |
| aws     | AWS      |
| huawei  | 华为云   |
| tencent | 腾讯云   |
| volcano | 火山引擎 |

## 已有 API（无需改动）

以下是服务树模块已有的绑定管理 API，前端如果已经对接过则无需修改：

| 方法   | 路由                      | 说明             |
| ------ | ------------------------- | ---------------- |
| POST   | /nodes/:id/bindings       | 绑定资源         |
| POST   | /nodes/:id/bindings/batch | 批量绑定         |
| GET    | /nodes/:id/bindings       | 查询绑定列表     |
| DELETE | /bindings/:id             | 解绑             |
| GET    | /resources/:type/:id/node | 查询资源所属节点 |

新增的 3 个 API 是在绑定基础上增加了 CMDB 资产详情的聚合查询能力，适合在 UI 上直接展示资产信息而不需要二次请求。
