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
)

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

// ---------------------------------------------------------------------
// SDK 窄接口与客户端工厂
// ---------------------------------------------------------------------

// volcanoCertLibraryAPI 火山四证书库 SDK 窄接口（生产实现为 volcanoSDKClients；
// 测试注入 fake）。签名即适配原签名——本层只消费 SDK，不改适配面。
// 只含证书库层必需方法：上传 ×4 库、查询 ×4 库、删除 ×4 库。
type volcanoCertLibraryAPI interface {
	// certificateservice 统一证书库（csv）
	ImportCertificateWithContext(ctx context.Context, input *volcanosdkcsv.ImportCertificateInput, opts ...request.Option) (*volcanosdkcsv.ImportCertificateOutput, error)
	CertificateGetInstanceWithContext(ctx context.Context, input *volcanosdkcsv.CertificateGetInstanceInput, opts ...request.Option) (*volcanosdkcsv.CertificateGetInstanceOutput, error)
	CertificateDeleteInstanceWithContext(ctx context.Context, input *volcanosdkcsv.CertificateDeleteInstanceInput, opts ...request.Option) (*volcanosdkcsv.CertificateDeleteInstanceOutput, error)
	// CDN 证书库
	AddCertificateWithContext(ctx context.Context, input *volcanosdkcdn.AddCertificateInput, opts ...request.Option) (*volcanosdkcdn.AddCertificateOutput, error)
	ListCertInfoWithContext(ctx context.Context, input *volcanosdkcdn.ListCertInfoInput, opts ...request.Option) (*volcanosdkcdn.ListCertInfoOutput, error)
	DeleteCdnCertificateWithContext(ctx context.Context, input *volcanosdkcdn.DeleteCdnCertificateInput, opts ...request.Option) (*volcanosdkcdn.DeleteCdnCertificateOutput, error)
	// WAF 服务证书库
	UploadWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.UploadWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.UploadWafServiceCertificateOutput, error)
	ListWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.ListWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.ListWafServiceCertificateOutput, error)
	DeleteWafServiceCertificateWithContext(ctx context.Context, input *volcanosdkwaf.DeleteWafServiceCertificateInput, opts ...request.Option) (*volcanosdkwaf.DeleteWafServiceCertificateOutput, error)
	// ALB 监听证书库（alb/nlb 共用）
	UploadCertificateWithContext(ctx context.Context, input *volcanosdkalb.UploadCertificateInput, opts ...request.Option) (*volcanosdkalb.UploadCertificateOutput, error)
	DescribeCertificatesWithContext(ctx context.Context, input *volcanosdkalb.DescribeCertificatesInput, opts ...request.Option) (*volcanosdkalb.DescribeCertificatesOutput, error)
	DeleteCertificateWithContext(ctx context.Context, input *volcanosdkalb.DeleteCertificateInput, opts ...request.Option) (*volcanosdkalb.DeleteCertificateOutput, error)
}

// volcanoSDKClients 火山四证书库 SDK 客户端束（生产实现，volcanoCertLibraryAPI）。
// 显式转发而非四客户端嵌入聚合——各服务方法名空间交叠（如 TagResources*），
// 嵌入提升选择器有歧义风险，显式转发构造性保证窄接口面。
type volcanoSDKClients struct {
	csv *volcanosdkcsv.CERTIFICATESERVICE
	cdn *volcanosdkcdn.CDN
	waf *volcanosdkwaf.WAF
	alb *volcanosdkalb.ALB
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

// newVolcanoSDKClients 生产客户端工厂：四服务共用一次会话装配（静态 AK/SK，
// 账号默认地域仅用于客户端签名装配——证书库为全局/账号级服务）。
func newVolcanoSDKClients(creds *sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) {
	config := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(creds.AccessKeyID, creds.AccessKeySecret, "")).
		WithRegion(volcanoCredsRegion(creds))
	sess, err := session.NewSession(config)
	if err != nil {
		return nil, fmt.Errorf("创建火山证书库会话失败: %w", err)
	}
	return &volcanoSDKClients{
		csv: volcanosdkcsv.New(sess),
		cdn: volcanosdkcdn.New(sess),
		waf: volcanosdkwaf.New(sess),
		alb: volcanosdkalb.New(sess),
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
// GetCert/CleanupOrphan）；BindResource/ListReferences 由任务 2 追加（本文件
// 内显式未装配桩，见编译期断言）。
type VolcanoDeployer struct {
	mappings domain.CloudCertMappingRepository // 可空：任务 2 ListReferences 指纹映射反查
	retry    RetryPolicy
	now      func() time.Time                                 // 上传名时间源（测试可注入）
	randHex  func(n int) string                               // 上传名随机后缀（测试可注入）
	sleep    func(ctx context.Context, d time.Duration) error // 退避睡眠（测试可注入）

	newClients func(creds *sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) // 客户端工厂（测试注入 fake）
}

// 编译期断言：满足 CloudDeployer 端口（供 5.3 CloudAPIChannel 注入；任务 2
// 追加绑定层后断言持续生效）。
var _ CloudDeployer = (*VolcanoDeployer)(nil)

// NewVolcanoDeployer 创建火山引擎部署器。mappings 允许 nil（任务 2 ListReferences
// 跳过映射反查）。客户端工厂缺省为 newVolcanoSDKClients（测试经选项或直接
// 字段注入 fake）。
func NewVolcanoDeployer(mappings domain.CloudCertMappingRepository, opts ...VolcanoOption) *VolcanoDeployer {
	d := &VolcanoDeployer{
		mappings:   mappings,
		retry:      DefaultRetryPolicy(),
		now:        time.Now,
		randHex:    randomHexSuffix,
		sleep:      sleepWithContext,
		newClients: newVolcanoSDKClients,
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
		id, uerr := d.uploadForProduct(ctx, acct, volcanoUploadProduct, name, certPEM, string(keyCopy))
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

// BindResource 两段式第二段（任务 2 落地：CDN BatchDeployCert / WAF 域名证书
// 替换 / ALB-NLB 监听证书更新）。本任务显式未装配桩——编译期断言要求端口
// 完整，先以哨兵占位，任务 2 以同签名真实实现替换。
func (d *VolcanoDeployer) BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error {
	_ = ctx
	return fmt.Errorf("volcano deployer: BindResource not wired yet (task 2 bind layer); product=%q", product)
}

// ListReferences 只读发现（任务 2 落地：四产品资源 → CertReference 指纹解析）。
// 本任务显式未装配桩（同 BindResource 口径）。
func (d *VolcanoDeployer) ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error) {
	_ = ctx
	return nil, fmt.Errorf("volcano deployer: ListReferences not wired yet (task 2 bind layer); product=%q", product)
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
