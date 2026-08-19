package deployer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"go.mongodb.org/mongo-driver/mongo"
)

// CloudAPIChannel 云 API 执行通道（tech-design Interface 1 实现，任务 5.3）：
// 以注入的 per 云 CloudDeployer（3.1/3.2 SDK 适配经 5.4/5.5 组装）编排两段式
// UploadCert→BindResource，第二段失败时 CleanupOrphan 补偿 + 映射 active→orphan
// （供 5.9 orphan-cleanup 队列消费），成功时写 CloudCertMapping{status=active}。
//
// 业务级状态机判断（成功/失败/回滚语义收敛）归 5.7/5.8（Hard Rule）；
// 限流退避重试属 5.4/5.5 部署器实现（有界），本层仅透传 cloudx.ErrCloudRateLimited
// 哨兵语义（errors.Is 判定）。
type CloudAPIChannel struct {
	mu       sync.RWMutex
	entries  map[string]cloudDeployerEntry // cloud → 注册项
	mappings domain.CloudCertMappingRepository
	material CertMaterialSource
	oldRefs  OldRefSource // nil=降级为无已知引用（OldCloudCertID 空）
}

// cloudDeployerEntry 单云部署器注册项。
type cloudDeployerEntry struct {
	deployer CloudDeployer
	products []string // 该云全部已支持产品（DiscoverScope.Products 为空时的默认范围）
}

// NewCloudAPIChannel 创建云 API 通道。deployer 实例经 RegisterDeployer 注册
// （5.4/5.5 组装 aliyun/tencent 实例；mappings/material 为两段式编排必需依赖，
// nil 时 Deploy 返回显式装配错误）。oldRefs 允许 nil（无引用快照来源时
// OldCloudCertID 为空、不构成孤儿候选）。
func NewCloudAPIChannel(
	mappings domain.CloudCertMappingRepository,
	material CertMaterialSource,
	oldRefs OldRefSource,
) *CloudAPIChannel {
	return &CloudAPIChannel{
		entries:  map[string]cloudDeployerEntry{},
		mappings: mappings,
		material: material,
		oldRefs:  oldRefs,
	}
}

// RegisterDeployer 注册单云部署器及其支持的证书产品集（产品集为
// DiscoverScope.Products 为空时的默认发现范围，至少一项）。
// 云已注册时覆盖旧实例（装配期幂等重注册）。
func (c *CloudAPIChannel) RegisterDeployer(cloud string, d CloudDeployer, products ...string) error {
	if cloud == "" {
		return fmt.Errorf("%w: cloud is empty", ErrDeployerNotRegistered)
	}
	if d == nil {
		return fmt.Errorf("%w: deployer is nil for cloud %q", ErrDeployerNotRegistered, cloud)
	}
	if len(products) == 0 {
		return fmt.Errorf("%w: cloud %q registers no products", ErrDeployerNotRegistered, cloud)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[cloud] = cloudDeployerEntry{deployer: d, products: products}
	return nil
}

// Type 通道类型恒为 cloud_api。
func (c *CloudAPIChannel) Type() ChannelType { return ChannelTypeCloudAPI }

// entry ���单云注册项。
func (c *CloudAPIChannel) entry(cloud string) (cloudDeployerEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[cloud]
	return e, ok
}

// Discover 只读发现：按凭证归属云（creds.Cloud，单一账号凭证不跨云）×
// scope.Products（空=注册的全部已支持产品）调用 ListReferences，逐项回写
// scope.SnapshotID（tech-design DiscoverScope 契约）。scope.Clouds 非空且
// 不含 creds.Cloud 时返回空（该账号不在本轮范围）；无法定位资源/无证书
// 关联的发现项不构成引用（同 3.5 口径过滤）。任一产品发现失败即整体失败
// （部分失败聚合属 3.5 扫描编排，不属通道）。
func (c *CloudAPIChannel) Discover(ctx context.Context, creds Credential, scope DiscoverScope) ([]domain.CertReference, error) {
	defer creds.Zeroize()
	if err := creds.Validate(); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if len(scope.Clouds) > 0 && !containsString(scope.Clouds, creds.Cloud) {
		return []domain.CertReference{}, nil
	}
	entry, ok := c.entry(creds.Cloud)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrDeployerNotRegistered, creds.Cloud)
	}
	products := scope.Products
	if len(products) == 0 {
		products = entry.products
	}
	out := []domain.CertReference{}
	for _, product := range products {
		refs, err := entry.deployer.ListReferences(ctx, creds, product)
		if err != nil {
			return nil, fmt.Errorf("cloud api channel: list %s %s references: %w", creds.Cloud, product, err)
		}
		for _, r := range refs {
			if r.ResourceID == "" || r.ReferencedCloudCertID == "" {
				continue
			}
			r.Cloud = domain.Cloud(creds.Cloud)
			r.Product = domain.Product(product)
			r.SnapshotID = scope.SnapshotID
			out = append(out, r)
		}
	}
	return out, nil
}

