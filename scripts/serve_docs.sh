#!/bin/bash

# API 文档服务器启动脚本

echo "🚀 启动 API 文档服务器..."
echo "================================"

# 检查是否安装了 Python
if command -v python3 &> /dev/null; then
    PYTHON_CMD=python3
elif command -v python &> /dev/null; then
    PYTHON_CMD=python
else
    echo "❌ 未找到 Python，请先安装 Python"
    exit 1
fi

# 进入 docs 目录
cd docs || exit 1

# 启动简单的 HTTP 服务器
echo "📖 API 文档地址: http://localhost:8080"
echo "📖 Swagger UI: http://localhost:8080/swagger-ui.html"
echo ""
echo "按 Ctrl+C 停止服务器"
echo "================================"

$PYTHON_CMD -m http.server 8080
