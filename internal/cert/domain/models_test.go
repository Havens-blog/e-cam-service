package domain

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// TestBsonTagsMatchSchema 校验 13 个模型（含嵌套结构）的 bson 字段名与
// design/schema.sql 的 $jsonSchema properties 1:1 对齐（不得自行增删字段）。
// 预期清单转录自 schema.sql 各集合 properties（_id 为 MongoDB 隐含主键）。
func TestBsonTagsMatchSchema(t *testing.T) {
	cases := []struct {
		name     string
		typ      any
		topLevel bool     // 集合文档（含 MongoDB 隐含 _id）or 内嵌结构
		expected []string // schema.sql 字段名（不含 _id）
	}{
		{"Certificate", Certificate{}, true, []string{
			"fingerprint", "commonName", "sans", "issuer", "serialNumber",
			"notBefore", "notAfter", "keyAlgorithm", "hostingStatus",
			"encryptedPrivateKey", "certPem", "expectedDomain", "protectUntil",
			"expiryAlertLevel", "createdAt",
		}},
		{"CertReference", CertReference{}, true, []string{
			"certFingerprint", "cloud", "product", "clusterId", "namespace", "kind",
			"resourceId", "referencedCloudCertId", "accountKey", "snapshotId", "scannedAt",
		}},
		{"ScanSnapshot", ScanSnapshot{}, true, []string{
			"startedAt", "finishedAt", "status", "failReason", "partialFailures", "coverageMeta",
		}},
		{"CoverageMeta", CoverageMeta{}, false, []string{"cloud", "product", "covered", "total", "lagging"}},
		{"ScanChannelFailure", ScanChannelFailure{}, false, []string{"cloud", "product", "account", "reason"}},
		{"ChangeOrder", ChangeOrder{}, true, []string{
			"oldCertFingerprint", "newCertId", "status", "batchInfo", "snapshotId",
			"verifyWindowUntil", "protectUntil", "activeMutex", "verifyExpected",
			"creator", "createdAt",
		}},
		{"BatchInfo", BatchInfo{}, false, []string{
			"totalBatches", "currentBatch", "batchSize", "paused", "pausedAt",
		}},
		{"VerifyExpected", VerifyExpected{}, false, []string{
			"newCertFingerprint", "domains", "excludedDomains", "windowUntil",
		}},
		{"ChangeItem", ChangeItem{}, true, []string{
			"orderId", "batchNo", "action", "resourceRef", "oldCloudCertId",
			"newCloudCertId", "status", "error", "heartbeatAt", "executedAt",
			"recheckedAt",
		}},
		{"ResourceRef", ResourceRef{}, false, []string{
			"channel", "cloud", "product", "accountKey",
			"clusterId", "namespace", "kind", "resourceId",
		}},
		{"CloudCertMapping", CloudCertMapping{}, true, []string{
			"certFingerprint", "cloud", "accountKey", "cloudCertId",
			"uploadedAt", "status",
		}},
		{"ProbeResult", ProbeResult{}, true, []string{
			"domain", "probeAt", "onlineFingerprint", "onlineNotAfter",
			"status", "changeOrderId",
		}},
		{"Exemption", Exemption{}, true, []string{"domain", "reason", "operator", "createdAt"}},
		{"AlertConfig", AlertConfig{}, true, []string{
			"webhookUrls", "emailGroup", "channelConfirmed", "thresholds",
			"verifyWindowRoute", "wildcardProbeOverrides",
		}},
		{"Thresholds", Thresholds{}, false, []string{
			"scanFreshnessHours", "verifyWindowHours", "rollbackProtectDays",
			"verifyConfirmProbes", "verifyProbeIntervalMinutes", "pauseTimeoutHours",
			"recheckDelayMinutes", "itemHeartbeatTimeoutMinutes", "scanTimeoutHours",
			"expiryLevels",
		}},
		{"VerifyWindowRoute", VerifyWindowRoute{}, false, []string{"enabled", "webhookUrls", "emailGroup"}},
		{"K8sCredential", K8sCredential{}, true, []string{"clusterName", "kubeconfig", "apiEndpoint", "createdAt"}},
		{"EncryptedSecret", EncryptedSecret{}, false, []string{"ciphertext", "keyVersion", "algo"}},
		{"CertBatchSession", CertBatchSession{}, true, []string{
			"status", "files", "progress", "operator", "createdAt", "finishedAt",
		}},
		{"BatchSessionFile", BatchSessionFile{}, false, []string{"fileName", "result", "certId", "errorReason"}},
		{"BatchProgress", BatchProgress{}, false, []string{"total", "done", "failed"}},
		{"DiscoveryImportSession", DiscoveryImportSession{}, true, []string{
			"status", "items", "progress", "operator", "createdAt", "finishedAt",
		}},
		{"DiscoveryImportItem", DiscoveryImportItem{}, false, []string{
			"cloud", "accountKey", "cloudCertId", "result", "mappedCertId", "errorReason",
		}},
		{"DiscoveryImportProgress", DiscoveryImportProgress{}, false, []string{"total", "succeeded", "failed"}},
		{"CrdRegistration", CrdRegistration{}, true, []string{
			"clusterId", "apiGroup", "kind", "certFieldPath", "enabled",
			"operator", "createdAt",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bsonFieldNames(tc.typ)
			want := append([]string{}, tc.expected...)
			if tc.topLevel {
				want = append(want, "_id")
			}
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("bson 字段与 schema.sql 不一致\n got: %v\nwant: %v", got, want)
			}
		})
	}
}