// Deploy 两段式部署（AC-2/AC-3）：
//
//	执行前读引用快照 → 取证书材料（私钥明文仅内存，用毕 Zeroize）→
//	第一段 UploadCert → 写 CloudCertMapping{status=active}
//	（uk_fp_cloud_account 两段式去重；第二段崩溃时 5.9 可据此发现未绑定产物）→
//	第二段 BindResource：
//	  成功 → 映射保持 active，OrphanCandidate=旧云证书存在（验证达标后入清理队列）；
//	  失败 → 映射 active→orphan（入 5.9 清理队列）+ 尽力 CleanupOrphan 补偿
//	         （补偿失败不吞错、映射保持 orphan 供 5.9 重试），OrphanCandidate=true。
//
// 凭证副本与材料明文在函数返回前清零（Hard Rule：明文零生命周期）。
func (c *CloudAPIChannel) Deploy(ctx context.Context, creds Credential, target DeployTarget, newCertFingerprint string) (DeployResult, error) {
	defer creds.Zeroize()
	if err := creds.Validate(); err != nil {
		return DeployResult{}, err
	}
	if err := target.Validate(); err != nil {
		return DeployResult{}, err
	}
	if newCertFingerprint == "" {
		return DeployResult{}, fmt.Errorf("%w: newCertFingerprint is empty", ErrInvalidTarget)
	}
	if c.mappings == nil {
		return DeployResult{}, errors.New("deployer: cloud api channel assembled without mapping repository")
	}
	if c.material == nil {
		return DeployResult{}, errors.New("deployer: cloud api channel assembled without material source")
	}
	entry, ok := c.entry(target.Cloud)
	if !ok {
		return DeployResult{}, fmt.Errorf("%w: %q", ErrDeployerNotRegistered, target.Cloud)
	}

	// 执行前从引用快照读取旧云证书 ID（回滚依据与孤儿候选判定）。
	var oldCloudCertID string
	if c.oldRefs != nil {
		oldRef, found, err := c.oldRefs.CurrentRef(ctx, target.Cloud, target.Product, target.ResourceID)
		if err != nil {
			return DeployResult{}, fmt.Errorf("cloud api channel: read current reference: %w", err)
		}
		if found {
			oldCloudCertID = oldRef.ReferencedCloudCertID
		}
	}

	certPEM, keyPEM, _, err := c.material.Material(ctx, newCertFingerprint)
	if err != nil {
		return DeployResult{}, fmt.Errorf("cloud api channel: load cert material: %w", err)
	}
	defer domain.Zeroize(&keyPEM)

	// 第一段：上传云证书库。
	newCloudCertID, err := entry.deployer.UploadCert(ctx, creds, certPEM, keyPEM)
	if err != nil {
		return DeployResult{OldCloudCertID: oldCloudCertID},
			fmt.Errorf("cloud api channel: upload cert to %s: %w", target.Cloud, err)
	}

	// 映射先行写入 active（两段式去重与崩溃恢复锚点）。
	if err := c.mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: newCertFingerprint,
		Cloud:           target.Cloud,
		AccountKey:      target.AccountKey,
		CloudCertID:     newCloudCertID,
		Status:          domain.MappingStatusActive,
	}); err != nil {
		return DeployResult{NewCloudCertID: newCloudCertID, OldCloudCertID: oldCloudCertID},
			fmt.Errorf("cloud api channel: upsert cloud cert mapping: %w", err)
	}

	// 第二段：绑定目标资源。
	if err := entry.deployer.BindResource(ctx, creds, target.Product, target.ResourceID, newCloudCertID); err != nil {
		bindErr := fmt.Errorf("cloud api channel: bind %s %s resource: %w", target.Cloud, target.Product, err)
		compensated := c.compensateBindFailure(ctx, entry.deployer, creds, target, newCloudCertID)
		if !compensated {
			return DeployResult{NewCloudCertID: newCloudCertID, OldCloudCertID: oldCloudCertID, OrphanCandidate: true},
				fmt.Errorf("%w (orphan compensation incomplete, mapping kept orphan for retry)", bindErr)
		}
		return DeployResult{NewCloudCertID: newCloudCertID, OldCloudCertID: oldCloudCertID, OrphanCandidate: true}, bindErr
	}

	return DeployResult{
		NewCloudCertID:  newCloudCertID,
		OldCloudCertID:  oldCloudCertID,
		OrphanCandidate: oldCloudCertID != "", // 旧云证书被替换 → 孤儿候选（验证达标后入清理队列）
	}, nil
}

