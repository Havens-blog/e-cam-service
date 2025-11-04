@echo off
REM API 文档服务器启动脚本 (Windows)

echo 🚀 启动 API 文档服务器...
echo ================================

REM 检查是否安装了 Python
where python >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo ❌ 未找到 Python，请先安装 Python
    exit /b 1
)

REM 进入 docs 目录
cd docs

REM 启动简单的 HTTP 服务器
echo 📖 API 文档地址: http://localhost:8080
echo 📖 Swagger UI: http://localhost:8080/swagger-ui.html
echo.
echo 按 Ctrl+C 停止服务器
echo ================================

python -m http.server 8080
