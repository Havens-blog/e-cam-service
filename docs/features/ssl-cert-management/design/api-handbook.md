---
created: "2026-08-14"
related: design/tech-design.md
---

# API Handbook: SSL 证书管理功能域

## API Overview

RESTful HTTP API（Gin），前缀 `/api/v1/certs`。鉴权接入 EIAM 三角色。所有响应统一信封 `{success, data, error, meta}`。私钥字段任何接口不返回。

## Endpoints

### 导入证书

**Method**: `POST`
**Path**: `/api/v1/certs`
**Auth**: 运维工程师

#### Request (multipart/form-data)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| certFile | file | yes | PEM 证书 |
| keyFile | file | no | 私钥（缺省走仅指纹登记） |
| expectedDomain | string | no | 预期域名（仅提示性比对） |

#### Response (200)

| Field | Type | Description |
|-------|------|-------------|
| certId | string | 证书 ID |
| fingerprint | string | 指纹 |
| hostingStatus | string | complete/fingerprint_only |

#### Error Responses

| Status | Code | Description |
|--------|------|-------------|
| 400 | CERT_KEY_MISMATCH | 证书与私钥不匹配 |
| 400 | CERT_CHAIN_INCOMPLETE | 证书链缺失 |
| 400 | CERT_PARSE_FAIL | SAN 结构无法解析/已过期 |
| 409 | CERT_DUPLICATE_FINGERPRINT | 重复指纹 |

---

### 批量导入

**Method**: `POST`  **Path**: `/api/v1/certs/batch`  **Auth**: 运维工程师
Request: multipart 多文件 + 逐文件可选私钥。
Response (202): `{batchId, status, files[]{fileName,result,errorReason,certId?}, progress{total,done,failed}}`——batchId 即 cert_batch_sessions._id（持久化导入会话句柄，轮询契约闭合）。
进度轮询 `GET /api/v1/certs/batch/:batchId` → 同构响应 `{batchId, status, files[], progress}`；status=completed（全部成功）/partial_failed（部分失败）为终态，会话 TTL 30 天清理。逐文件失败不阻塞其他文件，失败文件单独重试（重新 POST 单文件）。

---

### 证书列表 / 详情 / 删除 / 补传私钥

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/api/v1/certs` | 运维工程师/主管 | 列表（分页+筛选 hostingStatus/daysLeft+search） |
| GET | `/api/v1/certs/:id` | 运维工程师/主管 | 详情（不含明文私钥） |
| DELETE | `/api/v1/certs/:id` | 运维工程师 | 删除（活跃引用/保护期拦截 409 CERT_HAS_REFS） |
| POST | `/api/v1/certs/:id/key` | 运维工程师 | 补传私钥→升级完整托管 |

---

### 引用关系

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/api/v1/certs/:id/references` | 运维工程师 | 正向引用（分组+覆盖率元数据+盲区声明；返回 referenceStatus: has_refs/no_refs_scanned/blind_spot） |
| GET | `/api/v1/certs/reverse?domain=` | 运维工程师 | 反向查询（域名/资源→证书） |
| POST | `/api/v1/certs/:id/scan` | 运维工程师 | 立即扫描（防重 409 SCAN_IN_PROGRESS） |

---

### 台账统计（覆盖率双指标）

**Method**: `GET`  **Path**: `/api/v1/certs/stats`  **Auth**: 运维工程师/主管
Response: `{total, complete, fingerprintOnly, missingRegistrations, registrationRate, replaceableRate, fingerprintOnlyRate, denominator, denominatorSources{scannedUniqueFingerprints, manualOnlyFingerprints}}`
口径：分母 = 最新成功快照 CertReference 指纹去重 ∪ 台账全部指纹（PRD Goals"引用扫描在用证书去重集合 + 人工补充登记集合"）；registrationRate=登记覆盖率（台账/分母），replaceableRate=可更换托管覆盖率（complete/分母），fingerprintOnlyRate=仅指纹登记占比（台账内占比，单独可见）；missingRegistrations=扫描发现未登记的登记缺口数。

### 到期看板

