# E-CAM Service 部署指南

## 目录

- [环境要求](#环境要求)
- [本地开发部署](#本地开发部署)
- [Docker 部署](#docker-部署)
- [生产环境部署](#生产环境部署)
- [CI/CD 流水线](#cicd-流水线)
- [配置说明](#配置说明)
- [运维操作](#运维操作)
- [故障排查](#故障排查)

---

## 环境要求

### 必需组件

| 组件           | 版本    | 说明               |
| -------------- | ------- | ------------------ |
| Go             | 1.24.1+ | 编译语言           |
| MongoDB        | 7.0+    | 主数据库           |
| Redis          | 7.2+    | 缓存/会话          |
| Docker         | 24.0+   | 容器化部署（可选） |
| Docker Compose | 2.20+   | 编排工具（可选）   |

### 可选组件

| 组件          | 用途                    |
| ------------- | ----------------------- |
| Wire          | 依赖注入代码生成        |
| golangci-lint | 代码质量检查            |
| Make          | 构建工具（Linux/macOS） |
| Nginx         | 反向代理                |
| systemd       | 进程管理                |

---

## 本地开发部署

### 1. 克隆项目

```bash
git clone https://github.com/Havens-blog/e-cam-service.git
cd e-cam-service
```

### 2. 启动依赖服务

```bash
# 启动 MongoDB + Redis
docker-compose up -d mongodb redis

# 验证服务状态
docker-compose ps

# （可选）启动管理界面
docker-compose --profile tools up -d
```

默认连接信息：

| 服务            | 地址            | 用户名 | 密码     |
| --------------- | --------------- | ------ | -------- |
| MongoDB         | localhost:27017 | admin  | password |
| Redis           | localhost:6379  | -      | password |
| Mongo Express   | localhost:8082  | -      | -        |
| Redis Commander | localhost:8081  | -      | -        |

### 3. 配置环境

```bash
cp .env.example .env
# 编辑 .env 文件，根据实际环境修改配置
```

### 4. 安装工具 & 生成代码

```bash
# 安装 Wire
go install github.com/google/wire/cmd/wire@latest

# 生成依赖注入代码
wire gen ./ioc
wire gen ./internal/endpoint
```

### 5. 启动服务

```bash
# 方式一：使用 Makefile
make dev

# 方式二：直接运行
go run main.go start

# 方式三：PowerShell（Windows）
.\build.ps1 dev
```

服务默认监听 `http://localhost:8001`。

---

## Docker 部署

### 构建镜像

```bash
# 基础构建
docker build -t e-cam-service:latest .

# 带版本信息构建
docker build \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S') \
  --build-arg COMMIT_HASH=$(git rev-parse --short HEAD) \
  -t e-cam-service:latest .
```

### 运行容器

```bash
docker run -d \
  --name e-cam-service \
  --restart unless-stopped \
  -p 8001:8080 \
  -p 9090:9090 \
  -e MONGO_URI=mongodb://admin:password@host.docker.internal:27017 \
  -e REDIS_ADDR=host.docker.internal:6379 \
  -e REDIS_PASSWORD=password \
  -e CAM_ENCRYPTION_KEY="your-32-byte-secret-key-here!!!" \
  -v $(pwd)/logs:/app/logs \
  --network e-cam-network \
  e-cam-service:latest
```

### Docker Compose 全栈部署

```bash
# 启动所有服务（含数据库）
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down

# 停止并清除数据卷
docker-compose down -v
```

### Dockerfile 说明

项目使用多阶段构建：

1. **构建阶段**（golang:1.24.1-alpine）：编译 Go 二进制、生成 Wire 代码
2. **运行阶段**（alpine:3.19）：最小化运行镜像，非 root 用户运行

暴露端口：

- `8080`：HTTP API
- `9090`：gRPC

---

## 生产环境部署

### 方式一：二进制部署

#### 1. 编译

```bash
# Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X main.Version=v1.0.0 -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S') -X main.CommitHash=$(git rev-parse --short HEAD)" \
  -o e-cam-service .

# 多平台构建
make build-all
```

#### 2. 部署文件

将以下文件上传到服务器：

```
/opt/e-cam-service/
├── e-cam-service          # 二进制文件
├── config/
│   └── prod.yaml          # 生产配置
└── logs/                  # 日志目录
```

#### 3. 配置 systemd 服务

创建 `/etc/systemd/system/e-cam-service.service`：

```ini
[Unit]
Description=E-CAM Service - 多云资产管理后端
After=network.target mongod.service redis.service
Wants=mongod.service redis.service

[Service]
Type=simple
User=ecam
Group=ecam
WorkingDirectory=/opt/e-cam-service
ExecStart=/opt/e-cam-service/e-cam-service start
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

# 环境变量
Environment=CAM_ENCRYPTION_KEY=your-production-encryption-key
Environment=GIN_MODE=release

# 日志
StandardOutput=journal
StandardError=journal
SyslogIdentifier=e-cam-service

[Install]
WantedBy=multi-user.target
```

```bash
# 创建用户
sudo useradd -r -s /bin/false ecam

# 设置权限
sudo chown -R ecam:ecam /opt/e-cam-service

# 启用并启动服务
sudo systemctl daemon-reload
sudo systemctl enable e-cam-service
sudo systemctl start e-cam-service

# 查看状态
sudo systemctl status e-cam-service
journalctl -u e-cam-service -f
```

### 方式二：Docker 生产部署

```bash
# 拉取镜像
docker pull your-registry/e-cam-service:latest

# 运行
docker run -d \
  --name e-cam-service \
  --restart always \
  -p 8001:8080 \
  -p 9090:9090 \
  -e CAM_ENCRYPTION_KEY="production-key" \
  -e GIN_MODE=release \
  -v /data/e-cam-service/logs:/app/logs \
  -v /data/e-cam-service/config:/app/config:ro \
  --network cam-network \
  your-registry/e-cam-service:latest
```

### Nginx 反向代理配置

```nginx
upstream e-cam-backend {
    server 127.0.0.1:8001;
    keepalive 32;
}

server {
    listen 443 ssl http2;
    server_name api.example.com;

    ssl_certificate     /etc/nginx/ssl/cert.pem;
    ssl_certificate_key /etc/nginx/ssl/key.pem;

    location /api/v1/cam/ {
        proxy_pass http://e-cam-backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Authorization $http_authorization;
        proxy_set_header X-Tenant-ID $http_x_tenant_id;

        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }

    location /health {
        proxy_pass http://e-cam-backend/health;
        access_log off;
    }
}
```

---

## CI/CD 流水线

项目使用 GitHub Actions，配置文件位于 `.github/workflows/ci.yml`。

### 触发条件

| 事件         | 分支          | 执行内容                  |
| ------------ | ------------- | ------------------------- |
| push         | main, develop | 测试 + 构建 + Docker 推送 |
| pull_request | main, develop | 测试 + 构建               |
| release      | -             | 测试 + 构建 + 发布资产    |

### 流水线阶段

1. **Test**：单元测试、覆盖率、lint、vet
2. **Build**：多平台二进制编译（linux/windows/darwin × amd64/arm64）
3. **Docker**：构建并推送 Docker 镜像（仅 main/develop 分支）
4. **Release**：上传二进制到 GitHub Release（仅 release 事件）

### 所需 Secrets

| Secret            | 说明                        |
| ----------------- | --------------------------- |
| `DOCKER_USERNAME` | Docker Hub 用户名           |
| `DOCKER_PASSWORD` | Docker Hub 密码/Token       |
| `GITHUB_TOKEN`    | 自动提供，用于 Release 上传 |

---

## 配置说明

### 生产配置 (config/prod.yaml)

```yaml
e-cam-service:
  port: "8001" # HTTP 服务端口

security:
  encryption_key: "${CAM_ENCRYPTION_KEY}" # AES 加密密钥（16/24/32 字节）

logger:
  default:
    level: "info" # 日志级别：debug/info/warn/error
    format: "json" # 生产环境建议 json 格式
    outputPaths:
      - "stdout"
      - "logs/app.log"

redis:
  addr: "redis-host:6379"
  password: "your-redis-password"
  db: 3

session:
  cookie:
    domain: ".your-domain.com" # Cookie 域名
    name: "ecmdb-token-key"

grpc:
  server:
    port: 9090 # gRPC 端口
```

### 环境变量

| 变量                 | 必需 | 说明                                 |
| -------------------- | ---- | ------------------------------------ |
| `CAM_ENCRYPTION_KEY` | 是   | AES 加密密钥，必须 16/24/32 字节     |
| `GIN_MODE`           | 否   | Gin 运行模式，生产环境设为 `release` |
| `MONGO_URI`          | 否   | MongoDB 连接串（也可在 yaml 中配置） |
| `REDIS_ADDR`         | 否   | Redis 地址（也可在 yaml 中配置）     |

---

## 运维操作

### 健康检查

```bash
# HTTP 健康检查
curl http://localhost:8001/health

# Docker 容器健康状态
docker inspect --format='{{.State.Health.Status}}' e-cam-service
```

### 日志查看

```bash
# systemd 日志
journalctl -u e-cam-service -f --no-pager

# Docker 日志
docker logs -f e-cam-service --tail 100

# 文件日志
tail -f /opt/e-cam-service/logs/app.log
```

### 滚动更新（Docker）

```bash
# 拉取新镜像
docker pull your-registry/e-cam-service:latest

# 停止旧容器
docker stop e-cam-service
docker rm e-cam-service

# 启动新容器（使用上面的 docker run 命令）
```

### 数据库备份

```bash
# MongoDB 备份
mongodump --uri="mongodb://admin:password@localhost:27017" \
  --db=e_cam_service \
  --out=/backup/$(date +%Y%m%d)

# MongoDB 恢复
mongorestore --uri="mongodb://admin:password@localhost:27017" \
  --db=e_cam_service \
  /backup/20250101/e_cam_service
```

---

## 故障排查

### 常见问题

#### 1. 服务启动失败

```bash
# 检查端口占用
netstat -tlnp | grep 8001
# 或 Windows
netstat -ano | findstr :8001

# 检查配置文件语法
cat config/prod.yaml | python3 -c "import sys,yaml; yaml.safe_load(sys.stdin)"
```

#### 2. MongoDB 连接失败

```bash
# 测试连接
mongosh "mongodb://admin:password@localhost:27017" --eval "db.adminCommand('ping')"

# 检查网络
telnet mongodb-host 27017
```

#### 3. Redis 连接失败

```bash
# 测试连接
redis-cli -h localhost -p 6379 -a password ping
```

#### 4. Wire 代码生成失败

```bash
# 清理并重新生成
rm -f ioc/wire_gen.go internal/endpoint/wire_gen.go
wire gen ./ioc
wire gen ./internal/endpoint
```

#### 5. Docker 构建失败

```bash
# 清理 Docker 缓存
docker builder prune -f

# 无缓存构建
docker build --no-cache -t e-cam-service:latest .
```

### 性能调优

- MongoDB：确保常用查询字段有索引
- Redis：监控内存使用，配置合理的 maxmemory
- Go 服务：通过 `GOMAXPROCS` 控制并发度
- 文件描述符：生产环境建议 `LimitNOFILE=65536`
