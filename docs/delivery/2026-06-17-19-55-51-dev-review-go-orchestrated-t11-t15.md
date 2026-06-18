# T11-T15 Go 实现与复审交付

生成时间：2026-06-17 19:55:51 CST

## 任务背景

根据 `docs/tasks/task-11-agent-config-commands.md` 至 `docs/tasks/task-15-install-update.md`，完成 Go 版 agent 配置管理、runtime plan/manifest、systemd 生命周期与 unit/metadata、install/update/self update。

## 实现摘要

- 新增 `internal/agentconfig`：实现 `init/add/list/clone/member/remove` 的配置文件写入、端口分配、UUID 生成、member 维护和 stack 归档。
- 新增 `internal/runtime`：实现 generated file 收集、manifest read/write、sha256 diff、scope delete、atomic apply，并接入 `validate/check/render`。
- 新增 `internal/systemd`：实现 systemd runner、status/logs 输出、unit renderer、install/uninstall、metadata mode/owner 修复工具。
- 新增 `internal/install`：实现 target 展开、托管源 fallback、远端 URL 安全检查、受控 DialContext、sha256 校验、gzip/zip/tar 解包、原子替换、geo 回滚和 self update runner。
- 扩展 `ps-agent` CLI：挂载 T11-T15 相关命令，并保持 `check/render/validate` 只读、`service *` systemd-only。

## 评审问题与处理

独立 review agent 第一轮发现 7 个阻断问题和 1 个警告；第二轮发现 1 个阻断问题；第三轮复审通过。

- manifest scoped apply 丢失 scope 外记录：已改为按 raw target 合并旧 manifest，并补 scoped/disabled delete 测试。
- `service start/restart` 写 runtime：已拆成 systemd-only wrapper。
- 未知 unit target 默认全量：已改为返回错误并补测试。
- geo 多文件失败残留新文件：已补 created 删除、backup 恢复和排序写入测试。
- `config NAME` 缺少全量校验和 active-only 重启：已补全量校验、active 检测、apply plan 和 active unit restart。
- `status/logs` 输出被吞：已让 systemd 层返回 Result，CLI 转发 stdout/stderr。
- SSRF DNS rebinding 风险：已补默认受控 `DialContext` 和负面测试。
- metadata owner 修复不足：已补批量标准路径规则和 fake chown 测试。
- 组件级 disabled target 无法删除历史文件：已调整 `graph.ResolveTargetScope`，stack 存在但组件 disabled 时返回空 scope，并补 `xrelay/stack`、`clash/stack` delete 测试。

最终复审结论：通过，无阻断问题。

## 验证结果

- `go test ./internal/runtime ./internal/systemd ./internal/install ./internal/cli ./internal/agentconfig`：通过。
- `go test ./internal/graph ./internal/runtime`：通过。
- `go test ./...`：通过。
- `go build ./...`：通过。

## 风险与后续建议

- 真实 systemd、真实下载源和 root owner 修复仍需在部署环境手工验收。
- 下一步建议继续实现 T16 diagnostics/ipinfo，并在 T18 做完整端到端验收。
