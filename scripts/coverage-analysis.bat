@echo off
echo ========================================
echo CAM Service 覆盖率分析脚本
echo ========================================

echo.
echo 1. 运行 account.go 测试并生成覆盖率...
go test -v -cover ./internal/cam/internal/service/account_test.go ./internal/cam/internal/service/account.go

echo.
echo 2. 运行完整 service 包测试...
go test -v -cover ./internal/cam/internal/service/

echo.
echo 3. 生成详细覆盖率报告...
go test -coverprofile=account_coverage.out ./internal/cam/internal/service/account_test.go ./internal/cam/internal/service/account.go

if exist account_coverage.out (
    echo.
    echo 4. 显示函数级覆盖率...
    go tool cover -func account_coverage.out
    
    echo.
    echo 5. 生成 HTML 覆盖率报告...
    go tool cover -html account_coverage.out -o account_coverage.html
    echo HTML 覆盖率报告已生成: account_coverage.html
)

echo.
echo ========================================
echo 覆盖率分析完成！
echo ========================================
pause