// bsonFieldNames 提取结构体全部 bson 字段名（含显式 _id；忽略 -）。
func bsonFieldNames(typ any) []string {
	rt := reflect.TypeOf(typ)
	var names []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("bson")
		if tag == "" || tag == "-" {
			continue
		}
		names = append(names, strings.Split(tag, ",")[0])
	}
	return names
}

// TestEnumValuesMatchSchema 校验枚举值与 schema.sql enum 逐值对齐。
func TestEnumValuesMatchSchema(t *testing.T) {
	assertEnum := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s 枚举值与 schema.sql 不一致\n got: %v\nwant: %v", name, got, want)
		}
	}
	assertEnum("hostingStatus",
		[]string{string(HostingStatusComplete), string(HostingStatusFingerprintOnly)},
		[]string{"complete", "fingerprint_only"})
	assertEnum("keyAlgorithm",
		[]string{string(KeyAlgorithmRSA), string(KeyAlgorithmECDSA)},
		[]string{"RSA", "ECDSA"})
	assertEnum("expiryAlertLevel",
		[]string{string(ExpiryAlertNone), string(ExpiryAlertL30), string(ExpiryAlertL14), string(ExpiryAlertL7), string(ExpiryAlertExpired)},
		[]string{"none", "L30", "L14", "L7", "expired"})
	assertEnum("cloud",
		[]string{string(CloudAliyun), string(CloudTencent), string(CloudHuawei), string(CloudAWS), string(CloudAzure)},
		[]string{"aliyun", "tencent", "huawei", "aws", "azure"})
	assertEnum("product",
		[]string{string(ProductCDN), string(ProductDCDN), string(ProductWAF), string(ProductALB), string(ProductCLB), string(ProductNLB), string(ProductCRD)},
		[]string{"cdn", "dcdn", "waf", "alb", "clb", "nlb", "crd"})
	assertEnum("scanSnapshot.status",
		[]string{string(ScanStatusRunning), string(ScanStatusDone), string(ScanStatusFailed)},
		[]string{"running", "done", "failed"})
	assertEnum("changeOrder.status",
		[]string{string(ChangeStatusDraft), string(ChangeStatusPendingConfirm), string(ChangeStatusExecuting),
			string(ChangeStatusVerifying), string(ChangeStatusCompleted), string(ChangeStatusPartialCompleted),
			string(ChangeStatusRolledBack), string(ChangeStatusRollbackFailed), string(ChangeStatusCancelled)},
		[]string{"draft", "pending_confirm", "executing", "verifying", "completed",
			"partial_completed", "rolled_back", "rollback_failed", "cancelled"})
	assertEnum("changeItem.action",
		[]string{string(ActionUploadAndBind), string(ActionPatchCRD)},
		[]string{"upload_and_bind", "patch_crd"})
	assertEnum("changeItem.status",
		[]string{string(ItemStatusPending), string(ItemStatusRunning), string(ItemStatusSuccess),
			string(ItemStatusFailed), string(ItemStatusRateLimited), string(ItemStatusRolledBack),
			string(ItemStatusRollbackFailed), string(ItemStatusSkipped)},
		[]string{"pending", "running", "success", "failed", "rate_limited", "rolled_back", "rollback_failed", "skipped"})
	assertEnum("cloudCertMapping.status",
		[]string{string(MappingStatusActive), string(MappingStatusOrphan)},
		[]string{"active", "orphan"})
	assertEnum("probeResult.status",
		[]string{string(ProbeStatusConsistent), string(ProbeStatusDiff), string(ProbeStatusChangeLinkedDiff),
			string(ProbeStatusUnreachable), string(ProbeStatusExempt), string(ProbeStatusWildcardSkipped)},
		[]string{"consistent", "diff", "change_linked_diff", "unreachable", "exempt", "wildcard_skipped"})
	assertEnum("batchSession.status",
		[]string{string(BatchSessionRunning), string(BatchSessionCompleted), string(BatchSessionPartialFailed)},
		[]string{"running", "completed", "partial_failed"})
	assertEnum("batchSession.files[].result",
		[]string{string(BatchFilePending), string(BatchFileSuccess), string(BatchFileFailed)},
		[]string{"pending", "success", "failed"})
	assertEnum("discoveryImportSession.status",
		[]string{string(DiscoveryImportRunning), string(DiscoveryImportCompleted), string(DiscoveryImportPartialFailed)},
		[]string{"running", "completed", "partial_failed"})
	assertEnum("discoveryImportSession.items[].result",
		[]string{string(DiscoveryItemPending), string(DiscoveryItemSuccess), string(DiscoveryItemFailed)},
		[]string{"pending", "success", "failed"})
	assertEnum("channel",
		[]string{string(ChannelCloudAPI), string(ChannelK8sAPI)},
		[]string{"cloud_api", "k8s_api"})
}

