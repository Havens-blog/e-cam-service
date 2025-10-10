//go:build wireinject

package endpoint

import (
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/endpoint/internal/repository"
	"github.com/Havens-blog/e-cam-service/internal/endpoint/internal/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/endpoint/internal/service"
	"github.com/Havens-blog/e-cam-service/internal/endpoint/internal/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(
	web.NewHandler,
	service.NewService,
	repository.NewEndpointRepository,
)

func InitModule(db *mongox.Mongo) (*Module, error) {
	wire.Build(
		ProviderSet,
		InitEndpointDAO,
		wire.Struct(new(Module), "*"),
	)
	return new(Module), nil
}

var daoOnce = sync.Once{}

func InitCollectionOnce(db *mongox.Mongo) {
	daoOnce.Do(func() {
		err := dao.InitIndexes(db)
		if err != nil {
			panic(err)
		}
	})
}

func InitEndpointDAO(db *mongox.Mongo) dao.EndpointDAO {
	InitCollectionOnce(db)
	return dao.NewEndpointDAO(db)
}
