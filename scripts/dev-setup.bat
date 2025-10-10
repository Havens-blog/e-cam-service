@echo off
REM Windows 开发环境设置脚本

echo 🚀 设置 E-Cam Service 开发环境...

REM 检查必要工具
echo 📋 检查必要工具...

where go >nul 2>nul
if %errorlevel% neq 0 (
    echo ❌ Go 未安装，请先安装 Go
    exit /b 1
) else (
    echo ✅ Go 已安装
)

where git >nul 2>nul
if %errorlevel% neq 0 (
    echo ❌ Git 未安装，请先安装 Git
    exit /b 1
) else (
    echo ✅ Git 已安装
)

where make >nul 2>nul
if %errorlevel% neq 0 (
    echo ⚠️  Make 未安装，建议安装 Make 或使用 PowerShell
) else (
    echo ✅ Make 已安装
)

REM 检查可选工具
echo 📋 检查可选工具...

where docker >nul 2>nul
if %errorlevel% neq 0 (
    echo ⚠️  Docker 未安装，数据库服务将不可用
    set DOCKER_AVAILABLE=false
) else (
    echo ✅ Docker 已安装
    set DOCKER_AVAILABLE=true
)

where docker-compose >nul 2>nul
if %errorlevel% neq 0 (
    echo ⚠️  Docker Compose 未安装，数据库服务将不可用
    set COMPOSE_AVAILABLE=false
) else (
    echo ✅ Docker Compose 已安装
    set COMPOSE_AVAILABLE=true
)

REM 创建必要目录
echo 📁 创建项目目录...
if not exist logs mkdir logs
if not exist build mkdir build
if not exist dist mkdir dist
if not exist tmp mkdir tmp

REM 复制环境变量文件
if not exist .env (
    echo 📝 创建环境变量文件...
    copy .env.example .env
    echo ✅ 已创建 .env 文件，请根据需要修改配置
) else (
    echo ✅ .env 文件已存在
)

REM 安装 Go 工具
echo 🔧 安装开发工具...
go install github.com/google/wire/cmd/wire@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

REM 下载依赖
echo 📦 下载项目依赖...
go mod download
go mod tidy

REM 生成 Wire 代码
echo 🔌 生成 Wire 依赖注入代码...
wire gen ./ioc
wire gen ./internal/endpoint

REM 启动数据库服务（如果 Docker 可用）
if "%DOCKER_AVAILABLE%"=="true" if "%COMPOSE_AVAILABLE%"=="true" (
    echo 🗄️  启动数据库服务...
    docker-compose up -d mongodb redis
    echo ⏳ 等待数据库启动...
    timeout /t 10 /nobreak >nul
) else (
    echo ⚠️  跳过数据库服务启动（Docker 不可用）
)

REM 运行测试
echo 🧪 运行测试...
go test -v ./...

echo.
echo 🎉 开发环境设置完成！
echo.
echo 常用命令：
echo   make help          - 查看所有可用命令
echo   make dev           - 启动开发服务器
echo   make build         - 构建应用程序
echo   make test          - 运行测试
echo   make fmt           - 格式化代码
echo   make lint          - 代码检查
echo.
if "%DOCKER_AVAILABLE%"=="true" if "%COMPOSE_AVAILABLE%"=="true" (
    echo 数据库管理界面：
    echo   MongoDB: http://localhost:8082
    echo   Redis:   http://localhost:8081
    echo.
)
echo 开始开发：
echo   make dev

pause