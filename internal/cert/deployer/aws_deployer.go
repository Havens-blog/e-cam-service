// aws_deployer.go AWS CloudDeployer 实现（cert-multicloud-deployers 任务 2）。
//
// 分层定位：将 cloudx AWS 完整证书五方法适配（CertAdapter，SDK 单次调用封装）
// 组装为 deployer 层 CloudDeployer 端口实例，经 5.3 CloudAPIChannel 两段式编排
// 注入（UploadCert→BindResource，第二段失败 CleanupOrphan 补偿）。与 5.4 阿里云/
// 5.5 腾讯云/任务 1 华为云部署器结构对称（统一 CloudAPIChannel 消费口径）。
//
// 本层职责边界（Hard Rule）：
//   - 只做 per 云端口适配 + 限流有界退避重试 + 上传名逐次唯一生成（C7）；
//   - 不做业务级状态机判断——项级 failed/rate_limited 状态落库、回滚语义
//     归 5.7/5.8 引擎与回滚服务；
//   - 退避重试有上限次数与总时长双闸，禁止无限重试。
//
// AWS 差异（相对 5.4/5.5/任务 1）：
//   - 证书库 = ACM（区域服务），云证书 ID = ACM 证书 ARN（自包含地域信息）；
//     CloudDeployer.UploadCert 端口无 product 入参（签名固定），第一段统一以
//     CDN 口径上传——CloudFront 证书 us-east-1 硬约束使每个上传产物均为
//     CloudFront 可绑形态（Credential 不携带账号 Regions，账号主地域经适配层
//     缺省亦为 us-east-1）；ALB/NLB 绑定由适配层校验证书 ARN 地域与监听器
//     ARN 地域一致（ACM 区域资源），跨地域目标显式失败不猜测；
//   - CloudFront（cdn）绑定 = 分发级 ViewerCertificate 更新（IfMatch 并发控制，
//     Aliases 属 CNAME 配置面不改动）；ALB/NLB 绑定 = 监听器级
//     AddListenerCertificates 追加（ALB HTTPS 与 NLB TLS 同 API 形态）；
//   - ListReferences 复用 3.3 发现适配引用形态；指纹解析口径同 3.5/5.4/5.5
//     （映射反查 → GetCert 要素〔仅 SHA256 对齐口径〕→ 确定性占位指纹）；
//     IAM 托管证书 ID（非 ARN 形态）经 GetCert 返回结构化降级标记 → 占位指纹。
package deployer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// awsCertAPI AWS 完整证书适配窄接口（*aws.CertAdapter 天然满足；测试注入
// fake）。签名即适配原签名——本层只消费，不修改适配层。
type awsCertAPI interface {
	UploadCert(ctx context.Context, creds *sharedomain.CloudAccount, product, name, certPEM, keyPEM string) (string, error)
	BindResource(ctx context.Context, creds *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error
	ListReferences(ctx context.Context, creds *sharedomain.CloudAccount, product string) ([]aws.CloudCertRef, error)
	GetCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (aws.CloudCertInfo, error)
	CleanupOrphan(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) error
}

// AwsDeployer AWS CloudDeployer：覆盖 cdn/alb/nlb 三产品，按 DeployTarget.product
// 路由到适配对应方法（适配层内部按产品分发）。
//
// resourceId 粒度约定（3.3 发现适配消费口径，本层原样透传）：
//   - cdn = CloudFront 分发 ID；
//   - alb/nlb = ELBv2 监听器 ARN（自包含地域定位）。
type AwsDeployer struct {
	adapter  awsCertAPI
	mappings domain.CloudCertMappingRepository // 可空：ListReferences 指纹映射反查
	retry    RetryPolicy
	now      func() time.Time                                 // 上传名时间源（测试可注入）
	randHex  func(n int) string                               // 上传名随机后缀（测试可注入）
	sleep    func(ctx context.Context, d time.Duration) error // 退避睡眠（测试可注入）
}

// 编译期断言：满足 CloudDeployer 端口（供 5.3 CloudAPIChannel 注入）。
var _ CloudDeployer = (*AwsDeployer)(nil)

// NewAwsDeployer 创建 AWS 部署器。adapter 生产实现为 aws.NewCertAdapter 产物；
// mappings 允许 nil（ListReferences 跳过映射反查，直接 GetCert fallback）。
func NewAwsDeployer(adapter awsCertAPI, mappings domain.CloudCertMappingRepository, opts ...AwsOption) *AwsDeployer {
	d := &AwsDeployer{
		adapter:  adapter,
		mappings: mappings,
		retry:    DefaultRetryPolicy(),
		now:      time.Now,
		randHex:  randomHexSuffix,
		sleep:    sleepWithContext,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Stop 透传导配层限流器停止（进程退出时避免令牌协程泄漏；无 Stop 实现时 no-op）。
func (d *AwsDeployer) Stop() {
	if s, ok := d.adapter.(interface{ Stop() }); ok {
		s.Stop()
	}
}

// AwsOption AwsDeployer 装配选项。
type AwsOption func(*AwsDeployer)

// WithAwsRetryPolicy 覆盖限流退避参数（应用 config 读取路径；零值/非法
// 配置经 normalized 回退缺省保守值，重试安全侧）。
func WithAwsRetryPolicy(p RetryPolicy) AwsOption {
	return func(d *AwsDeployer) { d.retry = p.normalized() }
}

// withRetry 有界重试主干（实现收敛于 boundedRetry 单点，与 5.4/5.5/任务 1 同口径）：
//   - ErrCloudRateLimited → 按固定序列退避后重试（计入总时长上限）；
//   - 上传名冲突（保守启发式，B2 同口径）→ 不退避立即换名重试（仅 UploadCert
//     语境出现；ACM 无证书名唯一约束、名称仅经 Name 标签承载，该分支常态休眠）；
//   - 次数或总时长耗尽 → 包装末次错误返回（哨兵语义经 %w 保留，供 5.7/5.8 判定）。
//
// AWS SDK 自带重试（smithy Retryer）与本层退避分层：SDK 侧默认重试吸收瞬时
// 网络抖动，本层仅对限流哨兵（适配层 wrapCertCloudErr 映射）做有界退避。
//
// 业务级成败状态归 5.7 引擎（Hard Rule：本层无状态机判断）。
func (d *AwsDeployer) withRetry(ctx context.Context, fn func(attempt int) error) error {
	return boundedRetry(ctx, d.retry, d.sleep, fn)
}

// ---------------------------------------------------------------------
// C7：上传名唯一生成（与 5.4/5.5/任务 1 同公式）
// ---------------------------------------------------------------------

const (
	// awsUploadNamePrefix 上传名前缀（对齐 5.4/5.5/任务 1 ecam 口径，云侧可辨识平台来源）。
	awsUploadNamePrefix = "ecam"
	// awsUploadNameMaxLen 上传名长度上限（ACM 无证书名字段，名称经 Name 标签
	// 承载——标签值上限 256，沿用 63 字符对齐三云口径）。
	awsUploadNameMaxLen = 63
	// awsUploadProduct 两段式第一段统一以 CDN 口径上传：CloudFront 证书 us-east-1
	// 硬约束使每个上传产物均为 CloudFront 可绑形态；ALB/NLB 绑定由适配层校验
	// 证书 ARN 与监听器地域一致（ACM 区域资源），跨地域目标显式失败不猜测
	// （proposal 失败模式口径）。
	awsUploadProduct = aws.CertProductCDN
)

// generateUploadName 生成上传名 ecam-{指纹前8}-{unix秒}-{随机后缀}（C7 公式
// 实现收敛于 formatUploadName 单点）：指纹前缀与 5.4/5.5/任务 1 共用口径；unix
// 秒 + 随机后缀保证逐次唯一（ACM ImportCertificate 每次导入产生全新 ARN，
// 唯一性亦由云侧构造保证）。
func (d *AwsDeployer) generateUploadName(certPEM string) string {
	return formatUploadName(awsUploadNamePrefix, awsUploadNameMaxLen, certPEM, d.now, d.randHex)
}

// ---------------------------------------------------------------------
// CloudDeployer 五方法
// ---------------------------------------------------------------------

// UploadCert 两段式第一段：生成唯一名（C7）导入 ACM 证书库，返回云证书 ID
// （ACM 证书 ARN，三产品绑定共用）。每次尝试（含重试）生成全新名称——重试
// 不得复用可能已成功的名称（C7：重试即新副本，孤儿清理兜底）。
// keyPEM 明文仅内存传递，经 string 副本供适配层 SDK 构参（适配层副本用后
// 归零），不落日志。
func (d *AwsDeployer) UploadCert(ctx context.Context, creds Credential, certPEM string, keyPEM []byte) (string, error) {
	acct, err := d.account(creds)
	if err != nil {
		return "", err
	}
	if certPEM == "" || len(keyPEM) == 0 {
		return "", errors.New("aws deployer: upload cert requires cert PEM and key PEM")
	}
	var cloudCertID string
	err = d.withRetry(ctx, func(int) error {
		name := d.generateUploadName(certPEM)
		id, err := d.adapter.UploadCert(ctx, acct, awsUploadProduct, name, certPEM, string(keyPEM))
		if err != nil {
			return err
		}
		cloudCertID = id
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("aws deployer: upload cert: %w", err)
	}
	return cloudCertID, nil
}

// BindResource 两段式第二段：按 product 路由到适配绑定方法（cdn 分发级
// ViewerCertificate 更新，alb/nlb 监听器级 AddListenerCertificates 追加）。
// ELB 监听证书 ID 形态归一化（normalizeAwsListenerCertID）：AWS 引用与上传
// 产物均为自包含 ARN 形态，归一化为幂等透传（仅去空白；发现引用/回滚旧 ID
// 不被改写）。
func (d *AwsDeployer) BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	bindCertID := normalizeAwsListenerCertID(product, cloudCertID)
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.BindResource(ctx, acct, product, resourceID, bindCertID)
	})
	if err != nil {
		return fmt.Errorf("aws deployer: bind %s resource %s: %w", product, resourceID, err)
	}
	return nil
}

// normalizeAwsListenerCertID ELB（alb/nlb）监听证书 ID 形态归一化。
// 参照 5.4 normalizeAliyunListenerCertID 的归一化接缝：AWS 上传产物即 ACM ARN
// （自包含地域），与 ELB 监听引用（监听器 ARN 自包含定位）同形态，无需地域
// 后缀拼接——归一化为幂等透传（仅去首尾空白；含发现引用/回滚旧 ID 的原样
// 保持），形态差异若后续实网复核出现（如监听器证书 {arn}-{序号} 复合形态）
// 在此单点扩展。
func normalizeAwsListenerCertID(product, cloudCertID string) string {
	if product != aws.CertProductALB && product != aws.CertProductNLB {
		return cloudCertID
	}
	return strings.TrimSpace(cloudCertID)
}

// ListReferences 只读发现：复用 3.3 发现适配引用形态（CDN 分发查看器证书/
// ALB/NLB 监听器证书，监听器 ARN 自包含定位），指纹解析口径同 3.5/5.4/5.5
// （映射反查 → GetCert 要素〔仅接受 SHA256 对齐口径〕→ 确定性占位指纹，
// 占位公式与 3.5 扫描路径一致可对账）；同云证书多引用去重查询。
func (d *AwsDeployer) ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error) {
	acct, err := d.account(creds)
	if err != nil {
		return nil, err
	}
	var found []aws.CloudCertRef
	err = d.withRetry(ctx, func(int) error {
		var e error
		found, e = d.adapter.ListReferences(ctx, acct, product)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("aws deployer: list %s references: %w", product, err)
	}
	out := make([]domain.CertReference, 0, len(found))
	cache := make(map[string]string, len(found))
	for _, r := range found {
		out = append(out, domain.CertReference{
			CertFingerprint:       d.resolveFingerprint(ctx, acct, r, cache),
			Cloud:                 domain.CloudAWS,
			Product:               domain.Product(r.Product),
			ResourceID:            r.ResourceID,
			ReferencedCloudCertID: r.ReferencedCloudCertID,
			AccountKey:            r.AccountKey,
		})
	}
	return out, nil
}

// resolveFingerprint 引用指纹解析（逐次发现去重缓存）。
func (d *AwsDeployer) resolveFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, r aws.CloudCertRef, cache map[string]string,
) string {
	cacheKey := strings.Join([]string{string(domain.CloudAWS), r.AccountKey, r.ReferencedCloudCertID}, "|")
	if fp, ok := cache[cacheKey]; ok {
		return fp
	}
	fp := d.resolveUncachedFingerprint(ctx, acct, cacheKey, r)
	cache[cacheKey] = fp
	return fp
}

