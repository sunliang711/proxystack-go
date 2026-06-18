# ps-agent / ps-sub version 构建信息

## 任务背景

为 `ps-agent version` 和 `ps-sub version` 输出增加构建版本信息：版本号来自 git tag，另显示 git commit short hash 和 build datetime。

## 实现方案

- `internal/version` 改为通过构建期变量 `Version`、`Commit` 和 `BuildDateTime` 生成统一多行输出，首行是二进制名，后续缩进展示版本、commit 和 build datetime。
- `Makefile`、GitHub Release workflow、本地 source 安装脚本和 Docker sub 构建路径统一注入 `internal/version.Version`、`internal/version.Commit` 与 `internal/version.BuildDateTime`。
- 未能从 git tag 读取版本时回退为 `0.1.0-dev`；未注入 commit 时优先读取 Go build info，仍不可用时输出 `unknown`；未注入 build datetime 时输出 `unknown`。

## 文件变更

- `internal/version/version.go`：集中处理版本号、commit、build datetime 和输出格式。
- `internal/version/version_test.go`：覆盖构建期注入和 fallback。
- `internal/cli/cli_test.go`：更新 version 子命令 smoke 断言。
- `Makefile`、`.github/workflows/release.yml`、`scripts/*.sh`、`Dockerfile.sub`：补充版本构建参数注入。
- `docs/cli-spec.md`：补充 version 输出格式。

## 配置与依赖变更

- 无新增配置项。
- 无新增 Go 依赖。

## 测试结果

- `go test -count=1 ./internal/version ./internal/cli ./internal/cli/agent ./internal/cli/sub ./internal/deployment`：通过。
- `go test ./...`：通过。
- `shellcheck scripts/lib/common.sh scripts/install-agent.sh scripts/install-sub-local.sh scripts/deploy-sub-docker.sh`：通过。
- `make BIN_DIR=<tmp> BUILD_VERSION=v9.8.7 BUILD_COMMIT=abc1234 BUILD_DATETIME=2026-06-18T11:29:10Z build` 后执行 `ps-agent version`、`ps-sub version`：均输出注入的版本、commit 和 build datetime。
- `scripts/install-agent.sh --source ... --dry-run`、`scripts/install-sub-local.sh --source ... --dry-run`、`scripts/deploy-sub-docker.sh --dry-run --build`：均显示构建参数已注入。

## 风险与后续建议

- 当前提交没有匹配 git tag 时，本地构建会按约定显示 `0.1.0-dev`。
- Release workflow 在 tag push 场景直接使用 `GITHUB_REF_NAME`，可确保发布包版本与 tag 一致。
