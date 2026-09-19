// volcano_deployer.go 火山引擎 CloudDeployer 实现（cert-volcano-deployer 任务 1：
// 证书库层 UploadCert/GetCert/CleanupOrphan + 云证书 ID 归一）。
//
// 分层定位：火山引擎证书库 SDK（volcengine-go-sdk v1.2.9 既有包，无新增依赖）
// 单次调用封装与 CloudDeployer 端口适配收敛于本文件——火山各产品证书库独立
// （CDN AddCertificate / WAF 服务证书 / ALB-NLB 监听证书 / certificateservice
// ImportCertificate），证书库层与部署器层不拆两文件（任务 Hard Rule 文件清单
// 所限；结构对齐 huawei/aws/azure 三先例：窄接口 + 编译期断言 + 共享助手复用）。
// 任务 2 在本文件追加 BindResource/ListReferences（绑定层）。
//
// 云证书 ID 归一（对齐 huawei SCM / AWS ACM ARN 归一模式）：
// 火山四产品证书库相互独立，同一裸 ID 在不同库语义不同——云证书 ID 统一为
// `{product}:{id}` 前缀形态，product ∈ csv/cdn/waf/alb/nlb：
//   - csv = certificateservice 统一证书库（InstanceID）；
//   - cdn = CDN 证书库（CertId）；waf = WAF 服务证书库（Id）；
//   - alb/nlb = ALB 监听证书库（CertificateId，两产品共用 alb 服务 API）。
//
// 绑定引用（任务 2）与回滚旧 ID 均消费归一形态，GetCert/CleanupOrphan 按前缀
// 路由到对应证书库——解析失败 fail-fast（不猜测）。
//
// 第一段上传落库口径：CloudDeployer.UploadCert 端口无 product 入参（五云均为
// 全局证书库、第一段固定口径上传的端口先例），火山两段式第一段统一以
// certificateservice（csv）口径上传——proposal 明确 csv ImportCertificate 为
// 「上传/回滚/清理基础」，与 huawei SCM/AWS ACM 全局库先例同构。按产品分支
// 上传能力由 uploadForProduct 五分支承载（ID 归一逐产品断言），产品库定向
// 上传与绑定语义在任务 2/任务 5 接通验证中消费。
//
// 本层职责边界（Hard Rule）：
//   - 只做 per 云端口适配 + 限流有界退避重试 + 上传名逐次唯一生成（C7）；
//   - 不做业务级状态机判断——项级 failed/rate_limited 状态落库、回滚语义
//     归 5.7/5.8 引擎与回滚服务；
//   - 退避重试有上限次数与总时长双闸，禁止无限重试；
//   - 私钥明文仅内存传递（SDK 构参副本用后归零），不进日志/错误信息/返回结构。
package deployer

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	volcanosdkalb "github.com/volcengine/volcengine-go-sdk/service/alb"
	volcanosdkcdn "github.com/volcengine/volcengine-go-sdk/service/cdn"
	volcanosdkcsv "github.com/volcengine/volcengine-go-sdk/service/certificateservice"
	volcanosdkclb "github.com/volcengine/volcengine-go-sdk/service/clb"
	volcanosdkwaf "github.com/volcengine/volcengine-go-sdk/service/waf"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ---------------------------------------------------------------------
// 证书库产品前缀与上传常量
// ---------------------------------------------------------------------

// 火山证书库产品前缀（云证书 ID {product}:{id} 归一口径）。
// csv 为 certificateservice 统一证书库（非 domain.Product 枚举成员，属火山
// 证书库层内部形态）；cdn/waf/alb/nlb 与 domain.Product 枚举同值。
const (
	volcanoProductCSV = "csv"
	volcanoProductCDN = "cdn"
	volcanoProductWAF = "waf"
	volcanoProductALB = "alb"
	volcanoProductNLB = "nlb"
)

const (
	// volcanoUploadNamePrefix 上传名前缀（对齐五云 ecam 口径，云侧可辨识平台来源）。
	volcanoUploadNamePrefix = "ecam"
	// volcanoUploadNameMaxLen 上传名长度上限（ALB CertificateName 约束 1~128 字符，
	// 四库中最紧；csv/cdn 无证书名字段或仅描述字段，沿用同一上限）。
	volcanoUploadNameMaxLen = 128
	// volcanoUploadProduct 两段式第一段统一以 certificateservice（csv）口径上传：
	// csv 为火山统一导入证书库（上传/回滚/清理基础），csv InstanceID 即两段式
	// 产物锚点（对齐 huawei awsUploadProduct 统一第一段口径先例）。
	volcanoUploadProduct = volcanoProductCSV
	// volcanoALBCertType ALB 监听证书库上传的证书类型（服务端证书；CA 证书不走
	// 部署上传通路）。实网复核项：类型枚举若与云侧漂移在此单点修正。
	volcanoALBCertType = "server"
	// volcanoCDNListCertSource CDN ListCertInfo 查询 Source 必填枚举（本部署器
	// 经 AddCertificate 上传的证书口径）。实网复核项：Source 枚举若与云侧漂移
	// 在此单点修正。
	volcanoCDNListCertSource = "external"
	// volcanoRefPageSizeDefault 引用枚举（ListReferences）默认分页大小（四产品
	// 统一口径，对齐扫描适配器 volcanoScanPageSizeDefault 语义）。
	volcanoRefPageSizeDefault = int64(100)
)

// volcanoBindCertProducts 绑定目标产品合法集（BindResource product 入参校验）。
var volcanoBindCertProducts = map[string]bool{
	volcanoProductCDN: true,
	volcanoProductWAF: true,
	volcanoProductALB: true,
	volcanoProductNLB: true,
}

// volcanoCloudCertProducts 归一前缀合法集（splitVolcanoCloudCertID 消费）。
var volcanoCloudCertProducts = map[string]bool{
	volcanoProductCSV: true,
	volcanoProductCDN: true,
	volcanoProductWAF: true,
	volcanoProductALB: true,
	volcanoProductNLB: true,
}

// volcanoCloud 火山云标识：cert/domain.Cloud 枚举未含火山（历史五云口径），
// 经 shared/domain 账号 provider 常量转译同值 "volcano"（与发现导入
// discovery_adapter_volcano 口径一致）。
var volcanoCloud = domain.Cloud(sharedomain.CloudProviderVolcano)

// ErrVolcanoProductNotSupported 火山部署器未支持的产品哨兵（clb/dcdn 等；
// 任务 2 绑定层复用同一哨兵口径）。
var ErrVolcanoProductNotSupported = errors.New("volcano deployer: product not supported")

// errVolcanoCloudCertIDNotNormalized 云证书 ID 非归一形态错误（缺前缀/未知
// 前缀/空裸 ID——fail-fast 不猜测）。
var errVolcanoCloudCertIDNotNormalized = errors.New("volcano deployer: cloud cert id is not normalized ({product}:{id})")

// ErrVolcanoCSVCertNotBindable certificateservice（csv）统一证书库实例不可直接
// 绑定产品资源哨兵：火山四产品证书库独立，csv 实例私钥不可再导出（云侧仅
// 返回公开链），产品资源绑定消费的必须是该产品库证书（{product}:{id} 前缀）。
// 两段式第一段统一落 csv（UploadCert 端口无 product 入参的已记录 SPEC 偏差）
// 与本哨兵的接通形态归任务 5 接通验证裁决（「暴露任何编排层与火山 ID 归一的
// 不匹配」）——本层不猜测、不静默降级。
var ErrVolcanoCSVCertNotBindable = errors.New("volcano deployer: csv unified-library cert cannot bind product resource directly (product-library cert required)")

// errVolcanoBindCertProductMismatch 绑定云证书前缀与目标产品不符（如 cdn 证书
// 绑 waf 资源；alb/nlb 共用 ALB 监听证书库除外——与 GetCert/CleanupOrphan
// 路由口径一致）。
var errVolcanoBindCertProductMismatch = errors.New("volcano deployer: cloud cert product does not match bind target product")

// ---------------------------------------------------------------------
// SDK 窄接口与客户端工厂
// ---------------------------------------------------------------------

