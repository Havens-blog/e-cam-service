package normalizer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"pgregory.net/rapid"
)

// validServiceTypes is the set of valid unified service types.
var validServiceTypes = map[string]bool{
	domain.ServiceTypeCompute:    true,
	domain.ServiceTypeStorage:    true,
	domain.ServiceTypeNetwork:    true,
	domain.ServiceTypeDatabase:   true,
	domain.ServiceTypeMiddleware: true,
	domain.ServiceTypeOther:      true,
}

// validProviders is the list of valid cloud providers for bill generation.
var validProviders = []shareddomain.CloudProvider{
	shareddomain.CloudProviderAliyun,
	shareddomain.CloudProviderAWS,
	shareddomain.CloudProviderVolcano,
	shareddomain.CloudProviderHuawei,
	shareddomain.CloudProviderTencent,
}

// genProvider generates a random valid cloud provider.
func genProvider(t *rapid.T) shareddomain.CloudProvider {
	return validProviders[rapid.IntRange(0, len(validProviders)-1).Draw(t, "providerIdx")]
}

// genBillingCycle generates a valid "YYYY-MM" billing cycle string.
func genBillingCycle(t *rapid.T) string {
	year := rapid.IntRange(2020, 2030).Draw(t, "year")
	month := rapid.IntRange(1, 12).Draw(t, "month")
	return fmt.Sprintf("%04d-%02d", year, month)
}

// genRawBillItem generates a random valid RawBillItem for property testing.
func genRawBillItem(t *rapid.T) billing.RawBillItem {
	provider := genProvider(t)
	amount := rapid.Float64Range(0, 1_000_000).Draw(t, "amount")
	currency := rapid.StringMatching(`[A-Z]{3}`).Draw(t, "currency")
	resourceID := rapid.StringMatching(`[a-z0-9\-]{1,32}`).Draw(t, "resourceID")
	serviceType := rapid.StringMatching(`[a-zA-Z0-9_\-\.]{0,64}`).Draw(t, "serviceType")
	billingCycle := genBillingCycle(t)

	// Generate random tags
	numTags := rapid.IntRange(0, 5).Draw(t, "numTags")
	tags := make(map[string]string, numTags)
	for i := 0; i < numTags; i++ {
		key := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, fmt.Sprintf("tagKey%d", i))
		val := rapid.StringMatching(`[a-z0-9]{1,10}`).Draw(t, fmt.Sprintf("tagVal%d", i))
		tags[key] = val
	}

	return billing.RawBillItem{
		Provider:     provider,
		ServiceType:  serviceType,
		ResourceID:   resourceID,
		ResourceName: rapid.String().Draw(t, "resourceName"),
		Region:       rapid.StringMatching(`[a-z\-]{1,20}`).Draw(t, "region"),
		Amount:       amount,
		Currency:     currency,
		BillingCycle: billingCycle,
		Tags:         tags,
	}
}

// TestProperty7_NormalizationCompleteness verifies that for any valid RawBillItem,
// the normalized UnifiedBill satisfies completeness constraints.
//
// **Validates: Requirements 3.1, 3.2**
func TestProperty7_NormalizationCompleteness(t *testing.T) {
	svc := NewNormalizerService(nil, elog.DefaultLogger)

	rapid.Check(t, func(t *rapid.T) {
		item := genRawBillItem(t)

		bill, err := svc.NormalizeOne(item)
		if err != nil {
			t.Fatalf("NormalizeOne returned unexpected error: %v", err)
		}

		// 1. Provider is non-empty
		if bill.Provider == "" {
			t.Fatal("Provider must be non-empty")
		}

		// 2. BillingStart < BillingEnd
		if !bill.BillingStart.Before(bill.BillingEnd) {
			t.Fatalf("BillingStart (%v) must be before BillingEnd (%v)",
				bill.BillingStart, bill.BillingEnd)
		}

		// 3. ServiceType is one of the valid set
		if !validServiceTypes[bill.ServiceType] {
			t.Fatalf("ServiceType %q is not in the valid set", bill.ServiceType)
		}

		// 4. Amount >= 0
		if bill.Amount < 0 {
			t.Fatalf("Amount must be >= 0, got %f", bill.Amount)
		}

		// 5. Currency is non-empty
		if bill.Currency == "" {
			t.Fatal("Currency must be non-empty")
		}
	})
}

