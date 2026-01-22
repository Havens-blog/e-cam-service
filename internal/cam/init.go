package cam

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/iam"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
)

// InitModuleWithIAM 初始化CAM模块（包含IAM）
func InitModuleWithIAM(db *mongox.Mongo) (*Module, error) {
	logger := elog.DefaultLogger
	logger.Info("开始初始化CAM模块（包含IAM）")

	// 先初始化基础模块
	module, err := InitModule(db)
	if err != nil {
		logger.Error("初始化CAM基础模块失败", elog.FieldErr(err))
		return nil, err
	}
	logger.Info("CAM基础模块初始化成功")

	// 初始化IAM模块
	logger.Info("开始初始化IAM模块")
	iamModule, err := iam.InitModule(db)
	if err != nil {
		logger.Error("初始化IAM模块失败", elog.FieldErr(err))
		return nil, err
	}
	logger.Info("IAM模块初始化成功")

	module.IAMModule = iamModule
	logger.Info("CAM模块（包含IAM）初始化完成")
	return module, nil
}
