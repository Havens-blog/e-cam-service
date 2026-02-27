package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/alert"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
)

// InitAlertModule 初始化告警模块
func InitAlertModule(db *mongox.Mongo) *alert.Module {
	logger := elog.DefaultLogger
	module, err := alert.InitModule(db, logger)
	if err != nil {
		logger.Error("初始化告警模块失败", elog.FieldErr(err))
		return nil
	}
	return module
}
