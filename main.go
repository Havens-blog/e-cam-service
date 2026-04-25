// E-CAM Service API
// @title E-CAM Service API
// @version 1.0
// @description 企业云资产管理服务 API 文档
// @termsOfService http://swagger.io/terms/
// @contact.name API Support
// @contact.email support@example.com
// @license.name MIT
// @license.url https://opensource.org/licenses/MIT
// @host localhost:8001
// @BasePath /api/v1
// @schemes http https
package main

import (
	"fmt"

	"github.com/Havens-blog/e-cam-service/cmd"
	"github.com/fatih/color"
	git "github.com/purpleclay/gitz"
)

var (
	version string
)

func main() {
	ver := version
	if version == "" {
		ver = latestTag()
	}

	fmt.Println()
	fmt.Println("  _____        ____    _    __  __       ____  _____ ______     _____ ____ _____")
	fmt.Println(" | ____|      / ___|  / \\  |  \\/  |     / ___|| ____|  _ \\ \\   / /_ _/ ___| ____|")
	fmt.Println(" |  _| _____ | |     / _ \\ | |\\/| |_____\\___ \\|  _| | |_) \\ \\ / / | | |   |  _|")
	fmt.Println(" | |__|_____|| |___ / ___ \\| |  | |_____|___) | |___|  _ < \\ V /  | | |___| |___")
	fmt.Println(" |_____|      \\____/_/   \\_\\_|  |_|     |____/|_____|_| \\_\\ \\_/  |___\\____|_____|")
	fmt.Println()

	// 使用颜色来突出显示
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf(" %s: %s\n", cyan("Service Version"), green(ver))

	cmd.Execute(ver)
}

func latestTag() string {
	gc, err := git.NewClient()
	if err != nil {
		return ""
	}

	tags, _ := gc.Tags(
		git.WithShellGlob("*.*.*"),
		git.WithSortBy(git.CreatorDateDesc, git.VersionDesc),
		git.WithCount(1),
	)

	if len(tags) == 0 {
		return ""
	}

	return tags[0]
}
