#!/bin/bash
# e-cam-service 生产环境部署脚本
# 使用方式: bash scripts/deploy-prod.sh [命令]
# 命令: build | deploy | rollback | restart | stop | status | logs | cleanup

set -euo pipefail

# ==================== 配置 ====================
APP_NAME="e-cam-service"
IMAGE_NAME="e-cam-service"
CONTAINER_NAME="e-cam-service"
NETWORK_NAME="e-cam-network"
REGISTRY="${DOCKER_REGISTRY:-your-registry.example.com/your-namespace/e-cam}"
REGISTRY_TAG="latest"
APP_PORT=8001
GRPC_PORT=8099
CONFIG_FILE="config/prod.yaml"
DEPLOY_DIR=$(cd "$(dirname "$0")/.." && pwd)
BACKUP_DIR="${DEPLOY_DIR}/backups"
LOG_DIR="${DEPLOY_DIR}/logs"
MAX_BACKUPS=5

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $(date '+%Y-%m-%d %H:%M:%S') $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date '+%Y-%m-%d %H:%M:%S') $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') $*"; }
log_step()  { echo -e "${CYAN}[STEP]${NC}  $(date '+%Y-%m-%d %H:%M:%S') $*"; }

# ==================== 前置检查 ====================
check_docker() {
    if ! command -v docker &>/dev/null; then
        log_error "Docker 未安装"
        exit 1
    fi
    if ! docker info &>/dev/null; then
        log_error "Docker 服务未运行"
        exit 1
    fi
}

check_config() {
    if [ ! -f "${DEPLOY_DIR}/${CONFIG_FILE}" ]; then
        log_error "配置文件不存在: ${CONFIG_FILE}"
        exit 1
    fi
}

# ==================== 网络检查 ====================
ensure_network() {
    if ! docker network inspect "${NETWORK_NAME}" &>/dev/null; then
        log_step "创建 Docker 网络: ${NETWORK_NAME}"
        docker network create "${NETWORK_NAME}"
        log_info "网络创建完成"
    fi
}

# ==================== 版本信息 ====================
get_version() {
    git describe --tags --always --dirty 2>/dev/null || echo "dev"
}

get_commit() {
    git rev-parse --short HEAD 2>/dev/null || echo "unknown"
}

# ==================== 构建镜像 ====================
do_build() {
    check_docker

    local version commit build_time tag
    version=$(get_version)
    commit=$(get_commit)
    build_time=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    tag="${IMAGE_NAME}:${version}"

    log_info "=========================================="
    log_info "  构建 ${APP_NAME}"
    log_info "  版本: ${version}"
    log_info "  提交: ${commit}"
    log_info "=========================================="

    # 可选: 拉取最新代码
    if [ "${PULL:-false}" = "true" ]; then
        log_step "拉取最新代码..."
        git pull --rebase 2>/dev/null || log_warn "git pull 失败，使用本地代码"
    fi

    log_step "构建镜像: ${tag}"
    docker build \
        -f "${DEPLOY_DIR}/deploy/Dockerfile" \
        --build-arg VERSION="${version}" \
        --build-arg BUILD_TIME="${build_time}" \
        --build-arg COMMIT_HASH="${commit}" \
        -t "${tag}" \
        -t "${IMAGE_NAME}:latest" \
        "${DEPLOY_DIR}"

    log_info "=========================================="
    log_info "  构建完成: ${tag}"
    log_info "=========================================="

    # 推送到阿里云镜像仓库
    log_step "推送镜像到仓库: ${REGISTRY}:${REGISTRY_TAG}"
    docker tag "${IMAGE_NAME}:latest" "${REGISTRY}:${REGISTRY_TAG}"
    docker push "${REGISTRY}:${REGISTRY_TAG}"
    log_info "推送完成: ${REGISTRY}:${REGISTRY_TAG}"
}

