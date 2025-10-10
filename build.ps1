# PowerShell 构建脚本 - 替代 Makefile
# E-Cam Service 开发工具

param(
    [Parameter(Position=0)]
    [string]$Command = "help",
    
    [Parameter(Position=1)]
    [string]$Target = ""
)

# 项目配置
$PROJECT_NAME = "e-cam-service"
$BINARY_NAME = "e-cam-service"
$MODULE_NAME = "github.com/Havens-blog/e-cam-service"

# 获取版本信息
try {
    $VERSION = git describe --tags --always --dirty 2>$null
    if (-not $VERSION) { $VERSION = "dev" }
} catch {
    $VERSION = "dev"
}

$BUILD_TIME = Get-Date -Format "yyyy-MM-dd_HH:mm:ss"

try {
    $COMMIT_HASH = git rev-parse --short HEAD 2>$null
    if (-not $COMMIT_HASH) { $COMMIT_HASH = "unknown" }
} catch {
    $COMMIT_HASH = "unknown"
}

# 构建标志
$LDFLAGS = "-ldflags `"-X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME -X main.CommitHash=$COMMIT_HASH`""

# 目录配置
$BUILD_DIR = "build"
$DIST_DIR = "dist"
$LOGS_DIR = "logs"

# 平台配置
$PLATFORMS = @(
    @{OS="windows"; ARCH="amd64"},
    @{OS="linux"; ARCH="amd64"},
    @{OS="darwin"; ARCH="amd64"},
    @{OS="darwin"; ARCH="arm64"}
)

# 颜色输出函数
function Write-ColorOutput {
    param(
        [string]$Message,
        [string]$Color = "White"
    )
    Write-Host $Message -ForegroundColor $Color
}

function Write-Success {
    param([string]$Message)
    Write-ColorOutput "✅ $Message" "Green"
}

function Write-Info {
    param([string]$Message)
    Write-ColorOutput "🔨 $Message" "Cyan"
}

function Write-Warning {
    param([string]$Message)
    Write-ColorOutput "⚠️  $Message" "Yellow"
}

function Write-Error {
    param([string]$Message)
    Write-ColorOutput "❌ $Message" "Red"
}

# 帮助信息
function Show-Help {
    Write-ColorOutput "$PROJECT_NAME - 开发工具" "Yellow"
    Write-Host ""
    Write-Host "使用方法: .\build.ps1 <命令> [参数]"
    Write-Host ""
    Write-Host "可用命令:"
    Write-Host "  help              显示帮助信息"
    Write-Host "  dev               启动开发服务器"
    Write-Host "  build             构建应用程序"
    Write-Host "  build-all         构建所有平台的二进制文件"
    Write-Host "  test              运行测试"
    Write-Host "  test-coverage     运行测试并生成覆盖率报告"
    Write-Host "  test-race         运行竞态检测测试"
    Write-Host "  benchmark         运行基准测试"
    Write-Host "  fmt               格式化代码"
    Write-Host "  lint              运行代码检查"
    Write-Host "  vet               运行 go vet"
    Write-Host "  wire              生成 Wire 依赖注入代码"
    Write-Host "  deps              下载依赖"
    Write-Host "  deps-update       更新依赖"
    Write-Host "  clean             清理构建文件"
    Write-Host "  clean-cache       清理 Go 缓存"
    Write-Host "  tools             安装开发工具"
    Write-Host "  init              初始化项目"
    Write-Host "  run               运行应用程序"
    Write-Host "  run-start         运行 start 命令"
    Write-Host "  run-endpoint      运行 endpoint 命令"
    Write-Host "  db-up             启动数据库服务"
    Write-Host "  db-down           停止数据库服务"
    Write-Host "  info              显示项目信息"
    Write-Host "  check             运行所有检查"
    Write-Host ""
    Write-Host "示例:"
    Write-Host "  .\build.ps1 dev"
    Write-Host "  .\build.ps1 build"
    Write-Host "  .\build.ps1 test"
}

# 检查工具是否存在
function Test-Command {
    param([string]$CommandName)
    try {
        Get-Command $CommandName -ErrorAction Stop | Out-Null
        return $true
    } catch {
        return $false
    }
}

# 创建目录
function New-DirectoryIfNotExists {
    param([string]$Path)
    if (-not (Test-Path $Path)) {
        New-Item -ItemType Directory -Path $Path -Force | Out-Null
    }
}

# 开发服务器
function Start-Dev {
    Write-Info "启动开发服务器..."
    Invoke-Wire
    go run main.go start
}

# 构建应用程序
function Invoke-Build {
    Write-Info "构建应用程序..."
    Invoke-Clean
    Invoke-Wire
    New-DirectoryIfNotExists $BUILD_DIR
    
    $cmd = "go build $LDFLAGS -o $BUILD_DIR\$BINARY_NAME.exe ."
    Invoke-Expression $cmd
    
    if ($LASTEXITCODE -eq 0) {
        Write-Success "构建完成: $BUILD_DIR\$BINARY_NAME.exe"
    } else {
        Write-Error "构建失败"
        exit 1
    }
}

# 构建所有平台
function Invoke-BuildAll {
    Write-Info "构建所有平台..."
    Invoke-Clean
    Invoke-Wire
    New-DirectoryIfNotExists $DIST_DIR
    
    foreach ($platform in $PLATFORMS) {
        $OS = $platform.OS
        $ARCH = $platform.ARCH
        $OUTPUT_NAME = "$BINARY_NAME-$OS-$ARCH"
        
        if ($OS -eq "windows") {
            $OUTPUT_NAME += ".exe"
        }
        
        Write-Info "构建 $OS/$ARCH..."
        
        $env:GOOS = $OS
        $env:GOARCH = $ARCH
        
        $cmd = "go build $LDFLAGS -o $DIST_DIR\$OUTPUT_NAME ."
        Invoke-Expression $cmd
        
        if ($LASTEXITCODE -ne 0) {
            Write-Error "构建 $OS/$ARCH 失败"
            exit 1
        }
    }
    
    # 重置环境变量
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    
    Write-Success "所有平台构建完成"
}

# 运行测试
function Invoke-Test {
    Write-Info "运行测试..."
    go test -v ./...
}

# 测试覆盖率
function Invoke-TestCoverage {
    Write-Info "运行测试覆盖率..."
    New-DirectoryIfNotExists $BUILD_DIR
    go test -v -coverprofile="$BUILD_DIR\coverage.out" ./...
    go tool cover -html="$BUILD_DIR\coverage.out" -o "$BUILD_DIR\coverage.html"
    Write-Success "覆盖率报告生成: $BUILD_DIR\coverage.html"
}

# 竞态检测测试
function Invoke-TestRace {
    Write-Info "运行竞态检测测试..."
    go test -race -v ./...
}

# 基准测试
function Invoke-Benchmark {
    Write-Info "运行基准测试..."
    go test -bench=. -benchmem ./...
}

# 格式化代码
function Invoke-Format {
    Write-Info "格式化代码..."
    gofmt -s -w .
    go mod tidy
    Write-Success "代码格式化完成"
}

# 代码检查
function Invoke-Lint {
    Write-Info "运行代码检查..."
    if (Test-Command "golangci-lint") {
        golangci-lint run ./...
    } else {
        Write-Warning "golangci-lint 未安装，跳过代码检查"
        Write-Host "安装命令: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
    }
}

# Go vet
function Invoke-Vet {
    Write-Info "运行 go vet..."
    go vet ./...
    Write-Success "go vet 检查完成"
}

# Wire 代码生成
function Invoke-Wire {
    Write-Info "生成 Wire 代码..."
    if (Test-Command "wire") {
        wire gen ./ioc
        wire gen ./internal/endpoint
    } else {
        Write-Warning "wire 未安装，正在安装..."
        go install github.com/google/wire/cmd/wire@latest
        wire gen ./ioc
        wire gen ./internal/endpoint
    }
    Write-Success "Wire 代码生成完成"
}

# 下载依赖
function Invoke-Deps {
    Write-Info "下载依赖..."
    go mod download
    go mod tidy
}

# 更新依赖
function Invoke-DepsUpdate {
    Write-Info "更新依赖..."
    go mod tidy
    go get -u ./...
    go mod tidy
}

# 清理构建文件
function Invoke-Clean {
    Write-Info "清理构建文件..."
    go clean
    if (Test-Path $BUILD_DIR) { Remove-Item -Recurse -Force $BUILD_DIR }
    if (Test-Path $DIST_DIR) { Remove-Item -Recurse -Force $DIST_DIR }
    if (Test-Path "$BINARY_NAME.exe") { Remove-Item "$BINARY_NAME.exe" }
    Write-Success "清理完成"
}

# 清理缓存
function Invoke-CleanCache {
    Write-Info "清理 Go 缓存..."
    go clean -cache -modcache -testcache
    Write-Success "缓存清理完成"
}

# 安装工具
function Install-Tools {
    Write-Info "安装开发工具..."
    go install github.com/google/wire/cmd/wire@latest
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    Write-Success "开发工具安装完成"
}

# 初始化项目
function Initialize-Project {
    Write-Info "初始化项目..."
    Install-Tools
    Invoke-Deps
    Invoke-Wire
    New-DirectoryIfNotExists $LOGS_DIR
    New-DirectoryIfNotExists $BUILD_DIR
    Write-Success "项目初始化完成"
}

# 运行应用程序
function Start-App {
    Write-Info "运行应用程序..."
    Invoke-Wire
    go run main.go
}

function Start-AppStart {
    Write-Info "运行 start 命令..."
    Invoke-Wire
    go run main.go start
}

function Start-AppEndpoint {
    Write-Info "运行 endpoint 命令..."
    Invoke-Wire
    go run main.go endpoint
}

# 数据库服务
function Start-Database {
    Write-Info "启动数据库服务..."
    if (Test-Path "docker-compose.yml") {
        docker-compose up -d mongodb redis
    } else {
        Write-Warning "docker-compose.yml 文件不存在"
    }
}

function Stop-Database {
    Write-Info "停止数据库服务..."
    if (Test-Path "docker-compose.yml") {
        docker-compose down
    } else {
        Write-Warning "docker-compose.yml 文件不存在"
    }
}

# 显示项目信息
function Show-Info {
    Write-Host "项目信息:"
    Write-Host "  名称: $PROJECT_NAME"
    Write-Host "  模块: $MODULE_NAME"
    Write-Host "  版本: $VERSION"
    Write-Host "  构建时间: $BUILD_TIME"
    Write-Host "  提交哈希: $COMMIT_HASH"
    Write-Host "  Go 版本: $(go version)"
}

# 运行所有检查
function Invoke-Check {
    Write-Info "运行所有检查..."
    Invoke-Format
    Invoke-Vet
    Invoke-Lint
    Invoke-Test
    Write-Success "所有检查完成"
}

# 主逻辑
switch ($Command.ToLower()) {
    "help" { Show-Help }
    "dev" { Start-Dev }
    "build" { Invoke-Build }
    "build-all" { Invoke-BuildAll }
    "test" { Invoke-Test }
    "test-coverage" { Invoke-TestCoverage }
    "test-race" { Invoke-TestRace }
    "benchmark" { Invoke-Benchmark }
    "fmt" { Invoke-Format }
    "lint" { Invoke-Lint }
    "vet" { Invoke-Vet }
    "wire" { Invoke-Wire }
    "deps" { Invoke-Deps }
    "deps-update" { Invoke-DepsUpdate }
    "clean" { Invoke-Clean }
    "clean-cache" { Invoke-CleanCache }
    "tools" { Install-Tools }
    "init" { Initialize-Project }
    "run" { Start-App }
    "run-start" { Start-AppStart }
    "run-endpoint" { Start-AppEndpoint }
    "db-up" { Start-Database }
    "db-down" { Stop-Database }
    "info" { Show-Info }
    "check" { Invoke-Check }
    default {
        Write-Error "未知命令: $Command"
        Write-Host ""
        Show-Help
        exit 1
    }
}