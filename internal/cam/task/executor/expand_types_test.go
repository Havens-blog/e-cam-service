package executor

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpandAssetTypes_SingleTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "单个ECS",
			input:    []string{"ecs"},
			expected: []string{"ecs"},
		},
		{
			name:     "单个RDS",
			input:    []string{"rds"},
			expected: []string{"rds"},
		},
		{
			name:     "多个独立类型",
			input:    []string{"ecs", "rds", "vpc"},
			expected: []string{"ecs", "rds", "vpc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandAssetTypes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpandAssetTypes_AggregateTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "database展开",
			input:    []string{"database"},
			expected: []string{"rds", "redis", "mongodb"},
		},
		{
			name:     "db别名",
			input:    []string{"db"},
			expected: []string{"rds", "redis", "mongodb"},
		},
		{
			name:     "network展开",
			input:    []string{"network"},
			expected: []string{"vpc", "eip"},
		},
		{
			name:     "net别名",
			input:    []string{"net"},
			expected: []string{"vpc", "eip"},
		},
		{
			name:     "storage展开",
			input:    []string{"storage"},
			expected: []string{"nas", "oss"},
		},
		{
			name:     "middleware展开",
			input:    []string{"middleware"},
			expected: []string{"kafka", "elasticsearch"},
		},
		{
			name:     "mw别名",
			input:    []string{"mw"},
			expected: []string{"kafka", "elasticsearch"},
		},
		{
			name:     "compute展开",
			input:    []string{"compute"},
			expected: []string{"ecs", "disk", "snapshot", "security_group", "image"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandAssetTypes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpandAssetTypes_Deduplication(t *testing.T) {
	// database 包含 rds，再单独传 rds 不应重复
	result := expandAssetTypes([]string{"database", "rds"})
	sort.Strings(result)
	expected := []string{"mongodb", "rds", "redis"}
	sort.Strings(expected)
	assert.Equal(t, expected, result)
}

func TestExpandAssetTypes_MixedTypes(t *testing.T) {
	result := expandAssetTypes([]string{"ecs", "database", "vpc"})
	// ecs + rds,redis,mongodb + vpc
	assert.Contains(t, result, "ecs")
	assert.Contains(t, result, "rds")
	assert.Contains(t, result, "redis")
	assert.Contains(t, result, "mongodb")
	assert.Contains(t, result, "vpc")
	assert.Len(t, result, 5)
}

func TestExpandAssetTypes_EmptyInput(t *testing.T) {
	result := expandAssetTypes([]string{})
	assert.Empty(t, result)
}

func TestExpandAssetTypes_UnknownType(t *testing.T) {
	// 未知类型应该原样保留
	result := expandAssetTypes([]string{"unknown_type"})
	assert.Equal(t, []string{"unknown_type"}, result)
}

func TestExpandAssetTypes_AllAggregates(t *testing.T) {
	// 所有聚合类型一起展开
	result := expandAssetTypes([]string{"compute", "database", "network", "storage", "middleware"})

	expectedTypes := []string{
		"ecs", "disk", "snapshot", "security_group", "image",
		"rds", "redis", "mongodb",
		"vpc", "eip",
		"nas", "oss",
		"kafka", "elasticsearch",
	}

	sort.Strings(result)
	sort.Strings(expectedTypes)
	assert.Equal(t, expectedTypes, result)
}

func TestExpandAssetTypes_DuplicateInput(t *testing.T) {
	result := expandAssetTypes([]string{"ecs", "ecs", "ecs"})
	assert.Equal(t, []string{"ecs"}, result)
}
