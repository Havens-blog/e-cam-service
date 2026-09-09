// Package asset cert 域覆盖率分母的只读聚合数据源。
//
// 本包提供云资产（c_instance 等）的 domain/repository 层，当前唯一业务消费方是
// cert 域：覆盖率扫描启动时按云×产品聚合资产总数作为分母
// （internal/cert/service/coverage_meta.go）。
//
// 历史包袱：本包存在直写 c_instance 的旧路径，且目录归属（独立顶层 vs 收编进
// cert 域）未决——两者均随 CMDB 决策 0001 Phase 4 一并决断，本轮不做迁移、不收编。
//
// 外部消费方（import 级 grep，2026-09-09，task/sync/servicetree 删除后实测，
// 文件级一一对应，共 5 个文件）：
//
//   - internal/cert/module.go
//   - internal/cert/module_test.go
//   - internal/cert/service/coverage_meta.go
//   - internal/cert/service/reference_scan_service_test.go
//   - ioc/cert.go
//
// 归属与历史包袱的最终处置以决策 0001 为唯一事实源：
// D:\Haven\docs\decisions\0001-cmdb-consolidation-keep-ecmdb.md
package asset
