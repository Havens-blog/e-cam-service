// azure_deployer.go Azure CloudDeployer 实现（cert-multicloud-deployers 任务 3）。
//
// 分层定位：将 cloudx Azure 完整证书五方法适配（CertAdapter，REST 单次调用封装）
// 组装为 deployer 层 CloudDeployer 端口实例，经 5.3 CloudAPIChannel 两段式编排
// 注入（UploadCert→BindResource，第二段失败 CleanupOrphan 补偿）。与 5.4 阿里云/
// 5.5 腾讯云/任务 1 华为云/任务 2 AWS 部署器结构对称（统一 CloudAPIChannel
// 消费口径）。
//
// 本层职责边界（Hard Rule）：
//   - 只做 per 云端口适配 + 限流有界退避重试 + 上传名逐次唯一生成（C7）；
//   - 不做业务级状态机判断——项级 failed/rate_limited 状态落库、回滚语义
//     归 5.7/5.8 引擎与回滚服务；
//   - 退避重试有上限次数与总时长双闸，禁止无限重试。
//
// Azure 差异（相对 5.4/5.5/任务 1/任务 2）：
//   - 证书库 = Key Vault（账号级，无地域/产品语义），云证书 ID = KV secret ID
//     （https://{vault}.vault.azure.net/secrets/{name}/{version}）；上传无 AWS
//     CloudFront 式地域硬约束，第一段统一以 CDN 口径满足适配层产品校验（端口
//     形状与四云一致，无路由语义）；
//   - 绑定经 KV 证书引用而非直接上传 ID：CDN(Front Door) 自定义域名 HTTPS 配置
//     引用 KV secret、ALB(App Gateway) 监听器 SSL 证书资源改指 KV secret——
//     引用形态归一化收敛在适配层单点（Hard Rule），本层幂等透传；
//   - 限流语义：Azure KV 数据面/ARM 管理面均为请求计数限流（HTTP 429），适配层
//     归一映射 ErrCloudRateLimited 哨兵，本层按固定序列有界退避（与其他云同口径）；
//   - ListReferences 复用 3.3 发现适配引用形态；指纹解析口径同 3.5/5.4/5.5
//     （映射反查 → GetCert 要素〔仅 SHA256 对齐口径〕→ 确定性占位指纹）。
package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/azure"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// azureCertAPI Azure 完整证书适配窄接口（*azure.CertAdapter 天然满足；测试注入
// fake）。签名即适配原签名——本层只消费，不修改适配层。
type azureCertAPI interface {
	UploadCert(ctx context.Context, creds *sharedomain.CloudAccount, product, name, certPEM, keyPEM string) (string, error)
	BindResource(ctx context.Context, creds *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error
	ListReferences(ctx context.Context, creds *sharedomain.CloudAccount, product string) ([]azure.CloudCertRef, error)
	GetCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (azure.CloudCertInfo, error)
	CleanupOrphan(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) error
}

// AzureDeployer Azure CloudDeployer：覆盖 cdn/alb 两产品，按 DeployTarget.product
// 路由到适配对应方法（适配层内部按产品分发）。
//
// resourceId 粒度约定（3.3 发现适配消费口径，本层原样透传）：
//   - cdn = "{FrontDoor}/{FrontendEndpoint}" 复合形态；
//   - alb = "{ApplicationGateway}/{Listener}" 复合形态
//     （完整 ARM 资源 ID 由适配层经订阅级清单按名反查）。
type AzureDeployer struct {
	adapter  azureCertAPI
	mappings domain.CloudCertMappingRepository // 可空：ListReferences 指纹映射反查
	retry    RetryPolicy
	now      func() time.Time                                 // 上传名时间源（测试可注入）
	randHex  func(n int) string                               // 上传名随机后缀（测试可注入）
	sleep    func(ctx context.Context, d time.Duration) error // 退避睡眠（测试可注入）
}

// 编译期断言：满足 CloudDeployer 端口（供 5.3 CloudAPIChannel 注入）。
var _ CloudDeployer = (*AzureDeployer)(nil)

// NewAzureDeployer 创建 Azure 部署器。adapter 生产实现为 azure.NewCertAdapter
// 产物；mappings 允许 nil（ListReferences 跳过映射反查，直接 GetCert fallback）。
func NewAzureDeployer(adapter azureCertAPI, mappings domain.CloudCertMappingRepository, opts ...AzureOption) *AzureDeployer {
	d := &AzureDeployer{
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
func (d *AzureDeployer) Stop() {
	if s, ok := d.adapter.(interface{ Stop() }); ok {
		s.Stop()
	}
}

// AzureOption AzureDeployer 装配选项。
type AzureOption func(*AzureDeployer)

// WithAzureRetryPolicy 覆盖限流退避参数（应用 config 读取路径；零值/非法
// 配置经 normalized 回退缺省保守值，重试安全侧）。
func WithAzureRetryPolicy(p RetryPolicy) AzureOption {
	return func(d *AzureDeployer) { d.retry = p.normalized() }
}

// withRetry 有界重试主干（RetryPolicy/固定退避序列与 5.4/5.5/任务 1/任务 2 共用）：
//   - ErrCloudRateLimited → 按固定序列退避后重试（计入总时长上限）；
//   - 上传名冲突（保守启发式，B2 同口径）→ 不退避立即换名重试（仅 UploadCert
//     语境出现；KV 同名导入产生新版本而非冲突、删除按名删除全体版本，唯一名
//     由 C7 生成公式保证，该分支常态休眠）；
//   - 其余错误立即返回；
//   - 次数或总时长耗尽 → 包装末次错误返回（哨兵语义经 %w 保留，供 5.7 映射
//     rate_limited 状态与 5.8 rollbackErrCode 判定）。
//
// Azure REST 客户端无 SDK 内建重试层：适配层限流器（20 QPS 请求计数）+ 本层
// 退避即全部重试面，退避上限双闸保证有界（Hard Rule）。
//
// 业务级成败状态归 5.7 引擎（Hard Rule：本层无状态机判断）。
func (d *AzureDeployer) withRetry(ctx context.Context, fn func(attempt int) error) error {
	policy := d.retry
	waited := time.Duration(0)
	attempts := 0
	for {
		attempts++
		err := fn(attempts)
		if err == nil {
			return nil
		}
		rateLimited := errors.Is(err, cloudx.ErrCloudRateLimited)
		nameConflict := !rateLimited && isCertNameConflictErr(err)
		if !rateLimited && !nameConflict {
			return err
		}
		if attempts >= policy.MaxAttempts {
			return fmt.Errorf("retries exhausted after %d attempts (total backoff %s): %w", attempts, waited, err)
		}
		if rateLimited {
			idx := attempts - 1
			if idx >= len(policy.Backoffs) {
				idx = len(policy.Backoffs) - 1
			}
			backoff := policy.Backoffs[idx]
			if waited+backoff > policy.MaxTotalWait {
				return fmt.Errorf("retries exhausted by total backoff cap %s after %d attempts: %w", policy.MaxTotalWait, attempts, err)
			}
			waited += backoff
			if serr := d.sleep(ctx, backoff); serr != nil {
				return fmt.Errorf("backoff interrupted: %w", serr)
			}
		}
	}
}

// ---------------------------------------------------------------------
// C7：上传名唯一生成（与 5.4/5.5/任务 1/任务 2 同公式）
// ---------------------------------------------------------------------

const (
	// azureUploadNamePrefix 上传名前缀（对齐 5.4/5.5/任务 1/任务 2 ecam 口径，
	// 云侧可辨识平台来源）。
	azureUploadNamePrefix = "ecam"
	// azureUploadNameMaxLen 上传名长度上限（KV 证书名上限 127，沿用 63 字符
	// 对齐三云口径；KV 命名约束 [a-zA-Z0-9-] 与公式字符集兼容，适配层校验）。
	azureUploadNameMaxLen = 63
	// azureUploadProduct 两段式第一段统一以 CDN 口径上传：KV 为账号级证书库、
	// 无地域/产品语义（AWS CloudFront us-east-1 式硬约束不存在），口径选择纯为
	// 端口形状与四云一致——上传产物为 KV secret ID，两产品绑定共用。
	azureUploadProduct = azure.CertProductCDN
)

// generateUploadName 生成上传名 ecam-{指纹前8}-{unix秒}-{随机后缀}：
//   - 指纹前缀与 5.4/5.5/任务 1/任务 2 共用口径（证书叶 DER SHA256 前 8 hex，
//     解析失败回退整段 PEM SHA256）；
//   - unix 秒 + 随机后缀保证逐次唯一（C7：重试不复用可能已成功的名称——KV
//     同名导入产生新版本，而删除按证书名删除全体版本，名称复用会使孤儿清理
//     误删其他上传副本）；
//   - 防御性截断至 63 字符。
func (d *AzureDeployer) generateUploadName(certPEM string) string {
	name := fmt.Sprintf("%s-%s-%d-%s",
		azureUploadNamePrefix, certNameFingerprintPrefix(certPEM), d.now().Unix(), d.randHex(4))
	if len(name) > azureUploadNameMaxLen {
		name = name[:azureUploadNameMaxLen]
	}
	return name
}

// ---------------------------------------------------------------------
// CloudDeployer 五方法
// ---------------------------------------------------------------------

// UploadCert 两段式第一段：生成唯一名（C7）导入 Key Vault 证书库，返回云证书
// ID（KV secret ID 含版本形态，两产品绑定共用）。每次尝试（含重试）生成全新
// 名称——重试不得复用可能已成功的名称（C7：重试即新副本，孤儿清理兜底）。
// keyPEM 明文仅内存传递，经 string 副本供适配层 SDK 构参（适配层副本用后
// 归零），不落日志。
func (d *AzureDeployer) UploadCert(ctx context.Context, creds Credential, certPEM string, keyPEM []byte) (string, error) {
	acct, err := d.account(creds)
	if err != nil {
		return "", err
	}
	if certPEM == "" || len(keyPEM) == 0 {
		return "", errors.New("azure deployer: upload cert requires cert PEM and key PEM")
	}
	var cloudCertID string
	err = d.withRetry(ctx, func(int) error {
		name := d.generateUploadName(certPEM)
		id, err := d.adapter.UploadCert(ctx, acct, azureUploadProduct, name, certPEM, string(keyPEM))
		if err != nil {
			return err
		}
		cloudCertID = id
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("azure deployer: upload cert: %w", err)
	}
	return cloudCertID, nil
}

// BindResource 两段式第二段：按 product 路由到适配绑定方法（cdn = Front Door
// 自定义域名 HTTPS 配置，alb = App Gateway 监听器 SSL 证书 KV 引用）。Azure
// 证书引用统一 KV secret ID 形态且归一化收敛在适配层单点（Hard Rule）——本层
// 幂等透传（发现引用/回滚旧 ID 不被改写）。
func (d *AzureDeployer) BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.BindResource(ctx, acct, product, resourceID, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("azure deployer: bind %s resource %s: %w", product, resourceID, err)
	}
	return nil
}

// ListReferences 只读发现：复用 3.3 发现适配引用形态（Front Door 前端终结点
// 自定义 HTTPS 证书/App Gateway 监听器 KV 引用证书，复合资源 ID 自包含定位），
// 指纹解析口径同 3.5/5.4/5.5（映射反查 → GetCert 要素〔仅接受 SHA256 对齐
// 口径〕→ 确定性占位指纹，占位公式与 3.5 扫描路径一致可对账）；同云证书多
// 引用去重查询。
func (d *AzureDeployer) ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error) {
	acct, err := d.account(creds)
	if err != nil {
		return nil, err
	}
	var found []azure.CloudCertRef
	err = d.withRetry(ctx, func(int) error {
		var e error
		found, e = d.adapter.ListReferences(ctx, acct, product)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("azure deployer: list %s references: %w", product, err)
	}
	out := make([]domain.CertReference, 0, len(found))
	cache := make(map[string]string, len(found))
	for _, r := range found {
		out = append(out, domain.CertReference{
			CertFingerprint:       d.resolveFingerprint(ctx, acct, r, cache),
			Cloud:                 domain.CloudAzure,
			Product:               domain.Product(r.Product),
			ResourceID:            r.ResourceID,
			ReferencedCloudCertID: r.ReferencedCloudCertID,
			AccountKey:            r.AccountKey,
		})
	}
	return out, nil
}

// azureFingerprintPattern 台账指纹对齐口径 ^[0-9a-f]{64}$（同 3.5/5.4/5.5；
// 非对齐口径一律视为无法复核）。
var azureFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// resolveFingerprint 引用指纹解析（逐次发现去重缓存）。
func (d *AzureDeployer) resolveFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, r azure.CloudCertRef, cache map[string]string,
) string {
	cacheKey := strings.Join([]string{string(domain.CloudAzure), r.AccountKey, r.ReferencedCloudCertID}, "|")
	if fp, ok := cache[cacheKey]; ok {
		return fp
	}
	fp := d.resolveUncachedFingerprint(ctx, acct, cacheKey, r)
	cache[cacheKey] = fp
	return fp
}

// resolveUncachedFingerprint 解析主干：映射反查 → GetCert fallback → 占位指纹。
func (d *AzureDeployer) resolveUncachedFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, cacheKey string, r azure.CloudCertRef,
) string {
	if d.mappings != nil {
		if m, err := d.mappings.FindByCloudCertID(ctx, string(domain.CloudAzure), r.AccountKey, r.ReferencedCloudCertID); err == nil {
			return m.CertFingerprint
		}
		// 无命中/仓储异常不中断发现（同 3.5 口径），走 GetCert fallback
	}
	var info azure.CloudCertInfo
	if err := d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, r.ReferencedCloudCertID)
		return e
	}); err == nil && info.Exists && azureFingerprintPattern.MatchString(info.Fingerprint) {
		return info.Fingerprint
	}
	// 确定性占位指纹（与 3.5 service.resolveUncached 同公式，两路径结果可对账）。
	sum := sha256.Sum256([]byte("certscan-unresolved:" + cacheKey))
	return hex.EncodeToString(sum[:])
}

