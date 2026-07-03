# psctl ipinfo IPv4/IPv6 并发查询交付记录

## 任务背景

`psctl ipinfo STACK --family all` 原先按 IPv4、IPv6 串行查询。用户希望 IPv4 和 IPv6 并发检测，并在交互式终端中先显示：

```text
Detecting IPv4 ...
Detecting IPv6 ...
```

任一 family 完成后，直接在原行替换为一行结果摘要。

## 多 Agent 编排

- 实现 agent：负责代码实现和基础验证。
- review agent：独立审查本次未提交 diff，并在主线程修复后复审。
- 主 Agent：负责需求边界、修复集成、测试验证和最终交付。

## 实现方案

- `internal/diagnostics` 新增 family 级别进度事件，`all` 模式并发查询 IPv4 / IPv6。
- 单个 family 内仍保持来源串行 fallback，拿到匹配 IP 后停止后续来源。
- 并发结果按输入顺序回填，保证报告顺序稳定为 IPv4、IPv6。
- 一侧 family 出现 fatal error 时不提前取消另一侧，等待两侧完成后返回第一个错误。
- `internal/cli/agent` 在 TTY 输出下使用状态 renderer 原地刷新两行结果；非 TTY 输出保持普通完整报告，不输出 ANSI 控制字符。
- `go-isatty` 从 indirect 依赖调整为 direct 依赖，版本不变。

## 文件变更

- `go.mod`
- `internal/diagnostics/ipinfo.go`
- `internal/diagnostics/ipinfo_test.go`
- `internal/cli/agent/diagnostics_commands.go`
- `internal/cli/agent/diagnostics_commands_test.go`

## 测试结果

- `go test -count=1 ./internal/diagnostics ./internal/cli/agent`：通过。
- `go test -race -count=1 ./internal/diagnostics ./internal/cli/agent`：通过。
- `go mod tidy -diff`：通过，无 diff。
- `go test ./...`：失败在既有无关 golden 差异 `internal/generator/sub/TestRenderSubscriptionsMatchGolden`，差异涉及 tailscale 占位内容和 ADS 组输出，不属于本次 ipinfo 变更范围。

## 评审结论

review agent 初审未发现阻断问题，提出输出文案和错误取消语义两个一般风险，以及错误路径、TTY 乱序完成测试缺口。

主线程已处理：

- 恢复完整报告和 Summary 中原有 `未解析到` 文案。
- 调整并发错误处理，避免单侧错误取消另一侧 family。
- 补充单侧错误不取消 peer、progress done 事件、IPv6 先完成的 TTY 行替换测试。

review agent 复审结论：通过，未发现阻断问题、一般建议或必须补充的测试缺口。

## 风险与后续

- 真实 PTY 端到端未连接真实网络执行，当前由 renderer 单测覆盖 ANSI 行替换行为。
- 全仓测试仍受既有 `internal/generator/sub` golden 差异影响，需另行处理。
