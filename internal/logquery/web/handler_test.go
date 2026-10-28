package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cloudx-sdk/logquery"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"go.uber.org/zap/zapcore"
)

const (
	testCloud domain.CloudProvider = "webtestcloud"
	tenantKey                      = "tenant_id"
)

type webEntry struct {
	Ts int64 `json:"timestamp"`
}

func (e *webEntry) GetTimestamp() int64       { return e.Ts }
func (e *webEntry) GetMeta() logquery.LogMeta { return logquery.LogMeta{Cloud: testCloud} }

type webAccountSource struct{}

func (webAccountSource) List(_ context.Context, f domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	if f.TenantID != 3 {
		return nil, 0, nil
	}
	return []domain.CloudAccount{{ID: 1, Name: "acc", Provider: testCloud,
		Status: domain.CloudAccountStatusActive, TenantID: 3}}, 1, nil
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	now := time.Now().UnixMilli()
	logquery.RegisterProvider(testCloud, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &stubProvider{entries: []logquery.LogEntry{&webEntry{Ts: now - 1000}}}, nil
	})
	svc := service.NewFederationService(webAccountSource{}, nil)
	r := gin.New()
	g := r.Group("/api/v1/cam/logs")
	g.Use(func(c *gin.Context) {
		if c.Query("anon") == "1" {
			c.Next()
			return
		}
		c.Set(middleware.TenantIDKey, int64(3))
		c.Next()
	})
	NewLogQueryHandler(svc).RegisterRoutes(g)
	return r
}

type stubProvider struct {
	entries []logquery.LogEntry
}

func (p *stubProvider) Cloud() domain.CloudProvider { return testCloud }
func (p *stubProvider) LogType() logquery.LogType   { return logquery.LogTypeCDN }
func (p *stubProvider) ListLogSources(context.Context, *domain.CloudAccount) ([]logquery.LogSource, error) {
	return []logquery.LogSource{{Cloud: testCloud, ResourceID: "api.example.com", Enabled: true}}, nil
}
func (p *stubProvider) Search(context.Context, *domain.CloudAccount, logquery.SearchParams) ([]logquery.LogEntry, error) {
	return p.entries, nil
}

