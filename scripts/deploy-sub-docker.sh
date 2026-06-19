#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGE="proxystack-sub:latest"
CONTAINER_NAME="proxystack-sub"
BASE_DIR="/opt/proxystack-sub"
HOST="0.0.0.0"
PORT="3003"
CONTAINER_USER="10001:10001"
DATA_OWNER="10001:10001"
PULL_IMAGE="0"
BUILD_IMAGE="0"
REPLACE_CONTAINER="0"

# usage 展示 Docker 部署脚本用法。
usage() {
	cat <<'EOF'
Usage: scripts/deploy-sub-docker.sh [options]

Deploy proxystack-sub with Docker using a persistent host base directory mapped
to container /data. Existing containers are never replaced unless --replace is
explicitly provided.

Options:
  --image IMAGE            Docker image. Default: proxystack-sub:latest
  --name NAME              Container name. Default: proxystack-sub
  --base-dir DIR           Host base directory mounted to /data. Default: /opt/proxystack-sub
  --host HOST              Host bind address. Default: 0.0.0.0
  --port PORT              Host port mapped to container port 3003. Default: 3003
  --user UID:GID           Container user. Default: 10001:10001
  --data-owner UID:GID     Host data directory owner. Default: 10001:10001
  --build                  Build the image from this repository before running.
  --pull                   Pull image before running.
  --replace                Remove an existing same-name container before running.
  --dry-run                Print commands without executing writes.
  -h, --help               Show this help.
EOF
}

# parse_args 解析命令行参数。
parse_args() {
	while [[ "$#" -gt 0 ]]; do
		case "$1" in
			--image)
				IMAGE="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--image=*)
				IMAGE="${1#*=}"
				shift
				;;
			--name)
				CONTAINER_NAME="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--name=*)
				CONTAINER_NAME="${1#*=}"
				shift
				;;
			--base-dir)
				BASE_DIR="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--base-dir=*)
				BASE_DIR="${1#*=}"
				shift
				;;
			--host)
				HOST="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--host=*)
				HOST="${1#*=}"
				shift
				;;
			--port)
				PORT="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--port=*)
				PORT="${1#*=}"
				shift
				;;
			--user)
				CONTAINER_USER="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--user=*)
				CONTAINER_USER="${1#*=}"
				shift
				;;
			--data-owner)
				DATA_OWNER="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--data-owner=*)
				DATA_OWNER="${1#*=}"
				shift
				;;
			--pull)
				PULL_IMAGE="1"
				shift
				;;
			--build)
				BUILD_IMAGE="1"
				shift
				;;
			--replace)
				REPLACE_CONTAINER="1"
				shift
				;;
			--dry-run)
				DRY_RUN="1"
				shift
				;;
			-h|--help)
				usage
				exit 0
				;;
			*)
				die "Unknown argument: $1"
				;;
		esac
	done
}

# validate_args 校验 Docker 参数和托管数据目录。
validate_args() {
	if [[ -z "${IMAGE}" || "${IMAGE}" == -* ]]; then
		die "Image must not be empty or start with '-'"
	fi
	if [[ -z "${CONTAINER_NAME}" || "${CONTAINER_NAME}" == -* ]]; then
		die "Container name must not be empty or start with '-'"
	fi
	if [[ -z "${HOST}" || "${HOST}" == -* ]]; then
		die "Host must not be empty or start with '-'"
	fi
	if [[ ! "${PORT}" =~ ^[0-9]+$ || "${PORT}" -lt 1 || "${PORT}" -gt 65535 ]]; then
		die "Port must be between 1 and 65535"
	fi
	if [[ -z "${CONTAINER_USER}" || "${CONTAINER_USER}" == -* ]]; then
		die "Container user must not be empty or start with '-'"
	fi
	if [[ -z "${DATA_OWNER}" || "${DATA_OWNER}" == -* ]]; then
		die "Data owner must not be empty or start with '-'"
	fi
	if [[ "${PULL_IMAGE}" == "1" && "${BUILD_IMAGE}" == "1" ]]; then
		die "--pull and --build cannot be used together"
	fi
	guard_managed_path "${BASE_DIR}" "base directory"
}

