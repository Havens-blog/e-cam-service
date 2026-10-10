package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/port"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
)

// 环境推断的单份公共实现（SC：单份实现的根）：
// 绑定落库（ExecuteRules）、历史改绑（PreviewRebind/ApplyRebind）、
// 概览疑异（GetNodeAssetSummary）三方共用，禁止复制第二份。

// envNamePatterns 资产命名模式 → 标准环境代码（二期提案：-prod-/-uat-/-test-/-dev-）
var envNamePatterns = []struct {
	pattern string
	code    string
}{
	{"-prod-", domain.EnvCodeProd},
	{"-uat-", domain.EnvCodeStaging}, // uat 归一化为预发
	{"-test-", domain.EnvCodeTest},
	{"-dev-", domain.EnvCodeDev},
}

// normalizeEnvCode 环境值归一化：小写化，uat 视作 staging。
// 推断 canonical 码（dev/test/staging/prod）与租户环境表 code（dev/test/uat/prod）
// 双方经本函数归一后等价对齐（uat ≡ staging）。
func normalizeEnvCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "uat" {
		return domain.EnvCodeStaging
	}
	return code
}

// matchNameEnvCode 命名模式匹配环境代码（大小写不敏感），无命中返回空
func matchNameEnvCode(assetName string) string {
	lower := strings.ToLower(assetName)
	for _, p := range envNamePatterns {
		if strings.Contains(lower, p.pattern) {
			return p.code
		}
	}
	return ""
}

// isStandardEnvCode 是否为标准环境代码（dev/test/staging/prod）
func isStandardEnvCode(code string) bool {
	switch code {
	case domain.EnvCodeDev, domain.EnvCodeTest, domain.EnvCodeStaging, domain.EnvCodeProd:
		return true
	}
	return false
}

// extractTagEnv 取实例标签环境值：environment 优先、env 兜底。
// 实测标签键为 environment（192 条），env 键 0 条——env 仅为历史兼容保留。
func extractTagEnv(inst port.CMDBInstance) string {
	tags, ok := inst.Attributes["tags"].(map[string]any)
	if !ok {
		return ""
	}
	if val, ok := tags["environment"].(string); ok && val != "" {
		return val
	}
	val, _ := tags["env"].(string)
	return val
}

// inferEnvCode 公共环境推断入口：命名模式（-prod-/-uat-/-test-/-dev-）优先，
// 其次 tag.environment（兼容 tag.env）；返回 canonical 码 dev/test/staging/prod。
// fat 等非标准码与无信号均返回空串，调用方走规则 env 兜底，不强行归类。
func inferEnvCode(inst port.CMDBInstance) string {
	if code := matchNameEnvCode(inst.AssetName); code != "" {
		return code
	}
	tagCode := normalizeEnvCode(extractTagEnv(inst))
	if isStandardEnvCode(tagCode) {
		return tagCode
	}
	return ""
}

// detectEnvMismatch 环境错绑双信号检测：资产命名模式 + tag 环境与绑定环境矛盾。
// 仅当绑定环境代码已知且信号指向标准环境代码时判定；信号或绑定环境未知不误报。
// 返回矛盾原因列表（只提示不改绑，调用方自行决定展示）。
// 与 inferEnvCode 的差异：本函数逐信号独立报告（供概览展示），不做命名优先折叠。
func detectEnvMismatch(assetName, tagEnv, boundCode string) []string {
	if boundCode == "" {
		return nil
	}
	bound := normalizeEnvCode(boundCode)

	var reasons []string
	if nameCode := matchNameEnvCode(assetName); nameCode != "" && nameCode != bound {
		reasons = append(reasons, fmt.Sprintf("资产命名含 -%s- 与绑定环境 %s 矛盾", nameCode, bound))
	}
	tagCode := normalizeEnvCode(tagEnv)
	if isStandardEnvCode(tagCode) && tagCode != bound {
		reasons = append(reasons, fmt.Sprintf("tag.env=%s 与绑定环境 %s 矛盾", tagCode, bound))
	}
	return reasons
}

// loadEnvCodeMap 加载租户环境 ID → 代码映射（概览分布与错绑判定按环境代码口径）
func loadEnvCodeMap(ctx context.Context, envRepo repository.EnvironmentRepository, tenantID int64) (map[int64]string, error) {
	envs, err := envRepo.List(ctx, domain.EnvironmentFilter{TenantID: tenantID})
	if err != nil {
		return nil, fmt.Errorf("查询环境列表失败: %w", err)
	}
	m := make(map[int64]string, len(envs))
	for _, e := range envs {
		m[e.ID] = e.Code
	}
	return m, nil
}

// loadEnvIDByCode 加载租户归一化环境码 → 环境 ID 映射（code → env_id），
// 供绑定落库把 inferEnvCode 结果解析成租户环境 ID：双方 code 均经
// normalizeEnvCode 归一（uat ≡ staging）后直接查表；同码多环境先到先得。
func loadEnvIDByCode(ctx context.Context, envRepo repository.EnvironmentRepository, tenantID int64) (map[string]int64, error) {
	envs, err := envRepo.List(ctx, domain.EnvironmentFilter{TenantID: tenantID})
	if err != nil {
		return nil, fmt.Errorf("查询环境列表失败: %w", err)
	}
	m := make(map[string]int64, len(envs))
	for _, e := range envs {
		code := normalizeEnvCode(e.Code)
		if code == "" {
			continue
		}
		if _, exists := m[code]; !exists {
			m[code] = e.ID
		}
	}
	return m, nil
}
