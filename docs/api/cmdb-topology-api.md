# CMDB 拓扑视图 API 文档

## 概述

CMDB 拓扑 API 提供基于模型关系的图数据查询能力，支持前端渲染资产关系拓扑图。

数据结构采用标准的 `nodes + edges` 图模型，可直接对接 AntV G6、D3.js、ECharts 关系图等可视化库。

## 基础路径

```
/api/v1/cmdb/topology
```

所有请求需携带 `X-Tenant-ID` Header。

---

## 接口列表

| 方法 | 路由            | 说明         | 适用场景                           |
| ---- | --------------- | ------------ | ---------------------------------- |
| GET  | `/model`        | 模型拓扑图   | 全局架构图，展示模型间关系定义     |
| GET  | `/instance/:id` | 实例拓扑图   | 资产详情页，展示某个资产的关联关系 |
| GET  | `/related/:id`  | 关联实例列表 | 表格展示某个资产的关联资产         |

---

## 1. 模型拓扑图

展示模型之间的关系定义，如"ECS 属于 VPC"、"EIP 绑定 ECS"等全局架构关系。

### 请求

```
GET /api/v1/cmdb/topology/model?provider=aliyun
```

| 参数     | 类型   | 必填 | 说明                                                         |
| -------- | ------ | ---- | ------------------------------------------------------------ |
| provider | string | 否   | 云厂商过滤 (aliyun/aws/huawei/tencent/volcano)，不传返回全部 |

### 响应

```json
{
  "code": 0,
  "data": {
    "nodes": [
      {
        "uid": "cloud_vm",
        "name": "云虚拟机",
        "category": "compute",
        "provider": "all",
        "icon": "server"
      },
      {
        "uid": "cloud_vpc",
        "name": "VPC",
        "category": "network",
        "provider": "all",
        "icon": "network"
      },
      {
        "uid": "cloud_eip",
        "name": "弹性公网IP",
        "category": "network",
        "provider": "all",
        "icon": "eip"
      }
    ],
    "edges": [
      {
        "source_model_uid": "cloud_vm",
        "target_model_uid": "cloud_vpc",
        "relation_uid": "ecs_bindto_vpc",
        "relation_name": "ECS属于VPC",
        "relation_type": "belongs_to"
      },
      {
        "source_model_uid": "cloud_vm",
        "target_model_uid": "cloud_eip",
        "relation_uid": "ecs_bindto_eip",
        "relation_name": "ECS绑定EIP",
        "relation_type": "bindto"
      }
    ]
  }
}
```

### 字段说明

**ModelTopologyNode（模型节点）**

| 字段     | 类型   | 说明                                                            |
| -------- | ------ | --------------------------------------------------------------- |
| uid      | string | 模型唯一标识，如 `cloud_vm`、`cloud_vpc`                        |
| name     | string | 模型显示名称                                                    |
| category | string | 模型分类：`compute`/`network`/`database`/`storage`/`middleware` |
| provider | string | 云厂商标识，`all` 表示通用模型                                  |
| icon     | string | 图标标识，前端根据此字段渲染对应图标                            |

**ModelTopologyEdge（模型关系边）**

| 字段             | 类型   | 说明                                                    |
| ---------------- | ------ | ------------------------------------------------------- |
| source_model_uid | string | 源模型 UID                                              |
| target_model_uid | string | 目标模型 UID                                            |
| relation_uid     | string | 关系类型唯一标识                                        |
| relation_name    | string | 关系显示名称，可作为连线标签                            |
| relation_type    | string | 关系类型：`belongs_to`/`bindto`/`contains`/`depends_on` |

### 前端渲染建议

- 用 `category` 字段对节点分组或着色（如计算资源蓝色、网络资源绿色）
- 用 `icon` 字段渲染节点图标
- 用 `relation_name` 作为连线上的文字标签
- 用 `relation_type` 区分连线样式（如 `belongs_to` 用虚线，`bindto` 用实线）
- 点击模型节点可跳转到该类型的资产列表页

---

## 2. 实例拓扑图

