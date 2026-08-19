# schema.sql 落库验证记录（1.3）

> 任务：1.3 schema 落库验证（mongosh 执行 createCollection/$jsonSchema/createIndex 于开发库）。
> 对象：`design/schema.sql` 全文（12 个 createCollection + 19 条 createIndex）。
> 本文面向后续运维：上线执行时按「执行步骤」与「重放行为」章节操作，验证方法可复跑。

## 1. 执行环境与连接目标确认（硬规则）

| 项 | 值 |
|----|----|
| 执行日期 | 2026-08-18 |
| MongoDB | 7.0.14（Community，windows-x86_64，与 `deploy/docker-compose.yml` 的 `mongo:7.0` 大版本一致） |
| 连接目标 | `mongodb://127.0.0.1:27018/ecam` — **本机临时开发实例**（验证前确认：绑定 127.0.0.1、全新空实例、无业务数据） |
| mongosh | 2.10.0（win32-x64） |
| 禁连确认 | 生产库 `106.52.187.69:27017`（config/prod.yaml）**未连接、未执行任何命令**；测试环境 `118.145.73.93:27017` 网络不可达，亦未使用 |
| 实例生命周期 | 验证专用一次性实例，验证完成后实例与数据目录销毁；生产/共享环境零接触 |

> 说明：docker 与本地常驻 MongoDB 均不可用（docker 命令缺失、127.0.0.1:27017 无监听、测试环境超时），���以官方 zip 包临时拉起本机 mongod 作为开发库完成验证，等价于 compose 定义的本地开发实例。

## 2. 执行命令（可复跑）

```bash
# 全文执行 schema.sql（mongosh --file 逐段执行：12 createCollection → 19 createIndex）
mongosh "mongodb://127.0.0.1:27018/ecam" --quiet --file docs/features/ssl-cert-management/design/schema.sql
# 结果：exit 0，无报错 —— 全部 DDL 成功

# 全量核验脚本（一次性 verify.js：A 集合/校验器 → B 索引定义 → C 越界拒绝 → D 正控 → E 唯一 → F 互斥 → G 心跳部分索引；
# 全部断言与输出即第 3–7 节各表）
mongosh "mongodb://127.0.0.1:27018/ecam" --quiet --file verify.js
# 结果：PASS=77 FAIL=0

# 1.2 集成测试（同一实例上跑 Go 侧 EnsureIndexes 全套断言）
CERT_TEST_MONGODB_DSN="mongodb://127.0.0.1:27018" go test ./internal/cert/repository/ -count=1 -v
# 结果：全部 PASS（含 TestEnsureIndexes_* / TestValidators_* / TestChangeOrderMutex_*）
```

## 3. 集合清单（12/12 建立成功）

| # | 集合 | $jsonSchema 校验器 | validationAction |
|---|------|--------------------|------------------|
| 1 | cert_certificates | 已注册 | error（默认，拒绝写入） |
| 2 | cert_references | 已注册 | error |
| 3 | cert_scan_snapshots | 已注册 | error |
| 4 | cert_change_orders | 已注册 | error |
| 5 | cert_change_items | 已注册（含 allOf/anyOf 分支） | error |
| 6 | cert_cloud_cert_mappings | 已注册 | error |
| 7 | cert_probe_results | 已注册 | error |
| 8 | cert_exemptions | 已注册 | error |
| 9 | cert_alert_config | 已注册 | error |
| 10 | cert_k8s_credentials | 已注册 | error |
| 11 | cert_batch_sessions | 已注册 | error |
| 12 | cert_crd_registrations | 已注册 | error |

## 4. 索引清单（19/19 创建成功，定义与 schema.sql 逐项一致）

| 集合 | 索引名 | 键 | 类型/选项 | 核验值 |
|------|--------|-----|-----------|--------|
| cert_certificates | uk_fingerprint | {fingerprint:1} | 唯一 | unique=true |
| cert_certificates | idx_hosting_status | {hostingStatus:1} | 常规 | — |
| cert_certificates | idx_not_after | {notAfter:1} | 常规 | — |
| cert_references | idx_fp_cloud_product | {certFingerprint:1,cloud:1,product:1} | 常规 | — |
| cert_references | idx_snapshot | {snapshotId:1} | 常规 | — |
| cert_scan_snapshots | idx_started_at_desc | {startedAt:-1} | 常规（降序） | — |
| cert_change_orders | uk_active_mutex | {activeMutex:1} | **部分唯一** | unique=true；partialFilterExpression=`{activeMutex:{$type:"string"}}` |
| cert_change_orders | idx_old_fp_status | {oldCertFingerprint:1,status:1} | 常规 | — |
| cert_change_items | idx_order | {orderId:1} | 常规 | — |
| cert_change_items | idx_order_status | {orderId:1,status:1} | 常规 | — |
| cert_change_items | idx_order_batch | {orderId:1,batchNo:1} | 常规 | — |
| cert_change_items | idx_status_heartbeat | {status:1,heartbeatAt:1} | **部分** | partialFilterExpression=`{status:"running"}`；explain 实测 IXSCAN 命中 |
| cert_cloud_cert_mappings | uk_fp_cloud_account | {certFingerprint:1,cloud:1,accountKey:1} | 唯一（复合） | unique=true |
| cert_probe_results | idx_domain_probe_desc | {domain:1,probeAt:-1} | 常规 | — |
| cert_probe_results | ttl_probe_90d | {probeAt:1} | **TTL** | expireAfterSeconds=7776000（90d） |
| cert_exemptions | uk_domain | {domain:1} | 唯一 | unique=true |
| cert_k8s_credentials | uk_cluster_name | {clusterName:1} | 唯一 | unique=true |
| cert_batch_sessions | ttl_batch_session_30d | {createdAt:1} | **TTL** | expireAfterSeconds=2592000（30d） |
| cert_crd_registrations | uk_cluster_group_kind | {clusterId:1,apiGroup:1,kind:1} | 唯一（复合） | unique=true |
| cert_alert_config | （无二级索引） | — | 单文档集合 | 仅默认 `_id_` |