// volcanoCertLibraryAPI 火山四证书库 SDK 窄接口（生产实现为 volcanoSDKClients；
// 测试注入 fake）。签名即适配原签名——本层只消费 SDK，不改适配面。
// 覆盖证书库层（上传/查询/删除 ×4 库）+ 绑定层（任务 2：CDN BatchDeployCert /
// WAF UpdateDomain / ALB-NLB 监听属性更新）+ 引用枚举只读面（任务 2
// ListReferences 活体重查，与任务 3 扫描适配器同 API 口径）。
type volcanoCertLibraryAPI interface {
	// certificateservice 统一证书库（csv）
	ImportCertificateWithContext(ctx context.Context, input *volcanosdkcsv.ImportCertificateInput, opts ...request.Option) (*volcanosdkcsv.ImportCertificateOutput, error)
	CertificateGetInstanceWithContext(ctx context.Context, input *volcanosdkcsv.CertificateGetInstanceInput, opts ...request.Option) (*volcanosdkcsv.CertificateGetInstanceOutput, error)
	CertificateDeleteInstanceWithContext(ctx context.Context, input *volcanosdkcsv.CertificateDeleteInstanceInput, opts ...request.Option) (*volcanosdkcsv.CertificateDeleteInstanceOutput, error)
	// CDN 证书库
	AddCertificateWithContext(ctx context.Context, input *volcanosdkcdn.AddCertificateInput, opts ...request.Option) (*volcanosdkcdn.AddCertificateOutput, error)
	ListCertInfoWithContext(ctx context.Context, input *volcanosdkcdn.ListCertInfoInput, opts ...request.Option) (*volcanosdkcdn.ListCertInfoOutput, error)
	DeleteCdnCertificateWithContext(ctx context.Context, input *volcanosdkcdn.DeleteCdnCertificateInput, opts ...request.Option) (*volcanosdkcdn.DeleteCdnCertificateOutput, error)
	BatchDeployCertWithContext(ctx context.Context, input *volcanosdkcdn.BatchDeployCertInput, opts ...request.Option) (*volcanosdkcdn.BatchDeployCertOutput, error)
	ListCdnCertInfoWithContext(ctx context.Context, input *volcanosdkcdn.ListCdnCertInfoInput, opts ...request.Option) (*volcanosdkcdn.ListCdnCertInfoOutput, error)
	// WAF 服务证书库 + 防护域名（region 级；Region 经 input/客户端承载）
	UploadWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.UploadWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.UploadWafServiceCertificateOutput, error)
	ListWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.ListWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.ListWafServiceCertificateOutput, error)
	DeleteWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.DeleteWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.DeleteWafServiceCertificateOutput, error)
	ListDomainWithContext(ctx context.Context, input *volcanosdkwaf.ListDomainInput, opts ...request.Option) (*volcanosdkwaf.ListDomainOutput, error)
	UpdateDomainWithContext(ctx context.Context, input *volcanosdkwaf.UpdateDomainInput, opts ...request.Option) (*volcanosdkwaf.UpdateDomainOutput, error)
	// ALB 监听证书库（alb/nlb 共用）+ 监听/转发规则（region 级）
	UploadCertificateWithContext(ctx context.Context, input *volcanosdkalb.UploadCertificateInput, opts ...request.Option) (*volcanosdkalb.UploadCertificateOutput, error)
	DescribeCertificatesWithContext(ctx context.Context, input *volcanosdkalb.DescribeCertificatesInput, opts ...request.Option) (*volcanosdkalb.DescribeCertificatesOutput, error)
	DeleteCertificateWithContext(ctx context.Context, input *volcanosdkalb.DeleteCertificateInput, opts ...request.Option) (*volcanosdkalb.DeleteCertificateOutput, error)
	ModifyListenerAttributesWithContext(ctx context.Context, input *volcanosdkalb.ModifyListenerAttributesInput, opts ...request.Option) (*volcanosdkalb.ModifyListenerAttributesOutput, error)
	DescribeListenersWithContext(ctx context.Context, input *volcanosdkalb.DescribeListenersInput, opts ...request.Option) (*volcanosdkalb.DescribeListenersOutput, error)
	DescribeRulesWithContext(ctx context.Context, input *volcanosdkalb.DescribeRulesInput, opts ...request.Option) (*volcanosdkalb.DescribeRulesOutput, error)
	// NLB 监听器（clb 服务 NLB API；region 级）
	DescribeNLBListenersWithContext(ctx context.Context, input *volcanosdkclb.DescribeNLBListenersInput, opts ...request.Option) (*volcanosdkclb.DescribeNLBListenersOutput, error)
	ModifyNLBListenerAttributesWithContext(ctx context.Context, input *volcanosdkclb.ModifyNLBListenerAttributesInput, opts ...request.Option) (*volcanosdkclb.ModifyNLBListenerAttributesOutput, error)
}

// volcanoSDKClients 火山四证书库 SDK 客户端束（生产实现，volcanoCertLibraryAPI）。
// 显式转发而非四客户端嵌入聚合——各服务方法名空间交叠（如 TagResources*），
// 嵌入提升选择器有歧义风险，显式转发构造性保证窄接口面。
type volcanoSDKClients struct {
	csv *volcanosdkcsv.CERTIFICATESERVICE
	cdn *volcanosdkcdn.CDN
	waf *volcanosdkwaf.WAF
	alb *volcanosdkalb.ALB
	nlb *volcanosdkclb.CLB // NLB 监听器走 clb 服务 NLB API（对齐扫描适配器口径）
}

func (c *volcanoSDKClients) ImportCertificateWithContext(ctx context.Context, input *volcanosdkcsv.ImportCertificateInput, opts ...request.Option) (*volcanosdkcsv.ImportCertificateOutput, error) {
	return c.csv.ImportCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) CertificateGetInstanceWithContext(ctx context.Context, input *volcanosdkcsv.CertificateGetInstanceInput, opts ...request.Option) (*volcanosdkcsv.CertificateGetInstanceOutput, error) {
	return c.csv.CertificateGetInstanceWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) CertificateDeleteInstanceWithContext(ctx context.Context, input *volcanosdkcsv.CertificateDeleteInstanceInput, opts ...request.Option) (*volcanosdkcsv.CertificateDeleteInstanceOutput, error) {
	return c.csv.CertificateDeleteInstanceWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) AddCertificateWithContext(ctx context.Context, input *volcanosdkcdn.AddCertificateInput, opts ...request.Option) (*volcanosdkcdn.AddCertificateOutput, error) {
	return c.cdn.AddCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) ListCertInfoWithContext(ctx context.Context, input *volcanosdkcdn.ListCertInfoInput, opts ...request.Option) (*volcanosdkcdn.ListCertInfoOutput, error) {
	return c.cdn.ListCertInfoWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) DeleteCdnCertificateWithContext(ctx context.Context, input *volcanosdkcdn.DeleteCdnCertificateInput, opts ...request.Option) (*volcanosdkcdn.DeleteCdnCertificateOutput, error) {
	return c.cdn.DeleteCdnCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) UploadWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.UploadWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.UploadWafServiceCertificateOutput, error) {
	return c.waf.UploadWafServiceCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) ListWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.ListWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.ListWafServiceCertificateOutput, error) {
	return c.waf.ListWafServiceCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) DeleteWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.DeleteWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.DeleteWafServiceCertificateOutput, error) {
	return c.waf.DeleteWafServiceCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) UploadCertificateWithContext(ctx context.Context, input *volcanosdkalb.UploadCertificateInput, opts ...request.Option) (*volcanosdkalb.UploadCertificateOutput, error) {
	return c.alb.UploadCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) DescribeCertificatesWithContext(ctx context.Context, input *volcanosdkalb.DescribeCertificatesInput, opts ...request.Option) (*volcanosdkalb.DescribeCertificatesOutput, error) {
	return c.alb.DescribeCertificatesWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) DeleteCertificateWithContext(ctx context.Context, input *volcanosdkalb.DeleteCertificateInput, opts ...request.Option) (*volcanosdkalb.DeleteCertificateOutput, error) {
	return c.alb.DeleteCertificateWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) BatchDeployCertWithContext(ctx context.Context, input *volcanosdkcdn.BatchDeployCertInput, opts ...request.Option) (*volcanosdkcdn.BatchDeployCertOutput, error) {
	return c.cdn.BatchDeployCertWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) ListCdnCertInfoWithContext(ctx context.Context, input *volcanosdkcdn.ListCdnCertInfoInput, opts ...request.Option) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
	return c.cdn.ListCdnCertInfoWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) ListDomainWithContext(ctx context.Context, input *volcanosdkwaf.ListDomainInput, opts ...request.Option) (*volcanosdkwaf.ListDomainOutput, error) {
	return c.waf.ListDomainWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) UpdateDomainWithContext(ctx context.Context, input *volcanosdkwaf.UpdateDomainInput, opts ...request.Option) (*volcanosdkwaf.UpdateDomainOutput, error) {
	return c.waf.UpdateDomainWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) ModifyListenerAttributesWithContext(ctx context.Context, input *volcanosdkalb.ModifyListenerAttributesInput, opts ...request.Option) (*volcanosdkalb.ModifyListenerAttributesOutput, error) {
	return c.alb.ModifyListenerAttributesWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) DescribeListenersWithContext(ctx context.Context, input *volcanosdkalb.DescribeListenersInput, opts ...request.Option) (*volcanosdkalb.DescribeListenersOutput, error) {
	return c.alb.DescribeListenersWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) DescribeRulesWithContext(ctx context.Context, input *volcanosdkalb.DescribeRulesInput, opts ...request.Option) (*volcanosdkalb.DescribeRulesOutput, error) {
	return c.alb.DescribeRulesWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) DescribeNLBListenersWithContext(ctx context.Context, input *volcanosdkclb.DescribeNLBListenersInput, opts ...request.Option) (*volcanosdkclb.DescribeNLBListenersOutput, error) {
	return c.nlb.DescribeNLBListenersWithContext(ctx, input, opts...)
}

func (c *volcanoSDKClients) ModifyNLBListenerAttributesWithContext(ctx context.Context, input *volcanosdkclb.ModifyNLBListenerAttributesInput, opts ...request.Option) (*volcanosdkclb.ModifyNLBListenerAttributesOutput, error) {
	return c.nlb.ModifyNLBListenerAttributesWithContext(ctx, input, opts...)
}

