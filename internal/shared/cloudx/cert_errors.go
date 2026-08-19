package cloudx

import "errors"

// 证书域跨云公共错误定义（tech-design Error Handling / Error Types & Codes）。
// 供 5.3 CloudAPIChannel 以统一 CloudDeployer 抽象消费各云适配层的限流信号。

var (
	// ErrCloudRateLimited 云 API 限流/流控哨兵（CLOUD_API_RATELIMITED 语义）。
	// 各云适配层将云侧限流错误包装为本哨兵（%w），限流退避与重试策略属变更执行编排层；
	// HTTP 语境映射 503，异步子任务语境落 ChangeItem.status=rate_limited。
	ErrCloudRateLimited = errors.New("cloud api rate limited")

	// ErrDiscoveryOnly 只读发现哨兵（ERR_DISCOVERY_ONLY 语义，任务 3.3）。
	// 华为云/AWS/Azure 首期无部署器（PRD Out of Scope：三云部署器二期），
	// discovery-only 适配的 UploadCert/BindResource/CleanupOrphan 一律返回本哨兵，
	// 不产生任何云侧写操作；三云引用进入变更清单时为不可执行项
	// （AutoChangeable=false + Reason=ERR_DISCOVERY_ONLY，5.2 处理）。
	ErrDiscoveryOnly = errors.New("cloud deployer is discovery-only")
)
