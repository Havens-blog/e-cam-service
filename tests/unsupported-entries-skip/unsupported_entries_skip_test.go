// @feature cert-cloud-discovery-import @api-functional
//
// Shared fixtures for the unsupported-entries-skip journey tests.
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package unsupported_entries_skip

import (
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
)

// mixedWorld builds the journey setup: a done snapshot mixing the huawei
// group, an AWS IAM-hosted entry and normal parseable entries, with aliyun
// and aws(ARN) material adapters registered and accounts active.
func mixedWorld(t *testing.T) (*discoverytest.Harness, *discoverytest.StubCertAdapter, *discoverytest.StubCertAdapter, string) {
	t.Helper()
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	aws := discoverytest.NewStubCertAdapter(domain.CloudAWS)
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aliyun, aws}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a")},
			domain.CloudAWS:    {discoverytest.ActiveAccount(domain.CloudAWS, "acct-aws")},
		}
	})
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 24, 14, 0, 0, 0, time.UTC))
	h.SeedRefs(
		discoverytest.RefSpec{Cloud: domain.CloudHuawei, AccountKey: "acct-hw", CloudCertID: "scm-1", Fingerprint: discoverytest.FP("skip-hw"), ResourceID: "res-hw", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAWS, AccountKey: "acct-aws", CloudCertID: "iam-123", Fingerprint: discoverytest.FP("skip-iam"), ResourceID: "res-iam", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAWS, AccountKey: "acct-aws", CloudCertID: "arn:aws:acm:us-east-1:1:certificate/ok", Fingerprint: discoverytest.FP("skip-arn"), ResourceID: "res-arn", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-A", Fingerprint: discoverytest.FP("skip-aliyun"), ResourceID: "res-a", SnapshotID: snapID},
	)
	return h, aliyun, aws, snapID
}

// goodMaterial registers an existing in-cloud certificate bundle.
func goodMaterial(b *certtest.CertBundle) service.DiscoveryCertMaterial {
	return service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
}

// Static per-item failure reasons ( fact IMPORT_ITEM_ERROR_REASONS ).
const (
	reasonUnsupportedCloud = "CERT_IMPORT_UNSUPPORTED: 该云证书暂不支持自动解析"
	reasonIAMHosted        = "CERT_IMPORT_UNSUPPORTED: IAM-hosted 证书暂不支持自动解析"
	reasonCertGone         = "CERT_GET_FAILED: 云侧已不存在"
	reasonGetCertFailed    = "CERT_GET_FAILED: 云证书读取失败"
)
