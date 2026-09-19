package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	volcanosdkalb "github.com/volcengine/volcengine-go-sdk/service/alb"
	volcanosdkcdn "github.com/volcengine/volcengine-go-sdk/service/cdn"
	volcanosdkclb "github.com/volcengine/volcengine-go-sdk/service/clb"
	volcanosdkwaf "github.com/volcengine/volcengine-go-sdk/service/waf"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	volcanocert "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/volcano"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 火山引用扫描适配器测试（cert-volcano-deployer 任务 3）：fake SDK 窄接口
// fakeVolcanoScanAPI 全注入——4 产品引用枚举 + 指纹解析命中/占位两态 +
// 失败隔离，零真实云调用。

// 编译期断言：火山扫描适配器满足 5 云只读扫描端口（AC1）。
var _ CloudScanAdapter = NewVolcanoScanAdapter(nil)

// fakeVolcanoScanAPI 火山引用扫描 SDK 窄接口 fake：按方法注入输出/错误；
// 未注入的方法返回空输出（nil error）。cdnCertInfoCalls 记录 ListCdnCertInfo
// 调用次数（GetCert 路由确实发起云侧查询的断言依据）。
type fakeVolcanoScanAPI struct {
	listCdnCertInfo      func(in *volcanosdkcdn.ListCdnCertInfoInput) (*volcanosdkcdn.ListCdnCertInfoOutput, error)
	listDomain           func(in *volcanosdkwaf.ListDomainInput) (*volcanosdkwaf.ListDomainOutput, error)
	describeListeners    func(in *volcanosdkalb.DescribeListenersInput) (*volcanosdkalb.DescribeListenersOutput, error)
	describeRules        func(in *volcanosdkalb.DescribeRulesInput) (*volcanosdkalb.DescribeRulesOutput, error)
	describeNLBListeners func(in *volcanosdkclb.DescribeNLBListenersInput) (*volcanosdkclb.DescribeNLBListenersOutput, error)

	cdnCertInfoCalls int
	wafRegionsSeen   []string
}

func (f *fakeVolcanoScanAPI) ListCdnCertInfoWithContext(_ context.Context, in *volcanosdkcdn.ListCdnCertInfoInput, _ ...request.Option) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
	f.cdnCertInfoCalls++
	if f.listCdnCertInfo == nil {
		return &volcanosdkcdn.ListCdnCertInfoOutput{}, nil
	}
	return f.listCdnCertInfo(in)
}

func (f *fakeVolcanoScanAPI) ListDomainWithContext(_ context.Context, in *volcanosdkwaf.ListDomainInput, _ ...request.Option) (*volcanosdkwaf.ListDomainOutput, error) {
	if in != nil && in.Region != nil {
		f.wafRegionsSeen = append(f.wafRegionsSeen, *in.Region)
	}
	if f.listDomain == nil {
		return &volcanosdkwaf.ListDomainOutput{}, nil
	}
	return f.listDomain(in)
}

func (f *fakeVolcanoScanAPI) DescribeListenersWithContext(_ context.Context, in *volcanosdkalb.DescribeListenersInput, _ ...request.Option) (*volcanosdkalb.DescribeListenersOutput, error) {
	if f.describeListeners == nil {
		return &volcanosdkalb.DescribeListenersOutput{}, nil
	}
	return f.describeListeners(in)
}

func (f *fakeVolcanoScanAPI) DescribeRulesWithContext(_ context.Context, in *volcanosdkalb.DescribeRulesInput, _ ...request.Option) (*volcanosdkalb.DescribeRulesOutput, error) {
	if f.describeRules == nil {
		return &volcanosdkalb.DescribeRulesOutput{}, nil
	}
	return f.describeRules(in)
}

func (f *fakeVolcanoScanAPI) DescribeNLBListenersWithContext(_ context.Context, in *volcanosdkclb.DescribeNLBListenersInput, _ ...request.Option) (*volcanosdkclb.DescribeNLBListenersOutput, error) {
	if f.describeNLBListeners == nil {
		return &volcanosdkclb.DescribeNLBListenersOutput{}, nil
	}
	return f.describeNLBListeners(in)
}

