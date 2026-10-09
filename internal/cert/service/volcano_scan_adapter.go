// volcano_scan_adapter.go 火山引擎引用扫描适配器（cert-volcano-deployer 任务 3）：
// 四产品（CDN/WAF/ALB/NLB）资源证书引用 → CloudScanAdapter 只读端口，接入
// reference_scan_service 五云扫描管线（第 6 云）。
//
// 形态对齐 5 云 NewXxxScanAdapter（reference_scan_service.go cloudScanAdapter
// shim）：cloud/products 元数据 + listRefs/getCert 只读闭包；编译期经 shim
// 满足 CloudScanAdapter 端口。扫描只读（Hard Rule）：窄接口仅含枚举/查询读
// 方法，无任何云写通路。
//
// 引用形态（与任务 2 BindResource 消费形态共享约定，M/M 对齐）：
//   - ResourceID：CDN/WAF=加速域名/防护域名；ALB/NLB="{lbId}/{listenerId}"
//     监听复合 ID（对齐 aliyun lbScopedResourceID / huawei elb 先例，实例 ID
//     供控制台对账、监听 ID 供绑定定位）；
//   - ReferencedCloudCertID：{product}:{id} 归一形态（对齐任务 1 部署器
//     normalizeVolcanoCloudCertID 口径——变更清单回滚 GetCert/CleanupOrphan
//     按前缀路由消费同一形态）；
//   - ServedDomains：ALB 监听转发规则 Host 条件值展开（external DNS 对齐依据，
//     对齐 aliyun listALBServedDomains 口径）；NLB（L4）无规则为空。
//
// 指纹解析分工（Hard Rule：不复制部署器逻辑，复用既有口径）：
//   - 映射反查/占位指纹：service.resolveUncached 既有语义（cloudScanAdapter
//     shim 注入即自动接通），占位 certscan-unresolved: 与 5 云一致；
//   - GetCert 云侧要素：cdn → ListCdnCertInfo 原生 SHA256（64hex 对齐口径
//     直读）；csv → cloudx/volcano/cert.go CertAdapter（cert-volcano-import-sync
//     交付的链解析指纹基础）；waf/alb/nlb 证书库查询 API 不返回 sha256 指纹
//     → 无法复核哨兵（对齐华为 SCM SHA-1 口径语义：指纹仅可经映射反查恢复，
//     否则落占位）。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	volcanosdkalb "github.com/volcengine/volcengine-go-sdk/service/alb"
	volcanosdkcdn "github.com/volcengine/volcengine-go-sdk/service/cdn"
	volcanosdkclb "github.com/volcengine/volcengine-go-sdk/service/clb"
	volcanosdkwaf "github.com/volcengine/volcengine-go-sdk/service/waf"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	volcanocert "github.com/Havens-blog/e-cloudx-sdk/volcano"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// ---------------------------------------------------------------------
// 常量与哨兵
// ---------------------------------------------------------------------

// 火山证书库产品前缀（{product}:{id} 归一口径，与部署器任务 1 同值同源；
// csv 为 certificateservice 统一证书库——资源引用不直接携带，GetCert 防御性
// 路由保留）。分拆独立于 deployer 包私有常量：跨包导出受本任务 Hard Rule
// 文件清单所限，值域以注释锚点同步（漂移由 getCert 路由 fail-fast 暴露）。
const (
	volcanoScanProductCSV = "csv"
	volcanoScanProductCDN = "cdn"
	volcanoScanProductWAF = "waf"
	volcanoScanProductALB = "alb"
	volcanoScanProductNLB = "nlb"
)

// volcanoScanCertPrefixes 归一前缀合法集（volcanoScanSplitCertID 消费）。
var volcanoScanCertPrefixes = map[string]bool{
	volcanoScanProductCSV: true,
	volcanoScanProductCDN: true,
	volcanoScanProductWAF: true,
	volcanoScanProductALB: true,
	volcanoScanProductNLB: true,
}

