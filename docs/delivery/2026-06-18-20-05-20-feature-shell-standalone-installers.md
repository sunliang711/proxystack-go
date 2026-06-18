# 独立安装脚本调整

## 任务背景

`scripts/install-agent.sh` 和 `scripts/install-sub-local.sh` 需要作为独立脚本使用，默认从当前 Git `remote.origin.url` 对应的 GitHub Release 下载二进制，并默认安装到 `/usr/local/bin`。

## 变更摘要

- 两个安装脚本移除对 `scripts/lib/common.sh` 的运行时依赖，内联必要的参数校验、release 下载、checksum 校验和安装函数。
- release 仓库默认不再写死为固定仓库；未传 `--repo` 且未设置 `PROXYSTACK_RELEASE_REPO` 时，脚本会从当前 Git `remote.origin.url` 解析 `OWNER/REPO`。
- `ps-agent` 和 `ps-sub` 实际安装到 `--bin-dir`，默认 `/usr/local/bin`。
- 脚本内的 init、import、service install 和 start 调用改为使用 `--bin-dir` 下的 CLI。
- 部署文档同步说明默认仓库解析、独立运行限制和安装目录变化。

## 验证结果

- `bash -n scripts/install-agent.sh scripts/install-sub-local.sh`：通过。
- `shellcheck scripts/install-agent.sh scripts/install-sub-local.sh`：通过。
- `bash -n scripts/lib/common.sh scripts/install-agent.sh scripts/install-sub-local.sh scripts/deploy-sub-docker.sh`：通过。
- `go test -count=1 ./internal/deployment`：通过。
- `git diff --check`：通过。

## 注意事项

- 脱离 Git 工作区运行独立脚本时，脚本无法推导默认 release 仓库，需要传入 `--repo OWNER/REPO` 或设置 `PROXYSTACK_RELEASE_REPO`。