// GetCert 查询云侧证书在库状态（回滚目标有效性校验依据，只读；适配层已将
// secret 不存在〔含 KV 软删除态〕归一为 Exists=false 非错误，并经 PEM 净化材料
// 产出 SHA-256 对齐指纹；非证书 secret 返回 Exists=true 且指纹为空——5.8 回滚
// 三判定按"指纹无法复核"口径处理，不误判有效）。
func (d *AzureDeployer) GetCert(ctx context.Context, creds Credential, cloudCertID string) (CloudCertInfo, error) {
	acct, err := d.account(creds)
	if err != nil {
		return CloudCertInfo{}, err
	}
	var info azure.CloudCertInfo
	err = d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, cloudCertID)
		return e
	})
	if err != nil {
		return CloudCertInfo{}, fmt.Errorf("azure deployer: get cert: %w", err)
	}
	return CloudCertInfo{
		Exists:      info.Exists,
		NotAfter:    info.NotAfter,
		Fingerprint: info.Fingerprint,
	}, nil
}

// CleanupOrphan 孤儿证书清理（适配层对已删除证书幂等成功——已不存在/软删除态
// 重复删除 404 归一为成功，清理队列重放安全；KV 删除进软删除态，purge 不可逆
// 破坏性操作安全侧不执行）。
func (d *AzureDeployer) CleanupOrphan(ctx context.Context, creds Credential, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.CleanupOrphan(ctx, acct, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("azure deployer: cleanup orphan: %w", err)
	}
	return nil
}

// account Credential → 适配 *CloudAccount 转换（逐调用临时对象，仅内存；
// Secret 明文经 string 副本供 REST 构参，禁入日志/错误信息。Credential 不携带
// 账号 Regions——Azure 无地域语义（KV 账号级证书库），tenant/subscription 经
// 适配层 Option/env 注入，为任务 4 装配收敛时的账号源配置留缝）。
func (d *AzureDeployer) account(creds Credential) (*sharedomain.CloudAccount, error) {
	if err := creds.Validate(); err != nil {
		return nil, err
	}
	if creds.Cloud != string(domain.CloudAzure) {
		return nil, fmt.Errorf("azure deployer: credential cloud %q is not azure", creds.Cloud)
	}
	return &sharedomain.CloudAccount{
		Name:            creds.AccountKey,
		Provider:        sharedomain.CloudProviderAzure,
		AccessKeyID:     creds.AccessKey,
		AccessKeySecret: string(creds.Secret),
	}, nil
}
