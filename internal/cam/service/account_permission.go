package service

import (
	"context"
	"fmt"
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

// classifyProbeError 将探测错误归类为 ok / denied / error
func classifyProbeError(err error) (status, code, message string) {
	if err == nil {
		return camdomain.PermissionStatusOK, "", ""
	}
	raw := err.Error()
	for _, hint := range aliyunDeniedHints {
		if strings.Contains(raw, hint) {
			return camdomain.PermissionStatusDenied, hint, truncateMessage(raw)
		}
	}
	return camdomain.PermissionStatusError, "", truncateMessage(raw)
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

// aliyunCallerIdentity 调用 STS GetCallerIdentity 识别调用者身份
func aliyunCallerIdentity(account *domain.CloudAccount) (identityType string, accountID string, err error) {
	client, err := sts.NewClientWithAccessKey("cn-hangzhou", account.AccessKeyID, account.AccessKeySecret)
	if err != nil {
		return "", "", err
	}
	req := sts.CreateGetCallerIdentityRequest()
	req.Scheme = "https"
	resp, err := client.GetCallerIdentity(req)
	if err != nil {
		return "", "", err
	}
	return resp.IdentityType, resp.AccountId, nil
}

// assertNotPrimaryAccount 拒绝主账号 AK（仅阿里云；其他厂商暂不校验）
func (s *cloudAccountService) assertNotPrimaryAccount(ctx context.Context, account *domain.CloudAccount) error {
	if account.Provider != domain.CloudProviderAliyun {
		return nil
	}

	identityCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	identityType, accountID, err := aliyunCallerIdentity(account)
	if err != nil {
		// 识别失败不阻断接入：可能只是 STS 端点不可达，凭证有效性由后续验证兜底
		s.ensureLogger().Warn("识别阿里云调用者身份失败，跳过主账号校验",
			elog.String("provider", string(account.Provider)),
			elog.FieldErr(err))
		return nil
	}

	if strings.EqualFold(identityType, "PrimaryAccount") {
		s.ensureLogger().Warn("拒绝接入阿里云主账号 AK",
			elog.String("account_id", accountID),
			elog.String("identity_type", identityType))
		return fmt.Errorf("%s（当前身份：%s）", errPrimaryAccountForbidden, identityType)
	}

	_ = identityCtx
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
				elog.Int64("account_id", account.ID), elog.FieldErr(identityErr))
		}
		check.IdentityType = identityType
		check.IsPrimaryAccount = strings.EqualFold(identityType, "PrimaryAccount")

		for _, spec := range aliyunProbeSpecs() {
			probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			probeErr := spec.Probe(probeCtx, account.AccessKeyID, account.AccessKeySecret, region)
			cancel()

			status, code, message := classifyProbeError(probeErr)
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