// resolveUncachedFingerprint 解析主干：映射反查 → GetCert fallback → 占位指纹。
// IAM 托管证书 ID（非 ARN 形态）经 GetCert 返回 ErrCertPEMUnsupported 结构化
// 降级标记 → 占位指纹（不中断发现）。
func (d *AwsDeployer) resolveUncachedFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, cacheKey string, r aws.CloudCertRef,
) string {
	if d.mappings != nil {
		if m, err := d.mappings.FindByCloudCertID(ctx, string(domain.CloudAWS), r.AccountKey, r.ReferencedCloudCertID); err == nil {
			return m.CertFingerprint
		}
		// 无命中/仓储异常不中断发现（同 3.5 口径），走 GetCert fallback
	}
	var info aws.CloudCertInfo
	if err := d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, r.ReferencedCloudCertID)
		return e
	}); err == nil && info.Exists && certFingerprint64Pattern.MatchString(info.Fingerprint) {
		return info.Fingerprint
	}
	// 确定性占位指纹（与 3.5 service.resolveUncached 同公式，两路径结果可对账）。
	return unresolvedPlaceholderFingerprint(cacheKey)
}

// GetCert 查询云侧证书在库状态（回滚目标有效性校验依据，只读；适配层已将
// 证书不存在归一为 Exists=false 非错误，并经 GetCertificate 材料产出 SHA-256
// 对齐指纹；IAM 托管形态返回 ErrCertPEMUnsupported 结构化降级标记——5.8 回滚
// 三判定按错误 fail-safe 阻断，不误判有效）。
func (d *AwsDeployer) GetCert(ctx context.Context, creds Credential, cloudCertID string) (CloudCertInfo, error) {
	acct, err := d.account(creds)
	if err != nil {
		return CloudCertInfo{}, err
	}
	var info aws.CloudCertInfo
	err = d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, cloudCertID)
		return e
	})
	if err != nil {
		return CloudCertInfo{}, fmt.Errorf("aws deployer: get cert: %w", err)
	}
	return CloudCertInfo{
		Exists:      info.Exists,
		NotAfter:    info.NotAfter,
		Fingerprint: info.Fingerprint,
	}, nil
}

// CleanupOrphan 孤儿证书清理（适配层对已删除证书幂等成功——已不存在归一为
// 成功，清理队列重放安全；AWS 托管证书经 DescribeCertificate.Type 显式识别
// 拒绝删除）。
func (d *AwsDeployer) CleanupOrphan(ctx context.Context, creds Credential, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.CleanupOrphan(ctx, acct, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("aws deployer: cleanup orphan: %w", err)
	}
	return nil
}

// account Credential → 适配 *CloudAccount 转换（实现收敛于 cloudAccountFor
// 单点；Secret 明文经 string 副本供 SDK 构参，禁入日志/错误信息。Credential
// 不携带账号 Regions，适配层地域缺省 us-east-1——与任务 4 装配收敛时的扫描侧
// 账号源 Regions 口径分离，为后续产品感知上传留缝）。
func (d *AwsDeployer) account(creds Credential) (*sharedomain.CloudAccount, error) {
	return cloudAccountFor(creds, domain.CloudAWS, sharedomain.CloudProviderAWS)
}
