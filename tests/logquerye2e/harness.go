// @feature log-query-optimization @api-functional
//
// Package logquerye2e is the shared harness for the log-query-optimization
// live API functional tests (journeys under tests/<journey>/).
//
// Unlike the hermetic discoverytest harness, these tests exercise the REAL
// running logquery HTTP surface:
//
//	base URL: http://localhost:8001 (env LOGQUERY_E2E_BASE overrides)
//	auth:     Bearer JWT, either from env LOGQUERY_E2E_TOKEN or generated
//	          by running mktok.py via python (env LOGQUERY_E2E_PYTHON /
//	          LOGQUERY_E2E_MKTOK override the interpreter / script paths)
//
// Per dispatcher instruction the suite must stay silent-skippable: when the
// service is unreachable, the token cannot be produced, or auth is rejected,
// tests skip instead of failing (dispatcher: "不 block").
//
// No secret material is embedded in test code: the dev JWT is produced by the
// external mktok.py tool, never hardcoded here.
package logquerye2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// DefaultBaseURL is the logquery-context backend address.
const DefaultBaseURL = "http://localhost:8001"

// APIPrefix is the mounted route prefix of the logquery module.
const APIPrefix = "/api/v1/cam/logs"

// RequestTimeout bounds a single federation call (server-side federation
// timeout is 30s; give the client a little more headroom).
const RequestTimeout = 45 * time.Second

// BaseURL returns the target base URL (env LOGQUERY_E2E_BASE overrides).
func BaseURL() string {
	if v := strings.TrimSpace(os.Getenv("LOGQUERY_E2E_BASE")); v != "" {
		return v
	}
	return DefaultBaseURL
}

// Envelope is the uniform API response envelope (code/msg/data).
type Envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// OK reports whether the envelope carries business success.
func (e Envelope) OK() bool { return e.Code == 0 }

var (
	tokenOnce   sync.Once
	bearerToken string
	tokenErr    string

	liveOnce   sync.Once
	liveReason string // empty = live and reachable
)

// bearer produces a JWT via env token or the external mktok.py dev tool.
func bearer() (string, string) {
	tokenOnce.Do(func() {
		if v := strings.TrimSpace(os.Getenv("LOGQUERY_E2E_TOKEN")); v != "" {
			bearerToken = v
			return
		}
		script := strings.TrimSpace(os.Getenv("LOGQUERY_E2E_MKTOK"))
		if script == "" {
			script = "C:/Users/chengyinghao/AppData/Local/Temp/mktok.py"
		}
		interpreters := []string{"python", "D:/soft/python.exe"}
		if py := strings.TrimSpace(os.Getenv("LOGQUERY_E2E_PYTHON")); py != "" {
			interpreters = []string{py}
		}
		var out []byte
		for _, py := range interpreters {
			cmd := exec.Command(py, script)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			o, err := cmd.Output()
			if err == nil {
				out = o
				break
			}
			tokenErr = fmt.Sprintf("mktok via %s: %v: %s", py, err, strings.TrimSpace(stderr.String()))
		}
		if len(out) == 0 {
			if tokenErr == "" {
				tokenErr = "mktok produced no output"
			}
			return
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		bearerToken = strings.TrimSpace(lines[len(lines)-1])
		if bearerToken == "" {
			tokenErr = "mktok output has empty last line"
		}
	})
	return bearerToken, tokenErr
}

// RequireLive skips the test (silent-skip contract) when the live service
// cannot be exercised: no token, unreachable, or auth rejected.
func RequireLive(t *testing.T) {
	t.Helper()
	liveOnce.Do(func() {
		tok, terr := bearer()
		if tok == "" {
			liveReason = "no bearer token available: " + terr
			return
		}
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest(http.MethodGet, BaseURL()+APIPrefix+"/types", nil)
		if err != nil {
			liveReason = "build probe request: " + err.Error()
			return
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := client.Do(req)
		if err != nil {
			liveReason = "service unreachable: " + err.Error()
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			liveReason = "auth rejected (401); provide LOGQUERY_E2E_TOKEN"
			return
		}
		if resp.StatusCode != http.StatusOK {
			liveReason = fmt.Sprintf("probe status %d", resp.StatusCode)
		}
	})
	if liveReason != "" {
		t.Skipf("logquery live service unavailable: %s", liveReason)
	}
}

// Do issues an authenticated request against the live surface and decodes the
// uniform envelope. Transport-level failures and auth rejection skip (silent-
// skip contract); unexpected business responses are returned for assertion.
func Do(t *testing.T, method, path string, body any) (int, Envelope) {
	t.Helper()
	RequireLive(t)
	tok, _ := bearer()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, BaseURL()+path, reader)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: RequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("service unreachable during %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Skip("auth rejected (401); token no longer valid")
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope from %s %s (status %d): %v: %s",
			method, path, resp.StatusCode, err, truncate(string(raw), 512))
	}
	return resp.StatusCode, env
}

// Get performs GET path.
func Get(t *testing.T, path string) (int, Envelope) {
	return Do(t, http.MethodGet, path, nil)
}

// Post performs POST path with a JSON body.
func Post(t *testing.T, path string, body any) (int, Envelope) {
	return Do(t, http.MethodPost, path, body)
}

