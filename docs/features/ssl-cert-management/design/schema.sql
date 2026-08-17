-- SSL 证书管理功能域 — 集合文档结构 (MongoDB)
-- e-cam-service 使用 MongoDB (mongox)；此文件以 SQL 风格描述集合文档结构供可追溯性参考。
-- 实际无关系表，"表"对应 MongoDB 集合，"列"对应文档字段。

-- ============================================================
-- cert_certificates 证书台账
-- ============================================================
CREATE TABLE cert_certificates (
    id              STRING PK,
    fingerprint     STRING UNIQUE NOT NULL,        -- SHA256 hex，聚合主键
    common_name     STRING,
    sans            STRING[],                       -- SAN 域名数组
    issuer          STRING,
    serial_number   STRING,
    not_before      TIMESTAMP,
    not_after       TIMESTAMP,
    key_algorithm   STRING,                         -- RSA/ECDSA
    hosting_status  STRING NOT NULL,               -- complete | fingerprint_only
    encrypted_private_key OBJECT,                  -- {ciphertext, keyVersion, algo:"AES-256-GCM"}
    expected_domain  STRING,                        -- 可选，仅提示性比对
    protect_until   TIMESTAMP,                     -- 回滚保护期截止，>=now 则禁删
    created_at      TIMESTAMP
);

-- ============================================================
-- cert_references 引用扫描发现
-- ============================================================
CREATE TABLE cert_references (
    id                      STRING PK,
    cert_fingerprint        STRING NOT NULL,       -- 关联 certificates.fingerprint
    cloud                   STRING,               -- aliyun|tencent|huawei|aws|azure
    product                 STRING,               -- cdn|dcdn|waf|alb|clb|nlb|crd
    cluster_id              STRING,               -- K8s 集群（CRD 引用时）
    resource_id             STRING,
    referenced_cloud_cert_id STRING,              -- 云侧证书 ID
    account_key             STRING,
    snapshot_id             STRING NOT NULL,      -- 来源扫描快照
    scanned_at             TIMESTAMP
);

-- ============================================================
-- cert_scan_snapshots 扫描快照元数据
-- ============================================================
CREATE TABLE cert_scan_snapshots (
    id            STRING PK,
    started_at    TIMESTAMP,
    finished_at   TIMESTAMP,
    status        STRING,                         -- running|done|failed
    coverage_meta OBJECT                           -- [{cloud,product,covered,total}]
);

-- ============================================================
-- cert_change_orders 变更单
-- ============================================================
CREATE TABLE cert_change_orders (
    id                    STRING PK,
    old_cert_fingerprint  STRING NOT NULL,        -- 指纹聚合键
    new_cert_id           STRING NOT NULL,
    status                STRING NOT NULL,        -- 草稿|待确认|执行中|验证中|已完成|部分完成|已回滚|回滚失败
    batch_info            OBJECT,                 -- {totalBatches, currentBatch, batchSize}
    snapshot_id           STRING NOT NULL,        -- 绑定的扫描快照
    verify_window_until   TIMESTAMP,             -- 验证窗口截止
    protect_until         TIMESTAMP,             -- 回滚保护期截止
    creator               STRING,
    created_at            TIMESTAMP
);

-- ============================================================
-- cert_change_items 变更项（逐项执行）
-- ============================================================
CREATE TABLE cert_change_items (
    id                  STRING PK,
    order_id            STRING NOT NULL,
    resource_ref        OBJECT,                   -- {cloud,product,resourceId,clusterId}
    action              STRING,                   -- upload_and_bind | patch_crd
    old_cloud_cert_id   STRING,
    new_cloud_cert_id   STRING,
    status              STRING,                   -- pending|running|success|failed|rate_limited|rolled_back
    error              STRING,
    executed_at         TIMESTAMP
);

-- ============================================================
-- cert_cloud_cert_mappings 平台证书↔云证书ID映射
-- ============================================================
CREATE TABLE cert_cloud_cert_mappings (
    id                  STRING PK,
    cert_fingerprint    STRING NOT NULL,
    cloud               STRING NOT NULL,
    account_key         STRING NOT NULL,
    cloud_cert_id       STRING NOT NULL,
    uploaded_at         TIMESTAMP,
    status              STRING,                   -- active | orphan
    UNIQUE(cert_fingerprint, cloud, account_key)
);

-- ============================================================
-- cert_probe_results TLS 探测结果
-- ============================================================
CREATE TABLE cert_probe_results (
    id                  STRING PK,
    domain              STRING NOT NULL,
    probe_at            TIMESTAMP,
    online_fingerprint  STRING,
    online_not_after    TIMESTAMP,
    status              STRING                     -- consistent|diff|unreachable|exempt
);

-- ============================================================
-- cert_exemptions 探测豁免清单
-- ============================================================
CREATE TABLE cert_exemptions (
    id          STRING PK,
    domain      STRING UNIQUE NOT NULL,
    reason      STRING,
    operator    STRING,
    created_at  TIMESTAMP
);

-- ============================================================
-- cert_alert_config 全局告警配置（单文档）
-- ============================================================
CREATE TABLE cert_alert_config (
    id                STRING PK,
    webhook_urls      STRING[],
    email_group       STRING[],
    channel_confirmed BOOL,
    thresholds       OBJECT                       -- {scanFreshnessHours:24, verifyWindowHours:2~24, rollbackProtectDays:7~14}
);

-- ============================================================
-- cert_k8s_credentials K8s 集群接入凭证
-- ============================================================
CREATE TABLE cert_k8s_credentials (
    id            STRING PK,
    cluster_name  STRING UNIQUE NOT NULL,
    kubeconfig    OBJECT,                          -- {ciphertext, keyVersion, algo}，加密存储
    api_endpoint  STRING,
    created_at    TIMESTAMP
);