// TestChangeStatusPartition 活跃态/终态集合须覆盖全部 9 态且互不相交。
func TestChangeStatusPartition(t *testing.T) {
	all := append(append([]ChangeStatus{}, ActiveChangeStatuses...), TerminalChangeStatuses...)
	if len(all) != 9 {
		t.Fatalf("活跃+终态应覆盖 9 态，实际 %d", len(all))
	}
	for _, s := range ActiveChangeStatuses {
		if IsActiveChangeStatus(s) != true {
			t.Errorf("%s 应为活跃态", s)
		}
	}
	for _, s := range TerminalChangeStatuses {
		if IsActiveChangeStatus(s) {
			t.Errorf("%s 是终态，不应判为活跃态", s)
		}
	}
}

// TestEncryptedSecretBsonShape 密文形态序列化字段形状必须为 {ciphertext,keyVersion,algo}。
func TestEncryptedSecretBsonShape(t *testing.T) {
	raw, err := bson.Marshal(EncryptedSecret{
		Ciphertext: "aGVsbG8=", KeyVersion: 1, Algo: AlgoAES256GCM,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, f := range []string{"ciphertext", "keyVersion", "algo"} {
		if _, ok := doc[f]; !ok {
			t.Errorf("密文形态缺少字段 %s", f)
		}
	}
	if len(doc) != 3 {
		t.Errorf("密文形态应恰好 3 字段，实际 %d", len(doc))
	}
}
