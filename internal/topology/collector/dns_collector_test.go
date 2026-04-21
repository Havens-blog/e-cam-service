package collector

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/stretchr/testify/assert"
)

func TestDNSCollector_resolveFirstHop_CNAME_CDN(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "CNAME", Value: "api.cdn.aliyuncs.com",
	})
	assert.Contains(t, id, "cdn-")
	assert.Equal(t, domain.NodeTypeCDN, typ)
}

func TestDNSCollector_resolveFirstHop_CNAME_CloudFront(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "CNAME", Value: "d1234.cloudfront.net",
	})
	assert.Contains(t, id, "cdn-")
	assert.Equal(t, domain.NodeTypeCDN, typ)
}

func TestDNSCollector_resolveFirstHop_CNAME_WAF(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "CNAME", Value: "xxx.yundunwaf.com",
	})
	assert.Contains(t, id, "waf-")
	assert.Equal(t, domain.NodeTypeWAF, typ)
}

func TestDNSCollector_resolveFirstHop_CNAME_OSS(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "CNAME", Value: "bucket.oss-cn-hangzhou.aliyuncs.com",
	})
	assert.Contains(t, id, "oss-")
	assert.Equal(t, domain.NodeTypeOSS, typ)
}

func TestDNSCollector_resolveFirstHop_CNAME_S3(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "CNAME", Value: "bucket.s3.amazonaws.com",
	})
	assert.Contains(t, id, "oss-")
	assert.Equal(t, domain.NodeTypeOSS, typ)
}

func TestDNSCollector_resolveFirstHop_CNAME_External(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "CNAME", Value: "api.thirdparty.com",
	})
	assert.Contains(t, id, "ext-")
	assert.Equal(t, domain.NodeTypeExternal, typ)
}

func TestDNSCollector_resolveFirstHop_A_Record(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "A", Value: "47.96.1.1",
	})
	assert.Equal(t, "ip-47.96.1.1", id)
	assert.Equal(t, domain.NodeTypeUnknown, typ)
}

func TestDNSCollector_resolveFirstHop_Unsupported(t *testing.T) {
	c := &DNSCollector{}
	id, typ := c.resolveFirstHop(DNSRecord{
		RecordType: "MX", Value: "mail.example.com",
	})
	assert.Empty(t, id)
	assert.Empty(t, typ)
}

func TestSanitizeID(t *testing.T) {
	assert.Equal(t, "api-cdn-aliyuncs-com", sanitizeID("api.cdn.aliyuncs.com"))
	assert.Equal(t, "47-96-1-1", sanitizeID("47.96.1.1"))
}

func TestMatchesSuffix(t *testing.T) {
	assert.True(t, matchesSuffix("d1234.cloudfront.net", cdnDomainSuffixes))
	assert.True(t, matchesSuffix("api.cdn.aliyuncs.com", cdnDomainSuffixes))
	assert.False(t, matchesSuffix("api.example.com", cdnDomainSuffixes))
	assert.True(t, matchesSuffix("xxx.yundunwaf.com", wafDomainSuffixes))
	assert.True(t, matchesSuffix("bucket.oss-cn-hangzhou.aliyuncs.com", ossDomainSuffixes))
}
