// 定时 WAF 流量诊断看护:周期对已接入租户跑诊断,风险等级达到阈值时经
// 配置的 webhook 渠道推送告警(价值:被刷检测从"手动点击"升级为"持续监控")。
//
// 设计约束:
//   - 纯 env 配置,默认关闭(LOGQUERY_DIAG_WATCH_ENABLED=true 才启动);
//   - 无需 DB 装配:租户→云账号经 AccountSource(与联邦层同一窄接口);
//   - 渠道复用 internal/alert/channel(wecom/feishu/dingtalk 由 URL env 建配置);
//   - 去重:仅当风险等级**升档**才推送(持续高风险不重复轰炸;窗口推进后的
//     同档位变化不打扰,降档后再次升档会再报);
//   - 失败容错:单租户诊断失败/渠道发送失败仅记日志,不影响主循环。
package diagwatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/channel"
	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	cloudxdomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// Config 看护配置(env 注入,密钥只走 URL/secret 环境变量)。
type Config struct {
	Enabled  bool
	Interval time.Duration // 诊断周期
	Window   time.Duration // 诊断窗口(最近 N)
	MinRisk  int           // 风险等级档位门槛(1=low 2=medium 3=high)
	Channels []alertdomain.NotificationChannel
}

// RiskRank 风险等级档位(none=0 low=1 medium=2 high=3;≥MinRisk 才触发推送)。
func RiskRank(level string) int {
	switch level {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func cfgFromEnv() (Config, bool) {
	if strings.TrimSpace(os.Getenv("LOGQUERY_DIAG_WATCH_ENABLED")) != "true" {
		return Config{}, false
	}
	cfg := Config{Enabled: true, Interval: 30 * time.Minute, Window: time.Hour, MinRisk: RiskRank("medium")}
	if v, err := strconv.Atoi(os.Getenv("LOGQUERY_DIAG_WATCH_INTERVAL_SEC")); err == nil && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, err := strconv.Atoi(os.Getenv("LOGQUERY_DIAG_WATCH_WINDOW_SEC")); err == nil && v > 0 {
		cfg.Window = time.Duration(v) * time.Second
	}
	if v := strings.TrimSpace(os.Getenv("LOGQUERY_DIAG_WATCH_MIN_RISK")); v != "" {
		if r := RiskRank(v); r > 0 {
			cfg.MinRisk = r
		}
	}
	// 渠道:任一 URL 配置即加入(Email 依赖 SMTP 全套,不在本看护兜底范围)
	if u := strings.TrimSpace(os.Getenv("LOGQUERY_DIAG_WATCH_WECOM_URL")); u != "" {
		cfg.Channels = append(cfg.Channels, alertdomain.NotificationChannel{
			Type: alertdomain.ChannelWeCom, Config: map[string]any{"webhook": u},
		})
	}
	if u := strings.TrimSpace(os.Getenv("LOGQUERY_DIAG_WATCH_FEISHU_URL")); u != "" {
		cfg.Channels = append(cfg.Channels, alertdomain.NotificationChannel{
			Type: alertdomain.ChannelFeishu,
			Config: map[string]any{
				"webhook": u,
				"secret":  strings.TrimSpace(os.Getenv("LOGQUERY_DIAG_WATCH_FEISHU_SECRET")),
			},
		})
	}
	if u := strings.TrimSpace(os.Getenv("LOGQUERY_DIAG_WATCH_DINGTALK_URL")); u != "" {
		cfg.Channels = append(cfg.Channels, alertdomain.NotificationChannel{
			Type: alertdomain.ChannelDingTalk,
			Config: map[string]any{
				"webhook": u,
				"secret":  strings.TrimSpace(os.Getenv("LOGQUERY_DIAG_WATCH_DINGTALK_SECRET")),
			},
		})
	}
	if len(cfg.Channels) == 0 {
		return Config{}, false // 开了开关但没配渠道 = 无效,按未启用处理
	}
	return cfg, true
}

// Diagnoser 定时诊断的最小调用面(service.FederationService 满足;测试注入假实现)。
type Diagnoser interface {
	Diagnose(ctx context.Context, tenantID int64, req service.DiagnoseRequest) (*service.DiagnoseResponse, error)
}

// AccountSource 租户账号源(与联邦层窄接口一致;List 全量后按租户分组)。
type AccountSource interface {
	List(ctx context.Context, filter cloudxdomain.CloudAccountFilter) ([]cloudxdomain.CloudAccount, int64, error)
}

// Watcher 定时诊断看护。
type Watcher struct {
	svc      Diagnoser
	accounts AccountSource
	cfg      Config
	logger   *elog.Component

	mu   sync.Mutex
	last map[int64]int // 租户 → 最近已推送风险档位(仅升档推送)
	stop chan struct{}
}

// Start 从环境变量装配并启动看护(未启用返回 nil);返回的 Watcher 供 Stop。
func Start(svc Diagnoser, accounts AccountSource, logger *elog.Component) *Watcher {
	cfg, ok := cfgFromEnv()
	if !ok {
		return nil
	}
	log := logger
	if log == nil {
		log = elog.DefaultLogger
	}
	w := &Watcher{
		svc: svc, accounts: accounts, cfg: cfg, logger: log,
		last: map[int64]int{}, stop: make(chan struct{}),
	}
	go w.run()
	return w
}

// Stop 停止看护循环(幂等)。
func (w *Watcher) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
}

func (w *Watcher) run() {
	w.logger.Info("[diagwatch] started", elog.Any("interval", w.cfg.Interval), elog.Any("window", w.cfg.Window),
		elog.Int("min_risk", w.cfg.MinRisk), elog.Int("channels", len(w.cfg.Channels)))
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	w.checkOnce()
	for {
		select {
		case <-ticker.C:
			w.checkOnce()
		case <-w.stop:
			w.logger.Info("[diagwatch] stopped")
			return
		}
	}
}

// checkOnce 全租户一次诊断扫描。
func (w *Watcher) checkOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), w.cfg.Interval-5*time.Second)
	defer cancel()
	grouped, err := w.tenantAccounts(ctx)
	if err != nil {
		w.logger.Warn("[diagwatch] list accounts failed", elog.FieldErr(err))
		return
	}
	for tenant, clouds := range grouped {
		w.diagnoseTenant(ctx, tenant, clouds)
	}
}