// volcanoAdapterWith 构造注入 fake 的火山扫描适配器（shim 后形态）。
func volcanoAdapterWith(api volcanoScanAPI) CloudScanAdapter {
	return (&volcanoScanAdapter{
		newAPI: func(*sharedomain.CloudAccount, string) (volcanoScanAPI, error) { return api, nil },
	}).shim()
}

// volcanoAPIByAccount 按账号名分发 fake 的客户端工厂（多账号失败隔离测试）。
func volcanoAPIByAccount(byName map[string]volcanoScanAPI) func(*sharedomain.CloudAccount, string) (volcanoScanAPI, error) {
	return func(creds *sharedomain.CloudAccount, _ string) (volcanoScanAPI, error) {
		if creds != nil {
			if api, ok := byName[creds.Name]; ok {
				return api, nil
			}
		}
		return &fakeVolcanoScanAPI{}, nil
	}
}

// =====================================================================
// AC1：对齐 5 云形态（cloud 标识 + 产品枚举 + 编译期端口断言）
// =====================================================================

// TestVolcanoScanAdapter_CloudPort 火山云标识与产品枚举（对齐五云 shim 冒烟口径）。
func TestVolcanoScanAdapter_CloudPort(t *testing.T) {
	a := volcanoAdapterWith(&fakeVolcanoScanAPI{})
	assert.Equal(t, domain.Cloud("volcano"), a.Cloud())
	assert.Equal(t,
		[]domain.Product{domain.ProductCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB},
		a.Products())
}

// =====================================================================
// AC2：4 产品引用枚举（域名粒度 / 复合监听 ID / served domains 展开）
// =====================================================================

// TestVolcanoScanAdapter_CDNReferences CDN 域名粒度：按配置域名展开引用、
// 未配置证书的证书不产出引用、归一 {product}:{id} 云证书 ID、分页遍历。
func TestVolcanoScanAdapter_CDNReferences(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		listCdnCertInfo: func(in *volcanosdkcdn.ListCdnCertInfoInput) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
			out := &volcanosdkcdn.ListCdnCertInfoOutput{}
			switch volcengine.Int64Value(in.PageNum) {
			case 1:
				out.CertInfo = []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{
					{
						CertId: volcengine.String("cert-c1"),
						ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
							{Domain: volcengine.String("d1.example.com")},
							{Domain: volcengine.String("d2.example.com")},
						},
					},
					{CertId: volcengine.String("cert-c2")}, // 未配置域名：不构成引用
				}
			case 2:
				out.CertInfo = []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{
					{
						CertId:                 volcengine.String("cert-c3"),
						ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{{Domain: volcengine.String("d3.example.com")}},
					},
				}
			default:
				out.CertInfo = nil
			}
			return out, nil
		},
	}
	a := (&volcanoScanAdapter{
		newAPI:   func(*sharedomain.CloudAccount, string) (volcanoScanAPI, error) { return api, nil },
		pageSize: 2,
	}).shim()

	refs, err := a.ListReferences(context.Background(),
		&sharedomain.CloudAccount{Name: "acc-1"}, domain.ProductCDN)
	require.NoError(t, err)
	require.Len(t, refs, 3)
	byDomain := map[string]DiscoveredRef{}
	for _, r := range refs {
		byDomain[r.ResourceID] = r
	}
	for domain_, certID := range map[string]string{
		"d1.example.com": "cdn:cert-c1",
		"d2.example.com": "cdn:cert-c1",
		"d3.example.com": "cdn:cert-c3",
	} {
		r := byDomain[domain_]
		require.NotNil(t, r, domain_)
		assert.Equal(t, "volcano", r.Cloud)
		assert.Equal(t, "cdn", r.Product)
		assert.Equal(t, certID, r.ReferencedCloudCertID, "云证书 ID 归一 {product}:{id}")
		assert.Equal(t, "acc-1", r.AccountKey)
	}
	assert.Equal(t, 2, api.cdnCertInfoCalls, "pageSize=2 → 两页取尽")
}

