// Package account 跨域账号访问层（以读为主）。
//
// 本包提供云账号的 repository(+dao) 与 service 层，供 cam、cert、logquery、mcp
// 等多个域按需读取云账号凭据与元数据。定位以读为主，但存在一条如实登记的写路径
// 与一条已知的坏路径：
//
// 写路径：CloudAccountService.SyncAccount 经 taskQueue.Submit 向任务队列提交同步
// 任务（internal/account/service/account.go:338），并非纯只读。
//
// 坏路径：internal/mcp/deps.go:59 以 nil 任务队列构造 CloudAccountService，
// mcp 侧 sync_account 调用即 panic；其处置随 CMDB 决策 0001 一并决断。
//
// 外部消费方（import 级 grep，2026-09-09，task/sync/servicetree 删除后实测，
// 文件级一一对应，共 14 个文件）：
//
//   - internal/cert/module.go
//   - internal/cert/module_test.go
//   - internal/cert/service/execute_service.go
//   - internal/cert/service/execute_service_test.go
//   - internal/cert/service/k8s_credential_fetch_service.go
//   - internal/cert/service/reference_scan_service.go
//   - internal/cert/service/reference_scan_service_test.go
//   - internal/logquery/module.go
//   - internal/mcp/deps.go
//   - internal/cam/cost/collector/service.go
//   - internal/cam/task/module.go
//   - internal/cam/task/executor/sync_billing.go
//   - ioc/cert.go
//   - ioc/logquery.go
//
// 本包的最终归属（是否收编进 cam 域）不在本轮处理，统一以决策 0001 为唯一事实源：
// D:\Haven\docs\decisions\0001-cmdb-consolidation-keep-ecmdb.md
package account
