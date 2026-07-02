# T13 服务生命周期与 target scope

## 目标

实现 `start/stop/restart/status/logs/enable/disable/service`。

## 范围

- Target scope。
- Systemd runner。
- 顶层 lifecycle 命令。
- service 子命令。

## 输入文档

- [cli-spec.md](../cli-spec.md)
- [generator-spec.md](../generator-spec.md)
- 源项目 `src/proxystack/systemd/service.py`

## 交付物

- `internal/systemd`
- lifecycle 命令。
- fake systemd tests。

## 实现步骤

1. 实现 systemd runner 接口。
2. 实现 `service *` wrapper。
3. 实现 `start/restart`：检查二进制、apply runtime plan、调用 systemd。
4. 实现 `stop/status/logs/enable/disable`。
5. 实现 logs 多 unit 一次 journalctl 调用。

## 验收标准

- 订阅服务生命周期由 `pssub` 管理，不通过 `psctl` target 操作。
- 代理目标启动前检查二进制。
- `journalctl -f` 多 unit 一次调用。
- inactive status 退出码 3 不当作失败。
- systemd 错误保留 stdout/stderr 摘要。

## 依赖

- T12

## 风险

- 真实 systemd 仅做手工验收，自动测试必须使用 fake runner。
