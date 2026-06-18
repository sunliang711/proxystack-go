#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

SOURCE_DIR=""
INSTALL_SOURCE="release"
RELEASE_REPO="${PROXYSTACK_RELEASE_REPO:-eagle/proxystack-go}"
RELEASE_VERSION="latest"
BASE_DIR="/opt/proxystack"
BIN_DIR="/usr/local/bin"
INSTALL_USER="proxystack"
INSTALL_GROUP="proxystack"
RUN_INIT="1"
INSTALL_SYSTEMD="0"

# usage 展示 install-agent 用法。
usage() {
	cat <<'EOF'
Usage: scripts/install-agent.sh [options]

Download and install proxystack release binaries by default. The script
bootstraps users, directories, CLI links, and optionally initializes config or
installs systemd unit files. It does not install mihomo, xray-core, or geo data.

Options:
  --version VERSION        Release version to install. Default: latest
  --repo OWNER/REPO        GitHub release repository. Default: eagle/proxystack-go
  --source DIR             Build from a local source directory instead of downloading release
  --base-dir DIR           Managed base directory. Default: /opt/proxystack
  --bin-dir DIR            CLI symlink directory. Default: /usr/local/bin
  --user USER              System user. Default: proxystack
  --group GROUP            System group. Default: proxystack
  --no-init                Do not run ps-agent init.
  --install-systemd        Run ps-agent service install.
  --dry-run                Print commands without executing writes.
  -h, --help               Show this help.
EOF
}

# parse_args 解析命令行参数。
parse_args() {
	while [[ "$#" -gt 0 ]]; do
		case "$1" in
			--version)
				RELEASE_VERSION="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--version=*)
				RELEASE_VERSION="${1#*=}"
				shift
				;;
			--repo)
				RELEASE_REPO="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--repo=*)
				RELEASE_REPO="${1#*=}"
				shift
				;;
			--source)
				SOURCE_DIR="$(read_arg "$1" "${2:-}")"
				INSTALL_SOURCE="source"
				shift 2
				;;
			--source=*)
				SOURCE_DIR="${1#*=}"
				INSTALL_SOURCE="source"
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
			--bin-dir)
				BIN_DIR="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--bin-dir=*)
				BIN_DIR="${1#*=}"
				shift
				;;
			--user)
				INSTALL_USER="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--user=*)
				INSTALL_USER="${1#*=}"
				shift
				;;
			--group)
				INSTALL_GROUP="$(read_arg "$1" "${2:-}")"
				shift 2
				;;
			--group=*)
				INSTALL_GROUP="${1#*=}"
				shift
				;;
			--no-init)
				RUN_INIT="0"
				shift
				;;
			--install-systemd)
				INSTALL_SYSTEMD="1"
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

# validate_args 校验安装来源和关键路径。
validate_args() {
	guard_managed_path "${BASE_DIR}" "base directory"
	guard_system_dir "${BIN_DIR}" "bin directory"
	validate_identity "${INSTALL_USER}" "${INSTALL_GROUP}"
	ensure_systemd_defaults
	if [[ "${INSTALL_SOURCE}" == "source" ]]; then
		if [[ -z "${SOURCE_DIR}" ]]; then
			die "Source directory is required"
		fi
		if [[ ! -d "${SOURCE_DIR}" && "${DRY_RUN}" != "1" ]]; then
			die "Source directory does not exist: ${SOURCE_DIR}"
		fi
		return 0
	fi
	validate_release_repo "${RELEASE_REPO}"
	RELEASE_VERSION="$(normalize_release_version "${RELEASE_VERSION}")"
}

# ensure_systemd_defaults 避免自定义安装参数和固定 systemd unit 不一致。
ensure_systemd_defaults() {
	if [[ "${INSTALL_SYSTEMD}" != "1" ]]; then
		return 0
	fi
	if [[ "${INSTALL_USER}" != "proxystack" || "${INSTALL_GROUP}" != "proxystack" || "${BIN_DIR}" != "/usr/local/bin" ]]; then
		die "--install-systemd requires --user proxystack --group proxystack --bin-dir /usr/local/bin"
	fi
}

# ensure_agent_dirs 创建 agent 需要的托管目录。
ensure_agent_dirs() {
	local owner_group="${INSTALL_USER}:${INSTALL_GROUP}"

	ensure_dir "${BASE_DIR}" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/bin" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/geo" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/downloads" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/runtime" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/publish" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/stacks" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/sub" "0750" "${owner_group}"
}

