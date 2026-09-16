package scheduler

import (
	"testing"
	"time"
)

// 每日指标采集触发判定:首次触发/跨日触发/同日不重复/时区边界
func TestShouldTriggerDailyMetric(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)

	cases := []struct {
		name     string
		lastDate string
		now      time.Time
		want     bool
	}{
		{"从未触发,应触发", "", time.Date(2026, 9, 15, 10, 0, 0, 0, cst), true},
		{"同日不重复", "2026-09-15", time.Date(2026, 9, 15, 23, 0, 0, 0, cst), false},
		{"跨日重新触发", "2026-09-14", time.Date(2026, 9, 15, 0, 30, 0, 0, cst), true},
		// UTC 晚上 = CST 次日凌晨:服务器 UTC 20:00 已是北京次日 04:00
		{"UTC 晚上跨时区边界", "2026-09-14", time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC), true},
		// UTC 白天 = CST 同日(差 8 小时未跨日)
		{"UTC 白天同日", "2026-09-15", time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldTriggerDailyMetric(c.lastDate, c.now); got != c.want {
				t.Fatalf("shouldTriggerDailyMetric(%q, %v) = %v, want %v",
					c.lastDate, c.now, got, c.want)
			}
		})
	}
}
