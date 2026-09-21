package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// postCacheAnalyze 以 JSON body 调 POST /api/v1/cam/logs/cache-analyze。
func postCacheAnalyze(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/cache-analyze", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// cacheAnalyzeBody 标准请求体(10 分钟窗口,仅 CDN)。
func cacheAnalyzeBody(logType string) string {
	now := time.Now().UnixMilli()
	return `{"log_type":"` + logType + `","start_time":` +
		jsonInt64(now-600_000) + `,"end_time":` + jsonInt64(now) + `}`
}

func jsonInt64(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestCacheAnalyzeRouteFlagOff AC5:feature flag 默认关,接口明确报错。
func TestCacheAnalyzeRouteFlagOff(t *testing.T) {
	r := newTestRouter(t)
	w := postCacheAnalyze(r, cacheAnalyzeBody("cdn"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("flag 关闭应 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "LOGQUERY_CACHE_ANALYZE_ENABLED") {
		t.Errorf("错误应提示 flag 名: %s", w.Body.String())
	}
}

// TestCacheAnalyzeRouteOK 硬规则:flag 开启后 CDN 开放,SLB/WAF 明确报错;
// 无聚合能力的源(测试 stub)降级不白屏。
func TestCacheAnalyzeRouteOK(t *testing.T) {
	t.Setenv("LOGQUERY_CACHE_ANALYZE_ENABLED", "1")
	r := newTestRouter(t)

	// 仅 CDN:SLB 明确报错
	w := postCacheAnalyze(r, cacheAnalyzeBody("slb"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "cdn") {
		t.Errorf("slb 应明确报错仅支持 cdn, got %d: %s", w.Code, w.Body.String())
	}

	// CDN 正常响应(空数据降级:档位 unknown,不白屏)
	w = postCacheAnalyze(r, cacheAnalyzeBody("cdn"))
	if w.Code != http.StatusOK {
		t.Fatalf("cdn 应 200, got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Code int `json:"code"`
		Data struct {
			AggregateFrames int `json:"aggregate_frames"`
			Summary         string
			Result          *struct {
				Grade string `json:"grade"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != 0 || out.Data.Result == nil {
		t.Fatalf("响应应含 result, got: %s", w.Body.String())
	}
	if out.Data.AggregateFrames != 2 {
		t.Errorf("aggregate_frames = %d, want 2", out.Data.AggregateFrames)
	}
	if out.Data.Summary != "" {
		t.Errorf("summary 应为占位空串, got %q", out.Data.Summary)
	}
}
