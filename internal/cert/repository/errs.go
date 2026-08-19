package repository

import (
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mapDupKey 将 unique 索引冲突（duplicate key）映射为领域哨兵错误，其余错误原样返回。
// 供各仓储 Create/Upsert 写路径统一做冲突语义转换。
func mapDupKey(err error, sentinel error) error {
	if err == nil {
		return nil
	}
	if mongo.IsDuplicateKeyError(err) {
		return sentinel
	}
	return err
}

// objectIDFromHex 解析文档 ID；非法 hex 返回 ErrInvalidID（调用方参数错误，非未命中）。
func objectIDFromHex(hex string) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("%w: %q", domain.ErrInvalidID, hex)
	}
	return id, nil
}

// optionsUpsert 共享 upsert 选项（按唯一键写入路径复用）。
func optionsUpsert() *options.UpdateOptions {
	upsert := true
	return options.Update().SetUpsert(upsert)
}
