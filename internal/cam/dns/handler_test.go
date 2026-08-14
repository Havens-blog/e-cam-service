package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDNSService 模拟 DNSService
type mockDNSService struct {
	listDomainsResult   []DNSDomainVO
	listDomainsTotal    int64
	listDomainsErr      error
	listRecordsResult   []DNSRecordVO
	listRecordsTotal    int64
	listRecordsErr      error
	searchRecordsResult []DNSRecordVO
	searchRecordsTotal  int64
	searchRecordsErr    error
	createRecordResult  *DNSRecordVO
	createRecordErr     error
	updateRecordResult  *DNSRecordVO
	updateRecordErr     error
	deleteRecordErr     error
	batchDeleteResult   *BatchDeleteResult
	batchDeleteErr      error
	statsResult         *DNSStats
	statsErr            error
}

func (m *mockDNSService) ListDomains(_ context.Context, _ int64, _ DomainFilter) ([]DNSDomainVO, int64, error) {
	return m.listDomainsResult, m.listDomainsTotal, m.listDomainsErr
}

func (m *mockDNSService) ListRecords(_ context.Context, _ int64, _ string, _ RecordFilter) ([]DNSRecordVO, int64, error) {
	return m.listRecordsResult, m.listRecordsTotal, m.listRecordsErr
}

func (m *mockDNSService) SearchRecords(_ context.Context, _ int64, _ string, _ int64) ([]DNSRecordVO, int64, error) {
	return m.searchRecordsResult, m.searchRecordsTotal, m.searchRecordsErr
}

func (m *mockDNSService) CreateRecord(_ context.Context, _ int64, _ string, _ CreateRecordReq) (*DNSRecordVO, error) {
	return m.createRecordResult, m.createRecordErr
}

func (m *mockDNSService) UpdateRecord(_ context.Context, _ int64, _ string, _ string, _ UpdateRecordReq) (*DNSRecordVO, error) {
	return m.updateRecordResult, m.updateRecordErr
}

func (m *mockDNSService) DeleteRecord(_ context.Context, _ int64, _ string, _ string) error {
	return m.deleteRecordErr
}

func (m *mockDNSService) BatchDeleteRecords(_ context.Context, _ int64, _ string, _ []string) (*BatchDeleteResult, error) {
	return m.batchDeleteResult, m.batchDeleteErr
}

func (m *mockDNSService) GetStats(_ context.Context, _ int64) (*DNSStats, error) {
	return m.statsResult, m.statsErr
}

var _ DNSService = (*mockDNSService)(nil)

// testTenantID 测试用租户 ID。
// 迁移前这些测试靠 X-Tenant-ID 请求头注入租户，而该通道已被关闭：
// 租户只能由认证中间件写入上下文，故测试改为直接写上下文。
const testTenantID int64 = 5

// setupRouter 创建测试路由。
// withTenant 为 true 时直接把租户写入上下文，模拟认证中间件的行为
// （不再经由客户端请求头，那条注入通道已删除）。
func setupRouter(svc DNSService, withTenant bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	group := r.Group("/api/v1/cam")
	if withTenant {
		group.Use(func(c *gin.Context) {
			c.Set(middleware.TenantIDKey, testTenantID)
			c.Next()
		})
	}

	handler := NewDNSHandler(svc)
	handler.RegisterRoutes(group)
	return r
}

func setupRouterWithRequireTenant(svc DNSService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	logger := elog.DefaultLogger
	if logger == nil {
		logger = elog.EgoLogger
	}

	// 故意不注入租户：本组路由用于验证 RequireTenant 在无租户时拒绝请求。
	group := r.Group("/api/v1/cam")
	group.Use(middleware.RequireTenant(logger))

	handler := NewDNSHandler(svc)
	handler.RegisterRoutes(group)
	return r
}

// ==================== Handler 单元测试 ====================

func TestHandler_ListDomains(t *testing.T) {
	svc := &mockDNSService{
		listDomainsResult: []DNSDomainVO{
			{DomainName: "example.com", Provider: "aliyun", AccountID: 1},
		},
		listDomainsTotal: 1,
	}
	r := setupRouter(svc, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/dns/domains?keyword=example&limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["total"])
}

