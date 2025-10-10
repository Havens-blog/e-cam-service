package cam

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/internal/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/internal/web"
)

// 导出类型别名，方便外部使用
type (
	Service             = service.Service
	CloudAccountService = service.CloudAccountService
	Handler             = web.Handler
)