const (
	// volcanoScanPageSizeDefault 引用枚举默认分页大小（四产品统一口径）。
	volcanoScanPageSizeDefault = int64(100)
	// volcanoScanFallbackRegion 火山账号缺省地域（与部署器/发现导入口径一致）。
	volcanoScanFallbackRegion = "cn-beijing"
)

// 哨兵错误（扫描侧 GetCert 错误 → service.resolveUncached 吞并落占位指纹，
// 不中断扫描；ListReferences 错误 → 服务层 partials 记因并隔离其余产品）。
var (
	// errVolcanoScanFingerprintUnavailable waf/alb/nlb 证书库查询 API 不返回
	// sha256 指纹（对齐华为 SCM SHA-1 口径的"无法复核"语义）。
	errVolcanoScanFingerprintUnavailable = errors.New("volcano scan: cert library exposes no sha256 fingerprint channel")
	// errVolcanoScanCertIDNotNormalized 云证书 ID 非归一形态（缺前缀/未知前缀/
	// 空裸 ID——fail-fast 不猜测）。
	errVolcanoScanCertIDNotNormalized = errors.New("volcano scan: cloud cert id is not normalized ({product}:{id})")
	// errVolcanoScanProductNotSupported 未支持产品显式报错（不静默）。
	errVolcanoScanProductNotSupported = errors.New("volcano scan: product not supported")
)

// volcanoScanCloud 火山云标识（与 discoveryCloudVolcano 同值同源：cert/domain.
// Cloud 枚举未含火山，经 shared/domain 账号 provider 常量转译 "volcano"）。
var volcanoScanCloud = discoveryCloudVolcano

// ---------------------------------------------------------------------
// SDK 窄接口与客户端工厂
// ---------------------------------------------------------------------

// volcanoScanAPI 火山引用扫描 SDK 窄接口（只读 5 方法，生产实现为
// volcanoScanClients；测试注入 fake）。签名即适配原签名——本层只消费 SDK，
// 不改适配面。接口面无任何写方法（Hard Rule：扫描只读构造性保证）。
type volcanoScanAPI interface {
	// CDN 证书引用枚举（CDN 为全局服务，region 仅用于客户端签名装配）
	ListCdnCertInfoWithContext(ctx context.Context, input *volcanosdkcdn.ListCdnCertInfoInput, opts ...request.Option) (*volcanosdkcdn.ListCdnCertInfoOutput, error)
	// WAF 防护域名列表（region 级；Region 经 input 透传）
	ListDomainWithContext(ctx context.Context, input *volcanosdkwaf.ListDomainInput, opts ...request.Option) (*volcanosdkwaf.ListDomainOutput, error)
	// ALB 监听器枚举与转发规则（region 级）
	DescribeListenersWithContext(ctx context.Context, input *volcanosdkalb.DescribeListenersInput, opts ...request.Option) (*volcanosdkalb.DescribeListenersOutput, error)
	DescribeRulesWithContext(ctx context.Context, input *volcanosdkalb.DescribeRulesInput, opts ...request.Option) (*volcanosdkalb.DescribeRulesOutput, error)
	// NLB 监听器枚举（region 级，clb 服务 NLB API；证书内联监听器）
	DescribeNLBListenersWithContext(ctx context.Context, input *volcanosdkclb.DescribeNLBListenersInput, opts ...request.Option) (*volcanosdkclb.DescribeNLBListenersOutput, error)
}

// volcanoScanClients 火山引用扫描 SDK 客户端束（生产实现，volcanoScanAPI）。
// 显式转发而非四客户端嵌入聚合——各服务方法名空间交叠，嵌入提升选择器有
// 歧义风险（对齐部署器 volcanoSDKClients 先例），显式转发构造性保证窄接口面。
type volcanoScanClients struct {
	cdn *volcanosdkcdn.CDN
	waf *volcanosdkwaf.WAF
	alb *volcanosdkalb.ALB
	nlb *volcanosdkclb.CLB
}

