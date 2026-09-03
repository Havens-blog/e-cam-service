package service

import "testing"

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
