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
