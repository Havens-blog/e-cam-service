package ioc

import (
	"github.com/gotomicro/ego/task/ecron"
	"github.com/spf13/viper"
)

func InitJobs() []*ecron.Component {
	type Config struct {
		Enabled bool `mapstructure:"enabled"`
	}
	var cfg Config

	err := viper.UnmarshalKey("cronjob", &cfg)
	if err != nil {
		panic(err)
	}

	// 如果cronjob未启用，返回空列表
	if !cfg.Enabled {
		return []*ecron.Component{}
	}

	// 返回空的jobs列表，稍后可以添加具体的定时任务
	// 这里可以根据需要添加具体的cron job组件
	return []*ecron.Component{}
}
