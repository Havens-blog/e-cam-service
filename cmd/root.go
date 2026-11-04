package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Havens-blog/e-cam-service/cmd/endpoint"
	"github.com/Havens-blog/e-cam-service/cmd/start"
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

	mergeLocalOverride()
}

// mergeLocalOverride 合并同目录的 <name>.local.yaml（如 config/prod.yaml → config/prod.local.yaml）。
//
// 仓库里 config/prod.yaml 只放**占位符**（提交安全），真实凭据（如 eiam client_secret）应放在
// *local.yaml 中；.gitignore 早已约定 `config/*.local.yaml`，但此前**没有任何代码读取它**，
// 导致真实值只能写进会被提交的 prod.yaml。此函数补上这一环，使 .gitignore 的约定真正生效。
//
// 用 MergeInConfig 而非 ReadInConfig：后者会**覆盖**已有配置，前者只覆盖 local 文件里出现的键。
func mergeLocalOverride() {
	ext := filepath.Ext(confFile)
	if ext == "" {
		ext = ".yaml"
	}
	localFile := strings.TrimSuffix(confFile, filepath.Ext(confFile)) + ".local" + ext
	if _, statErr := os.Stat(localFile); statErr != nil {
		return // 没有本地覆盖文件属正常情况
	}

	viper.SetConfigFile(localFile)
	if mergeErr := viper.MergeInConfig(); mergeErr != nil {
		panic(mergeErr)
	}
}

func Execute(version string) {
	// 初始化设置
	cobra.OnInitialize(initAll)
	rootCmd.AddCommand(start.Cmd)
	rootCmd.AddCommand(endpoint.Cmd)
	err := rootCmd.Execute()
	cobra.CheckErr(err)
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&confFile, "config-file", "f", "config/prod.yaml", "the service config from file")
}
