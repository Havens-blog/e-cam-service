package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForAuditLogs 轮询等待异步审计写入落袋（中间件经 goroutine 写入）。
func waitForAuditLogs(t *testing.T, sink *captureSink, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.entries) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("audit logs not written within deadline: got %d want %d", len(sink.entries), n)
}

// captureSink 捕获中间件产出的 AuditEntry（领域无关，验证边界解耦后行为不变）。
type captureSink struct {
	entries []AuditEntry
}

func (s *captureSink) Write(_ context.Context, e AuditEntry) error {
	s.entries = append(s.entries, e)
	return nil
}

// TestInferOperationType_CertPaths cert 域操作类型标签（7.2：导入/删除/扫描/
// 配置面写入 ecam_audit_log 的 operation_type 可辨识）。
func TestInferOperationType_CertPaths(t *testing.T) {
	cases := []struct {
		path, method string
		want         string
	}{
		// cam 域既有口径不受影响
		{"/api/v1/cam/assets", http.MethodPost, "api_asset_create"},
		{"/api/v1/cam/assets/sync", http.MethodPost, "api_asset_sync"},
		{"/api/v1/cam/tasks", http.MethodDelete, "api_task_delete"},
		// cert 域：根路径导入/参数段归 cert，静态子资源段取段名
		{"/api/v1/certs", http.MethodPost, "api_cert_create"},
		{"/api/v1/certs/6590aabbccdd000000000001", http.MethodDelete, "api_cert_delete"},
		{"/api/v1/certs/6590aabbccdd000000000001/key", http.MethodPost, "api_cert_create"},
		{"/api/v1/certs/6590aabbccdd000000000001/scan", http.MethodPost, "api_cert_create"},
		{"/api/v1/certs/settings/exemptions", http.MethodPost, "api_settings_create"},
		{"/api/v1/certs/settings/exemptions/a.example.com", http.MethodDelete, "api_settings_delete"},
		{"/api/v1/certs/settings/test", http.MethodPost, "api_settings_create"},
		{"/api/v1/certs/settings/crds/6590aabbccdd000000000001", http.MethodDelete, "api_settings_delete"},
		{"/api/v1/certs/changes", http.MethodPost, "api_changes_create"},
		{"/api/v1/certs/changes/6590aabbccdd000000000001/cancel", http.MethodPost, "api_changes_create"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, inferOperationType(tc.path, tc.method), "%s %s", tc.method, tc.path)
	}
}

// TestAuditMiddleware_MultipartBodyOmitted multipart 请求体（证书/私钥上传）
// 不落审计日志（渗透式自查口径：日志无明文私钥），仅记录占位符。
func TestAuditMiddleware_MultipartBodyOmitted(t *testing.T) {
	sink := &captureSink{}
	mdl := NewAuditMiddleware(sink, elog.DefaultLogger)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(mdl.Build())
	engine.POST("/api/v1/certs", func(c *gin.Context) {
		// handler 侧仍可读取 body（multipart 边界在，文件可解析）
		_, err := io.Copy(io.Discard, c.Request.Body)
		assert.NoError(t, err)
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	body := "------boundary\r\nContent-Disposition: form-data; name=\"keyFile\"; filename=\"a.key\"\r\n\r\n-----BEGIN PRIVATE KEY-----\r\nsecretmaterial\r\n-----END PRIVATE KEY-----\r\n------boundary--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/certs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----boundary")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	waitForAuditLogs(t, sink, 1)
	log := sink.entries[0]
	assert.Equal(t, "api_cert_create", log.OperationType)
	assert.Equal(t, "[multipart/form-data body omitted]", log.RequestBody)
	assert.NotContains(t, log.RequestBody, "PRIVATE KEY")
	assert.Equal(t, "success", log.Result)
}

// TestAuditMiddleware_JSONBodySanitized JSON body 脱敏（既有行为回归：
// password/secret 类字段掩码后入审计）。
func TestAuditMiddleware_JSONBodySanitized(t *testing.T) {
	sink := &captureSink{}
	mdl := NewAuditMiddleware(sink, elog.DefaultLogger)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(mdl.Build())
	engine.PUT("/api/v1/certs/settings", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	payload, _ := json.Marshal(map[string]string{"webhookUrls": "https://hook.example.com/x", "password": "p@ss"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/certs/settings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	waitForAuditLogs(t, sink, 1)
	log := sink.entries[0]
	assert.Contains(t, log.RequestBody, "***")
	assert.NotContains(t, log.RequestBody, "p@ss")
	assert.Equal(t, "api_settings_update", log.OperationType)
}
