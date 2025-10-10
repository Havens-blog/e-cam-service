#!/bin/bash
# 部署脚本

set -e

# 配置
PROJECT_NAME="e-cam-service"
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
DEPLOY_ENV=${DEPLOY_ENV:-"staging"}

echo "🚀 部署 ${PROJECT_NAME} 到 ${DEPLOY_ENV}"
echo "版本: ${VERSION}"
echo ""

# 检查部署环境
case $DEPLOY_ENV in
    "staging"|"production")
        echo "✅ 部署环境: ${DEPLOY_ENV}"
        ;;
    *)
        echo "❌ 无效的部署环境: ${DEPLOY_ENV}"
        echo "支持的环境: staging, production"
        exit 1
        ;;
esac

# 确认部署
if [ "$DEPLOY_ENV" = "production" ]; then
    echo "⚠️  即将部署到生产环境！"
    read -p "确认继续？(y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "❌ 部署已取消"
        exit 1
    fi
fi

# 运行测试
echo "🧪 运行测试..."
./scripts/test.sh

# 构建应用
echo "🔨 构建应用..."
./scripts/build.sh all

# 创建部署包
echo "📦 创建部署包..."
DEPLOY_DIR="deploy/${DEPLOY_ENV}"
mkdir -p ${DEPLOY_DIR}

# 复制二进制文件
cp dist/${PROJECT_NAME}-linux-amd64 ${DEPLOY_DIR}/${PROJECT_NAME}

# 复制配置文件
cp -r config/ ${DEPLOY_DIR}/config/
cp docker-compose.yml ${DEPLOY_DIR}/
cp .env.example ${DEPLOY_DIR}/

# 创建部署脚本
cat > ${DEPLOY_DIR}/start.sh << 'EOF'
#!/bin/bash
# 服务启动脚本

set -e

echo "🚀 启动 e-cam-service..."

# 检查配置文件
if [ ! -f .env ]; then
    echo "❌ .env 文件不存在，请复制 .env.example 并配置"
    exit 1
fi

# 启动数据库服务
echo "🗄️  启动数据库服务..."
docker-compose up -d mongodb redis

# 等待数据库启动
echo "⏳ 等待数据库启动..."
sleep 10

# 启动应用服务
echo "🚀 启动应用服务..."
./e-cam-service start

EOF

chmod +x ${DEPLOY_DIR}/start.sh

# 创建停止脚本
cat > ${DEPLOY_DIR}/stop.sh << 'EOF'
#!/bin/bash
# 服务停止脚本

echo "🛑 停止 e-cam-service..."

# 停止应用服务
pkill -f e-cam-service || true

# 停止数据库服务
docker-compose down

echo "✅ 服务已停止"
EOF

chmod +x ${DEPLOY_DIR}/stop.sh

# 创建部署信息文件
cat > ${DEPLOY_DIR}/deploy-info.txt << EOF
部署信息
========
项目: ${PROJECT_NAME}
版本: ${VERSION}
环境: ${DEPLOY_ENV}
部署时间: $(date -u '+%Y-%m-%d %H:%M:%S UTC')
提交哈希: $(git rev-parse HEAD 2>/dev/null || echo "unknown")

部署文件:
- ${PROJECT_NAME}: 应用程序二进制文件
- config/: 配置文件目录
- docker-compose.yml: 数据库服务配置
- .env.example: 环境变量模板
- start.sh: 启动脚本
- stop.sh: 停止脚本

使用方法:
1. 复制 .env.example 为 .env 并配置
2. 运行 ./start.sh 启动服务
3. 运行 ./stop.sh 停止服务
EOF

# 创建压缩包
echo "📦 创建部署压缩包..."
cd deploy/
tar -czf ${PROJECT_NAME}-${VERSION}-${DEPLOY_ENV}.tar.gz ${DEPLOY_ENV}/
cd ..

echo ""
echo "✅ 部署包创建完成"
echo "部署目录: ${DEPLOY_DIR}"
echo "压缩包: deploy/${PROJECT_NAME}-${VERSION}-${DEPLOY_ENV}.tar.gz"
echo ""
echo "部署步骤:"
echo "1. 将压缩包上传到目标服务器"
echo "2. 解压: tar -xzf ${PROJECT_NAME}-${VERSION}-${DEPLOY_ENV}.tar.gz"
echo "3. 进入目录: cd ${DEPLOY_ENV}"
echo "4. 配置环境变量: cp .env.example .env && vi .env"
echo "5. 启动服务: ./start.sh"