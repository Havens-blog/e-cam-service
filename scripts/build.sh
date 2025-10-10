#!/bin/bash
# 构建脚本

set -e

# 配置
PROJECT_NAME="e-cam-service"
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建标志
LDFLAGS="-ldflags -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.CommitHash=${COMMIT_HASH}"

echo "🔨 构建 ${PROJECT_NAME}"
echo "版本: ${VERSION}"
echo "构建时间: ${BUILD_TIME}"
echo "提交哈希: ${COMMIT_HASH}"
echo ""

# 清理旧文件
echo "🧹 清理旧文件..."
rm -rf build/
rm -rf dist/
mkdir -p build/
mkdir -p dist/

# 生成 Wire 代码
echo "🔌 生成 Wire 代码..."
wire gen ./ioc
wire gen ./internal/endpoint

# 运行测试
echo "🧪 运行测试..."
go test -v ./...

# 构建当前平台
echo "🔨 构建当前平台..."
go build ${LDFLAGS} -o build/${PROJECT_NAME} .

# 构建多平台（如果指定）
if [ "$1" = "all" ]; then
    echo "🔨 构建所有平台..."
    
    platforms=(
        "windows/amd64"
        "linux/amd64"
        "darwin/amd64"
        "darwin/arm64"
    )
    
    for platform in "${platforms[@]}"; do
        OS=$(echo $platform | cut -d'/' -f1)
        ARCH=$(echo $platform | cut -d'/' -f2)
        OUTPUT_NAME=${PROJECT_NAME}-${OS}-${ARCH}
        
        if [ $OS = "windows" ]; then
            OUTPUT_NAME=${OUTPUT_NAME}.exe
        fi
        
        echo "构建 ${OS}/${ARCH}..."
        GOOS=$OS GOARCH=$ARCH go build ${LDFLAGS} -o dist/${OUTPUT_NAME} .
    done
fi

echo "✅ 构建完成"

# 显示构建结果
echo ""
echo "构建结果:"
if [ -f build/${PROJECT_NAME} ]; then
    echo "  当前平台: build/${PROJECT_NAME}"
fi

if [ "$1" = "all" ]; then
    echo "  多平台构建:"
    ls -la dist/
fi