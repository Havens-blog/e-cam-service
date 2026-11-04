package executor

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsRegionUnsupported 覆盖「地域不支持该产品」的识别。
//
// 这条判定决定日志级别：识别出来降级为 WARN（能力边界），否则按 ERROR（真故障）。
// 腾讯云实际返回形如：
// [TencentCloudSDKError] Code=UnsupportedRegion, Message=该接口不支持此地域访问。, RequestId=...
func TestIsRegionUnsupported(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil 不算", err: nil, want: false},
		{
			name: "腾讯云原始措辞",
			err: errors.New("[TencentCloudSDKError] Code=UnsupportedRegion, " +
				"Message=该接口不支持此地域访问。, RequestId=3b91b6d6"),
			want: true,
		},
		{name: "仅错误码", err: errors.New("Code=UnsupportedRegion"), want: true},
		{name: "中文措辞(此地域)", err: errors.New("该接口不支持此地域访问"), want: true},
		{name: "中文措辞(该地域)", err: errors.New("该地域不支持该产品"), want: true},
		{
			name: "包装后仍能识别",
			err:  fmt.Errorf("获取MongoDB实例失败: %w", errors.New("Code=UnsupportedRegion")),
			want: true,
		},
		{name: "普通故障不算", err: errors.New("connection refused"), want: false},
		{name: "限流不算", err: errors.New("RequestLimitExceeded"), want: false},
		{name: "空错误不算", err: errors.New(""), want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isRegionUnsupported(tc.err))
		})
	}
}

// TestLogSyncFailure_BothBranches 确认两个分支都不会 panic。
// 日志级别本身不在此断言（那属于 elog 实现细节）；这里锁定的是「分类后能安全落地」，
// 避免后续改动在这个高频路径上引入 panic。
func TestLogSyncFailure_BothBranches(t *testing.T) {
	e := &SyncAssetsExecutor{logger: testLogger()}

	regionUnsupported := errors.New("[TencentCloudSDKError] Code=UnsupportedRegion, Message=该接口不支持此地域访问。")
	realFailure := errors.New("connection refused")

	assert.NotPanics(t, func() { e.logSyncFailure("同步MongoDB失败", "ap-osaka", regionUnsupported) })
	assert.NotPanics(t, func() { e.logSyncFailure("同步Elasticsearch失败", "na-queretaro", realFailure) })
}
