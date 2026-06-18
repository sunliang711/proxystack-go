# ps-agent setup 启动选项

## 任务背景

首次部署需要连续执行 `ps-agent init`、`ps-agent install all` 和 `ps-agent service install`，操作步骤偏多。为降低安装心智负担，补充组合入口的可选启动能力，并让 systemd unit 安装后自动刷新配置。

## 实现方案

- `ps-agent setup` 保持原有组合安装流程：初始化配置、安装托管二进制和 geo 数据、安装服务 unit。
- 已存在 `config.yaml` 时，`setup` 不再因默认 init 的防覆盖保护而中断；它会保留既有配置，只补齐标准目录和缺失的 sub 默认配置。
- 新增 `ps-agent setup --start`：在服务 unit 安装完成后复用现有 `start` 生命周期，生成 runtime 文件、修复权限并启动全部 enabled stack 服务。
- `systemd.Manager.InstallUnits` 写入 unit 文件后执行 `systemctl daemon-reload`，避免新 unit 或更新后的 unit 未被 systemd 感知。

## 文件变更

- `internal/cli/agent/setup_commands.go`：新增 `--start` flag 和启动流程。
- `internal/agentconfig/commands.go`：新增幂等补齐标准目录的 layout 入口，并保留 `init` 不覆盖配置的默认语义。
- `internal/systemd/runner.go`：unit 安装后执行 `daemon-reload`。
- `internal/service/launchd.go`：launchd plist 不再固定写入 `UserName/GroupName=proxystack`，避免 macOS 未预置账户时启动失败。
- `internal/cli/agent/setup_commands_test.go`：覆盖已有 config 继续 setup、`--start` 生命周期调用和失败包装。
- `internal/agentconfig/commands_test.go`：覆盖 layout 补齐不覆盖配置。
- `internal/service/launchd_test.go`：覆盖 launchd plist 不固定服务账户。
- `internal/cli/agent/backup_commands_test.go`：补充 setup help 中 `--start` 的断言。
- `internal/systemd/runner_test.go`：补充 unit 安装后刷新 systemd 的断言。

## 配置与依赖变更

- 无新增配置项。
- 无新增 Go 依赖。

## 测试结果

- `go test -count=1 ./internal/systemd ./internal/cli/agent`
- `go test -count=1 ./internal/agentconfig ./internal/cli/agent ./internal/systemd`
- `go test -count=1 ./internal/service`
- `go test -count=1 ./internal/graph`
- `go test -count=1 ./...`

## 风险与后续建议

- `ps-agent setup --start` 会实际启动 systemd 服务，应在 root 或有 systemd 权限的环境中执行。
- 已保留不带 `--start` 的默认行为，适合只初始化并安装、不立即启动的场景。
