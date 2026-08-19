package deployer

import (
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
)

// AC-1：ChannelType 四常量取值与 tech-design Interface 1 注释逐项对齐
// （bastion/agent 为二期接口预留，仅常量）。
func TestChannelTypeConstants(t *testing.T) {
	assert.Equal(t, ChannelType("cloud_api"), ChannelTypeCloudAPI)
	assert.Equal(t, ChannelType("k8s_api"), ChannelTypeK8sAPI)
	assert.Equal(t, ChannelType("bastion"), ChannelTypeBastion)
	assert.Equal(t, ChannelType("agent"), ChannelTypeAgent)
}

// AC-1/AC-4：Credential 字段与分支约束（tech-design Service-Level Types）。
func TestCredentialValidate(t *testing.T) {
	validCloudAK := Credential{
		Kind: CredentialKindCloudAK, Cloud: "aliyun", AccountKey: "acc-1",
		AccessKey: "ak", Secret: []byte("sk"), KeyVersion: 1,
	}
	assert.NoError(t, validCloudAK.Validate())

	validKubeconfig := Credential{
		Kind: CredentialKindKubeconfig, Secret: []byte("kubeconfig-yaml"), KeyVersion: 2,
	}
	assert.NoError(t, validKubeconfig.Validate())

	cases := []struct {
		name    string
		mutate  func(c *Credential)
		wantErr error
	}{
		{"cloud_ak 缺 cloud", func(c *Credential) { c.Cloud = "" }, ErrInvalidCredential},
		{"cloud_ak 缺 accountKey", func(c *Credential) { c.AccountKey = "" }, ErrInvalidCredential},
		{"cloud_ak 缺 accessKey", func(c *Credential) { c.AccessKey = "" }, ErrInvalidCredential},
		{"kubeconfig 携带 cloud", func(c *Credential) { c.Kind = CredentialKindKubeconfig; c.Cloud = "aliyun" }, ErrInvalidCredential},
		{"kubeconfig 携带 accountKey", func(c *Credential) { c.Kind = CredentialKindKubeconfig; c.AccountKey = "acc" }, ErrInvalidCredential},
		{"未知 kind", func(c *Credential) { c.Kind = "token" }, ErrInvalidCredential},
		{"空 secret", func(c *Credential) { c.Secret = nil }, ErrInvalidCredential},
		{"keyVersion<1", func(c *Credential) { c.KeyVersion = 0 }, ErrInvalidCredential},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCloudAK
			tc.mutate(&c)
			err := c.Validate()
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// AC-4：Credential 明文零生命周期——Zeroize 清零共享底层数组、
// String() 不泄露 Secret。
func TestCredentialZeroizeAndString(t *testing.T) {
	creds := Credential{
		Kind: CredentialKindCloudAK, Cloud: "tencent", AccountKey: "acc",
		AccessKey: "ak", Secret: []byte("top-secret-sk-material"), KeyVersion: 1,
	}
	// 值副本 Zeroize：切片头置 nil、底层数组清零；先前捕获的共享底层数组
	// 视角可观测清零（通道值副本语义，Hard Rule）。
	callerView := creds
	backing := callerView.Secret // 共享底层数组的调用方引用
	callerView.Zeroize()
	assert.Nil(t, callerView.Secret, "切片头置 nil 释放引用")
	assert.True(t, len(backing) > 0)
	for i, b := range backing {
		assert.Zero(t, b, "byte %d not zeroed", i)
	}

	// String 不含 Secret 明文（防 fmt 打印进日志）。
	rendered := Credential{
		Kind: CredentialKindCloudAK, Cloud: "aliyun", AccountKey: "acc",
		AccessKey: "ak", Secret: []byte("leak-check"), KeyVersion: 3,
	}.String()
	assert.NotContains(t, rendered, "leak-check")
	assert.Contains(t, rendered, "cloud_ak")
	assert.Contains(t, rendered, "keyVersion:3")
}

// AC-4：DeployTarget 按 channel 分支校验必填
// （对齐 schema.sql cert_change_items.resourceRef anyOf 两分支）。
func TestDeployTargetValidate(t *testing.T) {
	validCloud := DeployTarget{
		Channel: "cloud_api", Cloud: "aliyun", Product: "cdn",
		AccountKey: "acc", ResourceID: "example.com",
	}
	assert.NoError(t, validCloud.Validate())

	validK8s := DeployTarget{
		Channel: "k8s_api", ClusterID: "c-1", Namespace: "default",
		Kind: "Certificate", ResourceID: "gw-cert",
	}
	assert.NoError(t, validK8s.Validate())

	cases := []struct {
		name   string
		target DeployTarget
	}{
		{"cloud_api 缺 cloud", DeployTarget{Channel: "cloud_api", Product: "cdn", AccountKey: "a", ResourceID: "r"}},
		{"cloud_api 缺 product", DeployTarget{Channel: "cloud_api", Cloud: "aliyun", AccountKey: "a", ResourceID: "r"}},
		{"cloud_api 缺 accountKey", DeployTarget{Channel: "cloud_api", Cloud: "aliyun", Product: "cdn", ResourceID: "r"}},
		{"k8s_api 缺 clusterId", DeployTarget{Channel: "k8s_api", Namespace: "ns", Kind: "k", ResourceID: "r"}},
		{"k8s_api 缺 namespace", DeployTarget{Channel: "k8s_api", ClusterID: "c", Kind: "k", ResourceID: "r"}},
		{"k8s_api 缺 kind", DeployTarget{Channel: "k8s_api", ClusterID: "c", Namespace: "ns", ResourceID: "r"}},
		{"未知 channel", DeployTarget{Channel: "carrier_pigeon", ResourceID: "r"}},
		{"缺 resourceId", DeployTarget{Channel: "cloud_api", Cloud: "aliyun", Product: "cdn", AccountKey: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.target.Validate(), ErrInvalidTarget)
		})
	}
}

// DeployTarget ↔ domain.ResourceRef 往返转换（5.7 子任务凭持久化
// resourceRef 重构 DeployTarget，不回查台账/快照）。
func TestDeployTargetResourceRefRoundTrip(t *testing.T) {
	target := DeployTarget{
		Channel: "cloud_api", Cloud: "tencent", Product: "clb",
		AccountKey: "acc", ResourceID: "lb-1/listener-1",
	}
	ref := target.ToResourceRef()
	assert.Equal(t, domain.ChannelCloudAPI, ref.Channel)
	assert.Equal(t, "tencent", ref.Cloud)
	assert.Equal(t, "clb", ref.Product)
	assert.Equal(t, "acc", ref.AccountKey)
	assert.Equal(t, "lb-1/listener-1", ref.ResourceID)

	k8s := DeployTarget{
		Channel: "k8s_api", ClusterID: "c", Namespace: "ns",
		Kind: "Certificate", ResourceID: "name",
	}
	assert.Equal(t, k8s, DeployTargetFromResourceRef(k8s.ToResourceRef()))
}

// DiscoverScope：SnapshotID 必填（发现结果必须可归属快照）。
func TestDiscoverScopeValidate(t *testing.T) {
	assert.NoError(t, DiscoverScope{SnapshotID: "snap-1"}.Validate())
	assert.ErrorIs(t, DiscoverScope{}.Validate(), ErrInvalidScope)
}

// Implementation Notes：BatchConf 校验（MaxBatchRatio ≤0.5 硬约束，供 5.7 复用）。
func TestValidateBatchConf(t *testing.T) {
	assert.NoError(t, ValidateBatchConf(BatchConf{Enabled: false}), "单批全量：分批字段零值合法")

	assert.NoError(t, ValidateBatchConf(BatchConf{
		Enabled: true, BatchSize: 10, MaxBatchRatio: 0.5,
	}), "MaxBatchRatio 恰为上限 0.5 合法")
	assert.NoError(t, ValidateBatchConf(BatchConf{
		Enabled: true, BatchSize: 3, MaxBatchRatio: 0.25,
	}))

	assert.ErrorIs(t, ValidateBatchConf(BatchConf{Enabled: true, BatchSize: 0, MaxBatchRatio: 0.5}),
		ErrInvalidBatchConf, "分批时 BatchSize 必填 >0")
	assert.ErrorIs(t, ValidateBatchConf(BatchConf{Enabled: true, BatchSize: 5, MaxBatchRatio: 0}),
		ErrInvalidBatchConf, "MaxBatchRatio 须 >0")
	assert.ErrorIs(t, ValidateBatchConf(BatchConf{Enabled: true, BatchSize: 5, MaxBatchRatio: 0.6}),
		ErrInvalidBatchConf, "MaxBatchRatio 硬上限 0.5")
	assert.ErrorIs(t, ValidateBatchConf(BatchConf{Enabled: true, BatchSize: 5, MaxBatchRatio: -0.1}),
		ErrInvalidBatchConf)
}

// Implementation Notes：有效批大小 = min(BatchSize, floor(total/2))
// （首批硬约束单批 ≤ floor(total/2)，对应 PRD 分批灰度 ≤50%）。
func TestEffectiveBatchSize(t *testing.T) {
	cases := []struct {
		batchSize, total, want int
		reason                 string
	}{
		{10, 100, 10, "BatchSize 低于半量：取 BatchSize"},
		{60, 100, 50, "BatchSize 超半量：截断为 floor(total/2)"},
		{50, 100, 50, "恰等半量"},
		{10, 101, 10, "奇数总量 floor"},
		{40, 81, 40, "奇数总量且 BatchSize 低于 floor"},
		{41, 81, 40, "奇数总量 floor(81/2)=40"},
		{10, 8, 4, "小总量截断"},
		{5, 2, 1, "最小可分批总量"},
		{5, 1, 1, "total<2：单引用无灰度拆分意义，返回 total"},
		{5, 0, 0, "total<2：空清单"},
	}
	for _, tc := range cases {
		got := EffectiveBatchSize(tc.batchSize, tc.total)
		assert.Equal(t, tc.want, got, "%s: EffectiveBatchSize(%d,%d)", tc.reason, tc.batchSize, tc.total)
	}
}

// RollbackResult 错误码常量对齐 tech-design RollbackResult.ErrCode 取值。
func TestRollbackErrCodeConstants(t *testing.T) {
	assert.Equal(t, "CLOUD_API_RATELIMITED", ErrCodeCloudRateLimited)
	assert.Equal(t, "K8S_UNREACHABLE", ErrCodeK8sUnreachable)
	assert.Equal(t, "ROLLBACK_TARGET_INVALID", ErrCodeRollbackTargetInvalid)
	assert.True(t, strings.HasPrefix(ErrInvalidCredential.Error(), "deployer:"))
}
