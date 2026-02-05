package domain

import "errors"

// 服务树相关错误
var (
	// 节点错误
	ErrNodeNotFound      = errors.New("服务树节点不存在")
	ErrNodeNameEmpty     = errors.New("节点名称不能为空")
	ErrNodeHasChildren   = errors.New("节点下存在子节点，无法删除")
	ErrNodeHasBindings   = errors.New("节点下存在绑定资源，无法删除")
	ErrNodeUIDExists     = errors.New("节点UID已存在")
	ErrNodeParentInvalid = errors.New("父节点无效")
	ErrNodeCyclicRef     = errors.New("不能将节点移动到其子节点下")

	// 环境错误
	ErrEnvNotFound    = errors.New("环境不存在")
	ErrEnvCodeExists  = errors.New("环境代码已存在")
	ErrEnvHasBindings = errors.New("环境下存在绑定资源，无法删除")

	// 绑定错误
	ErrBindingNotFound     = errors.New("绑定关系不存在")
	ErrBindingExists       = errors.New("资源已绑定到其他节点")
	ErrResourceNotFound    = errors.New("资源不存在")
	ErrInvalidResourceType = errors.New("无效的资源类型")

	// 规则错误
	ErrRuleNotFound        = errors.New("规则不存在")
	ErrRuleNameEmpty       = errors.New("规则名称不能为空")
	ErrRuleNodeIDEmpty     = errors.New("规则目标节点不能为空")
	ErrRuleTenantIDEmpty   = errors.New("规则租户ID不能为空")
	ErrRuleConditionsEmpty = errors.New("规则条件不能为空")
	ErrInvalidOperator     = errors.New("无效的操作符")
)
