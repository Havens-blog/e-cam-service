#!/bin/bash

echo "========================================"
echo "CAM Service 单元测试运行脚本"
echo "========================================"

echo ""
echo "1. 运行所有 CAM Service 测试..."
go test -v ./internal/cam/internal/service/...

echo ""
echo "2. 生成测试覆盖率报告..."
go test -coverprofile=coverage.out ./internal/cam/internal/service/...

if [ -f coverage.out ]; then
    echo ""
    echo "3. 生成 HTML 覆盖率报告..."
    go tool cover -html=coverage.out -o coverage.html
    echo "覆盖率报告已生成: coverage.html"
fi

echo ""
echo "4. 运行基准测试..."
go test -bench=. ./internal/cam/internal/service/

echo ""
echo "========================================"
echo "测试完成！"
echo "========================================"