以某个资产实例为中心，展示其关联的所有资产，支持多层展开。

### 请求

```
GET /api/v1/cmdb/topology/instance/123?depth=2&direction=both
```

| 参数      | 类型   | 必填 | 说明                                              |
| --------- | ------ | ---- | ------------------------------------------------- |
| id        | int    | 是   | 实例 ID（路径参数）                               |
| depth     | int    | 否   | 展开深度，默认 `1`，设为 `2` 可看到二级关联       |
| direction | string | 否   | 查询方向：`both`（默认）/ `outgoing` / `incoming` |
| model_uid | string | 否   | 按模型类型过滤关联节点，如只看 VPC：`cloud_vpc`   |
| tenant_id | string | 否   | 租户 ID                                           |

### 响应

```json
{
  "code": 0,
  "data": {
    "nodes": [
      {
        "id": 123,
        "model_uid": "cloud_vm",
        "model_name": "云虚拟机",
        "asset_id": "i-bp1234567890",
        "asset_name": "web-server-01",
        "icon": "server",
        "category": "compute",
        "attributes": {
          "cpu": 4,
          "memory": 8192,
          "private_ip": "10.0.1.100",
          "status": "running"
        }
      },
      {
        "id": 456,
        "model_uid": "cloud_vpc",
        "model_name": "VPC",
        "asset_id": "vpc-bp1234567890",
        "asset_name": "prod-vpc",
        "icon": "network",
        "category": "network",
        "attributes": {
          "cidr_block": "10.0.0.0/16"
        }
      },
      {
        "id": 789,
        "model_uid": "cloud_eip",
        "model_name": "弹性公网IP",
        "asset_id": "eip-bp1234567890",
        "asset_name": "web-eip",
        "icon": "eip",
        "category": "network",
        "attributes": {
          "ip_address": "47.100.1.1",
          "bandwidth": 100
        }
      }
    ],
    "edges": [
      {
        "source_id": 123,
        "target_id": 456,
        "relation_type_uid": "ecs_bindto_vpc",
        "relation_name": "ECS属于VPC",
        "relation_type": "belongs_to"
      },
      {
        "source_id": 123,
        "target_id": 789,
        "relation_type_uid": "ecs_bindto_eip",
        "relation_name": "ECS绑定EIP",
        "relation_type": "bindto"
      }
    ]
  }
}
```

### 字段说明

**TopologyNode（实例节点）**

| 字段       | 类型   | 说明                                           |
| ---------- | ------ | ---------------------------------------------- |
| id         | int64  | 实例 ID，作为节点唯一标识                      |
| model_uid  | string | 所属模型 UID                                   |
| model_name | string | 模型显示名称                                   |
| asset_id   | string | 云厂商资产 ID（如 `i-xxx`、`vpc-xxx`）         |
| asset_name | string | 资产名称                                       |
| icon       | string | 图标标识                                       |
| category   | string | 模型分类                                       |
| attributes | object | 资产属性（可选），包含 IP、CPU、状态等详细信息 |

**TopologyEdge（实例关系边）**

| 字段              | 类型   | 说明                              |
| ----------------- | ------ | --------------------------------- |
| source_id         | int64  | 源实例 ID，对应 nodes 中的 `id`   |
| target_id         | int64  | 目标实例 ID，对应 nodes 中的 `id` |
| relation_type_uid | string | 关系类型 UID                      |
| relation_name     | string | 关系显示名称                      |
| relation_type     | string | 关系类型                          |

### 前端渲染建议

- 起始节点（第一个 node）居中高亮显示
- 用 `category` 区分节点颜色
- 节点上显示 `asset_name`，hover 时展示 `attributes` 详情
- `depth=1` 适合资产详情页侧边栏，`depth=2` 适合全屏拓扑视图
- 点击节点可跳转到对应资产详情页

---

## 3. 关联实例列表

获取某个实例通过指定关系类型关联的所有实例，适合用表格展示。

### 请求

```
GET /api/v1/cmdb/topology/related/123?relation_type_uid=ecs_bindto_vpc
```

