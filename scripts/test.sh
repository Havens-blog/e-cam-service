#!/bin/bash
# 测试脚本

set -e

echo "🧪 运行测试套件..."

# 创建测试输出目录
mkdir -p build/test-results

# 运行基本测试
echo "📋 运行基本测试..."
go test -v ./... | tee build/test-results/test.log

# 运行竞态检测测试
echo "🏃 运行竞态检测测试..."
go test -race -v ./... | tee build/test-results/race.log

# 生成覆盖率报告
echo "📊 生成覆盖率报告..."
go test -coverprofile=build/test-results/coverage.out ./...
go tool cover -html=build/test-results/coverage.out -o build/test-results/coverage.html

# 运行基准测试
echo "⚡ 运行基准测试..."
go test -bench=. -benchmem ./... | tee build/test-results/benchmark.log

# 显示覆盖率统计
echo ""
echo "📊 覆盖率统计:"
go tool cover -func=build/test-results/coverage.out | tail -1

echo ""
echo "✅ 测试完成"
echo "测试结果保存在: build/test-results/"
echo "覆盖率报告: build/test-results/coverage.html"