# macOS 与 Linux 服务管理支持交付说明

## 任务背景

用户要求从两个角度补齐项目管理服务的平台能力：

- mihomo 下载不应限制在 `linux/amd64`。
- `ps-agent service` 不应只绑定 systemd，需要支持 macOS 与 Linux systemd 两套服务管理方案。

本次变更聚焦服务管理层：Linux 保留 systemd 行为，macOS 新增 launchd 后端。

## 编排方案

- 实现代理：已启动，但未在可接受时间内产出补丁，主 Agent 接手实现。
- 评审代理：完成独立评审与复审，指出 launchd `enable/disable`、`RunAtLoad`、active 判断、幂等和陈旧 plist 清理问题。
- 主 Agent：完成代码实现、评审问题修复、测试验证和本文档。

## 实现方案

新增 `internal/service` 作为统一服务管理抽象：

- `auto`：按平台选择服务后端。
  - Linux -> systemd。
  - macOS -> launchd。
- `systemd`：薄封装现有 `internal/systemd.Manager`，保持原有 unit、systemctl、journalctl 行为。
- `launchd`：生成 `/Library/LaunchDaemons` 风格 plist，并通过 `launchctl` / `log` 管理服务。

CLI 新增全局参数：

```bash
ps-agent --service-manager auto|systemd|launchd ...
```

默认值为 `auto`。

## 文件与配置变更

- 新增 `internal/service/manager.go`：服务管理接口、平台选择、systemd 适配。
- 新增 `internal/service/launchd.go`：launchd plist 渲染、生命周期命令、label 映射、陈旧 plist 清理。
- 新增 `internal/service/*_test.go`：auto 选择、launchd 渲染、生命周期、enable/disable、active 判断、陈旧文件清理测试。
- 修改 `internal/cli/agent/root.go`：新增 `--service-manager`。
- 修改 `internal/cli/agent/lifecycle_commands.go`：生命周期命令改用服务管理抽象。
- 修改 `internal/cli/agent/config_commands.go`：`list` 和配置编辑后的 active restart 改用服务管理抽象。
- 修改 `internal/cli/agent/setup_commands.go`：组合安装中的 service install 改用服务管理抽象。
- 修改 `tests/e2e_test.go`：E2E 显式使用 `--service-manager systemd`，保持 Linux systemd 断言稳定。

## 评审问题处理

| 问题 | 处理结果 |
|------|----------|
| launchd `enable/disable` 语义错误 | 已改为 `launchctl enable/disable system/<label>` |
| plist 安装后可能隐式自启 | 已移除 `KeepAlive`，保留 `RunAtLoad=false` |
| `IsActive` 误把 loaded 当 running | 已解析 `state = running` / `pid = ...` |
| start/stop/restart 不幂等 | 已补 `print` 探测、未加载 bootstrap、未加载 stop no-op |
| 陈旧 plist 不清理 | `install all` 会清理不再期望的 proxystack plist |
| 陈旧 loaded job 可能继续运行 | 清理陈旧 plist 前会对旧 label 做 `bootout` |
| uninstall 后 loaded job 可能残留 | 删除 plist 前会对目标 label 做 `bootout` |
| launchd 账户兼容性 | plist 不再固定写入 `UserName/GroupName=proxystack`，由 LaunchDaemon 默认系统上下文运行 |

## 测试结果

已通过：

```bash
go test ./internal/service
go test ./internal/cli/agent ./internal/systemd ./internal/service ./tests
go test ./...
```

## 风险与后续建议

- launchd 后端目前通过 fake runner 单测验证，尚未在真实 macOS root 环境执行 `launchctl bootstrap/enable/disable` 集成验证。
- macOS 需要存在 `proxystack` 用户和组；后续应补 macOS bootstrap/install 脚本或文档。
- launchd 未配置 `KeepAlive`，因此不具备 systemd `Restart=on-failure` 的等价异常自动拉起能力；这是为了保证安装 plist 后不会隐式自启。
- launchd 日志当前按进程名过滤，多个 stack 同时运行时仍可能混入同名进程日志；后续可改为每个 plist 配置独立 stdout/stderr 文件。
- `doctor` 仍检查 systemd unit 文件，未扩展为跨 service manager 诊断；本次未纳入生命周期执行路径改造。