// TestVolcanoScanAdapter_WAFReferences WAF 域名粒度：ListDomain 内联
// CertificateID、未配置证书（0 值）跳过、按账号地域遍历、归一前缀。
func TestVolcanoScanAdapter_WAFReferences(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		listDomain: func(in *volcanosdkwaf.ListDomainInput) (*volcanosdkwaf.ListDomainOutput, error) {
			out := &volcanosdkwaf.ListDomainOutput{}
			switch *in.Region {
			case "cn-beijing":
				out.Data = []*volcanosdkwaf.DataForListDomainOutput{
					{Domain: volcengine.String("w1.example.com"), CertificateID: volcengine.Int32(101)},
					{Domain: volcengine.String("w2.example.com"), CertificateID: volcengine.Int32(0)}, // 未配置
					{Domain: volcengine.String("w3.example.com")},                                     // 缺字段：未配置
				}
			case "cn-shanghai":
				out.Data = []*volcanosdkwaf.DataForListDomainOutput{
					{Domain: volcengine.String("w4.example.com"), CertificateID: volcengine.Int32(102)},
				}
			}
			return out, nil
		},
	}
	creds := &sharedomain.CloudAccount{Name: "acc-1", Regions: []string{"cn-beijing", "cn-shanghai"}}
	refs, err := volcanoAdapterWith(api).ListReferences(context.Background(), creds, domain.ProductWAF)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	byDomain := map[string]DiscoveredRef{}
	for _, r := range refs {
		byDomain[r.ResourceID] = r
	}
	assert.Equal(t, "waf:101", byDomain["w1.example.com"].ReferencedCloudCertID)
	assert.Equal(t, "waf:102", byDomain["w4.example.com"].ReferencedCloudCertID)
	assert.Equal(t, []string{"cn-beijing", "cn-shanghai"}, api.wafRegionsSeen, "按账号地域遍历")
}

// TestVolcanoScanAdapter_ALBReferences ALB 监听复合 ID + 主/SNI 证书展开 +
// served domains（DescribeRules Host 条件）；HTTP 监听与 SNI 重复证书跳过。
func TestVolcanoScanAdapter_ALBReferences(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		describeListeners: func(*volcanosdkalb.DescribeListenersInput) (*volcanosdkalb.DescribeListenersOutput, error) {
			return &volcanosdkalb.DescribeListenersOutput{
				Listeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{
					{
						ListenerId:     volcengine.String("lsn-1"),
						LoadBalancerId: volcengine.String("lb-1"),
						Protocol:       volcengine.String("https"),
						CertificateId:  volcengine.String("cert-default"),
						DomainExtensions: []*volcanosdkalb.DomainExtensionForDescribeListenersOutput{
							{Domain: volcengine.String("sni.example.com"), CertificateId: volcengine.String("cert-sni")},
							{Domain: volcengine.String("dup.example.com"), CertificateId: volcengine.String("cert-default")}, // 与主证书同 ID：跳过
						},
					},
					{ // HTTP 监听无服务器证书
						ListenerId:     volcengine.String("lsn-2"),
						LoadBalancerId: volcengine.String("lb-1"),
						Protocol:       volcengine.String("http"),
						CertificateId:  volcengine.String("cert-http"),
					},
				},
			}, nil
		},
		describeRules: func(in *volcanosdkalb.DescribeRulesInput) (*volcanosdkalb.DescribeRulesOutput, error) {
			assert.Equal(t, "lsn-1", volcengine.StringValue(in.ListenerId))
			return &volcanosdkalb.DescribeRulesOutput{
				Rules: []*volcanosdkalb.RuleForDescribeRulesOutput{
					{RuleConditions: []*volcanosdkalb.RuleConditionForDescribeRulesOutput{
						{Type: volcengine.String("host"), HostConfig: &volcanosdkalb.HostConfigForDescribeRulesOutput{
							Values: []*string{volcengine.String("a.example.com"), volcengine.String("b.example.com")},
						}},
					}},
					{RuleConditions: []*volcanosdkalb.RuleConditionForDescribeRulesOutput{
						{Type: volcengine.String("path"), PathConfig: &volcanosdkalb.PathConfigForDescribeRulesOutput{
							Values: []*string{volcengine.String("/ignored")},
						}},
					}},
				},
			}, nil
		},
	}
	refs, err := volcanoAdapterWith(api).ListReferences(context.Background(),
		&sharedomain.CloudAccount{Name: "acc-1"}, domain.ProductALB)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	byCert := map[string]DiscoveredRef{}
	for _, r := range refs {
		byCert[r.ReferencedCloudCertID] = r
	}
	def := byCert["alb:cert-default"]
	require.NotNil(t, def)
	assert.Equal(t, "lb-1/lsn-1", def.ResourceID, "监听复合 ID {lbId}/{listenerId}")
	assert.Equal(t, []string{"a.example.com", "b.example.com"}, def.ServedDomains)
	sni := byCert["alb:cert-sni"]
	require.NotNil(t, sni)
	assert.Equal(t, "lb-1/lsn-1", sni.ResourceID)
	assert.Equal(t, []string{"a.example.com", "b.example.com"}, sni.ServedDomains)
	assert.NotContains(t, byCert, "alb:cert-http", "HTTP 监听不产出引用")
}