| 参数              | 类型   | 必填 | 说明                               |
| ----------------- | ------ | ---- | ---------------------------------- |
| id                | int    | 是   | 实例 ID（路径参数）                |
| relation_type_uid | string | 否   | 关系类型 UID，不传返回所有关联实例 |

### 响应

```json
{
  "code": 0,
  "data": {
    "instances": [
      {
        "id": 456,
        "model_uid": "cloud_vpc",
        "asset_id": "vpc-bp1234567890",
        "asset_name": "prod-vpc",
        "tenant_id": "tenant-001",
        "account_id": 1,
        "attributes": {
          "cidr_block": "10.0.0.0/16",
          "status": "Available"
        },
        "create_time": 1706000000000,
        "update_time": 1706000000000
      }
    ],
    "total": 1
  }
}
```

---

## 已定义的关系类型

| relation_uid   | 源模型    | 目标模型  | 关系类型   | 说明         |
| -------------- | --------- | --------- | ---------- | ------------ |
| ecs_bindto_vpc | cloud_vm  | cloud_vpc | belongs_to | ECS 属于 VPC |
| ecs_bindto_eip | cloud_vm  | cloud_eip | bindto     | ECS 绑定 EIP |
| rds_bindto_vpc | cloud_rds | cloud_vpc | belongs_to | RDS 属于 VPC |
| eip_bindto_vpc | cloud_eip | cloud_vpc | belongs_to | EIP 属于 VPC |

> 关系类型可通过 `GET /api/v1/cmdb/model-relations` 接口查询完整列表。

---

## 前端页面建议

### 页面一：全局模型拓扑

- 调用接口 1（模型拓扑）
- 展示所有模型之间的关系架构图
- 可按 `provider` 筛选不同云厂商的模型
- 点击模型节点 → 跳转到该类型资产列表

### 页面二：资产详情 - 关联拓扑 Tab

- 调用接口 2（实例拓扑），`depth=2`
- 以当前资产为中心展示关联图
- 节点显示资产名称和图标
- 点击关联节点 → 跳转到对应资产详情

### 页面三：资产详情 - 关联资产 Tab

- 调用接口 3（关联实例列表）
- 按关系类型分 Tab 展示（如"所属VPC"、"绑定EIP"）
- 用表格展示关联资产的详细信息

---

## 可视化库对接示例

### AntV G6

```javascript
// 接口 2 返回的数据可直接转换为 G6 格式
const response = await fetch("/api/v1/cmdb/topology/instance/123?depth=2");
const { data } = await response.json();

const graphData = {
  nodes: data.nodes.map((node) => ({
    id: String(node.id),
    label: node.asset_name,
    type: node.category === "compute" ? "rect" : "circle",
    style: { fill: getCategoryColor(node.category) },
    icon: { show: true, img: getIconUrl(node.icon) },
    // 原始数据挂在 data 上，hover 时展示
    data: node,
  })),
  edges: data.edges.map((edge) => ({
    source: String(edge.source_id),
    target: String(edge.target_id),
    label: edge.relation_name,
    style: {
      lineDash: edge.relation_type === "belongs_to" ? [4, 4] : [],
    },
  })),
};

graph.data(graphData);
graph.render();
```

### ECharts 关系图

```javascript
const response = await fetch("/api/v1/cmdb/topology/instance/123?depth=2");
const { data } = await response.json();

const option = {
  series: [
    {
      type: "graph",
      layout: "force",
      data: data.nodes.map((node) => ({
        name: node.asset_name,
        symbolSize: node.id === 123 ? 60 : 40, // 起始节点大一些
        category: node.category,
        value: node,
      })),
      links: data.edges.map((edge) => ({
        source: data.nodes.findIndex((n) => n.id === edge.source_id),
        target: data.nodes.findIndex((n) => n.id === edge.target_id),
        label: { show: true, formatter: edge.relation_name },
      })),
      categories: [
        { name: "compute" },
        { name: "network" },
        { name: "database" },
        { name: "storage" },
      ],
    },
  ],
};
```
