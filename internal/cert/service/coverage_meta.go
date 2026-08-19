package service

import (
	"context"
	"fmt"
	"sort"

	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// 覆盖率分母端口（tech-design"覆盖率分母：资产独立盘点数据源"）
// ---------------------------------------------------------------------

// CloudProductKey 云×产品聚合键（coverageMeta 条目定位）。
type CloudProductKey struct {
	Cloud   domain.Cloud
	Product domain.Product
}

// k8sCoverageKey K8s CRD 引用的 coverageMeta 键（cloud 空 + product=crd；
// asset 不盘点 K8s，total 恒 -1=分母不可用（盲区声明）。
var k8sCoverageKey = CloudProductKey{Cloud: "", Product: domain.ProductCRD}

// AssetCountSource 覆盖率分母独立盘点数据源（internal/asset 资产同步的只读聚合视图）。
//
// Hard Rule（任务 3.5）：coverageMeta.total 必须来自 internal/asset 聚合，
// 不得用扫描自身发现的资源数当分母（防自指）——本端口是分母唯一入口，
// 扫描发现数仅作分子（covered）。
type AssetCountSource interface {
	// Counts 返回按 (provider, 证书产品) 聚合的 asset 在用资源计数
	//（Model 按 Provider 分类 + Instance 实例，由既有资产同步任务写入刷新）。
	// asset 不可用返回 error（调用方据此固化 total=-1 盲区声明）。
	Counts(ctx context.Context) (map[CloudProductKey]int, error)
}

// ---------------------------------------------------------------------
// coverageCalculator 分母固化 / 覆盖率收敛
// ---------------------------------------------------------------------

// coverageCalculator coverageMeta 计算组件。
type coverageCalculator struct {
	assets AssetCountSource
}

// snapshotTotals 扫描启动时固化分母（AC："扫描启动时按云×产品聚合 internal/asset
// 在用资源计数为 total"）：
//   - asset 聚合不可用 → scope 全部键 total=-1（分母不可用盲区声明）；
//   - 正常 → asset 计数；计数为 0 但上一成功快照历史 total>0（计数异常）→ -1；
//   - scope 外的 asset 键忽略（分母随扫描范围固化）。
//
// 返回 map 供快照创建时一次性写入（covered=0 占位，收敛时回填）。
func (c *coverageCalculator) snapshotTotals(
	ctx context.Context,
	scope []CloudProductKey,
	history []domain.CoverageMeta,
) map[CloudProductKey]int {
	histTotal := make(map[CloudProductKey]int, len(history))
	for _, h := range history {
		histTotal[CloudProductKey{Cloud: domain.Cloud(h.Cloud), Product: domain.Product(h.Product)}] = h.Total
	}

	totals := make(map[CloudProductKey]int, len(scope))
	counts, err := c.assets.Counts(ctx)
	if err != nil {
		// asset 不可用：scope 全部 -1（不中断扫描——分母盲区化，发现照常进行）
		for _, k := range scope {
			totals[k] = -1
		}
		return totals
	}
	for _, k := range scope {
		total := counts[k] // 缺键=0（该云产品无在用资产）
		if total == 0 && histTotal[k] > 0 {
			// 计数异常：0 但历史非 0（asset 同步丢失/滞后）→ 分母不可用
			total = -1
		}
		totals[k] = total
	}
	return totals
}

// sortCoverageKeys 按 (cloud, product) 字典序排序（coverageMeta 输出次序契约：
// 快照占位与收敛两侧共用）。
func sortCoverageKeys(keys []CloudProductKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Cloud != keys[j].Cloud {
			return keys[i].Cloud < keys[j].Cloud
		}
		return keys[i].Product < keys[j].Product
	})
}

