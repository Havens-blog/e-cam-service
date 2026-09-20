package diagwatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/diagnose"
	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	cloudxdomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

var (
	timeMin = time.Minute
	timeHour = time.Hour
	errBoom = errors.New("boom")
)

type fakeAccounts struct{ accs []cloudxdomain.CloudAccount }

func (f *fakeAccounts) List(context.Context, cloudxdomain.CloudAccountFilter) ([]cloudxdomain.CloudAccount, int64, error) {
	return f.accs, int64(len(f.accs)), nil
}

// fakeDiagnoser 按调用次数返回预置风险序列(测试升档去重)。
type fakeDiagnoser struct {
	seq []string // risk_level per call
	call atomic.Int64
	err  error
}

func (f *fakeDiagnoser) Diagnose(_ context.Context, _ int64, _ service.DiagnoseRequest) (*service.DiagnoseResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	i := int(f.call.Add(1)) - 1
	level := "none"
	if i < len(f.seq) {
		level = f.seq[i]
	}
	return respWith(level), nil
}

func respWith(level string) *service.DiagnoseResponse {
	return &service.DiagnoseResponse{
		WindowSec: 3600,
		Total:     8000,
		Prev:      &diagnose.PrevWindow{Total: 1000},
		Result: &diagnose.DiagnoseResult{
			RiskScore: 60, RiskLevel: level, AttackType: "cc_flood", SurgeMultiplier: 8,
			TopSources: []diagnose.DiagnoseSource{{IP: "1.2.3.4", Count: 5000, Share: 0.6}},
			Measures:   []string{"封禁 Top IP", "开启 CC 限频", "关注增长"},
		},
		Summary: "疑似批量刷量。",
	}
}

func baseCfg(channels ...alertdomain.NotificationChannel) Config {
	return Config{Enabled: true, Interval: timeMin, Window: timeHour, MinRisk: RiskRank("medium"), Channels: channels}
}

func acc(tenant int64, provider cloudxdomain.CloudProvider, status cloudxdomain.CloudAccountStatus) cloudxdomain.CloudAccount {
	return cloudxdomain.CloudAccount{ID: tenant * 100, TenantID: tenant, Provider: provider, Status: status}
}

func TestRiskRank(t *testing.T) {
	if RiskRank("high") != 3 || RiskRank("medium") != 2 || RiskRank("low") != 1 || RiskRank("none") != 0 || RiskRank("") != 0 {
		t.Fatal("RiskRank 档位映射错误")
	}
}

func TestCfgFromEnv(t *testing.T) {
	t.Run("默认关闭", func(t *testing.T) {
		t.Setenv("LOGQUERY_DIAG_WATCH_ENABLED", "")
		if _, ok := cfgFromEnv(); ok {
			t.Fatal("未开启应 false")
		}
	})
	t.Run("开启但无渠道 → 按未启用", func(t *testing.T) {
		t.Setenv("LOGQUERY_DIAG_WATCH_ENABLED", "true")
		t.Setenv("LOGQUERY_DIAG_WATCH_WECOM_URL", "")
		if _, ok := cfgFromEnv(); ok {
			t.Fatal("无渠道应 false")
		}
	})
	t.Run("开启+企业微信渠道", func(t *testing.T) {
		t.Setenv("LOGQUERY_DIAG_WATCH_ENABLED", "true")
		t.Setenv("LOGQUERY_DIAG_WATCH_WECOM_URL", "https://qyapi/x")
		t.Setenv("LOGQUERY_DIAG_WATCH_MIN_RISK", "high")
		t.Setenv("LOGQUERY_DIAG_WATCH_INTERVAL_SEC", "120")
		cfg, ok := cfgFromEnv()
		if !ok || cfg.MinRisk != 3 || len(cfg.Channels) != 1 || cfg.Channels[0].Type != alertdomain.ChannelWeCom || cfg.Interval != 120*time.Second {
			t.Fatalf("cfg = %+v ok=%v", cfg, ok)
		}
	})
}

func TestTenantAccounts(t *testing.T) {
	w := &Watcher{accounts: &fakeAccounts{accs: []cloudxdomain.CloudAccount{
		acc(1, "aliyun", cloudxdomain.CloudAccountStatusActive),
		acc(1, "huawei", cloudxdomain.CloudAccountStatusActive),
		acc(2, "tencent", cloudxdomain.CloudAccountStatusActive),
		acc(2, "aws", cloudxdomain.CloudAccountStatusDisabled),
	}}}
	got, err := w.tenantAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got[1]) != 2 || len(got[2]) != 1 || got[2][0] != "tencent" {
		t.Fatalf("分组错误(禁用账号应剔除): %v", got)
	}
	if got[1][0] != "aliyun" { // 排序稳定
		t.Fatalf("云序错误: %v", got[1])
	}
}

func TestCheckOnceEscalationAndDedup(t *testing.T) {
	// 真实 wecom webhook:记录收到的告警次数与消息格式
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if payload["msgtype"] != "markdown" {
			t.Errorf("msgtype = %v", payload["msgtype"])
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := &Watcher{
		svc: &fakeDiagnoser{seq: []string{"low", "medium", "medium", "high"}},
		accounts: &fakeAccounts{accs: []cloudxdomain.CloudAccount{
			acc(1, "aliyun", cloudxdomain.CloudAccountStatusActive),
		}},
		cfg:    baseCfg(alertdomain.NotificationChannel{Type: alertdomain.ChannelWeCom, Config: map[string]any{"webhook": srv.URL}}),
		logger: elog.DefaultLogger,
		last:   map[int64]int{}, stop: make(chan struct{}),
	}
	for i := 0; i < 4; i++ {
		w.checkOnce()
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("应恰好推送 2 次(low 不发、medium 首次发、同档不发、升 high 发),实际 %d", got)
	}
}

func TestCheckOnceSkipsFailedDiagnose(t *testing.T) {
	w := &Watcher{
		svc:      &fakeDiagnoser{err: errBoom},
		accounts: &fakeAccounts{accs: []cloudxdomain.CloudAccount{acc(1, "aliyun", cloudxdomain.CloudAccountStatusActive)}},
		cfg:      baseCfg(),
		logger:   elog.DefaultLogger,
		last:     map[int64]int{}, stop: make(chan struct{}),
	}
	w.checkOnce() // 诊断失败不 panic、不推送
	w.checkOnce()
}

func TestBuildMessage(t *testing.T) {
	msg := buildMessage(respWith("high"))
	if !strings.Contains(msg.Title, "high") || msg.Severity != "critical" {
		t.Fatalf("title/severity: %q / %q", msg.Title, msg.Severity)
	}
	if !strings.Contains(msg.Content, "1.2.3.4") || !strings.Contains(msg.Content, "封禁 Top IP") || !strings.Contains(msg.Content, "疑似批量刷量") {
		t.Fatalf("content 缺关键字段: %q", msg.Content)
	}
	if !msg.Markdown {
		t.Fatal("应 markdown")
	}
}