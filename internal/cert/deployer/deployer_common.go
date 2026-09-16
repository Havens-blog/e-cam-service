// deployer_common.go 部署器层共享小件（cert-multicloud-deployers 清理任务）：
// huawei/aws/azure 三云部署器逐字节重复的退避重试主干、上传名生成、凭证转换与
// 指纹口径收敛为单点实现。aliyun/tencent（5.4/5.5）既有私有副本暂保持原状
// （清理范围以 feature 变更文件为界），后续可平滑收编——共享函数签名与其
// withRetry/formatUploadName/account 方法体逐字段对应。
package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// boundedRetry 有界重试主干（RetryPolicy/固定退避序列与五云部署器共用口径）：
//   - ErrCloudRateLimited → 按固定序列退避后重试（计入总时长上限）；
//   - 上传名冲突（保守启发式，B2 同口径）→ 不退避立即换名重试（仅 UploadCert
//     语境出现，各云常态休眠分支）；
//   - 其余错误立即返回；
//   - 次数或总时长耗尽 → 包装末次错误返回（哨兵语义经 %w 保留，供 5.7 映射
//     rate_limited 状态与 5.8 rollbackErrCode 判定）。
//
// 业务级成败状态归 5.7 引擎（Hard Rule：部署器层无状态机判断）。
func boundedRetry(ctx context.Context, policy RetryPolicy, sleep func(context.Context, time.Duration) error, fn func(attempt int) error) error {
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
			if serr := sleep(ctx, backoff); serr != nil {
				return fmt.Errorf("backoff interrupted: %w", serr)
			}
		}
	}
}

// formatUploadName 生成上传名 {prefix}-{指纹前8}-{unix秒}-{随机后缀}（C7 单点公式）：
//   - 指纹前缀与五云部署器共用口径（证书叶 DER SHA256 前 8 hex，解析失败回退
//     整段 PEM SHA256，见 certNameFingerprintPrefix）；
//   - unix 秒 + 随机后缀保证逐次唯一（C7：重试不复用可能已成功的名称——重试即
//     新副本，孤儿清理兜底）；
//   - 防御性截断至 maxLen（各云证书名上限经部署器常量传入：huawei/aws=63、
//     tencent=50，上限约束的差异文档留在各云常量处）。
func formatUploadName(prefix string, maxLen int, certPEM string, now func() time.Time, randHex func(int) string) string {
	name := fmt.Sprintf("%s-%s-%d-%s",
		prefix, certNameFingerprintPrefix(certPEM), now().Unix(), randHex(4))
	if len(name) > maxLen {
		name = name[:maxLen]
	}
	return name
}

// cloudAccountFor Credential → 适配 *CloudAccount 转换（逐调用临时对象，仅内存；
// Secret 明文经 string 副本供 SDK/REST 构参，禁入日志/错误信息）。cloud 常量值
// 与错误文案的平台标签一致（domain.CloudHuawei="huawei" 等），各云部署器包装
// 方法的错误文案与本实现收敛前逐字节相同。
func cloudAccountFor(creds Credential, cloud domain.Cloud, provider sharedomain.CloudProvider) (*sharedomain.CloudAccount, error) {
	if err := creds.Validate(); err != nil {
		return nil, err
	}
	if creds.Cloud != string(cloud) {
		return nil, fmt.Errorf("%s deployer: credential cloud %q is not %s", cloud, creds.Cloud, cloud)
	}
	return &sharedomain.CloudAccount{
		Name:            creds.AccountKey,
		Provider:        provider,
		AccessKeyID:     creds.AccessKey,
		AccessKeySecret: string(creds.Secret),
	}, nil
}

// certFingerprint64Pattern 台账指纹对齐口径 ^[0-9a-f]{64}$（同 3.5/5.4/5.5；
// 非对齐口径一律视为无法复核——如华为云 SCM 原生 SHA-1 指纹（40hex）/空值，
// 上层按"指纹无法复核"处理，回滚判定 fail-safe 阻断，不误判有效）。
var certFingerprint64Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// unresolvedPlaceholderFingerprint 确定性占位指纹（与 3.5
// service.resolveUncached 同公式，两路径结果可对账）。
func unresolvedPlaceholderFingerprint(cacheKey string) string {
	sum := sha256.Sum256([]byte("certscan-unresolved:" + cacheKey))
	return hex.EncodeToString(sum[:])
}