// newVolcanoSDKClients 生产客户端工厂：四服务共用一次会话装配（静态 AK/SK，
// 账号默认地域仅用于客户端签名装配——证书库为全局/账号级服务）。
func newVolcanoSDKClients(creds *sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) {
	return newVolcanoSDKClientsForRegion(creds, volcanoCredsRegion(creds))
}

// newVolcanoSDKClientsForRegion 地域级客户端工厂（WAF/ALB/NLB 为地域级产品，
// 绑定定位与引用枚举按账号地域遍历——对齐扫描适配器 per-region 客户端口径）。
func newVolcanoSDKClientsForRegion(creds *sharedomain.CloudAccount, region string) (volcanoCertLibraryAPI, error) {
	if strings.TrimSpace(region) == "" {
		region = "cn-beijing"
	}
	config := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(creds.AccessKeyID, creds.AccessKeySecret, "")).
		WithRegion(region)
	sess, err := session.NewSession(config)
	if err != nil {
		return nil, fmt.Errorf("创建火山证书库会话失败: %w", err)
	}
	return &volcanoSDKClients{
		csv: volcanosdkcsv.New(sess),
		cdn: volcanosdkcdn.New(sess),
		waf: volcanosdkwaf.New(sess),
		alb: volcanosdkalb.New(sess),
		nlb: volcanosdkclb.New(sess),
	}, nil
}

// volcanoCredsRegion 账号默认地域（Regions[0]，缺省 cn-beijing，与既有火山
// 适配器 certCredsRegion 同口径）。
func volcanoCredsRegion(creds *sharedomain.CloudAccount) string {
	if creds == nil || len(creds.Regions) == 0 {
		return "cn-beijing"
	}
	return creds.Regions[0]
}

// ---------------------------------------------------------------------
// VolcanoDeployer
// ---------------------------------------------------------------------

// VolcanoDeployer 火山引擎 CloudDeployer：覆盖 cdn/waf/alb/nlb 四产品 +
// certificateservice 统一证书库。任务 1 交付证书库层三方法（UploadCert/
// GetCert/CleanupOrphan）；任务 2 追加绑定层 BindResource（四产品分支）与
// 只读发现 ListReferences（活体重查面）。
type VolcanoDeployer struct {
	mappings domain.CloudCertMappingRepository // 可空：任务 2 ListReferences 指纹映射反查
	retry    RetryPolicy
	now      func() time.Time                                 // 上传名时间源（测试可注入）
	randHex  func(n int) string                               // 上传名随机后缀（测试可注入）
	sleep    func(ctx context.Context, d time.Duration) error // 退避睡眠（测试可注入）

	newClients func(creds *sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) // 客户端工厂（测试注入 fake）
	// newClientsForRegion 地域级客户端工厂（任务 2 绑定定位/引用枚举按地域遍历；
	// 测试注入 fake）。与 newClients 分离：任务 1 证书库层三方法固定账号默认地域
	// 口径不受影响。
	newClientsForRegion func(creds *sharedomain.CloudAccount, region string) (volcanoCertLibraryAPI, error)
	refPageSize         int64 // 引用枚举分页大小（零值回退默认；测试可缩小覆盖翻页分支）
}

// 编译期断言：满足 CloudDeployer 端口（供 5.3 CloudAPIChannel 注入；任务 2
// 追加绑定层后断言持续生效）。
var _ CloudDeployer = (*VolcanoDeployer)(nil)