// compensateBindFailure 第二段失败补偿：映射 active→orphan（5.9 队列入口）+
// 尽力 CleanupOrphan 清理未绑定云侧孤儿证书。返回 true=补偿动作全部完成
// （orphan 映射已标记且云侧已清理）；false=存在未完成动作（映射保持 orphan
// 供 5.9 幂等重试，云侧清理交由 5.9 兜底——CleanupOrphan 对已删除证书幂等成功）。
// 补偿失败不改变部署失败这一主结果，仅通过返回值提示调用方补偿未竟。
func (c *CloudAPIChannel) compensateBindFailure(
	ctx context.Context,
	d CloudDeployer,
	creds Credential,
	target DeployTarget,
	newCloudCertID string,
) bool {
	complete := true
	m, err := c.mappings.FindByCloudCertID(ctx, target.Cloud, target.AccountKey, newCloudCertID)
	if err != nil {
		complete = false // 反查失败：映射未能转 orphan（保留 active，5.9 不消费；上层可见错误）
	} else if err := c.mappings.UpdateStatus(ctx, m.ID.Hex(), domain.MappingStatusOrphan); err != nil {
		complete = false
	}
	if err := d.CleanupOrphan(ctx, creds, newCloudCertID); err != nil {
		complete = false // 云侧未清理：映射已 orphan（或保留），5.9 幂等重试兜底
	}
	return complete
}

