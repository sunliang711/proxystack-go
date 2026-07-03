# disabled stack 生命周期命令修复记录

## 问题背景

`psctl stop <stack>` 显式指定 `enabled: false` 的 stack 时，生命周期命令会提示没有匹配服务，无法停止该 stack 可能遗留的历史 unit。

## 根因分析

生命周期命令通过 runtime target scope 中的服务节点生成底层服务名。服务图会过滤 `stack.enabled=false` 的 stack，因此显式 target 虽然能确认 stack 存在，但服务节点集合为空，CLI 只能输出未匹配服务的提示。

## 修复方案

- 默认空 target 行为不变，仍只作用于 enabled stack。
- 显式 target 的 `stop/status/logs/disable` 在 enabled 服务节点为空时，按 `NAME`、`xrelay/NAME`、`clash/NAME` 推导历史 unit 名。
- `start/restart` 不使用历史 unit 推导，避免启动 disabled stack。
- 未匹配服务提示改为 `No enabled services matched target`，避免误导为 stack 不存在。

## 文件与配置变更

- `internal/cli/agent/lifecycle_commands.go`
- `internal/cli/agent/lifecycle_commands_test.go`
- `internal/cli/agent/uninstall_commands_test.go`

不涉及配置变更。

## 验证结果

- `go test ./internal/cli/agent -run 'TestLifecycle(StopsDisabledExplicitStackTarget|StartSkipsDisabledExplicitStackTarget|TreatsAllAsStackName|RejectsSubWithoutStack)' -count=1`
- `go test ./internal/cli/agent -count=1`
- `go test ./internal/runtime ./internal/graph ./internal/cli/agent -count=1`

## 风险与后续建议

修复仅影响显式 target 的生命周期服务名解析。空 target 的默认批量生命周期行为不变，仍跳过 disabled stack。
