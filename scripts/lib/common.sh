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
