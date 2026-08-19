package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------------------------------------------------------------------
// 测试基建（内存假实现 + 脚本化通道 + 同步派发器）
// ---------------------------------------------------------------------

const (
	execTestOldFP = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22"
	execTestNewFP = "cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33"
)

// scriptedOutcome 单次 Deploy 尝试的脚本化行为。
type scriptedOutcome int

const (
	scriptSuccess scriptedOutcome = iota
	scriptFailure
	scriptRateLimit
	scriptPanic
	scriptBlock // 阻塞直到 release 关闭（心跳观测用）
)

// scriptedChannel 脚本化执行通道：按尝试次序回放行为（脚本耗尽沿用末值），
// 记录每次 Deploy 收到的 target/newCertFingerprint（子任务凭持久化
// resourceRef 重构 DeployTarget 的断言依据）。
type scriptedChannel struct {
	mu             sync.Mutex
	typ            deployer.ChannelType
	script         []scriptedOutcome
	calls          int
	targets        []deployer.DeployTarget
	fps            []string
	release        chan struct{}
	entered        chan struct{} // scriptBlock 进入信号（缓冲 1）
	k8sUnreachable bool          // scriptFailure 携带 ErrK8sUnreachable 哨兵
}

func newScriptedChannel(typ deployer.ChannelType, script ...scriptedOutcome) *scriptedChannel {
	return &scriptedChannel{
		typ: typ, script: script,
		release: make(chan struct{}),
		entered: make(chan struct{}, 1),
	}
}

func (c *scriptedChannel) Type() deployer.ChannelType { return c.typ }

func (c *scriptedChannel) Discover(_ context.Context, _ deployer.Credential, _ deployer.DiscoverScope) ([]domain.CertReference, error) {
	return nil, nil
}

func (c *scriptedChannel) Rollback(_ context.Context, _ deployer.Credential, _ deployer.DeployTarget, _ domain.CertReference) (deployer.RollbackResult, error) {
	return deployer.RollbackResult{}, nil
}

func (c *scriptedChannel) Deploy(_ context.Context, _ deployer.Credential, target deployer.DeployTarget, newCertFingerprint string) (deployer.DeployResult, error) {
	c.mu.Lock()
	c.calls++
	idx := c.calls - 1
	if idx >= len(c.script) {
		idx = len(c.script) - 1
	}
	outcome := scriptSuccess
	if len(c.script) > 0 {
		outcome = c.script[idx]
	}
	k8sUnreachable := c.k8sUnreachable
	c.targets = append(c.targets, target)
	c.fps = append(c.fps, newCertFingerprint)
	c.mu.Unlock()
	switch outcome {
	case scriptFailure:
		if k8sUnreachable {
			return deployer.DeployResult{}, fmt.Errorf("scripted k8s unreachable: %w", domain.ErrK8sUnreachable)
		}
		return deployer.DeployResult{}, errors.New("scripted deploy failure")
	case scriptRateLimit:
		return deployer.DeployResult{}, fmt.Errorf("scripted rate limited: %w", cloudx.ErrCloudRateLimited)
	case scriptPanic:
		panic("scripted deploy panic")
	case scriptBlock:
		select { // 进入信号（缓冲 1，无观察者不阻塞）
		case c.entered <- struct{}{}:
		default:
		}
		<-c.release
		return deployer.DeployResult{NewCloudCertID: "cloud-cert-blocked"}, nil
	default:
		return deployer.DeployResult{NewCloudCertID: fmt.Sprintf("cloud-cert-%d", c.calls)}, nil
	}
}

func (c *scriptedChannel) attempts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *scriptedChannel) recordedTargets() []deployer.DeployTarget {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]deployer.DeployTarget(nil), c.targets...)
}

// syncDispatcher 同步派发器：内联执行（ExecuteItem）。
type syncDispatcher struct {
	runner *changeExecuteService
}

func (d *syncDispatcher) DispatchItem(ctx context.Context, orderID, itemID string) error {
	return d.runner.ExecuteItem(ctx, orderID, itemID)
}

// fakeBatchVerify 批级验证判定假实现（5.10 端口注入缝）。
type fakeBatchVerify struct {
	verified bool
	reason   string
	err      error
	calls    int
}

func (f *fakeBatchVerify) BatchVerified(_ context.Context, _ domain.ChangeOrder) (bool, string, error) {
	f.calls++
	return f.verified, f.reason, f.err
}

// fakeExecuteNotifier 超时通知假实现。
type fakeExecuteNotifier struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeExecuteNotifier) NotifyItemTimedOut(_ context.Context, orderID, itemID string, _, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, orderID+"/"+itemID)
	return nil
}

// fakeCredentialSource 固定凭证假实现（形态合法即可通过通道校验）。
type fakeCredentialSource struct{}

func (fakeCredentialSource) CloudCredential(_ context.Context, cloud, accountKey string) (deployer.Credential, error) {
	return deployer.Credential{
		Kind: deployer.CredentialKindCloudAK, Cloud: cloud, AccountKey: accountKey,
		AccessKey: "ak", Secret: []byte("sk"), KeyVersion: 1,
	}, nil
}

func (fakeCredentialSource) K8sCredential(_ context.Context, _ string) (deployer.Credential, error) {
	return deployer.Credential{
		Kind: deployer.CredentialKindKubeconfig, Secret: []byte("kubeconfig"), KeyVersion: 1,
	}, nil
}

// executeHarness 执行引擎测试依赖聚合。
type executeHarness struct {
	svc      *changeExecuteService
	orders   *certtest.FakeChangeOrderRepo
	items    *certtest.FakeChangeItemRepo
	certs    *certtest.FakeCertificateRepo
	alertCfg *certtest.FakeAlertConfigRepo
	snaps    *certtest.FakeScanSnapshotRepo
	refs     *certtest.FakeCertReferenceRepo
	channel  *scriptedChannel
	verify   *fakeBatchVerify
	notifier *fakeExecuteNotifier
	now      func() time.Time
	certID   string
	sleepLog []time.Duration
	sleepMu  sync.Mutex
}

func newExecuteHarness(t *testing.T, script ...scriptedOutcome) *executeHarness {
	t.Helper()
	h := &executeHarness{
		orders:   certtest.NewFakeChangeOrderRepo(),
		items:    certtest.NewFakeChangeItemRepo(),
		certs:    certtest.NewFakeCertificateRepo(),
		alertCfg: certtest.NewFakeAlertConfigRepo(),
		snaps:    certtest.NewFakeScanSnapshotRepo(),
		refs:     certtest.NewFakeCertReferenceRepo(),
		channel:  newScriptedChannel(deployer.ChannelTypeCloudAPI, script...),
		verify:   &fakeBatchVerify{verified: true},
		notifier: &fakeExecuteNotifier{},
	}
	h.now = time.Now
	svc := NewChangeExecuteService(
		h.orders, h.items, h.certs, h.alertCfg, h.snaps, h.refs,
		[]deployer.ExecutionChannel{h.channel},
		fakeCredentialSource{},
		nil, // dispatch 由用例注入（h.bindDispatch）
		h.verify,
		nil, // sealer：5.10 固化缝由 verify_window_service_test 以真实现注入
		h.notifier,
		nil, // audit：item_result 审计由专项用例注入
	).(*changeExecuteService)
	h.svc = svc
	// 测试默认：毫秒级心跳 + 零等待退避（不拖慢用例；策略语义由专项用例覆盖）
	svc.heartbeatInterval = 2 * time.Millisecond
	svc.rateLimit = ItemRateLimitPolicy{MaxAttempts: 3, Backoffs: []time.Duration{time.Millisecond, time.Millisecond}, MaxTotalWait: 10 * time.Millisecond}
	svc.sleep = func(ctx context.Context, d time.Duration) error {
		h.sleepMu.Lock()
		h.sleepLog = append(h.sleepLog, d)
		h.sleepMu.Unlock()
		return sleepWithContext(ctx, time.Millisecond)
	}
	return h
}

