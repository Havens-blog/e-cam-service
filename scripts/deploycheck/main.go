// Command deploycheck 守护 cert-server 的"独立可部署"不变量。
//
// cert 服务抽取后，cmd/cert-server 必须只链接 cert 域及其出站端口的本地实现，
// 绝不能把 cam/cmdb/alert 等重单体域拖进二进制——否则"独立部署"名存实亡。
// 本检查对 ./cmd/cert-server 跑 `go list -deps`，断言其可达的 internal/<domain>
// 全部落在白名单内。用白名单而非黑名单：将来新增的任何重域都会被自动拦住。
//
// 这是继 depcheck（R1-R4，内部依赖边界）、cert HTTP 契约闸（对外 API 面）之后
// 的第三道闸——前两道锁"域内部不互相纠缠""对外契约不漂移"，本道锁"抽取后的
// 二进制不把单体拖回来"。三者合起来才是完整的抽取就绪保障。
//
// 退出码：0 = 闭包干净；1 = 有越界域（打印越界域与引入它的包路径线索）。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const (
	target       = "./cmd/cert-server"
	modulePrefix = "github.com/Havens-blog/e-cam-service/internal/"
)

// allowedDomains 是 cert-server 允许触达的 internal 域闭包：
//   - cert    ：被抽取的域本身；
//   - account ：云账号凭证解析（出站端口 CloudAccountLister 的本地实现依赖）；
//   - asset   ：覆盖率分母（InstanceCounter 本地适配器依赖）；
//   - audit   ：变更审计落库（ChangeAuditStore 本地适配器依赖）；
//   - shared  ：中间件/基础设施叶子（鉴权中间件等）。
//
// cert 抽到独立仓后，account/asset/audit 的本地适配器替换为 HTTP client，
// 这三项会从闭包消失、白名单可进一步收紧到 {cert, shared}。
var allowedDomains = map[string]bool{
	"cert":    true,
	"account": true,
	"asset":   true,
	"audit":   true,
	"shared":  true,
}

func main() {
	cmd := exec.Command("go", "list", "-deps", target)
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deploycheck: 运行 go list -deps %s 失败: %v\n", target, err)
		if ee, ok := err.(*exec.ExitError); ok {
			os.Stderr.Write(ee.Stderr)
		}
		os.Exit(2)
	}

	// domain -> 一个引入它的具体包路径（用于报错线索）。
	offenders := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		if !strings.HasPrefix(pkg, modulePrefix) {
			continue
		}
		sub := strings.TrimPrefix(pkg, modulePrefix) // 形如 "cam/dns"
		domain := sub
		if i := strings.IndexByte(sub, '/'); i >= 0 {
			domain = sub[:i]
		}
		if domain == "" || allowedDomains[domain] {
			continue
		}
		if _, seen := offenders[domain]; !seen {
			offenders[domain] = pkg
		}
	}

	if len(offenders) == 0 {
		allowed := make([]string, 0, len(allowedDomains))
		for d := range allowedDomains {
			allowed = append(allowed, d)
		}
		sort.Strings(allowed)
		fmt.Printf("deploycheck: OK — cert-server 依赖闭包仅含 {%s}，未拖入任何重单体域\n",
			strings.Join(allowed, ", "))
		return
	}

	domains := make([]string, 0, len(offenders))
	for d := range offenders {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	fmt.Fprintf(os.Stderr, "deploycheck: cert-server 依赖闭包越界——拖入了 %d 个非白名单 internal 域:\n", len(domains))
	for _, d := range domains {
		fmt.Fprintf(os.Stderr, "  internal/%s  （例如经由 %s 引入）\n", d, offenders[d])
	}
	fmt.Fprintln(os.Stderr, "这会使 cert-server 把单体重域链接进二进制，破坏独立可部署性。")
	fmt.Fprintln(os.Stderr, "正确做法：该域的能力应经 cert 消费者接口 + cmd/cert-server 内的适配器获取，")
	fmt.Fprintln(os.Stderr, "绝不直接 import 该域；若确为新的合法本地依赖，评审后加入 allowedDomains。")
	os.Exit(1)
}
