# uninstall purge flag 位置调整说明

## 任务背景

`--purge` 是 `ps-agent uninstall` 的危险选项，应只作为 uninstall 子命令的本地 flag 使用。

## 实现方案

- 移除 root persistent `--purge`。
- 在 `ps-agent uninstall` 子命令上注册本地 `--purge`。
- 保留普通 uninstall 与 purge uninstall 的既有卸载行为。

## 使用方式

支持：

```bash
ps-agent uninstall --purge
```

不再支持：

```bash
ps-agent --purge uninstall
```

## 验证结果

已通过：

```bash
go test -count=1 -run 'TestAgentUninstall|TestPurge|TestSetupCommand|TestAgentRejectsRemovedConfigFlag' ./internal/cli/agent
go test -count=1 ./internal/cli/agent
go test ./...
```

并手工确认：

- `ps-agent --help` 不再展示 `--purge`
- `ps-agent uninstall --help` 展示 `--purge`
- `ps-agent --purge uninstall` 返回 `unknown flag: --purge`
