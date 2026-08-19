// tencent_deployer.go 腾讯云 CloudDeployer 实现（任务 5.5）。
//
// 分层定位：将 3.2 cloudx 腾讯云证书五方法适配（CertAdapter，SDK 单次调用
// 封装）组装为 deployer 层 CloudDeployer 端口实例，经 5.3 CloudAPIChannel
// 两段式编排注入（UploadCert→BindResource，第二段失败 CleanupOrphan 补偿）。
// 与 5.4 阿里云部署器结构对称（统一 CloudAPIChannel 消费口径）。
//
// 本层职责边界（Hard Rule）：
//   - 只做 per 云端口适配 + 限流有界退避重试 + 上传别名逐次唯一生成（C7）；
//   - 不做业务级状态机判断——项级 failed/rate_limited 状态落库、回滚语义
//     归 5.7/5.8 引擎与回滚服务；
//   - 退避重试有上限次数与总时长双闸，禁止无限重试。
//
// 腾讯云差异（相对 5.4 阿里云，poc-notes §1/§6）：
//   - Alias 可重复（L4），无 CAS 命名冲突重试分支；Repeatable=true 每次独立
//     副本，别名逐次唯一仅为可观测性与 C7 重试不复用语义；
//   - 无 ALB/NLB 监听证书 ID 形态归一化——云证书 ID 即 ssl 库 CertificateId；
//   - CleanupOrphan 的异步删除语义（B1）在 3.2 适配层内部有界轮询消化，
//     本层 nil 即"已删除"（含同步完成与异步任务终态两路径）。
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
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// tencentCertAPI 3.2 CertAdapter 窄接口（*tencent.CertAdapter 天然满足；测试注入
// fake）。签名即 3.2 适配原签名——本层只消费，不修改适配层。
type tencentCertAPI interface {
	UploadCert(ctx context.Context, creds *sharedomain.CloudAccount, product, name, certPEM, keyPEM string) (string, error)
	BindResource(ctx context.Context, creds *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error
	ListReferences(ctx context.Context, creds *sharedomain.CloudAccount, product string) ([]tencent.CloudCertRef, error)
	GetCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (tencent.CloudCertInfo, error)
	CleanupOrphan(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) error
}

// TencentDeployer 腾讯云 CloudDeployer：覆盖 cdn/waf(EdgeOne)/clb 三产品，
// 按 DeployTarget.product 路由到 3.2 适配对应方法（适配层内部按产品分发）。
//
// resourceId 粒度约定（3.2 适配层消费口径，本层原样透传）：
//   - cdn  = 域名（www.example.com）
//   - waf  = EdgeOne 站点接入域名 "{ZoneId}/{Host}"
//   - clb  = 监听器级复合定位 "{LoadBalancerId}/{ListenerId}"
type TencentDeployer struct {
	adapter  tencentCertAPI
	mappings domain.CloudCertMappingRepository // 可空：ListReferences 指纹映射反查
	retry    RetryPolicy
	now      func() time.Time                                 // 上传别名时间源（测试可注入）
	randHex  func(n int) string                               // 上传别名随机后缀（测试可注入）
	sleep    func(ctx context.Context, d time.Duration) error // 退避睡眠（测试可注入）
}

// 编译期断言：满足 CloudDeployer 端口（供 5.3 CloudAPIChannel 注入）。
var _ CloudDeployer = (*TencentDeployer)(nil)

// NewTencentDeployer 创建腾讯云部署器。adapter 生产实现为 tencent.NewCertAdapter
// 产物；mappings 允许 nil（ListReferences 跳过映射反查，直接 GetCert fallback）。
func NewTencentDeployer(adapter tencentCertAPI, mappings domain.CloudCertMappingRepository, opts ...TencentOption) *TencentDeployer {
	d := &TencentDeployer{
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
func (d *TencentDeployer) Stop() {
	if s, ok := d.adapter.(interface{ Stop() }); ok {
		s.Stop()
	}
}

// TencentOption TencentDeployer 装配选项。
type TencentOption func(*TencentDeployer)

// WithTencentRetryPolicy 覆盖限流退避参数（应用 config 读取路径；零值/非法
// 配置经 normalized 回退缺省保守值，重试安全侧）。
func WithTencentRetryPolicy(p RetryPolicy) TencentOption {
	return func(d *TencentDeployer) { d.retry = p.normalized() }
}

// withRetry 有界重试主干（RetryPolicy/固定退避序列与 5.4 共用）：
//   - ErrCloudRateLimited → 按固定序列退避后重试（计入总时长上限）；
//   - 其余错误（含格式拒绝/配额超限等不可重试配置错误，poc-notes §6-C3）
//     立即返回；
//   - 次数或总时长耗尽 → 包装末次错误返回（哨兵语义经 %w 保留，供 5.7 映射
//     rate_limited 状态与 5.8 rollbackErrCode 判定）。
//
// 与阿里云差异：无 CAS 命名冲突分支——腾讯云 Alias 可重复（L4），唯一性由
// 逐次生成保证而非冲突重试。业务级成败状态归 5.7 引擎（Hard Rule）。
func (d *TencentDeployer) withRetry(ctx context.Context, fn func(attempt int) error) error {
	policy := d.retry
	waited := time.Duration(0)
	attempts := 0
	for {
		attempts++
		err := fn(attempts)
		if err == nil {
			return nil
		}
		if !errors.Is(err, cloudx.ErrCloudRateLimited) {
			return err
		}
		if attempts >= policy.MaxAttempts {
			return fmt.Errorf("retries exhausted after %d attempts (total backoff %s): %w", attempts, waited, err)
		}
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

// ---------------------------------------------------------------------
// C7：上传别名唯一生成（与 5.4 同公式，可观测性与重试不复用语义）
// ---------------------------------------------------------------------

const (
	// tencentUploadNamePrefix 上传别名前缀（对齐 5.4 ecam 口径，云侧可辨识平台来源）。
	tencentUploadNamePrefix = "ecam"
	// tencentUploadNameMaxLen 别名防御性截断上限（腾讯云未公布 Alias 长度硬限制，
	// 取保守 50；实际长度 ~29 字符，截断仅极端随机后缀场景生效）。
	tencentUploadNameMaxLen = 50
	// tencentUploadProduct 两段式第一段统一经 ssl 证书库上传（产品无关，参数仅
	// 作适配层校验口径）；CertificateId 即各产品绑定引用的云证书 ID。
	tencentUploadProduct = tencent.CertProductCDN
)

// generateUploadName 生成 ssl 库上传别名 ecam-{指纹前8}-{unix秒}-{随机后缀}：
//   - 指纹前缀与 5.4 共用口径（证书叶 DER SHA256 前 8 hex，解析失败回退整段 PEM）；
//   - unix 秒 + 随机后缀保证逐次唯一（C7：重试不复用可能已成功的别名；
//     云侧 Repeatable=true 本就允许重复，唯一性主要为孤儿归属可辨识）；
//   - 防御性截断。
func (d *TencentDeployer) generateUploadName(certPEM string) string {
	name := fmt.Sprintf("%s-%s-%d-%s",
		tencentUploadNamePrefix, certNameFingerprintPrefix(certPEM), d.now().Unix(), d.randHex(4))
	if len(name) > tencentUploadNameMaxLen {
		name = name[:tencentUploadNameMaxLen]
	}
	return name
}

// ---------------------------------------------------------------------
// CloudDeployer 五方法
// ---------------------------------------------------------------------

// UploadCert 两段式第一段：生成唯一别名（C7）上传 ssl 证书库，返回云证书 ID
// （CertificateId，各产品绑定共用）。每次尝试（含重试）生成全新别名——重试
// 不得复用可能已成功的别名（C7：重试即新副本，孤儿清理兜底）。
// keyPEM 明文仅内存传递，经 string 副本供 3.2 SDK 构参，不落日志。
func (d *TencentDeployer) UploadCert(ctx context.Context, creds Credential, certPEM string, keyPEM []byte) (string, error) {
	acct, err := d.account(creds)
	if err != nil {
		return "", err
	}
	if certPEM == "" || len(keyPEM) == 0 {
		return "", errors.New("tencent deployer: upload cert requires cert PEM and key PEM")
	}
	var cloudCertID string
	err = d.withRetry(ctx, func(int) error {
		name := d.generateUploadName(certPEM)
		id, err := d.adapter.UploadCert(ctx, acct, tencentUploadProduct, name, certPEM, string(keyPEM))
		if err != nil {
			return err
		}
		cloudCertID = id
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("tencent deployer: upload cert: %w", err)
	}
	return cloudCertID, nil
}

// BindResource 两段式第二段：按 product 路由到 3.2 适配绑定方法（cdn 域名级，
// waf(EdgeOne) 站点域名级，clb 监听器级——复合 resourceID 由适配层解析，
// 本层不做归一化：腾讯云证书 ID 即 ssl 库 CertificateId，无地域后缀形态）。
func (d *TencentDeployer) BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.BindResource(ctx, acct, product, resourceID, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("tencent deployer: bind %s resource %s: %w", product, resourceID, err)
	}
	return nil
}

// ListReferences 只读发现：返回完整 CertReference 形态，指纹解析口径同 3.5/5.4
// （映射反查 → GetCert 要素〔仅接受 SHA256 对齐口径；腾讯云 CertFingerprint 为
// SHA1 形态（poc-notes B3），40hex 回退值一律按无法复核处理〕→ 确定性占位指纹，
// 占位公式与 3.5 扫描路径一致可对账）；同云证书多引用去重查询。
func (d *TencentDeployer) ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error) {
	acct, err := d.account(creds)
	if err != nil {
		return nil, err
	}
	var found []tencent.CloudCertRef
	err = d.withRetry(ctx, func(int) error {
		var e error
		found, e = d.adapter.ListReferences(ctx, acct, product)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("tencent deployer: list %s references: %w", product, err)
	}
	out := make([]domain.CertReference, 0, len(found))
	cache := make(map[string]string, len(found))
	for _, r := range found {
		out = append(out, domain.CertReference{
			CertFingerprint:       d.resolveFingerprint(ctx, acct, r, cache),
			Cloud:                 domain.CloudTencent,
			Product:               domain.Product(r.Product),
			ResourceID:            r.ResourceID,
			ReferencedCloudCertID: r.ReferencedCloudCertID,
			AccountKey:            r.AccountKey,
		})
	}
	return out, nil
}

// tencentFingerprintPattern 台账指纹对齐口径 ^[0-9a-f]{64}$（同 3.5/5.4；
// 腾讯云 SHA1 回退指纹（40hex）/空值等非对齐口径一律视为无法复核）。
var tencentFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// resolveFingerprint 引用指纹解析（逐次发现去重缓存）。
func (d *TencentDeployer) resolveFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, r tencent.CloudCertRef, cache map[string]string,
) string {
	cacheKey := strings.Join([]string{string(domain.CloudTencent), r.AccountKey, r.ReferencedCloudCertID}, "|")
	if fp, ok := cache[cacheKey]; ok {
		return fp
	}
	fp := d.resolveUncachedFingerprint(ctx, acct, cacheKey, r)
	cache[cacheKey] = fp
	return fp
}

// resolveUncachedFingerprint 解析主干：映射反查 → GetCert fallback → 占位指纹。
func (d *TencentDeployer) resolveUncachedFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, cacheKey string, r tencent.CloudCertRef,
) string {
	if d.mappings != nil {
		if m, err := d.mappings.FindByCloudCertID(ctx, string(domain.CloudTencent), r.AccountKey, r.ReferencedCloudCertID); err == nil {
			return m.CertFingerprint
		}
		// 无命中/仓储异常不中断发现（同 3.5 口径），走 GetCert fallback
	}
	var info tencent.CloudCertInfo
	if err := d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, r.ReferencedCloudCertID)
		return e
	}); err == nil && info.Exists && tencentFingerprintPattern.MatchString(info.Fingerprint) {
		return info.Fingerprint
	}
	// 确定性占位指纹（与 3.5 service.resolveUncached 同公式，两路径结果可对账）。
	sum := sha256.Sum256([]byte("certscan-unresolved:" + cacheKey))
	return hex.EncodeToString(sum[:])
}

