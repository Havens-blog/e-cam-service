//go:build wireinject

package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/cmdb"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/google/wire"
)

//go:generate go run github.com/google/wire/cmd/wire

var BaseSet = wire.NewSet(
	InitLogger,
	InitMongoDB,
	InitRedis,
	InitGrpcServer,
	InitSessionProvider,
	InitGinMiddlewares,
	InitEtcdClient,
	InitEcmdbPolicyClient,
	InitEcmdbEndpointClient,
	InitCheckPolicyMiddleware,
	InitWebServer,
	InitJobs,
	endpoint.InitModule,
	cam.InitModuleWithIAM,
	cmdb.InitModule,
	InitAlertModule,
	wire.FieldsOf(new(*endpoint.Module), "Hdl"),
	wire.FieldsOf(new(*cam.Module), "Hdl", "TaskHdl"),
)

func InitApp() (*App, error) {
	panic(wire.Build(
		BaseSet,
		wire.Struct(new(App), "*"),
	))
}
