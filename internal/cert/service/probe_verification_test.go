package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// TestIsVerificationAlias 站点验证类别名判定：仅 CNAME 记录有意义；
// 目标命中已知验证服务后缀才跳过（A 记录、真实云资源一律不跳过）。
func TestIsVerificationAlias(t *testing.T) {
	testCases := []struct {
		name        string
		recordType  string
		recordValue string
		want        bool
	}{
		{"Google Workspace 验证", "CNAME", "gv-c6vh3ouihmgdcp.dv.googlehosted.com", true},
		{"Bing 站长验证", "CNAME", "verify.bing.com", true},
		{"腾讯企业邮验证", "CNAME", "mail.qq.com", true},
		{"HelpScout 验证", "CNAME", "example.helpscout.net", true},
		{"百度站长验证", "CNAME", "abc.baidu.com", true},
		{"大小写与尾点归一", "cname", "GV-XX.DV.GoogleHosted.com.", true},
		{"真实 CDN 端点（不跳过）", "CNAME", "attachment.forface3d.com.c.vedcdnlb.com", false},
		{"阿里云 ALB（不跳过）", "CNAME", "alb-0afzsdssb1ot3dht21.eu-central-1.alb.aliyuncsslbintl.com", false},
		{"AWS ELB（不跳过）", "CNAME", "alpha-lb-1577234852.ap-northeast-1.elb.amazonaws.com", false},
		{"A 记录不看值", "A", "1.2.3.4", false},
		{"CNAME 空值", "CNAME", "", false},
		{"CNAME 值非验证服务", "CNAME", "cdn.example.com", false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isVerificationAlias(tc.recordType, tc.recordValue))
		})
	}
}

// TestProbeOneDNS_VerificationSkipped 验证类别名不拨测，且保留记录字段供排障。
func TestProbeOneDNS_VerificationSkipped(t *testing.T) {
	d := &fakeClassifyDialer{remoteIP: "113.96.128.9"}
	svc := &probeService{dialer: d}
	tgt := DNSProbeTarget{
		Hostname:    "23ecpui2h5t7.jlc3dp.com",
		RecordType:  "CNAME",
		RecordValue: "gv-c6vh3ouihmgdcp.dv.googlehosted.com",
		TenantID:    3,
	}
	r := svc.probeOneDNS(tgt, nil, nil, nil, nil, nil)
	assert.Equal(t, domain.ProbeStatusVerificationSkipped, r.Status)
	assert.Equal(t, 0, d.called, "验证类别名不得触发拨测")
	assert.Equal(t, "CNAME", r.RecordType, "保留记录类型供排障")
	assert.Equal(t, "gv-c6vh3ouihmgdcp.dv.googlehosted.com", r.RecordValue)
	assert.Equal(t, int64(3), r.TenantID)
}

// TestComputeProbeRates_VerificationSkipped 验证类别名计入跳过项、从分母扣除。
func TestComputeProbeRates_VerificationSkipped(t *testing.T) {
	s := DashboardSummary{
		ProbeTotalCount:          100,
		InternalCount:            10,
		ProtocolSkippedCount:     5,
		VerificationSkippedCount: 3,
		WildcardSkippedCount:     2,
		ProbeSuccessCount:        70,
		DiffAlertCount:           7,
	}
	computeProbeRates(&s)
	assert.Equal(t, 80, s.ProbeableCount, "100 - 10 - 5 - 3 - 2 = 80")
	assert.InDelta(t, 70.0/80.0, s.ReachableRateOnProbeable, 1e-9)
	assert.InDelta(t, 7.0/80.0, s.DiffRateOnProbeable, 1e-9)
}