// GetCert 查询云侧证书在库状态（回滚目标有效性校验依据，只读；3.2 层已将
// 证书不存在归一为 Exists=false 非错误）。
func (d *TencentDeployer) GetCert(ctx context.Context, creds Credential, cloudCertID string) (CloudCertInfo, error) {
	acct, err := d.account(creds)
	if err != nil {
		return CloudCertInfo{}, err
	}
	var info tencent.CloudCertInfo
	err = d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, cloudCertID)
		return e
	})
	if err != nil {
		return CloudCertInfo{}, fmt.Errorf("tencent deployer: get cert: %w", err)
	}
	return CloudCertInfo{
		Exists:      info.Exists,
		NotAfter:    info.NotAfter,
		Fingerprint: info.Fingerprint,
	}, nil
}

// CleanupOrphan 孤儿证书清理（3.2 层对已删除证书幂等成功；B1 异步删除语义
// 已在适配层内部有界轮询消化——nil 即"已删除"，清理队列重放安全）。
func (d *TencentDeployer) CleanupOrphan(ctx context.Context, creds Credential, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.CleanupOrphan(ctx, acct, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("tencent deployer: cleanup orphan: %w", err)
	}
	return nil
}

// account Credential → 3.2 适配 *CloudAccount 转换（逐调用临时对象，仅内存；
// Secret 明文经 string 副本供 SDK 构参，禁入日志/错误信息）。
func (d *TencentDeployer) account(creds Credential) (*sharedomain.CloudAccount, error) {
	if err := creds.Validate(); err != nil {
		return nil, err
	}
	if creds.Cloud != string(domain.CloudTencent) {
		return nil, fmt.Errorf("tencent deployer: credential cloud %q is not tencent", creds.Cloud)
	}
	return &sharedomain.CloudAccount{
		Name:            creds.AccountKey,
		Provider:        sharedomain.CloudProviderTencent,
		AccessKeyID:     creds.AccessKey,
		AccessKeySecret: string(creds.Secret),
	}, nil
}
