package types

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// OSSMetric 落库字段形状冻结(T2 AC-1):bucket_name/date/storage_size(GB)/object_count/qc_status
// 为落库语义字段;分层大小(Standard/IA/Archive/ColdArchive)与 utilization 均不落库
// (Hard Rule:分层大小留二期,utilization 读取时由 storage_size 派生,模型层不得携带)。
func TestOSSMetricBsonShape(t *testing.T) {
	raw, err := bson.Marshal(OSSMetric{
		BucketName: "jlc-prod-logs", Date: "2026-09-19",
		StorageSize: 1378.14, ObjectCount: 42,
		QcStatus: OSSMetricQcZeroException, AccountID: 1, Provider: "aliyun",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"bucket_name", "date", "storage_size", "object_count", "qc_status", "account_id", "provider"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("OSSMetric 缺少落库字段 %q, doc=%v", key, doc)
		}
	}
	for _, key := range []string{"standard_size", "ia_size", "archive_size", "cold_archive_size", "utilization"} {
		if _, ok := doc[key]; ok {
			t.Fatalf("OSSMetric 不得携带 %q 字段(Hard Rule:分层大小/utilization 不落库)", key)
		}
	}
	// storage_size 须为 float64(GB,二进制 GiB):数量级自检下界 1MB(=1/1024 GiB)
	// 与实测小数容量需要小数精度,不得收敛成整数
	if doc["storage_size"] != 1378.14 {
		t.Fatalf("storage_size = %v(%T), want float64 1378.14", doc["storage_size"], doc["storage_size"])
	}
	// object_count 须为 int64(对象数量整数语义)
	if doc["object_count"] != int64(42) {
		t.Fatalf("object_count = %v(%T), want int64 42", doc["object_count"], doc["object_count"])
	}
}

// qc_status 取值冻结:zero_exception 是读取侧闭环(T10 data_status 映射)与
// 前端异常渲染的契约字面量,漂移会让异常行被当正常零容量。
// 取值与 NAS/CDN 共用同一字面量(复用 NASMetricQc* 常量作单一来源)。
func TestOSSMetricQcStatusValues(t *testing.T) {
	if OSSMetricQcOK != "" {
		t.Fatalf("OSSMetricQcOK = %q, want 空串(正常数据零值)", OSSMetricQcOK)
	}
	if OSSMetricQcZeroException != "zero_exception" {
		t.Fatalf("OSSMetricQcZeroException = %q, want zero_exception", OSSMetricQcZeroException)
	}
}
