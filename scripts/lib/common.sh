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

# install_file 安装文件并设置权限和 owner。
install_file() {
	local source_path="${1:-}"
	local target_path="${2:-}"
	local mode_value="${3:-0750}"
	local owner_group="${4:-}"
	if [[ ! -f "${source_path}" && "${DRY_RUN}" != "1" ]]; then
		die "Source file does not exist: ${source_path}"
	fi
	run install -m "${mode_value}" "${source_path}" "${target_path}"
	if [[ -n "${owner_group}" ]]; then
		run chown "${owner_group}" "${target_path}"
	fi
}

# validate_release_repo 校验 GitHub Release 仓库名。
validate_release_repo() {
	local repo_name="${1:-}"
	if [[ ! "${repo_name}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
		die "Release repository must be OWNER/REPO: ${repo_name}"
	fi
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
	local base_dir="${3:-}"
	local owner_group="${4:-}"
	local os_name arch_name asset_name temp_dir archive_path checksums_path archive_url checksums_url agent_binary sub_binary

	validate_release_repo "${repo_name}"
	version_value="$(normalize_release_version "${version_value}")"
	os_name="$(detect_release_os)"
	arch_name="$(detect_release_arch)"
	asset_name="$(release_asset_name "${version_value}" "${os_name}" "${arch_name}")"
	log "Download release: ${repo_name} ${version_value} ${os_name}/${arch_name}"
	log "Download asset: ${asset_name}"
	if is_dry_run; then
		temp_dir="${base_dir}/runtime/proxystack-release-dry-run"
		run install -d -m 0750 "${temp_dir}"
	else
		require_cmd tar
		temp_dir="$(mktemp -d)"
	fi
	archive_path="${temp_dir}/${asset_name}"
	checksums_path="${temp_dir}/SHA256SUMS"
	archive_url="$(release_download_url "${repo_name}" "${version_value}" "${asset_name}")"
	checksums_url="$(release_download_url "${repo_name}" "${version_value}" "SHA256SUMS")"
	download_file "${archive_url}" "${archive_path}"
	download_file "${checksums_url}" "${checksums_path}"
	verify_release_checksum "${temp_dir}" "${checksums_path}" "${asset_name}"
	run tar -xzf "${archive_path}" -C "${temp_dir}"
	agent_binary="$(release_binary_path "${temp_dir}" "ps-agent")"
	sub_binary="$(release_binary_path "${temp_dir}" "ps-sub")"
	install_file "${agent_binary}" "${base_dir}/bin/ps-agent" "0750" "${owner_group}"
	install_file "${sub_binary}" "${base_dir}/bin/ps-sub" "0750" "${owner_group}"
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