// Rollback 恢复引用为旧云证书 ID（重新绑定）。回滚目标有效性三判定
// （GetCert：已删除/已过期/被替换 → ROLLBACK_TARGET_INVALID）属 5.8
// ChangeService.Rollback 前置校验，通道不做业务级判定（Hard Rule）；
// 被替换新证书的孤儿标记/清理入 5.9 队列（由 5.8 编排经 ChangeReport.
// OrphanCleanup 呈现），OrphanCleaned 由上层回填。
func (c *CloudAPIChannel) Rollback(ctx context.Context, creds Credential, target DeployTarget, oldRef domain.CertReference) (RollbackResult, error) {
	defer creds.Zeroize()
	if err := creds.Validate(); err != nil {
		return RollbackResult{}, err
	}
	if err := target.Validate(); err != nil {
		return RollbackResult{}, err
	}
	if oldRef.ReferencedCloudCertID == "" {
		return RollbackResult{}, fmt.Errorf("%w: rollback requires oldRef.referencedCloudCertId", ErrInvalidTarget)
	}
	entry, ok := c.entry(target.Cloud)
	if !ok {
		return RollbackResult{}, fmt.Errorf("%w: %q", ErrDeployerNotRegistered, target.Cloud)
	}
	if err := entry.deployer.BindResource(ctx, creds, target.Product, target.ResourceID, oldRef.ReferencedCloudCertID); err != nil {
		return RollbackResult{
			Success: false,
			ErrCode: rollbackErrCode(err),
			Reason:  err.Error(), // 适配层错误为静态文案+产品上下文，不含私钥/凭证片段
		}, fmt.Errorf("cloud api channel: rollback bind %s %s resource: %w", target.Cloud, target.Product, err)
	}
	return RollbackResult{
		Success:       true,
		RestoredRef:   oldRef,
		OrphanCleaned: []string{},
	}, nil
}

// CleanupOrphanCert 孤儿云证书清理路由（任务 5.9 orphan-cleanup 消费者入口）：
// 经注册的 per 云 CloudDeployer.CleanupOrphan 执行（3.1/3.2 适配对已删除证书
// 幂等成功——重复消费无副作用）。凭证副本用毕清零（通道契约同 Deploy/Rollback）。
func (c *CloudAPIChannel) CleanupOrphanCert(ctx context.Context, creds Credential, cloud, cloudCertID string) error {
	defer creds.Zeroize()
	if err := creds.Validate(); err != nil {
		return err
	}
	if cloudCertID == "" {
		return fmt.Errorf("%w: cloudCertID is empty", ErrInvalidTarget)
	}
	entry, ok := c.entry(cloud)
	if !ok {
		return fmt.Errorf("%w: %q", ErrDeployerNotRegistered, cloud)
	}
	return entry.deployer.CleanupOrphan(ctx, creds, cloudCertID)
}

// InspectCloudCert 查询云侧证书在库状态（任务 5.8 回滚目标有效性三判定
// 数据源，只读）：经注册的 per 云 CloudDeployer.GetCert 路由（3.1/3.2 适配
// 已将云侧 404/NotExist 归一为 Exists=false 非错误）。凭证副本用毕清零
// （通道契约同 Discover/Deploy/Rollback）。
func (c *CloudAPIChannel) InspectCloudCert(ctx context.Context, creds Credential, cloud, cloudCertID string) (CloudCertInfo, error) {
	defer creds.Zeroize()
	if err := creds.Validate(); err != nil {
		return CloudCertInfo{}, err
	}
	if cloudCertID == "" {
		return CloudCertInfo{}, fmt.Errorf("%w: cloudCertID is empty", ErrInvalidTarget)
	}
	entry, ok := c.entry(cloud)
	if !ok {
		return CloudCertInfo{}, fmt.Errorf("%w: %q", ErrDeployerNotRegistered, cloud)
	}
	return entry.deployer.GetCert(ctx, creds, cloudCertID)
}

// rollbackErrCode 回滚失败错误码映射（限流哨兵 → CLOUD_API_RATELIMITED；
// K8S_UNREACHABLE/ROLLBACK_TARGET_INVALID 由 5.6/5.8 语境产生，云通道不产生）。
func rollbackErrCode(err error) string {
	if errors.Is(err, cloudx.ErrCloudRateLimited) {
		return ErrCodeCloudRateLimited
	}
	return ""
}

// containsString 切片包含判定（小工具，避免引入 slices 依赖风格差异）。
func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------
// 生产来源实现（CertMaterialSource / OldRefSource 端口）
// ---------------------------------------------------------------------

