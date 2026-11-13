package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alidns"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cdn"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ddosbgp"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/nas"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/r_kvstore"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/vpc"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/gotomicro/ego/core/elog"
)

// errPrimaryAccountForbidden 主账号 AK 接入被拒绝时返回的错误
const errPrimaryAccountForbidden = "禁止接入主账号 AK，请使用 RAM 子账号并授予只读权限"

// aliyunProbeSpec 单个资源类型的探测规格
type aliyunProbeSpec struct {
	AssetType string
	AssetName string
	Probe     func(ctx context.Context, ak, sk, region string) error
}

// aliyunProbeSpecs 阿里云按资源类型的最小列举探测（只读，limit=1 语义）
func aliyunProbeSpecs() []aliyunProbeSpec {
	return []aliyunProbeSpec{
		{AssetType: "ecs", AssetName: "云服务器 ECS", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := ecs.NewClientWithAccessKey(region, ak, sk)
			if err != nil {
				return err
			}
			req := ecs.CreateDescribeInstancesRequest()
			req.Scheme = "https"
			_, err = client.DescribeInstances(req)
			return err
		}},
		{AssetType: "rds", AssetName: "云数据库 RDS", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := rds.NewClientWithAccessKey(region, ak, sk)
			if err != nil {
				return err
			}
			req := rds.CreateDescribeDBInstancesRequest()
			req.Scheme = "https"
			_, err = client.DescribeDBInstances(req)
			return err
		}},
		{AssetType: "redis", AssetName: "云数据库 Redis", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := r_kvstore.NewClientWithAccessKey(region, ak, sk)
			if err != nil {
				return err
			}
			req := r_kvstore.CreateDescribeInstancesRequest()
			req.Scheme = "https"
			_, err = client.DescribeInstances(req)
			return err
		}},
		{AssetType: "slb", AssetName: "负载均衡 SLB", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := slb.NewClientWithAccessKey(region, ak, sk)
			if err != nil {
				return err
			}
			req := slb.CreateDescribeLoadBalancersRequest()
			req.Scheme = "https"
			_, err = client.DescribeLoadBalancers(req)
			return err
		}},
		{AssetType: "vpc", AssetName: "专有网络 VPC", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := vpc.NewClientWithAccessKey(region, ak, sk)
			if err != nil {
				return err
			}
			req := vpc.CreateDescribeVpcsRequest()
			req.Scheme = "https"
			_, err = client.DescribeVpcs(req)
			return err
		}},
		{AssetType: "nas", AssetName: "文件存储 NAS", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := nas.NewClientWithAccessKey(region, ak, sk)
			if err != nil {
				return err
			}
			req := nas.CreateDescribeFileSystemsRequest()
			req.Scheme = "https"
			_, err = client.DescribeFileSystems(req)
			return err
		}},
		{AssetType: "cdn", AssetName: "内容分发 CDN", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := cdn.NewClientWithAccessKey("cn-hangzhou", ak, sk)
			if err != nil {
				return err
			}
			req := cdn.CreateDescribeUserDomainsRequest()
			req.Scheme = "https"
			_, err = client.DescribeUserDomains(req)
			return err
		}},
		{AssetType: "ddos", AssetName: "DDoS 高防", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := ddosbgp.NewClientWithAccessKey(region, ak, sk)
			if err != nil {
				return err
			}
			req := ddosbgp.CreateDescribeInstanceListRequest()
			req.Scheme = "https"
			req.PageNo = "1"
			req.PageSize = "1"
			_, err = client.DescribeInstanceList(req)
			return err
		}},
		{AssetType: "dns", AssetName: "云解析 DNS", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := alidns.NewClientWithAccessKey("cn-hangzhou", ak, sk)
			if err != nil {
				return err
			}
			req := alidns.CreateDescribeDomainsRequest()
			req.Scheme = "https"
			_, err = client.DescribeDomains(req)
			return err
		}},
		{AssetType: "oss", AssetName: "对象存储 OSS", Probe: func(ctx context.Context, ak, sk, region string) error {
			client, err := oss.New(fmt.Sprintf("https://oss-%s.aliyuncs.com", region), ak, sk)
			if err != nil {
				return err
			}
			_, err = client.ListBuckets()
			return err
		}},
	}
}

