// huawei_deployer.go 华为云 CloudDeployer 实现（cert-multicloud-deployers 任务 1）。
//
// 分层定位：将 cloudx 华为云完整证书五方法适配（CertAdapter，SDK 单次调用
// 封装）组装为 deployer 层 CloudDeployer 端口实例，经 5.3 CloudAPIChannel
// 两段式编排注入（UploadCert→BindResource，第二段失败 CleanupOrphan 补偿）。
// 与 5.4 阿里云/5.5 腾讯云部署器结构对称（统一 CloudAPIChannel 消费口径）。
//
// 本层职责边界（Hard Rule）：
//   - 只做 per 云端口适配 + 限流有界退避重试 + 上传名逐次唯一生成（C7）；
//   - 不做业务级状态机判断——项级 failed/rate_limited 状态落库、回滚语义
//     归 5.7/5.8 引擎与回滚服务；
//   - 退避重试有上限次数与总时长双闸，禁止无限重试。
//
// 华为云差异（相对 5.4/5.5）：
//   - 证书库 = SCM（全局服务 cn-north-4 接入），云证书 ID = SCM 证书 ID
//     （UUID 形态，全局唯一）；四产品（cdn/waf/alb/nlb）绑定统一引用该 ID；
//   - ELB 监听证书 ID 形态归一化为幂等透传：SCM 证书 ID 无 aliyun
//     {certId}-{region} 式地域后缀形态（发现引用与上传产物同形态）；
//   - ListReferences 复用 3.3 发现适配引用形态；指纹解析口径同 3.5/5.4/5.5
//     （映射反查 → GetCert 要素〔仅 SHA256 对齐口径〕→ 确定性占位指纹）；
//     SCM ShowCertificate 原生指纹为 SHA-1 形态（40hex），完整适配层经
//     ExportCertificate 导出材料解析叶证书 SHA-256 对齐台账口径。
package deployer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cloudx-sdk/huawei"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// huaweiCertAPI 华为云完整证书适配窄接口（*huawei.CertAdapter 天然满足；测试
// 注入 fake）。签名即适配原签名——本层只消费，不修改适配层。
type huaweiCertAPI interface {
	UploadCert(ctx context.Context, creds *sharedomain.CloudAccount, product, name, certPEM, keyPEM string) (string, error)
	BindResource(ctx context.Context, creds *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error
	ListReferences(ctx context.Context, creds *sharedomain.CloudAccount, product string) ([]huawei.CloudCertRef, error)
	GetCert(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (huawei.CloudCertInfo, error)
	CleanupOrphan(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) error
}

// HuaweiDeployer 华为云 CloudDeployer：覆盖 cdn/waf/alb/nlb 四产品，
// 按 DeployTarget.product 路由到适配对应方法（适配层内部按产品分发）。
//
// resourceId 粒度约定（3.3 发现适配消费口径，本层原样透传）：
//   - cdn = 加速域名（www.example.com）
//   - waf = 云模式防护域名（hostname）
//   - alb/nlb = ELB v3 监听器复合定位 "{LoadBalancerId}/{ListenerId}"
//     （兼容存量纯监听 ID 形态）
type HuaweiDeployer struct {
	adapter  huaweiCertAPI
	mappings domain.CloudCertMappingRepository // 可空：ListReferences 指纹映射反查
	retry    RetryPolicy
	now      func() time.Time                                 // 上传名时间源（测试可注入）
	randHex  func(n int) string                               // 上传名随机后缀（测试可注入）
	sleep    func(ctx context.Context, d time.Duration) error // 退避睡眠（测试可注入）
}

// 编译期断言：满足 CloudDeployer 端口（供 5.3 CloudAPIChannel 注入）。
var _ CloudDeployer = (*HuaweiDeployer)(nil)

// NewHuaweiDeployer 创建华为云部署器。adapter 生产实现为 huawei.NewCertAdapter
// 产物；mappings 允许 nil（ListReferences 跳过映射反查，直接 GetCert fallback）。
func NewHuaweiDeployer(adapter huaweiCertAPI, mappings domain.CloudCertMappingRepository, opts ...HuaweiOption) *HuaweiDeployer {
	d := &HuaweiDeployer{
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
func (d *HuaweiDeployer) Stop() {
	if s, ok := d.adapter.(interface{ Stop() }); ok {
		s.Stop()
	}
}

// HuaweiOption HuaweiDeployer 装配选项。
type HuaweiOption func(*HuaweiDeployer)

// WithHuaweiRetryPolicy 覆盖限流退避参数（应用 config 读取路径；零值/非法
// 配置经 normalized 回退缺省保守值，重试安全侧）。
func WithHuaweiRetryPolicy(p RetryPolicy) HuaweiOption {
	return func(d *HuaweiDeployer) { d.retry = p.normalized() }
}

// withRetry 有界重试主干（实现收敛于 boundedRetry 单点，与 5.4/5.5 同口径）：
//   - ErrCloudRateLimited → 按固定序列退避后重试（计入总时长上限）；
//   - 上传名冲突（保守启发式，B2 同口径）→ 不退避立即换名重试（仅 UploadCert
//     语境出现；SCM name 非唯一键、DuplicateCheck=false 下常态休眠）；
//   - 次数或总时长耗尽 → 包装末次错误返回（哨兵语义经 %w 保留，供 5.7/5.8 判定）。
//
// 业务级成败状态归 5.7 引擎（Hard Rule：本层无状态机判断）。
func (d *HuaweiDeployer) withRetry(ctx context.Context, fn func(attempt int) error) error {
	return boundedRetry(ctx, d.retry, d.sleep, fn)
}

// ---------------------------------------------------------------------
// C7：上传名唯一生成（与 5.4/5.5 同公式）
// ---------------------------------------------------------------------

const (
	// huaweiUploadNamePrefix 上传名前缀（对齐 5.4/5.5 ecam 口径，云侧可辨识平台来源）。
	huaweiUploadNamePrefix = "ecam"
	// huaweiUploadNameMaxLen SCM 证书名长度上限（SCM 约束 3~63 字符：英文/数字/
	// 下划线/中划线/英文句点——ecam-{fp8}-{unix秒}-{随机hex} 形态天然合规）。
	huaweiUploadNameMaxLen = 63
	// huaweiUploadProduct 两段式第一段统一以 CDN 口径上传：SCM 证书 ID 即四产品
	// 绑定引用的统一云证书 ID（产品无关）。
	huaweiUploadProduct = huawei.CertProductCDN
)

// generateUploadName 生成 SCM 上传名 ecam-{指纹前8}-{unix秒}-{随机后缀}（C7
// 公式实现收敛于 formatUploadName 单点）：指纹前缀与 5.4/5.5 共用口径；unix 秒
// + 随机后缀保证逐次唯一（重试不复用可能已成功的名称；DuplicateCheck=false
// 下内容重复亦为独立副本）。
func (d *HuaweiDeployer) generateUploadName(certPEM string) string {
	return formatUploadName(huaweiUploadNamePrefix, huaweiUploadNameMaxLen, certPEM, d.now, d.randHex)
}

// ---------------------------------------------------------------------
// CloudDeployer 五方法
// ---------------------------------------------------------------------

// UploadCert 两段式第一段：生成唯一名（C7）上传 SCM 证书库，返回云证书 ID
// （SCM 证书 ID，四产品绑定共用）。每次尝试（含重试）生成全新名称——重试
// 不得复用可能已成功的名称（C7：重试即新副本，孤儿清理兜底）。
// keyPEM 明文仅内存传递，经 string 副本供适配层 SDK 构参，不落日志。
func (d *HuaweiDeployer) UploadCert(ctx context.Context, creds Credential, certPEM string, keyPEM []byte) (string, error) {
	acct, err := d.account(creds)
	if err != nil {
		return "", err
	}
	if certPEM == "" || len(keyPEM) == 0 {
		return "", errors.New("huawei deployer: upload cert requires cert PEM and key PEM")
	}
	var cloudCertID string
	err = d.withRetry(ctx, func(int) error {
		name := d.generateUploadName(certPEM)
		id, err := d.adapter.UploadCert(ctx, acct, huaweiUploadProduct, name, certPEM, string(keyPEM))
		if err != nil {
			return err
		}
		cloudCertID = id
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("huawei deployer: upload cert: %w", err)
	}
	return cloudCertID, nil
}

// BindResource 两段式第二段：按 product 路由到适配绑定方法（cdn/waf 域名级，
// alb/nlb 监听器级——复合 resourceID 由适配层解析）。ELB 监听证书 ID 形态
// 归一化（normalizeHuaweiListenerCertID）：华为云 SCM 证书 ID 全局唯一，无
// aliyun {certId}-{region} 式地域后缀形态，归一化为幂等透传（发现引用/回滚
// 旧 ID 不被改写）。
func (d *HuaweiDeployer) BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	bindCertID := normalizeHuaweiListenerCertID(product, cloudCertID)
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.BindResource(ctx, acct, product, resourceID, bindCertID)
	})
	if err != nil {
		return fmt.Errorf("huawei deployer: bind %s resource %s: %w", product, resourceID, err)
	}
	return nil
}

// normalizeHuaweiListenerCertID ELB（alb/nlb）监听证书 ID 形态归一化。
// 参照 5.4 normalizeAliyunListenerCertID 的归一化接缝：华为云 SCM 为全局服务，
// 证书 ID 全局唯一，ELB 监听引用（default_tls_container_ref）即裸 SCM 证书 ID，
// 无需地域后缀——归一化为幂等透传（含发现引用/回滚旧 ID 的原样保持），形态
// 差异若后续实网复核出现（如 ELB 托管证书前缀形态）在此单点扩展。
func normalizeHuaweiListenerCertID(product, cloudCertID string) string {
	if product != huawei.CertProductALB && product != huawei.CertProductNLB {
		return cloudCertID
	}
	return strings.TrimSpace(cloudCertID)
}

// ListReferences 只读发现：复用 3.3 发现适配引用形态（CDN 证书名/WAF SCM 证书
// ID/ELB SCM 证书 ID），指纹解析口径同 3.5/5.4/5.5（映射反查 → GetCert 要素
// 〔仅接受 SHA256 对齐口径；SCM 原生 SHA-1 指纹（40hex）一律按无法复核处理〕
// → 确定性占位指纹，占位公式与 3.5 扫描路径一致可对账）；同云证书多引用去重查询。
func (d *HuaweiDeployer) ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error) {
	acct, err := d.account(creds)
	if err != nil {
		return nil, err
	}
	var found []huawei.CloudCertRef
	err = d.withRetry(ctx, func(int) error {
		var e error
		found, e = d.adapter.ListReferences(ctx, acct, product)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("huawei deployer: list %s references: %w", product, err)
	}
	out := make([]domain.CertReference, 0, len(found))
	cache := make(map[string]string, len(found))
	for _, r := range found {
		out = append(out, domain.CertReference{
			CertFingerprint:       d.resolveFingerprint(ctx, acct, r, cache),
			Cloud:                 domain.CloudHuawei,
			Product:               domain.Product(r.Product),
			ResourceID:            r.ResourceID,
			ReferencedCloudCertID: r.ReferencedCloudCertID,
			AccountKey:            r.AccountKey,
		})
	}
	return out, nil
}

// resolveFingerprint 引用指纹解析（逐次发现去重缓存）。
func (d *HuaweiDeployer) resolveFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, r huawei.CloudCertRef, cache map[string]string,
) string {
	cacheKey := strings.Join([]string{string(domain.CloudHuawei), r.AccountKey, r.ReferencedCloudCertID}, "|")
	if fp, ok := cache[cacheKey]; ok {
		return fp
	}
	fp := d.resolveUncachedFingerprint(ctx, acct, cacheKey, r)
	cache[cacheKey] = fp
	return fp
}

// resolveUncachedFingerprint 解析主干：映射反查 → GetCert fallback → 占位指纹。
func (d *HuaweiDeployer) resolveUncachedFingerprint(
	ctx context.Context, acct *sharedomain.CloudAccount, cacheKey string, r huawei.CloudCertRef,
) string {
	if d.mappings != nil {
		if m, err := d.mappings.FindByCloudCertID(ctx, string(domain.CloudHuawei), r.AccountKey, r.ReferencedCloudCertID); err == nil {
			return m.CertFingerprint
		}
		// 无命中/仓储异常不中断发现（同 3.5 口径），走 GetCert fallback
	}
	var info huawei.CloudCertInfo
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
// 证书不存在归一为 Exists=false 非错误，并经导出材料产出 SHA-256 对齐指纹）。
func (d *HuaweiDeployer) GetCert(ctx context.Context, creds Credential, cloudCertID string) (CloudCertInfo, error) {
	acct, err := d.account(creds)
	if err != nil {
		return CloudCertInfo{}, err
	}
	var info huawei.CloudCertInfo
	err = d.withRetry(ctx, func(int) error {
		var e error
		info, e = d.adapter.GetCert(ctx, acct, cloudCertID)
		return e
	})
	if err != nil {
		return CloudCertInfo{}, fmt.Errorf("huawei deployer: get cert: %w", err)
	}
	return CloudCertInfo{
		Exists:      info.Exists,
		NotAfter:    info.NotAfter,
		Fingerprint: info.Fingerprint,
	}, nil
}

// CleanupOrphan 孤儿证书清理（适配层对已删除证书幂等成功——已不存在归一为
// 成功，清理队列重放安全）。
func (d *HuaweiDeployer) CleanupOrphan(ctx context.Context, creds Credential, cloudCertID string) error {
	acct, err := d.account(creds)
	if err != nil {
		return err
	}
	err = d.withRetry(ctx, func(int) error {
		return d.adapter.CleanupOrphan(ctx, acct, cloudCertID)
	})
	if err != nil {
		return fmt.Errorf("huawei deployer: cleanup orphan: %w", err)
	}
	return nil
}

// account Credential → 适配 *CloudAccount 转换（实现收敛于 cloudAccountFor
// 单点；Secret 明文经 string 副本供 SDK 构参，禁入日志/错误信息）。
func (d *HuaweiDeployer) account(creds Credential) (*sharedomain.CloudAccount, error) {
	return cloudAccountFor(creds, domain.CloudHuawei, sharedomain.CloudProviderHuawei)
}