// bindDispatch 绑定同步派发器（种子就绪后调用）。
func (h *executeHarness) bindDispatch() {
	h.svc.dispatch = &syncDispatcher{runner: h.svc}
}

// seedDoneSnapshot 写入指定时龄的成功快照，返回快照 ID。
func (h *executeHarness) seedDoneSnapshot(t *testing.T, age time.Duration) string {
	t.Helper()
	snap := &domain.ScanSnapshot{Status: domain.ScanStatusDone, StartedAt: h.now().Add(-age)}
	id, err := h.snaps.Create(context.Background(), snap)
	require.NoError(t, err)
	return id
}

// seedRefs 快照内写入旧证书指纹引用（Confirm 引用一致性数据源）。
func (h *executeHarness) seedRefs(t *testing.T, snapID string, resourceIDs ...string) {
	t.Helper()
	refs := make([]domain.CertReference, 0, len(resourceIDs))
	for _, rid := range resourceIDs {
		refs = append(refs, domain.CertReference{
			CertFingerprint:       execTestOldFP,
			Cloud:                 domain.CloudAliyun,
			Product:               domain.ProductCDN,
			ResourceID:            rid,
			ReferencedCloudCertID: "old-cert-" + rid,
			AccountKey:            "acct-main",
			SnapshotID:            snapID,
		})
	}
	_, err := h.refs.CreateMulti(context.Background(), refs)
	require.NoError(t, err)
}

// refIDs 生成 res-%02d 序列（清单项 resourceId 与快照引用同键）。
func refIDs(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("res-%02d", i))
	}
	return out
}

// seedPendingOrder 写入 pending_confirm 订单 + 待执行项（每项一个云引用），
// 返回订单 ID。skipStatuses 指定各项初始状态（长度可小于项数，缺省 pending）。
func (h *executeHarness) seedPendingOrder(t *testing.T, snapID string, statuses ...domain.ChangeItemStatus) string {
	t.Helper()
	order := &domain.ChangeOrder{
		OldCertFingerprint: execTestOldFP,
		NewCertID:          h.seedNewCert(t),
		Status:             domain.ChangeStatusPendingConfirm,
		SnapshotID:         snapID,
		ActiveMutex:        execTestOldFP,
		Creator:            "operator",
	}
	orderID, err := h.orders.Create(context.Background(), order)
	require.NoError(t, err)
	n := len(statuses)
	if n == 0 {
		n = 1
	}
	// 引用一致性数据源：快照内该指纹的去重引用与清单项一一对应
	h.seedRefs(t, snapID, refIDs(n)...)
	items := make([]domain.ChangeItem, 0, n)
	for i := 0; i < n; i++ {
		status := domain.ItemStatusPending
		if i < len(statuses) {
			status = statuses[i]
		}
		items = append(items, domain.ChangeItem{
			OrderID: orderID,
			Action:  domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
				AccountKey: "acct-main", ResourceID: fmt.Sprintf("res-%02d", i),
			},
			OldCloudCertID: fmt.Sprintf("old-cert-res-%02d", i),
			Status:         status,
		})
	}
	_, err = h.items.CreateMulti(context.Background(), items)
	require.NoError(t, err)
	return orderID
}

// seedNewCert 写入新证书（complete，含指纹），返回证书 ID（幂等：同 harness
// 多次种子只写一次）。
func (h *executeHarness) seedNewCert(t *testing.T) string {
	t.Helper()
	if h.certID != "" {
		return h.certID
	}
	cert := &domain.Certificate{
		ID:            primitive.NewObjectID(),
		Fingerprint:   execTestNewFP,
		CommonName:    "new-cert.example.com",
		HostingStatus: domain.HostingStatusComplete,
		CertPEM:       "-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----",
	}
	require.NoError(t, h.certs.Create(context.Background(), cert))
	h.certID = cert.ID.Hex()
	return h.certID
}

// seedExecutingOrder 写入 executing 订单（可指定批次信息）与若干项。
type seedItem struct {
	batch  int
	status domain.ChangeItemStatus
	err    string // skipped 项判定依据（生成期 skipped 携带 Reason）
	ref    domain.ResourceRef
}

func execCloudRef(resourceID string) domain.ResourceRef {
	return domain.ResourceRef{
		Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
		AccountKey: "acct-main", ResourceID: resourceID,
	}
}

func (h *executeHarness) seedExecutingOrder(t *testing.T, batch *domain.BatchInfo, seedItems ...seedItem) string {
	t.Helper()
	order := &domain.ChangeOrder{
		OldCertFingerprint: execTestOldFP,
		NewCertID:          h.seedNewCert(t),
		Status:             domain.ChangeStatusExecuting,
		SnapshotID:         h.seedDoneSnapshot(t, time.Hour),
		ActiveMutex:        execTestOldFP,
		BatchInfo:          batch,
		Creator:            "operator",
	}
	orderID, err := h.orders.Create(context.Background(), order)
	require.NoError(t, err)
	items := make([]domain.ChangeItem, 0, len(seedItems))
	for _, si := range seedItems {
		ref := si.ref
		if ref.ResourceID == "" {
			ref = execCloudRef(fmt.Sprintf("res-%02d", len(items)))
		}
		items = append(items, domain.ChangeItem{
			OrderID:     orderID,
			BatchNo:     si.batch,
			Action:      domain.ActionUploadAndBind,
			ResourceRef: ref,
			Status:      si.status,
			Error:       si.err,
		})
	}
	_, err = h.items.CreateMulti(context.Background(), items)
	require.NoError(t, err)
	return orderID
}

// ---------------------------------------------------------------------
// AC-1：Confirm 批次分配（floor(total/2) 上限、字典序、余量均分、门控）
// ---------------------------------------------------------------------

