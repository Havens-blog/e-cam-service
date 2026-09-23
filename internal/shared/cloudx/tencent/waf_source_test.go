package tencent

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	waf "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/waf/v20180125"
)

func TestConvertWAFDomainToInstanceUpstreamDomainList(t *testing.T) {
	// 列表接口兜底:UpstreamDomainList(域名回源)应并入 SourceIPs
	d := &waf.DomainInfo{
		Domain:             strPtr("example.com"),
		DomainId:           strPtr("waf-123"),
		Edition:            strPtr("sparta-waf"),
		State:              func() *int64 { v := int64(1); return &v }(),
		SrcList:            []*string{strPtr("1.2.3.4")},
		UpstreamDomainList: []*string{strPtr("origin.example.com")},
		Cname:              strPtr("example.com.cname"),
	}

	a := &WAFAdapter{}
	inst := a.convertWAFDomainToInstance(d, "ap-guangzhou")

	if len(inst.SourceIPs) != 2 {
		t.Fatalf("期望 SourceIPs 有 2 项(IP + 域名回源),实际 %d: %v", len(inst.SourceIPs), inst.SourceIPs)
	}
	if inst.SourceIPs[0] != "1.2.3.4" || inst.SourceIPs[1] != "origin.example.com" {
		t.Fatalf("SourceIPs 顺序错误: %v", inst.SourceIPs)
	}
	if inst.InstanceID != "waf-123" {
		t.Fatalf("InstanceID 错误: %s", inst.InstanceID)
	}
}

func TestMergeSaaSSources(t *testing.T) {
	t.Run("IP 回源 + 域名回源合并", func(t *testing.T) {
		detail := &waf.DomainsPartInfo{
			SrcList:        []*string{strPtr("10.0.0.1"), strPtr("10.0.0.2")},
			UpstreamDomain: strPtr("backend.example.com"),
			UpstreamType:   func() *uint64 { v := uint64(1); return &v }(),
			Cname:          strPtr("waf-cname.example.com"),
			CreateTime:     strPtr("2024-01-01 00:00:00"),
		}
		inst := &types.WAFInstance{Description: "WAF防护域名"}
		mergeSaaSSources(detail, inst)

		if len(inst.SourceIPs) != 3 {
			t.Fatalf("期望 SourceIPs 3 项,实际 %d: %v", len(inst.SourceIPs), inst.SourceIPs)
		}
		if inst.SourceIPs[2] != "backend.example.com" {
			t.Fatalf("域名回源未并入: %v", inst.SourceIPs)
		}
		if inst.Cname != "waf-cname.example.com" {
			t.Fatalf("Cname 未覆盖: %s", inst.Cname)
		}
		if inst.CreationTime != "2024-01-01 00:00:00" {
			t.Fatalf("CreationTime 未覆盖: %s", inst.CreationTime)
		}
		if inst.Description != "WAF防护域名 upstream_type=domain" {
			t.Fatalf("Description 未标注域名回源: %s", inst.Description)
		}
	})

	t.Run("仅 IP 回源(UpstreamType=0)不改 Description", func(t *testing.T) {
		detail := &waf.DomainsPartInfo{
			SrcList:      []*string{strPtr("10.0.0.1")},
			UpstreamType: func() *uint64 { v := uint64(0); return &v }(),
		}
		inst := &types.WAFInstance{Description: "WAF防护域名"}
		mergeSaaSSources(detail, inst)

		if inst.Description != "WAF防护域名" {
			t.Fatalf("IP 回源不应改 Description: %s", inst.Description)
		}
		if len(inst.SourceIPs) != 1 {
			t.Fatalf("期望 SourceIPs 1 项,实际 %d", len(inst.SourceIPs))
		}
	})

	t.Run("空详情不覆盖已有源站", func(t *testing.T) {
		detail := &waf.DomainsPartInfo{}
		inst := &types.WAFInstance{SourceIPs: []string{"1.2.3.4"}}
		mergeSaaSSources(detail, inst)
		if len(inst.SourceIPs) != 1 || inst.SourceIPs[0] != "1.2.3.4" {
			t.Fatalf("空详情不应清空源站: %v", inst.SourceIPs)
		}
	})
}

func TestMergeCLBSources(t *testing.T) {
	t.Run("关联 LB 写入 Description", func(t *testing.T) {
		detail := &waf.ClbDomainsInfo{
			LoadBalancerSet: []*waf.LoadBalancerPackageNew{
				{
					LoadBalancerId:   strPtr("lb-abc"),
					LoadBalancerName: strPtr("my-lb"),
					ListenerId:       strPtr("lbl-xyz"),
					ListenerName:     strPtr("https-443"),
					Protocol:         strPtr("https"),
				},
			},
		}
		inst := &types.WAFInstance{Description: "WAF防护域名"}
		mergeCLBSources(detail, &waf.DomainInfo{}, inst)

		if len(inst.SourceIPs) != 0 {
			t.Fatalf("CLB 型 SourceIPs 应留空,实际 %v", inst.SourceIPs)
		}
		if inst.Description == "WAF防护域名" {
			t.Fatalf("CLB 关联 LB 未写入 Description")
		}
		if inst.Description != "WAF防护域名(CLB) clb=my-lb(lb-abc) listener=https-443 protocol=https" {
			t.Fatalf("Description 拼装错误: %s", inst.Description)
		}
	})

	t.Run("详情无 LB 时回退列表 LB", func(t *testing.T) {
		detail := &waf.ClbDomainsInfo{}
		list := &waf.DomainInfo{
			LoadBalancerSet: []*waf.LoadBalancerPackageNew{
				{LoadBalancerId: strPtr("lb-fallback"), LoadBalancerName: strPtr("fallback-lb")},
			},
		}
		inst := &types.WAFInstance{Description: "WAF防护域名"}
		mergeCLBSources(detail, list, inst)

		if inst.Description != "WAF防护域名(CLB) clb=fallback-lb(lb-fallback)" {
			t.Fatalf("回退 LB 拼装错误: %s", inst.Description)
		}
	})

	t.Run("无任何 LB 不改 Description", func(t *testing.T) {
		detail := &waf.ClbDomainsInfo{}
		inst := &types.WAFInstance{Description: "WAF防护域名"}
		mergeCLBSources(detail, &waf.DomainInfo{}, inst)
		if inst.Description != "WAF防护域名" {
			t.Fatalf("无 LB 不应改 Description: %s", inst.Description)
		}
	})
}