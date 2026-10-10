package web

// cert 对外 HTTP API 契约校验（cert 服务抽取 · HTTP 内部契约闸）。
//
// 用 go/ast 解析本包各 *_handler.go 的 RegisterRoutes，重建每个端点的
// (HTTP 方法 + 子路径 + 生效授权角色)，比对 testdata/cert-http-contract.json
// 快照。cert 抽为独立服务后其对外 API 面必须与此一致，否则网关路由/鉴权契约
// 漂移即本测试变红。
//
// 守护重点：
//   - 端点集：路径漂移会断网关路由；
//   - 授权角色：误删/弱化某端点的 RequireRoles 是安全降级（最隐蔽、后果最重），
//     本闸显式锁定每端点生效角色集。
//
// 不纳入 DTO：请求/响应体字段变动频繁、边际安全低；端点+角色是稳定高价值契约面。
//
// 授权重建规则：
//   - 顶层 /api/v1/certs 的 CertRoleMiddleware 不含 Role* 实参，不贡献角色；
//   - 路由级 RequireRoles(RoleX, ...) 贡献该调用内的角色；
//   - 子组 s := g.Group("/settings", RequireRoles(...)) 贡献前缀 + 组级角色，
//     其上注册的路由继承两者；
//   - 端点生效角色 = 组级角色 ∪ 路由级角色，去重排序。
//
// 变更流程：改路由后同步快照 JSON，使本测试转绿。

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const certContractPath = "testdata/cert-http-contract.json"

type certContractEndpoint struct {
	Method  string   `json:"method"`
	Subpath string   `json:"subpath"`
	Roles   []string `json:"roles"`
}

type certContractSnapshot struct {
	ContractVersion int                    `json:"contract_version"`
	Note            string                 `json:"note"`
	PathPrefix      string                 `json:"path_prefix"`
	Endpoints       []certContractEndpoint `json:"endpoints"`
}

var httpVerbs = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
}

func TestCertHTTPContract(t *testing.T) {
	files, err := filepath.Glob("*_handler.go")
	if err != nil {
		t.Fatalf("glob handler 文件失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("未找到任何 *_handler.go")
	}

	var got []certContractEndpoint
	for _, f := range files {
		eps, err := extractHandlerEndpoints(f)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", f, err)
		}
		got = append(got, eps...)
	}

	want := loadCertContract(t)
	assertCertEndpoints(t, want.Endpoints, got)
}

// groupInfo 记录子组变量的前缀与组级角色。
type groupInfo struct {
	prefix string
	roles  []string
}

// extractHandlerEndpoints 解析单个 handler 文件的 RegisterRoutes，收集端点。
func extractHandlerEndpoints(path string) ([]certContractEndpoint, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	var out []certContractEndpoint
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "RegisterRoutes" || fn.Body == nil {
			continue
		}
		// 第一遍：收集子组变量绑定（x := y.Group(prefix, RequireRoles(...))）。
		groups := map[string]groupInfo{}
		for _, stmt := range fn.Body.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				continue
			}
			lhs, ok := assign.Lhs[0].(*ast.Ident)
			if !ok {
				continue
			}
			call, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
				continue
			}
			groups[lhs.Name] = groupInfo{
				prefix: firstStringLit(call.Args[0]),
				roles:  rolesFromArgs(call.Args),
			}
		}
		// 第二遍：收集 HTTP 动词调用。
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !httpVerbs[sel.Sel.Name] || len(call.Args) == 0 {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			gi := groups[recv.Name] // 零值：基础组 g（无前缀、无组级角色）
			sub := gi.prefix + firstStringLit(call.Args[0])
			roles := append(append([]string(nil), gi.roles...), rolesFromArgs(call.Args)...)
			out = append(out, certContractEndpoint{
				Method:  sel.Sel.Name,
				Subpath: sub,
				Roles:   normalizeRoles(roles),
			})
			return true
		})
	}
	return out, nil
}

// rolesFromArgs 从调用实参中找 RequireRoles(...) 并收集其 Role* 标识实参名。
func rolesFromArgs(args []ast.Expr) []string {
	var roles []string
	for _, a := range args {
		call, ok := a.(*ast.CallExpr)
		if !ok {
			continue
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "RequireRoles" {
			continue
		}
		for _, ra := range call.Args {
			if rid, ok := ra.(*ast.Ident); ok && strings.HasPrefix(rid.Name, "Role") {
				roles = append(roles, rid.Name)
			}
		}
	}
	return roles
}

// normalizeRoles 去重 + 排序（空集返回 []string{}，与快照 [] 对齐）。
func normalizeRoles(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range in {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	if out == nil {
		return []string{}
	}
	return out
}

// firstStringLit 返回表达式中源序第一个字符串字面量（剥去引号）。
func firstStringLit(expr ast.Expr) string {
	var out string
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			out = strings.Trim(bl.Value, "`\"")
			found = true
			return false
		}
		return true
	})
	return out
}

func assertCertEndpoints(t *testing.T, want, got []certContractEndpoint) {
	t.Helper()
	key := func(e certContractEndpoint) string {
		return e.Method + " " + e.Subpath + " [" + strings.Join(e.Roles, ",") + "]"
	}
	norm := func(eps []certContractEndpoint) []string {
		s := make([]string, 0, len(eps))
		for _, e := range eps {
			s = append(s, key(e))
		}
		sort.Strings(s)
		return s
	}
	w, g := norm(want), norm(got)
	if reflect.DeepEqual(w, g) {
		return
	}
	// 精确报差：契约缺失 / 实现新增。
	wset := map[string]bool{}
	for _, s := range w {
		wset[s] = true
	}
	gset := map[string]bool{}
	for _, s := range g {
		gset[s] = true
	}
	for _, s := range w {
		if !gset[s] {
			t.Errorf("契约声明但实现缺失（或方法/路径/角色漂移）: %s", s)
		}
	}
	for _, s := range g {
		if !wset[s] {
			t.Errorf("实现存在但契约未声明（新增端点或角色变更需同步快照）: %s", s)
		}
	}
}

func loadCertContract(t *testing.T) certContractSnapshot {
	t.Helper()
	raw, err := os.ReadFile(certContractPath)
	if err != nil {
		t.Fatalf("读取 cert 契约快照失败: %v", err)
	}
	var c certContractSnapshot
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("解析 cert 契约快照失败: %v", err)
	}
	for i := range c.Endpoints {
		c.Endpoints[i].Roles = normalizeRoles(c.Endpoints[i].Roles)
	}
	return c
}
