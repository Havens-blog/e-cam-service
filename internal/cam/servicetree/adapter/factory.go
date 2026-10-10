package adapter

import (
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/port"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
)

// NewCMDBPort 创建 CMDB 端口实现（强制远程模式）。
//
// CMDB 已拆分为独立服务 e-cmdb-service，本地进程内适配器（LocalCMDBAdapter）
// 及其对 internal/cmdb 的依赖已移除。servicetree 通过 HTTP 调用 e-cmdb-service。
//
// 配置项 cmdb.remote_url 为必填（指向 e-cmdb-service 内网地址）；缺失时启动失败，
// 避免静默回退到已删除的进程内实现。
func NewCMDBPort() port.CMDBPort {
	remoteURL := viper.GetString("cmdb.remote_url")
	if remoteURL == "" {
		panic(fmt.Errorf("cmdb.remote_url 未配置：CMDB 已拆分为独立服务，必须配置其内网地址"))
	}
	elog.DefaultLogger.Info("CMDB 适配器: 远程模式",
		elog.String("remote_url", remoteURL))
	return NewRemoteCMDBAdapter(remoteURL)
}