// TestConfirm_BatchAllocation 批次分配核心算法（表驱动）：
// 首批 = min(BatchSize, floor(total/2))；余下按 BatchSize 均分（末批可不足额）；
// batchInfo{totalBatches, currentBatch=1, batchSize=有效批大小}。
func TestConfirm_BatchAllocation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name        string
		total       int
		batchSize   int
		wantFirst   int   // 首批项数（min(BatchSize, floor(total/2))）
		wantBatches []int // 各批项数
		wantSize    int   // batchInfo.batchSize（有效批大小）
	}{
		{"余量均分末批不足额", 10, 4, 4, []int{4, 4, 2}, 4},
		{"首批触 floor(total/2) 上限", 10, 8, 5, []int{5, 5}, 5},
		{"奇数总量 floor 取整", 7, 2, 2, []int{2, 2, 2, 1}, 2},
		{"微小清单退化为单批", 1, 8, 1, []int{1}, 1},
		{"两项各半", 2, 1, 1, []int{1, 1}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newExecuteHarness(t)
			snapID := h.seedDoneSnapshot(t, time.Hour)
			statuses := make([]domain.ChangeItemStatus, c.total)
			orderID := h.seedPendingOrder(t, snapID, statuses...)

			require.NoError(t, h.svc.Confirm(ctx, orderID, deployer.BatchConf{
				Enabled: true, BatchSize: c.batchSize, MaxBatchRatio: 0.5,
			}))

			order, err := h.orders.GetByID(ctx, orderID)
			require.NoError(t, err)
			require.NotNil(t, order.BatchInfo)
			assert.Equal(t, len(c.wantBatches), order.BatchInfo.TotalBatches)
			assert.Equal(t, 1, order.BatchInfo.CurrentBatch)
			assert.Equal(t, c.wantSize, order.BatchInfo.BatchSize)
			assert.False(t, order.BatchInfo.Paused)

			got, err := h.items.ListByOrder(ctx, orderID)
			require.NoError(t, err)
			byBatch := map[int]int{}
			for _, it := range got {
				byBatch[it.BatchNo]++
			}
			for batchNo, want := range c.wantBatches {
				assert.Equal(t, want, byBatch[batchNo+1], "batch %d size", batchNo+1)
			}
		})
	}
}

// TestConfirm_StableLexicographicOrdering items 按 (cloud, product, resourceId)
// 字典序稳定排序切批：首批为字典序最小项。
func TestConfirm_StableLexicographicOrdering(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	snapID := h.seedDoneSnapshot(t, time.Hour)
	// 乱序写入 resourceId（res-10 字典序小于 res-2）
	orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 5)...)

	require.NoError(t, h.svc.Confirm(ctx, orderID, deployer.BatchConf{
		Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5, // floor(5/2)=2 → 首批 2 项
	}))

	got, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	first := []string{}
	for _, it := range got {
		if it.BatchNo == 1 {
			first = append(first, it.ResourceRef.ResourceID)
		}
	}
	assert.Equal(t, []string{"res-00", "res-01"}, first) // 字典序最小两项入首批
}

// TestConfirm_HardRules Hard Rule 门控：单批 ≤ floor(total/2)——单批全量
// 仅 total≤1 允许；MaxBatchRatio 范围与交叉校验。
func TestConfirm_HardRules(t *testing.T) {
	ctx := context.Background()
	t.Run("单批全量 total>=2 拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, time.Hour)
		orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 3)...)
		err := h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: false})
		require.Error(t, err)
		assert.ErrorIs(t, err, deployer.ErrInvalidBatchConf)
	})
	t.Run("单批全量单项允许", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, time.Hour)
		orderID := h.seedPendingOrder(t, snapID)
		require.NoError(t, h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: false}))
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, 1, order.BatchInfo.TotalBatches)
	})
	t.Run("maxBatchRatio 越界拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, time.Hour)
		orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 4)...)
		err := h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.7})
		require.Error(t, err)
		assert.ErrorIs(t, err, deployer.ErrInvalidBatchConf)
	})
	t.Run("有效批大小超比例上限拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, time.Hour)
		orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 10)...)
		// 有效批大小 = min(6, floor(10/2)) = 5 → 5/10=0.5 > maxBatchRatio=0.3
		err := h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 6, MaxBatchRatio: 0.3})
		require.Error(t, err)
		assert.ErrorIs(t, err, deployer.ErrInvalidBatchConf)
	})
}

// TestConfirm_SnapshotRevalidation 快照确认时点重校验：新鲜度与引用一致性。
func TestConfirm_SnapshotRevalidation(t *testing.T) {
	ctx := context.Background()
	t.Run("快照超新鲜度阈值拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, 25*time.Hour) // 阈值 24h
		orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 4)...)
		err := h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5})
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrScanStale)
	})
	t.Run("快照缺失拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		missingSnap := primitive.NewObjectID().Hex()
		orderID := h.seedPendingOrder(t, missingSnap, make([]domain.ChangeItemStatus, 4)...)
		err := h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5})
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrScanStale)
	})
	t.Run("引用不一致拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, time.Hour)
		orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 4)...)
		_, err := h.refs.DeleteBySnapshotID(context.Background(), snapID)
		require.NoError(t, err)
		h.seedRefs(t, snapID, "res-00", "res-01", "res-02") // 快照仅 3 引用 vs 4 清单项
		err = h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5})
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrScanStale)
	})
	t.Run("一致时通过", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, time.Hour)
		h.seedRefs(t, snapID, "res-00", "res-01", "res-02", "res-03")
		orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 4)...)
		require.NoError(t, h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5}))
	})
}

// TestConfirm_StateGates 状态与一次性固化门控。
func TestConfirm_StateGates(t *testing.T) {
	ctx := context.Background()
	t.Run("非 pending_confirm 拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
		err := h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 1, MaxBatchRatio: 0.5})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pending_confirm")
	})
	t.Run("重复确认拒绝（一次性固化）", func(t *testing.T) {
		h := newExecuteHarness(t)
		snapID := h.seedDoneSnapshot(t, time.Hour)
		h.seedRefs(t, snapID, "res-00", "res-01", "res-02", "res-03")
		orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 4)...)
		require.NoError(t, h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5}))
		err := h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already fixed")
	})
}

// ---------------------------------------------------------------------
// AC-2：Execute 派发（仅当前批、resourceRef 重构、逐项隔离）
// ---------------------------------------------------------------------

// TestExecute_DispatchesOnlyCurrentBatch 派发仅取 batchNo=currentBatch 的
// pending 项；不可执行项（skipped）与后续批项不派发。
func TestExecute_DispatchesOnlyCurrentBatch(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	batch := &domain.BatchInfo{TotalBatches: 3, CurrentBatch: 2, BatchSize: 1}
	orderID := h.seedExecutingOrder(t, batch,
		seedItem{batch: 1, status: domain.ItemStatusSuccess},
		seedItem{batch: 2, status: domain.ItemStatusPending},
		seedItem{batch: 2, status: domain.ItemStatusPending},
		seedItem{batch: 2, status: domain.ItemStatusSkipped, err: "ERR_DISCOVERY_ONLY: fixture"},
		seedItem{batch: 3, status: domain.ItemStatusPending},
	)
	var dispatched []string
	h.svc.dispatch = newRecordDispatcher(&dispatched)

	require.NoError(t, h.svc.Execute(ctx, orderID))

	assert.Equal(t, 2, len(dispatched)) // 仅批 2 的两个 pending
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	for _, it := range items {
		if it.BatchNo == 3 {
			assert.Equal(t, domain.ItemStatusPending, it.Status, "后续批项不被触碰")
		}
	}
}