func TestHandler_ListDomains_Error(t *testing.T) {
	svc := &mockDNSService{
		listDomainsErr: errors.New("cloud API failed"),
	}
	r := setupRouter(svc, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/dns/domains", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(500), resp["code"])
}

func TestHandler_ListRecords(t *testing.T) {
	svc := &mockDNSService{
		listRecordsResult: []DNSRecordVO{
			{RecordID: "r1", RR: "www", Type: "A", Value: "1.2.3.4"},
		},
		listRecordsTotal: 1,
	}
	r := setupRouter(svc, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/dns/domains/example.com/records?record_type=A", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp["code"])
}

func TestHandler_CreateRecord(t *testing.T) {
	svc := &mockDNSService{
		createRecordResult: &DNSRecordVO{
			RecordID: "r1", RR: "www", Type: "A", Value: "1.2.3.4",
		},
	}
	r := setupRouter(svc, true)

	body, _ := json.Marshal(CreateRecordReq{
		AccountID: 1, RR: "www", Type: "A", Value: "1.2.3.4", TTL: 600,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/dns/domains/example.com/records", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp["code"])
}

func TestHandler_CreateRecord_InvalidBody(t *testing.T) {
	svc := &mockDNSService{}
	r := setupRouter(svc, true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/dns/domains/example.com/records", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(400), resp["code"])
}

func TestHandler_CreateRecord_ValidationError(t *testing.T) {
	svc := &mockDNSService{
		createRecordErr: ErrDNSInvalidIPv4,
	}
	r := setupRouter(svc, true)

	body, _ := json.Marshal(CreateRecordReq{
		AccountID: 1, RR: "www", Type: "A", Value: "invalid", TTL: 600,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/dns/domains/example.com/records", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(400), resp["code"])
}

func TestHandler_UpdateRecord(t *testing.T) {
	svc := &mockDNSService{
		updateRecordResult: &DNSRecordVO{
			RecordID: "r1", RR: "www", Type: "A", Value: "5.6.7.8",
		},
	}
	r := setupRouter(svc, true)

	body, _ := json.Marshal(UpdateRecordReq{
		AccountID: 1, RR: "www", Type: "A", Value: "5.6.7.8", TTL: 600,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cam/dns/domains/example.com/records/r1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp["code"])
}

func TestHandler_DeleteRecord(t *testing.T) {
	svc := &mockDNSService{}
	r := setupRouter(svc, true)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cam/dns/domains/example.com/records/r1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp["code"])
}

func TestHandler_DeleteRecord_Error(t *testing.T) {
	svc := &mockDNSService{
		deleteRecordErr: errors.New("record not found"),
	}
	r := setupRouter(svc, true)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cam/dns/domains/example.com/records/r1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(500), resp["code"])
}

func TestHandler_BatchDeleteRecords(t *testing.T) {
	svc := &mockDNSService{
		batchDeleteResult: &BatchDeleteResult{
			Total: 2, SuccessCount: 2, FailedCount: 0, Failures: []FailureDetail{},
		},
	}
	r := setupRouter(svc, true)

	body, _ := json.Marshal(map[string]interface{}{
		"record_ids": []string{"r1", "r2"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/dns/domains/example.com/records/batch-delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp["code"])
}

func TestHandler_BatchDeleteRecords_EmptyIDs(t *testing.T) {
	svc := &mockDNSService{}
	r := setupRouter(svc, true)

	body, _ := json.Marshal(map[string]interface{}{
		"record_ids": []string{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/dns/domains/example.com/records/batch-delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(400), resp["code"])
}

func TestHandler_GetStats(t *testing.T) {
	svc := &mockDNSService{
		statsResult: &DNSStats{
			TotalDomains:      10,
			TotalRecords:      100,
			ProviderDistrib:   map[string]int64{"aliyun": 5, "aws": 5},
			RecordTypeDistrib: map[string]int64{"A": 50, "CNAME": 50},
		},
	}
	r := setupRouter(svc, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/dns/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp["code"])
}

func TestHandler_MissingTenantID(t *testing.T) {
	svc := &mockDNSService{}
	r := setupRouterWithRequireTenant(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/dns/domains", nil)
	// 上下文中没有租户 —— RequireTenant 直接拒绝。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 语义由「参数缺失」改为「未选定租户」，状态码相应由 400 改为 403。
	assert.Equal(t, http.StatusForbidden, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(403), resp["code"])
}