func (c *volcanoScanClients) ListCdnCertInfoWithContext(ctx context.Context, input *volcanosdkcdn.ListCdnCertInfoInput, opts ...request.Option) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
	return c.cdn.ListCdnCertInfoWithContext(ctx, input, opts...)
}

func (c *volcanoScanClients) ListDomainWithContext(ctx context.Context, input *volcanosdkwaf.ListDomainInput, opts ...request.Option) (*volcanosdkwaf.ListDomainOutput, error) {
	return c.waf.ListDomainWithContext(ctx, input, opts...)
}

func (c *volcanoScanClients) DescribeListenersWithContext(ctx context.Context, input *volcanosdkalb.DescribeListenersInput, opts ...request.Option) (*volcanosdkalb.DescribeListenersOutput, error) {
	return c.alb.DescribeListenersWithContext(ctx, input, opts...)
}

func (c *volcanoScanClients) DescribeRulesWithContext(ctx context.Context, input *volcanosdkalb.DescribeRulesInput, opts ...request.Option) (*volcanosdkalb.DescribeRulesOutput, error) {
	return c.alb.DescribeRulesWithContext(ctx, input, opts...)
}

func (c *volcanoScanClients) DescribeNLBListenersWithContext(ctx context.Context, input *volcanosdkclb.DescribeNLBListenersInput, opts ...request.Option) (*volcanosdkclb.DescribeNLBListenersOutput, error) {
	return c.nlb.DescribeNLBListenersWithContext(ctx, input, opts...)
}

// newVolcanoScanAPI 生产客户端工厂：一次会话装配四服务客户端（静态 AK/SK，
// region 仅用于客户端签名装配；region 级产品的实际地域经 input/遍历承载）。
func newVolcanoScanAPI(creds *sharedomain.CloudAccount, region string) (volcanoScanAPI, error) {
	config := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(creds.AccessKeyID, creds.AccessKeySecret, "")).
		WithRegion(region)
	sess, err := session.NewSession(config)
	if err != nil {
		return nil, fmt.Errorf("创建火山引用扫描会话失败: %w", err)
	}
	return &volcanoScanClients{
		cdn: volcanosdkcdn.New(sess),
		waf: volcanosdkwaf.New(sess),
		alb: volcanosdkalb.New(sess),
		nlb: volcanosdkclb.New(sess),
	}, nil
}

// ---------------------------------------------------------------------
// 适配器
// ---------------------------------------------------------------------

// volcanoCSVFingerprintSource certificateservice 统一证书库指纹源端口（生产
// 实现为 cloudx/volcano.CertAdapter——cert-volcano-import-sync 交付的链解析
// 指纹基础；接口面单方法，测试注入 fake）。
type volcanoCSVFingerprintSource interface {
	GetCertificate(ctx context.Context, creds *sharedomain.CloudAccount, instanceID string) (volcanocert.CloudCertInstance, error)
}

// volcanoScanAdapter 火山引用扫描适配器内核：SDK 窄接口工厂可注入 fake；
// csvCerts 为 certificateservice 统一证书库指纹源（GetCert csv 前缀路由复用）。
type volcanoScanAdapter struct {
	csvCerts volcanoCSVFingerprintSource
	newAPI   func(creds *sharedomain.CloudAccount, region string) (volcanoScanAPI, error)
	pageSize int64 // 引用枚举分页大小（默认 volcanoScanPageSizeDefault，测试可缩小覆盖翻页分支）
}

