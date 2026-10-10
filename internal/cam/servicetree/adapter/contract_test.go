package adapter

// CMDB 内部跨服务契约校验（消费方 e-cam-service 侧）。
//
// 本测试用 go/ast 解析本仓 remote_cmdb.go 的 instanceDTO/statsDTO/typeCountDTO
// 与各适配方法拼接的 HTTP 路径，比对 testdata/cmdb-internal-contract.json 快照。
// 生产方 e-cmdb-service 的 internal_api.go 以字节相同的快照副本做同样比对，两侧
// 共同锁住契约——任一侧实现漂移即本仓/对侧测试变红。
//
// 守护重点：DTO 字段集（JSON 解码静默忽略未知字段，是最隐蔽的契约裂缝）。
//
// 端点方法推断：本侧路由不在 RegisterRoutes，而嵌在每个方法的 getJSON/postJSON
// 调用里——getJSON ⇒ GET，postJSON ⇒ POST，首个字符串字面量为子路径（带 "?"
// 查询串的剥去 "?" 之后部分）。
//
// 边界（诚实声明）：单仓测试只保证“本仓实现 == 本仓快照副本”。跨仓强一致被
// 降维为“两份快照副本字节相同”这一个可一次性 diff review 的事实。彻底的跨仓
// 强制需要一个同时 checkout 两仓并 diff 两份副本的 CI job，超出单仓测试范围。
//
// 契约变更流程：以生产方 e-cmdb-service 快照为权威源，复制同一份字节到本仓
// testdata 副本，并同步改 remote_cmdb.go，使两仓测试同时转绿。

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const contractPath = "testdata/cmdb-internal-contract.json"

type contractEndpoint struct {
	Method  string `json:"method"`
	Subpath string `json:"subpath"`
}

type contractSnapshot struct {
	ContractVersion int                          `json:"contract_version"`
	Note            string                       `json:"note"`
	PathPrefix      string                       `json:"path_prefix"`
	Endpoints       []contractEndpoint           `json:"endpoints"`
	DTOs            map[string]map[string]string `json:"dtos"`
}

func TestCMDBInternalContract(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "remote_cmdb.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析 remote_cmdb.go 失败: %v", err)
	}

	gotDTOs := extractDTOs(file)
	gotEndpoints := extractRemoteEndpoints(file)

	want := loadContract(t)
	assertEndpoints(t, want.Endpoints, gotEndpoints)
	assertDTOs(t, want.DTOs, gotDTOs)
}

// extractDTOs 收集本文件 struct，按归一化名匹配契约 DTO 键，
// 返回 键 → (json-tag → 归一化类型)。
func extractDTOs(file *ast.File) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			key := normalizeType(ts.Name.Name)
			if key != "instance" && key != "stats" && key != "typecount" {
				continue
			}
			fields := map[string]string{}
			for _, f := range st.Fields.List {
				if f.Tag == nil || len(f.Names) == 0 {
					continue
				}
				tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`")).Get("json")
				name := strings.Split(tag, ",")[0]
				if name == "" || name == "-" {
					continue
				}
				fields[name] = normalizeType(renderType(f.Type))
			}
			out[key] = fields
		}
	}
	return out
}

// extractRemoteEndpoints 遍历所有方法体，收集 getJSON/postJSON 调用的
// (method, subpath)。subpath 取首个字符串字面量并剥去 "?" 查询串。
func extractRemoteEndpoints(file *ast.File) []contractEndpoint {
	var eps []contractEndpoint
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		var method string
		switch sel.Sel.Name {
		case "getJSON":
			method = "GET"
		case "postJSON":
			method = "POST"
		default:
			return true
		}
		// 第 2 个参数是路径表达式（internalPrefix + "/sub..." [+ q.Encode()]）。
		sub := firstStringLit(call.Args[1])
		if sub == "" {
			return true
		}
		if i := strings.IndexByte(sub, '?'); i >= 0 {
			sub = sub[:i]
		}
		eps = append(eps, contractEndpoint{Method: method, Subpath: sub})
		return true
	})
	return eps
}

// ---- 渲染/归一化/断言辅助（与生产方测试同形，跨 module 无法共享） ----

func renderType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + renderType(e.X)
	case *ast.ArrayType:
		return "[]" + renderType(e.Elt)
	case *ast.MapType:
		return "map[" + renderType(e.Key) + "]" + renderType(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.SelectorExpr:
		return renderType(e.X) + "." + e.Sel.Name
	default:
		return "?"
	}
}

// normalizeType 小写 + 去 "dto"，消解两仓 DTO 类型名大小写差异。
func normalizeType(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "dto", "")
}

// firstStringLit 返回表达式中源序第一个字符串字面量。
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

func assertEndpoints(t *testing.T, want, got []contractEndpoint) {
	t.Helper()
	norm := func(eps []contractEndpoint) []string {
		s := make([]string, 0, len(eps))
		for _, e := range eps {
			s = append(s, e.Method+" "+e.Subpath)
		}
		sort.Strings(s)
		return s
	}
	w, g := norm(want), norm(got)
	if !reflect.DeepEqual(w, g) {
		t.Errorf("端点集与契约不符:\n  契约: %v\n  实现: %v", w, g)
	}
}

func assertDTOs(t *testing.T, want, got map[string]map[string]string) {
	t.Helper()
	for key, wantFields := range want {
		gotFields, ok := got[key]
		if !ok {
			t.Errorf("DTO %q 在实现中缺失（契约要求）", key)
			continue
		}
		if !reflect.DeepEqual(wantFields, gotFields) {
			t.Errorf("DTO %q 字段集与契约不符:\n  契约: %v\n  实现: %v", key, wantFields, gotFields)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("DTO %q 在实现中存在但契约未声明（新增 DTO 需同步快照）", key)
		}
	}
}

func loadContract(t *testing.T) contractSnapshot {
	t.Helper()
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("读取契约快照失败: %v", err)
	}
	var c contractSnapshot
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("解析契约快照失败: %v", err)
	}
	return c
}
