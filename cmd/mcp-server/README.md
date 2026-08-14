# e-cam MCP Server

基于 cloudx 统一适配器层的多云资产管理 MCP Server。

## 架构

```
AI Agent (Claude/Kiro/etc.)
    │ MCP Protocol (stdio)
    ▼
MCP Server
    ├── 查询类 Tools → MongoDB (CMDB 已同步数据)
    ├── 操作类 Tools → cloudx → 云厂商 API
    └── 管理类 Tools → MongoDB (云账号管理)
```

## 可用 Tools

### 云账号管理

| Tool                      | 说明                                  |
| ------------------------- | ------------------------------------- |
| `list_accounts`           | 列出云账号列表，支持按云厂商/状态过滤 |
| `get_account`             | 获取云账号详情（凭证已脱敏）          |
| `test_account_connection` | 测试云账号连接有效性                  |

### 资产查询（走 CMDB，毫秒级）

| Tool                   | 说明                                   |
| ---------------------- | -------------------------------------- |
| `list_instances`       | 按类型列出资产（ECS/RDS/Redis/VPC 等） |
| `get_instance`         | 获取资产实例详情                       |
| `search_assets`        | 跨类型关键词搜索                       |
| `get_asset_statistics` | 资产统计概览                           |

### 同步与实时查询（走云厂商 API）

| Tool                | 说明              |
| ------------------- | ----------------- |
| `sync_assets`       | 触发资产同步任务  |
| `list_regions`      | 实时获取可用地域  |
| `realtime_list_ecs` | 实时查询 ECS 实例 |

## 构建

```bash
go build -o mcp-server ./cmd/mcp-server/
```

## 运行

```bash
# 使用默认配置 (config/prod.yaml)
./mcp-server

# 指定配置文件
./mcp-server config/test.yaml
```

## MCP Client 配置

### Kiro / Claude Desktop

在 `mcp.json` 中添加：

```json
{
  "mcpServers": {
    "e-cam": {
      "command": "D:/Haven/e-cam-service/mcp-server.exe",
      "args": ["config/prod.yaml"],
      "disabled": false
    }
  }
}
```

## 依赖

- MongoDB：复用主服务的同一个数据库
- 配置文件：复用主服务的 `config/*.yaml`
- 无需额外基础设施