TTL 实删验证：向 cert_probe_results 插入 probeAt=91 天前、向 cert_batch_sessions 插入 createdAt=31 天前的文档各一条，15 秒内均被 TTL 后台线程删除（count 归零）——两个 TTL 索引不仅定义正确且实际生效。

## 5. 越界文档拒绝样例（$jsonSchema，均为 code 121 Document failed validation）

| # | 集合 | 样例偏差 | 结果 |
|---|------|----------|------|
| C1 | cert_certificates | hostingStatus="bogus"（非枚举） | 拒绝 |
| C2 | cert_certificates | fingerprint="not-hex"（非 64 位 hex） | 拒绝 |
| C3 | cert_certificates | fingerprint 大写 hex（pattern ^[0-9a-f]{64}$） | 拒绝 |
| C4 | cert_certificates | 缺必填 createdAt | 拒绝 |
| C5 | cert_certificates | encryptedPrivateKey.algo="AES-128-CBC"（非枚举） | 拒绝 |
| C6 | cert_certificates | encryptedPrivateKey.keyVersion=0（minimum 1） | 拒绝 |
| C7 | cert_certificates | expiryAlertLevel="L21"（非枚举） | 拒绝 |
| C8 | cert_references | cloud="gcp"（非枚举） | 拒绝 |
| C9 | cert_references | product="ecs"（非枚举） | 拒绝 |
| C10 | cert_change_orders | status="waiting"（非 9 态枚举） | 拒绝 |
| C11 | cert_change_orders | verifyExpected.newCertFingerprint="short"（非 hex） | 拒绝 |
| C12 | cert_change_orders | batchInfo.totalBatches=0（minimum 1） | 拒绝 |
| C13 | cert_change_items | action="delete"（非枚举） | 拒绝 |
| C14 | cert_change_items | upload_and_bind 缺分支必填 accountKey（anyOf） | 拒绝 |
| C15 | cert_change_items | patch_crd 分支 channel="cloud_api"（anyOf） | 拒绝 |
| C16 | cert_change_items | status="bogus"（非枚举） | 拒绝 |
| C17 | cert_cloud_cert_mappings | certFingerprint="zz"（非 hex） | 拒绝 |
| C18 | cert_probe_results | status="bogus"（非枚举） | 拒绝 |
| C19 | cert_alert_config | thresholds 缺必填键 | 拒绝 |
| C20 | cert_alert_config | scanFreshnessHours=100（maximum 72） | 拒绝 |
| C21 | cert_alert_config | expiryLevels=[30,30]（uniqueItems） | 拒绝 |
| C22 | cert_batch_sessions | progress.total=0（minimum 1） | 拒绝 |
| C23 | cert_scan_snapshots | coverageMeta[].total=-5（minimum -1） | 拒绝 |
| C24 | cert_k8s_credentials | apiEndpoint=12345（bsonType string） | 拒绝 |

正控（合法文档被接受）：完整证书文档（含 encryptedPrivateKey 密文对象）、cloud_api / k8s_api 两分支 change item、界内全字段 thresholds 的 _id="global" 告警配置，均插入成功。

## 6. 唯一约束验证（code 11000 E11000 duplicate key）

| # | 索引 | 场景 | 结果 |
|---|------|------|------|
| E1 | uk_fingerprint | 同指纹第二条 | 拒绝（dup key: fingerprint） |
| E5 | uk_fp_cloud_account | 同 (fp,cloud,accountKey)、不同 cloudCertId | 拒绝（维度正确：仅三元组参与判重） |
| E6 | uk_fp_cloud_account | 同指纹不同 cloud | 允许（复合键维度隔离正确） |
| E7 | uk_domain | 同域名第二条 | 拒绝 |
| E8 | uk_cluster_name | 同集群名第二条 | 拒绝 |
| E9 | uk_cluster_group_kind | 同 (clusterId,apiGroup,kind) 第二条 | 拒绝 |
| E10 | uk_cluster_group_kind | 同集群不同 kind | 允许 |