// TestVolcanoScanAdapter_ALBRulesFailure 单监听规则查询失败不阻塞引用枚举
// （served 置空，回退 coverage 语义；对齐 aliyun listALBServedDomains 口径）。
func TestVolcanoScanAdapter_ALBRulesFailure(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		describeListeners: func(*volcanosdkalb.DescribeListenersInput) (*volcanosdkalb.DescribeListenersOutput, error) {
			return &volcanosdkalb.DescribeListenersOutput{
				Listeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{
					{ListenerId: volcengine.String("lsn-1"), LoadBalancerId: volcengine.String("lb-1"),
						Protocol: volcengine.String("https"), CertificateId: volcengine.String("cert-1")},
				},
			}, nil
		},
		describeRules: func(*volcanosdkalb.DescribeRulesInput) (*volcanosdkalb.DescribeRulesOutput, error) {
			return nil, errors.New("describe rules boom")
		},
	}
	refs, err := volcanoAdapterWith(api).ListReferences(context.Background(),
		&sharedomain.CloudAccount{Name: "acc-1"}, domain.ProductALB)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Empty(t, refs[0].ServedDomains, "规则失败 served 置空")
	assert.Equal(t, "lb-1/lsn-1", refs[0].ResourceID)
}

// TestVolcanoScanAdapter_NLBReferences NLB 监听内联证书：复合资源 ID + 归一
// 前缀；无证书监听不产出引用；NLB（L4）无 served domains。
func TestVolcanoScanAdapter_NLBReferences(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		describeNLBListeners: func(*volcanosdkclb.DescribeNLBListenersInput) (*volcanosdkclb.DescribeNLBListenersOutput, error) {
			return &volcanosdkclb.DescribeNLBListenersOutput{
				Listeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{
					{ListenerId: volcengine.String("nlb-lsn-1"), LoadBalancerId: volcengine.String("nlb-1"),
						Protocol: volcengine.String("tcpssl"), CertificateId: volcengine.String("cert-nlb")},
					{ListenerId: volcengine.String("nlb-lsn-2"), LoadBalancerId: volcengine.String("nlb-1"),
						Protocol: volcengine.String("tcp")}, // 无证书
				},
			}, nil
		},
	}
	refs, err := volcanoAdapterWith(api).ListReferences(context.Background(),
		&sharedomain.CloudAccount{Name: "acc-1"}, domain.ProductNLB)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "nlb-1/nlb-lsn-1", refs[0].ResourceID)
	assert.Equal(t, "nlb:cert-nlb", refs[0].ReferencedCloudCertID)
	assert.Empty(t, refs[0].ServedDomains)
}