// recordDispatcher 仅记录不执行的派发器。
type recordDispatcher struct{ dispatched *[]string }

func newRecordDispatcher(dispatched *[]string) recordDispatcher {
	return recordDispatcher{dispatched: dispatched}
}

func (d recordDispatcher) DispatchItem(_ context.Context, _, itemID string) error {
	*d.dispatched = append(*d.dispatched, itemID)
	return nil
}

// TestExecute_FirstExecutionTransitionsToExecuting 首批执行：
// pending_confirm→executing（activeMutex 保持）。
func TestExecute_FirstExecutionTransitionsToExecuting(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	snapID := h.seedDoneSnapshot(t, time.Hour)
	h.seedRefs(t, snapID, "res-00", "res-01", "res-02", "res-03")
	orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 4)...)
	require.NoError(t, h.svc.Confirm(ctx, orderID, deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5}))

	var dispatched []string
	h.svc.dispatch = newRecordDispatcher(&dispatched)

	require.NoError(t, h.svc.Execute(ctx, orderID))
	assert.Equal(t, 2, len(dispatched)) // 首批 2 项

	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status)
	assert.Equal(t, execTestOldFP, order.ActiveMutex)
}

// TestExecute_PausedOrderRejected 批间暂停：分批一律人工续批，
// 未 ConfirmBatch 放行前 Execute 拒绝（409 BATCH_NOT_CONFIRMABLE）。
func TestExecute_PausedOrderRejected(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	pausedAt := h.now().Add(-time.Minute)
	batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 2, Paused: true, PausedAt: &pausedAt}
	orderID := h.seedExecutingOrder(t, batch,
		seedItem{batch: 1, status: domain.ItemStatusSuccess},
		seedItem{batch: 2, status: domain.ItemStatusPending},
	)
	err := h.svc.Execute(ctx, orderID)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrBatchNotConfirmable)
}

// TestExecute_RequiresConfirmFirst 未固化批次分配的订单不可执行。
func TestExecute_RequiresConfirmFirst(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	snapID := h.seedDoneSnapshot(t, time.Hour)
	orderID := h.seedPendingOrder(t, snapID, make([]domain.ChangeItemStatus, 4)...)
	err := h.svc.Execute(ctx, orderID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not confirmed")
}

// TestExecute_InvalidStates 非 executing/pending_confirm 状态拒绝。
func TestExecute_InvalidStates(t *testing.T) {
	ctx := context.Background()
	for _, status := range []domain.ChangeStatus{domain.ChangeStatusVerifying, domain.ChangeStatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			h := newExecuteHarness(t)
			order := &domain.ChangeOrder{
				OldCertFingerprint: execTestOldFP, NewCertID: h.seedNewCert(t),
				Status: status, SnapshotID: h.seedDoneSnapshot(t, time.Hour), ActiveMutex: execTestOldFP,
				BatchInfo: &domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 1},
			}
			orderID, err := h.orders.Create(ctx, order)
			require.NoError(t, err)
			var dispatched []string
			h.svc.dispatch = newRecordDispatcher(&dispatched)
			err = h.svc.Execute(ctx, orderID)
			require.Error(t, err)
			var ite *domain.InvalidTransitionError
			assert.ErrorAs(t, err, &ite)
			assert.Empty(t, dispatched)
		})
	}
}

// TestExecuteItem_RebuildsTargetFromPersistedRef 子任务仅凭持久化
// resourceRef 重构 DeployTarget（不回查台账/快照）：通道收到的 target
// 字段与持久化 ref 一一对应。
func TestExecuteItem_RebuildsTargetFromPersistedRef(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptSuccess)
	ref := domain.ResourceRef{
		Channel: domain.ChannelCloudAPI, Cloud: "tencent", Product: "waf",
		AccountKey: "acct-waf", ResourceID: "waf-12345",
	}
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending, ref: ref})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	h.bindDispatch()

	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, items[0].ID.Hex()))

	targets := h.channel.recordedTargets()
	require.Len(t, targets, 1)
	assert.Equal(t, "cloud_api", targets[0].Channel)
	assert.Equal(t, "tencent", targets[0].Cloud)
	assert.Equal(t, "waf", targets[0].Product)
	assert.Equal(t, "acct-waf", targets[0].AccountKey)
	assert.Equal(t, "waf-12345", targets[0].ResourceID)
	assert.Equal(t, []string{execTestNewFP}, h.channel.fps) // 部署目标 = 新证书指纹

	got, err := h.items.GetByID(ctx, items[0].ID.Hex())
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, got.Status)
	assert.Equal(t, "cloud-cert-1", got.NewCloudCertID) // 两段式产物落项
}

// TestExecute_PartialFailureIsolation 逐项隔离：单项失败不阻塞其他项；
// 失败项落项级 failed + 错误详情，成功项正常收敛。
func TestExecute_PartialFailureIsolation(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptFailure, scriptSuccess) // 第 1 项失败、第 2 项成功
	batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 1}
	orderID := h.seedExecutingOrder(t, batch,
		seedItem{batch: 1, status: domain.ItemStatusPending},
		seedItem{batch: 1, status: domain.ItemStatusPending},
		seedItem{batch: 2, status: domain.ItemStatusPending},
	)
	h.bindDispatch()

	require.NoError(t, h.svc.Execute(ctx, orderID))

	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	byID := map[string]domain.ChangeItem{}
	for _, it := range items {
		byID[it.ResourceRef.ResourceID] = it
	}
	assert.Equal(t, domain.ItemStatusFailed, byID["res-00"].Status)
	assert.Contains(t, byID["res-00"].Error, "EXEC_FAILED")
	assert.Equal(t, domain.ItemStatusSuccess, byID["res-01"].Status)
	assert.Equal(t, domain.ItemStatusPending, byID["res-02"].Status, "后续批项不执行")

	// 当前批收敛 → 进入验证窗口 + 批间暂停（activeMutex 全程持有）
	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status)
	require.NotNil(t, order.BatchInfo)
	assert.True(t, order.BatchInfo.Paused)
	require.NotNil(t, order.BatchInfo.PausedAt)
	require.NotNil(t, order.VerifyWindowUntil)
	assert.Equal(t, execTestOldFP, order.ActiveMutex)
}

// TestExecute_ItemPanicRecovered 单项 panic recover 并落项级 failed。
func TestExecute_ItemPanicRecovered(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptPanic, scriptSuccess)
	orderID := h.seedExecutingOrder(t, nil,
		seedItem{batch: 1, status: domain.ItemStatusPending},
		seedItem{batch: 1, status: domain.ItemStatusPending},
	)
	h.bindDispatch()

	require.NoError(t, h.svc.Execute(ctx, orderID)) // panic 不外溢

	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	byRes := map[string]domain.ChangeItem{}
	for _, it := range items {
		byRes[it.ResourceRef.ResourceID] = it
	}
	assert.Equal(t, domain.ItemStatusFailed, byRes["res-00"].Status)
	assert.Contains(t, byRes["res-00"].Error, itemErrExecPanic)
	assert.Equal(t, domain.ItemStatusSuccess, byRes["res-01"].Status, "panic 项不阻塞其他项")
}

