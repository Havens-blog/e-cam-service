package initial

import "github.com/spf13/cobra"

var (
	TagVersion string
)

var Cmd = &cobra.Command{
	Use:   "initial",
	Short: "初始化命令",
	Long:  "初始化相关命令",
}
