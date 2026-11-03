package dao

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"go.mongodb.org/mongo-driver/bson"
)

// TestBuildAuditLogSort 覆盖审计日志排序的字段/方向白名单。
//
// 背景：审计日志列表原先在 DAO 里硬编码按 ctime 倒序，前端列表头虽然显示
// 「时间 / 耗时」可排序，但只是对当前页 20 行做客户端排序 —— 用户点「耗时降序」
// 会以为看到全局最慢的请求，实际只是当前页里最慢的。本用例守住后端排序契约：
// 字段与方向都必须走白名单，不能把任意字段名透传进 Mongo 的 sort。
func TestBuildAuditLogSort(t *testing.T) {
	cases := []struct {
		name   string
		filter domain.AuditLogFilter
		want   bson.D
	}{
		{
			name:   "空值默认 ctime 倒序",
			filter: domain.AuditLogFilter{},
			want:   bson.D{{Key: "ctime", Value: -1}},
		},
		{
			name:   "ctime 显式倒序",
			filter: domain.AuditLogFilter{SortBy: "ctime", Order: "desc"},
			want:   bson.D{{Key: "ctime", Value: -1}},
		},
		{
			name:   "ctime 正序",
			filter: domain.AuditLogFilter{SortBy: "ctime", Order: "asc"},
			want:   bson.D{{Key: "ctime", Value: 1}},
		},
		{
			name:   "duration_ms 倒序带 ctime 次级键",
			filter: domain.AuditLogFilter{SortBy: "duration_ms", Order: "desc"},
			want:   bson.D{{Key: "duration_ms", Value: -1}, {Key: "ctime", Value: -1}},
		},
		{
			name:   "duration_ms 正序带 ctime 次级键",
			filter: domain.AuditLogFilter{SortBy: "duration_ms", Order: "asc"},
			want:   bson.D{{Key: "duration_ms", Value: 1}, {Key: "ctime", Value: -1}},
		},
		{
			// 关键安全用例：任意字段名不得进入排序，必须回退 ctime
			name:   "非法字段回退 ctime",
			filter: domain.AuditLogFilter{SortBy: "$where", Order: "desc"},
			want:   bson.D{{Key: "ctime", Value: -1}},
		},
		{
			name:   "非法方向回退 desc",
			filter: domain.AuditLogFilter{SortBy: "duration_ms", Order: "sideways"},
			want:   bson.D{{Key: "duration_ms", Value: -1}, {Key: "ctime", Value: -1}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAuditLogSort(tc.filter)
			if len(got) != len(tc.want) {
				t.Fatalf("键数量不符：got=%v want=%v", got, tc.want)
			}
			for i := range tc.want {
				if got[i].Key != tc.want[i].Key || got[i].Value != tc.want[i].Value {
					t.Fatalf("第 %d 个键不符：got=%v want=%v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
