package dns

import "strings"

// CDN 域名后缀列表（按 fleet DNS 记录实测分布校准，2026-08：
// kunluncan/cdngslb=阿里云 CDN、edgesuite/akamaiedge=Akamai、
// cloudfront=AWS CloudFront；华为云 CDN CNAME 域名无固定后缀，见 cdnPrefixes）。
var cdnSuffixes = []string{
	".cdn.aliyuncs.com",
	".kunluncan.com",
	".w.cdngslb.com",
	".cdngslb.com",
	".cloudfront.net",
	".cdn.myqcloud.com",
	".cdn.volcengineapi.com",
	".cdn.hwcloudcdn.com",
	".edgesuite.net",
	".akamaiedge.net",
}

// CDN 域名标签前缀：华为云 CDN 的 CNAME 目标域名为 "cdnhw"+随机段（如
// cdnhwcxcy07.com），无固定后缀可枚举，按标签前缀识别。
var cdnLabelPrefixes = []string{"cdnhw"}

// WAF 域名后缀列表（aliyunwaf3/yundunwaf*=阿里云 WAF CNAME 接入域名）。
var wafSuffixes = []string{
	".waf.aliyuncs.com",
	".aliyunwaf3.com",
	".yundunwaf.com",
	".yundunwaf2.com",
	".yundunwaf3.com",
	".yundunwaf4.com",
	".yundunwaf5.com",
	".waf.tencentcloudwaf.com",
}

// ResourceLinker CNAME/A 记录关联资源识别器。
//
// 识别结果三态（cert probe 链路分层契约）：
//   - cdn：CNAME 指向已知 CDN 加速域名
//   - waf：CNAME 指向已知 WAF CNAME 接入域名
//   - external：A/AAAA 直连 IP，或 CNAME 指向非 CDN/WAF 目标（源站/外部，
//     含 ALB/NLB 端点域名--cert probe 侧经 ALB served-domains 索引对齐）
//   - nil：非 CNAME/A/AAAA 记录类型（MX/TXT/NS 等非拨测目标）
type ResourceLinker struct {
	cdnSuffixes      []string
	cdnLabelPrefixes []string
	wafSuffixes      []string
}

// NewResourceLinker 创建关联资源识别器。
func NewResourceLinker() *ResourceLinker {
	return &ResourceLinker{
		cdnSuffixes:      cdnSuffixes,
		cdnLabelPrefixes: cdnLabelPrefixes,
		wafSuffixes:      wafSuffixes,
	}
}

// Identify 根据记录类型和值识别关联资源。
func (l *ResourceLinker) Identify(recordType, value string) *LinkedResource {
	switch recordType {
	case "CNAME":
		return l.identifyCNAME(value)
	case "A", "AAAA":
		return l.identifyA(value)
	default:
		return nil
	}
}

// identifyCNAME 识别 CNAME 记录关联的资源：已知 CDN/WAF 域名精确归类，
// 其余（含 ALB/SLB 端点、任意外部目标）统一 external。
func (l *ResourceLinker) identifyCNAME(value string) *LinkedResource {
	lower := strings.ToLower(strings.TrimSpace(value))
	lower = strings.TrimSuffix(lower, ".") // CNAME 值可带根域点后缀
	for _, suffix := range l.cdnSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return &LinkedResource{Type: "cdn", Name: value}
		}
	}
	for _, labelPrefix := range l.cdnLabelPrefixes {
		// 华为云 CDN 前缀标签出现在目标域名任一标签（"foo.cdnhwcxcy07.com"）
		if strings.Contains(lower, "."+labelPrefix) || strings.HasPrefix(lower, labelPrefix) {
			return &LinkedResource{Type: "cdn", Name: value}
		}
	}
	for _, suffix := range l.wafSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return &LinkedResource{Type: "waf", Name: value}
		}
	}
	return &LinkedResource{Type: "external", Name: value}
}

// identifyA 识别 A/AAAA 记录关联的资源：直连 IP 统一 external（源站/外部；
// cert probe 侧对 external 目标查 ALB/NLB served-domains 索引做资源级对齐）。
func (l *ResourceLinker) identifyA(value string) *LinkedResource {
	return &LinkedResource{Type: "external", Name: value}
}
