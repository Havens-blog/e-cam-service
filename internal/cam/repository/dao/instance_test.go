package dao

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// TestBuildQueryModelUID 锁定 buildQuery 的 model_uid 过滤（注册表收敛后行为）。
func TestBuildQueryModelUID(t *testing.T) {
	d := &instanceDAO{}
	tests := []struct {
		modelUID string
		want     bson.M
	}{
		{"ecs", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_vm"},
			{"model_uid": bson.M{"$regex": "_ecs$"}},
		}}},
		{"cloud_vm", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_vm"},
			{"model_uid": bson.M{"$regex": "_ecs$"}},
		}}},
		// 漂移修复：cdn/waf/eni 现与 buildSearchQuery 共用注册表
		{"cdn", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_cdn"},
			{"model_uid": bson.M{"$regex": "_cdn$"}},
		}}},
		{"waf", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_waf"},
			{"model_uid": bson.M{"$regex": "_waf$"}},
		}}},
		{"eni", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_eni"},
			{"model_uid": bson.M{"$regex": "_eni$"}},
		}}},
		// lb 家族：注册表带全量通用名（cloud_slb/alb/nlb 精确匹配为冗余补充，结果集不变）
		{"lb", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_lb"},
			{"model_uid": "cloud_slb"},
			{"model_uid": "cloud_alb"},
			{"model_uid": "cloud_nlb"},
			{"model_uid": bson.M{"$regex": "_lb$"}},
			{"model_uid": bson.M{"$regex": "_slb$"}},
			{"model_uid": bson.M{"$regex": "_alb$"}},
			{"model_uid": bson.M{"$regex": "_nlb$"}},
			{"model_uid": bson.M{"$regex": "_elb$"}},
			{"model_uid": bson.M{"$regex": "_clb$"}},
		}}},
		{"slb", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_lb"},
			{"model_uid": "cloud_slb"},
			{"model_uid": "cloud_alb"},
			{"model_uid": "cloud_nlb"},
			{"model_uid": bson.M{"$regex": "_lb$"}},
			{"model_uid": bson.M{"$regex": "_slb$"}},
			{"model_uid": bson.M{"$regex": "_alb$"}},
			{"model_uid": bson.M{"$regex": "_nlb$"}},
			{"model_uid": bson.M{"$regex": "_elb$"}},
			{"model_uid": bson.M{"$regex": "_clb$"}},
		}}},
		{"subnet", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_vswitch"},
			{"model_uid": "cloud_subnet"},
			{"model_uid": bson.M{"$regex": "_vswitch$"}},
			{"model_uid": bson.M{"$regex": "_subnet$"}},
		}}},
		{"security-group", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_security_group"},
			{"model_uid": bson.M{"$regex": "_security_group$"}},
		}}},
		// 未注册类型 → 原样精确匹配（default 语义）
		{"not_a_type", bson.M{"model_uid": "not_a_type"}},
	}
	for _, tt := range tests {
		got := d.buildQuery(InstanceFilter{ModelUID: tt.modelUID})
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildQuery(ModelUID=%q)\n got: %#v\nwant: %#v", tt.modelUID, got, tt.want)
		}
	}
}

// TestBuildSearchQueryAssetTypes 锁定 buildSearchQuery 的资产类型过滤，
// 并验证漂移修复：cdn/waf/eni 不再静默缺失。
func TestBuildSearchQueryAssetTypes(t *testing.T) {
	d := &instanceDAO{}
	tests := []struct {
		assetTypes []string
		want       bson.M
	}{
		{[]string{"cdn"}, bson.M{"$or": []bson.M{
			{"model_uid": "cloud_cdn"},
			{"model_uid": bson.M{"$regex": "_cdn$"}},
		}}},
		{[]string{"waf"}, bson.M{"$or": []bson.M{
			{"model_uid": "cloud_waf"},
			{"model_uid": bson.M{"$regex": "_waf$"}},
		}}},
		{[]string{"eni"}, bson.M{"$or": []bson.M{
			{"model_uid": "cloud_eni"},
			{"model_uid": bson.M{"$regex": "_eni$"}},
		}}},
		{[]string{"ecs", "rds"}, bson.M{"$or": []bson.M{
			{"model_uid": "cloud_vm"},
			{"model_uid": bson.M{"$regex": "_ecs$"}},
			{"model_uid": "cloud_rds"},
			{"model_uid": bson.M{"$regex": "_rds$"}},
		}}},
		{[]string{"subnet", "vswitch"}, bson.M{"$or": []bson.M{
			{"model_uid": "cloud_vswitch"},
			{"model_uid": "cloud_subnet"},
			{"model_uid": bson.M{"$regex": "_vswitch$"}},
			{"model_uid": bson.M{"$regex": "_subnet$"}},
			{"model_uid": "cloud_vswitch"},
			{"model_uid": "cloud_subnet"},
			{"model_uid": bson.M{"$regex": "_vswitch$"}},
			{"model_uid": bson.M{"$regex": "_subnet$"}},
		}}},
		// 未知类型 → 无 $or（原 switch 无 default 行为）
		{[]string{"not_a_type"}, bson.M{}},
	}
	for _, tt := range tests {
		got := d.buildSearchQuery(SearchFilter{AssetTypes: tt.assetTypes})
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildSearchQuery(AssetTypes=%v)\n got: %#v\nwant: %#v", tt.assetTypes, got, tt.want)
		}
	}
}

// TestRelevanceScoreExpr 锁定相关性评分表达式结构：
// 6 个分支按 精确ID<精确名<ID前缀<名前缀<ID包含<名包含 排序，default 为 6；
// 关键词统一转小写（等值分支的常量值必须是小写）。
func TestRelevanceScoreExpr(t *testing.T) {
	expr := relevanceScoreExpr("K8S")

	if len(expr) != 1 || expr[0].Key != "$switch" {
		t.Fatalf("want single $switch, got %#v", expr)
	}
	sw, ok := expr[0].Value.(bson.D)
	if !ok {
		t.Fatalf("$switch value not bson.D: %T", expr[0].Value)
	}

	var branches bson.A
	var deflt any
	for _, e := range sw {
		switch e.Key {
		case "branches":
			branches = e.Value.(bson.A)
		case "default":
			deflt = e.Value
		}
	}
	if len(branches) != 6 {
		t.Fatalf("want 6 branches, got %d", len(branches))
	}
	if deflt != 6 {
		t.Fatalf("want default 6, got %v", deflt)
	}

	// 分支 then 值必须按 0..5 递增（相关性降序）
	for i, b := range branches {
		br, ok := b.(bson.D)
		if !ok {
			t.Fatalf("branch %d not bson.D: %T", i, b)
		}
		for _, e := range br {
			if e.Key == "then" && e.Value != i {
				t.Fatalf("branch %d then = %v, want %d", i, e.Value, i)
			}
		}
	}

	// 等值分支的关键词比较值必须已小写
	hasLowerKw := false
	var containsLower func(v any) bool
	containsLower = func(v any) bool {
		switch x := v.(type) {
		case string:
			return x == "k8s"
		case bson.A:
			for _, it := range x {
				if containsLower(it) {
					return true
				}
			}
		case bson.D:
			for _, e := range x {
				if containsLower(e.Value) {
					return true
				}
			}
		}
		return false
	}
	for _, b := range branches {
		if containsLower(b) {
			hasLowerKw = true
			break
		}
	}
	if !hasLowerKw {
		t.Fatalf("relevanceScoreExpr 未内嵌小写关键词 \"k8s\"，ToLower 疑似失效")
	}
}
