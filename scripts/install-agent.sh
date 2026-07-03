#!/usr/bin/env bash
set -euo pipefail

DRY_RUN="${DRY_RUN:-0}"

# log 输出统一英文步骤日志。
log() {
	printf '[proxystack] %s\n' "$*" >&2
}

# die 输出错误并终止脚本。
die() {
	printf '[proxystack] ERROR: %s\n' "$*" >&2
	exit 1
}

# is_dry_run 判断当前脚本是否只打印命令。
is_dry_run() {
	[[ "${DRY_RUN}" == "1" ]]
}

# quote_args 以可复制形式打印命令参数。
quote_args() {
	printf '%q ' "$@"
	printf '\n'
}

# run 执行命令；dry-run 模式只打印。
run() {
	if is_dry_run; then
		quote_args "$@"
		return 0
	fi
	"$@"
}

# run_stream 执行需要继承终端输出的命令。
run_stream() {
	run "$@"
}

# require_cmd 确认外部命令可用。
require_cmd() {
	local command_name="${1:-}"
	if [[ -z "${command_name}" ]]; then
		die "Command name is required"
	fi
	if ! command -v "${command_name}" >/dev/null 2>&1; then
		die "Required command not found: ${command_name}"
	fi
}

# resolve_build_version 从源码目录当前提交匹配的 git tag 推导 Go CLI 版本。
resolve_build_version() {
	local source_dir="${1:-}"
	local version_value="${PROXYSTACK_BUILD_VERSION:-}"

	if [[ -z "${version_value}" && -n "${source_dir}" ]]; then
		version_value="$(git -C "${source_dir}" describe --tags --exact-match 2>/dev/null || true)"
	fi
	if [[ -z "${version_value}" ]]; then
		version_value="0.1.0-dev"
	fi
	printf '%s' "${version_value}"
}

# resolve_build_commit 从源码目录读取当前 git commit short hash。
resolve_build_commit() {
	local source_dir="${1:-}"
	local commit_value="${PROXYSTACK_BUILD_COMMIT:-}"

	if [[ -z "${commit_value}" && -n "${source_dir}" ]]; then
		commit_value="$(git -C "${source_dir}" rev-parse --short HEAD 2>/dev/null || true)"
	fi
	if [[ -z "${commit_value}" ]]; then
		commit_value="unknown"
	fi
	printf '%s' "${commit_value}"
}

# resolve_build_datetime 生成构建 UTC 时间，默认使用 RFC3339 格式。
resolve_build_datetime() {
	local datetime_value="${PROXYSTACK_BUILD_DATETIME:-}"

	if [[ -z "${datetime_value}" ]]; then
		datetime_value="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	fi
	printf '%s' "${datetime_value}"
}

# go_build_ldflags 生成注入 version 包构建信息的 Go ldflags。
go_build_ldflags() {
	local source_dir="${1:-}"
	local build_version
	local build_commit
	local build_datetime

	build_version="$(resolve_build_version "${source_dir}")"
	build_commit="$(resolve_build_commit "${source_dir}")"
	build_datetime="$(resolve_build_datetime)"
	printf '%s' "-s -w -X github.com/eagle/proxystack-go/internal/version.Version=${build_version} -X github.com/eagle/proxystack-go/internal/version.Commit=${build_commit} -X github.com/eagle/proxystack-go/internal/version.BuildDateTime=${build_datetime}"
}

# require_root 确认脚本以 root 运行。
require_root() {
	if [[ "${EUID}" -ne 0 && "${DRY_RUN}" != "1" ]]; then
		die "This script must be run as root"
	fi
}

# read_arg 读取带值参数，并拒绝缺失值。
read_arg() {
	local option_name="${1:-}"
	local option_value="${2:-}"
	if [[ -z "${option_value}" || "${option_value}" == --* ]]; then
		die "${option_name} requires a value"
	fi
	printf '%s' "${option_value}"
}

