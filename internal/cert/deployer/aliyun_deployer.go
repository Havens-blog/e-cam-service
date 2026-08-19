// aliyun_deployer.go 阿里云 CloudDeployer 实现（任务 5.4）。
//
// 分层定位：将 3.1 cloudx 阿里云证书五方法适配（CertAdapter，SDK 单次调用
// 封装）组装为 deployer 层 CloudDeployer 端口实例，经 5.3 CloudAPIChannel
// 两段式编排注入（UploadCert→BindResource，第二段失败 CleanupOrphan 补偿）。
//
// 本层职责边界（Hard Rule）：
//   - 只做 per 云端口适配 + 限流有界退避重试 + CAS 唯一上传名生成（B2）；
//   - 不做业务级状态机判断——项级 failed/rate_limited 状态落库、回滚语义
//     归 5.7/5.8 引擎与回滚服务；
//   - 退避重试有上限次数与总时长双闸，禁止无限重试。
package deployer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	sdkerrors "github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// aliyunCertAPI 3.1 CertAdapter 窄接口（*aliyun.CertAdapter 天然满足；测试注入
// fake）。签名即 3.1 适配原签名——本层只消费，不修改适配层。
type aliyunCertAPI interface {
	UploadCert(ctx context.Context, creds *sharedomain.CloudAccount, product, name, certPEM, keyPEM string) (string, error)
	BindResource(ctx context.Context, creds *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error
	ListReferences(ctx context.Context, creds *sharedomain.CloudAccount, product string) ([]aliyun.CloudCertRef, error)
	GetCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (aliyun.CloudCertInfo, error)
	CleanupOrphan(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) error
}

// AliyunDeployer 阿里云 CloudDeployer：覆盖 cdn/dcdn/waf/alb/nlb 五产品，
// 按 DeployTarget.product 路由到 3.1 适配对应方法（适配层内部按产品分发）。
type AliyunDeployer struct {
	adapter  aliyunCertAPI
	mappings domain.CloudCertMappingRepository // 可空：ListReferences 指纹映射反查
	retry    RetryPolicy
	now      func() time.Time                                 // 上传名时间源（测试可注入）
	randHex  func(n int) string                               // 上传名随机后缀（测试可注入）
	sleep    func(ctx context.Context, d time.Duration) error // 退避睡眠（测试可注入）
}

// 编译期断言：满足 CloudDeployer 端口（供 5.3 CloudAPIChannel 注入）。
var _ CloudDeployer = (*AliyunDeployer)(nil)

// NewAliyunDeployer 创建阿里云部署器。adapter 生产实现为 aliyun.NewCertAdapter
// 产物；mappings 允许 nil（ListReferences 跳过映射反查，直接 GetCert fallback）。
func NewAliyunDeployer(adapter aliyunCertAPI, mappings domain.CloudCertMappingRepository, opts ...AliyunOption) *AliyunDeployer {
	d := &AliyunDeployer{
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
func (d *AliyunDeployer) Stop() {
	if s, ok := d.adapter.(interface{ Stop() }); ok {
		s.Stop()
	}
}

// ---------------------------------------------------------------------
// 限流退避有界策略（AC-3 / Hard Rule）
// ---------------------------------------------------------------------

const (
	// DefaultMaxRetryAttempts 总尝试次数上限（含首次）：1 + 4 次退避重试。
	DefaultMaxRetryAttempts = 5
	// DefaultMaxTotalRetryWait 退避总时长上限（=固定序列全量和）。
	DefaultMaxTotalRetryWait = 15 * time.Second
)

// DefaultRateLimitBackoffs 限流退避固定序列（1s/2s/4s/8s）。
var DefaultRateLimitBackoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}

// RetryPolicy 限流退避有界策略：MaxAttempts 与 MaxTotalWait 双闸，任一耗尽
// 即停止重试（禁无限重试）。AlertConfig.thresholds 现无退避字段，采用缺省
// 保守值。
type RetryPolicy struct {
	MaxAttempts  int             // 总尝试次数上限（含首次），>=1
	Backoffs     []time.Duration // 固定退避序列：第 n 次重试前等待 Backoffs[n-1]，序列耗尽沿用末值
	MaxTotalWait time.Duration   // 退避总时长上限；下一档将超限即停止
}

// DefaultRetryPolicy 缺省保守策略。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:  DefaultMaxRetryAttempts,
		Backoffs:     DefaultRateLimitBackoffs,
		MaxTotalWait: DefaultMaxTotalRetryWait,
	}
}

