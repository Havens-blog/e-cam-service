package dao

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
)

// TestBuildQuery_SourceAndModel 锁定变更列表筛选的查询构造：
// change_source / model_uid 条件按等值进入查询，未设置时不出现。
//
// 背景（2026-11-05）：变更列表接口此前不支持按模型/来源筛选，
// 统一审计面板无法回答「手工改的有哪些」「某模型的变更有哪些」。
// 两者均有索引（change_source 单字段 / {tenant_id, model_uid, ctime}），无全表扫风险。
func TestBuildQuery_SourceAndModel(t *testing.T) {
	t.Run("设置 change_source 与 model_uid", func(t *testing.T) {
		q := (&changeRecordDAO{}).buildQuery(domain.ChangeFilter{
			TenantID:     3,
			ChangeSource: "manual",
			ModelUID:     "ecs",
		})
		if q["change_source"] != "manual" {
			t.Fatalf("change_source 条件错误: %v", q["change_source"])
		}
		if q["model_uid"] != "ecs" {
			t.Fatalf("model_uid 条件错误: %v", q["model_uid"])
		}
		if q["tenant_id"] != int64(3) {
			t.Fatalf("tenant_id 条件错误: %v", q["tenant_id"])
		}
	})

	t.Run("未设置时不产生条件", func(t *testing.T) {
		q := (&changeRecordDAO{}).buildQuery(domain.ChangeFilter{TenantID: 3})
		if _, ok := q["change_source"]; ok {
			t.Fatalf("未设置 change_source 不应产生条件: %v", q)
		}
		if _, ok := q["model_uid"]; ok {
			t.Fatalf("未设置 model_uid 不应产生条件: %v", q)
		}
	})
}
