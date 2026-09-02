package dao

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkInst(id int64, expired any) Instance {
	return Instance{
		ID:         id,
		AssetName:  "inst",
		Attributes: map[string]interface{}{"expired_time": expired},
	}
}

func TestSelectExpiring(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local)
	deadline := now.AddDate(0, 0, 30)

	candidates := []Instance{
		mkInst(1, "2026-09-15T23:59:59+08:00"), // RFC3339 +08:00，范围内 -> 命中
		mkInst(2, "2026-09-20T16:00Z"),         // 分钟精度 UTC -> 命中
		mkInst(3, "2026-09-30 23:59:59"),       // 无时区 -> 命中
		mkInst(4, "2026-09-01T00:00:00+08:00"), // 已过期（早于 now）-> 排除
		mkInst(5, "2027-01-01T00:00:00+08:00"), // 超出 30 天窗口 -> 排除
		mkInst(6, "0001-01-01T00:00:00Z"),      // 零值垃圾 -> 排除
		mkInst(7, ""),                          // 空串 -> 排除
		mkInst(8, nil),                         // nil -> 排除
		mkInst(9, 12345),                       // 非法类型 -> 排除
		mkInst(10, "2026-09-18T00:00:00+08:00"),// 第二个命中的，按时间排序应排在 1 之后
	}

	result := selectExpiring(candidates, now, deadline)

	require.Len(t, result, 4)
	// 到期时间升序：9-15 < 9-18 < 9-20(UTC=9-20T16:00Z) < 9-30
	assert.Equal(t, int64(1), result[0].ID)
	assert.Equal(t, int64(10), result[1].ID)
	assert.Equal(t, int64(2), result[2].ID)
	assert.Equal(t, int64(3), result[3].ID)
}

func TestSelectExpiring_EmptyCandidates(t *testing.T) {
	now := time.Now()
	result := selectExpiring(nil, now, now.AddDate(0, 0, 7))
	assert.Empty(t, result)
}
