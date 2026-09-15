package huawei

import (
	"testing"
)

// ShowDomainStats result {域名:{stat_type:[逐日数组]}} 的解析与聚合
func TestLookupAndAggregateDailySeries(t *testing.T) {
	result := map[string]interface{}{
		"www.example.com": map[string]interface{}{
			"flux": []interface{}{100.0, "250", "-"}, // 数字/字符串/无数据
		},
	}
	domainData, ok := lookupDomainResult(result, "www.example.com")
	if !ok {
		t.Fatal("domain lookup failed")
	}
	series, ok := lookupSeries(domainData, "flux")
	if !ok || len(series) != 3 {
		t.Fatalf("series = %v, %v", series, ok)
	}
	got := aggregateDailySeries(series, "2026-09-13", aggregateModeSum)
	if got["2026-09-13"] != 100 || got["2026-09-14"] != 250 || got["2026-09-15"] != 0 {
		t.Fatalf("aggregated = %v", got)
	}
}

func TestLookupDomainResultCaseInsensitive(t *testing.T) {
	result := map[string]interface{}{
		"WWW.Example.com": map[string]interface{}{"bw": []interface{}{5.0}},
	}
	if _, ok := lookupDomainResult(result, "www.example.com"); !ok {
		t.Fatal("case-insensitive lookup failed")
	}
}

func TestAggregateDailySeriesMaxAndHit(t *testing.T) {
	series := []float64{500, 3000, 250}
	maxed := aggregateDailySeries(series, "2026-09-14", aggregateModeMax)
	if maxed["2026-09-16"] != 250 {
		t.Fatalf("max = %v", maxed)
	}
	// 命中率百分制归一
	hit := aggregateDailySeries([]float64{97.35, 100, 0.9}, "2026-09-14", aggregateModeHit)
	if diff := hit["2026-09-14"] - 0.9735; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("hit = %v", hit["2026-09-14"])
	}
	if hit["2026-09-15"] != 1 {
		t.Fatalf("hit = %v", hit["2026-09-15"])
	}
}

func TestNormalizeHitRateValueHuawei(t *testing.T) {
	if v := normalizeHitRateValue(97.35); v < 0.9734 || v > 0.9736 {
		t.Fatalf("hit = %v", v)
	}
	if v := normalizeHitRateValue(0.9); v != 0.9 {
		t.Fatalf("hit = %v", v)
	}
	if v := normalizeHitRateValue(-1); v != 0 {
		t.Fatalf("hit = %v", v)
	}
}

func TestMetricDateRangeHuawei(t *testing.T) {
	dates, err := metricDateRange("2026-09-13", "2026-09-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(dates) != 3 {
		t.Fatalf("dates = %v", dates)
	}
	if _, err := metricDateRange("2026-01-01", "2026-09-15"); err == nil {
		t.Fatal("expected error for oversized range")
	}
}

func TestDayStartUnixMilli(t *testing.T) {
	if got := dayStartUnixMilli("2026-09-15"); got != 1789401600000 { // 09-15 00:00 CST
		t.Fatalf("dayStart = %d", got)
	}
}
