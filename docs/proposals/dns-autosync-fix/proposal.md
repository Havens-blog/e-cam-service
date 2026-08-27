---
created: "2026-08-27"
author: "Haven"
status: Draft
intent: "fix"
---

# Proposal: 修复多云 DNS 解析记录不自动同步的问题

## Problem

启用自动同步的云账号，若未显式配置 `SupportedAssetTypes`，其 DNS 域名与解析记录**永远不会被自动同步**——`AutoSyncScheduler` 构建任务时的默认资产类型清单遗漏了 `"dns"`，导致 DNS 数据只在手动触发同步后才更新。

### Evidence

- [auto_sync.go:223-229](../../../e-cam-service/internal/cam/scheduler/auto_sync.go) 默认清单为 `ecs/disk/snapshot/security_group/image/rds/redis/mongodb/vpc/vswitch/eip/lb/cdn/waf/nas/oss/kafka/elasticsearch`，**不含 `dns`**（也不含 `eni`）。
- [sync_assets.go:207-225](../../../e-cam-service/internal/cam/task/executor/sync_assets.go) 只有当 `expandAssetTypes(params.AssetTypes)` 结果包含 `"dns"` 时才调用 `syncDNS`；调度器提交的任务参数不含 `dns`，因此该分支永不命中。
- `expandAssetTypes` 的 default 分支原样透传未知类型（[sync_assets.go:314](../../../e-cam-service/internal/cam/task/executor/sync_assets.go)），证明把 `dns` 加入默认清单即可生效，无需改动执行器。
- 用户观察：「DNS 数据有滞后」——实际不是滞后，是从不自动更新，只有手动同步（显式带 dns/network 资产类型）才更新。
- 静默性：跳过 DNS 同步没有任何警告日志，问题难以从日志侧察觉。

### Urgency

DNS 解析记录是证书引用扫描（cert 域 reference 扫描）与资产管理视图的数据基础。长期不更新意味着平台展示与云上实际状态脱节，用户对平台数据失去信任。且该缺陷是静默跳过，随账号数量增长排查成本递增。

## Proposed Solution

1. 在共享位置定义**唯一的默认同步资产类型常量**（在现有清单基础上补充 `dns`），`AutoSyncScheduler.triggerSync` 使用该常量替换硬编码清单。
2. 将散落在 `auto_sync.go` / `sync_assets.go` / `asset_sync.go` 的同类硬编码资产清单（默认清单与 network 子类型清单）收敛到同一处定义，防止未来再漏。
3. 补充单元测试锁定语义：默认清单含 `dns`、`expandAssetTypes` 透传 `dns`、用户显式配置的 `SupportedAssetTypes` 语义不变。

### Innovation Highlights

无创新——这是对既有多资产类型清单维护模式的常规缺陷修复（单一权威来源 + 行为等价替换）。不引入新机制。

## Requirements Analysis

### Key Scenarios

- **主路径**：账号启用自动同步、`SupportedAssetTypes` 为空 → 调度器按默认常量（含 `dns`）提交任务 → 执行器命中 `dns` 分支 → `c_dns_domain` / `c_dns_record` 按账号同步周期自动更新。
- **显式配置**：账号显式配置了 `SupportedAssetTypes` → 完全按配置执行；配置不含 `dns` 则不自动同步 DNS（尊重用户意图，不强制）。
- **回归防护**：默认常量今后增删类型只需改一处，调度器与执行器自动一致。

### Non-Functional Requirements

- **兼容性**：除新增 `dns` 外，其余资产类型行为等价替换，不改变现有同步结果。
- **API 配额**：每次自动同步新增按域名一次 `ListRecords` 调用；DNS 记录量通常小，限速风险低，可观察后按需调整账号 `SyncInterval`。

### Constraints & Dependencies

- 依赖现有 `taskx` 任务队列与 `cloudx.DNSAdapter`，均已就绪，无外部新增依赖。
- 常量放置位置需避免 import cycle（scheduler ↔ executor ↔ service）。

## Alternatives & Industry Benchmarking

### Industry Solutions