// aliyunDeniedHints 判定「明确权限不足」的错误关键字
var aliyunDeniedHints = []string{
	"Forbidden",
	"AccessDenied",
	"NoPermission",
	"NotAuthorized",
	"not authorized",
	"Unauthorized",
	"no permission",
	"InvalidAccessKeyId",
	"SignatureDoesNotMatch",
	"InvalidAccessKeySecret",
	"403",
}

// urlQueryPattern 匹配 URL 查询串，用于脱敏（阿里云 SDK 报错会把完整签名 URL 带上）
var urlQueryPattern = regexp.MustCompile(`\?[^"\s]*`)

// redactMessage 从错误信息中抹掉凭证与签名查询串，避免落库/回显泄露
func redactMessage(msg, accessKeyID, accessKeySecret string) string {
	if accessKeyID != "" {
		msg = strings.ReplaceAll(msg, accessKeyID, "***")
	}
	if accessKeySecret != "" {
		msg = strings.ReplaceAll(msg, accessKeySecret, "***")
	}
	return urlQueryPattern.ReplaceAllString(msg, "?<redacted>")
}

// classifyProbeError 将探测错误归类为 ok / denied / error（消息已脱敏）
func classifyProbeError(err error, accessKeyID, accessKeySecret string) (status, code, message string) {
	if err == nil {
		return camdomain.PermissionStatusOK, "", ""
	}
	raw := redactMessage(err.Error(), accessKeyID, accessKeySecret)
	for _, hint := range aliyunDeniedHints {
		if strings.Contains(raw, hint) {
			return camdomain.PermissionStatusDenied, hint, truncateMessage(raw)
		}
	}
	return camdomain.PermissionStatusError, "", truncateMessage(raw)
}

// isTransientProbeError 判断是否为可重试的瞬时网络错误
func isTransientProbeError(err error) bool {
	if err == nil {
		return false
	}
	raw := err.Error()
	for _, hint := range []string{"i/o timeout", "connection reset", "TLS handshake timeout", "use of closed network connection"} {
		if strings.Contains(raw, hint) {
			return true
		}
	}
	return false
}

// truncateMessage 截断过长的错误信息，避免把整段堆栈落库
func truncateMessage(msg string) string {
	const maxLen = 300
	msg = strings.TrimSpace(strings.ReplaceAll(msg, "\n", " "))
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen] + "..."
}

// resolveAliyunRegion 取账号默认地域，缺省回退 cn-hangzhou
func resolveAliyunRegion(account *domain.CloudAccount) string {
	if len(account.Regions) > 0 && account.Regions[0] != "" {
		return account.Regions[0]
	}
	if len(account.Config.SupportedRegions) > 0 && account.Config.SupportedRegions[0] != "" {
		return account.Config.SupportedRegions[0]
	}
	return "cn-hangzhou"
}

// aliyunCallerIdentity 调用 STS GetCallerIdentity 识别调用者身份（网络抖动时重试一次）
func aliyunCallerIdentity(account *domain.CloudAccount) (identityType string, accountID string, err error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		client, cErr := sts.NewClientWithAccessKey("cn-hangzhou", account.AccessKeyID, account.AccessKeySecret)
		if cErr != nil {
			return "", "", cErr
		}
		req := sts.CreateGetCallerIdentityRequest()
		req.Scheme = "https"

		var resp *sts.GetCallerIdentityResponse
		resp, err = client.GetCallerIdentity(req)
		if err == nil {
			return resp.IdentityType, resp.AccountId, nil
		}
		lastErr = err
		if !isTransientProbeError(err) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", "", lastErr
}

// rejectPrimaryIdentity 身份判定：PrimaryAccount 一律拒绝，其余放行
// 单独抽成纯函数，便于在不依赖线上 STS 的情况下验证规则本身。
func rejectPrimaryIdentity(identityType string) error {
	if strings.EqualFold(identityType, "PrimaryAccount") {
		return fmt.Errorf("%s（当前身份：%s）", errPrimaryAccountForbidden, identityType)
	}
	return nil
}

