package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	cmdbdomain "github.com/Havens-blog/e-cam-service/internal/cmdb/domain"
)

// ---- infer_env 公共环境推断测试：命名/tag 双信号、uat↔staging 对齐、code→env_id 映射 ----

// inferEnvRepoStub 嵌入接口，仅实现 List（命名避开同包既有 stub）
type inferEnvRepoStub struct {
	repository.EnvironmentRepository
	listFn func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error)
}

func (s *inferEnvRepoStub) List(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
	return s.listFn(ctx, filter)
}

func inferInst(name string, tags map[string]any) cmdbdomain.Instance {
	attrs := map[string]any{}
	if tags != nil {
		attrs["tags"] = tags
	}
	return cmdbdomain.Instance{AssetName: name, Attributes: attrs}
}

// TestInferEnvCodeByName 命名模式优先：-prod-/-uat-/-test-/-dev- 命中（大小写不敏感），未命中返回空。
func TestInferEnvCodeByName(t *testing.T) {
	tests := []struct {
		name      string
		assetName string
		wantCode  string
	}{
		{name: "命名 prod 命中", assetName: "rod-cpp-prod-app-01", wantCode: domain.EnvCodeProd},
		{name: "命名 uat 归一 staging", assetName: "rod-cpp-uat-web-01", wantCode: domain.EnvCodeStaging},
		{name: "命名 test 命中", assetName: "rod-cpp-test-web-01", wantCode: domain.EnvCodeTest},
		{name: "命名 dev 命中", assetName: "rod-cpp-dev-redis-01", wantCode: domain.EnvCodeDev},
		{name: "命名匹配大小写不敏感", assetName: "APP-Prod-Web-01", wantCode: domain.EnvCodeProd},
		{name: "命名无模式未命中", assetName: "web-01", wantCode: ""},
		{name: "fat 非标准码无信号", assetName: "rod-cpp-fat-web-01", wantCode: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := inferEnvCode(inferInst(tt.assetName, nil))
			// Assert
			if got != tt.wantCode {
				t.Errorf("inferEnvCode(%q) = %q, want %q", tt.assetName, got, tt.wantCode)
			}
		})
	}
}

// TestInferEnvCodeByTag tag 信号：environment 优先（实测 192 条）、env 兜底（历史兼容）。
func TestInferEnvCodeByTag(t *testing.T) {
	tests := []struct {
		name     string
		tags     map[string]any
		wantCode string
	}{
		{name: "tag.environment prod 命中", tags: map[string]any{"environment": "prod"}, wantCode: domain.EnvCodeProd},
		{name: "tag.env 兜底命中", tags: map[string]any{"env": "prod"}, wantCode: domain.EnvCodeProd},
		{name: "environment 优先于 env", tags: map[string]any{"environment": "dev", "env": "prod"}, wantCode: domain.EnvCodeDev},
		{name: "tag uat 归一 staging", tags: map[string]any{"environment": "uat"}, wantCode: domain.EnvCodeStaging},
		{name: "tag fat 非标准码无信号", tags: map[string]any{"environment": "fat"}, wantCode: ""},
		{name: "tag 空值无信号", tags: map[string]any{"environment": ""}, wantCode: ""},
		{name: "无 tags 无信号", tags: nil, wantCode: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := inferEnvCode(inferInst("web-01", tt.tags))
			// Assert
			if got != tt.wantCode {
				t.Errorf("inferEnvCode(tags=%v) = %q, want %q", tt.tags, got, tt.wantCode)
			}
		})
	}
}

// TestInferEnvCodeTagFallbackAfterName 命名命中时 tag 不参与（命名优先）；
// 命名未命中才读 tag。
func TestInferEnvCodeTagFallbackAfterName(t *testing.T) {
	// Arrange
	inst := inferInst("app-prod-web-01", map[string]any{"environment": "dev"})

	// Act
	got := inferEnvCode(inst)

	// Assert
	if got != domain.EnvCodeProd {
		t.Errorf("命名优先应得 prod, got %q", got)
	}
}

// TestEnvCodeAlignment uat↔staging 对齐：infer canonical staging 与租户 code uat
// 经 normalizeEnvCode 归一后等价。
func TestEnvCodeAlignment(t *testing.T) {
	// Arrange
	tenantCode := "uat"
	inferred := domain.EnvCodeStaging // -uat- 命名推断结果

	// Act / Assert
	if normalizeEnvCode(tenantCode) != normalizeEnvCode(inferred) {
		t.Errorf("uat 与 staging 应归一为等价: %q vs %q", normalizeEnvCode(tenantCode), normalizeEnvCode(inferred))
	}
	if normalizeEnvCode("UAT") != domain.EnvCodeStaging {
		t.Errorf("归一化应大小写不敏感, got %q", normalizeEnvCode("UAT"))
	}
	if normalizeEnvCode(" staging ") != domain.EnvCodeStaging {
		t.Errorf("归一化应去空白, got %q", normalizeEnvCode(" staging "))
	}
	if isStandardEnvCode("fat") {
		t.Error("fat 不应视为标准环境码")
	}
}

// TestLoadEnvIDByCode code→env_id 映射：租户 code 经 normalizeEnvCode 归一后建反向 map，
// infer 的 staging 可直接查到 uat 环境的 ID。
func TestLoadEnvIDByCode(t *testing.T) {
	// Arrange
	envRepo := &inferEnvRepoStub{
		listFn: func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
			return []domain.Environment{
				{ID: 5, Code: "dev"},
				{ID: 8, Code: "uat"},
				{ID: 9, Code: "prod"},
			}, nil
		},
	}

	// Act
	m, err := loadEnvIDByCode(context.Background(), envRepo, 3)

	// Assert
	if err != nil {
		t.Fatalf("loadEnvIDByCode() error = %v", err)
	}
	if m[domain.EnvCodeDev] != 5 {
		t.Errorf("dev → %d, want 5", m[domain.EnvCodeDev])
	}
	// uat 环境应以归一后的 staging 键可达
	if m[domain.EnvCodeStaging] != 8 {
		t.Errorf("staging(归一自 uat) → %d, want 8", m[domain.EnvCodeStaging])
	}
	if m[domain.EnvCodeProd] != 9 {
		t.Errorf("prod → %d, want 9", m[domain.EnvCodeProd])
	}
}

// TestLoadEnvIDByCode_QueryError 环境列表查询失败时错误透传。
func TestLoadEnvIDByCode_QueryError(t *testing.T) {
	// Arrange
	envRepo := &inferEnvRepoStub{
		listFn: func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
			return nil, context.DeadlineExceeded
		},
	}

	// Act
	_, err := loadEnvIDByCode(context.Background(), envRepo, 3)

	// Assert
	if err == nil {
		t.Fatal("查询失败应返回错误")
	}
}