// NewVolcanoScanAdapter 创建火山引擎引用扫描适配器（对齐 5 云构造器形态：
// 返回 CloudScanAdapter 端口；csvCerts 供 GetCert csv 路由复用 cloudx 指纹基础，
// 缺省传 nil 仅关闭 csv 防御路由——四产品资源引用不直接携带 csv 前缀）。
func NewVolcanoScanAdapter(csvCerts *volcanocert.CertAdapter) CloudScanAdapter {
	return (&volcanoScanAdapter{
		csvCerts: csvCerts,
		newAPI:   newVolcanoScanAPI,
	}).shim()
}

// shim 包装为 5 云通用扫描 shim（cloud/products 元数据 + 只读方法闭包）。
func (a *volcanoScanAdapter) shim() CloudScanAdapter {
	return cloudScanAdapter{
		cloud: volcanoScanCloud,
		products: []domain.Product{
			domain.ProductCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB,
		},
		listRefs: a.listReferences,
		getCert:  a.getCert,
	}
}

// scanPageSize 获取枚举分页大小（零值回退默认）。
func (a *volcanoScanAdapter) scanPageSize() int64 {
	if a.pageSize <= 0 {
		return volcanoScanPageSizeDefault
	}
	return a.pageSize
}

// ---------------------------------------------------------------------
// ListReferences（按产品分发；单产品错误经服务层 partials 隔离）
// ---------------------------------------------------------------------

// listReferences 只读发现产品下全部证书引用（按产品分发）。
func (a *volcanoScanAdapter) listReferences(ctx context.Context, creds *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
	if creds == nil {
		return nil, fmt.Errorf("volcano scan: nil creds")
	}
	switch product {
	case domain.ProductCDN:
		return a.listCDNReferences(ctx, creds)
	case domain.ProductWAF:
		return a.listWAFReferences(ctx, creds)
	case domain.ProductALB:
		return a.listALBReferences(ctx, creds)
	case domain.ProductNLB:
		return a.listNLBReferences(ctx, creds)
	default:
		return nil, fmt.Errorf("%w: %q", errVolcanoScanProductNotSupported, product)
	}
}

// ref 统一引用形态构造（cloud/product/accountKey 写通 + 归一云证书 ID 透传）。
func (a *volcanoScanAdapter) ref(product domain.Product, resourceID, cloudCertID string, creds *sharedomain.CloudAccount) DiscoveredRef {
	return DiscoveredRef{
		Cloud:                 string(volcanoScanCloud),
		Product:               string(product),
		ResourceID:            resourceID,
		ReferencedCloudCertID: cloudCertID,
		AccountKey:            creds.Name,
	}
}

// listCDNReferences CDN 加速域名证书引用（域名粒度）：ListCdnCertInfo 分页
// 枚举证书库（Source 不过滤——扫描不区分证书来源），逐证书按已配置域名
// （ConfiguredDomainDetail）展开引用；未配置域名的证书不构成引用。
// 实网复核项：ConfiguredDomainDetail 为云侧已配置域名口径（若云侧仅回填
// ConfiguredDomain 摘要字段，在此单点补充回退展开）。
func (a *volcanoScanAdapter) listCDNReferences(ctx context.Context, creds *sharedomain.CloudAccount) ([]DiscoveredRef, error) {
	client, err := a.newAPI(creds, volcanoScanDefaultRegion(creds))
	if err != nil {
		return nil, err
	}
	pageSize := a.scanPageSize()
	var refs []DiscoveredRef
	for pageNum := int64(1); ; pageNum++ {
		out, err := client.ListCdnCertInfoWithContext(ctx, &volcanosdkcdn.ListCdnCertInfoInput{
			PageNum:  volcengine.Int64(pageNum),
			PageSize: volcengine.Int64(pageSize),
		})
		if err != nil {
			return nil, wrapVolcanoScanErr("cdn_list_cert_info", err)
		}
		if out == nil || len(out.CertInfo) == 0 {
			break
		}
		for _, cert := range out.CertInfo {
			certID := strings.TrimSpace(volcengine.StringValue(cert.CertId))
			if certID == "" {
				continue
			}
			for _, conf := range cert.ConfiguredDomainDetail {
				dom := strings.TrimSpace(volcengine.StringValue(conf.Domain))
				if dom == "" {
					continue
				}
				refs = append(refs, a.ref(domain.ProductCDN, dom, volcanoScanProductCDN+":"+certID, creds))
			}
		}
		if int64(len(out.CertInfo)) < pageSize {
			break // 整页未满即最后一页
		}
	}
	return refs, nil
}