**Method**: `GET`  **Path**: `/api/v1/certs/dashboard`  **Auth**: 全角色（含只读）
Response: `{summary{countsByLevel[5],diffAlertCount,exemptCount,wildcardSkippedCount,registrationRate,replaceableRate,fingerprintOnlyRate}, items[]{domain,daysLeft,level,hostingType,probeStatus,referencedClouds[]}, lastInspectionAt}`（三个 rate 字段口径同 GET /stats；wildcardSkippedCount=通配符 SAN 跳过拨测计数，探测覆盖显式缺口）

---

### 变更管理

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/api/v1/certs/changes` | 运维工程师/主管 | 变更单列表（状态 Tab 筛选） |
| POST | `/api/v1/certs/changes` | 运维工程师 | 生成变更清单（oldFingerprint+newCertId；前置校验：扫描新鲜度/SAN⊇/在途互斥/新证书 hostingStatus=complete，fingerprint_only → 409 NEW_CERT_FINGERPRINT_ONLY） |
| GET | `/api/v1/certs/changes/:id` | 运维工程师/主管 | 变更单/报告详情 |
| POST | `/api/v1/certs/changes/:id/confirm` | 运维工程师 | 确认执行（含分批配置；分批在此固化批次分配，单批 ≤ floor(total/2)，一律人工续批） |
| POST | `/api/v1/certs/changes/:id/execute` | 运维工程师 | 触发批量执行（执行当前批 batchNo=currentBatch 的项） |
| POST | `/api/v1/certs/changes/:id/confirm-batch` | 运维工程师 | 人工续批（门控：上一批全部 success 且批级验证达标——提频探测连续 verifyConfirmProbes 次一致；不满足 409 BATCH_NOT_CONFIRMABLE） |
| POST | `/api/v1/certs/changes/:id/cancel` | 运维工程师 | 取消（draft/pending_confirm/批间暂停整单取消，未执行项标 skipped；executing 中止=未开始项标 skipped、执行中项不中断等待完成后收敛 cancelled；其余状态 409 CHANGE_NOT_CANCELLABLE） |
| GET | `/api/v1/certs/changes/:id/progress` | 运维工程师 | 逐项进度轮询 |
| POST | `/api/v1/certs/changes/:id/rollback` | 运维工程师 | 回滚成功项（前置 GetCert 有效性校验：云侧旧证书存在/未过期/指纹一致；无效 409 ROLLBACK_TARGET_INVALID 转人工） |
| GET | `/api/v1/certs/changes/:id/audit` | 运维工程师/主管/审计 | 按变更单号查询审计流水 |

#### 变更审计（按单号）

`GET /api/v1/certs/changes/:id/audit` → `{logs[]{at, actor, action, detail, itemId?}}`。action 覆盖 create/confirm/execute/item_result/rollback/verify/orphan_cleanup，可与 ChangeReport 逐条比对一致（PRD Story 5 AC）。

#### 生成清单错误

| Status | Code | Description |
|--------|------|-------------|
| 409 | SCAN_STALE | 扫描超新鲜度阈值，阻断生成 |
| 409 | SAN_INSUFFICIENT | 新证书 SAN 不⊇目标域名 |
| 409 | CHANGE_IN_FLIGHT | 同一旧证书存在在途变更单 |
| 409 | NEW_CERT_FINGERPRINT_ONLY | 新证书仅指纹登记（无私钥），无法上传云证书库执行更换 |
| 409 | BATCH_NOT_CONFIRMABLE | 续批门控未满足（上一批存在失败项或批级验证未达标） |
| 409 | CHANGE_NOT_CANCELLABLE | 当前状态不可取消（draft/pending_confirm/批间暂停整单取消；executing 仅未开始项可中止、执行中项等待完成） |

回滚路径错误（rollback 端点）：409 ROLLBACK_TARGET_INVALID——回滚目标无效（云侧旧证书已删除/已过期/被替换），不自动回滚转人工决策（产生路径见 tech-design CloudDeployer.GetCert）。

---

### 全局配置

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/api/v1/certs/settings` | 运维主管/审计 | 告警配置+阈值+豁免清单 |
| PUT | `/api/v1/certs/settings` | 运维主管/审计 | 更新告警接收人/阈值/通配符探测替代清单 wildcardProbeOverrides（越界 400） |
| POST | `/api/v1/certs/settings/exemptions` | 运维主管/审计 | 添加豁免 |
| DELETE | `/api/v1/certs/settings/exemptions/:domain` | 运维主管/审计 | 移除豁免 |
| POST | `/api/v1/certs/settings/test` | 运维主管/审计 | 发送测试告警 |
| POST | `/api/v1/certs/settings/crds` | 运维主管/审计 | 登记自定义 CRD（clusterId+apiGroup+kind+certFieldPath；仅限 spec 含云托管证书引用字段的网关类资源；重复登记 409） |
| GET | `/api/v1/certs/settings/crds` | 运维主管/审计 | 登记列表（含 enabled 状态） |
| DELETE | `/api/v1/certs/settings/crds/:id` | 运维主管/审计 | 删除登记（该 CRD 回归扫描盲区并在视图声明） |