func TestTypes(t *testing.T) {
	r := newTestRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/logs/types", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var resp struct {
		Data []struct {
			Type   string `json:"type"`
			Fields []struct {
				Key string `json:"key"`
			} `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 6 {
		t.Fatalf("types = %d, want 6(cdn/waf/slb/access/app/k8s-event)", len(resp.Data))
	}
	// k8s-event 类型含集群/原因/级别等事件契约字段
	var k8sFields []string
	for _, m := range resp.Data {
		if m.Type != "k8s-event" {
			continue
		}
		for _, f := range m.Fields {
			k8sFields = append(k8sFields, f.Key)
		}
	}
	for _, want := range []string{"cluster", "namespace", "reason", "level", "message"} {
		if !slices.Contains(k8sFields, want) {
			t.Errorf("k8s-event 字段缺 %s(有 %v)", want, k8sFields)
		}
	}
	// app 类型含 RCA 服务间消费的契约字段(service/level/message)
	var appFields []string
	for _, m := range resp.Data {
		if m.Type != "app" {
			continue
		}
		for _, f := range m.Fields {
			appFields = append(appFields, f.Key)
		}
	}
	for _, want := range []string{"service", "level", "message"} {
		found := false
		for _, k := range appFields {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("app 类型缺契约字段 %q, fields=%v", want, appFields)
		}
	}
	for _, m := range resp.Data {
		if len(m.Fields) == 0 {
			t.Errorf("type %s has no fields", m.Type)
		}
	}
}

func TestSources(t *testing.T) {
	r := newTestRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/logs/sources?log_type=cdn", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []logquery.LogSource `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ResourceID != "api.example.com" {
		t.Fatalf("sources = %+v", resp.Data)
	}
	// 缺 log_type:400
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/v1/cam/logs/sources", nil))
	if w2.Code != http.StatusBadRequest {
		t.Errorf("missing log_type: code = %d, want 400", w2.Code)
	}
}

func TestSearch(t *testing.T) {
	r := newTestRouter(t)
	now := time.Now().UnixMilli()
	body, _ := json.Marshal(map[string]any{
		"log_type":   "cdn",
		"start_time": now - 3600_000,
		"end_time":   now,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Total   int                     `json:"total"`
			Entries []json.RawMessage       `json:"entries"`
			Sources []service.SourceOutcome `json:"sources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Total != 1 || len(resp.Data.Entries) != 1 {
		t.Fatalf("data = %+v", resp.Data)
	}
	var entry map[string]any
	if err := json.Unmarshal(resp.Data.Entries[0], &entry); err != nil {
		t.Fatal(err)
	}
	if entry["timestamp"] == nil {
		t.Fatalf("entry missing timestamp: %v", entry)
	}
	// 无租户上下文:403
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search?anon=1", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("anon: code = %d, want 403", w2.Code)
	}
	// 非法 log_type:400
	bad, _ := json.Marshal(map[string]any{"log_type": "nope", "start_time": now, "end_time": now + 1})
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", bytes.NewReader(bad))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Errorf("bad type: code = %d, want 400", w3.Code)
	}
}

// TestSearchRejectionContract 400 拒绝契约:状态码与 msg 原文钉死。
// rejectBadRequest 引入日志后响应体必须逐字节不变(客户端按 msg 文案
// 展示;契约漂移会让前端降级文案失配)。同时覆盖各校验分支,作为
// "服务端拒绝可观测"改造的行为回归。
func TestSearchRejectionContract(t *testing.T) {
	r := newTestRouter(t)
	now := time.Now().UnixMilli()
	cases := []struct {
		name     string
		body     string
		wantCode int
		wantMsg  string // 精确匹配;不确定后缀时以 prefix 标注
		prefix   bool
	}{
		{
			name:     "bind:损坏 JSON",
			body:     `{`,
			wantCode: 400,
			wantMsg:  "invalid request:",
			prefix:   true,
		},
		{
			name:     "bind:类型不符(account_ids 传字符串)",
			body:     `{"log_type":"cdn","start_time":1,"end_time":2,"account_ids":["5"]}`,
			wantCode: 400,
			wantMsg:  "invalid request:",
			prefix:   true,
		},
		{
			name:     "validate:非法 log_type",
			body:     `{"log_type":"nope","start_time":1,"end_time":2}`,
			wantCode: 400,
			wantMsg:  "invalid log type: nope",
		},
		{
			name:     "validate:窗口值过小(秒纪元之下)",
			body:     `{"log_type":"cdn","start_time":12345,"end_time":67890}`,
			wantCode: 400,
			wantMsg:  "invalid window value 12345",
			prefix:   true,
		},
		{
			name:     "validate:end <= start",
			body:     fmt.Sprintf(`{"log_type":"cdn","start_time":%d,"end_time":%d}`, now, now-1000),
			wantCode: 400,
			wantMsg:  "invalid time window:",
			prefix:   true,
		},
		{
			name:     "validate:非法筛选操作符",
			body:     fmt.Sprintf(`{"log_type":"cdn","start_time":%d,"end_time":%d,"filters":[{"field":"host","op":"regex","value":"a"}]}`, now-3600_000, now),
			wantCode: 400,
			wantMsg:  "invalid filter op: regex",
		},
		{
			name:     "validate:筛选条件不完整",
			body:     fmt.Sprintf(`{"log_type":"cdn","start_time":%d,"end_time":%d,"filters":[{"field":"host","op":"eq","value":""}]}`, now-3600_000, now),
			wantCode: 400,
			wantMsg:  "incomplete field filter:",
			prefix:   true,
		},
		{
			// 服务端窗口纵深防御(2026-10-28 口径确认):超 max_window_days 一律 400,
			// 绕过前端 clampWindow 直连 API 同样被拒;窗口毫秒数为固定输入的确定值
			name:     "validate:超窗(cdn 30d > 7d)",
			body:     fmt.Sprintf(`{"log_type":"cdn","start_time":%d,"end_time":%d}`, now-30*86400_000, now),
			wantCode: 400,
			wantMsg:  "time window 2592000000ms exceeds max_window_days 7 for cdn",
		},
		{
			name:     "validate:超窗(slb 8d > 3d)",
			body:     fmt.Sprintf(`{"log_type":"slb","start_time":%d,"end_time":%d}`, now-8*86400_000, now),
			wantCode: 400,
			wantMsg:  "time window 691200000ms exceeds max_window_days 3 for slb",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d, body=%s", w.Code, tc.wantCode, w.Body.String())
			}
			var resp struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if tc.prefix {
				if !strings.HasPrefix(resp.Msg, tc.wantMsg) {
					t.Fatalf("msg = %q, want prefix %q", resp.Msg, tc.wantMsg)
				}
			} else if resp.Msg != tc.wantMsg {
				t.Fatalf("msg = %q, want %q", resp.Msg, tc.wantMsg)
			}
		})
	}
}

// ---- 拒绝日志可观测性验证:内存 zapcore 捕获 elog 输出 ----
// 生产 elog 默认 EnableAsync,短命测试进程缓冲不刷出;此处以同步内存
// core 直捕 Write,验证"400 必有对应 Warn 日志"这一本轮核心行为。

type memEntry struct {
	level  zapcore.Level
	msg    string
	fields map[string]string
}

type memCore struct {
	mu      sync.Mutex
	entries []memEntry
}

func (c *memCore) Enabled(zapcore.Level) bool        { return true }
func (c *memCore) With([]zapcore.Field) zapcore.Core { return c }
func (c *memCore) Sync() error                       { return nil }
func (c *memCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}
func (c *memCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := memEntry{level: ent.Level, msg: ent.Message, fields: map[string]string{}}
	for _, f := range fields {
		// Field.String() 对非字符串类型返回空串,须经编码器取值
		enc := zapcore.NewMapObjectEncoder()
		f.AddTo(enc)
		e.fields[f.Key] = fmt.Sprintf("%v", enc.Fields[f.Key])
	}
	c.entries = append(c.entries, e)
	return nil
}

func TestSearchRejectionLogged(t *testing.T) {
	core := &memCore{}
	old := elog.DefaultLogger
	elog.DefaultLogger = elog.DefaultContainer().Build(
		elog.WithZapCore(core), elog.WithEnableAsync(false), elog.WithLevel("warn"))
	defer func() { elog.DefaultLogger = old }()

	r := newTestRouter(t)
	body := `{"log_type":"nope","start_time":1,"end_time":2}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}

	core.mu.Lock()
	defer core.mu.Unlock()
	for _, e := range core.entries {
		if e.msg != "logquery request rejected" {
			continue
		}
		if e.fields["stage"] != "validate" || e.fields["path"] != "/api/v1/cam/logs/search" ||
			e.fields["reason"] != "invalid log type: nope" || e.fields["tenant_id"] != "3" {
			t.Fatalf("reject log fields = %+v", e.fields)
		}
		return // 命中即通过
	}
	t.Fatalf("未捕获 logquery request rejected 日志: entries=%+v", core.entries)
}

// 窗口边界:恰好等于 max_window_days 放行(200),超 1ms 拒绝(400)——
// 钳制语义与前端 clampWindow(end-start <= max)一致,不多收也不误伤。
func TestWindowBoundary(t *testing.T) {
	r := newTestRouter(t)
	now := time.Now().UnixMilli()
	maxMs := int64(7 * 86400_000) // cdn max_window_days = 7

	for _, tc := range []struct {
		name     string
		delta    int64
		wantCode int
	}{
		{"恰好 7 天放行", maxMs, http.StatusOK},
		{"超 1ms 拒绝", maxMs + 1, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"log_type":"cdn","start_time":%d,"end_time":%d}`, now-tc.delta, now)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d, body=%s", w.Code, tc.wantCode, w.Body.String())
			}
		})
	}
}

// 窗口守卫覆盖全部四入口(search/aggregate/diagnose/cache-analyze,#39 只测了 search):
// 超窗一律 400 且原因文案统一;合法窗口不得被守卫误拒(仅断言"未命中守卫文案"——
// 合法窗口后仍可能因特性开关关闭/类型不支持/无 provider 等原因另行报错,那不是本测试对象)。
func TestWindowGuardAllEndpoints(t *testing.T) {
	r := newTestRouter(t)
	now := time.Now().UnixMilli()
	day := int64(86400_000)

	cases := []struct {
		name    string
		path    string
		logType string
		maxDays int
		extra   string // 各入口专有字段
	}{
		{"aggregate cdn 超窗", "/api/v1/cam/logs/aggregate", "cdn", 7, `,"dimension":"host"`},
		{"diagnose waf 超窗", "/api/v1/cam/logs/diagnose", "waf", 7, ""},
		{"cache-analyze cdn 超窗", "/api/v1/cam/logs/cache-analyze", "cdn", 7, ""},
	}
	post := func(path, body string) (int, string) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var resp struct {
			Msg string `json:"msg"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp.Msg
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 超窗(上限+23 天):400 + 统一守卫文案
			over := fmt.Sprintf(`{"log_type":"%s","start_time":%d,"end_time":%d%s}`,
				tc.logType, now-int64(tc.maxDays+23)*day, now, tc.extra)
			code, msg := post(tc.path, over)
			want := fmt.Sprintf("exceeds max_window_days %d for %s", tc.maxDays, tc.logType)
			if code != http.StatusBadRequest {
				t.Fatalf("超窗 code = %d, want 400, msg=%q", code, msg)
			}
			if !strings.Contains(msg, want) {
				t.Fatalf("超窗 msg = %q, want 含 %q", msg, want)
			}

			// 合法窗口(1h):守卫不得命中(其余失败原因另议)
			ok := fmt.Sprintf(`{"log_type":"%s","start_time":%d,"end_time":%d%s}`,
				tc.logType, now-3600_000, now, tc.extra)
			if _, okMsg := post(tc.path, ok); strings.Contains(okMsg, "exceeds max_window_days") {
				t.Fatalf("合法窗口被守卫误拒: %q", okMsg)
			}
		})
	}
}
