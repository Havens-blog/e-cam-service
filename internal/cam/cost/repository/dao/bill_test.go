package dao

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"
	"pgregory.net/rapid"
)

// genRawData generates a random map[string]interface{} with BSON-safe value types.
// We restrict to types that survive BSON round-trip without loss: string, int32, int64, float64, bool.
func genRawData(t *rapid.T) map[string]interface{} {
	size := rapid.IntRange(1, 10).Draw(t, "mapSize")
	m := make(map[string]interface{}, size)
	for i := 0; i < size; i++ {
		key := rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{0,19}`).Draw(t, "key")
		val := genBSONSafeValue(t)
		m[key] = val
	}
	return m
}

// genBSONSafeValue generates a value that survives BSON marshal/unmarshal round-trip.
func genBSONSafeValue(t *rapid.T) interface{} {
	// BSON decodes integers as int32 or int64, so we generate those directly.
	// Avoid nested maps to keep the test focused and deterministic.
	choice := rapid.IntRange(0, 4).Draw(t, "valueType")
	switch choice {
	case 0:
		return rapid.String().Draw(t, "stringVal")
	case 1:
		return rapid.Int32().Draw(t, "int32Val")
	case 2:
		return rapid.Int64().Draw(t, "int64Val")
	case 3:
		return rapid.Float64().Draw(t, "float64Val")
	case 4:
		return rapid.Bool().Draw(t, "boolVal")
	default:
		return rapid.String().Draw(t, "defaultVal")
	}
}

// genRawBillRecord generates a random valid RawBillRecord using rapid generators.
func genRawBillRecord(t *rapid.T) domain.RawBillRecord {
	providers := []string{"aliyun", "aws", "volcano", "huawei", "tencent"}
	return domain.RawBillRecord{
		ID:          rapid.Int64Range(1, 1<<53).Draw(t, "id"),
		AccountID:   rapid.Int64Range(1, 1<<53).Draw(t, "accountID"),
		Provider:    providers[rapid.IntRange(0, len(providers)-1).Draw(t, "providerIdx")],
		RawData:     genRawData(t),
		CollectID:   rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "collectID"),
		BillingDate: rapid.StringMatching(`20[2-3][0-9]-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])`).Draw(t, "billingDate"),
		CreateTime:  rapid.Int64Range(0, 1<<53).Draw(t, "createTime"),
	}
}

// Feature: multicloud-finops, Property 3: 原始账单数据存储往返
// For any 有效的原始账单数据，存储到 MongoDB 后再读取应产生等价的数据（RawData 字段完整保留）。
// Since we cannot connect to a real MongoDB in unit tests, we test the BSON serialization
// round-trip directly, which is the core mechanism MongoDB uses for storage and retrieval.
//
// **Validates: Requirements 2.3**
func TestProperty3_RawBillRecordBSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		original := genRawBillRecord(rt)

		// Marshal to BSON (simulates MongoDB storage)
		data, err := bson.Marshal(original)
		if err != nil {
			rt.Fatalf("BSON marshal failed: %v", err)
		}

		// Unmarshal from BSON (simulates MongoDB retrieval)
		var decoded domain.RawBillRecord
		err = bson.Unmarshal(data, &decoded)
		if err != nil {
			rt.Fatalf("BSON unmarshal failed: %v", err)
		}

		// Verify all scalar fields are preserved
		assert.Equal(t, original.ID, decoded.ID, "ID should be preserved")
		assert.Equal(t, original.AccountID, decoded.AccountID, "AccountID should be preserved")
		assert.Equal(t, original.Provider, decoded.Provider, "Provider should be preserved")
		assert.Equal(t, original.CollectID, decoded.CollectID, "CollectID should be preserved")
		assert.Equal(t, original.BillingDate, decoded.BillingDate, "BillingDate should be preserved")
		assert.Equal(t, original.CreateTime, decoded.CreateTime, "CreateTime should be preserved")

		// Verify RawData map is fully preserved (the core property)
		assert.Equal(t, len(original.RawData), len(decoded.RawData), "RawData map size should be preserved")
		for key, origVal := range original.RawData {
			decodedVal, exists := decoded.RawData[key]
			assert.True(t, exists, "RawData key %q should exist after round-trip", key)
			if exists {
				assertBSONValueEqual(t, key, origVal, decodedVal)
			}
		}
	})
}

// assertBSONValueEqual compares values accounting for BSON type coercions.
// BSON may decode int32 as int32 and int64 as int64, but Go's interface{}
// comparisons handle this correctly when types match.
func assertBSONValueEqual(t *testing.T, key string, original, decoded interface{}) {
	t.Helper()
	switch v := original.(type) {
	case float64:
		dv, ok := decoded.(float64)
		assert.True(t, ok, "RawData[%q] should be float64, got %T", key, decoded)
		if ok {
			assert.Equal(t, v, dv, "RawData[%q] float64 value should match", key)
		}
	case int32:
		dv, ok := decoded.(int32)
		assert.True(t, ok, "RawData[%q] should be int32, got %T", key, decoded)
		if ok {
			assert.Equal(t, v, dv, "RawData[%q] int32 value should match", key)
		}
	case int64:
		dv, ok := decoded.(int64)
		assert.True(t, ok, "RawData[%q] should be int64, got %T", key, decoded)
		if ok {
			assert.Equal(t, v, dv, "RawData[%q] int64 value should match", key)
		}
	case string:
		dv, ok := decoded.(string)
		assert.True(t, ok, "RawData[%q] should be string, got %T", key, decoded)
		if ok {
			assert.Equal(t, v, dv, "RawData[%q] string value should match", key)
		}
	case bool:
		dv, ok := decoded.(bool)
		assert.True(t, ok, "RawData[%q] should be bool, got %T", key, decoded)
		if ok {
			assert.Equal(t, v, dv, "RawData[%q] bool value should match", key)
		}
	default:
		assert.Equal(t, original, decoded, "RawData[%q] value should match", key)
	}
}
