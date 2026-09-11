// Package providers 是多云厂商实现包的唯一注册入口（import manifest）。
//
// 各厂商包通过 init() 向四张能力注册表自注册：
//   - 资产 CloudAdapter：internal/shared/cloudx/registry.go（RegisterAdapter）
//   - 计费 BillingAdapter：internal/shared/cloudx/billing/registry.go（RegisterBillingAdapter）
//   - 日志 LogProvider：internal/shared/cloudx/logquery/registry.go（RegisterProvider）
//   - 云 IAM：internal/shared/cloudx/iam/registry.go（RegisterIAMAdapter）
//
// 新增云厂商：在对应实现包写 init() 注册，然后在本文件补一行 blank import。
// 业务代码不得再自行 blank import 厂商包（历史分散清单已收敛至此）。
//
// cert 部署通道（internal/cert/deployer）为显式 DI 注册、凭证形态不同，
// 不在本 manifest 管辖内。azure 目前仅证书发现（无 Adapter 注册），
// 接入后在此补 import。
package providers

import (
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/volcano"

	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/aliyun"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/aws"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/huawei"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/tencent"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/volcano"

	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/aliyun"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/aws"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/huawei"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/tencent"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/volcano"

	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery/aliyun"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery/aws"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery/huawei"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery/tencent"
)
