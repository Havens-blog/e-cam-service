package service

import (
	"context"
	"fmt"

	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/gotomicro/ego/core/elog"
)

// PreviewRebind 改绑计划预览：重算所有 rule 绑定的资产，找出应按更高优先级规则改绑的候选。
// 只读不落库。manual 绑定锁定不动；rule 绑定按当前启用规则（优先级升序）重匹配：
// 目标节点取规则、目标环境按资产推断重算（resolveEnvID，未命中沿用现绑），
// 节点或环境与现绑不同则该资源入候选（改绑到新目标）。
//
// 与 ExecuteRules 的区别：ExecuteRules 对已绑定资产整体跳过（幂等 + 手动优先 locked），
// 因此新增/调整规则后既有 rule 绑定不会自动收敛——本方法正是补这一缺口，且改绑只在
// 用户确认后落库（ApplyRebind），同步后自动触发的 ExecuteRules 保持 create-only 不静默改绑。
func (s *ruleEngineService) PreviewRebind(ctx context.Context, tenantID int64) (*stdomain.RebindPlan, error) {
	candidates, err := s.computeRebindCandidates(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &stdomain.RebindPlan{Items: candidates, Total: int64(len(candidates))}, nil
}

// ApplyRebind 确认改绑：按请求中的资源 ID 应用改绑（重新计算目标后落库，天然幂等）。
// 返回实际改绑条数。请求中的 ID 若非候选（可能已被并发改绑或非 rule 绑定）则跳过，不报错。
func (s *ruleEngineService) ApplyRebind(ctx context.Context, tenantID int64, resourceIDs []int64) (int64, error) {
	byID, err := s.computeRebindCandidates(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	candidates := make(map[int64]stdomain.RebindCandidate, len(byID))
	for _, c := range byID {
		candidates[c.ResourceID] = c
	}

	// 绑定 ID 映射（UpdateTarget 按绑定记录 ID 定位，而非资源 ID）
	bindings, err := s.bindingRepo.List(ctx, stdomain.BindingFilter{
		TenantID:     tenantID,
		ResourceType: stdomain.ResourceTypeInstance,
		Limit:        100000,
	})
	if err != nil {
		return 0, fmt.Errorf("获取绑定记录失败: %w", err)
	}
	bindingIDByResource := make(map[int64]int64, len(bindings))
	for _, b := range bindings {
		bindingIDByResource[b.ResourceID] = b.ID
	}

	applied := int64(0)
	for _, rid := range resourceIDs {
		c, ok := candidates[rid]
		if !ok {
			continue // 非候选（可能已改绑/非 rule 绑定/并发变更），跳过
		}
		bindingID, ok := bindingIDByResource[rid]
		if !ok || bindingID == 0 {
			s.logger.Warn("改绑目标绑定记录不存在", elog.Int64("resourceID", rid))
			continue
		}
		if err := s.bindingRepo.UpdateTarget(ctx, bindingID, c.ToNodeID, c.ToEnvID, c.ToRuleID); err != nil {
			// 单条失败不阻塞整体，仅告警（与 ExecuteRules 统计落库容错口径一致）
			s.logger.Warn("改绑失败", elog.Int64("resourceID", rid), elog.FieldErr(err))
			continue
		}
		applied++
	}
	return applied, nil
}

// computeRebindCandidates 计算改绑候选（预览与应用共用），并回填 from/to 节点名。
func (s *ruleEngineService) computeRebindCandidates(ctx context.Context, tenantID int64) ([]stdomain.RebindCandidate, error) {
	rules, err := s.ruleRepo.ListEnabled(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取规则列表失败: %w", err)
	}
	instances, err := s.listTenantInstances(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取实例列表失败: %w", err)
	}
	bindings, err := s.bindingRepo.List(ctx, stdomain.BindingFilter{
		TenantID:     tenantID,
		ResourceType: stdomain.ResourceTypeInstance,
		Limit:        100000,
	})
	if err != nil {
		return nil, fmt.Errorf("获取已绑定资源失败: %w", err)
	}
	bindingByResource := make(map[int64]stdomain.ResourceBinding, len(bindings))
	for _, b := range bindings {
		bindingByResource[b.ResourceID] = b
	}

	// 加载租户环境码 → env_id 映射（与 ExecuteRules 同口径的降级：envRepo 未注入或加载
	// 失败退回空映射，推断全部走现绑兜底，改绑退化为「仅节点变化」的旧行为，不阻塞预览）
	envIDByCode := make(map[string]int64)
	if s.envRepo != nil {
		m, err := loadEnvIDByCode(ctx, s.envRepo, tenantID)
		if err != nil {
			s.logger.Warn("加载租户环境码映射失败，改绑环境退回现绑兜底",
				elog.Int64("tenantID", tenantID), elog.FieldErr(err))
		} else {
			envIDByCode = m
		}
	}

	candidates := make([]stdomain.RebindCandidate, 0)
	for _, inst := range instances {
		cur, ok := bindingByResource[inst.ID]
		if !ok || cur.BindType != stdomain.BindTypeRule {
			continue // 未绑定或 manual 锁定，不参与改绑
		}
		// 重匹配：规则已按优先级升序，首个命中即最高优先级
		for _, rule := range rules {
			if !s.matchRule(inst, rule) {
				continue
			}
			// 目标环境与节点同走重算口径：资产推断优先（resolveEnvID 查租户环境码映射）；
			// 推断未命中（无信号/非标准码/码未映射）沿用**当前绑定 env** 而非规则 env——
			// 避免改绑把无信号资产的环境改成规则写死值（历史 691 条错标即源于恒用 rule.EnvID）
			toEnvID := resolveEnvID(inferInstanceEnvCode(inst), cur.EnvID, envIDByCode)
			// 候选判定：节点或环境任一与现绑不同即入候选（环境口径为本任务新增，
			// 仅环境变化的 rule 绑定此前被漏）
			if rule.NodeID != cur.NodeID || toEnvID != cur.EnvID {
				candidates = append(candidates, stdomain.RebindCandidate{
					ResourceID: inst.ID,
					AssetID:    inst.AssetID,
					AssetName:  inst.AssetName,
					Provider:   inst.GetStringAttribute("provider"),
					Region:     inst.GetStringAttribute("region"),
					FromNodeID: cur.NodeID,
					FromEnvID:  cur.EnvID,
					FromRuleID: cur.RuleID,
					ToNodeID:   rule.NodeID,
					ToEnvID:    toEnvID,
					ToRuleID:   rule.ID,
				})
			}
			break // 无论是否改绑，首个命中规则即为最终归属
		}
	}

	s.fillRebindNodeNames(ctx, candidates)
	return candidates, nil
}

// fillRebindNodeNames 批量回填 from/to 节点名（一次批量查，避免 N+1）
func (s *ruleEngineService) fillRebindNodeNames(ctx context.Context, items []stdomain.RebindCandidate) {
	idSet := make(map[int64]struct{}, len(items)*2)
	ids := make([]int64, 0, len(items)*2)
	for _, item := range items {
		for _, nid := range []int64{item.FromNodeID, item.ToNodeID} {
			if nid > 0 {
				if _, ok := idSet[nid]; !ok {
					idSet[nid] = struct{}{}
					ids = append(ids, nid)
				}
			}
		}
	}
	if len(ids) == 0 {
		return
	}

	nodes, err := s.nodeRepo.GetByIDs(ctx, ids)
	if err != nil {
		s.logger.Warn("改绑候选节点名回填失败", elog.FieldErr(err))
		return
	}
	nameByID := make(map[int64]string, len(nodes))
	for _, n := range nodes {
		nameByID[n.ID] = n.Name
	}
	for i := range items {
		if name, ok := nameByID[items[i].FromNodeID]; ok {
			items[i].FromNodeName = name
		}
		if name, ok := nameByID[items[i].ToNodeID]; ok {
			items[i].ToNodeName = name
		}
	}
}