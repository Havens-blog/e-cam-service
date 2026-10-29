package dao

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// 回归:资产类型过滤必须同时覆盖三种 model_uid 写法。
//
// 背景(2026-10-29 实测):阿里云 ECS 在实例库中的 model_uid 为**裸 `ecs`**(711 台,
// prov=aliyun/acct=1/tenant=3),而查询只匹配 `cloud_vm` 与 `~/_ecs$/` → 云主机页
// total=0、详情 404 instance not found,数据其实一直在库里。
func TestBuildQueryModelUIDIncludesBareType(t *testing.T) {
	d := &instanceDAO{}

	cases := []struct {
		modelUID string
		wantBare string
		wantOld  string
	}{
		{"ecs", "ecs", "cloud_vm"},
		{"cloud_vm", "ecs", "cloud_vm"},
		{"rds", "rds", "cloud_rds"},
		{"redis", "redis", "cloud_redis"},
		{"mongodb", "mongodb", "cloud_mongodb"},
		{"vpc", "vpc", "cloud_vpc"},
		{"eip", "eip", "cloud_eip"},
		{"nas", "nas", "cloud_nas"},
		{"oss", "oss", "cloud_oss"},
	}

	for _, tc := range cases {
		t.Run(tc.modelUID, func(t *testing.T) {
			q := d.buildQuery(InstanceFilter{ModelUID: tc.modelUID, TenantID: 3})
			or, ok := q["$or"].([]bson.M)
			if !ok || len(or) == 0 {
				t.Fatalf("$or 缺失: %#v", q)
			}
			var hasBare, hasOld, hasRegex bool
			for _, clause := range or {
				if v, ok := clause["model_uid"].(string); ok {
					if v == tc.wantBare {
						hasBare = true
					}
					if v == tc.wantOld {
						hasOld = true
					}
				}
				if m, ok := clause["model_uid"].(bson.M); ok {
					if re, _ := m["$regex"].(string); re == "_"+tc.wantBare+"$" {
						hasRegex = true
					}
				}
			}
			if !hasBare {
				t.Errorf("model_uid=%s 的过滤缺少裸类型 %q(阿里云 ECS 即此形态,漏配导致整类不可见): %#v", tc.modelUID, tc.wantBare, or)
			}
			if !hasOld {
				t.Errorf("model_uid=%s 的过滤缺少旧名 %q: %#v", tc.modelUID, tc.wantOld, or)
			}
			if !hasRegex {
				t.Errorf("model_uid=%s 的过滤缺少 {provider}_%s 正则: %#v", tc.modelUID, tc.wantBare, or)
			}
			if q["tenant_id"] != int64(3) {
				t.Errorf("tenant_id 过滤丢失: %#v", q)
			}
		})
	}
}

// 未知 model_uid 仍走等值匹配(不放大到 $or),避免把其它类型混入。
func TestBuildQueryUnknownModelUIDStaysEquality(t *testing.T) {
	d := &instanceDAO{}
	q := d.buildQuery(InstanceFilter{ModelUID: "custom_type"})
	if _, hasOr := q["$or"]; hasOr {
		t.Fatalf("未知类型不应使用 $or: %#v", q)
	}
	if q["model_uid"] != "custom_type" {
		t.Fatalf("未知类型应为等值匹配: %#v", q)
	}
}

// 搜索路径(资产搜索)使用同一候选集:裸类型同样必须命中。
// 注意:带关键词时类型 $or 会被放进 $and 里(与关键词 $or 并存),断言需遍历两者。
func TestBuildSearchQueryModelUIDIncludesBareType(t *testing.T) {
	d := &instanceDAO{}
	q := d.buildSearchQuery(SearchFilter{Keyword: "x", AssetTypes: []string{"ecs"}, TenantID: 3})

	var candidates []bson.M
	if or, ok := q["$or"].([]bson.M); ok {
		candidates = append(candidates, or...)
	}
	if and, ok := q["$and"].([]bson.M); ok {
		for _, clause := range and {
			if or, ok := clause["$or"].([]bson.M); ok {
				candidates = append(candidates, or...)
			}
		}
	}
	if len(candidates) == 0 {
		t.Fatalf("未找到类型/关键词的 $or 子句: %#v", q)
	}
	var hasBare bool
	for _, clause := range candidates {
		if v, ok := clause["model_uid"].(string); ok && v == "ecs" {
			hasBare = true
		}
	}
	if !hasBare {
		t.Errorf("资产搜索的类型过滤缺少裸类型 ecs: %#v", q)
	}
}
