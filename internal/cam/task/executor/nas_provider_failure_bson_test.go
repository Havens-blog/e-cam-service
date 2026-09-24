package executor

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// TestNASProviderFailure_BSONRoundTripPreservesSnakeCase 锁定后端/前端字段名契约：
// Result["failures"] 经 mongo-driver BSON 落库后仍须是 account_id/error_count/
// last_error 蛇形键。若 nasProviderFailure 缺 bson tag，驱动会按字段名小写编码，
// 产出 accountid/errorcount/lasterror，前端按 account_id 等键读取时取不到值，
// 使运营「采集异常」warn 态不可达（NAS/Disk/OSS/回填共用该结构，一处收口）。
func TestNASProviderFailure_BSONRoundTripPreservesSnakeCase(t *testing.T) {
	result := map[string]any{
		"failures": []nasProviderFailure{
			{
				Provider:   "huawei",
				AccountID:  13,
				ErrorCount: 2,
				LastError:  "context deadline exceeded",
			},
		},
	}

	raw, err := bson.Marshal(result)
	if err != nil {
		t.Fatalf("bson.Marshal: %v", err)
	}

	var decoded bson.M
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("bson.Unmarshal: %v", err)
	}

	failuresRaw, ok := decoded["failures"]
	if !ok {
		t.Fatal("decoded 缺少 failures 键")
	}
	failures, ok := failuresRaw.(bson.A)
	if !ok {
		t.Fatalf("failures 类型错误: %T", failuresRaw)
	}
	if len(failures) != 1 {
		t.Fatalf("failures 长度 = %d, want 1", len(failures))
	}

	item, ok := failures[0].(bson.M)
	if !ok {
		t.Fatalf("failure 明细类型错误: %T", failures[0])
	}

	for _, key := range []string{"provider", "account_id", "error_count", "last_error"} {
		if _, ok := item[key]; !ok {
			t.Errorf("failure 明细缺蛇形键 %q，实际 keys=%v（缺 bson tag 会被压成 accountid/errorcount/lasterror）", key, item)
		}
	}

	if got := item["account_id"]; got != int64(13) {
		t.Errorf("account_id = %v (%T), want int64(13)", got, got)
	}
	if got := item["error_count"]; got != int32(2) && got != int64(2) {
		t.Errorf("error_count = %v (%T), want 2", got, got)
	}
	if got := item["last_error"]; got != "context deadline exceeded" {
		t.Errorf("last_error = %v, want %q", got, "context deadline exceeded")
	}
}
