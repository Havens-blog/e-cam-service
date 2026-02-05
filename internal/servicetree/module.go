// Package servicetree 提供服务树功能
// 这是从 internal/cam/servicetree 重构后的独立模块
package servicetree

import (
	camst "github.com/Havens-blog/e-cam-service/internal/cam/servicetree"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
)

// Module 服务树模块 - 别名到 cam/servicetree.Module
type Module = camst.Module

// NewModule 创建服务树模块
var NewModule = camst.NewModule

// InitIndexes 初始化数据库索引
func InitIndexes(db *mongox.Mongo) error {
	return camst.InitIndexes(db)
}
