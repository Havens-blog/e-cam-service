#!/bin/bash
# 开发环境设置脚本

set -e

echo "🚀 设置 E-Cam Service 开发环境..."

# 检查必要工具
check_tool() {
    if ! command -v $1 &> /dev/null; then
        echo "❌ $1 未安装，请先安装 $1"
        exit 1
    else
        echo "✅ $1 已安装"
    fi
}

echo "📋 检查必要工具..."
check_tool "go"
check_tool "git"
check_tool "make"

# 检查可选工具
echo "📋 检查可选工具..."
if command -v docker &> /dev/null; then
    echo "✅ Docker 已安装"
    DOCKER_AVAILABLE=true
else
    echo "⚠️  Docker 未安装，数据库服务将不可用"
    DOCKER_AVAILABLE=false
fi

if command -v docker-compose &> /dev/null; then
    echo "✅ Docker Compose 已安装"
    COMPOSE_AVAILABLE=true
else
    echo "⚠️  Docker Compose 未安装，数据库服务将不可用"
    COMPOSE_AVAILABLE=false
fi

# 创建必要目录
echo "📁 创建项目目录..."
mkdir -p logs
mkdir -p build
mkdir -p dist
mkdir -p tmp

# 复制环境变量文件
if [ ! -f .env ]; then
    echo "📝 创建环境变量文件..."
    cp .env.example .env
    echo "✅ 已创建 .env 文件，请根据需要修改配置"
else
    echo "✅ .env 文件已存在"
fi

# 安装 Go 工具
echo "🔧 安装开发工具..."
make tools

# 下载依赖
echo "📦 下载项目依赖..."
make deps

# 生成 Wire 代码
echo "🔌 生成 Wire 依赖注入代码..."
make wire

# 启动数据库服务（如果 Docker 可用）
if [ "$DOCKER_AVAILABLE" = true ] && [ "$COMPOSE_AVAILABLE" = true ]; then
    echo "🗄️  启动数据库服务..."
    make db-up
    echo "⏳ 等待数据库启动..."
    sleep 10
else
    echo "⚠️  跳过数据库服务启动（Docker 不可用）"
fi

# 运行测试
echo "🧪 运行测试..."
make test

echo ""
echo "🎉 开发环境设置完成！"
echo ""
echo "常用命令："
echo "  make help          - 查看所有可用命令"
echo "  make dev           - 启动开发服务器"
echo "  make build         - 构建应用程序"
echo "  make test          - 运行测试"
echo "  make fmt           - 格式化代码"
echo "  make lint          - 代码检查"
echo ""
if [ "$DOCKER_AVAILABLE" = true ] && [ "$COMPOSE_AVAILABLE" = true ]; then
    echo "数据库管理界面："
    echo "  MongoDB: http://localhost:8082"
    echo "  Redis:   http://localhost:8081"
    echo ""
fi
echo "开始开发："
echo "  make dev"