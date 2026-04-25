# CAM 模块重构计划

## 概述

本文档描述了将 `internal/cam` 模块拆分为独立模块的重构计划。

## 重构目标

1. 将 CAM 模块拆分为独立的业务模块
2. 提取共享组件到 `internal/shared`
3. 保持 API 路由不变
4. 支持渐进式迁移

## 当前状态

### 已完成 ✅

1. **共享组件提取**
   - `internal/shared/middleware/tenant.go` - 租户中间件
   - `internal/shared/errs/code.go` - 统一错误码
   - `internal/shared/domain/` - 共享领域模型 (CloudAccount, IAM 相关)
   - `internal/shared/cloudx/` - 多云适配器

2. **独立模块创建**

   | 模块                    | 状态        | 说明                                    |
   | ----------------------- | ----------- | --------------------------------------- |
   | `internal/iam/`         | ✅ 独立实现 | 完整的 repository/service/web 层        |
   | `internal/account/`     | ✅ 独立实现 | 完整的 repository/dao/service 层        |
   | `internal/asset/`       | ✅ 独立实现 | Instance 完整实现，Model 使用别名       |
   | `internal/servicetree/` | ✅ 独立实现 | 完整的 domain/repository/service/web 层 |
   | `internal/sync/`        | ✅ 独立实现 | 使用 cloudx 适配器的同步服务            |
   | `internal/task/`        | ✅ 独立实现 | 完整的 executor/service/web 层          |

3. **编译验证**
   - 所有模块编译通过
   - 项目整体编译成功

4. **Wire 依赖注入更新** ✅
   - `internal/iam/wire.go` - 独立 IAM 模块的 wire 配置
   - `internal/iam/wire_gen.go` - 由 wire 自动生成
   - `internal/cam/init.go` - 使用 `internal/iam.InitModule` 初始化 IAM 模块
   - `internal/cam/module.go` - IAMModule 类型使用 `*iam.Module`
   - `ioc/wire_gen.go` - 主应用 wire 配置已重新生成

### 待完成 📋

1. **清理旧代码** (可选，低优先级)
   - 删除 cam 中的重复实现
   - 更新所有导入路径
   - 建议：待系统稳定运行后再评估

## 模块结构

```
internal/
├── iam/                      # IAM 管理 ✅ 独立实现
│   ├── module.go
│   ├── repository/
│   │   ├── dao/
│   │   └── *.go
│   ├── service/
│   └── web/
├── account/                  # 云账号管理 ✅ 独立实现
│   ├── module.go
│   ├── repository/
│   │   ├── dao/
│   │   │   └── account.go
│   │   └── account.go
│   └── service/
│       └── account.go
├── asset/                    # 资产管理 ✅ 独立实现
│   ├── module.go
│   ├── domain/
│   │   ├── instance.go
│   │   ├── model.go
│   │   └── field.go
│   ├── repository/
│   │   ├── dao/
│   │   │   └── instance.go
│   │   └── instance.go
│   └── service/
│       └── instance.go
├── servicetree/              # 服务树 ✅ 独立实现
│   ├── module.go
│   ├── domain/
│   │   ├── node.go
│   │   ├── binding.go
│   │   ├── rule.go
│   │   ├── environment.go
│   │   └── errors.go
│   ├── repository/
│   │   ├── dao/
│   │   │   ├── node.go
│   │   │   ├── binding.go
│   │   │   ├── rule.go
│   │   │   ├── environment.go
│   │   │   └── init.go
│   │   ├── node.go
│   │   ├── binding.go
│   │   ├── rule.go
│   │   └── environment.go
│   ├── service/
│   │   ├── tree.go
│   │   ├── binding.go
│   │   ├── rule_engine.go
│   │   └── environment.go
│   └── web/
│       ├── handler.go
│       ├── env_handler.go
│       └── vo.go
├── sync/                     # 同步服务 ✅ 独立实现
│   ├── module.go
│   ├── domain/
│   │   ├── adapter.go
│   │   ├── errors.go
│   │   └── task.go
│   └── service/
│       └── sync_service.go
├── task/                     # 异步任务 ✅ 独立实现
│   ├── module.go
│   ├── types.go
│   ├── domain/
│   │   └── task.go
│   ├── executor/
│   │   ├── types.go
│   │   ├── sync_assets.go
│   │   ├── sync_database.go
│   │   ├── sync_network.go
│   │   ├── sync_storage.go
│   │   └── sync_middleware.go
│   ├── service/
│   │   └── task_service.go
│   └── web/
│       ├── handler.go
│       └── vo.go
├── shared/                   # 共享组件
│   ├── cloudx/              # 多云适配器
│   ├── domain/              # 共享领域模型
│   ├── middleware/          # 共享中间件
│   └── errs/                # 共享错误码
└── cam/                      # 核心模块（保留，逐步废弃）
```

## 导入路径

### 推荐使用

```go
import (
    "github.com/Havens-blog/e-cam-service/internal/iam"
    "github.com/Havens-blog/e-cam-service/internal/account"
    "github.com/Havens-blog/e-cam-service/internal/asset"
    "github.com/Havens-blog/e-cam-service/internal/shared/middleware"
    "github.com/Havens-blog/e-cam-service/internal/shared/errs"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)
```

### 仍然可用（兼容旧代码）

```go
import (
    "github.com/Havens-blog/e-cam-service/internal/cam/iam"
    "github.com/Havens-blog/e-cam-service/internal/cam/middleware"
)
```

## 迁移优先级

1. **高优先级** - 已完成 ✅
   - iam (独立实现)
   - account (独立实现)
   - asset (独立实现)
   - servicetree (独立实现)
   - sync (独立实现，使用 cloudx 适配器)
   - task (独立实现，使用 account/asset 仓储)
   - shared/domain (共享领域模型)
   - shared/errs (共享错误码)
   - shared/middleware (共享中间件)

2. **低优先级** - 待评估
   - 更新 wire 依赖注入
   - 清理 cam 中的旧实现

## 注意事项

1. API 路由保持不变（`/api/v1/cam/*`）
2. 数据库集合名称保持不变
3. 配置文件格式保持不变
4. 不自动提交代码，所有变更需手动审查
5. 别名模式的模块可以逐步迁移为独立实现

## 变更历史

- 2026-02-05: 完成 wire 依赖注入更新 (IAM 模块使用独立实现，ioc/wire_gen.go 重新生成)
- 2026-02-05: 完成 task 模块独立实现 (executor/service/web 完整层，使用 account/asset 仓储)
- 2026-02-05: 完成 sync 模块独立实现 (使用 cloudx 适配器)
- 2026-02-05: 完成 servicetree 模块独立实现 (domain/repository/service/web 完整层)
- 2026-02-05: servicetree 模块使用 asset 模块的 Instance 类型替代 cam/domain
- 2026-02-05: 完成 asset 模块独立实现 (Instance 部分)
- 2026-02-05: 完成 account 模块独立实现
- 2026-02-05: 完成 iam 模块独立实现
- 2026-02-05: 创建共享组件 (middleware, errs, domain)