// TestExecuteItem_IdempotentOnRedelivery 任务框架重投递幂等：
// 已终态项仅触发重算，不重复执行。
func TestExecuteItem_IdempotentOnRedelivery(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptSuccess)
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	itemID := items[0].ID.Hex()

	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, itemID))
	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, itemID)) // 重投递

	assert.Equal(t, 1, h.channel.attempts())
	got, err := h.items.GetByID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, got.Status)
}

// ---------------------------------------------------------------------
// AC-3：心跳 + executing-timeout 恢复
// ---------------------------------------------------------------------

// TestExecuteItem_HeartbeatUpdatesWhileRunning 执行期心跳：running 期间
// heartbeatAt 持续刷新（默认 30s 间隔，测试注入 2ms）。
func TestExecuteItem_HeartbeatUpdatesWhileRunning(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptBlock)
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	itemID := items[0].ID.Hex()

	done := make(chan error, 1)
	go func() { done <- h.svc.ExecuteItem(ctx, orderID, itemID) }()
	<-h.channel.entered // Deploy 已进入阻塞

	hb1 := heartbeatOf(t, h, itemID)
	require.NotNil(t, hb1, "领取即写心跳基准")
	time.Sleep(30 * time.Millisecond)
	hb2 := heartbeatOf(t, h, itemID)
	require.NotNil(t, hb2)
	assert.True(t, hb2.After(*hb1), "执行期心跳应持续刷新")

	close(h.channel.release)
	require.NoError(t, <-done)
}

func heartbeatOf(t *testing.T, h *executeHarness, itemID string) *time.Time {
	t.Helper()
	got, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	return got.HeartbeatAt
}

// TestRecoverTimedOutItems 心跳超时恢复：failed(EXEC_TIMEOUT) + 告警 +
// 单据状态重算；新鲜心跳项不受影响。
func TestRecoverTimedOutItems(t *testing.T) {
	ctx := context.Background()
	t.Run("超时项转 failed 并告警，在途项不动", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := h.seedExecutingOrder(t, nil,
			seedItem{batch: 1, status: domain.ItemStatusRunning},
			seedItem{batch: 1, status: domain.ItemStatusRunning},
		)
		items, err := h.items.ListByOrder(ctx, orderID)
		require.NoError(t, err)
		stale, fresh := items[0], items[1]
		old := h.now().Add(-2 * time.Hour)
		require.NoError(t, h.items.UpdateHeartbeat(ctx, stale.ID.Hex(), old))
		require.NoError(t, h.items.UpdateHeartbeat(ctx, fresh.ID.Hex(), h.now()))

		recovered, err := h.svc.RecoverTimedOutItems(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, recovered)

		got, err := h.items.GetByID(ctx, stale.ID.Hex())
		require.NoError(t, err)
		assert.Equal(t, domain.ItemStatusFailed, got.Status)
		assert.Contains(t, got.Error, domain.CodeExecTimeout)

		gotFresh, err := h.items.GetByID(ctx, fresh.ID.Hex())
		require.NoError(t, err)
		assert.Equal(t, domain.ItemStatusRunning, gotFresh.Status)

		require.Len(t, h.notifier.events, 1)
		assert.Contains(t, h.notifier.events[0], orderID)
	})
	t.Run("恢复后无在途项且终批完成 → 进入验证窗口", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := h.seedExecutingOrder(t, &domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 2},
			seedItem{batch: 1, status: domain.ItemStatusRunning},
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
		)
		items, err := h.items.ListByOrder(ctx, orderID)
		require.NoError(t, err)
		for _, it := range items {
			require.NoError(t, h.items.UpdateHeartbeat(ctx, it.ID.Hex(), h.now().Add(-time.Hour)))
		}

		recovered, err := h.svc.RecoverTimedOutItems(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, recovered)

		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, domain.ChangeStatusVerifying, order.Status)
		require.NotNil(t, order.VerifyWindowUntil)
	})
	t.Run("阈值取自配置", func(t *testing.T) {
		h := newExecuteHarness(t)
		cfg, err := h.alertCfg.Get(ctx)
		require.NoError(t, err)
		cfg.Thresholds.ItemHeartbeatTimeoutMinutes = 5
		require.NoError(t, h.alertCfg.Save(ctx, &cfg))

		orderA := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusRunning})
		itemsA, err := h.items.ListByOrder(ctx, orderA)
		require.NoError(t, err)
		require.Len(t, itemsA, 1)
		require.NoError(t, h.items.UpdateHeartbeat(ctx, itemsA[0].ID.Hex(), h.now().Add(-10*time.Minute)))
		recovered, err := h.svc.RecoverTimedOutItems(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, recovered) // 10min > 5min 阈值命中

		// 3 分钟前心跳不命中（独立 harness：同指纹第二张活跃单触发互斥）
		h2 := newExecuteHarness(t)
		orderB := h2.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusRunning})
		itemsB, err := h2.items.ListByOrder(ctx, orderB)
		require.NoError(t, err)
		require.Len(t, itemsB, 1)
		require.NoError(t, h2.items.UpdateHeartbeat(ctx, itemsB[0].ID.Hex(), h2.now().Add(-3*time.Minute)))
		recovered, err = h2.svc.RecoverTimedOutItems(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, recovered)
	})
}

// ---------------------------------------------------------------------
// AC-4：rate_limited 退避（可见 → 上限耗尽 → failed）
// ---------------------------------------------------------------------

// TestExecuteItem_RateLimitBackoffThenSuccess 限流退避重试后成功：
// 退避期间 status=rate_limited（进度轮询可见"限流重试中"）。
func TestExecuteItem_RateLimitBackoffThenSuccess(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptRateLimit, scriptRateLimit, scriptSuccess)
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	itemID := items[0].ID.Hex()

	var observed atomic.Value // 退避入睡时点的项状态
	h.svc.sleep = func(ctx context.Context, d time.Duration) error {
		got, _ := h.items.GetByID(ctx, itemID)
		observed.Store(got.Status)
		return sleepWithContext(ctx, time.Millisecond)
	}

	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, itemID))

	assert.Equal(t, domain.ItemStatusSuccess, statusOf(t, h, itemID))
	assert.Equal(t, 3, h.channel.attempts())
	assert.Equal(t, domain.ItemStatusRateLimited, observed.Load(), "退避期间进度轮询应见 rate_limited")
}