## 7. 在途互斥（uk_active_mutex 部分唯一索引）验证

| # | 场景 | 结果 |
|---|------|------|
| F1 | 首张活跃单（activeMutex=oldCertFingerprint）插入 | 成功 |
| F2 | 同 activeMutex 第二张活跃单 | **拒绝 E11000**（应用层映射 CHANGE_IN_FLIGHT，关闭 check-then-insert 竞态） |
| F3 | 无 activeMutex 的终态单 | 允许（不参与部分唯一约束） |
| F4 | 不同 token 的活跃单 | 允许（按指纹隔离） |
| F5 | 终态原子迁移：status 置换 + `$unset` activeMutex 同一 update | modifiedCount=1 |
| F6 | token 释放后同 activeMutex 新单再插 | 成功（互斥活性保障成立） |

并发竞态由 1.2 的 `TestChangeOrderMutex_ConcurrentDoubleInsert` 在同一实例复核通过。

## 8. 与 1.2 EnsureIndexes 一致性比对

**方法（三重证据）**：

1. **静态比对**：`internal/cert/repository/validators.go`（collectionValidators）与 `indexes.go`（collectionIndexes）逐条对照 schema.sql；
2. **运行时等价**：`CERT_TEST_MONGODB_DSN=mongodb://127.0.0.1:27018 go test ./internal/cert/repository/ -count=1` 全部 PASS（EnsureIndexes 在同一服务器版本上建立同样的 12 集合 + 索引名 + 唯一/部分/TTL 选项，校验器拒绝行为一致）；
3. **存储层差分**：Go `EnsureIndexes` 另建 `ecam_ensureindexes_diff` 库，与 mongosh 落库的 `ecam` 库做 listCollections（validator）+ listIndexes 深度语义比对（忽略键序与 uuid）。

**结论：索引完全一致（19/19，键/唯一/部分过滤/TTL 值逐项 SAME）；校验器 11/12 语义一致，1 处注释性偏差**：

| 偏差 | schema.sql | 1.2 validators.go | 影响 | 处置 |
|------|-----------|-------------------|------|------|
| D-1 | `cert_change_items.status` 带 `description:"DEFAULT=pending"`（L157） | `enumStr(...)` 仅 bsonType+enum，未转录 description | 无——MongoDB 校验器忽略 description 字段，两侧拒绝/接受行为实测完全一致；仅 1:1 转录纯度问题 | **交回 1.2**：validators.go 补转录该 description（或双方约定移除该注释性字段），本任务不改 schema.sql 字段定义 |

## 9. 重放行为（运维上线参考）

| 场景 | 行为（实测） |
|------|--------------|
| 空库全文执行 schema.sql | 全部成功（本次验证路径，exit 0） |
| 集合已存在且**定义完全一致**时重放 | createCollection/createIndex 均为 no-op 成功（幂等，实测 exit 0） |
| 集合已存在但校验器**定义不同**时重放 | 报错终止：`MongoServerError: namespace ... already exists, but with different options`（code 48） |
| 已存在集合的校验器演进 | 不要重放 createCollection；走 `collMod` 更新 validator —— 1.2 `EnsureIndexes` 的 ensureValidator 已实现该路径（createCollection 失败 code 48 → collMod 同步，CreateMany 对同名索引 no-op），服务启动时自动收敛 |

**上线步骤建议**：

1. 确认目标连接（禁止连生产库执行未经审批的 DDL）；
2. 新环境：`mongosh "<uri>/ecam" --file docs/features/ssl-cert-management/design/schema.sql`；
3. 已有环境（1.2 代码已部署）：无需手动执行，模块 init 的 EnsureIndexes 幂等收敛（空缺创建、缺失索引补建、校验器 collMod 同步）；
4. 验证：`db.getCollectionNames()` 应含 12 个 cert_* 前缀集合；逐集合 `db.<coll>.getIndexes()` 对照第 4 节清单。

## 10. 验收判定

| AC | 判定 | 证据 |
|----|------|------|
| 12 个 createCollection 成功、$jsonSchema 生效 | PASS | 第 3 节；24 例越界拒绝���第 5 节） |
| 全部 createIndex 成功且定义正确（唯一/部分唯一/TTL） | PASS | 第 4 节 19/19 逐项核验 + TTL 实删 |
| 越界文档被拒绝并记录 | PASS | 第 5 节 C1–C24（code 121） |
| 部分唯一互斥验证（dup → $unset → 可再插） | PASS | 第 7 节 F1–F6 |
| 验证记录产出含一致性比对结论 | PASS | 本文 + 第 8 节（1 处偏差已记录并交回 1.2） |

**总评**：schema.sql 可在开发库（MongoDB 7.0）全文执行且行为符合设计；与 1.2 EnsureIndexes 除 1 处注释性偏差（D-1，无语义影响）外完全一致。
