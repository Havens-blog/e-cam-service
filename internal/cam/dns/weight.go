package dns

import sdktypes "github.com/Havens-blog/e-cloudx-sdk/types"

// DNS 权重能力矩阵（单一事实源在 SDK types，此处仅做服务层透出）。
//
// 为什么需要透出：前端表单必须知道"该云能否写权重"，否则会出现能填但云端推不上去的
// 假表单；把判定收敛到 SDK（types.DNSWeightWritable/DNSWeightSupported），服务层与
// 前端接口都从这里取，避免前后端各写一份映射导致漂移。
//
// 现状（2026-10-29 核对各云 vendored SDK）：
//   - aliyun：可读（DescribeDomainRecords.Record.Weight），不可写（Add/UpdateDomainRecord 无 Weight）；
//   - huawei / volcano：不支持；
//   - tencent / aws：写能力待核实，SDK 侧暂返回 false。
func DNSWeightProviderWritable(provider string) bool {
	return sdktypes.DNSWeightWritable(provider)
}

// DNSWeightProviderSupported 该云是否可能带权重（读取侧口径）。
func DNSWeightProviderSupported(provider string) bool {
	return sdktypes.DNSWeightSupported(provider)
}
