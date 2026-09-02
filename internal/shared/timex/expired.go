// Package timex 时间解析工具。
//
// 云厂商 SDK 返回的到期时间格式不统一，历史落库数据混存多种格式
// （实测 ecam_instance.attributes.expired_time：RFC3339 带时区、
// RFC3339 UTC、分钟精度 UTC、无时区字符串、BSON date 各占一部分，
// 且存在 0001-01-01 零值垃圾）。所有读取到期时间的消费方
// （仪表盘「即将过期」、告警到期检测）必须经本包解析，禁止各自
// 用单一 layout 严格解析或对字符串做字节序比较。
package timex

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// expiredTimeLayouts 按实测数据频率排列，命中即返回。
var expiredTimeLayouts = []string{
	time.RFC3339,             // 2026-09-15T23:59:59+08:00 / 2026-09-21T10:44:33Z
	"2006-01-02T15:04Z07:00", // 2026-09-20T16:00Z（分钟精度，阿里云 UTC）
	"2006-01-02 15:04:05",    // 2026-09-30 23:59:59（无时区，按本地时区）
	"2006-01-02T15:04:05",    // 2026-09-30T23:59:59（无时区）
	"2006-01-02",             // 2026-09-30（纯日期）
}

// ParseExpiredTime 宽容解析云端到期时间。
// 接受 string（多格式）、time.Time、primitive.DateTime（BSON date 解码产物）。
// 空串、无法解析、以及 0001-01-01 一类零值（云端“无到期时间”被误落的值）
// 一律返回 false，调用方应视为无到期时间处理。
func ParseExpiredTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case string:
		return parseExpiredTimeString(t)
	case time.Time:
		return validExpire(t)
	case primitive.DateTime:
		return validExpire(t.Time())
	default:
		return time.Time{}, false
	}
}

func parseExpiredTimeString(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range expiredTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return validExpire(t)
		}
	}
	return time.Time{}, false
}

// validExpire 过滤 0001-01-01 等零值：年份 <= 1 视为无到期时间。
func validExpire(t time.Time) (time.Time, bool) {
	if t.Year() <= 1 {
		return time.Time{}, false
	}
	return t, true
}