// TestProperty8_CurrencyConversion verifies currency handling and exchange rate conversion.
//
// **Validates: Requirements 3.3, 3.4, 3.5, 3.6**
func TestProperty8_CurrencyConversion(t *testing.T) {
	// CNY providers: aliyun, huawei, tencent, volcano
	cnYProviders := []shareddomain.CloudProvider{
		shareddomain.CloudProviderAliyun,
		shareddomain.CloudProviderHuawei,
		shareddomain.CloudProviderTencent,
		shareddomain.CloudProviderVolcano,
	}

	t.Run("CNY_providers", func(t *testing.T) {
		svc := NewNormalizerService(nil, elog.DefaultLogger)

		rapid.Check(t, func(t *rapid.T) {
			provider := cnYProviders[rapid.IntRange(0, len(cnYProviders)-1).Draw(t, "providerIdx")]
			amount := rapid.Float64Range(0, 1_000_000).Draw(t, "amount")
			billingCycle := genBillingCycle(t)

			item := billing.RawBillItem{
				Provider:     provider,
				ServiceType:  "ecs",
				ResourceID:   "res-001",
				ResourceName: "test-resource",
				Region:       "cn-hangzhou",
				Amount:       amount,
				Currency:     "CNY",
				BillingCycle: billingCycle,
			}

			bill, err := svc.NormalizeOne(item)
			if err != nil {
				t.Fatalf("NormalizeOne returned unexpected error: %v", err)
			}

			if bill.Currency != CurrencyCNY {
				t.Fatalf("expected Currency %q for provider %q, got %q",
					CurrencyCNY, provider, bill.Currency)
			}

			if bill.AmountCNY != amount {
				t.Fatalf("expected AmountCNY == Amount (%f) for CNY provider %q, got %f",
					amount, provider, bill.AmountCNY)
			}
		})
	})

	t.Run("AWS_USD_conversion", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			amount := rapid.Float64Range(0, 1_000_000).Draw(t, "amount")
			rate := rapid.Float64Range(0.01, 100).Draw(t, "exchangeRate")
			billingCycle := genBillingCycle(t)

			svc := NewNormalizerServiceWithConfig(nil, elog.DefaultLogger, rate)

			item := billing.RawBillItem{
				Provider:     shareddomain.CloudProviderAWS,
				ServiceType:  "AmazonEC2",
				ResourceID:   "i-abc123",
				ResourceName: "test-instance",
				Region:       "us-east-1",
				Amount:       amount,
				Currency:     "USD",
				BillingCycle: billingCycle,
			}

			bill, err := svc.NormalizeOne(item)
			if err != nil {
				t.Fatalf("NormalizeOne returned unexpected error: %v", err)
			}

			if bill.Currency != CurrencyUSD {
				t.Fatalf("expected Currency %q for AWS, got %q",
					CurrencyUSD, bill.Currency)
			}

			expected := amount * rate
			if expected == 0 {
				if bill.AmountCNY != 0 {
					t.Fatalf("expected AmountCNY == 0 when amount is 0, got %f", bill.AmountCNY)
				}
				return
			}

			relErr := (bill.AmountCNY - expected) / expected
			if relErr < 0 {
				relErr = -relErr
			}
			if relErr > 1e-6 {
				t.Fatalf("AmountCNY relative error too large: got %f, expected %f (rate=%f, amount=%f, relErr=%e)",
					bill.AmountCNY, expected, rate, amount, relErr)
			}
		})
	})
}

// TestProperty9_ServiceTypeMapClosure verifies that for any valid cloud provider
// and any arbitrary raw service type string, the mapped unified service type is
// always one of {compute, storage, network, database, middleware, other} — never
// empty or undefined.
//
// **Validates: Requirements 3.7**
func TestProperty9_ServiceTypeMapClosure(t *testing.T) {
	mapper := NewServiceTypeMapper()

	rapid.Check(t, func(t *rapid.T) {
		provider := genProvider(t)
		rawServiceType := rapid.String().Draw(t, "rawServiceType")

		result := mapper.Map(provider, rawServiceType)

		// Result must never be empty
		if result == "" {
			t.Fatalf("Map(%q, %q) returned empty string", provider, rawServiceType)
		}

		// Result must be one of the valid unified service types
		if !validServiceTypes[result] {
			t.Fatalf("Map(%q, %q) returned %q which is not in the valid service type set",
				provider, rawServiceType, result)
		}
	})
}