# ==================== 备份当前版本 ====================
backup_current() {
    local current_image
    current_image=$(docker inspect --format='{{.Config.Image}}' "${CONTAINER_NAME}" 2>/dev/null || echo "")

    if [ -n "${current_image}" ]; then
        local backup_tag="backup-$(date '+%Y%m%d%H%M%S')"
        log_step "备份当前镜像: ${current_image} -> ${IMAGE_NAME}:${backup_tag}"
        docker tag "${current_image}" "${IMAGE_NAME}:${backup_tag}" 2>/dev/null || true

        mkdir -p "${BACKUP_DIR}"
        echo "${backup_tag}|${current_image}|$(date '+%Y-%m-%d %H:%M:%S')" >> "${BACKUP_DIR}/history.log"

        # 清理旧备份
        local backup_count
        backup_count=$(docker images "${IMAGE_NAME}" --format '{{.Tag}}' | grep '^backup-' | wc -l)
        if [ "${backup_count}" -gt "${MAX_BACKUPS}" ]; then
            log_info "清理旧备份镜像 (保留最近 ${MAX_BACKUPS} 个)..."
            docker images "${IMAGE_NAME}" --format '{{.Tag}}' | grep '^backup-' | sort | head -n -${MAX_BACKUPS} | while read -r old_tag; do
                docker rmi "${IMAGE_NAME}:${old_tag}" 2>/dev/null || true
            done
        fi
    fi
}

# ==================== 启动容器 (公共逻辑) ====================
start_container() {
    local image_tag="$1"

    docker run -d \
        --name "${CONTAINER_NAME}" \
        --restart unless-stopped \
        --network "${NETWORK_NAME}" \
        -p ${APP_PORT}:${APP_PORT} \
        -p ${GRPC_PORT}:${GRPC_PORT} \
        -v "${DEPLOY_DIR}/config:/app/config:ro" \
        -v "${LOG_DIR}:/app/logs" \
        -e EGO_CONFIG_PATH="/app/config/prod.yaml" \
        -e TZ=Asia/Shanghai \
        --health-cmd="wget --no-verbose --tries=1 --spider http://localhost:${APP_PORT}/api/v1/cam/health || exit 1" \
        --health-interval=30s \
        --health-timeout=3s \
        --health-start-period=10s \
        --health-retries=3 \
        "${image_tag}" \
        ./e-cam-service start
}

# ==================== 健康检查 ====================
health_check() {
    log_step "健康检查..."
    local retries=0
    local max_retries=20

    while [ ${retries} -lt ${max_retries} ]; do
        local status
        status=$(docker inspect --format='{{.State.Health.Status}}' "${CONTAINER_NAME}" 2>/dev/null || echo "starting")

        case "${status}" in
            healthy)
                log_info "服务健康检查通过"
                return 0
                ;;
            unhealthy)
                log_error "服务健康检查失败"
                docker logs --tail 30 "${CONTAINER_NAME}"
                return 1
                ;;
            *)
                retries=$((retries + 1))
                echo -ne "\r  等待健康检查... (${retries}/${max_retries})"
                sleep 3
                ;;
        esac
    done

    echo ""
    if docker ps -q -f name="${CONTAINER_NAME}" &>/dev/null; then
        log_warn "容器运行中，但健康检查未通过，请手动确认"
        docker logs --tail 10 "${CONTAINER_NAME}"
    else
        log_error "容器未运行"
        docker logs --tail 30 "${CONTAINER_NAME}"
        return 1
    fi
}