// tenantAccounts List 全量账号并按租户分组,返回 租户 → 云列表(排序稳定)。
func (w *Watcher) tenantAccounts(ctx context.Context) (map[int64][]cloudxdomain.CloudProvider, error) {
	accounts, _, err := w.accounts.List(ctx, cloudxdomain.CloudAccountFilter{})
	if err != nil {
		return nil, err
	}
	grouped := map[int64][]cloudxdomain.CloudProvider{}
	for _, acc := range accounts {
		if acc.Status != cloudxdomain.CloudAccountStatusActive {
			continue
		}
		grouped[acc.TenantID] = append(grouped[acc.TenantID], acc.Provider)
	}
	for k := range grouped {
		sort.Slice(grouped[k], func(i, j int) bool { return grouped[k][i] < grouped[k][j] })
	}
	return grouped, nil
}

func (w *Watcher) diagnoseTenant(ctx context.Context, tenant int64, clouds []cloudxdomain.CloudProvider) {
	now := time.Now()
	resp, err := w.svc.Diagnose(ctx, tenant, service.DiagnoseRequest{
		LogType:  "waf",
		StartTime: now.Add(-w.cfg.Window).UnixMilli(),
		EndTime:   now.UnixMilli(),
		Clouds:    clouds,
	})
	if err != nil {
		w.logger.Warn("[diagwatch] diagnose failed", elog.Int64("tenant", tenant), elog.FieldErr(err))
		return
	}
	rank := RiskRank(resp.Result.RiskLevel)
	w.mu.Lock()
	prevRank := w.last[tenant]
	w.mu.Unlock()
	if rank < w.cfg.MinRisk || rank <= prevRank {
		return // 低于门槛 / 未升档:不打扰
	}
	if err := w.notify(ctx, tenant, resp); err != nil {
		w.logger.Warn("[diagwatch] notify failed", elog.Int64("tenant", tenant), elog.FieldErr(err))
	}
	w.mu.Lock()
	w.last[tenant] = rank
	w.mu.Unlock()
}

func (w *Watcher) notify(ctx context.Context, tenant int64, resp *service.DiagnoseResponse) error {
	if len(w.cfg.Channels) == 0 {
		return errors.New("no channels configured")
	}
	msg := buildMessage(resp)
	var errs []string
	for _, ch := range w.cfg.Channels {
		sender, err := channel.NewSender(ch)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := sender.Send(ctx, msg); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, ";"))
	}
	return nil
}

// buildMessage 诊断结果 → 通知消息(仅聚合指标与措施,无原始日志)。
func buildMessage(resp *service.DiagnoseResponse) *channel.Message {
	var b strings.Builder
	fmt.Fprintf(&b, "窗口 %d 秒 · 共 %d 次请求\n", resp.WindowSec, resp.Total)
	if resp.Prev != nil {
		delta := "前窗不可比"
		if resp.Prev.Total > 0 && resp.Result.SurgeMultiplier > 0 {
			delta = fmt.Sprintf("前窗 %d 次,突增 %.2f 倍", resp.Prev.Total, resp.Result.SurgeMultiplier)
		}
		b.WriteString(delta + "\n")
	}
	if len(resp.Result.TopSources) > 0 {
		b.WriteString("\nTop 来源 IP:")
		for _, s := range resp.Result.TopSources[:min(len(resp.Result.TopSources), 3)] {
			fmt.Fprintf(&b, "\n- %s (%d 次, %.1f%%)", s.IP, s.Count, s.Share*100)
		}
	}
	if resp.Summary != "" {
		fmt.Fprintf(&b, "\n\n%s", resp.Summary)
	}
	for i, m := range resp.Result.Measures {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&b, "\n%d. %s", i+1, m)
	}
	sev := alertdomain.SeverityWarning
	if RiskRank(resp.Result.RiskLevel) >= RiskRank("high") {
		sev = alertdomain.SeverityCritical
	}
	return &channel.Message{
		Title:    fmt.Sprintf("[WAF 诊断] %s 风险 - %s", resp.Result.RiskLevel, resp.Result.AttackType),
		Content:  b.String(),
		Severity: sev,
		Markdown: true,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}