// NewVolcanoDeployer 创建火山引擎部署器。mappings 允许 nil（任务 2 ListReferences
// 跳过映射反查）。客户端工厂缺省为 newVolcanoSDKClients（测试经选项或直接
// 字段注入 fake）。
func NewVolcanoDeployer(mappings domain.CloudCertMappingRepository, opts ...VolcanoOption) *VolcanoDeployer {
	d := &VolcanoDeployer{
		mappings:            mappings,
		retry:               DefaultRetryPolicy(),
		now:                 time.Now,
		randHex:             randomHexSuffix,
		sleep:               sleepWithContext,
		newClients:          newVolcanoSDKClients,
		newClientsForRegion: newVolcanoSDKClientsForRegion,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// VolcanoOption VolcanoDeployer 装配选项。
type VolcanoOption func(*VolcanoDeployer)

// WithVolcanoRetryPolicy 覆盖限流退避参数（应用 config 读取路径；零值/非法
// 配置经 normalized 回退缺省保守值，重试安全侧）。
func WithVolcanoRetryPolicy(p RetryPolicy) VolcanoOption {
	return func(d *VolcanoDeployer) { d.retry = p.normalized() }
}

// WithVolcanoCertLibrary 注入证书库 SDK 窄接口实现（功能级测试 seam，对齐
// huawei/aws/azure 构参注入先例）：默认/地域级客户端工厂均返回同一实现
// （绑定定位与引用枚举的地域遍历落在同一 fake 上）。生产缺省客户端工厂
// newVolcanoSDKClients 不受影响。
func WithVolcanoCertLibrary(api volcanoCertLibraryAPI) VolcanoOption {
	return func(d *VolcanoDeployer) {
		d.newClients = func(*sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) {
			return api, nil
		}
		d.newClientsForRegion = func(*sharedomain.CloudAccount, string) (volcanoCertLibraryAPI, error) {
			return api, nil
		}
	}
}

// Stop 透传导配层限流器停止（火山证书库客户端无令牌限流器，no-op；与五云
// 部署器同形态保留接缝）。
func (d *VolcanoDeployer) Stop() {}

// client 按账号装配证书库客户端（逐调用临时对象；工厂失败显式报错）。
func (d *VolcanoDeployer) client(acct *sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) {
	return d.newClients(acct)
}

// withRetry 有界重试主干（实现收敛于 boundedRetry 单点，与五云同口径）：
//   - ErrCloudRateLimited → 按固定序列退避后重试（计入总时长上限）；
//   - 上传名冲突（保守启发式，B2 同口径）→ 不退避立即换名重试（仅上传语境；
//     csv/cdn 无证书名字段或可重复导入，该分支常态休眠）；
//   - 次数或总时长耗尽 → 包装末次错误返回（哨兵语义经 %w 保留）。
//
// 业务级成败状态归 5.7 引擎（Hard Rule：本层无状态机判断）。
func (d *VolcanoDeployer) withRetry(ctx context.Context, fn func(attempt int) error) error {
	return boundedRetry(ctx, d.retry, d.sleep, fn)
}

// ---------------------------------------------------------------------
// C7：上传名唯一生成（与五云同公式）
// ---------------------------------------------------------------------

// generateUploadName 生成上传名 ecam-{指纹前8}-{unix秒}-{随机后缀}（C7 公式
// 实现收敛于 formatUploadName 单点）：unix 秒 + 随机后缀保证逐次唯一（重试
// 不复用可能已成功的名称——重试即新副本，孤儿清理兜底）。csv/cdn 库无证书名
// 字段（名称仅作 WAF Name / ALB CertificateName / CDN Desc 承载），唯一性由
// 云侧每次导入新实例构造保证。
func (d *VolcanoDeployer) generateUploadName(certPEM string) string {
	return formatUploadName(volcanoUploadNamePrefix, volcanoUploadNameMaxLen, certPEM, d.now, d.randHex)
}

// ---------------------------------------------------------------------
// 云证书 ID 归一（{product}:{id}）
// ---------------------------------------------------------------------

// normalizeVolcanoCloudCertID 裸 ID → {product}:{id} 归一形态（去首尾空白）。
func normalizeVolcanoCloudCertID(product, rawID string) string {
	return product + ":" + strings.TrimSpace(rawID)
}

// splitVolcanoCloudCertID 归一形态 → (product, 裸 ID)；非归一形态（缺前缀/
// 未知前缀/空裸 ID）返回 ok=false（fail-fast 不猜测）。
func splitVolcanoCloudCertID(cloudCertID string) (product, rawID string, ok bool) {
	p, raw, found := strings.Cut(cloudCertID, ":")
	if !found || !volcanoCloudCertProducts[p] || strings.TrimSpace(raw) == "" {
		return "", "", false
	}
	return p, raw, true
}

// ---------------------------------------------------------------------
// CloudDeployer 五方法（任务 1：UploadCert/GetCert/CleanupOrphan）
// ---------------------------------------------------------------------

// UploadCert 两段式第一段：生成唯一名（C7）上传火山证书库，返回归一云证书 ID。
// 第一段统一以 certificateservice（csv）口径上传（端口无 product 入参，见文件
// 头说明）；每次尝试（含重试）生成全新名称——重试不得复用可能已成功的名称
// （C7：重试即新副本，孤儿清理兜底）。
// 私钥卫生（Hard Rule）：keyPEM 明文仅内存传递——SDK 构参副本经
// string(keyCopy) 传入后即刻 Zeroize（string 副本为 SDK 字段类型所必需，
// 从不写入日志/错误信息/返回结构）。
func (d *VolcanoDeployer) UploadCert(ctx context.Context, creds Credential, certPEM string, keyPEM []byte) (string, error) {
	return d.uploadCertIntoLibrary(ctx, creds, volcanoUploadProduct, certPEM, keyPEM)
}

// UploadCertForProduct 两段式第一段产品库定向上传（ProductAwareUploader 可选
// 升级端口实现，cert-volcano-deployer 任务 5 接通裁决）：火山四产品证书库
// 相互独立，统一 csv 库实例私钥不可再导出、无法在适配层内晋升产品库
// （ErrVolcanoCSVCertNotBindable 已显式化该缺口），两段式主流程第一段按目标
// 产品定向其产品证书库上传，产物 {product}:{id} 即该产品库可绑定证书。
// product 仅接受四绑定目标（csv 仅证书库非绑定目标）；每次尝试（含重试）
// 生成全新名称——重试不得复用可能已成功的名称（C7：重试即新副本，孤儿清理
// 兜底）。私钥卫生（Hard Rule）同 UploadCert：keyPEM 明文仅内存传递，SDK
// 构参副本用后即刻 Zeroize，从不写入日志/错误信息/返回结构。
func (d *VolcanoDeployer) UploadCertForProduct(ctx context.Context, creds Credential, product, certPEM string, keyPEM []byte) (string, error) {
	if !volcanoBindCertProducts[product] {
		return "", fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
	return d.uploadCertIntoLibrary(ctx, creds, product, certPEM, keyPEM)
}

// 编译期断言：产品库定向上传升级端口（CloudAPIChannel.Deploy 类型断言分发）。
var _ ProductAwareUploader = (*VolcanoDeployer)(nil)

// uploadCertIntoLibrary 上传主干（UploadCert 统一 csv 口径 / UploadCertForProduct
// 产品库定向共用的单点实现）：生成唯一名（C7）上传指定产品证书库，返回归一
// 云证书 ID。
func (d *VolcanoDeployer) uploadCertIntoLibrary(ctx context.Context, creds Credential, product, certPEM string, keyPEM []byte) (string, error) {
	acct, err := d.account(creds)
	if err != nil {
		return "", err
	}
	if certPEM == "" || len(keyPEM) == 0 {
		return "", errors.New("volcano deployer: upload cert requires cert PEM and key PEM")
	}
	keyCopy := append([]byte(nil), keyPEM...) // SDK 构参副本；调用返回后即刻归零
	defer cloudx.Zeroize(keyCopy)
	var cloudCertID string
	err = d.withRetry(ctx, func(int) error {
		name := d.generateUploadName(certPEM)
		id, uerr := d.uploadForProduct(ctx, acct, product, name, certPEM, string(keyCopy))
		if uerr != nil {
			return uerr
		}
		cloudCertID = id
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("volcano deployer: upload cert: %w", err)
	}
	return cloudCertID, nil
}

// uploadForProduct 按产品分支上传至对应火山证书库，返回 {product}:{id} 归一
// 云证书 ID（cert-volcano-deployer AC-1 多产品分支）。keyPEM 为明文私钥 string
// 副本，仅供 SDK 构参，不落日志/错误信息。
func (d *VolcanoDeployer) uploadForProduct(ctx context.Context, acct *sharedomain.CloudAccount, product, name, certPEM, keyPEM string) (string, error) {
	client, err := d.client(acct)
	if err != nil {
		return "", err
	}
	switch product {
	case volcanoProductCSV:
		// certificateservice 统一证书库：Repeatable=true 允许同材料重复导入产生
		// 新实例（C7 重试即新副本）。
		out, err := client.ImportCertificateWithContext(ctx, &volcanosdkcsv.ImportCertificateInput{
			CertificateInfo: &volcanosdkcsv.CertificateInfoForImportCertificateInput{
				CertificateChain: volcengine.String(certPEM),
				PrivateKey:       volcengine.String(keyPEM),
			},
			Repeatable: volcengine.Bool(true),
		})
		if err != nil {
			return "", wrapVolcanoCertErr("import_certificate", err)
		}
		if out == nil || out.InstanceId == nil || *out.InstanceId == "" {
			return "", fmt.Errorf("volcano import_certificate: empty instance id")
		}
		return normalizeVolcanoCloudCertID(volcanoProductCSV, *out.InstanceId), nil
	case volcanoProductCDN:
		// CDN 证书库：AddCertificate 无证书名字段，C7 名经 Desc 承载。
		out, err := client.AddCertificateWithContext(ctx, &volcanosdkcdn.AddCertificateInput{
			Certificate: volcengine.String(certPEM),
			PrivateKey:  volcengine.String(keyPEM),
			Desc:        volcengine.String(name),
			Repeatable:  volcengine.Bool(true),
		})
		if err != nil {
			return "", wrapVolcanoCertErr("cdn_add_certificate", err)
		}
		if out == nil || out.CertId == nil || *out.CertId == "" {
			return "", fmt.Errorf("volcano cdn_add_certificate: empty cert id")
		}
		return normalizeVolcanoCloudCertID(volcanoProductCDN, *out.CertId), nil
	case volcanoProductWAF:
		// WAF 服务证书库：Name/Description 为 API 必填，C7 名承载。
		out, err := client.UploadWafServiceCertificateWithContext(ctx, &volcanosdkwaf.UploadWafServiceCertificateInput{
			Name:        volcengine.String(name),
			Description: volcengine.String(name),
			PublicKey:   volcengine.String(certPEM),
			PrivateKey:  volcengine.String(keyPEM),
		})
		if err != nil {
			return "", wrapVolcanoCertErr("waf_upload_service_certificate", err)
		}
		if out == nil || out.Id == nil || *out.Id == 0 {
			return "", fmt.Errorf("volcano waf_upload_service_certificate: empty cert id")
		}
		return normalizeVolcanoCloudCertID(volcanoProductWAF, strconv.FormatInt(int64(*out.Id), 10)), nil
	case volcanoProductALB, volcanoProductNLB:
		// ALB 监听证书库（alb/nlb 共用 alb 服务 API）：C7 名经 CertificateName 承载。
		out, err := client.UploadCertificateWithContext(ctx, &volcanosdkalb.UploadCertificateInput{
			CertificateName: volcengine.String(name),
			CertificateType: volcengine.String(volcanoALBCertType),
			PublicKey:       volcengine.String(certPEM),
			PrivateKey:      volcengine.String(keyPEM),
		})
		if err != nil {
			return "", wrapVolcanoCertErr("alb_upload_certificate", err)
		}
		if out == nil || out.CertificateId == nil || *out.CertificateId == "" {
			return "", fmt.Errorf("volcano alb_upload_certificate: empty certificate id")
		}
		return normalizeVolcanoCloudCertID(product, *out.CertificateId), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
}

// BindResource 两段式第二段（CDN BatchDeployCert / WAF 防护域名证书替换 /
// ALB-NLB 监听证书更新）：云证书 ID 为 {product}:{id} 归一形态（fail-fast 解析），
// 证书前缀须与目标产品证书库兼容（alb/nlb 共用 ALB 监听证书库，与 GetCert/
// CleanupOrphan 路由口径一致；csv 统一库实例不可直接绑定——ErrVolcanoCSVCertNotBindable）。
// 幂等：各绑定 API 均为「置位」语义（把资源证书设为指定 ID），同一
// (resource, cloudCertID) 重绑收敛同结果。
func (d *VolcanoDeployer) BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	certProduct, rawID, ok := splitVolcanoCloudCertID(cloudCertID)
	if !ok {
		return fmt.Errorf("%w: %q", errVolcanoCloudCertIDNotNormalized, cloudCertID)
	}
	if err := volcanoBindProductCompat(product, certProduct); err != nil {
		return err
	}
	if strings.TrimSpace(resourceID) == "" {
		return fmt.Errorf("volcano deployer: bind %s requires non-empty resource id", product)
	}
	if err := d.withRetry(ctx, func(int) error {
		return d.bindForProduct(ctx, acct, product, resourceID, rawID)
	}); err != nil {
		return fmt.Errorf("volcano deployer: bind %s resource %s: %w", product, resourceID, err)
	}
	return nil
}

// volcanoBindProductCompat 绑定目标产品与云证书前缀兼容性校验：
//   - 目标产品须为四部署产品之一（csv 仅证书库，非绑定目标）；
//   - 证书前缀与目标产品同库（同前缀，或 alb/nlb 互跨——共用 ALB 监听证书库）；
//   - csv 前缀 → ErrVolcanoCSVCertNotBindable（两段式第一段落库与绑定的接通
//     形态归任务 5 裁决，本层 fail-fast 不猜测）。
func volcanoBindProductCompat(product, certProduct string) error {
	if !volcanoBindCertProducts[product] {
		return fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
	if certProduct == product {
		return nil
	}
	if certProduct == volcanoProductCSV {
		return fmt.Errorf("%w: cert %q -> target product %q", ErrVolcanoCSVCertNotBindable, certProduct, product)
	}
	albFamily := (certProduct == volcanoProductALB || certProduct == volcanoProductNLB) &&
		(product == volcanoProductALB || product == volcanoProductNLB)
	if albFamily {
		return nil
	}
	return fmt.Errorf("%w: cert %q -> target product %q", errVolcanoBindCertProductMismatch, certProduct, product)
}

// bindForProduct 按目标产品路由绑定（单次定位 + 单次绑定调用）。
func (d *VolcanoDeployer) bindForProduct(ctx context.Context, acct *sharedomain.CloudAccount, product, resourceID, certID string) error {
	switch product {
	case volcanoProductCDN:
		return d.bindCDN(ctx, acct, resourceID, certID)
	case volcanoProductWAF:
		return d.bindWAF(ctx, acct, resourceID, certID)
	case volcanoProductALB, volcanoProductNLB:
		return d.bindLBListener(ctx, acct, product, resourceID, certID)
	default:
		return fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
}

// bindCDN CDN 加速域名绑定（resourceID=加速域名，对齐扫描域名粒度）：
// BatchDeployCert 置位语义（重绑收敛）。逐域部署结果（DeployResult）非 success
// 即失败（携带云侧 ErrorMsg，不含敏感材料）。实网复核项：Status 枚举大小写
// 以云侧为准。
func (d *VolcanoDeployer) bindCDN(ctx context.Context, acct *sharedomain.CloudAccount, domain, certID string) error {
	client, err := d.client(acct) // CDN 为全局服务（账号默认地域签名装配）
	if err != nil {
		return err
	}
	out, err := client.BatchDeployCertWithContext(ctx, &volcanosdkcdn.BatchDeployCertInput{
		CertId: volcengine.String(certID),
		Domain: volcengine.String(domain),
	})
	if err != nil {
		return wrapVolcanoCertErr("cdn_batch_deploy_cert", err)
	}
	for _, item := range out.DeployResult {
		if item == nil || item.Status == nil {
			continue
		}
		if !strings.EqualFold(*item.Status, "success") {
			return fmt.Errorf("volcano cdn_batch_deploy_cert: domain %s deploy failed: %s",
				volcengine.StringValue(item.Domain), volcengine.StringValue(item.ErrorMsg))
		}
	}
	return nil
}

// bindWAF WAF 防护域名证书替换（resourceID=防护域名）：逐地域定位域名
// （ListDomain 分页，Region 经 input 透传）读取现网 AccessMode（UpdateDomain
// 必填，缺失 fail-fast——不猜默认值），UpdateDomain 仅携带 Domain/CertificateID/
// AccessMode 三字段（其余字段 omitempty 不下发）。实网复核项：UpdateDomain 对
// 未携带字段的云侧保留语义以此单点验证。
func (d *VolcanoDeployer) bindWAF(ctx context.Context, acct *sharedomain.CloudAccount, domain, certID string) error {
	certNum, err := strconv.ParseInt(certID, 10, 32)
	if err != nil {
		return fmt.Errorf("volcano waf bind: cert id %q is not a waf service certificate id (numeric)", certID)
	}
	regions := volcanoRegions(acct)
	for _, region := range regions {
		client, cerr := d.newClientsForRegion(acct, region)
		if cerr != nil {
			return cerr
		}
		item, found, lerr := findVolcanoWAFDomain(ctx, client, region, domain)
		if lerr != nil {
			return lerr
		}
		if !found {
			continue // 地域未命中继续（域名定位与 AccessMode 读取一并完成）
		}
		if item.AccessMode == nil {
			return fmt.Errorf("volcano waf_update_domain: domain %s access mode unavailable (region %s)", domain, region)
		}
		if _, uerr := client.UpdateDomainWithContext(ctx, &volcanosdkwaf.UpdateDomainInput{
			Domain:        volcengine.String(domain),
			CertificateID: volcengine.Int32(int32(certNum)),
			AccessMode:    item.AccessMode,
		}); uerr != nil {
			return wrapVolcanoCertErr("waf_update_domain", uerr)
		}
		return nil
	}
	return fmt.Errorf("volcano waf_update_domain: domain %s not found in regions %v", domain, regions)
}

// findVolcanoWAFDomain 逐页定位防护域名条目（分页耗尽/空页即未命中）。
func findVolcanoWAFDomain(ctx context.Context, client volcanoCertLibraryAPI, region, domain string) (*volcanosdkwaf.DataForListDomainOutput, bool, error) {
	pageSize := int32(volcanoRefPageSizeDefault)
	for page := int32(1); ; page++ {
		out, err := client.ListDomainWithContext(ctx, &volcanosdkwaf.ListDomainInput{
			Page:     volcengine.Int32(page),
			PageSize: volcengine.Int32(pageSize),
			Region:   volcengine.String(region),
		})
		if err != nil {
			return nil, false, wrapVolcanoCertErr("waf_list_domain", err)
		}
		if out == nil || len(out.Data) == 0 {
			return nil, false, nil
		}
		for _, item := range out.Data {
			if item != nil && volcengine.StringValue(item.Domain) == domain {
				return item, true, nil
			}
		}
		if int32(len(out.Data)) < pageSize {
			return nil, false, nil
		}
	}
}

// bindLBListener ALB/NLB 监听证书绑定（resourceID="{lbId}/{listenerId}" 监听
// 复合形态，纯监听形态容忍——对齐快照旧引用升级窗口互认口径）：逐地域经
// ListenerIds 定点定位监听（对齐 aliyun findALBListener 先例），命中地域执行
// 监听属性置位；HTTP 监听无服务器证书显式报错（对齐扫描跳过口径）。
func (d *VolcanoDeployer) bindLBListener(ctx context.Context, acct *sharedomain.CloudAccount, product, resourceID, certID string) error {
	lbID, listenerID := volcanoSplitLBResourceID(resourceID)
	if listenerID == "" {
		return fmt.Errorf("volcano %s bind: resource id %q must be {lbId}/{listenerId} composite", product, resourceID)
	}
	regions := volcanoRegions(acct)
	for _, region := range regions {
		client, cerr := d.newClientsForRegion(acct, region)
		if cerr != nil {
			return cerr
		}
		protocol, found, lerr := findVolcanoLBListener(ctx, client, product, lbID, listenerID)
		if lerr != nil {
			return lerr
		}
		if !found {
			continue
		}
		if product == volcanoProductALB && strings.EqualFold(protocol, "http") {
			return fmt.Errorf("volcano alb bind: listener %s is HTTP (no server certificate)", listenerID)
		}
		switch product {
		case volcanoProductALB:
			if _, err := client.ModifyListenerAttributesWithContext(ctx, &volcanosdkalb.ModifyListenerAttributesInput{
				ListenerId:    volcengine.String(listenerID),
				CertificateId: volcengine.String(certID),
			}); err != nil {
				return wrapVolcanoCertErr("alb_modify_listener_attributes", err)
			}
		case volcanoProductNLB:
			if _, err := client.ModifyNLBListenerAttributesWithContext(ctx, &volcanosdkclb.ModifyNLBListenerAttributesInput{
				ListenerId:    volcengine.String(listenerID),
				CertificateId: volcengine.String(certID),
			}); err != nil {
				return wrapVolcanoCertErr("nlb_modify_listener_attributes", err)
			}
		}
		return nil
	}
	return fmt.Errorf("volcano %s bind: listener %s not found in regions %v", product, listenerID, regions)
}

// findVolcanoLBListener 逐地域定点定位监听（ListenerIds 过滤单查，复合形态附
// LoadBalancerId 收窄）。返回协议（alb HTTP 拒绑判定用）与命中标记。
func findVolcanoLBListener(ctx context.Context, client volcanoCertLibraryAPI, product, lbID, listenerID string) (protocol string, found bool, err error) {
	switch product {
	case volcanoProductALB:
		out, err := client.DescribeListenersWithContext(ctx, &volcanosdkalb.DescribeListenersInput{
			ListenerIds:    []*string{volcengine.String(listenerID)},
			LoadBalancerId: nullableVolcanoString(lbID),
		})
		if err != nil {
			return "", false, wrapVolcanoCertErr("alb_describe_listeners", err)
		}
		if out == nil || len(out.Listeners) == 0 || out.Listeners[0] == nil {
			return "", false, nil
		}
		return volcengine.StringValue(out.Listeners[0].Protocol), true, nil
	case volcanoProductNLB:
		out, err := client.DescribeNLBListenersWithContext(ctx, &volcanosdkclb.DescribeNLBListenersInput{
			ListenerIds:    []*string{volcengine.String(listenerID)},
			LoadBalancerId: nullableVolcanoString(lbID),
		})
		if err != nil {
			return "", false, wrapVolcanoCertErr("nlb_describe_listeners", err)
		}
		if out == nil || len(out.Listeners) == 0 || out.Listeners[0] == nil {
			return "", false, nil
		}
		return volcengine.StringValue(out.Listeners[0].Protocol), true, nil
	default:
		return "", false, fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
}

// ListReferences 只读发现（活体重查面）：四产品资源枚举 → CertReference，与
// 任务 3 扫描适配器共享 resourceId/ReferencedCloudCertID 形态（CDN/WAF=域名、
// ALB/NLB={lbId}/{listenerId} 监听复合 ID、{product}:{id} 归一前缀；ALB 按
// served domains 展开、NLB 无规则为空），映射反查键与扫描产出同口径。指纹解析
// 口径同 3.5/5.4/5.5（映射反查 → GetCert 要素〔仅接受 SHA256 对齐口径；csv
// 链解析/cdn 原生 sha256 通道，waf/alb/nlb 无法复核〕→ 确定性占位指纹，占位
// 公式与 3.5 扫描路径一致可对账）；同云证书多引用去重查询。
func (d *VolcanoDeployer) ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error) {
	acct, err := d.account(creds)
	if err != nil {
		return nil, err
	}
	var found []volcanoRef
	err = d.withRetry(ctx, func(int) error {
		var e error
		found, e = d.listRefsForProduct(ctx, acct, product)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("volcano deployer: list %s references: %w", product, err)
	}
	out := make([]domain.CertReference, 0, len(found))
	cache := make(map[string]string, len(found))
	for _, r := range found {
		out = append(out, domain.CertReference{
			CertFingerprint:       d.resolveVolcanoFingerprint(ctx, acct, acct.Name, r.cloudCertID, cache),
			Cloud:                 volcanoCloud,
			Product:               domain.Product(r.product),
			ResourceID:            r.resourceID,
			ReferencedCloudCertID: r.cloudCertID,
			AccountKey:            acct.Name,
			ServedDomains:         r.servedDomains,
		})
	}
	return out, nil
}

// volcanoRef 引用枚举内部形态（产品/资源 ID/归一云证书 ID/served domains）。
type volcanoRef struct {
	product       string
	resourceID    string
	cloudCertID   string
	servedDomains []string
}

// listRefsForProduct 按产品分发引用枚举（错误透传，partials 隔离归服务层编排）。
func (d *VolcanoDeployer) listRefsForProduct(ctx context.Context, acct *sharedomain.CloudAccount, product string) ([]volcanoRef, error) {
	switch product {
	case volcanoProductCDN:
		return d.listCDNRefs(ctx, acct)
	case volcanoProductWAF:
		return d.listWAFRefs(ctx, acct)
	case volcanoProductALB:
		return d.listALBRefs(ctx, acct)
	case volcanoProductNLB:
		return d.listNLBRefs(ctx, acct)
	default:
		return nil, fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
}

// listCDNRefs CDN 加速域名证书引用（域名粒度）：ListCdnCertInfo 分页枚举证书
// 库（Source 不过滤——活体重查与扫描同口径），逐证书按已配置域名展开引用；
// 未配置域名的证书不构成引用。
func (d *VolcanoDeployer) listCDNRefs(ctx context.Context, acct *sharedomain.CloudAccount) ([]volcanoRef, error) {
	client, err := d.client(acct) // CDN 为全局服务
	if err != nil {
		return nil, err
	}
	pageSize := d.refPageSizeOr()
	var refs []volcanoRef
	for pageNum := int64(1); ; pageNum++ {
		out, err := client.ListCdnCertInfoWithContext(ctx, &volcanosdkcdn.ListCdnCertInfoInput{
			PageNum:  volcengine.Int64(pageNum),
			PageSize: volcengine.Int64(pageSize),
		})
		if err != nil {
			return nil, wrapVolcanoCertErr("cdn_list_cert_info", err)
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
				refs = append(refs, volcanoRef{
					product:     volcanoProductCDN,
					resourceID:  dom,
					cloudCertID: normalizeVolcanoCloudCertID(volcanoProductCDN, certID),
				})
			}
		}
		if int64(len(out.CertInfo)) < pageSize {
			break // 整页未满即最后一页
		}
	}
	return refs, nil
}

// listWAFRefs WAF 防护域名证书引用（域名粒度）：按账号地域遍历 ListDomain
// 分页——列表内联 CertificateID（WAF 服务证书库 Id，0/缺省=未配置证书不构成
// 引用），无 N+1 展开（对齐扫描 listWAFReferences 口径）。
func (d *VolcanoDeployer) listWAFRefs(ctx context.Context, acct *sharedomain.CloudAccount) ([]volcanoRef, error) {
	var refs []volcanoRef
	pageSize := d.refPageSizeOr()
	for _, region := range volcanoRegions(acct) {
		client, err := d.newClientsForRegion(acct, region)
		if err != nil {
			return nil, err
		}
		for page := int32(1); ; page++ {
			out, err := client.ListDomainWithContext(ctx, &volcanosdkwaf.ListDomainInput{
				Page:     volcengine.Int32(page),
				PageSize: volcengine.Int32(int32(pageSize)),
				Region:   volcengine.String(region),
			})
			if err != nil {
				return nil, wrapVolcanoCertErr("waf_list_domain", err)
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
				refs = append(refs, volcanoRef{
					product:     volcanoProductWAF,
					resourceID:  dom,
					cloudCertID: normalizeVolcanoCloudCertID(volcanoProductWAF, strconv.FormatInt(certID, 10)),
				})
			}
			if int64(len(out.Data)) < int64(pageSize) {
				break // 整页未满即最后一页
			}
		}
	}
	return refs, nil
}

// listALBRefs ALB 监听证书引用（L7 终结）：按账号地域遍历 DescribeListeners
// 分页——主证书（CertificateId，证书中心形态回退 CertCenterCertificateId）与
// SNI 扩展证书（DomainExtensions，与主证书同 ID 去重）均为引用；HTTP 监听无
// 服务器证书跳过；served domains 逐监听经转发规则展开（失败置空不阻塞——
// 回退 coverage 语义，对齐扫描/aliyun 口径）。
func (d *VolcanoDeployer) listALBRefs(ctx context.Context, acct *sharedomain.CloudAccount) ([]volcanoRef, error) {
	var refs []volcanoRef
	pageSize := d.refPageSizeOr()
	for _, region := range volcanoRegions(acct) {
		client, err := d.newClientsForRegion(acct, region)
		if err != nil {
			return nil, err
		}
		for pageNum := int64(1); ; pageNum++ {
			out, err := client.DescribeListenersWithContext(ctx, &volcanosdkalb.DescribeListenersInput{
				PageNumber: volcengine.Int64(pageNum),
				PageSize:   volcengine.Int64(pageSize),
			})
			if err != nil {
				return nil, wrapVolcanoCertErr("alb_describe_listeners", err)
			}
			if out == nil || len(out.Listeners) == 0 {
				break
			}
			for _, listener := range out.Listeners {
				refs = append(refs, d.albListenerRefs(ctx, client, listener)...)
			}
			if int64(len(out.Listeners)) < pageSize {
				break // 整页未满即最后一页
			}
		}
	}
	return refs, nil
}

// albListenerRefs 展开单个 ALB 监听的证书引用（主证书 + SNI 扩展证书）。
func (d *VolcanoDeployer) albListenerRefs(ctx context.Context, client volcanoCertLibraryAPI, listener *volcanosdkalb.ListenerForDescribeListenersOutput) []volcanoRef {
	if listener == nil || strings.EqualFold(volcengine.StringValue(listener.Protocol), "http") {
		return nil // HTTP 监听无服务器证书
	}
	listenerID := volcengine.StringValue(listener.ListenerId)
	if listenerID == "" {
		return nil
	}
	resourceID := volcanoLBResourceID(volcengine.StringValue(listener.LoadBalancerId), listenerID)
	served := volcanoALBServedDomains(ctx, client, listenerID)

	defaultCertID := strings.TrimSpace(volcengine.StringValue(listener.CertificateId))
	if defaultCertID == "" {
		// 证书中心（cert center）形态监听：CertificateId 缺省时回退
		// CertCenterCertificateId（对齐扫描口径；实网复核项：两形态互斥性以云侧为准）。
		defaultCertID = strings.TrimSpace(volcengine.StringValue(listener.CertCenterCertificateId))
	}
	var refs []volcanoRef
	if defaultCertID != "" {
		refs = append(refs, volcanoRef{
			product:       volcanoProductALB,
			resourceID:    resourceID,
			cloudCertID:   normalizeVolcanoCloudCertID(volcanoProductALB, defaultCertID),
			servedDomains: served,
		})
	}
	for _, ext := range listener.DomainExtensions {
		extCertID := ""
		if ext != nil {
			extCertID = strings.TrimSpace(volcengine.StringValue(ext.CertificateId))
		}
		if ext == nil || extCertID == "" || extCertID == defaultCertID {
			continue
		}
		refs = append(refs, volcanoRef{
			product:       volcanoProductALB,
			resourceID:    resourceID,
			cloudCertID:   normalizeVolcanoCloudCertID(volcanoProductALB, extCertID),
			servedDomains: served,
		})
	}
	return refs
}

// volcanoALBServedDomains 逐监听遍历转发规则，提取 Host 条件值（served
// hostname）。失败返回 nil（不阻塞引用枚举主干——对齐 aliyun listALBServedDomains）。
func volcanoALBServedDomains(ctx context.Context, client volcanoCertLibraryAPI, listenerID string) []string {
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

// listNLBRefs NLB 监听证书引用（L4 TLS）：按账号地域遍历 DescribeNLBListeners
// （NextToken 分页）——证书内联监听器，无证书监听不产出引用；NLB 无转发规则，
// ServedDomains 为空。
func (d *VolcanoDeployer) listNLBRefs(ctx context.Context, acct *sharedomain.CloudAccount) ([]volcanoRef, error) {
	var refs []volcanoRef
	for _, region := range volcanoRegions(acct) {
		client, err := d.newClientsForRegion(acct, region)
		if err != nil {
			return nil, err
		}
		var nextToken *string
		for {
			out, err := client.DescribeNLBListenersWithContext(ctx, &volcanosdkclb.DescribeNLBListenersInput{
				MaxResults: volcengine.Int64(d.refPageSizeOr()),
				NextToken:  nextToken,
			})
			if err != nil {
				return nil, wrapVolcanoCertErr("nlb_describe_listeners", err)
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
				refs = append(refs, volcanoRef{
					product:     volcanoProductNLB,
					resourceID:  volcanoLBResourceID(volcengine.StringValue(listener.LoadBalancerId), listenerID),
					cloudCertID: normalizeVolcanoCloudCertID(volcanoProductNLB, certID),
				})
			}
			if out.NextToken == nil || *out.NextToken == "" {
				break
			}
			nextToken = out.NextToken
		}
	}
	return refs, nil
}

// resolveVolcanoFingerprint 引用指纹解析（逐次发现去重缓存，对齐 huawei/aws
// resolveFingerprint 结构）。
func (d *VolcanoDeployer) resolveVolcanoFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, accountKey, cloudCertID string, cache map[string]string,
) string {
	cacheKey := strings.Join([]string{string(volcanoCloud), accountKey, cloudCertID}, "|")
	if fp, ok := cache[cacheKey]; ok {
		return fp
	}
	fp := d.resolveVolcanoUncachedFingerprint(ctx, acct, accountKey, cacheKey, cloudCertID)
	cache[cacheKey] = fp
	return fp
}

// resolveVolcanoUncachedFingerprint 解析主干：映射反查 → GetCert 要素 → 占位
// 指纹。GetCert 走既有 getCertForProduct 路由（csv 链解析 / cdn 原生 sha256
// / waf-alb-nlb 无指纹通道——非对齐口径留空落占位，对齐华为 SCM SHA-1 口径
// 语义）。
func (d *VolcanoDeployer) resolveVolcanoUncachedFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, accountKey, cacheKey, cloudCertID string,
) string {
	if d.mappings != nil {
		if m, err := d.mappings.FindByCloudCertID(ctx, string(volcanoCloud), accountKey, cloudCertID); err == nil {
			return m.CertFingerprint
		}
		// 无命中/仓储异常不中断发现（同 3.5 口径），走 GetCert fallback
	}
	var info CloudCertInfo
	if err := d.withRetry(ctx, func(int) error {
		var e error
		product, rawID, ok := splitVolcanoCloudCertID(cloudCertID)
		if !ok {
			return fmt.Errorf("%w: %q", errVolcanoCloudCertIDNotNormalized, cloudCertID)
		}
		info, e = d.getCertForProduct(ctx, acct, product, rawID)
		return e
	}); err == nil && info.Exists && certFingerprint64Pattern.MatchString(info.Fingerprint) {
		return info.Fingerprint
	}
	// 确定性占位指纹（与 3.5 service.resolveUncached 同公式，两路径结果可对账）。
	return unresolvedPlaceholderFingerprint(cacheKey)
}

// refPageSizeOr 获取引用枚举分页大小（零值回退默认）。
func (d *VolcanoDeployer) refPageSizeOr() int64 {
	if d.refPageSize <= 0 {
		return volcanoRefPageSizeDefault
	}
	return d.refPageSize
}

// volcanoSplitLBResourceID 监听复合资源 ID 解析："{lbId}/{listenerId}" →
// (lbId, listenerId)；纯监听形态（无 "/"，旧快照/存量变更单升级窗口互认形态）
// lbId 返回空（定点定位仅按 ListenerIds）；空串整体非法由调用方拒绝。
func volcanoSplitLBResourceID(resourceID string) (lbID, listenerID string) {
	idx := strings.LastIndexByte(resourceID, '/')
	if idx < 0 {
		return "", strings.TrimSpace(resourceID)
	}
	return strings.TrimSpace(resourceID[:idx]), strings.TrimSpace(resourceID[idx+1:])
}

// volcanoLBResourceID 构造负载均衡监听复合资源 ID "{lbId}/{listenerId}"
// （对齐 aliyun lbScopedResourceID / 华为 ELB 先例：实例 ID 供控制台对账，
// 监听 ID 供绑定定位）。lbID 为空（云侧响应异常缺字段）时回退纯监听形态，
// 不产生 "/lsn-*" 脏值（与扫描适配器 volcanoLBResourceID 同语义同形态）。
func volcanoLBResourceID(lbID, listenerID string) string {
	if lbID == "" {
		return listenerID
	}
	return lbID + "/" + listenerID
}

// volcanoRegions 账号地域清单（WAF/ALB/NLB 为地域级产品按地域遍历绑定定位与
// 引用枚举；缺省回退默认地域——对齐扫描适配器 volcanoScanRegions 口径）。
func volcanoRegions(acct *sharedomain.CloudAccount) []string {
	if acct != nil && len(acct.Regions) > 0 {
		return acct.Regions
	}
	return []string{"cn-beijing"}
}

// nullableVolcanoString 空串 → nil（定点定位的 LoadBalancerId 过滤仅复合形态
// 携带，纯监听形态不下发空过滤值）。
func nullableVolcanoString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return volcengine.String(s)
}

// GetCert 查询云侧证书在库状态（回滚目标有效性校验依据，只读）：按归一前缀
// 路由——csv 经链解析产出 SHA-256 对齐指纹与有效期；产品库经各自查询 API 产出
// 在库存在性/有效期/原生指纹（仅 64hex 对齐口径直读，否则留空=无法复核）。
// 云侧「已不存在」归一为 Exists=false 非错误（幂等口径，五云同语义）；其余
// 查询错误透传（回滚判定 fail-safe 阻断，不误判有效）。
func (d *VolcanoDeployer) GetCert(ctx context.Context, creds Credential, cloudCertID string) (CloudCertInfo, error) {
	acct, err := d.account(creds)
	if err != nil {
		return CloudCertInfo{}, err
	}
	product, rawID, ok := splitVolcanoCloudCertID(cloudCertID)
	if !ok {
		return CloudCertInfo{}, fmt.Errorf("%w: %q", errVolcanoCloudCertIDNotNormalized, cloudCertID)
	}
	var info CloudCertInfo
	err = d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.getCertForProduct(ctx, acct, product, rawID)
		return e
	})
	if err != nil {
		return CloudCertInfo{}, fmt.Errorf("volcano deployer: get cert: %w", err)
	}
	// 指纹通道缺失库（waf/alb/nlb 云侧不返回证书指纹）回退映射反查——对齐既有
	// 指纹解析链「映射反查 → 云侧要素」的云无关口径；csv/cdn 云侧指纹为权威
	// 不受影响。映射指纹为 e-cam 侧记录值：库内无指纹通道时这是回滚目标复核
	// 的唯一依据（fail-safe：映射亦无记录则指纹留空，上层三判定阻断转人工）。
	if info.Exists && info.Fingerprint == "" && d.mappings != nil {
		if m, merr := d.mappings.FindByCloudCertID(ctx, string(volcanoCloud), creds.AccountKey, cloudCertID); merr == nil {
			info.Fingerprint = m.CertFingerprint
		}
	}
	return info, nil
}

// getCertForProduct 按证书库前缀路由查询（单次云 API 调用）。
func (d *VolcanoDeployer) getCertForProduct(ctx context.Context, acct *sharedomain.CloudAccount, product, rawID string) (CloudCertInfo, error) {
	client, err := d.client(acct)
	if err != nil {
		return CloudCertInfo{}, err
	}
	switch product {
	case volcanoProductCSV:
		return d.getCertCSV(ctx, client, rawID)
	case volcanoProductCDN:
		return getCertCDN(ctx, client, rawID)
	case volcanoProductWAF:
		return getCertWAF(ctx, client, rawID)
	case volcanoProductALB, volcanoProductNLB:
		return getCertALB(ctx, client, rawID)
	default:
		return CloudCertInfo{}, fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
}

// getCertCSV certificateservice 统一证书库查询：CertificateGetInstance →
// 状态过滤（revoked/非 Issued = Exists=false，不可作回滚目标）→ 链解析
// （块级净化仅保留 CERTIFICATE 块；叶指纹 SHA256 对齐台账口径、有效期取叶
// NotAfter——对齐 cloudx/volcano 解析口径）。not-found 归一为 Exists=false。
func (d *VolcanoDeployer) getCertCSV(ctx context.Context, client volcanoCertLibraryAPI, instanceID string) (CloudCertInfo, error) {
	out, err := client.CertificateGetInstanceWithContext(ctx, &volcanosdkcsv.CertificateGetInstanceInput{
		InstanceId: volcengine.String(instanceID),
	})
	if err != nil {
		if isVolcanoCertNotFoundErr(err) {
			return CloudCertInfo{Exists: false}, nil
		}
		return CloudCertInfo{}, wrapVolcanoCertErr("certificate_get_instance", err)
	}
	if out == nil {
		return CloudCertInfo{}, fmt.Errorf("volcano certificate_get_instance: empty response (instance %s)", instanceID)
	}
	if volcengine.BoolValue(out.IsCertificateRevoked) || volcengine.StringValue(out.Status) != "Issued" {
		return CloudCertInfo{Exists: false}, nil
	}
	var rawChain []byte
	if out.CertificateDetail != nil {
		rawChain = concatVolcanoChainPEM(out.CertificateDetail.Chain)
	}
	chainPEM := cloudx.SanitizeCertChainPEM(rawChain)
	info := CloudCertInfo{Exists: true}
	if leaf, ok := parseVolcanoLeafPEM(chainPEM); ok {
		sum := sha256.Sum256(leaf.Raw)
		info.Fingerprint = hex.EncodeToString(sum[:])
		info.NotAfter = leaf.NotAfter
	}
	// 链缺失/不可解析：Exists=true 但指纹留空（上层按「指纹无法复核」fail-safe）。
	return info, nil
}

// getCertCDN CDN 证书库查询：ListCertInfo（Source 必填，按上传口径过滤）→
// CertId 命中 → 原生 Sha256 指纹（64hex 对齐口径直读）+ ExpireTime（unix 秒）。
func getCertCDN(ctx context.Context, client volcanoCertLibraryAPI, certID string) (CloudCertInfo, error) {
	out, err := client.ListCertInfoWithContext(ctx, &volcanosdkcdn.ListCertInfoInput{
		CertId: volcengine.String(certID),
		Source: volcengine.String(volcanoCDNListCertSource),
	})
	if err != nil {
		if isVolcanoCertNotFoundErr(err) {
			return CloudCertInfo{Exists: false}, nil
		}
		return CloudCertInfo{}, wrapVolcanoCertErr("cdn_list_cert_info", err)
	}
	if out == nil {
		return CloudCertInfo{}, fmt.Errorf("volcano cdn_list_cert_info: empty response (cert %s)", certID)
	}
	for _, item := range out.CertInfo {
		if item == nil || volcengine.StringValue(item.CertId) != certID {
			continue
		}
		info := CloudCertInfo{Exists: true}
		if item.ExpireTime != nil {
			info.NotAfter = time.Unix(*item.ExpireTime, 0)
		}
		if item.CertFingerprint != nil {
			fp := volcengine.StringValue(item.CertFingerprint.Sha256)
			if len(fp) == 64 {
				info.Fingerprint = fp
			}
		}
		return info, nil
	}
	return CloudCertInfo{Exists: false}, nil
}

// getCertWAF WAF 服务证书库查询：ListWafServiceCertificate 全量列表反查
// Id（列表无过滤参数）→ 有效期（ExpireTime 云侧字符串形态，尽力解析）。
func getCertWAF(ctx context.Context, client volcanoCertLibraryAPI, certID string) (CloudCertInfo, error) {
	out, err := client.ListWafServiceCertificateWithContext(ctx, &volcanosdkwaf.ListWafServiceCertificateInput{})
	if err != nil {
		if isVolcanoCertNotFoundErr(err) {
			return CloudCertInfo{Exists: false}, nil
		}
		return CloudCertInfo{}, wrapVolcanoCertErr("waf_list_service_certificate", err)
	}
	if out == nil {
		return CloudCertInfo{}, fmt.Errorf("volcano waf_list_service_certificate: empty response")
	}
	for _, item := range out.Data {
		if item == nil || item.Id == nil || strconv.FormatInt(int64(*item.Id), 10) != certID {
			continue
		}
		info := CloudCertInfo{Exists: true, NotAfter: parseVolcanoCloudTime(volcengine.StringValue(item.ExpireTime))}
		return info, nil
	}
	return CloudCertInfo{Exists: false}, nil
}

// getCertALB ALB 监听证书库（alb/nlb 共用）查询：DescribeCertificates 按
// CertificateIds 定点反查 → 有效期（ExpiredAt 云侧字符串形态，尽力解析）。
// ALB 库不返回证书指纹（存在性/有效期判定不受影响，指纹留空=无法复核）。
func getCertALB(ctx context.Context, client volcanoCertLibraryAPI, certID string) (CloudCertInfo, error) {
	out, err := client.DescribeCertificatesWithContext(ctx, &volcanosdkalb.DescribeCertificatesInput{
		CertificateIds: []*string{volcengine.String(certID)},
	})
	if err != nil {
		if isVolcanoCertNotFoundErr(err) {
			return CloudCertInfo{Exists: false}, nil
		}
		return CloudCertInfo{}, wrapVolcanoCertErr("alb_describe_certificates", err)
	}
	if out == nil {
		return CloudCertInfo{}, fmt.Errorf("volcano alb_describe_certificates: empty response (cert %s)", certID)
	}
	for _, item := range out.Certificates {
		if item == nil || volcengine.StringValue(item.CertificateId) != certID {
			continue
		}
		return CloudCertInfo{Exists: true, NotAfter: parseVolcanoCloudTime(volcengine.StringValue(item.ExpiredAt))}, nil
	}
	return CloudCertInfo{Exists: false}, nil
}

// CleanupOrphan 孤儿证书清理：按归一前缀路由到对应证书库删除 API；云侧
// 「已不存在」归一为成功（幂等——已删除证书双调用同结果，清理队列重放安全，
// 对齐 3.1/3.2 口径）。
func (d *VolcanoDeployer) CleanupOrphan(ctx context.Context, creds Credential, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.cleanupForProduct(ctx, acct, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("volcano deployer: cleanup orphan: %w", err)
	}
	return nil
}

// cleanupForProduct 按证书库前缀路由删除（单次云 API 调用；not-found 归一成功）。
func (d *VolcanoDeployer) cleanupForProduct(ctx context.Context, acct *sharedomain.CloudAccount, cloudCertID string) error {
	product, rawID, ok := splitVolcanoCloudCertID(cloudCertID)
	if !ok {
		return fmt.Errorf("%w: %q", errVolcanoCloudCertIDNotNormalized, cloudCertID)
	}
	client, err := d.client(acct)
	if err != nil {
		return err
	}
	var err2 error
	switch product {
	case volcanoProductCSV:
		_, err2 = client.CertificateDeleteInstanceWithContext(ctx, &volcanosdkcsv.CertificateDeleteInstanceInput{
			InstanceId: volcengine.String(rawID),
		})
	case volcanoProductCDN:
		_, err2 = client.DeleteCdnCertificateWithContext(ctx, &volcanosdkcdn.DeleteCdnCertificateInput{
			CertId: volcengine.String(rawID),
		})
	case volcanoProductWAF:
		_, err2 = client.DeleteWafServiceCertificateWithContext(ctx, &volcanosdkwaf.DeleteWafServiceCertificateInput{
			Id: volcengine.String(rawID),
		})
	case volcanoProductALB, volcanoProductNLB:
		_, err2 = client.DeleteCertificateWithContext(ctx, &volcanosdkalb.DeleteCertificateInput{
			CertificateId: volcengine.String(rawID),
		})
	default:
		return fmt.Errorf("%w: %q", ErrVolcanoProductNotSupported, product)
	}
	if err2 != nil && isVolcanoCertNotFoundErr(err2) {
		return nil // 已删除=幂等成功（清理队列重放安全）
	}
	if err2 != nil {
		return wrapVolcanoCertErr("delete_"+product+"_cert", err2)
	}
	return nil
}

// account Credential → 适配 *CloudAccount 转换（实现收敛于 cloudAccountFor
// 单点；Secret 明文经 string 副本供 SDK 构参，禁入日志/错误信息）。
func (d *VolcanoDeployer) account(creds Credential) (*sharedomain.CloudAccount, error) {
	return cloudAccountFor(creds, volcanoCloud, sharedomain.CloudProviderVolcano)
}

// ---------------------------------------------------------------------
// 云侧响应解析小件
// ---------------------------------------------------------------------

// isVolcanoCertNotFoundErr 云侧「证书/实例已不存在」保守启发式（各库错误文案
// 无统一错误码暴露，按消息关键词归一；实网复核后可收窄为错误码判定）。
// 判定宁缺毋滥：不命中关键词一律按真实错误处理（清理重试兜底，不误吞错误）。
func isVolcanoCertNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"not found", "notfound", "not exist", "nosuch", "no such",
		"404", "invalidinstanceid", "invalidcertificateid", "invalidcertid",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// wrapVolcanoCertErr 云 API 错误统一包装（对齐既有跨云 wrapCertCloudErr 归一
// 语义）：统一云/操作前缀 + %w 透传云侧错误明文（私钥材料不存在于本层任何
// 错误路径）。
func wrapVolcanoCertErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("volcano %s api error: %w", op, err)
}

// concatVolcanoChainPEM 拼接云侧 Chain（PEM 列表，叶在前口径由云侧返回顺序
// 保证）：块间补换行避免块尾与下一块头粘连导致 pem.Decode 漏块（对齐
// cloudx/volcano.concatCertChainPEM 口径）；空/nil 元素跳过。
func concatVolcanoChainPEM(chain []*string) []byte {
	var out []byte
	for _, item := range chain {
		if item == nil || strings.TrimSpace(*item) == "" {
			continue
		}
		out = append(out, []byte(strings.TrimSpace(*item))...)
		out = append(out, '\n')
	}
	return out
}

// parseVolcanoLeafPEM 解析 PEM 证书束首个 CERTIFICATE 块为 leaf（无有效块 →
// false；对齐 cloudx/volcano.parseCertLeafPEM 口径）。
func parseVolcanoLeafPEM(pemStr string) (*x509.Certificate, bool) {
	if pemStr == "" {
		return nil, false
	}
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, false
	}
	return leaf, true
}

// parseVolcanoCloudTime 云侧字符串时间尽力解析（RFC3339 优先，回退火山常用
// "2006-01-02 15:04:05" 形态）；解析失败返回零值（上层按「有效期未知」处理）。
func parseVolcanoCloudTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
