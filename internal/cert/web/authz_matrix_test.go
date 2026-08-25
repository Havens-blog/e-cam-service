package web

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rollbackBody 回滚请求体（itemIds 必填非空才进 handler；门卫在其前）。
func rollbackBody() map[string]any {
	return map[string]any{"itemIds": []string{"6590aabbccdd000000000001"}}
}

// ---------------------------------------------------------------------
// 三角色×关键端点权限矩阵（任务 7.2 AC：角色不足 → 403 FORBIDDEN）。
//
// 矩阵口径（api-handbook Auth 列 + 任务 AC"运维主管/审计=审计+配置+变更
// 查看；只读查看者=dashboard+详情只读"）：
//   - 运维工程师：导入/台账/引用/变更全读写（settings 面除外）；
//   - 运维主管：台账读/看板/变更查看（含审计流水），写面 403；
//   - 审计：与主管同面（变更查看+审计流水+配置），写面 403；
//   - 只读查看者：仅看板（无门卫）+证书详情只读，其余 403。
// 允许角色断言"非 403"（业务层 4xx/404 由既有端点测试覆盖，此处只验门卫）。
// ---------------------------------------------------------------------

func TestCertDomain_RoleMatrix(t *testing.T) {
	anyID := "6590aabbccdd000000000001"
	seedFP := "cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33" // 64 位 hex 台账种子指纹
	endpoints := []struct {
		method, path string
		body         any
		allowed      map[Role]bool // 未列角色一律 403
	}{
		// 导入面（工程师）
		{http.MethodPost, "/api/v1/certs", map[string]string{},
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodPost, "/api/v1/certs/batch", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodGet, "/api/v1/certs/batch/" + anyID, nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodPost, "/api/v1/certs/" + anyID + "/key", map[string]string{},
			map[Role]bool{RoleOpsEngineer: true}},
		// 台账面（列表/统计=工程师+主管；详情=工程师+主管+查看者；删除=工程师）
		{http.MethodGet, "/api/v1/certs", nil,
			map[Role]bool{RoleOpsEngineer: true, RoleOpsSupervisor: true}},
		{http.MethodGet, "/api/v1/certs/stats", nil,
			map[Role]bool{RoleOpsEngineer: true, RoleOpsSupervisor: true}},
		{http.MethodGet, "/api/v1/certs/" + anyID, nil,
			map[Role]bool{RoleOpsEngineer: true, RoleOpsSupervisor: true, RoleViewer: true}},
		{http.MethodDelete, "/api/v1/certs/" + anyID, nil,
			map[Role]bool{RoleOpsEngineer: true}},
		// 引用面（工程师）
		{http.MethodGet, "/api/v1/certs/" + anyID + "/references", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodGet, "/api/v1/certs/reverse?domain=a.example.com", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodPost, "/api/v1/certs/" + anyID + "/scan", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		// 发现导入查询面（工程师；cert-cloud-discovery-import 任务 3 SC-8：
		// preview 与 snapshot-status 均限 OpsEngineer——导入类端点权限沿用）
		{http.MethodGet, "/api/v1/certs/discovery/preview", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodGet, "/api/v1/certs/discovery/snapshot-status", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		// 配置面（主管/审计）
		{http.MethodPut, "/api/v1/certs/settings", map[string]string{},
			map[Role]bool{RoleOpsSupervisor: true, RoleAuditor: true}},
		{http.MethodPost, "/api/v1/certs/settings/exemptions", map[string]string{"domain": "a.example.com"},
			map[Role]bool{RoleOpsSupervisor: true, RoleAuditor: true}},
		{http.MethodPost, "/api/v1/certs/settings/crds", map[string]string{},
			map[Role]bool{RoleOpsSupervisor: true, RoleAuditor: true}},
		// 变更面（查看=工程师/主管/审计；写=工程师）
		{http.MethodGet, "/api/v1/certs/changes", nil,
			map[Role]bool{RoleOpsEngineer: true, RoleOpsSupervisor: true, RoleAuditor: true}},
		{http.MethodPost, "/api/v1/certs/changes", map[string]string{"oldFingerprint": "f", "newCertId": "c"},
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodGet, "/api/v1/certs/changes/" + anyID, nil,
			map[Role]bool{RoleOpsEngineer: true, RoleOpsSupervisor: true, RoleAuditor: true}},
		{http.MethodPost, "/api/v1/certs/changes/" + anyID + "/confirm", map[string]string{},
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodPost, "/api/v1/certs/changes/" + anyID + "/execute", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodPost, "/api/v1/certs/changes/" + anyID + "/confirm-batch", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodPost, "/api/v1/certs/changes/" + anyID + "/cancel", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodPost, "/api/v1/certs/changes/" + anyID + "/rollback", rollbackBody(),
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodGet, "/api/v1/certs/changes/" + anyID + "/progress", nil,
			map[Role]bool{RoleOpsEngineer: true}},
		{http.MethodGet, "/api/v1/certs/changes/" + anyID + "/audit", nil,
			map[Role]bool{RoleOpsEngineer: true, RoleOpsSupervisor: true, RoleAuditor: true}},
	}

	roles := []Role{RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor, RoleViewer, ""} // ""=未设置（deny-by-default）
	for _, ep := range endpoints {
		for _, role := range roles {
			allowed := ep.allowed[role]
			t.Run(string(role)+"→"+ep.method+" "+ep.path, func(t *testing.T) {
				engine, d := newChangeRouter(t, role)
				// 种子最小数据：看板/台账等读端点在允许角色下走到业务层不 panic。
				d.seedCert(t, seedFP, []string{"a.example.com"}, domain.HostingStatusComplete)
				w := doJSON(t, engine, ep.method, ep.path, ep.body)
				if !allowed {
					require.Equal(t, http.StatusForbidden, w.Code, "%s %s role=%q: %s",
						ep.method, ep.path, role, w.Body.String())
					env := decode(t, w)
					require.NotNil(t, env.Error)
					assert.Equal(t, CodeForbidden, env.Error.Code)
					return
				}
				assert.NotEqual(t, http.StatusForbidden, w.Code,
					"%s %s role=%q should pass role guard: %s", ep.method, ep.path, role, w.Body.String())
			})
		}
	}
}

// TestCertDomain_DashboardOpen 全角色（含未设置角色模拟全局链放行路径）
// 可达看板（4.5 既定契约：看板不做角色门卫，认证由全局链承接）。
func TestCertDomain_DashboardOpen(t *testing.T) {
	for _, role := range []Role{RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor, RoleViewer, ""} {
		engine, _ := newChangeRouter(t, role)
		w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
		assert.Equal(t, http.StatusOK, w.Code, "role=%q: %s", role, w.Body.String())
	}
}
