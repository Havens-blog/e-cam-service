package ioc

import (
	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/logquery"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
)

// InitLogQueryModule 初始化多云统一日志查询功能域(Phase 1)。
//
// 依赖来源(既有装配复用,不新增 provider):db 与 cam/alert 同一 Mongo,
// 仅用于云账号仓储;日志本体联邦实时查询不落库(ADR D1)。
func InitLogQueryModule(db *mongox.Mongo) (*logquery.Module, error) {
	accounts := accountrepo.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(db))
	return logquery.InitLogQueryModule(accounts, elog.DefaultLogger)
}