多云管理平台（如 CloudQuery、Steampipe 类资产盘点）通行做法是「资产类型清单单一来源 + 显式 opt-out」，而非各调用点各自硬编码默认清单。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing | - | 零改动 | DNS 数据持续静默过期，下游失真 | Rejected: 平台数据可信度受损 |
| 仅最小修复（只改 auto_sync.go） | - | 改动最小 | 硬编码清单仍散落三处，同类遗漏必然重现 | Rejected: 用户已选择收敛 |
| 解耦 DNS 与资产过滤（适配器支持即同步） | - | 鲁棒，永不遗漏 | 覆盖用户显式排除 `dns` 的配置意图 | Rejected: 违反配置语义 |
| **最小修复 + 常量收敛** | 本提案 | 治根且防复发，改动可控 | 涉及多文件等价替换 | **Selected: 用户确认** |

## Feasibility Assessment

### Technical Feasibility

已验证修复路径完整成立：`expandAssetTypes` 透传 `dns` → `syncDNS` 分支命中 → upsert + 清理逻辑现成。纯 Go 代码改动，无技术阻碍。

### Resource & Timeline

单次 /quick 管线可完成：常量定义 + 三个文件替换 + 单元测试。

### Dependency Readiness

无外部依赖。云厂商 DNS API 调用路径（`dnsAdapter.ListDomains/ListRecords`）已在手动同步中验证可用。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| 「数据滞后」是同步间隔太长导致 | XY Detection | Overturned：根因是 `dns` 根本不在自动同步默认清单，与间隔无关（用户已确认现象为「从不自动更新」） |
| 「自动同步覆盖所有资产类型」 | 5 Whys（为何滞后→默认清单遗漏→为何遗漏→清单在多处独立硬编码） | Confirmed：清单散落是根因，收敛常量是防复发手段 |
| 「应该在执行器侧强制同步 DNS」 | Assumption Flip（若强制则显式排除配置失效） | Confirmed 维持现状：显式配置语义优先，默认值补齐即可 |

## Scope

### In Scope

- 新增共享默认同步资产类型常量（现清单 + `dns`），并收敛 `auto_sync.go` 调度器默认清单。
- 收敛 `sync_assets.go` / `asset_sync.go` 中语义相同的硬编码清单（默认清单、network 子类型清单）到共享定义，行为等价替换。
- 单元测试：默认清单含 `dns`；`expandAssetTypes` 对 `dns` 的透传；显式 `SupportedAssetTypes` 不被强制覆盖。

### Out of Scope

- 实时 / 事件驱动 / 增量 DNS 同步（云事件订阅、变更检测）。
- 强制同步 DNS（覆盖用户显式排除配置）。
- `eni` 等其他未入默认清单类型的行为变更。
- 前端同步状态展示与同步间隔调参。

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| 默认清单新增 `dns` 增加云 API 调用量，多域名账号可能触碰限速 | M | L | DNS 记录量通常小；上线后观察任务耗时与错误日志，必要时按账号调大 `SyncInterval` |
| 常量收敛涉及多文件替换，可能引入回归 | M | M | 行为等价替换（除 `dns` 外不改其他类型）；单元测试锁定清单内容 |
| 显式配置不含 `dns` 的存量账号依旧不同步，仍可能被感知为「滞后」 | M | L | 属预期语义，交付说明中明确；如需强制另行提案 |

## Success Criteria

- [ ] `SupportedAssetTypes` 为空且启用自动同步的账号，自动同步任务参数包含 `dns`（任务 params 或调度日志可验证）。
- [ ] 自动同步执行后，`c_dns_domain` / `c_dns_record` 的 `utime` 随同步周期更新；云上新增/删除的解析记录在下一个同步周期后在平台可查。
- [ ] 默认资产清单在仓库中只有一个权威定义，调度器与执行器引用同一常量（grep 验证无重复硬编码清单）。
- [ ] 显式配置 `SupportedAssetTypes`（不含 `dns`）的账号，自动同步任务参数不含 `dns`（配置语义不被覆盖）。
- [ ] 单元测试覆盖上述清单内容与透传行为，全部通过。

## Next Steps

- Proceed to `/quick-tasks` to generate task breakdown