// assertNotPrimaryAccount 拒绝主账号 AK（仅阿里云；其他厂商暂不校验）
func (s *cloudAccountService) assertNotPrimaryAccount(ctx context.Context, account *domain.CloudAccount) error {
	if account.Provider != domain.CloudProviderAliyun {
		return nil
	}

	identityType, accountID, err := aliyunCallerIdentity(account)
	if err != nil {
		// 识别失败不阻断接入：可能只是 STS 端点不可达，凭证有效性由后续验证兜底
		s.ensureLogger().Warn("识别阿里云调用者身份失败，跳过主账号校验",
			elog.String("provider", string(account.Provider)),
			elog.String("error", redactMessage(err.Error(), account.AccessKeyID, account.AccessKeySecret)))
		return nil
	}

	if err := rejectPrimaryIdentity(identityType); err != nil {
		s.ensureLogger().Warn("拒绝接入阿里云主账号 AK",
			elog.String("account_id", accountID),
			elog.String("identity_type", identityType))
		return err
	}

	return nil
}

// CheckPermissions 对云账号做权限体检并落库（当前仅阿里云实现探测，其余厂商返回 unsupported）
func (s *cloudAccountService) CheckPermissions(ctx context.Context, id int64) (*camdomain.PermissionCheck, error) {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	check := &camdomain.PermissionCheck{
		AccountID: account.ID,
		Provider:  string(account.Provider),
		CheckedAt: time.Now(),
	}

	if account.Provider != domain.CloudProviderAliyun {
		for _, spec := range aliyunProbeSpecs() {
			check.Items = append(check.Items, camdomain.PermissionItem{
				AssetType: spec.AssetType,
				AssetName: spec.AssetName,
				Status:    camdomain.PermissionStatusUnsupported,
				Message:   "该云厂商暂不支持权限探测",
			})
			check.Summary.Unsupported++
		}
	} else {
		region := resolveAliyunRegion(&account)

		identityType, _, identityErr := aliyunCallerIdentity(&account)
		if identityErr != nil {
			s.ensureLogger().Warn("权限体检：识别调用者身份失败",
				elog.Int64("account_id", account.ID),
				elog.String("error", redactMessage(identityErr.Error(), account.AccessKeyID, account.AccessKeySecret)))
			// 身份未识别必须显式提示：否则「禁止主账号」在这条链路上是静默失效的
			check.Items = append(check.Items, camdomain.PermissionItem{
				AssetType: "identity",
				AssetName: "AK 身份识别",
				Status:    camdomain.PermissionStatusError,
				Code:      "IdentityUnavailable",
				Message:   "STS 不可达，未能确认该 AK 是否为主账号；主账号拦截此刻无法生效，请检查网络后重试体检",
			})
			check.Summary.Error++
		}
		check.IdentityType = identityType
		check.IsPrimaryAccount = strings.EqualFold(identityType, "PrimaryAccount")

		for _, spec := range aliyunProbeSpecs() {
			probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			probeErr := spec.Probe(probeCtx, account.AccessKeyID, account.AccessKeySecret, region)
			cancel()

			// 瞬时网络错误重试一次，减少把「网络抖动」误报成「异常」
			if isTransientProbeError(probeErr) {
				retryCtx, retryCancel := context.WithTimeout(ctx, 8*time.Second)
				probeErr = spec.Probe(retryCtx, account.AccessKeyID, account.AccessKeySecret, region)
				retryCancel()
			}

			status, code, message := classifyProbeError(probeErr, account.AccessKeyID, account.AccessKeySecret)
			check.Items = append(check.Items, camdomain.PermissionItem{
				AssetType: spec.AssetType,
				AssetName: spec.AssetName,
				Status:    status,
				Code:      code,
				Message:   message,
			})

			switch status {
			case camdomain.PermissionStatusOK:
				check.Summary.OK++
			case camdomain.PermissionStatusDenied:
				check.Summary.Denied++
			case camdomain.PermissionStatusError:
				check.Summary.Error++
			}
		}
	}

	if s.permissionRepo != nil {
		if err := s.permissionRepo.Save(ctx, *check); err != nil {
			s.ensureLogger().Warn("权限体检结果落库失败",
				elog.Int64("account_id", account.ID), elog.FieldErr(err))
		}
	}

	s.ensureLogger().Info("云账号权限体检完成",
		elog.Int64("account_id", account.ID),
		elog.Int("ok", check.Summary.OK),
		elog.Int("denied", check.Summary.Denied),
		elog.Int("error", check.Summary.Error))

	return check, nil
}

// GetPermissionCheck 读取账号最近一次权限体检快照；未体检过返回 (nil, nil)
func (s *cloudAccountService) GetPermissionCheck(ctx context.Context, id int64) (*camdomain.PermissionCheck, error) {
	if s.permissionRepo == nil {
		return nil, nil
	}
	return s.permissionRepo.GetByAccountID(ctx, id)
}
