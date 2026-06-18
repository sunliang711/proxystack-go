# systemd 服务账户与权限修复说明

## 问题背景

在远端执行：

```bash
ssh root@10.2.113.219
ps-agent logs usa1
```

`proxystack-clash@usa1.service` 和 `proxystack-xray@usa1.service` 反复失败，日志中出现：

```text
Failed to determine user credentials: No such process
Failed at step USER spawning /opt/proxystack/bin/mihomo: No such process
status=217/USER
```

## 根因分析

systemd unit 使用：

```ini
User=proxystack
Group=proxystack
```

但远端系统不存在 `proxystack` 用户和组。同时 `/opt/proxystack` 目录、bin、geo、runtime generated 文件均为 `root:root`，即使手动创建用户后，也可能继续因为服务进程无法读取配置或执行二进制而失败。

## 修复方案

- `ps-agent init` / `ps-agent setup`
  - 在 Linux root 环境下幂等创建 `proxystack` 组和用户。
  - 初始化后修复标准目录和文件的 mode / owner。
- `ps-agent install` / `ps-agent update`
  - 安装完成后修复标准目录和托管文件的 mode / owner。
- `ps-agent start` / `ps-agent restart`
  - 写入 runtime generated 配置后修复 metadata，再启动 systemd 服务。
- `ps-agent doctor`
  - 新增服务用户和组存在性检查。
  - 新增标准路径 owner 检查。
  - OK 摘要输出服务用户、组、uid/gid 和文件系统 metadata 期望 owner。
- systemd metadata 规则
  - 纳入 `runtime/generated`、`runtime/generated/xray`、`runtime/generated/mihomo` 目录。
  - 纳入 `runtime/generated/xray/*.json`、`runtime/generated/mihomo/*.yaml` 和 `runtime/manifest.json`。

## 文件变更

- `internal/cli/agent/service_account.go`
  - 新增 Linux 服务账户创建和 metadata 修复 helper。
- `internal/cli/agent/config_commands.go`
  - `init` 接入服务账户创建和 metadata 修复。
- `internal/cli/agent/setup_commands.go`
  - `setup` 接入服务账户创建和 metadata 修复。
- `internal/cli/agent/install_commands.go`
  - `install/update` 安装后修复 metadata。
- `internal/cli/agent/lifecycle_commands.go`
  - `start/restart` 写 runtime 后修复 metadata。
- `internal/cli/agent/diagnostics_commands.go`
  - `doctor` 增加服务账户与 owner 检查。
- `internal/systemd/runner.go`
  - 扩展标准 metadata 规则覆盖 runtime generated 文件。

## 验证结果

已通过：

```bash
go test -count=1 ./internal/cli/agent ./internal/systemd ./internal/install
go test ./...
```

远端最小修复验证：

```bash
chown proxystack:proxystack /opt/proxystack/runtime/generated/xray /opt/proxystack/runtime/generated/mihomo
chmod 750 /opt/proxystack/runtime/generated/xray /opt/proxystack/runtime/generated/mihomo
systemctl reset-failed proxystack-xray@usa1.service proxystack-clash@usa1.service
ps-agent restart usa1
ps-agent status usa1
```

验证结果：`proxystack-clash@usa1.service` 和 `proxystack-xray@usa1.service` 均已恢复为 `active (running)`。

## 风险与后续建议

- 自动创建服务账户仅在 Linux 且当前进程为 root 时执行，非 root 本地开发目录保持原有行为。
- macOS launchd 的账户创建未纳入本次修复；如果要完整支持 launchd 安装，需要单独设计 `dscl`/系统用户策略。
- 已有远端机器可通过重新执行 `ps-agent init` 触发用户创建；如果配置已存在，`init` 仍会保持原有报错语义。随后执行 `ps-agent install all` 或 `ps-agent restart usa1` 会触发 metadata 修复。
