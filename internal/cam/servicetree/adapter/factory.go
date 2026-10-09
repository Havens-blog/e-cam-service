package adapter

import (
	cmdbrepository "github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/port"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
)

// NewCMDBPort 按配置选择 CMDB 适配器实现（拆分开关）。
//
//   - 配置了 cmdb.remote_url  → RemoteCMDBAdapter，走 HTTP 调用独立的 e-cmdb-service；
//   - 未配置                  → LocalCMDBAdapter，进程内直接调用 cmdb repository。
//
// 这是 CAM / CMDB 从「单体进程内」平滑过渡到「独立微服务」的唯一切换点：
// servicetree 业务代码只认 port.CMDBPort 接口，切换零改动。
//
// 返回 port.CMDBPort 接口（而非具体类型），因此 wire 无需 wire.Bind。
func NewCMDBPort(cmdbRepo cmdbrepository.InstanceRepository) port.CMDBPort {
	remoteURL := viper.GetString("cmdb.remote_url")
	if remoteURL != "" {
		elog.DefaultLogger.Info("CMDB 适配器: 远程模式",
			elog.String("remote_url", remoteURL))
		return NewRemoteCMDBAdapter(remoteURL)
	}
	elog.DefaultLogger.Info("CMDB 适配器: 本地进程内模式")
	return NewLocalCMDBAdapter(cmdbRepo)
}