# ==================== 部署 (仅启动，不构建) ====================
do_deploy() {
    check_docker
    check_config
    ensure_network
    mkdir -p "${BACKUP_DIR}" "${LOG_DIR}"

    # 从仓库拉取最新镜像
    log_step "拉取镜像: ${REGISTRY}:${REGISTRY_TAG}"
    docker pull "${REGISTRY}:${REGISTRY_TAG}"

    local image_tag="${REGISTRY}:${REGISTRY_TAG}"

    local version
    version=$(get_version)
    log_info "=========================================="
    log_info "  部署 ${APP_NAME}"
    log_info "  镜像: ${image_tag}"
    log_info "=========================================="

    # 备份当前版本
    backup_current

    # 停止旧容器
    log_step "停止旧容器..."
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true

    # 启动新容器
    log_step "启动新容器..."
    start_container "${image_tag}"

    # 健康检查
    health_check

    log_info "=========================================="
    log_info "  部署完成"
    log_info "  HTTP: http://localhost:${APP_PORT}"
    log_info "  gRPC: localhost:${GRPC_PORT}"
    log_info "=========================================="
}

# ==================== 回滚 ====================
do_rollback() {
    check_docker
    ensure_network
    mkdir -p "${LOG_DIR}"

    local latest_backup
    latest_backup=$(docker images "${IMAGE_NAME}" --format '{{.Tag}}' | grep '^backup-' | sort -r | head -1)

    if [ -z "${latest_backup}" ]; then
        log_error "没有可用的备份镜像"
        exit 1
    fi

    log_info "回滚到: ${IMAGE_NAME}:${latest_backup}"

    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true

    start_container "${IMAGE_NAME}:${latest_backup}"
    health_check
    log_info "回滚完成"
}

# ==================== 重启 ====================
do_restart() {
    log_step "重启 ${CONTAINER_NAME}..."
    docker restart "${CONTAINER_NAME}"
    health_check
    log_info "重启完成"
}

# ==================== 停止 ====================
do_stop() {
    log_step "停止服务..."
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    log_info "服务已停止"
}

# ==================== 状态 ====================
do_status() {
    echo ""
    echo "===== 容器状态 ====="
    docker ps -a --filter "name=${CONTAINER_NAME}" --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"
    echo ""
    echo "===== 可用镜像 ====="
    docker images "${IMAGE_NAME}" --format "table {{.Tag}}\t{{.Size}}\t{{.CreatedSince}}" | head -10
    echo ""
    local health
    health=$(docker inspect --format='{{.State.Health.Status}}' "${CONTAINER_NAME}" 2>/dev/null || echo "未运行")
    echo "健康状态: ${health}"
    echo ""
}

# ==================== 日志 ====================
do_logs() {
    local lines=${1:-100}
    docker logs --tail "${lines}" -f "${CONTAINER_NAME}"
}

# ==================== 清理 ====================
do_cleanup() {
    log_step "清理无用镜像..."
    docker image prune -f
    docker images "${IMAGE_NAME}" -f "dangling=true" -q | xargs -r docker rmi 2>/dev/null || true
    log_info "清理完成"
}

# ==================== 主入口 ====================
usage() {
    echo "用法: $0 <命令> [选项]"
    echo ""
    echo "命令:"
    echo "  build     构建镜像并推送到阿里云仓库 (可选: PULL=true 先拉取代码)"
    echo "  deploy    从仓库拉取镜像并部署"
    echo "  rollback  回滚到上一个版本"
    echo "  restart   重启服务"
    echo "  stop      停止服务"
    echo "  status    查看服务状态"
    echo "  logs      查看日志 (可选参数: 行数，默认100)"
    echo "  cleanup   清理无用镜像"
    echo ""
    echo "示例:"
    echo "  $0 build                    # 构建并推送镜像"
    echo "  PULL=true $0 build          # 拉取代码后构建并推送"
    echo "  $0 deploy                   # 拉取镜像并部署"
    echo ""
    echo "仓库: ${REGISTRY}:${REGISTRY_TAG}"
    echo ""
}

case "${1:-}" in
    build)    do_build ;;
    deploy)   do_deploy ;;
    rollback) do_rollback ;;
    restart)  do_restart ;;
    stop)     do_stop ;;
    status)   do_status ;;
    logs)     do_logs "${2:-100}" ;;
    cleanup)  do_cleanup ;;
    *)        usage; exit 1 ;;
esac
