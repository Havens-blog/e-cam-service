package web

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRouter 构造挂载 /api/v1/certs 路由的测试引擎（内存假实现 + 测试主密钥）。
func newRouter(t *testing.T) (*gin.Engine, *certtest.FakeCertificateRepo, *certtest.FakeBatchSessionRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	certs := certtest.NewFakeCertificateRepo()
	batches := certtest.NewFakeBatchSessionRepo()
	svc := service.NewImportService(certs, batches, certtest.NewTestCrypto(t))
	refs := certtest.NewFakeCertReferenceRepo()
	snaps := certtest.NewFakeScanSnapshotRepo()
	ledgerSvc := service.NewLedgerService(certs, refs, snaps)
	querySvc := service.NewReferenceQueryService(certs, refs, snaps, &fakeScanTrigger{})
	dashH, settingsH := newDashboardSettingsHandlers(certs, refs, snaps)
	engine := gin.New()
	// 7.2 角色门卫全量接线后，导入面测试以运维工程师身份发起。
	engine.Use(withRole(RoleOpsEngineer))
	RegisterRoutes(engine, NewCertHandler(svc), NewReferenceHandler(querySvc),
		NewDiscoveryHandler(service.NewDiscoveryPreviewService(snaps, refs, certs, certtest.NewFakeCloudCertMappingRepo()), newDiscoveryImportSvcForRouter()),
		NewLedgerHandler(ledgerSvc), dashH, settingsH, newChangeHandlerFixture(t))
	return engine, certs, batches
}

// multipartBody 构造 multipart 请求体：files 逐项 (field, filename, content)。
func multipartBody(t *testing.T, fields map[string]string, files []filePart) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	for _, f := range files {
		w, err := mw.CreateFormFile(f.field, f.name)
		require.NoError(t, err)
		_, err = w.Write(f.content)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return body, mw.FormDataContentType()
}

type filePart struct {
	field, name string
	content     []byte
}

func doMultipart(t *testing.T, engine *gin.Engine, method, path string, fields map[string]string, files []filePart) *httptest.ResponseRecorder {
	t.Helper()
	body, ctype := multipartBody(t, fields, files)
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// envelope 统一信封解码（data/meta 保留原始 JSON 以便字段级断言）。
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *apiErrVO       `json:"error"`
	Meta    json.RawMessage `json:"meta"`
}

type apiErrVO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), "响应须为统一信封: %s", w.Body.String())
	return env
}

// assertNoKeyMaterial Hard Rule/AC6：任何响应不得含明文私钥与密文字段。
func assertNoKeyMaterial(t *testing.T, w *httptest.ResponseRecorder, keyPEMs ...[]byte) {
	t.Helper()
	body := w.Body.String()
	assert.NotContains(t, body, "PRIVATE KEY", "响应不得含明文私钥")
	assert.NotContains(t, body, "encryptedPrivateKey", "响应不得含密文字段")
	assert.NotContains(t, body, "ciphertext", "响应不得含密文字段")
	assert.NotContains(t, body, "certPem", "响应不得回显证书束")
	for i, k := range keyPEMs {
		if len(k) == 0 {
			continue
		}
		assert.NotContains(t, body, string(k), "响应不得含第 %d 把明文私钥", i)
	}
}