# guard_absolute_path 校验路径必须是绝对路径。
guard_absolute_path() {
	local path_value="${1:-}"
	local label="${2:-path}"
	if [[ -z "${path_value}" || "${path_value}" != /* ]]; then
		die "${label} must be an absolute path"
	fi
}

# reject_unsafe_path_segments 拒绝路径穿越和容易误读的重复分隔符。
reject_unsafe_path_segments() {
	local path_value="${1:-}"
	local label="${2:-path}"
	if [[ "${path_value}" == *"/.."* || "${path_value}" == *".."*"/"* ]]; then
		die "${label} must not contain '..': ${path_value}"
	fi
	if [[ "${path_value}" == *"//"* ]]; then
		die "${label} must not contain '//': ${path_value}"
	fi
}

# guard_managed_path 拒绝空路径、根目录和明显危险的托管目录。
guard_managed_path() {
	local path_value="${1:-}"
	local label="${2:-managed path}"
	guard_absolute_path "${path_value}" "${label}"
	reject_unsafe_path_segments "${path_value}" "${label}"
	case "${path_value}" in
		/|/root|/home|/Users|/opt|/usr|/usr/local|/etc|/var|/tmp)
			die "${label} is too broad: ${path_value}"
			;;
	esac
}

# guard_system_dir 校验系统链接目录。
guard_system_dir() {
	local path_value="${1:-}"
	local label="${2:-system directory}"
	guard_absolute_path "${path_value}" "${label}"
	reject_unsafe_path_segments "${path_value}" "${label}"
	case "${path_value}" in
		/|/root|/home|/Users|/opt|/usr|/usr/local|/etc|/var|/tmp)
			die "${label} is too broad: ${path_value}"
			;;
	esac
}

# validate_identity 校验系统用户和组参数。
validate_identity() {
	local user_name="${1:-}"
	local group_name="${2:-}"
	if [[ -z "${user_name}" || "${user_name}" == -* ]]; then
		die "User must not be empty or start with '-'"
	fi
	if [[ -z "${group_name}" || "${group_name}" == -* ]]; then
		die "Group must not be empty or start with '-'"
	fi
}

# ensure_group 创建系统组，已存在时保持不动。
ensure_group() {
	local group_name="${1:-}"
	if is_dry_run; then
		run groupadd --system "${group_name}"
		return 0
	fi
	require_cmd getent
	if getent group "${group_name}" >/dev/null 2>&1; then
		log "SKIP group exists: ${group_name}"
		return 0
	fi
	require_cmd groupadd
	run groupadd --system "${group_name}"
}

# ensure_user 创建系统用户，已存在时保持不动。
ensure_user() {
	local user_name="${1:-}"
	local group_name="${2:-}"
	local home_dir="${3:-}"
	if is_dry_run; then
		run useradd --system --home "${home_dir}" --shell /usr/sbin/nologin --gid "${group_name}" "${user_name}"
		return 0
	fi
	require_cmd id
	if id -u "${user_name}" >/dev/null 2>&1; then
		log "SKIP user exists: ${user_name}"
		return 0
	fi
	require_cmd useradd
	run useradd --system --home "${home_dir}" --shell /usr/sbin/nologin --gid "${group_name}" "${user_name}"
}

# ensure_dir 创建目录并设置权限和可选 owner。
ensure_dir() {
	local path_value="${1:-}"
	local mode_value="${2:-0750}"
	local owner_group="${3:-}"
	guard_managed_path "${path_value}" "directory"
	run install -d -m "${mode_value}" "${path_value}"
	if [[ -n "${owner_group}" ]]; then
		run chown "${owner_group}" "${path_value}"
	fi
}

# install_file 安装文件并设置权限和可选 owner。
install_file() {
	local source_path="${1:-}"
	local target_path="${2:-}"
	local mode_value="${3:-0750}"
	local owner_group="${4:-}"
	if [[ ! -f "${source_path}" && "${DRY_RUN}" != "1" ]]; then
		die "Source file does not exist: ${source_path}"
	fi
	if [[ -L "${target_path}" ]]; then
		run rm -f "${target_path}"
	fi
	run install -m "${mode_value}" "${source_path}" "${target_path}"
	if [[ -n "${owner_group}" ]]; then
		run chown "${owner_group}" "${target_path}"
	fi
}

# install_cli_alias 在 CLI 安装目录创建兼容命令软链接。
install_cli_alias() {
	local target_name="${1:-}"
	local link_path="${2:-}"
	if [[ -z "${target_name}" || -z "${link_path}" ]]; then
		die "CLI alias target and link path are required"
	fi
	run ln -sfn "${target_name}" "${link_path}"
}

# validate_release_repo 校验 GitHub Release 仓库名。
validate_release_repo() {
	local repo_name="${1:-}"
	if [[ ! "${repo_name}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
		die "Release repository must be OWNER/REPO: ${repo_name}"
	fi
}

# resolve_default_release_repo 读取环境变量或返回当前项目默认 Release 仓库。
resolve_default_release_repo() {
	local repo_name="${PROXYSTACK_RELEASE_REPO:-${DEFAULT_RELEASE_REPO}}"

	validate_release_repo "${repo_name}"
	printf '%s' "${repo_name}"
}

# normalize_release_version 规范 release 版本号，latest 原样保留。
normalize_release_version() {
	local version_value="${1:-}"
	if [[ -z "${version_value}" ]]; then
		die "Release version is required"
	fi
	if [[ "${version_value}" == "latest" ]]; then
		printf '%s' "${version_value}"
		return 0
	fi
	if [[ "${version_value}" != v* ]]; then
		version_value="v${version_value}"
	fi
	if [[ ! "${version_value}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		die "Release version must be latest or vMAJOR.MINOR.PATCH: ${version_value}"
	fi
	printf '%s' "${version_value}"
}

# detect_release_os 识别 release 资产中的操作系统名。
detect_release_os() {
	local os_name
	os_name="$(uname -s)"
	case "${os_name}" in
		Linux)
			printf 'linux'
			;;
		Darwin)
			printf 'macos'
			;;
		*)
			die "Unsupported OS for release binary: ${os_name}"
			;;
	esac
}

# detect_release_arch 识别 release 资产中的 CPU 架构名。
detect_release_arch() {
	local arch_name
	arch_name="$(uname -m)"
	case "${arch_name}" in
		x86_64|amd64)
			printf 'amd64'
			;;
		arm64|aarch64)
			printf 'arm64'
			;;
		*)
			die "Unsupported architecture for release binary: ${arch_name}"
			;;
	esac
}

# release_download_url 生成 GitHub Release 下载地址。
release_download_url() {
	local repo_name="${1:-}"
	local version_value="${2:-}"
	local asset_name="${3:-}"
	if [[ "${version_value}" == "latest" ]]; then
		printf 'https://github.com/%s/releases/latest/download/%s' "${repo_name}" "${asset_name}"
		return 0
	fi
	printf 'https://github.com/%s/releases/download/%s/%s' "${repo_name}" "${version_value}" "${asset_name}"
}

# release_asset_name 根据版本和平台生成 proxystack release 归档名。
release_asset_name() {
	local version_value="${1:-}"
	local os_name="${2:-}"
	local arch_name="${3:-}"

	if [[ "${version_value}" == "latest" ]]; then
		printf 'proxystack-go_%s_%s.tar.gz' "${os_name}" "${arch_name}"
		return 0
	fi
	printf 'proxystack-go_%s_%s_%s.tar.gz' "${version_value}" "${os_name}" "${arch_name}"
}

# release_binary_path 返回 release 解包后的标准二进制路径。
release_binary_path() {
	local work_dir="${1:-}"
	local short_name="${2:-}"
	local short_path="${work_dir}/${short_name}"

	if is_dry_run || [[ -f "${short_path}" ]]; then
		printf '%s' "${short_path}"
		return 0
	fi
	die "Release archive is missing binary: ${short_name}"
}

# download_file 下载文件；dry-run 模式只打印 curl 命令。
download_file() {
	local source_url="${1:-}"
	local target_path="${2:-}"
	if is_dry_run; then
		run_stream curl -fL --retry 3 -o "${target_path}" "${source_url}"
		return 0
	fi
	if command -v curl >/dev/null 2>&1; then
		run_stream curl -fL --retry 3 -o "${target_path}" "${source_url}"
		return 0
	fi
	if command -v wget >/dev/null 2>&1; then
		run_stream wget -O "${target_path}" "${source_url}"
		return 0
	fi
	die "Required command not found: curl or wget"
}

# latest_release_api_url 生成 GitHub latest release metadata 地址。
latest_release_api_url() {
	local repo_name="${1:-}"
	printf 'https://api.github.com/repos/%s/releases/latest' "${repo_name}"
}

# download_metadata_file 下载小型 metadata 文件，避免进度条淹没版本日志。
download_metadata_file() {
	local source_url="${1:-}"
	local target_path="${2:-}"
	if command -v curl >/dev/null 2>&1; then
		run curl -fsSL --retry 3 -o "${target_path}" "${source_url}"
		return 0
	fi
	if command -v wget >/dev/null 2>&1; then
		run wget -q -O "${target_path}" "${source_url}"
		return 0
	fi
	die "Required command not found: curl or wget"
}

# resolve_latest_release_version 解析 latest 对应的真实 GitHub release tag。
resolve_latest_release_version() {
	local repo_name="${1:-}"
	local work_dir="${2:-}"
	local metadata_path tag_name

	if [[ -z "${work_dir}" ]]; then
		die "Release metadata directory is required"
	fi
	metadata_path="${work_dir}/latest-release.json"
	download_metadata_file "$(latest_release_api_url "${repo_name}")" "${metadata_path}"
	tag_name="$(awk -F'"' '/"tag_name"[[:space:]]*:/ { print $4; found=1; exit } END { exit found ? 0 : 1 }' "${metadata_path}" || true)"
	if [[ -z "${tag_name}" ]]; then
		die "GitHub latest release tag_name not found"
	fi
	normalize_release_version "${tag_name}"
}

# verify_release_checksum 校验下载归档的 SHA256。
verify_release_checksum() {
	local work_dir="${1:-}"
	local checksums_path="${2:-}"
	local asset_name="${3:-}"
	local check_file="${work_dir}/${asset_name}.sha256"
	if is_dry_run; then
		log "DRY-RUN verify checksum: ${asset_name}"
		return 0
	fi
	if ! awk -v asset="${asset_name}" '($2 == asset || $2 == "*" asset) { print; found=1 } END { exit found ? 0 : 1 }' "${checksums_path}" > "${check_file}"; then
		die "Checksum entry not found: ${asset_name}"
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "${work_dir}" && sha256sum -c "$(basename "${check_file}")")
		return 0
	fi
	if command -v shasum >/dev/null 2>&1; then
		(cd "${work_dir}" && shasum -a 256 -c "$(basename "${check_file}")")
		return 0
	fi
	die "Required command not found: sha256sum or shasum"
}

# install_release_binaries 从 GitHub Release 下载并安装当前平台二进制。
install_release_binaries() {
	local repo_name="${1:-}"
	local version_value="${2:-}"
	local bin_dir="${3:-}"
	local os_name arch_name asset_name display_version temp_dir archive_path checksums_path archive_url checksums_url agent_binary sub_binary

	validate_release_repo "${repo_name}"
	version_value="$(normalize_release_version "${version_value}")"
	os_name="$(detect_release_os)"
	arch_name="$(detect_release_arch)"
	if is_dry_run; then
		temp_dir="${BASE_DIR}/runtime/proxystack-release-dry-run"
		run install -d -m 0750 "${temp_dir}"
	else
		require_cmd tar
		temp_dir="$(mktemp -d)"
	fi
	display_version="${version_value}"
	if [[ "${version_value}" == "latest" && "${DRY_RUN}" != "1" ]]; then
		display_version="$(resolve_latest_release_version "${repo_name}" "${temp_dir}")"
	fi
	asset_name="$(release_asset_name "${version_value}" "${os_name}" "${arch_name}")"
	log "Download release: ${repo_name} ${display_version} ${os_name}/${arch_name}"
	log "Download asset: ${asset_name}"
	archive_path="${temp_dir}/${asset_name}"
	checksums_path="${temp_dir}/SHA256SUMS"
	archive_url="$(release_download_url "${repo_name}" "${version_value}" "${asset_name}")"
	checksums_url="$(release_download_url "${repo_name}" "${version_value}" "SHA256SUMS")"
	download_file "${archive_url}" "${archive_path}"
	download_file "${checksums_url}" "${checksums_path}"
	verify_release_checksum "${temp_dir}" "${checksums_path}" "${asset_name}"
	run tar -xzf "${archive_path}" -C "${temp_dir}"
	agent_binary="$(release_binary_path "${temp_dir}" "psctl")"
	sub_binary="$(release_binary_path "${temp_dir}" "pssub")"
	install_file "${agent_binary}" "${bin_dir}/psctl" "0755"
	install_file "${sub_binary}" "${bin_dir}/pssub" "0755"
	if ! is_dry_run; then
		run rm -rf "${temp_dir}"
	fi
}

# run_as_user 使用指定系统用户执行命令。
run_as_user() {
	local user_name="${1:-}"
	shift
	if [[ -z "${user_name}" ]]; then
		die "Run user is required"
	fi
	if [[ "${EUID}" -eq 0 || "${DRY_RUN}" == "1" ]]; then
		if command -v runuser >/dev/null 2>&1; then
			run runuser -u "${user_name}" -- "$@"
		else
			require_cmd sudo
			run sudo -u "${user_name}" -- "$@"
		fi
		return 0
	fi
	run "$@"
}

SOURCE_DIR=""
INSTALL_SOURCE="release"
DEFAULT_RELEASE_REPO="sunliang711/proxystack-go"
RELEASE_REPO=""
RELEASE_VERSION="latest"
BASE_DIR="/opt/proxystack"
BIN_DIR="/usr/local/bin"
INSTALL_USER="proxystack"
INSTALL_GROUP="proxystack"
RUN_SETUP_LOCAL="1"
INSTALL_SYSTEMD="0"

# usage 展示 install-agent 用法。
usage() {
	cat <<'EOF'
Usage: scripts/install-agent.sh [options]

Download and install proxystack release binaries by default. The script
bootstraps users, directories, CLI links, and runs psctl setup local by default.
It does not install mihomo, xray-core, or geo data.

Options:
  --version VERSION        Release version to install. Default: latest
  --repo OWNER/REPO        GitHub release repository. Default: sunliang711/proxystack-go
  --source DIR             Build from a local source directory instead of downloading release
  --base-dir DIR           Managed base directory. Default: /opt/proxystack
  --bin-dir DIR            CLI install directory. Default: /usr/local/bin
  --user USER              System user. Default: proxystack
  --group GROUP            System group. Default: proxystack
  --no-setup-local         Do not run psctl setup local.
  --install-systemd        Compatibility flag; setup local installs service files.
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
			--no-setup-local|--no-init)
				RUN_SETUP_LOCAL="0"
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
	if [[ -z "${RELEASE_REPO}" ]]; then
		RELEASE_REPO="$(resolve_default_release_repo)"
	fi
	validate_release_repo "${RELEASE_REPO}"
	RELEASE_VERSION="$(normalize_release_version "${RELEASE_VERSION}")"
}

# ensure_systemd_defaults 避免自定义安装参数和固定 systemd unit 不一致。
ensure_systemd_defaults() {
	if [[ "${RUN_SETUP_LOCAL}" != "1" && "${INSTALL_SYSTEMD}" != "1" ]]; then
		return 0
	fi
	if [[ "${INSTALL_USER}" != "proxystack" || "${INSTALL_GROUP}" != "proxystack" ]]; then
		die "setup local requires --user proxystack --group proxystack"
	fi
}

# ensure_agent_dirs 创建 agent 需要的托管目录。
ensure_agent_dirs() {
	local owner_group="${INSTALL_USER}:${INSTALL_GROUP}"

	ensure_dir "${BASE_DIR}" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/geo" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/downloads" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/runtime" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/publish" "0750" "${owner_group}"
	ensure_dir "${BASE_DIR}/stacks" "0750" "${owner_group}"
}

# ensure_cli_dir 创建 CLI 安装目录。
ensure_cli_dir() {
	run install -d -m 0755 "${BIN_DIR}"
}

# build_go_binaries 构建 psctl 和 pssub，并安装到系统 bin 目录。
build_go_binaries() {
	local build_ldflags
	local temp_dir

	require_cmd go
	build_ldflags="$(go_build_ldflags "${SOURCE_DIR}")"
	if is_dry_run; then
		temp_dir="${BASE_DIR}/runtime/proxystack-build-dry-run"
	else
		temp_dir="$(mktemp -d)"
	fi
	run_stream go build -trimpath -ldflags "${build_ldflags}" -o "${temp_dir}/psctl" "${SOURCE_DIR}/cmd/ps-agent"
	run_stream go build -trimpath -ldflags "${build_ldflags}" -o "${temp_dir}/pssub" "${SOURCE_DIR}/cmd/ps-sub"
	install_file "${temp_dir}/psctl" "${BIN_DIR}/psctl" "0755"
	install_file "${temp_dir}/pssub" "${BIN_DIR}/pssub" "0755"
	if ! is_dry_run; then
		run rm -rf "${temp_dir}"
	fi
}

# install_binaries 按参数选择 release 下载或本地源码构建。
install_binaries() {
	if [[ "${INSTALL_SOURCE}" == "source" ]]; then
		build_go_binaries
	else
		install_release_binaries "${RELEASE_REPO}" "${RELEASE_VERSION}" "${BIN_DIR}"
	fi
	install_cli_alias "psctl" "${BIN_DIR}/ps-agent"
	install_cli_alias "psctl" "${BIN_DIR}/psagent"
	install_cli_alias "pssub" "${BIN_DIR}/ps-sub"
}

# maybe_setup_local 根据参数决定是否执行本地初始化和 service 文件安装。
maybe_setup_local() {
	if [[ "${RUN_SETUP_LOCAL}" != "1" ]]; then
		log "SKIP setup local disabled"
		return 0
	fi
	run "${BIN_DIR}/psctl" --base-dir "${BASE_DIR}" setup local
}

# maybe_install_systemd 兼容旧参数；setup local 已默认安装 service 文件。
maybe_install_systemd() {
	if [[ "${INSTALL_SYSTEMD}" != "1" ]]; then
		return 0
	fi
	if [[ "${RUN_SETUP_LOCAL}" == "1" ]]; then
		log "SKIP systemd install covered by setup local"
		return 0
	fi
	run "${BIN_DIR}/psctl" --base-dir "${BASE_DIR}" service install
}

# print_next_steps 输出安装后的建议命令。
print_next_steps() {
	cat <<EOF

Next steps:
  sudo ${BIN_DIR}/psctl --base-dir ${BASE_DIR} setup deps
  sudo ${BIN_DIR}/psctl --base-dir ${BASE_DIR} add usa1 --no-edit
  sudo ${BIN_DIR}/psctl --base-dir ${BASE_DIR} check
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
	log "Prepare CLI directory"
	ensure_cli_dir
	log "Install Go binaries"
	install_binaries
	log "Run local setup"
	maybe_setup_local
	log "Install systemd units"
	maybe_install_systemd
	print_next_steps
}

main "$@"
