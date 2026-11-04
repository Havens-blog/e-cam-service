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
	localFile := localOverridePath(confFile)
	if _, statErr := os.Stat(localFile); statErr != nil {
		return // 没有本地覆盖文件属正常情况
	}

	viper.SetConfigFile(localFile)
	if mergeErr := viper.MergeInConfig(); mergeErr != nil {
		panic(mergeErr)
	}
}

// localOverridePath 由主配置路径推导本地覆盖文件路径：config/prod.yaml → config/prod.local.yaml。
//
// 无扩展名时按 .yaml 补全（否则会得到 "config/prod.local" 这种 viper 无法按格式解析的文件名）。
// 抽成纯函数是为了能直接用表驱动用例覆盖各种路径形态，不必启动整个配置引导流程。
func localOverridePath(conf string) string {
	ext := filepath.Ext(conf)
	if ext == "" {
		return conf + ".local.yaml"
	}
	return strings.TrimSuffix(conf, ext) + ".local" + ext
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