// pollBatchTerminal 轮询 GET /api/v1/certs/batch/:batchId 直至终态。
func pollBatchTerminal(t *testing.T, engine *gin.Engine, batchID string) (envelope, map[string]any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/certs/batch/"+batchID, nil)
		engine.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		env := decode(t, w)
		var data map[string]any
		require.NoError(t, json.Unmarshal(env.Data, &data))
		if data["status"] != string(domain.BatchSessionRunning) {
			return env, data
		}
		if time.Now().After(deadline) {
			t.Fatalf("batch %s 未在期限内到达终态: %v", batchID, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---- AC1：单张导入 ----

func TestImportCertAPIComplete(t *testing.T) {
	engine, certs, _ := newRouter(t)
	b := certtest.NewBundle(t, "www.example.com", []string{"www.example.com", "api.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil, []filePart{
		{"certFile", "www.crt", b.CertPEM},
		{"keyFile", "www.key", b.KeyPEM},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env := decode(t, w)
	require.True(t, env.Success)
	require.Nil(t, env.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.Equal(t, b.Fingerprint, data["fingerprint"])
	assert.Equal(t, string(domain.HostingStatusComplete), data["hostingStatus"])
	assert.NotEmpty(t, data["certId"])
	// Hard Rule：data 仅含三字段
	assert.Len(t, data, 3, "导入成功响应仅含 certId/fingerprint/hostingStatus: %v", data)
	assert.Empty(t, env.Meta, "无 expectedDomain 时无提示 meta")

	assertNoKeyMaterial(t, w, b.KeyPEM)

	stored, err := certs.GetByFingerprint(t.Context(), b.Fingerprint)
	require.NoError(t, err)
	assert.NotNil(t, stored.EncryptedPrivateKey, "私钥经 1.1 信封加密存储")
}

func TestImportCertAPIFingerprintOnly(t *testing.T) {
	engine, certs, _ := newRouter(t)
	b := certtest.NewBundle(t, "fp.example.com", []string{"fp.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil, []filePart{
		{"certFile", "fp.crt", b.CertPEM},
	})
	require.Equal(t, http.StatusOK, w.Code)
	env := decode(t, w)
	require.True(t, env.Success)

	var data map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.Equal(t, string(domain.HostingStatusFingerprintOnly), data["hostingStatus"])

	stored, err := certs.GetByFingerprint(t.Context(), b.Fingerprint)
	require.NoError(t, err)
	assert.Nil(t, stored.EncryptedPrivateKey)
}

func TestImportCertAPIExpectedDomainHint(t *testing.T) {
	engine, _, _ := newRouter(t)
	b := certtest.NewBundle(t, "adv.example.com", []string{"www.example.com"}, nil)

	// 不一致 → 200 + meta 提示（不拦截）
	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs",
		map[string]string{"expectedDomain": "blog.example.com"},
		[]filePart{{"certFile", "adv.crt", b.CertPEM}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env := decode(t, w)
	require.True(t, env.Success)
	require.NotNil(t, env.Meta, "expectedDomain 不一致时响应附提示字段")
	var meta struct {
		ExpectedDomainMissing []string `json:"expectedDomainMissing"`
	}
	require.NoError(t, json.Unmarshal(env.Meta, &meta))
	assert.Equal(t, []string{"blog.example.com"}, meta.ExpectedDomainMissing)

	// 命中 SAN → 无提示
	b2 := certtest.NewBundle(t, "adv2.example.com", []string{"www.example.com"}, nil)
	w = doMultipart(t, engine, http.MethodPost, "/api/v1/certs",
		map[string]string{"expectedDomain": "www.example.com"},
		[]filePart{{"certFile", "adv2.crt", b2.CertPEM}})
	require.Equal(t, http.StatusOK, w.Code)
	env = decode(t, w)
	require.True(t, env.Success)
	assert.Empty(t, env.Meta, "expectedDomain 命中时无提示")
}

// ---- AC2：校验失败映射 ----

func TestImportCertAPIErrors(t *testing.T) {
	engine, _, _ := newRouter(t)
	b := certtest.NewBundle(t, "val.example.com", []string{"val.example.com"}, nil)

	tests := []struct {
		name      string
		files     []filePart
		wantCode  string
		wantState int
	}{
		{
			name:      "key mismatch",
			files:     []filePart{{"certFile", "v.crt", b.CertPEM}, {"keyFile", "v.key", certtest.NewKeyPEM(t)}},
			wantCode:  domain.CodeCertKeyMismatch,
			wantState: http.StatusBadRequest,
		},
		{
			name:      "chain incomplete",
			files:     []filePart{{"certFile", "v.crt", b.LeafOnlyPEM()}},
			wantCode:  domain.CodeCertChainIncomplete,
			wantState: http.StatusBadRequest,
		},
		{
			name:      "parse fail garbage",
			files:     []filePart{{"certFile", "v.crt", []byte("garbage")}},
			wantCode:  domain.CodeCertParseFail,
			wantState: http.StatusBadRequest,
		},
		{
			name: "parse fail expired",
			files: []filePart{{"certFile", "v.crt", certtest.NewBundle(t, "exp.example.com",
				[]string{"exp.example.com"}, func(c *x509.Certificate) {
					c.NotBefore = time.Now().Add(-48 * time.Hour)
					c.NotAfter = time.Now().Add(-24 * time.Hour)
				}).CertPEM}},
			wantCode:  domain.CodeCertParseFail,
			wantState: http.StatusBadRequest,
		},
		{
			name:      "missing certFile",
			files:     []filePart{{"keyFile", "v.key", b.KeyPEM}},
			wantCode:  CodeInvalidRequest,
			wantState: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil, tt.files)
			assert.Equal(t, tt.wantState, w.Code, w.Body.String())
			env := decode(t, w)
			assert.False(t, env.Success)
			require.NotNil(t, env.Error)
			assert.Equal(t, tt.wantCode, env.Error.Code)
			assertNoKeyMaterial(t, w, b.KeyPEM)
		})
	}
}

// TestImportCertAPIDuplicateFingerprintMerge 重复指纹幂等导入：同指纹重导
// （仅指纹登记 → 补完整链+私钥）返回 200 + 既有 certId + hostingStatus=complete，
// 不再 409（变更向导要求新证书完整托管，重导升级条目后可直接选用）。
func TestImportCertAPIDuplicateFingerprintMerge(t *testing.T) {
	engine, _, _ := newRouter(t)
	b := certtest.NewBundle(t, "dup.example.com", []string{"dup.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil,
		[]filePart{{"certFile", "d.crt", b.CertPEM}})
	require.Equal(t, http.StatusOK, w.Code)
	first := decode(t, w)
	require.True(t, first.Success)

	// 重导：同指纹 + 私钥 → 200 幂等合并，同一 certId，托管状态升级
	w = doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil,
		[]filePart{{"certFile", "d.crt", b.CertPEM}, {"keyFile", "d.key", b.KeyPEM}})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env := decode(t, w)
	assert.True(t, env.Success)
	assert.Nil(t, env.Error)
	assertNoKeyMaterial(t, w, b.KeyPEM)
}

// ---- AC3/AC4：批量导入 + 进度轮询 ----

func TestBatchImportAPI(t *testing.T) {
	engine, certs, _ := newRouter(t)
	good1 := certtest.NewBundle(t, "b1.example.com", []string{"b1.example.com"}, nil)
	good2 := certtest.NewBundle(t, "b2.example.com", []string{"b2.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs/batch", nil, []filePart{
		{"certFiles", "a.crt", good1.CertPEM},
		{"keyFiles", "a.key", good1.KeyPEM},
		{"certFiles", "b.crt", []byte("garbage")},
		{"certFiles", "c.crt", good2.CertPEM},
	})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	env := decode(t, w)
	require.True(t, env.Success)

	var data map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &data))
	batchID, _ := data["batchId"].(string)
	require.NotEmpty(t, batchID, "batchId=cert_batch_sessions._id")
	assert.Equal(t, string(domain.BatchSessionRunning), data["status"])
	assert.Equal(t, float64(3), data["progress"].(map[string]any)["total"])
	files, _ := data["files"].([]any)
	require.Len(t, files, 3)
	assert.Equal(t, "pending", files[0].(map[string]any)["result"])
	assertNoKeyMaterial(t, w, good1.KeyPEM)

	// 轮询至终态：混合成败 → partial_failed
	_, terminal := pollBatchTerminal(t, engine, batchID)
	assert.Equal(t, string(domain.BatchSessionPartialFailed), terminal["status"])
	progress := terminal["progress"].(map[string]any)
	assert.Equal(t, float64(3), progress["total"])
	assert.Equal(t, float64(2), progress["done"])
	assert.Equal(t, float64(1), progress["failed"])

	tfiles := terminal["files"].([]any)
	f0 := f0v(tfiles[0])
	f1 := f0v(tfiles[1])
	f2 := f0v(tfiles[2])
	assert.Equal(t, "success", f0["result"])
	assert.NotEmpty(t, f0["certId"])
	assert.Equal(t, "failed", f1["result"])
	assert.Contains(t, f1["errorReason"], domain.CodeCertParseFail)
	assert.Equal(t, "success", f2["result"])
	// a.crt 的私钥已加密落库（同名 a.key 配对）
	stored, err := certs.GetByFingerprint(t.Context(), good1.Fingerprint)
	require.NoError(t, err)
	assert.NotNil(t, stored.EncryptedPrivateKey, "同名 .key 私钥应配对加密落库")
	// c.crt 无私钥 → fingerprint_only
	stored2, err := certs.GetByFingerprint(t.Context(), good2.Fingerprint)
	require.NoError(t, err)
	assert.Nil(t, stored2.EncryptedPrivateKey)

	// 失败文件可单独重试（重新 POST 单文件）
	fixed := certtest.NewBundle(t, "b1fixed.example.com", []string{"b1fixed.example.com"}, nil)
	w = doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil,
		[]filePart{{"certFile", "b.crt", fixed.CertPEM}})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestBatchImportAPIAllSuccess(t *testing.T) {
	engine, _, _ := newRouter(t)
	b1 := certtest.NewBundle(t, "ok1.example.com", []string{"ok1.example.com"}, nil)
	b2 := certtest.NewBundle(t, "ok2.example.com", []string{"ok2.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs/batch", nil, []filePart{
		{"certFiles", "a.crt", b1.CertPEM},
		{"certFiles", "b.crt", b2.CertPEM},
	})
	require.Equal(t, http.StatusAccepted, w.Code)
	var data map[string]any
	require.NoError(t, json.Unmarshal(decode(t, w).Data, &data))

	_, terminal := pollBatchTerminal(t, engine, data["batchId"].(string))
	assert.Equal(t, string(domain.BatchSessionCompleted), terminal["status"])
}

func TestBatchImportAPIValidation(t *testing.T) {
	engine, _, _ := newRouter(t)

	// 缺 certFiles → 400
	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs/batch", nil, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, CodeInvalidRequest, env.Error.Code)
}

func TestGetBatchAPIErrors(t *testing.T) {
	engine, _, _ := newRouter(t)

	// 未知 batchId → 404
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/certs/batch/000000000000000000000000", nil)
	engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	env := decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, CodeNotFound, env.Error.Code)

	// 非法 batchId → 400
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/certs/batch/not-hex", nil)
	engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	env = decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, CodeInvalidID, env.Error.Code)
}

// ---- AC5：补传私钥升级 ----

func TestUploadKeyAPI(t *testing.T) {
	engine, certs, _ := newRouter(t)
	b := certtest.NewBundle(t, "up.example.com", []string{"up.example.com"}, nil)

	// 先仅指纹登记
	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil,
		[]filePart{{"certFile", "up.crt", b.CertPEM}})
	require.Equal(t, http.StatusOK, w.Code)
	var data map[string]any
	require.NoError(t, json.Unmarshal(decode(t, w).Data, &data))
	certID := data["certId"].(string)

	// 补传匹配私钥 → 升级 complete
	w = doMultipart(t, engine, http.MethodPost, "/api/v1/certs/"+certID+"/key", nil,
		[]filePart{{"keyFile", "up.key", b.KeyPEM}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env := decode(t, w)
	require.True(t, env.Success)
	var up map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &up))
	assert.Equal(t, string(domain.HostingStatusComplete), up["hostingStatus"])
	assert.Equal(t, b.Fingerprint, up["fingerprint"])
	assert.Len(t, up, 3, "补传成功响应仅含三字段")
	assertNoKeyMaterial(t, w, b.KeyPEM)

	stored, err := certs.GetByID(t.Context(), certID)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusComplete, stored.HostingStatus)
	require.NotNil(t, stored.EncryptedPrivateKey)
}

func TestUploadKeyAPIMismatch(t *testing.T) {
	engine, certs, _ := newRouter(t)
	b := certtest.NewBundle(t, "mm.example.com", []string{"mm.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil,
		[]filePart{{"certFile", "mm.crt", b.CertPEM}})
	require.Equal(t, http.StatusOK, w.Code)
	var data map[string]any
	require.NoError(t, json.Unmarshal(decode(t, w).Data, &data))
	certID := data["certId"].(string)

	w = doMultipart(t, engine, http.MethodPost, "/api/v1/certs/"+certID+"/key", nil,
		[]filePart{{"keyFile", "mm.key", certtest.NewKeyPEM(t)}})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	env := decode(t, w)
	assert.False(t, env.Success)
	require.NotNil(t, env.Error)
	assert.Equal(t, domain.CodeCertKeyMismatch, env.Error.Code)

	stored, err := certs.GetByID(t.Context(), certID)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, stored.HostingStatus, "不匹配不得升级")
}

func TestUploadKeyAPINotFound(t *testing.T) {
	engine, _, _ := newRouter(t)
	b := certtest.NewBundle(t, "nf.example.com", []string{"nf.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs/000000000000000000000000/key", nil,
		[]filePart{{"keyFile", "nf.key", b.KeyPEM}})
	assert.Equal(t, http.StatusNotFound, w.Code)
	env := decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, CodeNotFound, env.Error.Code)

	// 缺 keyFile → 400
	w = doMultipart(t, engine, http.MethodPost, "/api/v1/certs/000000000000000000000000/key", nil, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---- 响应信封形态 ----

func TestEnvelopeShape(t *testing.T) {
	engine, _, _ := newRouter(t)
	b := certtest.NewBundle(t, "env.example.com", []string{"env.example.com"}, nil)

	w := doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil,
		[]filePart{{"certFile", "e.crt", b.CertPEM}})
	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	for _, key := range []string{"success", "data"} {
		_, ok := raw[key]
		assert.True(t, ok, "信封必须含 %s 字段", key)
	}
	_, hasErr := raw["error"]
	assert.False(t, hasErr, "成功响应不携带 error 字段（omitempty）")

	// 错误信封：success=false + error.code
	w = doMultipart(t, engine, http.MethodPost, "/api/v1/certs", nil,
		[]filePart{{"certFile", "e2.crt", []byte("garbage")}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decode(t, w)
	assert.False(t, env.Success)
	require.NotNil(t, env.Error)
	assert.NotEmpty(t, env.Error.Code)
}

func f0v(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
