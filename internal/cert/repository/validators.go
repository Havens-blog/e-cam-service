package repository

import "go.mongodb.org/mongo-driver/bson"

// 本文件是 design/schema.sql 全部 $jsonSchema 校验器的 Go 转录（1:1 对齐，不得自行增删）。
// keyVersion 等 int 字段由 Go int 序列化为 BSON int32，满足 bsonType:"int"。

// jsonSchema 组装顶层 validator 包装。
func jsonSchema(required bson.A, properties bson.M, extra ...bson.E) bson.M {
	spec := bson.M{
		"bsonType":   "object",
		"required":   required,
		"properties": properties,
	}
	for _, e := range extra {
		spec[e.Key] = e.Value
	}
	return bson.M{"$jsonSchema": spec}
}

// str 字符串字段。
func str() bson.M { return bson.M{"bsonType": "string"} }

// date 日期字段。
func date() bson.M { return bson.M{"bsonType": "date"} }

// enumStr 带枚举的字符串字段。
func enumStr(values ...string) bson.M {
	return bson.M{"bsonType": "string", "enum": toBsonA(values)}
}

// toBsonA 字符串切片转 bson.A。
func toBsonA(values []string) bson.A {
	out := make(bson.A, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// fingerprintPattern SHA256 hex 指纹约束。
func fingerprintPattern() bson.M {
	return bson.M{"bsonType": "string", "pattern": "^[0-9a-f]{64}$"}
}

// encryptedSecretSchema 信封加密密文对象（encryptedPrivateKey / kubeconfig 共用形状）。
func encryptedSecretSchema() bson.M {
	return bson.M{
		"bsonType": "object",
		"required": bson.A{"ciphertext", "keyVersion", "algo"},
		"properties": bson.M{
			"ciphertext": str(),
			"keyVersion": bson.M{"bsonType": "int", "minimum": 1},
			"algo":       enumStr("AES-256-GCM"),
		},
	}
}

// collectionValidators 集合名 → $jsonSchema 校验器（与 schema.sql createCollection 逐条对应）。
var collectionValidators = map[string]bson.M{
	// cert_certificates 证书台账
	CertificatesCollection: jsonSchema(
		bson.A{"fingerprint", "hostingStatus", "createdAt"},
		bson.M{
			"fingerprint":         fingerprintPattern(),
			"commonName":          str(),
			"sans":                bson.M{"bsonType": "array", "items": str()},
			"issuer":              str(),
			"serialNumber":        str(),
			"notBefore":           date(),
			"notAfter":            date(),
			"keyAlgorithm":        enumStr("RSA", "ECDSA"),
			"hostingStatus":       enumStr("complete", "fingerprint_only"),
			"encryptedPrivateKey": encryptedSecretSchema(),
			"expectedDomain":      str(),
			"protectUntil":        date(),
			"expiryAlertLevel":    enumStr("none", "L30", "L14", "L7", "expired"),
			"createdAt":           date(),
		},
	),

	// cert_references 引用扫描发现
	// product 含 cas（阿里云 CAS 证书库清单形态引用，cert-cas-library-scan）：
	// 仅引用发现语义，cert_change_items.resourceRef.product enum 不含 cas
	//（证书库条目无资源绑定语义，不可部署）。
	CertReferencesCollection: jsonSchema(
		bson.A{"certFingerprint", "snapshotId", "scannedAt"},
		bson.M{
			"certFingerprint":       fingerprintPattern(),
			"cloud":                 enumStr("aliyun", "tencent", "huawei", "aws", "azure"),
			"product":               enumStr("cdn", "dcdn", "waf", "alb", "clb", "nlb", "crd", "cas"),
			"clusterId":             str(),
			"namespace":             str(),
			"kind":                  str(),
			"resourceId":            str(),
			"referencedCloudCertId": str(),
			"accountKey":            str(),
			"snapshotId":            str(),
			"scannedAt":             date(),
		},
	),

	// cert_scan_snapshots 扫描快照元数据
	ScanSnapshotsCollection: jsonSchema(
		bson.A{"startedAt", "status"},
		bson.M{
			"startedAt":  date(),
			"finishedAt": date(),
			"status":     enumStr("running", "done", "failed"),
			"failReason": str(),
			"partialFailures": bson.M{
				"bsonType": "array",
				"items": bson.M{
					"bsonType": "object",
					"required": bson.A{"cloud", "product", "reason"},
					"properties": bson.M{
						"cloud":   str(),
						"product": str(),
						"account": str(),
						"reason":  str(),
					},
				},
			},
			"coverageMeta": bson.M{
				"bsonType": "array",
				"items": bson.M{
					"bsonType": "object",
					"required": bson.A{"cloud", "product", "covered", "total"},
					"properties": bson.M{
						"cloud":   str(),
						"product": str(),
						"covered": bson.M{"bsonType": "int", "minimum": 0},
						"total":   bson.M{"bsonType": "int", "minimum": -1},
						"lagging": bson.M{"bsonType": "bool"},
					},
				},
			},
		},
	),

	// cert_change_orders 变更单
	ChangeOrdersCollection: jsonSchema(
		bson.A{"oldCertFingerprint", "newCertId", "status", "snapshotId", "creator", "createdAt"},
		bson.M{
			"oldCertFingerprint": fingerprintPattern(),
			"newCertId":          str(),
			"status": enumStr("draft", "pending_confirm", "executing", "verifying",
				"completed", "partial_completed", "rolled_back", "rollback_failed", "cancelled"),
			"batchInfo": bson.M{
				"bsonType": "object",
				"required": bson.A{"totalBatches", "currentBatch", "batchSize"},
				"properties": bson.M{
					"totalBatches": bson.M{"bsonType": "int", "minimum": 1},
					"currentBatch": bson.M{"bsonType": "int", "minimum": 1},
					"batchSize":    bson.M{"bsonType": "int", "minimum": 1},
					"paused":       bson.M{"bsonType": "bool"},
					"pausedAt":     date(),
				},
			},
			"snapshotId":        str(),
			"verifyWindowUntil": date(),
			"protectUntil":      date(),
			"activeMutex":       str(),
			"verifyExpected": bson.M{
				"bsonType": "object",
				"required": bson.A{"newCertFingerprint", "domains", "windowUntil"},
				"properties": bson.M{
					"newCertFingerprint": fingerprintPattern(),
					"domains":            bson.M{"bsonType": "array", "items": str()},
					"excludedDomains":    bson.M{"bsonType": "array", "items": str()},
					"windowUntil":        date(),
				},
			},
			"creator":   str(),
			"createdAt": date(),
		},
	),

	// cert_change_items 变更项（resourceRef 按 action 分支必填，anyOf 强制）
	ChangeItemsCollection: jsonSchema(
		bson.A{"orderId", "action", "resourceRef", "status"},
		bson.M{
			"orderId":        str(),
			"batchNo":        bson.M{"bsonType": "int", "minimum": 1},
			"action":         enumStr("upload_and_bind", "patch_crd"),
			"resourceRef":    bson.M{"bsonType": "object"},
			"oldCloudCertId": str(),
			"newCloudCertId": str(),
			"status": enumStr("pending", "running", "success", "failed",
				"rate_limited", "rolled_back", "rollback_failed", "skipped"),
			"error":       str(),
			"heartbeatAt": date(),
			"executedAt":  date(),
			"recheckedAt": date(), // crd-recheck 单轮复检完成时点（任务 5.9 幂等标记）
		},
		bson.E{Key: "allOf", Value: bson.A{bson.M{"anyOf": bson.A{
			// upload_and_bind(cloud_api): channel+cloud+product+accountKey+resourceId
			bson.M{"properties": bson.M{
				"action": bson.M{"enum": bson.A{"upload_and_bind"}},
				"resourceRef": bson.M{
					"bsonType": "object",
					"required": bson.A{"channel", "cloud", "product", "accountKey", "resourceId"},
					"properties": bson.M{
						"channel":    bson.M{"enum": bson.A{"cloud_api"}},
						"cloud":      str(),
						"product":    enumStr("cdn", "dcdn", "waf", "alb", "clb", "nlb"),
						"accountKey": str(),
						"resourceId": str(),
					},
				},
			}},
			// patch_crd(k8s_api): channel+clusterId+namespace+kind+resourceId
			bson.M{"properties": bson.M{
				"action": bson.M{"enum": bson.A{"patch_crd"}},
				"resourceRef": bson.M{
					"bsonType": "object",
					"required": bson.A{"channel", "clusterId", "namespace", "kind", "resourceId"},
					"properties": bson.M{
						"channel":    bson.M{"enum": bson.A{"k8s_api"}},
						"clusterId":  str(),
						"namespace":  str(),
						"kind":       str(),
						"resourceId": str(),
					},
				},
			}},
		}}}},
	),

	// cert_cloud_cert_mappings 平台证书↔云证书ID映射
	CloudCertMappingsCollection: jsonSchema(
		bson.A{"certFingerprint", "cloud", "accountKey", "cloudCertId", "uploadedAt", "status"},
		bson.M{
			"certFingerprint": fingerprintPattern(),
			"cloud":           str(),
			"accountKey":      str(),
			"cloudCertId":     str(),
			"uploadedAt":      date(),
			"status":          enumStr("active", "orphan"),
		},
	),

	// cert_probe_results TLS 探测结果
	ProbeResultsCollection: jsonSchema(
		bson.A{"domain", "probeAt", "status"},
		bson.M{
			"domain":            str(),
			"probeAt":           date(),
			"onlineFingerprint": str(),
			"onlineNotAfter":    date(),
			"status": enumStr("consistent", "diff", "change_linked_diff",
				"unreachable", "exempt", "wildcard_skipped"),
			"changeOrderId": str(),
		},
	),

	// cert_exemptions 探测豁免清单
	ExemptionsCollection: jsonSchema(
		bson.A{"domain", "createdAt"},
		bson.M{
			"domain":    str(),
			"reason":    str(),
			"operator":  str(),
			"createdAt": date(),
		},
	),

	// cert_alert_config 全局告警配置（单文档，固定 _id="global"）
	AlertConfigCollection: jsonSchema(
		bson.A{"thresholds"},
		bson.M{
			"webhookUrls":      bson.M{"bsonType": "array", "items": str()},
			"emailGroup":       bson.M{"bsonType": "array", "items": str()},
			"channelConfirmed": bson.M{"bsonType": "bool"},
			"thresholds": bson.M{
				"bsonType": "object",
				"required": bson.A{
					"scanFreshnessHours", "verifyWindowHours", "rollbackProtectDays",
					"verifyConfirmProbes", "verifyProbeIntervalMinutes", "pauseTimeoutHours",
					"recheckDelayMinutes", "itemHeartbeatTimeoutMinutes", "scanTimeoutHours",
					"expiryLevels",
				},
				"properties": bson.M{
					"scanFreshnessHours":          bson.M{"bsonType": "int", "minimum": 1, "maximum": 72},
					"verifyWindowHours":           bson.M{"bsonType": "int", "minimum": 2, "maximum": 24},
					"rollbackProtectDays":         bson.M{"bsonType": "int", "minimum": 7, "maximum": 14},
					"verifyConfirmProbes":         bson.M{"bsonType": "int", "minimum": 1, "maximum": 10},
					"verifyProbeIntervalMinutes":  bson.M{"bsonType": "int", "minimum": 5, "maximum": 60},
					"pauseTimeoutHours":           bson.M{"bsonType": "int", "minimum": 24, "maximum": 168},
					"recheckDelayMinutes":         bson.M{"bsonType": "int", "minimum": 1, "maximum": 60},
					"itemHeartbeatTimeoutMinutes": bson.M{"bsonType": "int", "minimum": 5, "maximum": 180},
					"scanTimeoutHours":            bson.M{"bsonType": "int", "minimum": 1, "maximum": 12},
					"expiryLevels": bson.M{
						"bsonType": "array", "uniqueItems": true, "minItems": 1, "maxItems": 5,
						"items": bson.M{"bsonType": "int", "minimum": 1, "maximum": 90},
					},
				},
			},
			"verifyWindowRoute": bson.M{
				"bsonType": "object",
				"required": bson.A{"enabled"},
				"properties": bson.M{
					"enabled":     bson.M{"bsonType": "bool"},
					"webhookUrls": bson.M{"bsonType": "array", "items": str()},
					"emailGroup":  bson.M{"bsonType": "array", "items": str()},
				},
			},
			"wildcardProbeOverrides": bson.M{
				"bsonType":             "object",
				"additionalProperties": str(),
			},
		},
	),

	// cert_k8s_credentials K8s 集群接入凭证
	K8sCredentialsCollection: jsonSchema(
		bson.A{"clusterName", "kubeconfig", "createdAt"},
		bson.M{
			"clusterName": str(),
			"kubeconfig":  encryptedSecretSchema(),
			"apiEndpoint": str(),
			"createdAt":   date(),
		},
	),

	// cert_batch_sessions 批量导入会话
	BatchSessionsCollection: jsonSchema(
		bson.A{"status", "files", "progress", "operator", "createdAt"},
		bson.M{
			"status": enumStr("running", "completed", "partial_failed"),
			"files": bson.M{
				"bsonType": "array",
				"items": bson.M{
					"bsonType": "object",
					"required": bson.A{"fileName", "result"},
					"properties": bson.M{
						"fileName":    str(),
						"result":      enumStr("pending", "success", "failed"),
						"certId":      str(),
						"errorReason": str(),
					},
				},
			},
			"progress": bson.M{
				"bsonType": "object",
				"required": bson.A{"total", "done", "failed"},
				"properties": bson.M{
					"total":  bson.M{"bsonType": "int", "minimum": 1},
					"done":   bson.M{"bsonType": "int", "minimum": 0},
					"failed": bson.M{"bsonType": "int", "minimum": 0},
				},
			},
			"operator":   str(),
			"createdAt":  date(),
			"finishedAt": date(),
		},
	),

	// cert_discovery_import_sessions 云端发现导入会话（cert-cloud-discovery-import）
	DiscoveryImportSessionsCollection: jsonSchema(
		bson.A{"status", "items", "progress", "operator", "createdAt"},
		bson.M{
			"status": enumStr("running", "completed", "partial_failed"),
			"items": bson.M{
				"bsonType": "array",
				"items": bson.M{
					"bsonType": "object",
					// 条目以 cloud+accountKey+cloudCertId 三元组定位（区别于批量导入 fileName）
					"required": bson.A{"cloud", "accountKey", "cloudCertId", "result"},
					"properties": bson.M{
						"cloud":        str(),
						"accountKey":   str(),
						"cloudCertId":  str(),
						"result":       enumStr("pending", "success", "failed"),
						"mappedCertId": str(),
						"errorReason":  str(),
					},
				},
			},
			"progress": bson.M{
				"bsonType": "object",
				"required": bson.A{"total", "succeeded", "failed"},
				"properties": bson.M{
					"total":     bson.M{"bsonType": "int", "minimum": 1},
					"succeeded": bson.M{"bsonType": "int", "minimum": 0},
					"failed":    bson.M{"bsonType": "int", "minimum": 0},
				},
			},
			"operator":   str(),
			"createdAt":  date(),
			"finishedAt": date(),
		},
	),

	// cert_crd_registrations 自定义 CRD 扫描登记
	CrdRegistrationsCollection: jsonSchema(
		bson.A{"clusterId", "apiGroup", "kind", "certFieldPath", "enabled", "operator", "createdAt"},
		bson.M{
			"clusterId":     str(),
			"apiGroup":      str(),
			"kind":          str(),
			"certFieldPath": str(),
			"enabled":       bson.M{"bsonType": "bool"},
			"operator":      str(),
			"createdAt":     date(),
		},
	),
}
