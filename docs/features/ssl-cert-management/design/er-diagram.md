# ER Diagram: SSL 证书管理功能域

```mermaid
erDiagram
    CERTIFICATES ||--o{ CERT_REFERENCES : "fingerprint"
    CERTIFICATES ||--o{ CLOUD_CERT_MAPPINGS : "fingerprint"
    CERTIFICATES ||--o{ CHANGE_ORDERS : "oldFingerprint / newCertId"
    CERTIFICATES ||--o{ PROBE_RESULTS : "domain -> fingerprint"
    SCAN_SNAPSHOTS ||--o{ CERT_REFERENCES : "snapshotId"
    CHANGE_ORDERS ||--o{ CHANGE_ITEMS : "orderId"
    CHANGE_ORDERS }o--|| SCAN_SNAPSHOTS : "bound snapshot"
    CERTIFICATES ||--o{ EXEMPTIONS : "domain"
    K8S_CREDENTIALS ||--o{ CERT_REFERENCES : "cluster"
    ALERT_CONFIG ||--|| THRESHOLDS : "embed"

    CERTIFICATES {
        string id PK
        string fingerprint UK
        string commonName
        string[] sans
        string issuer
        string serialNumber
        time notBefore
        time notAfter
        string keyAlgorithm
        string hostingStatus "complete|fingerprint_only"
        object encryptedPrivateKey "cipher,keyVersion,algo"
        string expectedDomain "optional"
        time protectUntil "回滚保护期"
        time createdAt
    }

    CERT_REFERENCES {
        string id PK
        string certFingerprint FK
        string cloud
        string product
        string clusterId FK
        string resourceId
        string referencedCloudCertId
        string accountKey
        string snapshotId FK
        time scannedAt
    }

    SCAN_SNAPSHOTS {
        string id PK
        time startedAt
        time finishedAt
        string status
        object coverageMeta "cloud,product,covered,total"
    }

    CHANGE_ORDERS {
        string id PK
        string oldCertFingerprint FK
        string newCertId FK
        string status "8态状态机"
        object batchInfo
        string snapshotId FK
        time verifyWindowUntil
        time protectUntil
        string creator
        time createdAt
    }

    CHANGE_ITEMS {
        string id PK
        string orderId FK
        string resourceRef
        string action
        string oldCloudCertId
        string newCloudCertId
        string status
        string error
        time executedAt
    }

    CLOUD_CERT_MAPPINGS {
        string id PK
        string certFingerprint FK
        string cloud
        string accountKey
        string cloudCertId
        time uploadedAt
        string status "active|orphan"
    }

    PROBE_RESULTS {
        string id PK
        string domain
        time probeAt
        string onlineFingerprint
        time onlineNotAfter
        string status "consistent|diff|unreachable|exempt"
    }

    EXEMPTIONS {
        string id PK
        string domain
        string reason
        string operator
        time createdAt
    }

    ALERT_CONFIG {
        string id PK
        string[] webhookUrls
        string[] emailGroup
        bool channelConfirmed
        object thresholds
    }

    K8S_CREDENTIALS {
        string id PK
        string clusterName
        object kubeconfig "encrypted,keyVersion"
        string apiEndpoint
        time createdAt
    }

    THRESHOLDS {
        int scanFreshnessHours
        int verifyWindowHours
        int rollbackProtectDays
    }
```

## 索引策略

| 集合 | 索引 | 用途 |
|------|------|------|
| cert_certificates | fingerprint (unique) | 去重/聚合键 |
| cert_certificates | hostingStatus | 台账统计 |
| cert_references | certFingerprint + cloud + product | 引用查询 |
| cert_references | snapshotId | 按快照清理 |
| cert_change_orders | oldCertFingerprint + status | 在途互斥查询 |
| cert_change_items | orderId | 变更单明细 |
| cert_cloud_cert_mappings | certFingerprint + cloud + accountKey (unique) | 两段式去重 |
| cert_probe_results | domain + probeAt (desc) | 最近探测查询 |