// listWAFReferences WAF 防护域名证书引用（域名粒度）：按账号地域遍历
// ListDomain 分页——列表内联 CertificateID（WAF 服务证书库 Id，0/缺省=未配置
// 证书不构成引用），无 N+1 展开列表项自带域名+证书字段（对齐华为 ShowHost
// N+1 的替代口径：火山列表接口直接回填）。
func (a *volcanoScanAdapter) listWAFReferences(ctx context.Context, creds *sharedomain.CloudAccount) ([]DiscoveredRef, error) {
	var refs []DiscoveredRef
	for _, region := range volcanoScanRegions(creds) {
		client, err := a.newAPI(creds, region)
		if err != nil {
			return nil, err
		}
		pageSize := int32(a.scanPageSize())
		for page := int32(1); ; page++ {
			out, err := client.ListDomainWithContext(ctx, &volcanosdkwaf.ListDomainInput{
				Page:     volcengine.Int32(page),
				PageSize: volcengine.Int32(pageSize),
				Region:   volcengine.String(region),
			})
			if err != nil {
				return nil, wrapVolcanoScanErr("waf_list_domain", err)
			}
			if out == nil || len(out.Data) == 0 {
				break
			}
			for _, item := range out.Data {
				dom := strings.TrimSpace(volcengine.StringValue(item.Domain))
				var certID int64
				if item.CertificateID != nil {
					certID = int64(*item.CertificateID)
				}
				if dom == "" || certID == 0 {
					continue
				}
				refs = append(refs, a.ref(domain.ProductWAF, dom,
					volcanoScanProductWAF+":"+strconv.FormatInt(certID, 10), creds))
			}
			if int32(len(out.Data)) < pageSize {
				break // 整页未满即最后一页
			}
		}
	}
	return refs, nil
}

// listALBReferences ALB 监听证书引用（L7 终结）：按账号地域遍历
// DescribeListeners 分页——主证书（CertificateId）与 SNI 扩展证书
// （DomainExtensions，与主证书同 ID 去重）均为引用；HTTP 监听无服务器证书
// 跳过。资源 ID 为监听复合形态，served domains 逐监听经转发规则展开。
// 实网复核项：协议枚举大小写与 QUIC 监听证书面以云侧为准（当前按
// 大小写不敏感 http 排除，其余协议无证书字段时自然零引用）。
func (a *volcanoScanAdapter) listALBReferences(ctx context.Context, creds *sharedomain.CloudAccount) ([]DiscoveredRef, error) {
	var refs []DiscoveredRef
	for _, region := range volcanoScanRegions(creds) {
		client, err := a.newAPI(creds, region)
		if err != nil {
			return nil, err
		}
		pageSize := a.scanPageSize()
		for pageNum := int64(1); ; pageNum++ {
			out, err := client.DescribeListenersWithContext(ctx, &volcanosdkalb.DescribeListenersInput{
				PageNumber: volcengine.Int64(pageNum),
				PageSize:   volcengine.Int64(pageSize),
			})
			if err != nil {
				return nil, wrapVolcanoScanErr("alb_describe_listeners", err)
			}
			if out == nil || len(out.Listeners) == 0 {
				break
			}
			for _, listener := range out.Listeners {
				refs = append(refs, a.albListenerRefs(ctx, client, listener, creds)...)
			}
			if int64(len(out.Listeners)) < pageSize {
				break // 整页未满即最后一页
			}
		}
	}
	return refs, nil
}

