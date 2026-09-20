package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/logquery/diagnose"
)

// resetCfg 清掉进程级 once 缓存,使用例能独立设置环境变量。
func resetCfg(t *testing.T) {
	t.Helper()
	cfgOnce = sync.Once{}
	cfgOk = false
	cfgVal = Config{}
}

func setCfg(t *testing.T, base, key, model string) {
	t.Helper()
	resetCfg(t)
	if base == "" && key == "" {
		t.Setenv("LOGQUERY_LLM_BASE_URL", "")
		t.Setenv("LOGQUERY_LLM_API_KEY", "")
		return
	}
	t.Setenv("LOGQUERY_LLM_BASE_URL", base)
	t.Setenv("LOGQUERY_LLM_API_KEY", key)
	if model != "" {
		t.Setenv("LOGQUERY_LLM_MODEL", model)
	}
}

func TestEnabled(t *testing.T) {
	setCfg(t, "", "", "")
	if Enabled() {
		t.Fatal("未配置网关时应 Enabled()=false")
	}
	setCfg(t, "http://x/v1", "k", "")
	if !Enabled() {
		t.Fatal("配置 base+key 后应 Enabled()=true")
	}
}

func TestSummarizeDiagnoseOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径 = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q", got)
		}
		var cr chatRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Errorf("decode req: %v", err)
			w.WriteHeader(400)
			return
		}
		if cr.Model != "qwen-max" {
			t.Errorf("model = %q", cr.Model)
		}
		var joined string
		for _, m := range cr.Messages {
			joined += m.Content
		}
		for _, want := range []string{"/api/login(600 次)", "风险分 78", "突增", "Top 来源 IP"} {
			if !strings.Contains(joined, want) {
				t.Errorf("prompt 缺少 %q(全文: %s)", want, joined)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":" 高风控结论。 \n建议1… \n"}}]}`))
	}))
	defer srv.Close()
	setCfg(t, srv.URL, "sk-test", "qwen-max")

	prev := int64(1000)
	in := DiagnoseInput{
		WindowSec: 3600, Total: 8000, PrevTotal: &prev, Surge: 8, RiskScore: 78, RiskLevel: "high", AttackType: "cc_flood",
		TopIPs:  []string{"1.2.3.4(5000 次)", "5.6.7.8(1000 次)"},
		TopURIs: []string{"/api/login(600 次)"},
	}
	got, err := SummarizeDiagnose(context.Background(), in)
	if err != nil {
		t.Fatalf("SummarizeDiagnose err: %v", err)
	}
	if got != "高风控结论。 \n建议1…" {
		t.Errorf("content 未 trim: %q", got)
	}
}

func TestSummarizeDiagnoseErrors(t *testing.T) {
	t.Run("未配置返回错误", func(t *testing.T) {
		setCfg(t, "", "", "")
		if _, err := SummarizeDiagnose(context.Background(), DiagnoseInput{}); err == nil {
			t.Fatal("未配置应报错")
		}
	})
	t.Run("非 200 返回错误", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "over quota", http.StatusTooManyRequests)
		}))
		defer srv.Close()
		setCfg(t, srv.URL, "k", "")
		if _, err := SummarizeDiagnose(context.Background(), DiagnoseInput{}); err == nil {
			t.Fatal("429 应报错")
		}
	})
	t.Run("空 choices 返回错误", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[]}`))
		}))
		defer srv.Close()
		setCfg(t, srv.URL, "k", "")
		if _, err := SummarizeDiagnose(context.Background(), DiagnoseInput{}); err == nil {
			t.Fatal("空 choices 应报错")
		}
	})
	t.Run("超时返回错误(调用方降级空串)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			select {} // 永不返回
		}))
		defer srv.Close()
		setCfg(t, srv.URL, "k", "") // timeout 12s;用已取消 ctx 快速触发
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := SummarizeDiagnose(ctx, DiagnoseInput{}); err == nil {
			t.Fatal("已取消 ctx 应报错")
		}
	})
}

func TestFromDiagnose(t *testing.T) {
	res := &diagnose.DiagnoseResult{
		RiskScore: 32, RiskLevel: "low", AttackType: "normal", SurgeMultiplier: 1.0,
		TopSources: []diagnose.DiagnoseSource{{IP: "1.2.3.4", Count: 5000, Share: 0.6}},
		Measures:   []string{"m1", "m2", "m3"}, Degraded: true, DegradedReason: "前窗无数据",
	}
	prev := int64(1000)
	in := FromDiagnose(res, 8000, 3600, &prev, []string{"/a", "/b", "/c", "/d"}, []string{"404", "200"}, []string{"block"})
	if len(in.TopIPs) != 1 || !strings.Contains(in.TopIPs[0], "1.2.3.4") {
		t.Fatalf("TopIPs = %v", in.TopIPs)
	}
	if len(in.TopURIs) != 3 { // 4 → 截断为 3
		t.Fatalf("TopURIs 截断 = %v", in.TopURIs)
	}
	if in.PrevTotal == nil || *in.PrevTotal != 1000 {
		t.Fatalf("PrevTotal = %v", in.PrevTotal)
	}
	if !strings.Contains(in.DimensionGap, "前窗无数据") {
		t.Fatalf("DimensionGap = %q", in.DimensionGap)
	}
}