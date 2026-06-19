# ps-agent target cleanup delivery

## 任务背景

用户要求不考虑兼容性，清理 `ps-agent` 生命周期和 service wrapper 中的 `all`、`sub` 伪 target：

- 空 target 表示所有 enabled stack。
- `all`、`sub` 如存在同名 stack，按普通 stack 名处理。
- `ps-agent sub ...` 订阅导出类命令保留。
- 订阅服务生命周期统一由 `ps-sub` 管理。

## 多 Agent 编排

- T1 实现 agent：已启动但未落地代码，主线程完成最终实现。
- T2 review agent：独立审查本次 target 清理相关文件，发现 1 个阻断问题和 1 个警告问题；修复后复审通过。

## 实现方案

- 移除 graph/runtime 中 `all` 作为全量 target 的逻辑，只保留空 target 表示全量。
- 移除 `ps-agent` 生命周期里 `sub` 作为订阅服务特殊 target 的逻辑。
- 为 `start/stop/restart/enable/disable` 输出将要操作的 stack 组件和底层服务名；无匹配服务时输出明确提示。
- `ps-agent setup` 和 `ps-agent uninstall` 不再安装、停止或卸载订阅服务。
- systemd 空 target 只管理 xray/mihomo stack 模板；`ps-sub` 的最小配置仍可安装订阅服务 unit。
- launchd 空 target 只管理 stack plist；`ps-sub` 的最小配置仍可安装订阅服务 plist。
- systemd 非空 target 卸载时避免误删其它 stack 仍依赖的共享模板。

## 文件变更

- `internal/cli/agent/lifecycle_commands.go`
- `internal/cli/agent/lifecycle_commands_test.go`
- `internal/cli/agent/setup_commands.go`
- `internal/cli/agent/setup_commands_test.go`
- `internal/cli/agent/uninstall_commands.go`
- `internal/cli/agent/uninstall_commands_test.go`
- `internal/graph/dependencies.go`
- `internal/graph/graph_test.go`
- `internal/runtime/plan.go`
- `internal/service/manager.go`
- `internal/service/launchd.go`
- `internal/service/launchd_test.go`
- `internal/systemd/runner.go`
- `internal/systemd/runner_test.go`
- `docs/cli-spec.md`
- `docs/task-breakdown.md`
- `docs/tasks/task-13-service-lifecycle.md`
- `docs/testing-acceptance-matrix.md`

## 测试结果

- 通过：`go test ./internal/graph ./internal/runtime ./internal/cli/agent ./internal/service ./internal/systemd`
- 未通过：`go test ./...`

全量测试失败在 `internal/deployment`，失败内容为 `install-sub-local.sh` 相关测试仍期待 `ps-agent`。该失败与本次 target 清理无直接关系，且工作区中存在订阅 input/set-host 相关未提交改动，未纳入本次修改范围。

## 评审处理

- 阻断问题：systemd `service uninstall TARGET` 会误删共享模板。
  - 处理：新增 `selectUnitFilesForUninstall`，非空 target 仅在 target 外没有其它 enabled stack 使用同类组件时删除模板。
  - 验证：新增共享模板保留和无人使用时删除的回归测试。
- 警告问题：`setup --start` 无匹配服务时仍输出 `Started services`。
  - 处理：移除无条件成功提示，由生命周期命令输出计划或无匹配提示。

复审结论：review agent 未发现新的阻断或警告问题。

## 风险与后续建议

- 可选补充：再增加 `clash/NAME` 或 `NAME` target 的 systemd uninstall 专项测试。
- 需要单独处理：当前工作区里的订阅 input/set-host 相关改动和 `internal/deployment` 全量测试失败。