# build_go_binaries 构建 ps-agent 和 ps-sub，并安装到托管 bin 目录。
build_go_binaries() {
	local owner_group="${INSTALL_USER}:${INSTALL_GROUP}"
	local temp_dir

	require_cmd go
	if is_dry_run; then
		temp_dir="${BASE_DIR}/runtime/proxystack-build-dry-run"
	else
		temp_dir="$(mktemp -d)"
	fi
	run_stream go build -trimpath -o "${temp_dir}/proxystack-agent" "${SOURCE_DIR}/cmd/ps-agent"
	run_stream go build -trimpath -o "${temp_dir}/proxystack-sub" "${SOURCE_DIR}/cmd/ps-sub"
	install_file "${temp_dir}/proxystack-agent" "${BASE_DIR}/bin/proxystack-agent" "0750" "${owner_group}"
	install_file "${temp_dir}/proxystack-sub" "${BASE_DIR}/bin/proxystack-sub" "0750" "${owner_group}"
	if ! is_dry_run; then
		run rm -rf "${temp_dir}"
	fi
}

# install_binaries 按参数选择 release 下载或本地源码构建。
install_binaries() {
	if [[ "${INSTALL_SOURCE}" == "source" ]]; then
		build_go_binaries
		return 0
	fi
	install_release_binaries "${RELEASE_REPO}" "${RELEASE_VERSION}" "${BASE_DIR}" "${INSTALL_USER}:${INSTALL_GROUP}"
}

# link_cli_commands 链接 CLI 入口到系统 bin 目录。
link_cli_commands() {
	run install -d -m 0755 "${BIN_DIR}"
	run ln -sf "${BASE_DIR}/bin/proxystack-agent" "${BIN_DIR}/proxystack-agent"
	run ln -sf "${BASE_DIR}/bin/proxystack-agent" "${BIN_DIR}/ps-agent"
	run ln -sf "${BASE_DIR}/bin/proxystack-sub" "${BIN_DIR}/proxystack-sub"
	run ln -sf "${BASE_DIR}/bin/proxystack-sub" "${BIN_DIR}/ps-sub"
}

# maybe_init_project 根据参数决定是否初始化 config.yaml。
maybe_init_project() {
	if [[ "${RUN_INIT}" != "1" ]]; then
		log "SKIP project init disabled"
		return 0
	fi
	if [[ -f "${BASE_DIR}/config.yaml" && "${DRY_RUN}" != "1" ]]; then
		log "SKIP config exists: ${BASE_DIR}/config.yaml"
		return 0
	fi
	run_as_user "${INSTALL_USER}" "${BASE_DIR}/bin/proxystack-agent" --base-dir "${BASE_DIR}" init
}

# maybe_install_systemd 根据参数决定是否安装 systemd unit。
maybe_install_systemd() {
	if [[ "${INSTALL_SYSTEMD}" != "1" ]]; then
		return 0
	fi
	run "${BASE_DIR}/bin/proxystack-agent" --base-dir "${BASE_DIR}" service install
}

# print_next_steps 输出安装后的建议命令。
print_next_steps() {
	cat <<EOF

Next steps:
  sudo ${BIN_DIR}/ps-agent --base-dir ${BASE_DIR} install all
  sudo ${BIN_DIR}/ps-agent --base-dir ${BASE_DIR} service install
  sudo ${BIN_DIR}/ps-agent --base-dir ${BASE_DIR} add usa1 --no-edit
  sudo ${BIN_DIR}/ps-agent --base-dir ${BASE_DIR} check
EOF
}

# main 执行 agent bootstrap 主流程。
main() {
	parse_args "$@"
	log "Validate arguments"
	validate_args
	log "Check root permission"
	require_root
	log "Prepare identity"
	ensure_group "${INSTALL_GROUP}"
	ensure_user "${INSTALL_USER}" "${INSTALL_GROUP}" "${BASE_DIR}"
	log "Prepare directories"
	ensure_agent_dirs
	log "Install Go binaries"
	install_binaries
	log "Link CLI commands"
	link_cli_commands
	log "Initialize project"
	maybe_init_project
	log "Install systemd units"
	maybe_install_systemd
	print_next_steps
}

main "$@"
