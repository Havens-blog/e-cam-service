package service

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
)

func TestExtractAssetType(t *testing.T) {
	tests := []struct {
		modelUID string
		want     string
	}{
		// 通用模型
		{"cloud_vm", "ecs"},
		{"cloud_rds", "rds"},
		{"cloud_slb", "slb"},
		// 厂商前缀:按首个下划线切,多词类型不得切错
		{"aliyun_ecs", "ecs"},
		{"volcano_ecs", "ecs"},
		{"aliyun_security_group", "security_group"}, // 回归:LastIndex 会错切成 "group"
		{"volcengine_elasticsearch", "elasticsearch"},
		{"huawei_mongodb", "mongodb"},
		{"aws_rds", "rds"},
		// 无前缀原样返回
		{"ecs", "ecs"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := extractAssetType(tt.modelUID); got != tt.want {
			t.Errorf("extractAssetType(%q) = %q, want %q", tt.modelUID, got, tt.want)
		}
	}
}

func TestSlicePage(t *testing.T) {
	n := func(count int) []domain.NodeAssetVO {
		out := make([]domain.NodeAssetVO, count)
		for i := range out {
			out[i].ID = int64(i + 1)
		}
		return out
	}

	cases := []struct {
		name                string
		offset, limit       int64
		count               int
		wantLen             int
		wantFirst, wantLast int64
	}{
		{"首页截断", 0, 20, 100, 20, 1, 20},
		{"翻页", 20, 20, 100, 20, 21, 40},
		{"末页不足", 80, 20, 100, 20, 81, 100},
		{"越界 offset", 200, 20, 100, 0, 0, 0},
		{"limit<=0 不限制", 0, 0, 5, 5, 1, 5},
		{"负 offset 归一", -1, 10, 5, 5, 1, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := slicePage(n(c.count), c.offset, c.limit)
			if len(got) != c.wantLen {
				t.Fatalf("len = %d, want %d", len(got), c.wantLen)
			}
			if c.wantLen > 0 {
				if got[0].ID != c.wantFirst || got[len(got)-1].ID != c.wantLast {
					t.Errorf("range = [%d..%d], want [%d..%d]", got[0].ID, got[len(got)-1].ID, c.wantFirst, c.wantLast)
				}
			}
		})
	}
}
