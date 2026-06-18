# GitHub Release 二进制发布与安装脚本

## 任务背景

需要在推送 `v1.2.3` 这类 tag 时自动构建 `ps-agent` 和 `ps-sub` 的 macOS/Linux、amd64/arm64 二进制，并上传到 GitHub Release。现有安装脚本也需要改为默认从 GitHub Release 下载 binary，支持指定版本，默认使用 latest。

## 实现方案

- 新增 GitHub Actions release workflow：tag push 匹配 `v*.*.*` 后运行，并用 bash 正则严格校验 `^v[0-9]+\.[0-9]+\.[0-9]+$`。
- workflow 构建 `linux/amd64`、`linux/arm64`、`macos/amd64`、`macos/arm64` 四个 tar.gz 包，每个包包含 `ps-agent` 和 `ps-sub`。
- Release 资产主命名为 `proxystack-go_<version>_<os>_<arch>.tar.gz`，同时保留 `proxystack-go_<os>_<arch>.tar.gz` 作为 latest 兼容别名，另上传 `SHA256SUMS`。
- `scripts/install-agent.sh` 和 `scripts/install-sub-local.sh` 默认下载 GitHub Release，`--version` 默认 `latest`，也支持 `--version v1.2.3` 或 `--version 1.2.3`。
- 保留 `--source DIR`，用于本地源码构建，不影响开发态验证。
- 支持通过 `--repo OWNER/REPO` 或 `PROXYSTACK_RELEASE_REPO` 覆盖默认 release 仓库。

## 文件变更

- `.github/workflows/release.yml`：新增 tag release 构建和上传 workflow。
- `scripts/lib/common.sh`：新增 release 仓库/版本校验、平台识别、下载、SHA256 校验和解包安装逻辑。
- `scripts/install-agent.sh`：默认改为 release 下载，新增 `--version`、`--repo`，保留 `--source`。
- `scripts/install-sub-local.sh`：默认改为 release 下载，新增 `--version`、`--repo`，保留 `--source`。
- `internal/deployment/deployment_test.go`：更新部署脚本断言，并新增指定版本 release 下载 dry-run 覆盖。
- `docs/deployment.md`：同步安装方式说明。

## 配置与依赖变更

- 无新增 Go 依赖。
- GitHub Actions 使用 `actions/checkout@v4`、`actions/setup-go@v5` 和 `softprops/action-gh-release@v2`。

## 测试结果

- `bash -n scripts/lib/common.sh scripts/install-agent.sh scripts/install-sub-local.sh`：通过。
- `shellcheck scripts/lib/common.sh scripts/install-agent.sh scripts/install-sub-local.sh`：通过。
- `go test -count=1 ./internal/deployment`：通过。
- 本地交叉编译 `linux/darwin` x `amd64/arm64` 的 `ps-agent` 和 `ps-sub`：通过。
- `git diff --check`：通过。
- `go test ./...`：未全量通过，仍失败于既有 `internal/generator/sub TestRenderSubscriptionsMatchGolden` golden 差异，表现为 proxy-groups 顺序、ADS 分组和 AdsRules 目标不一致。

## 风险与后续建议

- Release workflow 需要仓库开启默认 `GITHUB_TOKEN` 的 `contents: write` 权限，当前 workflow 已声明所需权限。
- 当前未引入 actionlint；如 CI 环境已有 actionlint，可补充 workflow 语义检查。
