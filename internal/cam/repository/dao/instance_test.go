package dao

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// TestBuildQueryModelUID 閿佸畾 buildQuery 鐨?model_uid 杩囨护锛堟敞鍐岃〃鏀舵暃鍚庤涓猴級銆?
func TestBuildQueryModelUID(t *testing.T) {
	d := &instanceDAO{}
	tests := []struct {
		modelUID string
		want     bson.M
	}{
		{"ecs", bson.M{"$or": []bson.M{
			{"model_uid": "ecs"},
			{"model_uid": "cloud_vm"},
			{"model_uid": bson.M{"$regex": "_ecs$"}},
		}}},
		{"cloud_vm", bson.M{"$or": []bson.M{
			{"model_uid": "cloud_vm"},
			{"model_uid": bson.M{"$regex": "_ecs$"}},
		}}},
		// 婕傜Щ淇锛歝dn/waf/eni 鐜颁笌 buildSearchQuery 鍏辩敤娉ㄥ唽琛?
		{"cdn", bson.M{"$or": []bson.M{
			{"model_uid": "cdn"},
			{"model_uid": "cloud_cdn"},
			{"model_uid": bson.M{"$regex": "_cdn$"}},
		}}},
		{"waf", bson.M{"$or": []bson.M{
			{"model_uid": "waf"},
			{"model_uid": "cloud_waf"},
			{"model_uid": bson.M{"$regex": "_waf$"}},
		}}},
		{"eni", bson.M{"$or": []bson.M{
			{"model_uid": "eni"},
			{"model_uid": "cloud_eni"},
			{"model_uid": bson.M{"$regex": "_eni$"}},
		}}},
		// lb 瀹舵棌锛氭敞鍐岃〃甯﹀叏閲忛€氱敤鍚嶏紙cloud_slb/alb/nlb 绮剧‘鍖归厤涓哄啑浣欒ˉ鍏咃紝缁撴灉闆嗕笉鍙橈級
		{"lb", bson.M{"$or": []bson.M{
			{"model_uid": "lb"},
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
			{"model_uid": "slb"},
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
			{"model_uid": "subnet"},
			{"model_uid": "cloud_vswitch"},
			{"model_uid": "cloud_subnet"},
			{"model_uid": bson.M{"$regex": "_vswitch$"}},
			{"model_uid": bson.M{"$regex": "_subnet$"}},
		}}},
		{"security-group", bson.M{"$or": []bson.M{
			{"model_uid": "security-group"},
			{"model_uid": "cloud_security_group"},
			{"model_uid": bson.M{"$regex": "_security_group$"}},
		}}},
		// 鏈敞鍐岀被鍨?鈫?鍘熸牱绮剧‘鍖归厤锛坉efault 璇箟锛?
		{"not_a_type", bson.M{"model_uid": "not_a_type"}},
	}
	for _, tt := range tests {
		got := d.buildQuery(InstanceFilter{ModelUID: tt.modelUID})
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildQuery(ModelUID=%q)\n got: %#v\nwant: %#v", tt.modelUID, got, tt.want)
		}
	}
}

// TestBuildSearchQueryAssetTypes 閿佸畾 buildSearchQuery 鐨勮祫浜х被鍨嬭繃婊わ紝
// 骞堕獙璇佹紓绉讳慨澶嶏細cdn/waf/eni 涓嶅啀闈欓粯缂哄け銆?
func TestBuildSearchQueryAssetTypes(t *testing.T) {
	d := &instanceDAO{}
	tests := []struct {
		assetTypes []string
		want       bson.M
	}{
		{[]string{"cdn"}, bson.M{"$or": []bson.M{
			{"model_uid": "cdn"},
			{"model_uid": "cloud_cdn"},
			{"model_uid": bson.M{"$regex": "_cdn$"}},
		}}},
		{[]string{"waf"}, bson.M{"$or": []bson.M{
			{"model_uid": "waf"},
			{"model_uid": "cloud_waf"},
			{"model_uid": bson.M{"$regex": "_waf$"}},
		}}},
		{[]string{"eni"}, bson.M{"$or": []bson.M{
			{"model_uid": "eni"},
			{"model_uid": "cloud_eni"},
			{"model_uid": bson.M{"$regex": "_eni$"}},
		}}},
		{[]string{"ecs", "rds"}, bson.M{"$or": []bson.M{
			{"model_uid": "ecs"},
			{"model_uid": "cloud_vm"},
			{"model_uid": bson.M{"$regex": "_ecs$"}},
			{"model_uid": "rds"},
			{"model_uid": "cloud_rds"},
			{"model_uid": bson.M{"$regex": "_rds$"}},
		}}},
		{[]string{"subnet", "vswitch"}, bson.M{"$or": []bson.M{
			{"model_uid": "subnet"},
			{"model_uid": "cloud_vswitch"},
			{"model_uid": "cloud_subnet"},
			{"model_uid": bson.M{"$regex": "_vswitch$"}},
			{"model_uid": bson.M{"$regex": "_subnet$"}},
			{"model_uid": "vswitch"},
			{"model_uid": "cloud_vswitch"},
			{"model_uid": "cloud_subnet"},
			{"model_uid": bson.M{"$regex": "_vswitch$"}},
			{"model_uid": bson.M{"$regex": "_subnet$"}},
		}}},
		// 鏈煡绫诲瀷 鈫?鏃?$or锛堝師 switch 鏃?default 琛屼负锛?
		{[]string{"not_a_type"}, bson.M{}},
	}
	for _, tt := range tests {
		got := d.buildSearchQuery(SearchFilter{AssetTypes: tt.assetTypes})
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildSearchQuery(AssetTypes=%v)\n got: %#v\nwant: %#v", tt.assetTypes, got, tt.want)
		}
	}
}

// TestRelevanceScoreExpr 閿佸畾鐩稿叧鎬ц瘎鍒嗚〃杈惧紡缁撴瀯锛?
// 6 涓垎鏀寜 绮剧‘ID<绮剧‘鍚?ID鍓嶇紑<鍚嶅墠缂€<ID鍖呭惈<鍚嶅寘鍚?鎺掑簭锛宒efault 涓?6锛?
// 鍏抽敭璇嶇粺涓€杞皬鍐欙紙绛夊€煎垎鏀殑甯搁噺鍊煎繀椤绘槸灏忓啓锛夈€?
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

	// 鍒嗘敮 then 鍊煎繀椤绘寜 0..5 閫掑锛堢浉鍏虫€ч檷搴忥級
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

	// 绛夊€煎垎鏀殑鍏抽敭璇嶆瘮杈冨€煎繀椤诲凡灏忓啓
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
		t.Fatalf("relevanceScoreExpr 鏈唴宓屽皬鍐欏叧閿瘝 \"k8s\"锛孴oLower 鐤戜技澶辨晥")
	}
}