## Data Contracts

- `ChangeStatus` = 草稿|待确认|执行中|验证中|已完成|部分完成|已回滚|回滚失败|已取消（cancelled 终态：draft/pending_confirm 人工取消、批间暂停人工取消或 pauseTimeoutHours 超时自动中止；未执行项计 skipped）
- `ProbeStatus` = consistent|diff|change_linked_diff|unreachable|exempt|wildcard_skipped（change_linked_diff=验证窗口内变更关联差异，��� changeOrderId，走变更关联告警通道）
- wildcard_skipped = 通配符 SAN（如 *.example.com）无法直接 DNS 解析/SNI 拨测，默认跳过并写探测记录（计数可见、不计差异、不告警）；可经 settings `wildcardProbeOverrides` 指定具体子域名替代探测；验证窗口内无 override 的通配符验证项计 skipped，不阻塞达标
- `HostingStatus` = complete|fingerprint_only
- `BatchSessionStatus` = running|completed|partial_failed（批量导入会话终态；轮询端点数据源 cert_batch_sessions）
- `ExpiryAlertLevel` = none|L30|L14|L7|expired（到期分级告警去重状态，仅升级触发）
- `ReferenceStatus` = has_refs|no_refs_scanned|blind_spot（has_refs=有引用；no_refs_scanned=未发现引用/已扫描无匹配；blind_spot=盲区/未纳入扫描或扫描失败）
- `CoverageMeta` = `{cloud, product, covered, total}`（分母 total 来源 `internal/asset` 资产同步盘点，独立于引用扫描通道；total=-1 表示分母不可用，输出盲区声明而非 0%）

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| CERT_KEY_MISMATCH | 400 | 证书与私钥不匹配 |
| CERT_CHAIN_INCOMPLETE | 400 | 证书链缺失 |
| CERT_PARSE_FAIL | 400 | SAN 无法解析/已过期 |
| CERT_DUPLICATE_FINGERPRINT | 409 | 重复指纹导入 |
| CERT_HAS_REFS | 409 | 存在活跃引用或处于保护期，禁止删除 |
| SCAN_STALE | 409 | 扫描超新鲜度阈值 |
| SCAN_IN_PROGRESS | 409 | 扫描进行中（防重） |
| SAN_INSUFFICIENT | 409 | 新证书 SAN 不⊇目标域名 |
| NEW_CERT_FINGERPRINT_ONLY | 409 | 新证书仅指纹登记（无私钥），无法上传云证书库执行更换 |
| CHANGE_IN_FLIGHT | 409 | 同一旧证书存在在途变更单 |
| BATCH_NOT_CONFIRMABLE | 409 | 续批门控未满足 |
| CHANGE_NOT_CANCELLABLE | 409 | 当前状态不可取消（executing 仅未开始项可中止） |
| ROLLBACK_TARGET_INVALID | 409 | 回滚目标无效（云侧旧证书已删除/已过期/被替换），转人工 |
| CLOUD_API_RATELIMITED | 503 | 云 API 限流 |
| K8S_UNREACHABLE | 503 | 集群不可达 |
| FORBIDDEN | 403 | EIAM 权限不足 |
