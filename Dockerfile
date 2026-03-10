# 多阶段构建 Dockerfile
# 构建方式: docker build -t e-cam-service:1.0 .

# 构建阶段
FROM golang:1.24.1-alpine AS builder

# 安装必要工具
RUN apk add --no-cache git make

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

RUN apk --no-cache add ca-certificates tzdata

RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /app/e-cam-service .
COPY --from=builder /app/config ./config

RUN mkdir -p logs && chown -R appuser:appgroup /app

USER appuser

EXPOSE 8080 9090

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./e-cam-service", "start"]