// albListenerRefs 展开单个 ALB 监听的证书引用（主证书 + SNI 扩展证书；
// served domains 单监听提取，失败置空不阻塞——回退 coverage 语义对齐 aliyun）。
func (a *volcanoScanAdapter) albListenerRefs(ctx context.Context, client volcanoScanAPI, listener *volcanosdkalb.ListenerForDescribeListenersOutput, creds *sharedomain.CloudAccount) []DiscoveredRef {
	if listener == nil || strings.EqualFold(volcengine.StringValue(listener.Protocol), "http") {
		return nil // HTTP 监听无服务器证书
	}
	listenerID := volcengine.StringValue(listener.ListenerId)
	if listenerID == "" {
		return nil
	}
	resourceID := volcanoLBResourceID(volcengine.StringValue(listener.LoadBalancerId), listenerID)
	served := a.albServedDomains(ctx, client, listenerID)

	defaultCertID := strings.TrimSpace(volcengine.StringValue(listener.CertificateId))
	if defaultCertID == "" {
		// 证书中心（cert center）形态监听：CertificateId 缺省时回退
		// CertCenterCertificateId（实网复核项：两形态互斥性以云侧为准）。
		defaultCertID = strings.TrimSpace(volcengine.StringValue(listener.CertCenterCertificateId))
	}
	var refs []DiscoveredRef
	if defaultCertID != "" {
		refs = append(refs, a.ref(domain.ProductALB, resourceID,
			volcanoScanProductALB+":"+defaultCertID, creds))
		refs[0].ServedDomains = served
	}
	for _, ext := range listener.DomainExtensions {
		extCertID := strings.TrimSpace(volcengine.StringValue(ext.CertificateId))
		if ext == nil || extCertID == "" || extCertID == defaultCertID {
			continue
		}
		r := a.ref(domain.ProductALB, resourceID, volcanoScanProductALB+":"+extCertID, creds)
		r.ServedDomains = served
		refs = append(refs, r)
	}
	return refs
}

// albServedDomains 逐监听遍历转发规则，提取 Host 条件值（served hostname，
// external DNS 记录 → ALB 资源级 expected 对齐依据）。失败返回 nil（不阻塞
// 引用扫描主干，served 置空回退 coverage——对齐 aliyun listALBServedDomains）。
func (a *volcanoScanAdapter) albServedDomains(ctx context.Context, client volcanoScanAPI, listenerID string) []string {
	out, err := client.DescribeRulesWithContext(ctx, &volcanosdkalb.DescribeRulesInput{
		ListenerId: volcengine.String(listenerID),
	})
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var served []string
	for _, rule := range out.Rules {
		for _, cond := range rule.RuleConditions {
			if cond == nil || !strings.EqualFold(volcengine.StringValue(cond.Type), "host") || cond.HostConfig == nil {
				continue
			}
			for _, v := range cond.HostConfig.Values {
				name := strings.TrimSpace(volcengine.StringValue(v))
				if name == "" {
					continue
				}
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				served = append(served, name)
			}
		}
	}
	return served
}

