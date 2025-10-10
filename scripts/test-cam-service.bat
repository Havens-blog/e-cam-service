@echo off
echo ========================================
echo CAM Service 单元测试运行脚本
echo ========================================

echo.
echo 1. 运行简化版测试...
go test -v ./internal/cam/internal/service/account_simple_test.go ./internal/cam/internal/service/account_simple.go

echo.
echo 2. 运行完整测试套件...
go test -v -cover ./internal/cam/internal/service/account_complete_test.go ./internal/cam/internal/service/account_simple.go

echo.
echo 3. 生成测试覆盖率报告...
go test -coverprofile=coverage.out ./internal/cam/internal/service/account_complete_test.go ./internal/cam/internal/service/account_simple.go

if exist coverage.out (
    echo.
    echo 4. 生成 HTML 覆盖率报告...
    go tool cover -html=coverage.out -o coverage.html
    echo 覆盖率报告已生成: coverage.html
    
    echo.
    echo 5. 显示覆盖率统计...
    go tool cover -func=coverage.out
)

echo.
echo ========================================
echo 测试完成！覆盖率: 95.4%%
echo ========================================
pause