// normalized 零值/非法配置回退缺省保守值（重试安全侧：宁可用保守默认也不带病运行）。
func (p RetryPolicy) normalized() RetryPolicy {
	def := DefaultRetryPolicy()
	if p.MaxAttempts < 1 || len(p.Backoffs) == 0 || p.MaxTotalWait <= 0 {
		return def
	}
	for _, b := range p.Backoffs {
		if b <= 0 {
			return def
		}
	}
	return p
}

// AliyunOption AliyunDeployer 装配选项。
type AliyunOption func(*AliyunDeployer)

// withRetry 有界重试主干：
//   - ErrCloudRateLimited → 按固定序列退避后重试（计入总时长上限）；
//   - CAS 命名冲突（B2/C2）→ 不退避立即换名重试（仅 UploadCert 语境出现）；
//   - 其余错误立即返回；
//   - 次数或总时长耗尽 → 包装末次错误返回（哨兵语义经 %w 保留，供 5.7 映射
//     rate_limited 状态与 5.8 rollbackErrCode 判定）。
//
// 业务级成败状态归 5.7 引擎（Hard Rule：本层无状态机判断）。
func (d *AliyunDeployer) withRetry(ctx context.Context, fn func(attempt int) error) error {
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

// sleepWithContext 退避睡眠（可被 ctx 取消打断）。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ---------------------------------------------------------------------
// B2/C2/C7：CAS 唯一上传名生成
// ---------------------------------------------------------------------

const (
	// aliyunUploadNamePrefix 上传名前缀（poc-notes §6-C2 命名规则）。
	aliyunUploadNamePrefix = "ecam"
	// aliyunUploadNameMaxLen CAS Name 同一用户下唯一且 ≤63 字符（poc-notes L8/B2）。
	aliyunUploadNameMaxLen = 63
	// aliyunUploadProduct 两段式第一段统一以 CDN 口径上传：CAS 纯数字 ID 形态；
	// ALB/NLB 监听引用的 "{certId}-{region}" 形态由 BindResource 按产品归一化追加。
	aliyunUploadProduct = aliyun.CertProductCDN
)

// generateUploadName 生成 CAS 上传名 ecam-{指纹前8}-{unix秒}-{随机后缀}：
//   - 指纹前缀取证书叶 DER SHA256 前 8 hex（解析失败回退整段 PEM SHA256）；
//   - unix 秒 + 随机后缀保证逐次唯一（重试换名，C7：不复用可能已成功的名称）；
//   - 防御性截断至 63 字符。
func (d *AliyunDeployer) generateUploadName(certPEM string) string {
	name := fmt.Sprintf("%s-%s-%d-%s",
		aliyunUploadNamePrefix, certNameFingerprintPrefix(certPEM), d.now().Unix(), d.randHex(4))
	if len(name) > aliyunUploadNameMaxLen {
		name = name[:aliyunUploadNameMaxLen]
	}
	return name
}

// certNameFingerprintPrefix 证书材料指纹前缀（8 hex）。
func certNameFingerprintPrefix(certPEM string) string {
	if block, _ := pem.Decode([]byte(certPEM)); block != nil {
		if leaf, err := x509.ParseCertificate(block.Bytes); err == nil {
			sum := sha256.Sum256(leaf.Raw)
			return hex.EncodeToString(sum[:])[:8]
		}
	}
	sum := sha256.Sum256([]byte(certPEM))
	return hex.EncodeToString(sum[:])[:8]
}

// randomHexSuffix n 位随机 hex 后缀；随机源失效时时间纳秒兜底（唯一性降级不失败）。
func randomHexSuffix(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n]
	}
	return hex.EncodeToString(b)[:n]
}

// isCertNameConflictErr CAS 证书名冲突判定（B2：Name 同一用户下唯一；实际错误
// 码待实网确认 poc-notes F9，按 ServerError 错误码/文案保守启发式）。
func isCertNameConflictErr(err error) bool {
	if err == nil {
		return false
	}
	var serverErr *sdkerrors.ServerError
	if errors.As(err, &serverErr) {
		return certNameConflictText(serverErr.ErrorCode()) || certNameConflictText(serverErr.Message())
	}
	return certNameConflictText(err.Error())
}

