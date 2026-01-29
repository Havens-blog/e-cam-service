package types

import "strings"

// 标准化的 ECS 实例状态 (全小写)
const (
	StatusRunning    = "running"    // 运行中
	StatusStopped    = "stopped"    // 已停止
	StatusStarting   = "starting"   // 启动中
	StatusStopping   = "stopping"   // 停止中
	StatusPending    = "pending"    // 创建中
	StatusTerminated = "terminated" // 已销毁
	StatusRebooting  = "rebooting"  // 重启中
	StatusDeleted    = "deleted"    // 已删除
	StatusError      = "error"      // 异常
	StatusUnknown    = "unknown"    // 未知
)

// 各云平台状态映射表
var statusMapping = map[string]string{
	// 阿里云 (首字母大写)
	"Running":  StatusRunning,
	"Stopped":  StatusStopped,
	"Starting": StatusStarting,
	"Stopping": StatusStopping,
	"Pending":  StatusPending,

	// AWS (全小写)
	"running":       StatusRunning,
	"stopped":       StatusStopped,
	"pending":       StatusPending,
	"stopping":      StatusStopping,
	"shutting-down": StatusStopping,
	"terminated":    StatusTerminated,

	// 华为云 (全大写)
	"ACTIVE":      StatusRunning,
	"SHUTOFF":     StatusStopped,
	"BUILD":       StatusPending,
	"REBOOT":      StatusRebooting,
	"HARD_REBOOT": StatusRebooting,
	"REBUILD":     StatusPending,
	"MIGRATING":   StatusRunning,
	"RESIZE":      StatusPending,
	"ERROR":       StatusError,
	"DELETED":     StatusDeleted,

	// 腾讯云 (全大写)
	"RUNNING":       StatusRunning,
	"STOPPED":       StatusStopped,
	"PENDING":       StatusPending,
	"REBOOTING":     StatusRebooting,
	"STARTING":      StatusStarting,
	"STOPPING":      StatusStopping,
	"EXPIRED":       StatusStopped,
	"TERMINATING":   StatusStopping,
	"SHUTDOWN":      StatusStopped,
	"LAUNCH_FAILED": StatusError,

	// 火山引擎 (全大写)
	// RUNNING, STOPPED 等已在上面定义
}

// NormalizeStatus 标准化实例状态
// 将各云平台的状态值统一转换为小写的标准值
func NormalizeStatus(status string) string {
	// 先尝试精确匹配
	if normalized, ok := statusMapping[status]; ok {
		return normalized
	}

	// 尝试大写匹配
	if normalized, ok := statusMapping[strings.ToUpper(status)]; ok {
		return normalized
	}

	// 尝试首字母大写匹配
	if len(status) > 0 {
		titleCase := strings.ToUpper(status[:1]) + strings.ToLower(status[1:])
		if normalized, ok := statusMapping[titleCase]; ok {
			return normalized
		}
	}

	// 兜底：转小写返回
	lower := strings.ToLower(status)
	if lower == "" {
		return StatusUnknown
	}
	return lower
}

// IsRunningStatus 判断是否为运行中状态
func IsRunningStatus(status string) bool {
	return NormalizeStatus(status) == StatusRunning
}

// IsStoppedStatus 判断是否为停止状态
func IsStoppedStatus(status string) bool {
	return NormalizeStatus(status) == StatusStopped
}