// LedgerMaterialSource 证书材料来源生产实现：台账仓储取证书束 PEM 与
// 信封加密私钥，经 EnvelopeCrypto（任务 1.1）按 keyVersion 路由解密。
// 返回的 keyPEM 明文仅内存，调用方（CloudAPIChannel）用毕 Zeroize。
type LedgerMaterialSource struct {
	certs  domain.CertificateRepository
	crypto *domain.EnvelopeCrypto
}

// NewLedgerMaterialSource 创建台账证书材料来源。
func NewLedgerMaterialSource(certs domain.CertificateRepository, crypto *domain.EnvelopeCrypto) *LedgerMaterialSource {
	return &LedgerMaterialSource{certs: certs, crypto: crypto}
}

// Material 按指纹取证书束与解密私钥。证书未登记、无私钥（fingerprint_only）
// 或解密失败返回 ErrCertMaterialUnavailable（消息仅含指纹与托管状态等安全参数）。
func (s *LedgerMaterialSource) Material(ctx context.Context, fingerprint string) (string, []byte, int, error) {
	cert, err := s.certs.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		return "", nil, 0, fmt.Errorf("%w: get certificate: %v", ErrCertMaterialUnavailable, err)
	}
	if cert.CertPEM == "" || cert.EncryptedPrivateKey == nil || cert.HostingStatus != domain.HostingStatusComplete {
		return "", nil, 0, fmt.Errorf("%w: fingerprint=%s hostingStatus=%s hasPrivateKey=%t",
			ErrCertMaterialUnavailable, fingerprint, cert.HostingStatus, cert.EncryptedPrivateKey != nil)
	}
	keyPEM, err := s.crypto.Decrypt(cert.EncryptedPrivateKey.Ciphertext, cert.EncryptedPrivateKey.KeyVersion)
	if err != nil {
		return "", nil, 0, fmt.Errorf("%w: decrypt private key: %v", ErrCertMaterialUnavailable, err)
	}
	return cert.CertPEM, keyPEM, cert.EncryptedPrivateKey.KeyVersion, nil
}

// SnapshotOldRefSource 引用快照来源生产实现：最新成功扫描快照
// （ScanSnapshot status=done）中目标资源的最新引用（scannedAt 最新一条，
// 跨账号同资源以最近扫描为准）。无成功快照或快照内无该资源引用时
// found=false（首次部署语义，OldCloudCertID 空、不构成孤儿候选）。
type SnapshotOldRefSource struct {
	snapshots domain.ScanSnapshotRepository
	refs      domain.CertReferenceRepository
}

// NewSnapshotOldRefSource 创建快照引用来源。
func NewSnapshotOldRefSource(snapshots domain.ScanSnapshotRepository, refs domain.CertReferenceRepository) *SnapshotOldRefSource {
	return &SnapshotOldRefSource{snapshots: snapshots, refs: refs}
}

// CurrentRef 读取目标资源当前引用（cloud+product+resourceId 匹配）。
func (s *SnapshotOldRefSource) CurrentRef(ctx context.Context, cloud, product, resourceID string) (domain.CertReference, bool, error) {
	snap, err := s.snapshots.LatestDone(ctx)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return domain.CertReference{}, false, nil // 无成功快照=无已知引用
		}
		return domain.CertReference{}, false, fmt.Errorf("deployer: latest done snapshot: %w", err)
	}
	all, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return domain.CertReference{}, false, fmt.Errorf("deployer: list snapshot references: %w", err)
	}
	var best *domain.CertReference
	for i := range all {
		r := &all[i]
		if r.Cloud != domain.Cloud(cloud) || r.Product != domain.Product(product) || r.ResourceID != resourceID {
			continue
		}
		if best == nil || r.ScannedAt.After(best.ScannedAt) {
			best = r
		}
	}
	if best == nil {
		return domain.CertReference{}, false, nil
	}
	return *best, true, nil
}