// listNLBReferences NLB 监听证书引用（L4 TLS）：按账号地域遍历
// DescribeNLBListeners（NextToken 分页）——证书内联监听器（CertificateId），
// 无证书监听不产出引用；NLB 无转发规则，ServedDomains 为空。
func (a *volcanoScanAdapter) listNLBReferences(ctx context.Context, creds *sharedomain.CloudAccount) ([]DiscoveredRef, error) {
	var refs []DiscoveredRef
	for _, region := range volcanoScanRegions(creds) {
		client, err := a.newAPI(creds, region)
		if err != nil {
			return nil, err
		}
		var nextToken *string
		for {
			out, err := client.DescribeNLBListenersWithContext(ctx, &volcanosdkclb.DescribeNLBListenersInput{
				MaxResults: volcengine.Int64(a.scanPageSize()),
				NextToken:  nextToken,
			})
			if err != nil {
				return nil, wrapVolcanoScanErr("nlb_describe_listeners", err)
			}
			if out == nil || len(out.Listeners) == 0 {
				break
			}
			for _, listener := range out.Listeners {
				listenerID := volcengine.StringValue(listener.ListenerId)
				certID := strings.TrimSpace(volcengine.StringValue(listener.CertificateId))
				if listenerID == "" || certID == "" {
					continue // 无证书监听（tcp/udp 等）不构成引用
				}
				refs = append(refs, a.ref(domain.ProductNLB,
					volcanoLBResourceID(volcengine.StringValue(listener.LoadBalancerId), listenerID),
					volcanoScanProductNLB+":"+certID, creds))
			}
			if out.NextToken == nil || *out.NextToken == "" {
				break
			}
			nextToken = out.NextToken
		}
	}
	return refs, nil
}

// ---------------------------------------------------------------------
// GetCert（指纹解析 fallback：映射反查未命中时的云侧要素通道）
// ---------------------------------------------------------------------

// getCert 按归一前缀路由云侧证书在库状态（只读）：
//   - cdn：ListCdnCertInfo 定点反查 → 原生 SHA256 指纹（64hex 对齐口径直读，
//     非对齐口径留空=无法复核）+ ExpireTime；
//   - csv：cloudx/volcano CertAdapter 链解析（SHA256 对齐台账口径的指纹基础，
//     Hard Rule 指定复用通道）；ErrCertFiltered（revoked/非 Issued）归一
//     Exists=false（对齐部署器 getCertCSV 口径）；
//   - waf/alb/nlb：证书库查询 API 不返回 sha256 指纹 → 无法复核哨兵（指纹仅
//     可经映射反查恢复，否则服务层落占位——对齐华为 SCM SHA-1 口径语义）；
//   - 非归一形态 fail-fast（不猜测）。
func (a *volcanoScanAdapter) getCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
	product, rawID, ok := volcanoScanSplitCertID(cloudCertID)
	if !ok {
		return CloudCertStatus{}, fmt.Errorf("%w: %q", errVolcanoScanCertIDNotNormalized, cloudCertID)
	}
	switch product {
	case volcanoScanProductCDN:
		return a.getCertCDN(ctx, creds, rawID)
	case volcanoScanProductCSV:
		return a.getCertCSV(ctx, creds, rawID)
	default:
		return CloudCertStatus{}, fmt.Errorf("%w: %q", errVolcanoScanFingerprintUnavailable, product)
	}
}

// getCertCDN CDN 证书库定点查询：ListCdnCertInfo（CertId 过滤）→ 命中 →
// 原生 Sha256 指纹（64hex 对齐口径直读）+ ExpireTime（unix 秒）；未命中归一
// Exists=false 非错误（幂等口径，五云同语义）。
func (a *volcanoScanAdapter) getCertCDN(ctx context.Context, creds *sharedomain.CloudAccount, certID string) (CloudCertStatus, error) {
	client, err := a.newAPI(creds, volcanoScanDefaultRegion(creds))
	if err != nil {
		return CloudCertStatus{}, err
	}
	out, err := client.ListCdnCertInfoWithContext(ctx, &volcanosdkcdn.ListCdnCertInfoInput{
		CertId: volcengine.String(certID),
	})
	if err != nil {
		return CloudCertStatus{}, wrapVolcanoScanErr("cdn_list_cert_info", err)
	}
	if out == nil {
		return CloudCertStatus{}, fmt.Errorf("volcano cdn_list_cert_info: empty response (cert %s)", certID)
	}
	for _, cert := range out.CertInfo {
		if cert == nil || volcengine.StringValue(cert.CertId) != certID {
			continue
		}
		status := CloudCertStatus{Exists: true}
		if cert.ExpireTime != nil {
			status.NotAfter = time.Unix(*cert.ExpireTime, 0)
		}
		if cert.CertFingerprint != nil {
			if fp := volcengine.StringValue(cert.CertFingerprint.Sha256); len(fp) == 64 {
				status.Fingerprint = fp
			}
		}
		return status, nil
	}
	return CloudCertStatus{Exists: false}, nil
}

