package volcengine

import (
	"math"
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// mapAccessLog 火山自采 nginx/gateway 访问日志 -> SLBLogEntry。
//
// TLS 投递的实例访问日志(采集管道展开,字段全字符串)与阿里 ALB nginx schema
// 同构:http_host/method/uri/request_uri/status/request_time/upstream_response_time/
// body_bytes_send/request_length/remote_addr/x_forwarded_for/cdn_src_ip/protocol。
//
// 时间戳:__time__(unix ms 字符串)优先,@timestamp(ISO)兜底;datetime 是节点本地
// 时间(+0800 标签下数值与 UTC 实际不一致),不作时间戳。protocol 仅含版本("1.1"),
// 拼 "HTTP/" 前缀对齐统一模型。request_time/upstream_response_time 为秒(字符串,
// 可能 "0.123"),转毫秒。
func mapAccessLog(m logquery.LogMeta, raw map[string]any) *logquery.SLBLogEntry {
	protocol := logquery.Str(raw["protocol"])
	if protocol != "" && !strings.HasPrefix(protocol, "HTTP/") {
		protocol = "HTTP/" + protocol
	}
	e := &logquery.SLBLogEntry{
		Meta:              m,
		Timestamp:         logquery.ParseTimeMs(firstNonEmpty(raw["__time__"], raw["@timestamp"])),
		ClientIP:          clientIP(raw),
		Host:              logquery.Str(raw["http_host"]),
		Method:            logquery.Str(raw["method"]),
		URL:               requestURI(raw),
		Protocol:          protocol,
		Status:            int(logquery.Int(raw["status"])),
		RequestLength:     logquery.Int(raw["request_length"]),
		BytesSent:         logquery.Int(raw["body_bytes_send"]),
		LatencyMs:         secondsToMs(logquery.Str(raw["request_time"])),
		UpstreamLatencyMs: secondsToMs(logquery.Str(raw["upstream_response_time"])),
		RequestID:         logquery.Str(firstNonEmpty(raw["upstream_trace_id"], raw["j_trace_id"])),
		Raw:               raw,
	}
	if addr := logquery.Str(raw["upstream_addr"]); addr != "" && addr != "-" {
		ip, port := splitAddr(addr)
		e.TargetIP, e.TargetPort = ip, port
	}
	return e
}

// clientIP 真实客户端 IP:x_forwarded_for 首段(反代注入)优先,cdn_src_ip 兜底,
// 再退回 remote_addr(直连对端)。
func clientIP(raw map[string]any) string {
	if xff := logquery.Str(raw["x_forwarded_for"]); xff != "" && xff != "-" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if cdn := logquery.Str(raw["cdn_src_ip"]); cdn != "" && cdn != "-" {
		return cdn
	}
	return logquery.Str(raw["remote_addr"])
}

// requestURI 完整请求 URI:request_uri(path+query) 优先,uri+uri_param 兜底。
func requestURI(raw map[string]any) string {
	if u := logquery.Str(raw["request_uri"]); u != "" {
		return u
	}
	u := logquery.Str(raw["uri"])
	if p := logquery.Str(raw["uri_param"]); p != "" {
		u += "?" + p
	}
	return u
}

// secondsToMs 秒字符串("-"/空为 0)转毫秒,浮点四舍五入。
func secondsToMs(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 1000))
}

// splitAddr 拆后端地址 "ip:port"(多后端取首个)。
func splitAddr(addr string) (string, int) {
	addr = strings.TrimSpace(strings.Split(addr, ",")[0])
	i := strings.LastIndexByte(addr, ':')
	if i <= 0 {
		return addr, 0
	}
	port, _ := strconv.Atoi(addr[i+1:])
	return addr[:i], port
}

// firstNonEmpty 取首个非空值(用于字段回退)。
func firstNonEmpty(vs ...any) any {
	for _, v := range vs {
		if s := logquery.Str(v); s != "" && s != "-" {
			return v
		}
	}
	return vs[len(vs)-1]
}