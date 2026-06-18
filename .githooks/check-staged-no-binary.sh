#!/bin/sh
set -eu

allow_regex_cli="${1:-}"
allow_file_arg="${2:-}"
allow_regex=""
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
allow_file="${allow_file_arg:-${repo_root}/.precommit-allow}"

append_allow_regex() {
    local fragment="${1:-}"

    [ -n "${fragment}" ] || return 0
    if [ -n "${allow_regex}" ]; then
        allow_regex="${allow_regex}|${fragment}"
    else
        allow_regex="${fragment}"
    fi
}

load_allow_regex() {
    local line=""

    append_allow_regex "${allow_regex_cli}"

    [ -f "${allow_file}" ] || return 0

    while IFS= read -r line || [ -n "${line}" ]; do
        case "${line}" in
            ''|'#'*)
                continue
                ;;
        esac
        append_allow_regex "${line}"
    done <"${allow_file}"
}

path_is_allowed() {
    local path="${1:?missing path}"

    [ -n "${allow_regex}" ] || return 1
    printf '%s\n' "${path}" | grep -Eq -- "${allow_regex}"
}

load_allow_regex

binary_paths="$(
    git diff --cached --numstat --diff-filter=AM -- |
        awk -F '\t' '$1 == "-" && $2 == "-" { print $3 }' |
        while IFS= read -r path; do
            [ -n "${path}" ] || continue
            if ! path_is_allowed "${path}"; then
                printf '%s\n' "${path}"
            fi
        done
)"

if [ -n "${binary_paths}" ]; then
    echo "Binary files are staged for commit:" >&2
    printf '%s\n' "${binary_paths}" >&2
    echo "Commit blocked. Move them to Git LFS, an artifact store, or add an explicit allowlist." >&2
    exit 1
fi