// TestProperty10_UnifiedBillSerializationRoundTrip verifies that for any valid
// UnifiedBill, JSON serialization followed by deserialization produces an
// equivalent UnifiedBill with all field values identical.
//
// **Validates: Requirements 3.8**
func TestProperty10_UnifiedBillSerializationRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate valid service type keys for random selection
		serviceTypeKeys := []string{
			domain.ServiceTypeCompute,
			domain.ServiceTypeStorage,
			domain.ServiceTypeNetwork,
			domain.ServiceTypeDatabase,
			domain.ServiceTypeMiddleware,
			domain.ServiceTypeOther,
		}

		// Generate valid provider strings
		providerStrs := []string{"aliyun", "aws", "volcano", "huawei", "tencent"}

		// Generate BillingStart: random time truncated to second precision in UTC
		year := rapid.IntRange(2020, 2030).Draw(t, "year")
		month := rapid.IntRange(1, 12).Draw(t, "month")
		day := rapid.IntRange(1, 28).Draw(t, "day")
		hour := rapid.IntRange(0, 23).Draw(t, "hour")
		min := rapid.IntRange(0, 59).Draw(t, "min")
		sec := rapid.IntRange(0, 59).Draw(t, "sec")
		billingStart := time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC)

		// BillingEnd = BillingStart + random positive duration (1 hour to 31 days)
		durationSec := rapid.IntRange(3600, 31*24*3600).Draw(t, "durationSec")
		billingEnd := billingStart.Add(time.Duration(durationSec) * time.Second)

		// Generate Amount: non-negative finite float64
		amount := rapid.Float64Range(0, 1_000_000).Draw(t, "amount")
		amountCNY := rapid.Float64Range(0, 1_000_000).Draw(t, "amountCNY")

		// Generate Currency: 3-letter uppercase
		currency := rapid.StringMatching(`[A-Z]{3}`).Draw(t, "currency")

		// Generate Tags: random map or nil
		useNilTags := rapid.Bool().Draw(t, "useNilTags")
		var tags map[string]string
		if !useNilTags {
			numTags := rapid.IntRange(0, 5).Draw(t, "numTags")
			tags = make(map[string]string, numTags)
			for i := 0; i < numTags; i++ {
				key := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, fmt.Sprintf("tagKey%d", i))
				val := rapid.StringMatching(`[a-z0-9]{1,10}`).Draw(t, fmt.Sprintf("tagVal%d", i))
				tags[key] = val
			}
		}

		// Generate BillingDate in YYYY-MM-DD format
		bdYear := rapid.IntRange(2020, 2030).Draw(t, "bdYear")
		bdMonth := rapid.IntRange(1, 12).Draw(t, "bdMonth")
		bdDay := rapid.IntRange(1, 28).Draw(t, "bdDay")
		billingDate := fmt.Sprintf("%04d-%02d-%02d", bdYear, bdMonth, bdDay)

		original := domain.UnifiedBill{
			ID:              rapid.Int64().Draw(t, "id"),
			Provider:        providerStrs[rapid.IntRange(0, len(providerStrs)-1).Draw(t, "providerIdx")],
			AccountID:       rapid.Int64Min(1).Draw(t, "accountID"),
			AccountName:     rapid.String().Draw(t, "accountName"),
			BillingStart:    billingStart,
			BillingEnd:      billingEnd,
			ServiceType:     serviceTypeKeys[rapid.IntRange(0, len(serviceTypeKeys)-1).Draw(t, "serviceTypeIdx")],
			ServiceTypeName: rapid.String().Draw(t, "serviceTypeName"),
			ResourceID:      rapid.String().Draw(t, "resourceID"),
			ResourceName:    rapid.String().Draw(t, "resourceName"),
			Region:          rapid.String().Draw(t, "region"),
			Amount:          amount,
			Currency:        currency,
			AmountCNY:       amountCNY,
			ChargeType:      rapid.String().Draw(t, "chargeType"),
			Tags:            tags,
			TenantID:        rapid.String().Draw(t, "tenantID"),
			BillingDate:     billingDate,
			CreateTime:      rapid.Int64Min(0).Draw(t, "createTime"),
			UpdateTime:      rapid.Int64Min(0).Draw(t, "updateTime"),
		}

		// Step 1: JSON marshal
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		// Step 2: JSON unmarshal
		var deserialized domain.UnifiedBill
		err = json.Unmarshal(data, &deserialized)
		if err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		// Step 3: Compare all fields
		if !reflect.DeepEqual(original, deserialized) {
			t.Fatalf("round-trip mismatch:\noriginal:     %+v\ndeserialized: %+v", original, deserialized)
		}
	})
}