// TestExecuteItem_RateLimitCeilingExhausted 退避上限耗尽 → failed
// （CLOUD_API_RATELIMITED + 尝试次数），不无限重试。
func TestExecuteItem_RateLimitCeilingExhausted(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptRateLimit)
	h.svc.rateLimit = ItemRateLimitPolicy{MaxAttempts: 2, Backoffs: []time.Duration{time.Millisecond}, MaxTotalWait: 5 * time.Millisecond}
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)

	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, items[0].ID.Hex()))

	got, err := h.items.GetByID(ctx, items[0].ID.Hex())
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusFailed, got.Status)
	assert.Contains(t, got.Error, domain.CodeCloudApiRateLimited)
	assert.Contains(t, got.Error, "退避上限耗尽")
	assert.Equal(t, 2, h.channel.attempts(), "次数闸门生效，不无限重试")
}

// TestExecuteItem_K8sChannelFailureMapping K8s 通道项：集群不可达哨兵 →
// error=K8S_UNREACHABLE（kubeconfig 凭证分支覆盖）。
func TestExecuteItem_K8sChannelFailureMapping(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	k8sChan := newScriptedChannel(deployer.ChannelTypeK8sAPI)
	k8sChan.script = []scriptedOutcome{scriptFailure}
	// 让失败携带 ErrK8sUnreachable 哨兵：复用脚本通道的失败不携带哨兵，
	// 直接以 k8s 语义包装一次。
	k8sChan.mu.Lock()
	k8sChan.k8sUnreachable = true
	k8sChan.mu.Unlock()
	h.svc.channels[string(deployer.ChannelTypeK8sAPI)] = k8sChan

	ref := domain.ResourceRef{
		Channel: domain.ChannelK8sAPI, ClusterID: "prod-cluster", Namespace: "default",
		Kind: "Ingress", ResourceID: "gw-ingress",
	}
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending, ref: ref})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)

	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, items[0].ID.Hex()))

	got, err := h.items.GetByID(ctx, items[0].ID.Hex())
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusFailed, got.Status)
	assert.Contains(t, got.Error, domain.CodeK8sUnreachable)
}

// TestItemRateLimitPolicyNormalized 非法/零值策略回退缺省（重试安全侧）。
func TestItemRateLimitPolicyNormalized(t *testing.T) {
	def := DefaultItemRateLimitPolicy()
	assert.Equal(t, def, ItemRateLimitPolicy{}.normalized())
	assert.Equal(t, def, ItemRateLimitPolicy{MaxAttempts: 2, Backoffs: []time.Duration{time.Second}, MaxTotalWait: -1}.normalized())
	assert.Equal(t, def, ItemRateLimitPolicy{MaxAttempts: 2, Backoffs: []time.Duration{0}, MaxTotalWait: time.Second}.normalized())
	custom := ItemRateLimitPolicy{MaxAttempts: 4, Backoffs: []time.Duration{time.Second, time.Second}, MaxTotalWait: 3 * time.Second}
	assert.Equal(t, custom, custom.normalized())
}

// TestItemRateLimitPolicyWaitAfter 双闸门纯函数：次数与总时长任一耗尽即停。
func TestItemRateLimitPolicyWaitAfter(t *testing.T) {
	p := ItemRateLimitPolicy{MaxAttempts: 3, Backoffs: []time.Duration{time.Second, 2 * time.Second}, MaxTotalWait: 3 * time.Second}
	d, ok := p.waitAfter(1, 0)
	assert.True(t, ok)
	assert.Equal(t, time.Second, d)
	d, ok = p.waitAfter(2, time.Second)
	assert.True(t, ok)
	assert.Equal(t, 2*time.Second, d)
	_, ok = p.waitAfter(3, 3*time.Second) // 次数耗尽
	assert.False(t, ok)
	strict := ItemRateLimitPolicy{MaxAttempts: 10, Backoffs: []time.Duration{2 * time.Second}, MaxTotalWait: 3 * time.Second}
	_, ok = strict.waitAfter(2, 2*time.Second) // 2+2 > 3 总时长耗尽
	assert.False(t, ok)
}

// ---------------------------------------------------------------------
// AC-5：ConfirmBatch 人工续批门控
// ---------------------------------------------------------------------

// TestConfirmBatch_Gates 续批双门控：上一批全部 success + 批级验证达标。
func TestConfirmBatch_Gates(t *testing.T) {
	ctx := context.Background()
	newPausedOrder := func(t *testing.T, h *executeHarness, items ...seedItem) string {
		t.Helper()
		pausedAt := h.now().Add(-time.Minute)
		batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 2, Paused: true, PausedAt: &pausedAt}
		return h.seedExecutingOrder(t, batch, items...)
	}
	t.Run("上一批存在失败项拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := newPausedOrder(t, h,
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 1, status: domain.ItemStatusFailed, err: "EXEC_FAILED: fixture"},
			seedItem{batch: 2, status: domain.ItemStatusPending},
		)
		err := h.svc.ConfirmBatch(ctx, orderID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrBatchNotConfirmable)
		assert.Contains(t, err.Error(), "failed")
	})
	t.Run("批级验证未达标拒绝（附原因）", func(t *testing.T) {
		h := newExecuteHarness(t)
		h.verify.verified = false
		h.verify.reason = "提频探测未连续一致"
		orderID := newPausedOrder(t, h,
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 2, status: domain.ItemStatusPending},
		)
		err := h.svc.ConfirmBatch(ctx, orderID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrBatchNotConfirmable)
		assert.Contains(t, err.Error(), "提频探测未连续一致")
		assert.Equal(t, 1, h.verify.calls)
	})
	t.Run("批级验证通道未接入拒绝（5.10 前安全侧）", func(t *testing.T) {
		h := newExecuteHarness(t)
		h.svc.verify = nil
		orderID := newPausedOrder(t, h,
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 2, status: domain.ItemStatusPending},
		)
		err := h.svc.ConfirmBatch(ctx, orderID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrBatchNotConfirmable)
	})
	t.Run("判定通道故障拒绝", func(t *testing.T) {
		h := newExecuteHarness(t)
		h.verify.err = errors.New("probe backend down")
		orderID := newPausedOrder(t, h,
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 2, status: domain.ItemStatusPending},
		)
		err := h.svc.ConfirmBatch(ctx, orderID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrBatchNotConfirmable)
	})
	t.Run("不可执行项 skipped 不阻塞（不计分母）", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := newPausedOrder(t, h,
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 1, status: domain.ItemStatusSkipped, err: "ERR_DISCOVERY_ONLY: fixture"},
			seedItem{batch: 2, status: domain.ItemStatusPending},
		)
		require.NoError(t, h.svc.ConfirmBatch(ctx, orderID))
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, 2, order.BatchInfo.CurrentBatch)
	})
	t.Run("放行：currentBatch+1 且暂停标记清除", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := newPausedOrder(t, h,
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 2, status: domain.ItemStatusPending},
		)
		require.NoError(t, h.svc.ConfirmBatch(ctx, orderID))
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, 2, order.BatchInfo.CurrentBatch)
		assert.False(t, order.BatchInfo.Paused)
		assert.Nil(t, order.BatchInfo.PausedAt)
		assert.Equal(t, domain.ChangeStatusExecuting, order.Status)
		assert.Equal(t, execTestOldFP, order.ActiveMutex, "续批全程持有互斥")
	})
	t.Run("verifying 态放行（批级验证达标后）", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := h.seedExecutingOrder(t,
			&domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 2},
			seedItem{batch: 1, status: domain.ItemStatusSuccess},
			seedItem{batch: 2, status: domain.ItemStatusPending},
		)
		// 批间流转：executing→verifying + 暂停标记（引擎 EnterVerify 语义）
		ok, err := h.orders.EnterVerify(ctx, orderID, h.now().Add(24*time.Hour), h.now())
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, h.svc.ConfirmBatch(ctx, orderID))
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, 2, order.BatchInfo.CurrentBatch)
		assert.Equal(t, domain.ChangeStatusExecuting, order.Status)
	})
	t.Run("终批不可续批", func(t *testing.T) {
		h := newExecuteHarness(t)
		pausedAt := h.now().Add(-time.Minute)
		orderID := h.seedExecutingOrder(t,
			&domain.BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 2, Paused: true, PausedAt: &pausedAt},
			seedItem{batch: 2, status: domain.ItemStatusSuccess},
		)
		err := h.svc.ConfirmBatch(ctx, orderID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrBatchNotConfirmable)
		assert.Contains(t, err.Error(), "last batch")
	})
	t.Run("未分批单不可续批", func(t *testing.T) {
		h := newExecuteHarness(t)
		orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusSuccess})
		err := h.svc.ConfirmBatch(ctx, orderID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrBatchNotConfirmable)
	})
}

