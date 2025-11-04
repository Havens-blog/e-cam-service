// Package docs E-CAM Service API Documentation
//
// 企业云资产管理服务 API 文档
//
// @title E-CAM Service API
// @version 1.0.0
// @description 企业云资产管理服务，提供云资产管理、模型管理、资源同步等功能
// @description
// @description ## 功能模块
// @description - 云资产管理：管理多云环境下的资产
// @description - 模型管理：定义和管理资源模型
// @description - 云账号管理：管理云厂商账号
// @description - 资源同步：自动同步云资源
// @description
// @description ## 认证方式
// @description 使用 JWT Token 进行认证，在请求头中添加：
// @description ```
// @description Authorization: Bearer <token>
// @description ```
//
// @contact.name API Support
// @contact.email support@example.com
//
// @license.name MIT
// @license.url https://opensource.org/licenses/MIT
//
// @host localhost:8001
// @BasePath /
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.
//
// @tag.name 云资产
// @tag.description 云资产管理相关接口
//
// @tag.name 模型管理
// @tag.description 资源模型管理接口
//
// @tag.name 云账号
// @tag.description 云账号管理接口
//
// @tag.name 资源同步
// @tag.description 云资源同步接口
package docs
