// Package daocheck 用静态检查守住一类**只在运行期才炸**的 DAO 写法。
//
// 【背景】mongo-driver 对**多键排序**要求有序结构：`SetSort(bson.M{"a":1,"b":-1})` 会在运行时
// 报 `multi-key map passed in for ordered parameter sort`。这类错误**编译期完全看不出来**，
// 实测后果是「策略模板列表」接口 **100% 500**（2026-10-31，页面完全不可用）。
// 单键 `bson.M{"ctime": -1}` 驱动接受，故只禁止**多键**。
//
// 本测试扫描仓库内所有 .go 源文件，发现多键 `SetSort(bson.M{...})` 即失败，
// 并提示改用 `bson.D{{Key:..., Value:...}}`。
package daocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 匹配 SetSort(bson.M{ "k": v, "k2": v2 }) 这类**多键**写法（键的引号出现 ≥4 次）。
var multiKeySortRe = regexp.MustCompile(`SetSort\(bson\.M\{(?:"[^"]+"\s*:\s*[^,]+,)+[^}]*\}\)`)

func TestNoMultiKeyBsonMSort(t *testing.T) {
	root := moduleRoot(t)
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 忽略不可读项，不因此让用例红
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "node_modules" || base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// 路径来自 filepath.Walk(模块根)，不是外部输入；只为静态检查读源码。
		raw, readErr := os.ReadFile(path) //nolint:gosec // G304：path 由 Walk 在仓库根下枚举得到，非用户可控输入
		if readErr != nil {
			return nil
		}
		for _, m := range multiKeySortRe.FindAllString(string(raw), -1) {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel+"  →  "+m)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历源码失败: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("发现多键 bson.M 排序（运行期会报 multi-key map passed in for ordered parameter sort，请改用 bson.D）：\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// moduleRoot 向上找到含 go.mod 的目录（避免对测试执行目录做硬编码假设）。
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 12; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("未能从当前目录向上找到 go.mod")
	return ""
}