// finalizeCoverage 扫描收敛（AC："covered=本轮 CertReference 去重资源数；covered>total
// 时以 covered 为准并标记滞后"）：totals 为启动时固化的分母（保持不动，防自指），
// covered 为本轮各键去重资源数；输出按 (cloud, product) 字典序稳定排序。
// K8s crd 键 total 恒 -1（asset 不盘点 K8s）。
func finalizeCoverage(totals map[CloudProductKey]int, covered map[CloudProductKey]int) []domain.CoverageMeta {
	keys := make(map[CloudProductKey]struct{}, len(totals)+len(covered))
	for k := range totals {
		keys[k] = struct{}{}
	}
	for k := range covered {
		keys[k] = struct{}{}
	}
	ordered := make([]CloudProductKey, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sortCoverageKeys(ordered)

	out := make([]domain.CoverageMeta, 0, len(ordered))
	for _, k := range ordered {
		total, ok := totals[k]
		if !ok {
			// 本轮才发现、启动时未固化分母的键（理论不可达：scope 启动即全量），
			// 防御性按分母不可用处理
			total = -1
		}
		if k == k8sCoverageKey {
			total = -1 // asset 不盘点 K8s CRD
		}
		cm := domain.CoverageMeta{
			Cloud:   string(k.Cloud),
			Product: string(k.Product),
			Covered: covered[k],
			Total:   total,
		}
		if total >= 0 && cm.Covered > total {
			// 异构时点数据不强制 covered<=total：以 covered 为准（EffectiveTotal），
			// 标记"asset 盘点滞后"警告
			cm.Lagging = true
		}
		out = append(out, cm)
	}
	return out
}

// ---------------------------------------------------------------------
// 生产实现：internal/asset 资产盘点计数
// ---------------------------------------------------------------------

// assetModelProductMap model_uid 后缀 → 证书产品映射表（首期启发式）。
// 资产同步 model_uid 约定为 "{provider}_{type}"（见 cam/service/asset_sync.go
// 包注释，如 aliyun_cdn / tencent_clb）；LB 家族按 type 后缀细分，通用 lb 归
// clb（legacy 通用型），aws elb 归 alb（L7 默认）。映射口径随首批 PoC（5.12）校准。
var assetModelProductMap = map[string]domain.Product{
	"cdn":  domain.ProductCDN,
	"dcdn": domain.ProductDCDN,
	"waf":  domain.ProductWAF,
	"clb":  domain.ProductCLB,
	"slb":  domain.ProductCLB, // 阿里云 SLB=经典型负载均衡（CLB 旧称）
	"alb":  domain.ProductALB,
	"nlb":  domain.ProductNLB,
	"lb":   domain.ProductCLB, // 通用 lb 资产类型（auto_sync 默认清单）
	"elb":  domain.ProductALB, // AWS ELB 家族（L7 默认，PoC 校准）
}

// scanProviders 参与扫描的云清单（与 cert_references.cloud enum 对齐）。
var scanProviders = []domain.Cloud{
	domain.CloudAliyun, domain.CloudTencent, domain.CloudHuawei, domain.CloudAWS, domain.CloudAzure,
}

// assetRepositoryCounts AssetCountSource 生产实现：按候选 model_uid 逐项 Count
// asset 在用实例（独立于证书域维护的 ecam_instance 盘点集合）。
// 候选表未覆盖的自定义 model_uid 不计入（分母口径保守；配合 -1 失效规则兜底）。
type assetRepositoryCounts struct {
	instances assetrepo.InstanceRepository
}

// NewAssetRepositoryCounts 创建 asset 盘点计数器。
func NewAssetRepositoryCounts(instances assetrepo.InstanceRepository) AssetCountSource {
	return &assetRepositoryCounts{instances: instances}
}

// Counts 逐云逐候选 model_uid Count 聚合（任一查询失败 → 整体不可用 → error）。
func (a *assetRepositoryCounts) Counts(ctx context.Context) (map[CloudProductKey]int, error) {
	counts := make(map[CloudProductKey]int)
	for _, provider := range scanProviders {
		for suffix, product := range assetModelProductMap {
			modelUID := fmt.Sprintf("%s_%s", provider, suffix)
			n, err := a.instances.Count(ctx, assetdomain.InstanceFilter{ModelUID: modelUID})
			if err != nil {
				return nil, fmt.Errorf("cert: asset inventory count %s: %w", modelUID, err)
			}
			key := CloudProductKey{Cloud: provider, Product: product}
			counts[key] += int(n)
		}
	}
	return counts, nil
}
