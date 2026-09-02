package timex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestParseExpiredTime(t *testing.T) {
	// 本地时区按测试机 Asia/Shanghai 假定仅在断言无时区格式时使用
	local := time.Local

	tests := []struct {
		name    string
		input   any
		wantOK  bool
		wantAt  time.Time
	}{
		{
			name:   "RFC3339 带时区",
			input:  "2026-09-15T23:59:59+08:00",
			wantOK: true,
			wantAt: time.Date(2026, 9, 15, 23, 59, 59, 0, time.FixedZone("", 8*3600)),
		},
		{
			name:   "RFC3339 UTC Z",
			input:  "2026-09-21T10:44:33Z",
			wantOK: true,
			wantAt: time.Date(2026, 9, 21, 10, 44, 33, 0, time.UTC),
		},
		{
			name:   "分钟精度 UTC（阿里云格式）",
			input:  "2026-09-20T16:00Z",
			wantOK: true,
			wantAt: time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC),
		},
		{
			name:   "无时区字符串按本地时区",
			input:  "2026-09-30 23:59:59",
			wantOK: true,
			wantAt: time.Date(2026, 9, 30, 23, 59, 59, 0, local),
		},
		{
			name:   "纯日期",
			input:  "2026-09-30",
			wantOK: true,
			wantAt: time.Date(2026, 9, 30, 0, 0, 0, 0, local),
		},
		{
			name:   "time.Time 直通",
			input:  time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			wantOK: true,
			wantAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "BSON date 直通",
			input:  primitive.NewDateTimeFromTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
			wantOK: true,
			wantAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "0001-01-01 零值字符串视为无到期",
			input:  "0001-01-01T00:00:00Z",
			wantOK: false,
		},
		{
			name:   "BSON date 零值视为无到期",
			input:  primitive.NewDateTimeFromTime(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)),
			wantOK: false,
		},
		{
			name:   "空串",
			input:  "",
			wantOK: false,
		},
		{
			name:   "纯空白",
			input:  "   ",
			wantOK: false,
		},
		{
			name:   "非法格式",
			input:  "next month",
			wantOK: false,
		},
		{
			name:   "不支持的类型",
			input:  12345,
			wantOK: false,
		},
		{
			name:   "nil",
			input:  nil,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseExpiredTime(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.True(t, tt.wantAt.Equal(got), "want %v, got %v", tt.wantAt, got)
			}
		})
	}
}
