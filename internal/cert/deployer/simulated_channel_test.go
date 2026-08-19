package deployer

import (
	"context"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/stretchr/testify/assert"
)

// AC-5：SimulatedChannel 全路径——Discover/Deploy/Rollback × 成功/失败/限流。
func TestSimulatedChannelAllPaths(t *testing.T) {
	ctx := context.Background()
	creds := Credential{
		Kind: CredentialKindCloudAK, Cloud: "aliyun", AccountKey: "acc",
		AccessKey: "ak", Secret: []byte("sim-sk"), KeyVersion: 1,
	}
	target := DeployTarget{Channel: "cloud_api", Cloud: "aliyun", Product: "cdn", AccountKey: "acc", ResourceID: "www.example.com"}
	oldRef := domain.CertReference{ResourceID: "www.example.com", ReferencedCloudCertID: "old-1"}

	t.Run("Type 可配置（bastion/agent 二期通道扮演）", func(t *testing.T) {
		assert.Equal(t, ChannelTypeBastion, NewSimulatedChannel(ChannelTypeBastion).Type())
		assert.Equal(t, ChannelTypeAgent, NewSimulatedChannel(ChannelTypeAgent).Type())
		assert.Equal(t, ChannelTypeCloudAPI, NewSimulatedChannel(ChannelTypeCloudAPI).Type())
	})

	t.Run("Deploy 成功：递增模拟���证书 ID", func(t *testing.T) {
		ch := NewSimulatedChannel(ChannelTypeBastion)
		r1, err := ch.Deploy(ctx, creds, target, strings.Repeat("a", 64))
		assert.NoError(t, err)
		r2, err := ch.Deploy(ctx, creds, target, strings.Repeat("b", 64))
		assert.NoError(t, err)
		assert.Equal(t, "sim-cert-1", r1.NewCloudCertID)
		assert.Equal(t, "sim-cert-2", r2.NewCloudCertID)
	})

	t.Run("Deploy 失败与限流", func(t *testing.T) {
		fail := NewSimulatedChannel(ChannelTypeBastion).SetDeployOutcome(SimulatedFailure)
		_, err := fail.Deploy(ctx, creds, target, strings.Repeat("a", 64))
		assert.ErrorContains(t, err, "simulated channel failure")

		rl := NewSimulatedChannel(ChannelTypeBastion).SetDeployOutcome(SimulatedRateLimit)
		_, err = rl.Deploy(ctx, creds, target, strings.Repeat("a", 64))
		assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "限流哨兵在错误链中（rate_limited 状态承接验证）")
	})

	t.Run("Rollback 成功/失败/限流", func(t *testing.T) {
		ok := NewSimulatedChannel(ChannelTypeAgent)
		res, err := ok.Rollback(ctx, creds, target, oldRef)
		assert.NoError(t, err)
		assert.True(t, res.Success)
		assert.Equal(t, oldRef, res.RestoredRef)

		fail := NewSimulatedChannel(ChannelTypeAgent).SetRollbackOutcome(SimulatedFailure)
		res, err = fail.Rollback(ctx, creds, target, oldRef)
		assert.Error(t, err)
		assert.False(t, res.Success)
		assert.Empty(t, res.ErrCode)

		rl := NewSimulatedChannel(ChannelTypeAgent).SetRollbackOutcome(SimulatedRateLimit)
		res, err = rl.Rollback(ctx, creds, target, oldRef)
		assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
		assert.Equal(t, ErrCodeCloudRateLimited, res.ErrCode)
	})

	t.Run("Discover 成功/失败/限流与 snapshotId 回写", func(t *testing.T) {
		tpl := domain.CertReference{
			ResourceID: "www.example.com", ReferencedCloudCertID: "c-1",
			CertFingerprint: strings.Repeat("f", 64),
		}
		ok := NewSimulatedChannel(ChannelTypeBastion).SetDiscoverRef(tpl)
		refs, err := ok.Discover(ctx, creds, DiscoverScope{SnapshotID: "snap-1"})
		assert.NoError(t, err)
		assert.Len(t, refs, 1)
		assert.Equal(t, "snap-1", refs[0].SnapshotID, "scope.SnapshotID 回写契约")
		assert.Equal(t, domain.CloudAliyun, refs[0].Cloud, "凭证归属云回写")

		empty := NewSimulatedChannel(ChannelTypeBastion) // 未配置模板 → 空发现
		refs, err = empty.Discover(ctx, creds, DiscoverScope{SnapshotID: "snap-1"})
		assert.NoError(t, err)
		assert.Empty(t, refs)

		fail := NewSimulatedChannel(ChannelTypeBastion).SetDiscoverOutcome(SimulatedFailure)
		_, err = fail.Discover(ctx, creds, DiscoverScope{SnapshotID: "snap-1"})
		assert.ErrorContains(t, err, "simulated channel failure")

		rl := NewSimulatedChannel(ChannelTypeBastion).SetDiscoverOutcome(SimulatedRateLimit)
		_, err = rl.Discover(ctx, creds, DiscoverScope{SnapshotID: "snap-1"})
		assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	})

	t.Run("调用记录与明文清零", func(t *testing.T) {
		ch := NewSimulatedChannel(ChannelTypeBastion)
		localCreds := creds
		_, err := ch.Deploy(ctx, localCreds, target, strings.Repeat("a", 64))
		assert.NoError(t, err)
		for i, b := range localCreds.Secret {
			assert.Zero(t, b, "模拟通道同样遵守明文用后清零契约 byte %d", i)
		}
		_, err = ch.Rollback(ctx, localCreds, target, oldRef)
		assert.NoError(t, err)
		_, err = ch.Discover(ctx, localCreds, DiscoverScope{SnapshotID: "s"})
		assert.NoError(t, err)

		calls := ch.Calls()
		assert.Len(t, calls, 3)
		assert.Equal(t, "Deploy", calls[0].Method)
		assert.Equal(t, "cdn", calls[0].Product)
		assert.Equal(t, "www.example.com", calls[0].ResourceID)
		assert.Equal(t, strings.Repeat("a", 64), calls[0].CertForDeploy)
		assert.Equal(t, "Rollback", calls[1].Method)
		assert.Equal(t, "Discover", calls[2].Method)
	})
}

