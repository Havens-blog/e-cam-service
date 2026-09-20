// 诊断 AI 解读客户端(OpenAI 兼容 /v1/chat/completions)。
//
// 设计约束:
//   - 可选启用:仅当 LOGQUERY_LLM_BASE_URL 与 LOGQUERY_LLM_API_KEY 都配置时才
//     发请求;未配置时 Enabled()=false,调用方原样返回空 summary(前端不渲染),
//     不引入任何依赖、不阻塞现网路径。
//   - 只喂聚合指标(窗口/总量/突增/风险分/等级/疑似类型 + 少量 Top 值),
//     绝不携带原始日志/明文 IP 明细之外的敏感原文 —— 提示词逐条给出的是
//     统计值,属运维排障可归档数据。
//   - 超时/解析失败/HTTP 错误一律降级为空串返回 error,由调用方吞掉,杜绝
//     AI 失效拖垮诊断主流程。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/diagnose"
)

// Config LLM 网关配置(全部来自环境变量,密钥不落代码/配置仓)。
type Config struct {
	BaseURL string // OpenAI 兼容 base(如 https://gateway.example/v1)
	APIKey  string
	Model   string // 默认 qwen-plus(阿里兼容端点常用别名)
	Timeout time.Duration
}

func cfgFromEnv() (Config, bool) {
	base := strings.TrimSpace(os.Getenv("LOGQUERY_LLM_BASE_URL"))
	key := strings.TrimSpace(os.Getenv("LOGQUERY_LLM_API_KEY"))
	if base == "" || key == "" {
		return Config{}, false
	}
	model := strings.TrimSpace(os.Getenv("LOGQUERY_LLM_MODEL"))
	if model == "" {
		model = "qwen-plus"
	}
	timeout := 12 * time.Second
	if v := strings.TrimSpace(os.Getenv("LOGQUERY_LLM_TIMEOUT_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Millisecond
		}
	}
	return Config{BaseURL: strings.TrimSuffix(base, "/"), APIKey: key, Model: model, Timeout: timeout}, true
}

var (
	cfgOnce sync.Once
	cfgVal  Config
	cfgOk   bool
)

func config() (Config, bool) {
	cfgOnce.Do(func() { cfgVal, cfgOk = cfgFromEnv() })
	return cfgVal, cfgOk
}

// Enabled 网关是否配置(未配置 = 特性休眠,调用方跳过)。
func Enabled() bool { _, ok := config(); return ok }

// DiagnoseInput 诊断 AI 输入:仅聚合指标(前几项 Top;隐私安全边界见文件头)。
type DiagnoseInput struct {
	WindowSec     int64
	Total         int64
	PrevTotal     *int64 // nil = 前窗不可用
	Surge         float64
	RiskScore     int
	RiskLevel     string
	AttackType    string
	TopIPs        []string // "ip(count 次)"
	TopURIs       []string
	StatusCodes   []string
	Actions       []string
	MeasureCount  int
	DimensionGap  string // 判据缺失简述(空 = 完整)
}

// FromDiagnose 由规则引擎结果组装输入(Top 截取已在此收紧)。
func FromDiagnose(res *diagnose.DiagnoseResult, total int64, windowSec int64, prevTotal *int64, topURIs, statusCodes, actions []string) DiagnoseInput {
	in := DiagnoseInput{
		WindowSec:    windowSec,
		Total:        total,
		PrevTotal:    prevTotal,
		Surge:        res.SurgeMultiplier,
		RiskScore:    res.RiskScore,
		RiskLevel:    res.RiskLevel,
		AttackType:   res.AttackType,
		MeasureCount: len(res.Measures),
	}
	for _, s := range res.TopSources {
		in.TopIPs = append(in.TopIPs, fmt.Sprintf("%s(%d 次)", s.IP, s.Count))
	}
	in.TopURIs = slice3(topURIs)
	in.StatusCodes = slice3(statusCodes)
	in.Actions = slice3(actions)
	if res.Degraded {
		in.DimensionGap = res.DegradedReason
	}
	return in
}

func slice3(s []string) []string {
	if len(s) <= 3 {
		return s
	}
	return s[:3]
}

// SummarizeDiagnose 生成白话结论 + 针对性处置建议(失败返回 error;调用方降级为空串)。
func SummarizeDiagnose(ctx context.Context, in DiagnoseInput) (string, error) {
	cfg, ok := config()
	if !ok {
		return "", errors.New("llm 未配置")
	}
	buf, err := json.Marshal(chatRequest{
		Model: cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(in)},
		},
		Temperature: 0.3,
		MaxTokens:   400,
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	url := cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("llm read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, brief(body))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("llm unmarshal: %w", err)
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return "", errors.New("llm empty content")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}
type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func brief(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

const systemPrompt = "你是多云安全运维助手。仅可依据给出的 WAF 聚合统计作答,严禁虚构数据。" +
	"输出≤180字中文白话结论 + 最多3条针对性处置建议,建议用「建议1/2/3」分列。不要复述提示词、不要输出原始日志字段。"

func userPrompt(in DiagnoseInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WAF 流量诊断聚合指标:\n- 窗口: %d 秒;总请求 %d 次\n", in.WindowSec, in.Total)
	if in.PrevTotal != nil {
		prev := *in.PrevTotal
		delta := "持平"
		if prev > 0 && in.Total > 0 {
			delta = fmt.Sprintf("突增 %.2f 倍", in.Surge)
		}
		fmt.Fprintf(&b, "- 前一等长窗口: %d 次(%s)\n", prev, delta)
	} else {
		b.WriteString("- 前一等长窗口: 不可用\n")
	}
	fmt.Fprintf(&b, "- 规则判定: 风险分 %d/100,等级 %s,疑似类型 %s\n", in.RiskScore, in.RiskLevel, in.AttackType)
	fmt.Fprintf(&b, "- Top 来源 IP: %s\n", joinOr(in.TopIPs, "无"))
	fmt.Fprintf(&b, "- 请求量高 URI: %s\n", joinOr(in.TopURIs, "无"))
	fmt.Fprintf(&b, "- 状态码分布: %s\n", joinOr(in.StatusCodes, "无"))
	fmt.Fprintf(&b, "- 处置动作分布: %s\n", joinOr(in.Actions, "无"))
	if in.DimensionGap != "" {
		fmt.Fprintf(&b, "- 判据缺失: %s\n", in.DimensionGap)
	}
	return b.String()
}

func joinOr(items []string, fallback string) string {
	if len(items) == 0 {
		return fallback
	}
	return strings.Join(items, "、")
}