// TestVolcanoScanAdapter_UnsupportedProduct 未支持产品显式报错（不静默）。
func TestVolcanoScanAdapter_UnsupportedProduct(t *testing.T) {
	_, err := volcanoAdapterWith(&fakeVolcanoScanAPI{}).ListReferences(context.Background(),
		&sharedomain.CloudAccount{Name: "acc-1"}, domain.ProductCLB)
	require.Error(t, err)
}

// =====================================================================
// AC2：指纹解析路由（GetCert 要素 / 无法复核哨兵 / 非归一 ID fail-fast）
// =====================================================================

// TestVolcanoScanAdapter_GetCertRouting GetCert 按归一前缀路由：
// cdn → ListCdnCertInfo sha256 要素；csv → cloudx 适配器（nil 适配器报错）；
// waf/alb/nlb 证书库无 sha256 指纹通道 → 无法复核哨兵（对齐华为 SHA-1 口径语义）；
// 非归一形态 fail-fast。
func TestVolcanoScanAdapter_GetCertRouting(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		listCdnCertInfo: func(in *volcanosdkcdn.ListCdnCertInfoInput) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
			certID := volcengine.StringValue(in.CertId)
			out := &volcanosdkcdn.ListCdnCertInfoOutput{}
			switch certID {
			case "cert-hit":
				out.CertInfo = []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{{
					CertId:     in.CertId,
					ExpireTime: volcengine.Int64(1700000000),
					CertFingerprint: &volcanosdkcdn.CertFingerprintForListCdnCertInfoOutput{
						Sha256: volcengine.String("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
					},
				}}
			case "cert-shortfp":
				out.CertInfo = []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{{
					CertId: in.CertId,
					CertFingerprint: &volcanosdkcdn.CertFingerprintForListCdnCertInfoOutput{
						Sha256: volcengine.String("0123456789abcdef0123456789abcdef01234567"), // 40 hex 不对齐
					},
				}}
			}
			return out, nil
		},
	}
	a := (&volcanoScanAdapter{
		newAPI: func(*sharedomain.CloudAccount, string) (volcanoScanAPI, error) { return api, nil },
	}).shim()
	creds := &sharedomain.CloudAccount{Name: "acc-1"}

	// 1. cdn 命中：sha256 对齐口径要素
	st, err := a.GetCert(context.Background(), creds, "cdn:cert-hit")
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.Equal(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", st.Fingerprint)
	assert.False(t, st.NotAfter.IsZero())
	assert.Equal(t, 1, api.cdnCertInfoCalls)

	// 2. cdn 云侧不存在：Exists=false 非错误
	st, err = a.GetCert(context.Background(), creds, "cdn:cert-missing")
	require.NoError(t, err)
	assert.False(t, st.Exists)
	assert.Equal(t, 2, api.cdnCertInfoCalls, "不存在判定经云侧查询")

	// 3. cdn 非 64hex 指纹：留空（上层按无法复核 → 占位）
	st, err = a.GetCert(context.Background(), creds, "cdn:cert-shortfp")
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.Empty(t, st.Fingerprint)

	// 4. waf/alb/nlb 证书库无 sha256 指纹通道：无法复核哨兵
	for _, id := range []string{"waf:101", "alb:cert-x", "nlb:cert-y"} {
		_, err = a.GetCert(context.Background(), creds, id)
		require.Error(t, err, id)
		assert.True(t, errors.Is(err, errVolcanoScanFingerprintUnavailable), id)
	}

	// 5. 非归一形态 fail-fast
	for _, id := range []string{"cert-raw", "unknown:cert-1", "cdn:"} {
		_, err = a.GetCert(context.Background(), creds, id)
		require.Error(t, err, id)
		assert.True(t, errors.Is(err, errVolcanoScanCertIDNotNormalized), id)
	}

	// 6. csv 前缀：cloudx 适配器缺位报错（装配期恒传入，防御路径）
	_, err = a.GetCert(context.Background(), creds, "csv:inst-1")
	require.Error(t, err)
}

