package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
)

func InitEndpointService(db *mongox.Mongo) endpoint.Service {
	// 使用endpoint模块的wire依赖注入来初始化service
	module, err := endpoint.InitModule(db)
	if err != nil {
		panic(err)
	}
	return module.Svc
}
