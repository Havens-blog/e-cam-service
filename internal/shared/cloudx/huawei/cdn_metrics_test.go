package huawei

import (
	"testing"
)

func f64(v float64) *float64 { return &v }

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
	if series[0] == nil || *series[0] != 100 || series[1] == nil || *series[1] != 250 {
		t.Fatalf("series values = %v", series)
	}
	if series[2] != nil {
		t.Fatalf("no-data day ('-') should be nil, got %v", *series[2])
	}
	got := aggregateDailySeries(series, "2026-09-13", aggregateModeSum)
	if got["2026-09-13"] != 100 || got["2026-09-14"] != 250 {
		t.Fatalf("aggregated = %v", got)
	}
	// 无数据日不得占位 0
	if _, ok := got["2026-09-15"]; ok {
		t.Fatalf("no-data day must be absent, got %v", got["2026-09-15"])
	}
}

func TestLookupSeriesNoDataSentinels(t *testing.T) {
	result := map[string]interface{}{
		"d": map[string]interface{}{
			"hit_flux_rate": []interface{}{
				"-",    // 字符串无数据
				-1.0,   // 厂商哨兵 -1
				"-1",   // 字符串哨兵
				nil,    // JSON null
				"null", // 字符串 null
				97.35,  // 正常百分制
			},
		},
	}
	series, ok := lookupSeries(result["d"].(map[string]interface{}), "hit_flux_rate")
	if !ok || len(series) != 6 {
		t.Fatalf("series = %v, %v", series, ok)
	}
	for i := 0; i < 5; i++ {
		if series[i] != nil {
			t.Fatalf("index %d should be nil (no data), got %v", i, *series[i])
		}
	}
	if series[5] == nil || *series[5] != 97.35 {
		t.Fatalf("index 5 = %v", series[5])
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
	series := []*float64{f64(500), f64(3000), f64(250)}
	maxed := aggregateDailySeries(series, "2026-09-14", aggregateModeMax)
	if maxed["2026-09-16"] != 250 {
		t.Fatalf("max = %v", maxed)
	}
	// 命中率百分制归一
	hit := aggregateDailySeries([]*float64{f64(97.35), f64(100), f64(0.9)}, "2026-09-14", aggregateModeHit)
	if diff := hit["2026-09-14"] - 0.9735; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("hit = %v", hit["2026-09-14"])
	}
	if hit["2026-09-15"] != 1 {
		t.Fatalf("hit = %v", hit["2026-09-15"])
	}
	// 无数据日(nil)跳过,不得产出 0
	hitGap := aggregateDailySeries([]*float64{nil, f64(98)}, "2026-09-14", aggregateModeHit)
	if _, ok := hitGap["2026-09-14"]; ok {
		t.Fatalf("no-data hit day must be absent, got %v", hitGap["2026-09-14"])
	}
	if hitGap["2026-09-15"] != 0.98 {
		t.Fatalf("hit = %v", hitGap["2026-09-15"])
	}
}

// 回归:响应含 "-" / -1 / null 无数据日时,产出的 CDNMetric.HitRate
// 必须保持未知哨兵 -1,不得被归一伪造为 0%
func TestBuildDailyMetricsNoDataDaysKeepUnknownHitRate(t *testing.T) {
	hitSeries, ok := lookupSeries(map[string]interface{}{
		"hit_flux_rate": []interface{}{"-", -1.0, nil, "97.35"},
	}, "hit_flux_rate")
	if !ok {
		t.Fatal("lookupSeries failed")
	}
	hitRate := aggregateDailySeries(hitSeries, "2026-09-14", aggregateModeHit)

	metrics := buildDailyMetrics(
		"www.example.com",
		[]string{"2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17"},
		map[string]float64{}, // flux 全无
		map[string]float64{}, // bw 全无
		hitRate,
	)
	if len(metrics) != 4 {
		t.Fatalf("metrics len = %d", len(metrics))
	}
	for i, d := range []string{"2026-09-14", "2026-09-15", "2026-09-16"} {
		if metrics[i].Date != d {
			t.Fatalf("date = %s, want %s", metrics[i].Date, d)
		}
		if metrics[i].HitRate != unknownHitRate {
			t.Fatalf("%s HitRate = %v, want %v (unknown)", d, metrics[i].HitRate, unknownHitRate)
		}
	}
	if metrics[3].HitRate < 0.9734 || metrics[3].HitRate > 0.9736 {
		t.Fatalf("2026-09-17 HitRate = %v, want 0.9735", metrics[3].HitRate)
	}
}

func TestNormalizeHitRateValueHuawei(t *testing.T) {
	if v := normalizeHitRateValue(97.35); v < 0.9734 || v > 0.9736 {
		t.Fatalf("hit = %v", v)
	}
	if v := normalizeHitRateValue(0.9); v != 0.9 {
		t.Fatalf("hit = %v", v)
	}
	if v := normalizeHitRateValue(0); v != 0 {
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
