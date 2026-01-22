package start

import (
	"fmt"

	"github.com/Havens-blog/e-cam-service/ioc"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/task/ecron"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var Cmd = &cobra.Command{
	Use:   "start",
	Short: "ecmdb API服务",
	Long:  "启动服务，对外暴露接口",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 初始化自定义日志格式 (可读时间 + 调用者信息)
		ioc.InitCustomLogger()

		logger := elog.DefaultLogger
		logger.Info("开始启动ECMDB服务")

		logger.Info("初始化应用程序组件")
		app, err := ioc.InitApp()
		if err != nil {
			logger.Error("应用程序初始化失败", elog.FieldErr(err))
			panic(fmt.Errorf("failed to initialize application: %w", err))
		}
		logger.Info("应用程序组件初始化完成")

		logger.Info("初始化定时任务")
		initCronjob(app.Jobs)

		type Config struct {
			Port string `mapstructure:"port"`
		}
		var cfg Config

		err = viper.UnmarshalKey("e-cam-service", &cfg)
		if err != nil {
			logger.Error("读取服务配置失败", elog.FieldErr(err))
			panic(fmt.Errorf("failed to read service config: %w", err))
		}

		logger.Info("启动HTTP服务器", elog.String("port", cfg.Port))
		go func() {
			if err = app.Web.Run(fmt.Sprintf(":%s", cfg.Port)); err != nil {
				logger.Error("HTTP服务器启动失败", elog.FieldErr(err))
				panic(fmt.Errorf("HTTP server failed to start: %w", err))
			}
		}()

		logger.Info("启动gRPC服务器")
		// gRPC 服务器启动（阻塞）
		if err = app.Grpc.Serve(); err != nil {
			logger.Error("gRPC服务器启动失败", elog.FieldErr(err))
			panic(fmt.Errorf("gRPC server failed to serve: %w", err))
		}
		return nil
	},
}

// 注册定时任务
func initCronjob(jobs []*ecron.Component) {
	logger := elog.DefaultLogger
	logger.Info("开始初始化定时任务")

	type Config struct {
		Enabled bool `mapstructure:"enabled"`
	}

	var cfg Config
	if err := viper.UnmarshalKey("cronjob", &cfg); err != nil {
		logger.Error("读取定时任务配置失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to read cronjob config: %w", err))
	}

	if !cfg.Enabled {
		logger.Info("定时任务功能已禁用")
		return
	}

	logger.Info("定时任务功能已启用", elog.Int("job_count", len(jobs)))
	for i, job := range jobs {
		logger.Info("启动定时任务", elog.Int("job_index", i))
		go job.Start()
	}
}