# ensure_base_dirs 创建 Docker volume 持久化目录。
ensure_base_dirs() {
	ensure_dir "${BASE_DIR}" "0750" "${DATA_OWNER}"
	ensure_dir "${BASE_DIR}/inputs" "0750" "${DATA_OWNER}"
	ensure_dir "${BASE_DIR}/templates" "0750" "${DATA_OWNER}"
}

# ensure_sub_config_exists 确认 ps-sub 配置存在，避免容器以隐式默认配置启动。
ensure_sub_config_exists() {
	if is_dry_run; then
		log "SKIP sub config check in dry-run"
		return 0
	fi
	if [[ ! -f "${BASE_DIR}/config.yaml" ]]; then
		die "Sub config does not exist: ${BASE_DIR}/config.yaml"
	fi
}

# container_exists 判断同名容器是否已存在。
container_exists() {
	docker container inspect "${CONTAINER_NAME}" >/dev/null 2>&1
}

# check_container_conflict 未指定 --replace 时拒绝同名容器。
check_container_conflict() {
	if [[ "${REPLACE_CONTAINER}" == "1" || "${DRY_RUN}" == "1" ]]; then
		return 0
	fi
	if container_exists; then
		die "Container already exists: ${CONTAINER_NAME}. Use --replace to remove it."
	fi
}

# maybe_pull_image 根据参数决定是否拉取镜像。
maybe_pull_image() {
	if [[ "${PULL_IMAGE}" != "1" ]]; then
		return 0
	fi
	run_stream docker pull "${IMAGE}"
}

# maybe_build_image 根据参数决定是否从当前仓库构建镜像。
maybe_build_image() {
	if [[ "${BUILD_IMAGE}" != "1" ]]; then
		return 0
	fi
	run_stream docker build \
		--build-arg "BUILD_VERSION=$(resolve_build_version "${PROJECT_ROOT}")" \
		--build-arg "BUILD_COMMIT=$(resolve_build_commit "${PROJECT_ROOT}")" \
		--build-arg "BUILD_DATETIME=$(resolve_build_datetime)" \
		-f "${PROJECT_ROOT}/Dockerfile.sub" \
		-t "${IMAGE}" \
		"${PROJECT_ROOT}"
}

# ensure_image_available 确认镜像可用后再替换旧容器。
ensure_image_available() {
	if is_dry_run; then
		log "SKIP image availability check in dry-run"
		return 0
	fi
	if docker image inspect "${IMAGE}" >/dev/null 2>&1; then
		return 0
	fi
	die "Docker image not found locally: ${IMAGE}. Use --pull to fetch it."
}

# maybe_replace_container 指定 --replace 时删除旧容器。
maybe_replace_container() {
	if [[ "${REPLACE_CONTAINER}" != "1" ]]; then
		return 0
	fi
	if is_dry_run || container_exists; then
		run docker rm -f "${CONTAINER_NAME}"
	fi
}

# run_container 使用安全默认参数启动订阅服务容器。
run_container() {
	local port_mapping="${HOST}:${PORT}:3003"

	run docker run -d \
		--name "${CONTAINER_NAME}" \
		--restart unless-stopped \
		--publish "${port_mapping}" \
		--volume "${BASE_DIR}:/data" \
		--user "${CONTAINER_USER}" \
		--read-only \
		--cap-drop ALL \
		--security-opt no-new-privileges:true \
		--tmpfs /tmp:rw,noexec,nosuid,size=64m \
		"${IMAGE}" \
		ps-sub --base-dir /data serve
}

# main 执行 Docker sub 部署主流程。
main() {
	parse_args "$@"
	log "Validate arguments"
	validate_args
	log "Check Docker command"
	require_cmd docker
	log "Check container conflict"
	check_container_conflict
	log "Prepare data directories"
	ensure_base_dirs
	log "Check ps-sub config"
	ensure_sub_config_exists
	log "Pull image"
	maybe_pull_image
	log "Build image"
	maybe_build_image
	log "Check image"
	ensure_image_available
	log "Replace container"
	maybe_replace_container
	log "Start container"
	run_container
}

main "$@"
