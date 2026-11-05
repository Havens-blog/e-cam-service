// 索引幂等创建工具。
//
// 背景：CreateMany 在遇到"键序相同但名字不同"的已有索引时会报
// IndexOptionsConflict，且**阻断同批其余索引的创建**——
// ecam_audit_log 上就发生过：库里已有手工建的 idx_tenant_id_ctime，
// 代码每次启动都尝试建同键的自动名索引 ⇒ 每次启动一条 ERROR，
// 而真正缺失的 api_path/request_id/http_method 三条索引也一直没建上。
package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ensureIndexes 幂等地确保索引存在：按键序签名跳过"已存在等价索引"，
// 只创建真正缺失的。
//
// 键序签名基于 bson.D 的顺序（bson.D 保序）：{a:1,b:-1} 与 {b:-1,a:1}
// 签名不同——两者查询语义不同，不算等价，不会误跳过。
//
// 【适用范围】仅适用于**无 Options** 的普通索引（本包 audit/change 两处）。
// 带 Unique/PartialFilterExpression 等选项的索引**不要**用这里——
// 同键不同选项在语义上并不等价（如唯一索引），按键签名跳过会漏建。
func ensureIndexes(ctx context.Context, coll *mongo.Collection, models []mongo.IndexModel) error {
	existing, err := listIndexSignatures(ctx, coll)
	if err != nil {
		return fmt.Errorf("列出现有索引失败: %w", err)
	}
	for _, m := range models {
		keys, ok := m.Keys.(bson.D)
		if !ok {
			// 无法计算签名时保守起见仍尝试创建，由 Mongo 决定冲突与否
			if _, err := coll.Indexes().CreateOne(ctx, m); err != nil && !isEquivalentIndexConflict(err) {
				return err
			}
			continue
		}
		if _, dup := existing[keySignature(keys)]; dup {
			continue
		}
		if _, err := coll.Indexes().CreateOne(ctx, m); err != nil && !isEquivalentIndexConflict(err) {
			return err
		}
	}
	return nil
}

// listIndexSignatures 返回集合现有索引的键序签名集合。
func listIndexSignatures(ctx context.Context, coll *mongo.Collection) (map[string]struct{}, error) {
	existing := make(map[string]struct{})
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var doc bson.D
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		for _, e := range doc {
			if e.Key != "key" {
				continue
			}
			if keys, ok := e.Value.(bson.D); ok {
				existing[keySignature(keys)] = struct{}{}
			}
			break
		}
	}
	return existing, cur.Err()
}

// keySignature 生成 "field:dir,field:dir" 形式的键序签名。
func keySignature(keys bson.D) string {
	parts := make([]string, 0, len(keys))
	for _, e := range keys {
		parts = append(parts, fmt.Sprintf("%s:%v", e.Key, e.Value))
	}
	return strings.Join(parts, ",")
}

// isEquivalentIndexConflict 判断错误是否为"等价索引已存在（仅名字不同）"。
// 该情形视为幂等跳过；而"同名不同键/选项"（IndexKeySpecsConflict，server code 86）
// 是真问题，必须暴露。server code 85 = IndexOptionsConflict。
func isEquivalentIndexConflict(err error) bool {
	if err == nil {
		return false
	}
	var ce mongo.CommandError
	if errors.As(err, &ce) && ce.Code == 85 {
		return true
	}
	return strings.Contains(err.Error(), "Index already exists with a different name")
}
