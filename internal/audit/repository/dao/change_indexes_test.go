package dao

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"
)

// indexKeyString 把 IndexModel.Keys 序列化为 "a_1,b_-1" 形式，便于断言键顺序。
func indexKeyString(m bson.D) string {
	parts := make([]string, 0, len(m))
	for _, e := range m {
		parts = append(parts, fmt.Sprintf("%s_%v", e.Key, e.Value))
	}
	return strings.Join(parts, ",")
}

// changeHistoryIndexKeys 收集全部索引的键序列。
func changeHistoryIndexKeys() []string {
	out := make([]string, 0, 8)
	for _, idx := range changeHistoryIndexes() {
		keys, ok := idx.Keys.(bson.D)
		if !ok {
			continue
		}
		out = append(out, indexKeyString(keys))
	}
	return out
}

// TestChangeHistoryIndexes_KeepExisting 锁定历史索引不得被无意删除。
// 这些索引服务于列表查询（asset_id / ctime 排序）与按来源过滤，删掉会直接让列表变慢。
func TestChangeHistoryIndexes_KeepExisting(t *testing.T) {
	keys := changeHistoryIndexKeys()
	required := []string{
		"asset_id_1,ctime_-1",
		"tenant_id_1,ctime_-1",
		"model_uid_1,ctime_-1",
		"change_source_1",
	}
	for _, want := range required {
		assert.Contains(t, keys, want, "历史索引被删除了: %s", want)
	}
}

// TestChangeHistoryIndexes_GroupByCoverage 锁定"租户内分组统计"的覆盖索引。
//
// 背景（2026-11-04 实测，ecam_change_history 5,399,871 条）：
//   - group by model_uid  → 1 分 38 秒
//   - group by field_name → 超过 200 秒
//
// 其中 field_name 与 provider 当时**完全没有索引**，只能全表扫描。
// 这三条复合索引用 ESR 顺序（租户等值 → 分组字段 → ctime 区间）覆盖 GetSummary 的三个统计项。
// 若有人调整键顺序（例如把 ctime 提前），分组就无法利用索引，本用例会失败。
func TestChangeHistoryIndexes_GroupByCoverage(t *testing.T) {
	keys := changeHistoryIndexKeys()
	required := []string{
		"tenant_id_1,model_uid_1,ctime_-1",
		"tenant_id_1,field_name_1,ctime_-1",
		"tenant_id_1,provider_1,ctime_-1",
	}
	for _, want := range required {
		assert.Contains(t, keys, want, "分组统计覆盖索引缺失或键顺序被改: %s", want)
	}
}

// TestChangeHistoryIndexes_NoDuplicate 防止重复定义同形状索引（重复会在建索引时报错或浪费写入）。
func TestChangeHistoryIndexes_NoDuplicate(t *testing.T) {
	keys := changeHistoryIndexKeys()
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		_, dup := seen[k]
		assert.False(t, dup, "存在重复索引定义: %s", k)
		seen[k] = struct{}{}
	}
}
