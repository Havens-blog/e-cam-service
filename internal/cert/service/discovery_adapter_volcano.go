package service

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	volcanocert "github.com/Havens-blog/e-cloudx-sdk/volcano"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// 火山引擎发现导入材料适配（cert-volcano-import-sync 任务 2）：任务 1 的
// cloudx/volcano CertAdapter（List/Get 只读证书库端口）经既有 discoveryCertAdapter
// shim 包装为 DiscoveryCertAdapter，与五云同管线（幂等台账、映射补建、
// ALREADY_IN_LEDGER 全复用）。包装层仅做端口形态适配（GetCertificate 实例
// 材料 → DiscoveryCertMaterial），云侧 List/Get 逻辑全部在 cloudx 适配器内，
// 不复制适配逻辑（Hard Rule）。

// discoveryCloudVolcano 火山云发现导入云标识：cert/domain.Cloud 枚举未含火山
// （历史五云口径），经 shared/domain 账号 provider 常量转译同值 "volcano"
// （发现导入条目 cloud 口径与账号 provider 枚举一致）。
const discoveryCloudVolcano = domain.Cloud(sharedomain.CloudProviderVolcano)

// 编译期接口断言：通用 shim 满足发现导入材料端口（火山包装经 shim 承载）。
var _ DiscoveryCertAdapter = discoveryCertAdapter{}

// NewVolcanoDiscoveryCertAdapter 火山引擎导入材料适配（certificateservice
// CertificateGetInstance 链下载通道；链经适配层构造性净化为仅 CERTIFICATE 块，
// 私钥材料在 cloudx 适配层即不进入返回结构）。与五云构造器同构：
// service.NewVolcanoDiscoveryCertAdapter(volcanocert.NewCertAdapter(logger))。
func NewVolcanoDiscoveryCertAdapter(a *volcanocert.CertAdapter) DiscoveryCertAdapter {
	return discoveryCertAdapter{
		cloud: discoveryCloudVolcano,
		getChain: func(ctx context.Context, creds *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
			// 带私钥通道：火山 certificateservice 响应携带 PrivateKey（PEM 原文，
			// 实测标准 PKCS#1）——导入侧经校验+信封加密落库实现「导入即完整托管」
			// （部署器期「csv 私钥不可再导出」裁决已修正，见 cloudx/volcano/cert.go）。
			// 私钥仅在本材料字段内流转，导入侧即时加密并 Zeroize。
			m, err := a.GetCertificateWithKey(ctx, creds, cloudCertID)
			return volcanoCertMaterial(m, err)
		},
	}
}

// volcanoCertMaterial 云侧证书材料（含私钥）→ 导入材料端口形态（端口适配
// 映射，纯函数）：火山证书库无"在库但不存在"的独立返回态（不存在即 API
// 错误），成功即 Exists；错误（含 ErrCertFiltered 过滤哨兵——revoked/非
// Issued 实例）原样透传不伪造材料，调用方按通用 CERT_GET_FAILED 口径记因。
// PrivateKeyPEM 仅在本材料字段内流转，导入侧即时校验+信封加密落库并 Zeroize。
func volcanoCertMaterial(m volcanocert.CloudCertKeyMaterial, err error) (DiscoveryCertMaterial, error) {
	if err != nil {
		return DiscoveryCertMaterial{}, err
	}
	return DiscoveryCertMaterial{
		Exists:        true,
		CertChainPEM:  m.CertChainPEM,
		PrivateKeyPEM: m.PrivateKeyPEM,
	}, nil
}