// ---------------------------------------------------------------------
// AC-6：订单状态重算（批间循环、abort 收敛）
// ---------------------------------------------------------------------

// TestRecompute_AbortConvergesToCancelled executing 中止路径收敛：
// Cancel Abort 已将 pending 项标 skipped（无 error），running 项完成后
// 按剩余项重算收敛 cancelled（token 同原子清除）。
func TestRecompute_AbortConvergesToCancelled(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	batch := &domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 2}
	orderID := h.seedExecutingOrder(t, batch,
		seedItem{batch: 1, status: domain.ItemStatusSuccess},
		seedItem{batch: 1, status: domain.ItemStatusSkipped}, // cancel-skipped：无 error
	)
	h.bindDispatch()

	require.NoError(t, h.svc.Execute(ctx, orderID)) // 无 pending 可派发 → 重算收敛

	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCancelled, order.Status)
	assert.Empty(t, order.ActiveMutex, "终态同原子清除互斥 token")
}

// TestRecompute_FutureBatchSkippedConvergesCancelled 后续批整体被跳过
// （中止波及未到期批次）→ cancelled。
func TestRecompute_FutureBatchSkippedConvergesCancelled(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 2}
	orderID := h.seedExecutingOrder(t, batch,
		seedItem{batch: 1, status: domain.ItemStatusSuccess},
		seedItem{batch: 1, status: domain.ItemStatusFailed, err: "EXEC_FAILED: fixture"},
		seedItem{batch: 2, status: domain.ItemStatusSkipped}, // cancel-skipped
	)
	h.bindDispatch()

	require.NoError(t, h.svc.Execute(ctx, orderID))

	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCancelled, order.Status)
}

// TestRecompute_GenerationSkippedStillVerifies 生成期不可执行项（skipped
// 携带 Reason）不构成取消痕迹：终批完成走验证窗口。
func TestRecompute_GenerationSkippedStillVerifies(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	batch := &domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 2}
	orderID := h.seedExecutingOrder(t, batch,
		seedItem{batch: 1, status: domain.ItemStatusSuccess},
		seedItem{batch: 1, status: domain.ItemStatusSkipped, err: "ERR_DISCOVERY_ONLY: fixture"},
	)
	h.bindDispatch()

	require.NoError(t, h.svc.Execute(ctx, orderID))

	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status)
}

// ---------------------------------------------------------------------
// internal/task 框架挂载（taskx 执行器 + 派发器）
// ---------------------------------------------------------------------

// fakeTaskxRepo taskx.TaskRepository 内存假实现（队列装配测试）。
type fakeTaskxRepo struct {
	mu    sync.Mutex
	tasks map[string]*taskx.Task
}

func newFakeTaskxRepo() *fakeTaskxRepo { return &fakeTaskxRepo{tasks: map[string]*taskx.Task{}} }

func (f *fakeTaskxRepo) Create(_ context.Context, task taskx.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := task
	f.tasks[t.ID] = &t
	return nil
}
func (f *fakeTaskxRepo) GetByID(_ context.Context, id string) (taskx.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tasks[id]; ok {
		return *t, nil
	}
	return taskx.Task{}, fmt.Errorf("task %s not found", id)
}
func (f *fakeTaskxRepo) Update(_ context.Context, task taskx.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID] = &task
	return nil
}
func (f *fakeTaskxRepo) UpdateStatus(_ context.Context, id string, status taskx.TaskStatus, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tasks[id]; ok {
		t.Status = status
	}
	return nil
}
func (f *fakeTaskxRepo) UpdateProgress(_ context.Context, _ string, _ int, _ string) error {
	return nil
}
func (f *fakeTaskxRepo) List(_ context.Context, _ taskx.TaskFilter) ([]taskx.Task, error) {
	return nil, nil
}
func (f *fakeTaskxRepo) Count(_ context.Context, _ taskx.TaskFilter) (int64, error) {
	return 0, nil
}
func (f *fakeTaskxRepo) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tasks, id)
	return nil
}

// TestTaskxDispatch_ExecutesViaQueue 生产装配路径：TaskxItemDispatcher →
// taskx.Queue → ChangeItemExecutor → 项执行收敛（框架挂载验证）。
func TestTaskxDispatch_ExecutesViaQueue(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptSuccess)
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	itemID := items[0].ID.Hex()

	queue := taskx.NewQueue(newFakeTaskxRepo(), elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 8})
	queue.RegisterExecutor(NewChangeItemExecutor(h.svc))
	queue.Start()
	defer queue.Stop()

	require.NoError(t, TaskxItemDispatcher{Queue: queue}.DispatchItem(ctx, orderID, itemID))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if statusOf(t, h, itemID) == domain.ItemStatusSuccess {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("item not executed via taskx queue, status=%s", statusOf(t, h, itemID))
}

