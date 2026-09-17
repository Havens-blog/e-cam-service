package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/volcano"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
)

// 编译期接口断言（AC-1）：火山包装构造器返回值满足发现导入材料端口。
var _ DiscoveryCertAdapter = NewVolcanoDiscoveryCertAdapter(nil)

// TestVolcanoDiscoveryCertAdapter_CloudPort 火山包装云标识（对齐五云 shim 冒烟
// 口径 TestDiscoveryImport_AdapterShims）：cert/domain.Cloud 未枚举火山（历史
// 五云），云值经 shared/domain 账号 provider 常量转译（"volcano"，同值同源）。
func TestVolcanoDiscoveryCertAdapter_CloudPort(t *testing.T) {
	a := NewVolcanoDiscoveryCertAdapter(volcano.NewCertAdapter(elog.DefaultLogger))
	assert.Equal(t, domain.Cloud(sharedomain.CloudProviderVolcano), a.Cloud())
}

// TestVolcanoDiscoveryCertAdapter_GetCertChainErrorPassthrough 错误透传（免网络）：
// nil 凭证在 cloudx 适配器入口即报错（不发起云 API 调用），端口返回零值材料 +
// 错误原样透传——不伪造 Exists/材料（不存在/过滤态语义均由错误承载）。
func TestVolcanoDiscoveryCertAdapter_GetCertChainErrorPassthrough(t *testing.T) {
	a := NewVolcanoDiscoveryCertAdapter(volcano.NewCertAdapter(elog.DefaultLogger))
	material, err := a.GetCertChain(context.Background(), nil, "inst-1")
	assert.Error(t, err)
	assert.Equal(t, DiscoveryCertMaterial{}, material)
}

// TestVolcanoCertMaterial_Mapping 端口映射纯函数：成功 → Exists + 链透传（链
// 净化在 cloudx 适配层构造性完成，包装层不重复处理）；错误（含 ErrCertFiltered
// 过滤哨兵）→ 零值材料 + 错误保持（调用方 errors.Is 可识别过滤态）。
func TestVolcanoCertMaterial_Mapping(t *testing.T) {
	t.Run("成功映射", func(t *testing.T) {
		inst := volcano.CloudCertInstance{CloudCertID: "inst-1", CertChainPEM: "chain-pem"}
		material, err := volcanoCertMaterial(inst, nil)
		assert.NoError(t, err)
		assert.Equal(t, DiscoveryCertMaterial{Exists: true, CertChainPEM: "chain-pem"}, material)
	})
	t.Run("过滤哨兵透传", func(t *testing.T) {
		filtered := fmt.Errorf("%w: instance inst-1", volcano.ErrCertFiltered)
		material, err := volcanoCertMaterial(volcano.CloudCertInstance{}, filtered)
		assert.ErrorIs(t, err, volcano.ErrCertFiltered)
		assert.Equal(t, DiscoveryCertMaterial{}, material)
	})
}