// AC-5 可插拔性演示：消费方（5.7 执行引擎形态）仅按 Type()/target.Channel 路由，
// 云 API 通道与模拟堡垒机/Agent 通道零上层改动互换。
func TestExecutionChannelPluggability(t *testing.T) {
	// 消费方：与具体通道实现解耦的路由器（模拟 5.7 引擎装配形态，二期内零改动）。
	routeAndDeploy := func(
		ctx context.Context,
		channels map[ChannelType]ExecutionChannel,
		creds Credential,
		target DeployTarget,
		fingerprint string,
	) (DeployResult, error) {
		ch, ok := channels[ChannelType(target.Channel)]
		if !ok {
			return DeployResult{}, ErrDeployerNotRegistered
		}
		return ch.Deploy(ctx, creds, target, fingerprint)
	}

	ctx := context.Background()
	creds := testCloudCreds()
	cloudTarget := testCloudTarget()
	mappings := certtest.NewFakeCloudCertMappingRepo()
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	cloudAPI := NewCloudAPIChannel(mappings, mat, &fakeOldRefs{})
	assert.NoError(t, cloudAPI.RegisterDeployer("aliyun", &fakeCloudDeployer{uploadID: "cloud-1"}, "cdn"))

	// 一期装配：cloud_api 通道。
	firstWave := map[ChannelType]ExecutionChannel{ChannelTypeCloudAPI: cloudAPI}
	res, err := routeAndDeploy(ctx, firstWave, creds, cloudTarget, testFingerprint)
	assert.NoError(t, err)
	assert.Equal(t, "cloud-1", res.NewCloudCertID)

	// 二期接入：同一消费方零改动，仅装配表新增 bastion/agent 模拟通道。
	secondWave := map[ChannelType]ExecutionChannel{
		ChannelTypeCloudAPI: cloudAPI,
		ChannelTypeBastion:  NewSimulatedChannel(ChannelTypeBastion),
		ChannelTypeAgent:    NewSimulatedChannel(ChannelTypeAgent).SetDeployOutcome(SimulatedRateLimit),
	}
	bastionTarget := DeployTarget{Channel: "bastion", ResourceID: "vm-nginx-1"}
	res, err = routeAndDeploy(ctx, secondWave, creds, bastionTarget, testFingerprint)
	assert.NoError(t, err)
	assert.Equal(t, "sim-cert-1", res.NewCloudCertID, "堡垒机通道经同一消费方零改动执行")

	agentTarget := DeployTarget{Channel: "agent", ResourceID: "host-1"}
	_, err = routeAndDeploy(ctx, secondWave, creds, agentTarget, testFingerprint)
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "Agent 通道限流语义与其他通道一致")
}