// =====================================================================
// AC2（端到端）+ AC3/AC4：扫描管线接入——指纹两态与失败隔离经服务层验证
// =====================================================================

// newVolcanoScanHarness 构造火山扫描适配器专用装置（单适配器、acc-1 账号）。
func newVolcanoScanHarness(t *testing.T, adapter CloudScanAdapter) *scanHarness {
	t.Helper()
	return newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
}

// TestVolcanoScanAdapter_FingerprintResolutionEndToEnd 火山引用进入扫描管线：
// 映射反查命中 → 精确指纹；GetCert sha256 要素 → 精确指纹；无法复核（waf）→
// certscan-unresolved: 占位指纹（与 5 云同口径、同确定性公式）。
func TestVolcanoScanAdapter_FingerprintResolutionEndToEnd(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		listCdnCertInfo: func(*volcanosdkcdn.ListCdnCertInfoInput) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
			return &volcanosdkcdn.ListCdnCertInfoOutput{
				CertInfo: []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{
					{
						CertId: volcengine.String("cert-mapped"),
						ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
							{Domain: volcengine.String("m.example.com")},
						},
					},
					{
						CertId: volcengine.String("cert-get"),
						CertFingerprint: &volcanosdkcdn.CertFingerprintForListCdnCertInfoOutput{
							Sha256: volcengine.String("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
						},
						ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
							{Domain: volcengine.String("g.example.com")},
						},
					},
				},
			}, nil
		},
		listDomain: func(*volcanosdkwaf.ListDomainInput) (*volcanosdkwaf.ListDomainOutput, error) {
			return &volcanosdkwaf.ListDomainOutput{
				Data: []*volcanosdkwaf.DataForListDomainOutput{
					{Domain: volcengine.String("w.example.com"), CertificateID: volcengine.Int32(101)},
				},
			}, nil
		},
	}
	h := newVolcanoScanHarness(t, volcanoAdapterWith(api))
	require.NoError(t, h.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: "fp-mapped", Cloud: "volcano", AccountKey: "acc-1", CloudCertID: "cdn:cert-mapped",
	}))

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.ScanStatusDone, res.Status)

	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, refs, 3)
	byDomain := map[string]domain.CertReference{}
	for _, r := range refs {
		byDomain[r.ResourceID] = r
		assert.Equal(t, domain.Cloud("volcano"), r.Cloud, "云标识入引用落库形态")
		assert.Equal(t, "acc-1", r.AccountKey)
	}
	// 1. 映射反查命中
	assert.Equal(t, "fp-mapped", byDomain["m.example.com"].CertFingerprint)
	// 2. GetCert 云侧要素（CDN sha256 对齐口径）
	assert.Equal(t, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		byDomain["g.example.com"].CertFingerprint)
	// 3. waf 无法复核 → 占位指纹（确定性公式与 5 云一致）
	want := sha256Hex(unresolvedFingerprintPrefix + ":" + "volcano|acc-1|waf:101")
	assert.Equal(t, want, byDomain["w.example.com"].CertFingerprint)
	assert.Regexp(t, validFingerprintRe, byDomain["w.example.com"].CertFingerprint)
}