// certNameConflictText 名称冲突文案判定（大小写不敏感子串）。
func certNameConflictText(s string) bool {
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "name") && !strings.Contains(lower, "名称") {
		return false
	}
	return strings.Contains(lower, "exist") ||
		strings.Contains(lower, "duplicate") ||
		strings.Contains(lower, "already") ||
		strings.Contains(lower, "已存在") ||
		strings.Contains(lower, "重复")
}

// ---------------------------------------------------------------------
// CloudDeployer 五方法
// ---------------------------------------------------------------------

// UploadCert 两段式第一段：生成唯一名（B2）上传 CAS，返回云证书 ID（纯数字
// 形态）。每次尝试（含重试）生成全新名称——CAS Name 用户级唯一，且重试不得
// 复用可能已成功的名称（C7：重试即新副本，孤儿清理兜底）。
// keyPEM 明文仅内存传递，经 string 副本供 3.1 SDK 构参，不落日志。
func (d *AliyunDeployer) UploadCert(ctx context.Context, creds Credential, certPEM string, keyPEM []byte) (string, error) {
	acct, err := d.account(creds)
	if err != nil {
		return "", err
	}
	if certPEM == "" || len(keyPEM) == 0 {
		return "", errors.New("aliyun deployer: upload cert requires cert PEM and key PEM")
	}
	var cloudCertID string
	err = d.withRetry(ctx, func(int) error {
		name := d.generateUploadName(certPEM)
		id, err := d.adapter.UploadCert(ctx, acct, aliyunUploadProduct, name, certPEM, string(keyPEM))
		if err != nil {
			return err
		}
		cloudCertID = id
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("aliyun deployer: upload cert: %w", err)
	}
	return cloudCertID, nil
}

// BindResource 两段式第二段：按 product 路由到 3.1 适配绑定方法（cdn/dcdn/waf
// 域名级，alb/nlb 监听级）。ALB/NLB 监听引用 "{certId}-{region}" 形态在此归一化
// （第一段产物为纯数字 ID；已带后缀的回滚/发现引用幂等保持）。
func (d *AliyunDeployer) BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	bindCertID := normalizeAliyunListenerCertID(product, cloudCertID, acct)
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.BindResource(ctx, acct, product, resourceID, bindCertID)
	})
	if err != nil {
		return fmt.Errorf("aliyun deployer: bind %s resource %s: %w", product, resourceID, err)
	}
	return nil
}

// normalizeAliyunListenerCertID ALB/NLB 监听证书 ID 形态归一化。
func normalizeAliyunListenerCertID(product, cloudCertID string, acct *sharedomain.CloudAccount) string {
	if product != aliyun.CertProductALB && product != aliyun.CertProductNLB {
		return cloudCertID
	}
	if cloudCertID == "" || strings.Contains(cloudCertID, "-") {
		return cloudCertID
	}
	return cloudCertID + "-" + casRegionForAccount(acct)
}

// casRegionForAccount CAS 接入地域（镜像 3.1 certCASRegion 映射：仅 ap-southeast-1
// 国际站直连，其余一律 cn-hangzhou；与第一段上传地域一致，后缀自洽）。
func casRegionForAccount(acct *sharedomain.CloudAccount) string {
	if acct != nil && len(acct.Regions) > 0 && acct.Regions[0] == "ap-southeast-1" {
		return "ap-southeast-1"
	}
	return "cn-hangzhou"
}

