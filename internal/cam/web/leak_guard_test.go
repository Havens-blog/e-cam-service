package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// knownExceptions 记录"已知且有意保留"的站点（必须写明原因）。
// 想新增例外就要同时改这里 —— 让例外在评审里可见，而不是悄悄回归。
var knownExceptions = map[string]string{
	"internal/topology/web/handler.go": "ginx 包装器只能给 200/500；该分支需要区分校验(400)与服务端(500)，待 ginx 支持结果状态或将路由改为非包装处理器",
}

var leakPatterns = []*regexp.Regexp{
	// ErrorResultWithMsg(errs.XxxError, err.Error()) —— 把原始错误写进响应体
	regexp.MustCompile(`ErrorResultWithMsg\(\s*errs\.\w+\s*,\s*err\.Error\(\)\s*\)`),
	// ginx.Result{Code: N, Msg: err.Error()}
	regexp.MustCompile(`ginx\.Result\{[^}]*Msg:\s*err\.Error\(\)`),
}

// TestNoRawErrorInHTTPResponses 是本会话（2026-11-01 ~ 11-03）持续清理"把实现细节回给调用方"
// 这条线的**回归守卫**：全仓曾累计 300+ 处（template/dns/tag/dictionary/cost/task/topology/
// servicetree/cam-web 等模块），其中不少还伴随"包装器返回 nil ⇒ HTTP 200 + 错误码"的
// "失败冒充成功"。现已归零，凡新增即失败（除 knownExceptions 中已写明原因的站点）。
func TestNoRawErrorInHTTPResponses(t *testing.T) {
	root := filepath.Join("..", "..", "..") // internal/cam/web -> 仓库根
	internal := filepath.Join(root, "internal")
	if _, err := os.Stat(internal); err != nil {
		t.Fatalf("找不到 internal 目录（CWD 可能不是包目录）: %v", err)
	}

	var offenders []string
	err := filepath.WalkDir(internal, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, ok := knownExceptions[rel]; ok {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, re := range leakPatterns {
				if re.MatchString(line) {
					offenders = append(offenders, rel+":"+strconv.Itoa(i+1))
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("发现 %d 处把原始错误写进 HTTP 响应的站点（首 10 条）:\n  %s", len(offenders), strings.Join(limit(offenders), "\n  "))
	}
}

func limit(s []string) []string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}
