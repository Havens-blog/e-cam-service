package service

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

type crdRegFixture struct {
	svc  CrdRegistrationService
	regs *certtest.FakeCrdRegistrationRepo
}

func newCrdRegFixture(t *testing.T) *crdRegFixture {
	t.Helper()
	regs := certtest.NewFakeCrdRegistrationRepo()
	return &crdRegFixture{svc: NewCrdRegistrationService(regs), regs: regs}
}

func TestRegisterHappyPath(t *testing.T) {
	fx := newCrdRegFixture(t)

	view, err := fx.svc.Register(t.Context(), RegisterCrdInput{
		ClusterID:     "prod-cluster",
		APIGroup:      "gateway.example.com",
		Kind:          "CustomGateway",
		CertFieldPath: "spec.certs[].cloudCertId",
		Operator:      "alice",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, view.ID)
	assert.Equal(t, "prod-cluster", view.ClusterID)
	assert.Equal(t, "gateway.example.com", view.APIGroup)
	assert.Equal(t, "CustomGateway", view.Kind)
	assert.Equal(t, "spec.certs[].cloudCertId", view.CertFieldPath)
	assert.True(t, view.Enabled, "登记默认 enabled=true（随扫描范围生效）")
	assert.False(t, view.Builtin)
	assert.Equal(t, "alice", view.Operator)
	assert.False(t, view.CreatedAt.IsZero())
}

func TestRegisterValidation(t *testing.T) {
	fx := newCrdRegFixture(t)

	cases := []struct {
		name string
		in   RegisterCrdInput
		want error
	}{
		{
			name: "empty clusterId",
			in:   RegisterCrdInput{Kind: "K", CertFieldPath: "spec.a"},
		},
		{
			name: "empty kind",
			in:   RegisterCrdInput{ClusterID: "c1", CertFieldPath: "spec.a"},
		},
		{
			name: "invalid certFieldPath empty segment",
			in:   RegisterCrdInput{ClusterID: "c1", Kind: "K", CertFieldPath: "spec..a"},
			want: k8s.ErrInvalidCertFieldPath,
		},
		{
			name: "invalid certFieldPath index subscript",
			in:   RegisterCrdInput{ClusterID: "c1", Kind: "K", CertFieldPath: "spec.certs[0].id"},
			want: k8s.ErrInvalidCertFieldPath,
		},
		{
			name: "builtin shadow ingress",
			in:   RegisterCrdInput{ClusterID: "c1", APIGroup: "networking.k8s.io", Kind: "Ingress", CertFieldPath: "spec.tls[].secretName"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fx.svc.Register(t.Context(), tc.in)
			require.Error(t, err)
			if tc.want != nil {
				assert.ErrorIs(t, err, tc.want)
				assert.Contains(t, err.Error(), tc.in.CertFieldPath, "certFieldPath 拒绝需可读错误信息")
			}
			regs, err := fx.regs.List(t.Context())
			require.NoError(t, err)
			assert.Empty(t, regs, "校验失败不得落库")
		})
	}
}

func TestRegisterDuplicateConflict(t *testing.T) {
	fx := newCrdRegFixture(t)
	in := RegisterCrdInput{
		ClusterID: "c1", APIGroup: "gateway.example.com", Kind: "CustomGateway",
		CertFieldPath: "spec.certs[].cloudCertId", Operator: "alice",
	}
	_, err := fx.svc.Register(t.Context(), in)
	require.NoError(t, err)

	// clusterId+apiGroup+kind 唯一冲突哨兵（供 4.5 映射 409）
	_, err = fx.svc.Register(t.Context(), in)
	assert.ErrorIs(t, err, domain.ErrDuplicateCrdRegistration)

	// 同集群不同 kind / 不同集群同 kind 均允许
	_, err = fx.svc.Register(t.Context(), RegisterCrdInput{
		ClusterID: "c1", APIGroup: "gateway.example.com", Kind: "OtherGateway",
		CertFieldPath: "spec.certs[].cloudCertId",
	})
	assert.NoError(t, err)
	_, err = fx.svc.Register(t.Context(), RegisterCrdInput{
		ClusterID: "c2", APIGroup: "gateway.example.com", Kind: "CustomGateway",
		CertFieldPath: "spec.certs[].cloudCertId",
	})
	assert.NoError(t, err)
}

func TestListBuiltinFlag(t *testing.T) {
	fx := newCrdRegFixture(t)
	require.NoError(t, fx.svc.EnsureBuiltinRegistrations(t.Context(), "c1"))
	_, err := fx.svc.Register(t.Context(), RegisterCrdInput{
		ClusterID: "c1", APIGroup: "gateway.example.com", Kind: "CustomGateway",
		CertFieldPath: "spec.certs[].cloudCertId",
	})
	require.NoError(t, err)

	views, err := fx.svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, views, 5)
	builtinCount := 0
	for _, v := range views {
		if v.Builtin {
			builtinCount++
		}
	}
	assert.Equal(t, 4, builtinCount, "四类内置标记 Builtin=true")
}