// ListReferences 只读发现：返回完整 CertReference 形态，指纹解析口径同 3.5
// （映射反查 → GetCert 要素〔仅接受 SHA256 对齐口径〕→ 确定性占位指纹，
// 占位公式与 3.5 扫描路径一致可对账）；同云证书多引用去重查询。
func (d *AliyunDeployer) ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error) {
	acct, err := d.account(creds)
	if err != nil {
		return nil, err
	}
	var found []aliyun.CloudCertRef
	err = d.withRetry(ctx, func(int) error {
		var e error
		found, e = d.adapter.ListReferences(ctx, acct, product)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("aliyun deployer: list %s references: %w", product, err)
	}
	out := make([]domain.CertReference, 0, len(found))
	cache := make(map[string]string, len(found))
	for _, r := range found {
		out = append(out, domain.CertReference{
			CertFingerprint:       d.resolveFingerprint(ctx, acct, r, cache),
			Cloud:                 domain.CloudAliyun,
			Product:               domain.Product(r.Product),
			ResourceID:            r.ResourceID,
			ReferencedCloudCertID: r.ReferencedCloudCertID,
			AccountKey:            r.AccountKey,
		})
	}
	return out, nil
}

// aliyunFingerprintPattern 台账指纹对齐口径 ^[0-9a-f]{64}$（同 3.5；CAS 原始
// 指纹等非对齐口径一律视为无法复核）。
var aliyunFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// resolveFingerprint 引用指纹解析（逐次发现去重缓存）。
func (d *AliyunDeployer) resolveFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, r aliyun.CloudCertRef, cache map[string]string,
) string {
	cacheKey := strings.Join([]string{string(domain.CloudAliyun), r.AccountKey, r.ReferencedCloudCertID}, "|")
	if fp, ok := cache[cacheKey]; ok {
		return fp
	}
	fp := d.resolveUncachedFingerprint(ctx, acct, cacheKey, r)
	cache[cacheKey] = fp
	return fp
}

// resolveUncachedFingerprint 解析主干：映射反查 → GetCert fallback → 占位指纹。
func (d *AliyunDeployer) resolveUncachedFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, cacheKey string, r aliyun.CloudCertRef,
) string {
	if d.mappings != nil {
		if m, err := d.mappings.FindByCloudCertID(ctx, string(domain.CloudAliyun), r.AccountKey, r.ReferencedCloudCertID); err == nil {
			return m.CertFingerprint
		}
		// 无命中/仓储异常不中断发现（同 3.5 口径），走 GetCert fallback
	}
	var info aliyun.CloudCertInfo
	if err := d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, r.ReferencedCloudCertID)
		return e
	}); err == nil && info.Exists && aliyunFingerprintPattern.MatchString(info.Fingerprint) {
		return info.Fingerprint
	}
	// 确定性占位指纹（与 3.5 service.resolveUncached 同公式，两路径结果可对账）。
	sum := sha256.Sum256([]byte("certscan-unresolved:" + cacheKey))
	return hex.EncodeToString(sum[:])
}

// GetCert 查询云侧证书在库状态（回滚目标有效性校验依据，只读；3.1 层已将
// 404/NotExist 归一为 Exists=false 非错误）。
func (d *AliyunDeployer) GetCert(ctx context.Context, creds Credential, cloudCertID string) (CloudCertInfo, error) {
	acct, err := d.account(creds)
	if err != nil {
		return CloudCertInfo{}, err
	}
	var info aliyun.CloudCertInfo
	err = d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, cloudCertID)
		return e
	})
	if err != nil {
		return CloudCertInfo{}, fmt.Errorf("aliyun deployer: get cert: %w", err)
	}
	return CloudCertInfo{
		Exists:      info.Exists,
		NotAfter:    info.NotAfter,
		Fingerprint: info.Fingerprint,
	}, nil
}

// CleanupOrphan 孤儿证书清理（3.1 层对已删除证书幂等成功，清理队列重放安全）。
func (d *AliyunDeployer) CleanupOrphan(ctx context.Context, creds Credential, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.CleanupOrphan(ctx, acct, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("aliyun deployer: cleanup orphan: %w", err)
	}
	return nil
}

// account Credential → 3.1 适配 *CloudAccount 转换（逐调用临时对象，仅内存；
// Secret 明文经 string 副本供 SDK 构参，禁入日志/错误信息）。
func (d *AliyunDeployer) account(creds Credential) (*sharedomain.CloudAccount, error) {
	if err := creds.Validate(); err != nil {
		return nil, err
	}
	if creds.Cloud != string(domain.CloudAliyun) {
		return nil, fmt.Errorf("aliyun deployer: credential cloud %q is not aliyun", creds.Cloud)
	}
	return &sharedomain.CloudAccount{
		Name:            creds.AccountKey,
		Provider:        sharedomain.CloudProviderAliyun,
		AccessKeyID:     creds.AccessKey,
		AccessKeySecret: string(creds.Secret),
	}, nil
}
