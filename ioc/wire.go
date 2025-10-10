//go:build wireinject

package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/google/wire"
)

//go:generate go run github.com/google/wire/cmd/wire

var BaseSet = wire.NewSet(
	InitMongoDB,
	InitRedis,
	InitGrpcServer,
	InitSessionProvider,
	InitGinMiddlewares,
	InitWebServer,
	InitJobs,
	endpoint.InitModule,
	cam.InitModule,
	wire.FieldsOf(new(*endpoint.Module), "Hdl", "Svc"),
	wire.FieldsOf(new(*cam.Module), "Hdl", "Svc"),
)

func InitApp() (*App, error) {
	panic(wire.Build(
		BaseSet,
		wire.Struct(new(App), "*"),
	))
}