// TestVolcanoScanAdapter_ProductFailureIsolation 单产品失败不中断该云其余
// 产品（AC3）：CDN 通道报错 → partials 记因，WAF/ALB/NLB 引用照常落库。
func TestVolcanoScanAdapter_ProductFailureIsolation(t *testing.T) {
	api := &fakeVolcanoScanAPI{
		listCdnCertInfo: func(*volcanosdkcdn.ListCdnCertInfoInput) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
			return nil, errors.New("cdn api boom")
		},
		listDomain: func(*volcanosdkwaf.ListDomainInput) (*volcanosdkwaf.ListDomainOutput, error) {
			return &volcanosdkwaf.ListDomainOutput{
				Data: []*volcanosdkwaf.DataForListDomainOutput{
					{Domain: volcengine.String("w.example.com"), CertificateID: volcengine.Int32(101)},
				},
			}, nil
		},
		describeListeners: func(*volcanosdkalb.DescribeListenersInput) (*volcanosdkalb.DescribeListenersOutput, error) {
			return &volcanosdkalb.DescribeListenersOutput{
				Listeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{
					{ListenerId: volcengine.String("lsn-1"), LoadBalancerId: volcengine.String("lb-1"),
						Protocol: volcengine.String("https"), CertificateId: volcengine.String("cert-1")},
				},
			}, nil
		},
		describeNLBListeners: func(*volcanosdkclb.DescribeNLBListenersInput) (*volcanosdkclb.DescribeNLBListenersOutput, error) {
			return &volcanosdkclb.DescribeNLBListenersOutput{
				Listeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{
					{ListenerId: volcengine.String("nlb-lsn-1"), LoadBalancerId: volcengine.String("nlb-1"),
						CertificateId: volcengine.String("cert-nlb")},
				},
			}, nil
		},
	}
	h := newVolcanoScanHarness(t, volcanoAdapterWith(api))

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusDone, res.Status, "部分通道失败整体仍 done")
	assert.Equal(t, 4, res.ChannelsAttempted, "1 适配器 × 4 产品 × 1 账号")
	assert.Equal(t, 1, res.ChannelsFailed)
	require.Len(t, res.PartialFailures, 1)
	assert.Equal(t, "volcano", res.PartialFailures[0].Cloud)
	assert.Equal(t, "cdn", res.PartialFailures[0].Product)

	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, refs, 3, "waf/alb/nlb 引用照常落库")
	products := map[string]bool{}
	for _, r := range refs {
		products[string(r.Product)] = true
	}
	assert.Equal(t, map[string]bool{"waf": true, "alb": true, "nlb": true}, products)
}

// TestVolcanoScanAdapter_AccountFailureIsolation 单账号单产品失败不中断其余
// 账号（AC3）：acc-2 的 CDN 失败 → acc-1 的 CDN 与两账号其余产品照常落库。
func TestVolcanoScanAdapter_AccountFailureIsolation(t *testing.T) {
	newAPI := func(cdnErr bool) volcanoScanAPI {
		return &fakeVolcanoScanAPI{
			listCdnCertInfo: func(*volcanosdkcdn.ListCdnCertInfoInput) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
				if cdnErr {
					return nil, errors.New("cdn api boom")
				}
				return &volcanosdkcdn.ListCdnCertInfoOutput{
					CertInfo: []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{{
						CertId: volcengine.String("cert-acc"),
						ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
							{Domain: volcengine.String("acc.example.com")},
						},
					}},
				}, nil
			},
		}
	}
	adapter := (&volcanoScanAdapter{
		newAPI: volcanoAPIByAccount(map[string]volcanoScanAPI{
			"acc-1": newAPI(false),
			"acc-2": newAPI(true),
		}),
	}).shim()
	h := newVolcanoScanHarness(t, adapter)
	h.accounts.byCloud[domain.Cloud("volcano")] = []*sharedomain.CloudAccount{
		{Name: "acc-1", Provider: sharedomain.CloudProviderVolcano},
		{Name: "acc-2", Provider: sharedomain.CloudProviderVolcano},
	}

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusDone, res.Status)
	assert.Equal(t, 8, res.ChannelsAttempted, "4 产品 × 2 账号")
	assert.Equal(t, 1, res.ChannelsFailed, "仅 acc-2 的 CDN 通道失败")

	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "acc.example.com", refs[0].ResourceID)
	assert.Equal(t, "acc-1", refs[0].AccountKey, "acc-1 的 CDN 引用未受 acc-2 失败影响")
}

// =====================================================================
// 覆盖补强：csv 指纹源路由 / 生产客户端工厂 / 纯函数边界
// =====================================================================

