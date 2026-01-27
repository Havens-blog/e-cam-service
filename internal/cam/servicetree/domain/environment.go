package domain

import (
	"fmt"
	"time"
)

// 预定义环境代码
const (
	EnvCodeDev     = "dev"     // 开发环境
	EnvCodeTest    = "test"    // 测试环境
	EnvCodeStaging = "staging" // 预发环境
	EnvCodeProd    = "prod"    // 生产环境
)

// EnvStatus 环境状态
const (
	EnvStatusEnabled  = 1 // 启用
	EnvStatusDisabled = 0 // 禁用
)

// Environment 环境领域模型
type Environment struct {
	ID          int64  // 环境ID
	Code        string // 环境代码 (dev/test/staging/prod)
	Name        string // 环境名称
	TenantID    string // 租户ID
	Description string // 描述
	Color       string // 显示颜色 (用于前端区分)
	Order       int    // 排序权重
	Status      int    // 状态
	CreateTime  time.Time
	UpdateTime  time.Time
}

// Validate 验证环境数据
func (e *Environment) Validate() error {
	if e.Code == "" {
		return fmt.Errorf("环境代码不能为空")
	}
	if e.Name == "" {
		return fmt.Errorf("环境名称不能为空")
	}
	if e.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}
	return nil
}

// IsProduction 是否为生产环境
func (e *Environment) IsProduction() bool {
	return e.Code == EnvCodeProd
}

// EnvironmentFilter 环境过滤条件
type EnvironmentFilter struct {
	TenantID string
	Code     string
	Status   *int
	Offset   int64
	Limit    int64
}

// DefaultEnvironments 默认环境列表
func DefaultEnvironments(tenantID string) []Environment {
	return []Environment{
		{Code: EnvCodeDev, Name: "开发环境", TenantID: tenantID, Color: "#52c41a", Order: 1, Status: EnvStatusEnabled},
		{Code: EnvCodeTest, Name: "测试环境", TenantID: tenantID, Color: "#1890ff", Order: 2, Status: EnvStatusEnabled},
		{Code: EnvCodeStaging, Name: "预发环境", TenantID: tenantID, Color: "#faad14", Order: 3, Status: EnvStatusEnabled},
		{Code: EnvCodeProd, Name: "生产环境", TenantID: tenantID, Color: "#f5222d", Order: 4, Status: EnvStatusEnabled},
	}
}
