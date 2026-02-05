// Package task 异步任务模块
// 这是从 internal/cam/task 重构后的独立模块
// 当前阶段：别名模式，重新导出 cam/task 的实现
package task

import (
	camtask "github.com/Havens-blog/e-cam-service/internal/cam/task"
)

// Module 任务模块 - 别名到 cam/task.Module
type Module = camtask.Module

// InitModule 初始化任务模块
var InitModule = camtask.InitModule