// TypesData decodes GET /types data.
func TypesData(t *testing.T, env Envelope) []TypeMeta {
	t.Helper()
	var out []TypeMeta
	decodeData(t, env, &out)
	return out
}

// SourcesData decodes GET /sources data.
func SourcesData(t *testing.T, env Envelope) []LogSource {
	t.Helper()
	var out []LogSource
	decodeData(t, env, &out)
	return out
}

// SearchData decodes POST /search data.
func SearchData(t *testing.T, env Envelope) SearchResponse {
	t.Helper()
	var out SearchResponse
	decodeData(t, env, &out)
	return out
}

// AggregateData decodes POST /aggregate data.
func AggregateData(t *testing.T, env Envelope) AggregateResponse {
	t.Helper()
	var out AggregateResponse
	decodeData(t, env, &out)
	return out
}

func decodeData(t *testing.T, env Envelope, into any) {
	t.Helper()
	if !env.OK() {
		t.Fatalf("unexpected business error: code=%d msg=%s", env.Code, env.Msg)
	}
	if len(env.Data) == 0 {
		t.Fatalf("envelope has no data payload")
	}
	if err := json.Unmarshal(env.Data, into); err != nil {
		t.Fatalf("decode data: %v: %s", err, truncate(string(env.Data), 512))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// ---- shared response models (subset of the wire contract) ----

// TypeMeta GET /types item.
type TypeMeta struct {
	Type          string     `json:"type"`
	Label         string     `json:"label"`
	Fields        []FieldDef `json:"fields"`
	MaxWindowDays int        `json:"max_window_days"`
}

// FieldDef GET /types field definition.
type FieldDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Fixed bool   `json:"fixed"`
}

// LogSource GET /sources item.
type LogSource struct {
	Cloud       string `json:"cloud"`
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
	Region      string `json:"region"`
	LogType     string `json:"log_type"`
	ResourceID  string `json:"resource_id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Note        string `json:"note"`
}

// SourceOutcome per-source status inside search/aggregate responses.
type SourceOutcome struct {
	Cloud       string `json:"cloud"`
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
	Count       int    `json:"count"`
	Error       string `json:"error"`
	DurationMs  int64  `json:"duration_ms"`
	Total       int64  `json:"total"`
}

// LogEntry unified entry subset (meta + timestamp + type-specific fields kept
// loose so one decoder serves cdn/waf/slb payloads).
type LogEntry map[string]any

// Meta returns the meta block of an entry.
func (e LogEntry) Meta() map[string]any {
	m, _ := e["meta"].(map[string]any)
	return m
}

// MetaString reads a string field from meta ("").
func (e LogEntry) MetaString(key string) string {
	m := e.Meta()
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// String reads a top-level string field ("").
func (e LogEntry) String(key string) string {
	s, _ := e[key].(string)
	return s
}

// Number reads a top-level numeric field as float64 (0 when absent).
func (e LogEntry) Number(key string) float64 {
	f, _ := e[key].(float64)
	return f
}

// Timestamp returns entry timestamp in Unix ms (0 when absent).
func (e LogEntry) Timestamp() int64 { return int64(e.Number("timestamp")) }

// SearchResponse POST /search data.
type SearchResponse struct {
	LogType   string          `json:"log_type"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated"`
	Entries   []LogEntry      `json:"entries"`
	Sources   []SourceOutcome `json:"sources"`
}

// AggregateResponse POST /aggregate data.
type AggregateResponse struct {
	LogType   string          `json:"log_type"`
	Total     int64           `json:"total"`
	Buckets   []Bucket        `json:"buckets"`
	TopN      []TopNItem      `json:"topn"`
	Sources   []SourceOutcome `json:"sources"`
	TopNSkip  string          `json:"topn_skip"`
	Truncated bool            `json:"truncated"`
}

// Bucket aggregate time bucket.
type Bucket struct {
	Timestamp int64 `json:"timestamp"`
	Count     int64 `json:"count"`
}

// TopNItem aggregate TopN item.
type TopNItem struct {
	Name  string  `json:"name"`
	Count int64   `json:"count"`
	Value float64 `json:"value"`
}

// SearchBody builds a POST /search request body.
type SearchBody struct {
	LogType    string        `json:"log_type"`
	StartTime  int64         `json:"start_time"`
	EndTime    int64         `json:"end_time"`
	Query      string        `json:"query,omitempty"`
	Clouds     []string      `json:"clouds,omitempty"`
	AccountIDs []int64       `json:"account_ids,omitempty"`
	Resources  []string      `json:"resources,omitempty"`
	Filters    []FieldFilter `json:"filters,omitempty"`
	Limit      int           `json:"limit,omitempty"`
}

// FieldFilter structured field filter (AND-stacked).
type FieldFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// AggregateBody builds a POST /aggregate request body.
type AggregateBody struct {
	LogType   string        `json:"log_type"`
	StartTime int64         `json:"start_time"`
	EndTime   int64         `json:"end_time"`
	Query     string        `json:"query,omitempty"`
	Clouds    []string      `json:"clouds,omitempty"`
	Resources []string      `json:"resources,omitempty"`
	Filters   []FieldFilter `json:"filters,omitempty"`
	Dimension string        `json:"dimension,omitempty"`
	Metric    string        `json:"metric,omitempty"`
}

// NowMs current wall clock in Unix milliseconds.
func NowMs() int64 { return time.Now().UnixMilli() }
