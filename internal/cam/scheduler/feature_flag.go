// Package scheduler 特性开关:持久化日闸回滚开关(spec「特性开关与回滚」)。
//
// 持久化日闸以 SCHEDULER_PERSISTENT_GATE_ENABLED 控制,默认开启;若 mongo 日闸
// 出现不可恢复故障,一键切回 NAS/CDN 原内存闸(接受重启重复提交旧缺陷换取
// 调度器可用),回滚后记录原因与重新开启计划。回滚后调度任务仍正常提交、
// 指标采集不中断(回滚验证纳入 SC,Hard Rule:生产行为变更必须有退路)。
package scheduler

import (
	"os"
	"strings"
)

// EnvPersistentGateEnabled 持久化日闸特性开关环境变量名
const EnvPersistentGateEnabled = "SCHEDULER_PERSISTENT_GATE_ENABLED"

// IsPersistentGateEnabled 读取持久化日闸特性开关(默认开启)。
// 仅显式设置 false/0/off(大小写不敏感、容忍空白)时关闭;未设置或其他值
// 一律开启——避免误配置静默关闭持久化闸,让「重启重复提交」缺陷无声回归。
func IsPersistentGateEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvPersistentGateEnabled))) {
	case "false", "0", "off":
		return false
	default:
		return true
	}
}
