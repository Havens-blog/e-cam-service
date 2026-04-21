package dns

import "strings"

// CDN 域名后缀列表
var cdnSuffixes = []string{
	".cdn.aliyuncs.com",
	".cloudfront.net",
	".cdn.myqcloud.com",
	".cdn.volcengineapi.com",
	".cdn.hwcloudcdn.com",
}

// WAF 域名后缀列表
var wafSuffixes = []string{
	".waf.aliyuncs.com",
	".waf.tencentcloudwaf.com",
}

// ResourceLinker CNAME/A 记录关联资源识别器
type ResourceLinker struct {
	cdnSuffixes []string
	wafSuffixes []string
}

// NewResourceLinker 创建关联资源识别器
func NewResourceLinker() *ResourceLinker {
	return &ResourceLinker{
		cdnSuffixes: cdnSuffixes,
		wafSuffixes: wafSuffixes,
	}
}

// Identify 根据记录类型和值识别关联资源
func (l *ResourceLinker) Identify(recordType, value string) *LinkedResource {
	switch recordType {
	case "CNAME":
		return l.identifyCNAME(value)
	case "A":
		return l.identifyA(value)
	default:
		return nil
	}
}

// identifyCNAME 识别 CNAME 记录关联的资源
func (l *ResourceLinker) identifyCNAME(value string) *LinkedResource {
	lower := strings.ToLower(value)
	for _, suffix := range l.cdnSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return &LinkedResource{
				Type: "cdn",
				Name: value,
			}
		}
	}
	for _, suffix := range l.wafSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return &LinkedResource{
				Type: "waf",
				Name: value,
			}
		}
	}
	return nil
}

// identifyA 识别 A 记录关联的资源（简化实现：暂不匹配 SLB/EIP）
func (l *ResourceLinker) identifyA(_ string) *LinkedResource {
	// 简化实现：A 记录需要与已同步的 SLB/EIP 地址匹配
	// 当前版本暂不实现，返回 nil
	return nil
}
