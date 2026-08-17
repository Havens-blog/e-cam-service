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
Request: multipart 多文件 + 逐文件可选私钥。Response: `{files[]{fileName,result,errorReason,certId?}, progress}`。错误同上 + 进度轮询 `GET /api/v1/certs/batch/:batchId`。

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
| GET | `/api/v1/certs/:id/references` | 运维工程师 | 正向引用（分组+覆盖率元数据+盲区声明） |
| GET | `/api/v1/certs/reverse?domain=` | 运维工程师 | 反向查询（域名/资源→证书） |
| POST | `/api/v1/certs/:id/scan` | 运维工程师 | 立即扫描（防重 409 SCAN_IN_PROGRESS） |

---

### 到期看板

**Method**: `GET`  **Path**: `/api/v1/certs/dashboard`  **Auth**: 全角色（含只读）
Response: `{summary{countsByLevel[5],diffAlertCount,exemptCount}, items[]{domain,daysLeft,level,hostingType,probeStatus,referencedClouds[]}, lastInspectionAt}`

---

### 变更管理

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/api/v1/certs/changes` | 运维工程师/主管 | 变更单列表（状态 Tab 筛选） |
| POST | `/api/v1/certs/changes` | 运维工程师 | 生成变更清单（oldFingerprint+newCertId） |
| GET | `/api/v1/certs/changes/:id` | 运维工程师/主管 | 变更单/报告详情 |
| POST | `/api/v1/certs/changes/:id/confirm` | 运维工程师 | 确认执行（含分批配置） |
| POST | `/api/v1/certs/changes/:id/execute` | 运维工程师 | 触发批量执行 |
| GET | `/api/v1/certs/changes/:id/progress` | 运维工程师 | 逐项进度轮询 |
| POST | `/api/v1/certs/changes/:id/rollback` | 运维工程师 | 回滚成功项（前置目标有效性校验） |

#### 生成清单错误

| Status | Code | Description |
|--------|------|-------------|
| 409 | SCAN_STALE | 扫描超新鲜度阈值，阻断生成 |
| 409 | SAN_INSUFFICIENT | 新证书 SAN 不⊇目标域名 |
| 409 | CHANGE_IN_FLIGHT | 同一旧证书存在在途变更单 |
| 409 | ROLLBACK_TARGET_INVALID | 旧证书已删除/过期，转人工 |

---

### 全局配置

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/api/v1/certs/settings` | 运维主管/审计 | 告警配置+阈值+豁免清单 |
| PUT | `/api/v1/certs/settings` | 运维主管/审计 | 更新告警接收人/阈值（越界 400） |
| POST | `/api/v1/certs/settings/exemptions` | 运维主管/审计 | 添加豁免 |
| DELETE | `/api/v1/certs/settings/exemptions/:domain` | 运维主管/审计 | 移除豁免 |
| POST | `/api/v1/certs/settings/test` | 运维主管/审计 | 发送测试告警 |

## Data Contracts

- `ChangeStatus` = 草稿|待确认|执行中|验证中|已完成|部分完成|已回滚|回滚失败
- `ProbeStatus` = consistent|diff|unreachable|exempt
- `HostingStatus` = complete|fingerprint_only
- `CoverageMeta` = `{cloud, product, covered, total}`（分母 total 来源资产同步独立盘点）

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
| CHANGE_IN_FLIGHT | 409 | 同一旧证书存在在途变更单 |
| ROLLBACK_TARGET_INVALID | 409 | 回滚目标无效，转人工 |
| CLOUD_API_RATELIMITED | 503 | 云 API 限流 |
| K8S_UNREACHABLE | 503 | 集群不可达 |
| FORBIDDEN | 403 | EIAM 权限不足 |
