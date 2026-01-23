package cam

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
)

// 导出类型别名，方便外部使用
type (
	Service             = service.Service
	CloudAccountService = service.CloudAccountService
	ModelService        = service.ModelService
	Handler             = web.Handler
)