// getCertCSV certificateservice 统一证书库查询（cloudx/volcano/cert.go 复用）：
// 链解析产出 SHA256 对齐指纹与有效期。ErrCertFiltered（revoked/非 Issued）
// 归一 Exists=false 非错误（不可作回滚目标，对齐部署器 getCertCSV 口径）。
func (a *volcanoScanAdapter) getCertCSV(ctx context.Context, creds *sharedomain.CloudAccount, instanceID string) (CloudCertStatus, error) {
	if a.csvCerts == nil {
		return CloudCertStatus{}, fmt.Errorf("volcano scan: csv cert adapter unavailable")
	}
	inst, err := a.csvCerts.GetCertificate(ctx, creds, instanceID)
	if errors.Is(err, volcanocert.ErrCertFiltered) {
		return CloudCertStatus{Exists: false}, nil
	}
	if err != nil {
		return CloudCertStatus{}, err
	}
	return CloudCertStatus{Exists: true, NotAfter: inst.NotAfter, Fingerprint: inst.Fingerprint}, nil
}

// ---------------------------------------------------------------------
// 公共小件
// ---------------------------------------------------------------------

// volcanoScanSplitCertID 归一形态 → (product, 裸 ID)；非归一形态（缺前缀/
// 未知前缀/空裸 ID）返回 ok=false（fail-fast 不猜测）。{product}:{id} 为
// 与部署器任务 1 共享的归一口径（splitVolcanoCloudCertID 同语义，跨包不导出
// 受 Hard Rule 文件清单所限）。
func volcanoScanSplitCertID(cloudCertID string) (product, rawID string, ok bool) {
	p, raw, found := strings.Cut(cloudCertID, ":")
	if !found || !volcanoScanCertPrefixes[p] || strings.TrimSpace(raw) == "" {
		return "", "", false
	}
	return p, raw, true
}

// volcanoLBResourceID 构造负载均衡监听复合资源 ID "{lbId}/{listenerId}"
// （对齐 aliyun lbScopedResourceID / 华为 ELB 先例：实例 ID 供控制台对账，
// 监听 ID 供绑定定位）。lbID 为空（云侧响应异常缺字段）时回退纯监听形态，
// 不产生 "/lsn-*" 脏值。
func volcanoLBResourceID(lbID, listenerID string) string {
	if lbID == "" {
		return listenerID
	}
	return lbID + "/" + listenerID
}

// volcanoScanRegions 账号地域清单（WAF/ALB/NLB 为地域级产品按地域遍历发现；
// 缺省回退默认地域——对齐 aliyun credsRegions 口径）。
func volcanoScanRegions(creds *sharedomain.CloudAccount) []string {
	if creds != nil && len(creds.Regions) > 0 {
		return creds.Regions
	}
	return []string{volcanoScanFallbackRegion}
}

// volcanoScanDefaultRegion 账号默认地域（Regions[0]；全局服务客户端签名
// 装配用——CDN 证书引用枚举与 GetCert 路由共用）。
func volcanoScanDefaultRegion(creds *sharedomain.CloudAccount) string {
	if creds != nil && len(creds.Regions) > 0 {
		return creds.Regions[0]
	}
	return volcanoScanFallbackRegion
}

// wrapVolcanoScanErr 云 API 错误统一包装（对齐既有跨云 wrapCertCloudErr 归一
// 语义）：统一云/操作前缀 + %w 透传云侧错误明文（扫描只读路径，无密钥材料）。
func wrapVolcanoScanErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("volcano %s api error: %w", op, err)
}
