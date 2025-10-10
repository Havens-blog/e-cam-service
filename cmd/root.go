package cmd

import (
	"github.com/Havens-blog/e-cam-service/cmd/start"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	confFile string
)

var rootCmd = &cobra.Command{
	Use:   "e-cas-sevice",
	Short: "多云统一管理平台",
	Long:  "多云统一管理平台",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func initAll() {
	// 初始化配置文件
	initViper()
}

func initViper() {
	// 直接指定文件路径
	viper.SetConfigFile(confFile)
	viper.WatchConfig()
	err := viper.ReadInConfig()

	if err != nil {
		panic(err)
	}
}

func Execute(version string) {
	// 初始化设置
	cobra.OnInitialize(initAll)
	rootCmd.AddCommand(start.Cmd)
	// TODO: 暂时注释掉有问题的命令，等修复import路径后再启用
	// rootCmd.AddCommand(endpoint.Cmd)
	err := rootCmd.Execute()
	cobra.CheckErr(err)
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&confFile, "config-file", "f", "config/prod.yaml", "the service config from file")
}
