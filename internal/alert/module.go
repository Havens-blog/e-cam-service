// Package alert 告警通知模块
package alert

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/detector"
	"github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/alert/service"
	"github.com/Havens-blog/e-cam-service/internal/alert/web"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// Module 告警通知模块
type Module struct {
	AlertService *service.AlertService
	Detector     *detector.ChangeDetector
	AlertHandler *web.AlertHandler
	Logger       *elog.Component
	stopCh       chan struct{}
}

// InitModule 初始化告警模块
func InitModule(db *mongox.Mongo, logger *elog.Component) (*Module, error) {
	// 初始化 DAO
	alertDAO := dao.NewAlertDAO(db)

	// 初始化索引
	if err := alertDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("初始化告警索引失败", elog.FieldErr(err))
		// 不阻塞启动
	}

	// 初始化服务
	alertService := service.NewAlertService(alertDAO, logger)

	// 初始化检测器
	changeDetector := detector.NewChangeDetector(alertService, logger)

	// 初始化 Handler
	alertHandler := web.NewAlertHandler(alertService, logger)

	return &Module{
		AlertService: alertService,
		Detector:     changeDetector,
		AlertHandler: alertHandler,
		Logger:       logger,
		stopCh:       make(chan struct{}),
	}, nil
}

// RegisterRoutes 注册告警模块路由
func (m *Module) RegisterRoutes(r *gin.Engine) {
	alertGroup := r.Group("/api/v1/cam")
	alertGroup.Use(middleware.TenantMiddleware(m.Logger))
	alertGroup.Use(middleware.RequireTenant(m.Logger))

	m.AlertHandler.RegisterRoutes(alertGroup)
}

// StartEventProcessor 启动告警事件处理协程
func (m *Module) StartEventProcessor(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		m.Logger.Info("告警事件处理器已启动", elog.Duration("interval", interval))

		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := m.AlertService.ProcessPendingEvents(ctx); err != nil {
					m.Logger.Error("处理告警事件失败", elog.FieldErr(err))
				}
				cancel()
			case <-m.stopCh:
				m.Logger.Info("告警事件处理器已停止")
				return
			}
		}
	}()
}

// Stop 停止告警模块
func (m *Module) Stop() {
	close(m.stopCh)
}
