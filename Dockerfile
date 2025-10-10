# 多阶段构建 Dockerfile

# 构建阶段
FROM golang:1.24.1-alpine AS builder

# 安装必要工具
RUN apk add --no-cache git make

# 设置工作目录
WORKDIR /app

# 复制 go mod 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 安装 Wire
RUN go install github.com/google/wire/cmd/wire@latest

# 复制源代码
COPY . .

# 生成 Wire 代码
RUN wire gen ./ioc && wire gen ./internal/endpoint

# 构建应用程序
ARG VERSION=dev
ARG BUILD_TIME
ARG COMMIT_HASH

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.CommitHash=${COMMIT_HASH}" \
    -a -installsuffix cgo \
    -o e-cam-service .

# 运行阶段
FROM alpine:3.19

# 安装必要的运行时依赖
RUN apk --no-cache add ca-certificates tzdata

# 创建非 root 用户
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/e-cam-service .

# 复制配置文件
COPY --from=builder /app/config ./config

# 创建日志目录
RUN mkdir -p logs && chown -R appuser:appgroup /app

# 切换到非 root 用户
USER appuser

# 暴露端口
EXPOSE 8080 9090

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# 启动应用程序
CMD ["./e-cam-service", "start"]