// fakeVolcanoCSVSource certificateservice 统一证书库指纹源 fake。
type fakeVolcanoCSVSource struct {
	inst volcanocert.CloudCertInstance
	err  error
}

func (f *fakeVolcanoCSVSource) GetCertificate(context.Context, *sharedomain.CloudAccount, string) (volcanocert.CloudCertInstance, error) {
	return f.inst, f.err
}

// TestVolcanoScanAdapter_GetCertCSVPath csv 前缀路由 cloudx 指纹基础：
// 成功 → 链解析指纹/有效期透传；ErrCertFiltered → Exists=false 非错误。
func TestVolcanoScanAdapter_GetCertCSVPath(t *testing.T) {
	a := (&volcanoScanAdapter{
		newAPI: func(*sharedomain.CloudAccount, string) (volcanoScanAPI, error) { return &fakeVolcanoScanAPI{}, nil },
		csvCerts: &fakeVolcanoCSVSource{
			inst: volcanocert.CloudCertInstance{
				Fingerprint: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				NotAfter:    time.Unix(1800000000, 0),
			},
		},
	}).shim()
	creds := &sharedomain.CloudAccount{Name: "acc-1"}

	st, err := a.GetCert(context.Background(), creds, "csv:inst-1")
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.Equal(t, "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", st.Fingerprint)
	assert.Equal(t, time.Unix(1800000000, 0).UTC(), st.NotAfter.UTC())

	filtered := (&volcanoScanAdapter{
		csvCerts: &fakeVolcanoCSVSource{err: fmt.Errorf("wrapped: %w", volcanocert.ErrCertFiltered)},
	}).shim()
	st, err = filtered.GetCert(context.Background(), creds, "csv:inst-2")
	require.NoError(t, err)
	assert.False(t, st.Exists, "revoked/非 Issued 归一 Exists=false（不可作回滚目标）")
}

// TestVolcanoScanAdapter_ClientFactory 生产客户端工厂离线装配（构建不发起
// 网络请求）：四服务客户端齐备非 nil。
func TestVolcanoScanAdapter_ClientFactory(t *testing.T) {
	api, err := newVolcanoScanAPI(&sharedomain.CloudAccount{
		Name: "acc-1", AccessKeyID: "ak", AccessKeySecret: "sk", Regions: []string{"cn-beijing"},
	}, "cn-beijing")
	require.NoError(t, err)
	require.IsType(t, &volcanoScanClients{}, api)
	clients := api.(*volcanoScanClients)
	assert.NotNil(t, clients.cdn)
	assert.NotNil(t, clients.waf)
	assert.NotNil(t, clients.alb)
	assert.NotNil(t, clients.nlb)
}

// TestVolcanoScanAdapter_ResourceIDAndRegionHelpers 纯函数边界：
// 复合资源 ID 缺 lbId 回退纯监听形态；地域清单缺省回退默认地域。
func TestVolcanoScanAdapter_ResourceIDAndRegionHelpers(t *testing.T) {
	assert.Equal(t, "lsn-1", volcanoLBResourceID("", "lsn-1"))
	assert.Equal(t, "lb-1/lsn-1", volcanoLBResourceID("lb-1", "lsn-1"))
	assert.Equal(t, []string{volcanoScanFallbackRegion}, volcanoScanRegions(nil))
	assert.Equal(t, []string{volcanoScanFallbackRegion},
		volcanoScanRegions(&sharedomain.CloudAccount{Name: "acc-1"}))
	assert.Equal(t, []string{"cn-beijing", "cn-shanghai"},
		volcanoScanRegions(&sharedomain.CloudAccount{Regions: []string{"cn-beijing", "cn-shanghai"}}))
	assert.Equal(t, volcanoScanFallbackRegion, volcanoScanDefaultRegion(nil))
	assert.Equal(t, "cn-beijing", volcanoScanDefaultRegion(&sharedomain.CloudAccount{Regions: []string{"cn-beijing"}}))
}