// TestChangeItemExecutor_ParamsValidation 框架适配参数校验。
func TestChangeItemExecutor_ParamsValidation(t *testing.T) {
	exec := NewChangeItemExecutor(nil)
	assert.Equal(t, TaskTypeExecuteChangeItem, exec.GetType())
	err := exec.Execute(context.Background(), &taskx.Task{ID: "t1", Params: map[string]interface{}{"orderId": "o1"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "itemId")
}

func statusOf(t *testing.T, h *executeHarness, itemID string) domain.ChangeItemStatus {
	t.Helper()
	got, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	return got.Status
}

// ---------------------------------------------------------------------
// 凭证来源生产实现（AccountCredentialSource）
// ---------------------------------------------------------------------

// execFakeAccountRepo account 仓储假实现（嵌入接口仅实现 List，其余不触达）。
type execFakeAccountRepo struct {
	accountrepo.CloudAccountRepository
	accounts []sharedomain.CloudAccount
}

func (f *execFakeAccountRepo) List(_ context.Context, filter sharedomain.CloudAccountFilter) ([]sharedomain.CloudAccount, int64, error) {
	out := make([]sharedomain.CloudAccount, 0, len(f.accounts))
	for _, a := range f.accounts {
		if filter.Provider != "" && a.Provider != filter.Provider {
			continue
		}
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		out = append(out, a)
	}
	return out, int64(len(out)), nil
}

func TestAccountCredentialSource(t *testing.T) {
	ctx := context.Background()
	t.Run("云账号凭证解析", func(t *testing.T) {
		accounts := &execFakeAccountRepo{accounts: []sharedomain.CloudAccount{
			{Name: "acct-main", Provider: sharedomain.CloudProviderAliyun, Status: sharedomain.CloudAccountStatusActive,
				AccessKeyID: "ak-123", AccessKeySecret: "sk-456"},
			{Name: "acct-disabled", Provider: sharedomain.CloudProviderAliyun, Status: sharedomain.CloudAccountStatusDisabled,
				AccessKeyID: "ak-x", AccessKeySecret: "sk-x"},
		}}
		src := NewAccountCredentialSource(accounts, nil, nil)
		cred, err := src.CloudCredential(ctx, "aliyun", "acct-main")
		require.NoError(t, err)
		assert.Equal(t, deployer.CredentialKindCloudAK, cred.Kind)
		assert.Equal(t, "aliyun", cred.Cloud)
		assert.Equal(t, "acct-main", cred.AccountKey)
		assert.Equal(t, "ak-123", cred.AccessKey)
		assert.Equal(t, []byte("sk-456"), cred.Secret)
		require.NoError(t, cred.Validate())
	})
	t.Run("非 active 或不存在账号拒绝", func(t *testing.T) {
		accounts := &execFakeAccountRepo{accounts: []sharedomain.CloudAccount{
			{Name: "acct-disabled", Provider: sharedomain.CloudProviderAliyun, Status: sharedomain.CloudAccountStatusDisabled,
				AccessKeyID: "ak-x", AccessKeySecret: "sk-x"},
		}}
		src := NewAccountCredentialSource(accounts, nil, nil)
		_, err := src.CloudCredential(ctx, "aliyun", "acct-disabled")
		require.Error(t, err)
		_, err = src.CloudCredential(ctx, "aliyun", "acct-missing")
		require.Error(t, err)
	})
	t.Run("K8s kubeconfig 信封解密", func(t *testing.T) {
		k8sCreds := certtest.NewFakeK8sCredentialRepo()
		crypto := certtest.NewTestCrypto(t)
		cipher, ver, err := crypto.Encrypt([]byte("apiVersion: v1 fixture-kubeconfig"))
		require.NoError(t, err)
		require.NoError(t, k8sCreds.Create(ctx, &domain.K8sCredential{
			ClusterName: "prod-cluster",
			Kubeconfig:  &domain.EncryptedSecret{Ciphertext: cipher, KeyVersion: ver},
		}))
		src := NewAccountCredentialSource(nil, k8sCreds, crypto)
		cred, err := src.K8sCredential(ctx, "prod-cluster")
		require.NoError(t, err)
		assert.Equal(t, deployer.CredentialKindKubeconfig, cred.Kind)
		assert.Equal(t, []byte("apiVersion: v1 fixture-kubeconfig"), cred.Secret)
		require.NoError(t, cred.Validate())
	})
	t.Run("未登记集群拒绝", func(t *testing.T) {
		src := NewAccountCredentialSource(nil, certtest.NewFakeK8sCredentialRepo(), certtest.NewTestCrypto(t))
		_, err := src.K8sCredential(ctx, "missing-cluster")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------
// item_result 审计（任务 7.2）：执行引擎项级终态 → ChangeAuditWriter 追加
// actor/时间/操作对象/结果；审计失败不阻塞执行主流程。
// ---------------------------------------------------------------------

// fakeChangeAuditWriter 审计写入端口假实现（记录追加事件）。
type fakeChangeAuditWriter struct {
	events []ChangeAuditEvent
	err    error // 注入写入失败
}

func (f *fakeChangeAuditWriter) WriteChangeAudit(_ context.Context, e ChangeAuditEvent) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

// TestExecuteItem_AuditItemResult 成功/失败项终态各追加一条 item_result 审计
// （actor 默认 executor；ctx 携带操作者时归因操作者；detail 含状态/错误/云证书）。
func TestExecuteItem_AuditItemResult(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptSuccess)
	writer := &fakeChangeAuditWriter{}
	h.svc.audit = writer
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	h.bindDispatch()

	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, items[0].ID.Hex()))

	require.Len(t, writer.events, 1)
	e := writer.events[0]
	assert.Equal(t, orderID, e.OrderID)
	assert.Equal(t, items[0].ID.Hex(), e.ItemID)
	assert.Equal(t, AuditActionItemResult, e.Action)
	assert.Equal(t, ActorExecutor, e.Actor, "无操作者 ctx 回退 executor 标识")
	assert.Contains(t, e.Detail, "status=success")
	assert.Contains(t, e.Detail, "cloudCertId=cloud-cert-1")
	assert.False(t, e.At.IsZero())

	// ctx 携带操作者（HTTP 触发路径经角色中间件注入）：item_result 归因操作者
	h2 := newExecuteHarness(t, scriptSuccess)
	writer2 := &fakeChangeAuditWriter{}
	h2.svc.audit = writer2
	orderID2 := h2.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items2, err := h2.items.ListByOrder(ctx, orderID2)
	require.NoError(t, err)
	h2.bindDispatch()
	opCtx := WithOperator(ctx, "ops@example.com")
	require.NoError(t, h2.svc.ExecuteItem(opCtx, orderID2, items2[0].ID.Hex()))
	require.Len(t, writer2.events, 1)
	assert.Equal(t, "ops@example.com", writer2.events[0].Actor)
}

// TestExecuteItem_AuditWriteFailureIsolated 审计写入失败不阻塞执行主流程：
// 项级终态与订单状态照常收敛（端口契约同 5.8 回滚审计）。
func TestExecuteItem_AuditWriteFailureIsolated(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t, scriptSuccess)
	h.svc.audit = &fakeChangeAuditWriter{err: assert.AnError}
	orderID := h.seedExecutingOrder(t, nil, seedItem{batch: 1, status: domain.ItemStatusPending})
	items, err := h.items.ListByOrder(ctx, orderID)
	require.NoError(t, err)
	h.bindDispatch()

	require.NoError(t, h.svc.ExecuteItem(ctx, orderID, items[0].ID.Hex()))

	got, err := h.items.GetByID(ctx, items[0].ID.Hex())
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, got.Status, "审计失败不得影响项级终态")
}