func TestSetEnabledToggle(t *testing.T) {
	fx := newCrdRegFixture(t)
	view, err := fx.svc.Register(t.Context(), RegisterCrdInput{
		ClusterID: "c1", APIGroup: "gateway.example.com", Kind: "CustomGateway",
		CertFieldPath: "spec.certs[].cloudCertId",
	})
	require.NoError(t, err)

	// 停用：该 CRD 回归盲区（ListEnabled 不再返回）
	require.NoError(t, fx.svc.SetEnabled(t.Context(), view.ID, false))
	enabled, err := fx.regs.ListEnabled(t.Context())
	require.NoError(t, err)
	assert.Empty(t, enabled)

	// 重新启用：纳入扫描范围
	require.NoError(t, fx.svc.SetEnabled(t.Context(), view.ID, true))
	enabled, err = fx.regs.ListEnabled(t.Context())
	require.NoError(t, err)
	assert.Len(t, enabled, 1)

	// 未命中 / 非法 ID
	err = fx.svc.SetEnabled(t.Context(), "000000000000000000000000", true)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	err = fx.svc.SetEnabled(t.Context(), "not-hex", true)
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestDeleteCustomRegistration(t *testing.T) {
	fx := newCrdRegFixture(t)
	view, err := fx.svc.Register(t.Context(), RegisterCrdInput{
		ClusterID: "c1", APIGroup: "gateway.example.com", Kind: "CustomGateway",
		CertFieldPath: "spec.certs[].cloudCertId",
	})
	require.NoError(t, err)

	require.NoError(t, fx.svc.Delete(t.Context(), view.ID))
	regs, err := fx.regs.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, regs)

	err = fx.svc.Delete(t.Context(), view.ID)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	err = fx.svc.Delete(t.Context(), "not-hex")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestDeleteBuiltinBlocked(t *testing.T) {
	fx := newCrdRegFixture(t)
	require.NoError(t, fx.svc.EnsureBuiltinRegistrations(t.Context(), "c1"))

	views, err := fx.svc.List(t.Context())
	require.NoError(t, err)
	var ingressID string
	for _, v := range views {
		if v.Kind == "Ingress" && v.APIGroup == "networking.k8s.io" {
			ingressID = v.ID
		}
	}
	require.NotEmpty(t, ingressID)

	err = fx.svc.Delete(t.Context(), ingressID)
	require.ErrorIs(t, err, domain.ErrBuiltinCrdRegistration, "内置登记删除给出明确错误")
	assert.Contains(t, err.Error(), "networking.k8s.io/Ingress", "错误指明具体内置项")

	// 行未删除，登记保持 enabled
	regs, err := fx.regs.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, regs, 4)
}

func TestEnsureBuiltinRegistrationsIdempotent(t *testing.T) {
	fx := newCrdRegFixture(t)

	require.NoError(t, fx.svc.EnsureBuiltinRegistrations(t.Context(), "c1"))
	require.NoError(t, fx.svc.EnsureBuiltinRegistrations(t.Context(), "c1"), "重复初始化幂等")
	require.NoError(t, fx.svc.EnsureBuiltinRegistrations(t.Context(), "c1"))

	regs, err := fx.regs.List(t.Context())
	require.NoError(t, err)
	require.Len(t, regs, 4, "三次初始化仍为四行（不重复）")

	want := map[string]bool{}
	for _, b := range k8s.BuiltinRegistrations {
		want[b.Kind] = true
	}
	for _, r := range regs {
		assert.True(t, want[r.Kind], "登记项与固定枚举一致: %s", r.Kind)
		assert.Equal(t, "c1", r.ClusterID)
		assert.Equal(t, "system", r.Operator)
		assert.True(t, r.Enabled, "内置登记 enabled=true")
		for _, b := range k8s.BuiltinRegistrations {
			if b.Kind == r.Kind {
				assert.Equal(t, b.APIGroup, r.APIGroup)
				assert.Equal(t, b.CertFieldPath, r.CertFieldPath)
			}
		}
	}

	// 空集群 ID 拒绝
	err = fx.svc.EnsureBuiltinRegistrations(t.Context(), "  ")
	require.Error(t, err)
}

// TestEnsureBuiltinPreservesDisabledState 初始化幂等不覆盖人工停用决策。
func TestEnsureBuiltinPreservesDisabledState(t *testing.T) {
	fx := newCrdRegFixture(t)
	require.NoError(t, fx.svc.EnsureBuiltinRegistrations(t.Context(), "c1"))

	views, err := fx.svc.List(t.Context())
	require.NoError(t, err)
	var gatewayID string
	for _, v := range views {
		if v.Kind == "Gateway" {
			gatewayID = v.ID
		}
	}
	require.NoError(t, fx.svc.SetEnabled(t.Context(), gatewayID, false))

	require.NoError(t, fx.svc.EnsureBuiltinRegistrations(t.Context(), "c1"))
	regs, err := fx.regs.List(t.Context())
	require.NoError(t, err)
	require.Len(t, regs, 4)
	for _, r := range regs {
		if r.Kind == "Gateway" {
			assert.False(t, r.Enabled, "重跑初始化不重新启用已停用项")
		}
	}
}
