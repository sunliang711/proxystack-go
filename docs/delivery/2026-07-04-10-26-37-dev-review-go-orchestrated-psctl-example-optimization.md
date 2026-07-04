# psctl example 优化交付记录

## 任务背景

用户希望按既定方案优化 `psctl example`，范围限定为一致性补齐、片段有效性验证和错误提示体验优化，并要求由独立 agent 开发、另一个独立 agent review。

## 编排方案

- 实现 agent：负责 `psctl example` 代码、测试和 CLI 文档改动。
- 主 Agent：负责检查实现差异、补齐 review 建议、运行验证并生成交付记录。
- review agent：独立审查最终差异，重点检查成功路径回归、错误提示契约和测试覆盖有效性。

## 实现方案

- 将 `psctl example` 文档 usage 从 `[stack|xray|clash]` 补齐为 `[config|stack|xray|clash]`。
- 将 help 示例中的 `psctl example xray inbound vmess` 改为主名称 `vmess-raw`，保留 `vmess -> vmess-raw` 兼容。
- 未知片段错误改为分层提示：
  - 未知 area 只列出 area。
  - 未知 section 只列出当前 area 下的 section。
  - 未知 type 只列出当前 section 下的 type。
- 增加 `Did you mean` 建议，基于短命令片段编辑距离匹配，距离过大时不提示。
- 新增 snippet 测试，覆盖片段定义唯一性、ID 元数据一致性、模板路径存在、渲染非空，以及注入最小父结构后的 config/domain 校验。

## 文件与配置变更

- `internal/cli/agent/example_commands.go`
- `internal/cli/agent/example_commands_test.go`
- `internal/agentconfig/snippet_templates_test.go`
- `docs/cli-spec.md`

未新增依赖，未修改模板内容、domain model、生成器或运行时逻辑。

## 测试结果

- `gofmt -w internal/cli/agent/example_commands.go internal/cli/agent/example_commands_test.go internal/agentconfig/snippet_templates_test.go`：通过。
- `git diff --check`：通过。
- `go test ./internal/agentconfig ./internal/cli/agent -count=1`：通过。
- `go test ./internal/cli/agent -run 'TestAgentExample' -count=1`：通过。
- 手工验证：
  - `go run ./cmd/ps-agent example xray inbund vmess-raw`：输出 section 层级建议。
  - `go run ./cmd/ps-agent example clash listener socsk`：输出 type 层级建议。
  - `go run ./cmd/ps-agent example xray inbound vmess`：仍兼容输出 `vmess-raw` YAML 片段。
- `go test ./...`：失败在既有 `internal/generator/sub` golden mismatch，差异为 Tailscale 占位和 ADS 组内容；本次未修改订阅生成器、订阅模板或 golden。

## 评审问题清单与处理结果

- 文档描述仍只写 stack YAML，且覆盖范围缺少 `config users`：已修复。
- `Did you mean` 建议未实现：已补齐 area/section/type 三层建议和测试。

复审结论：未发现阻断问题，最终差异可以通过 review。

## 风险与后续建议

- 新增 snippet 测试覆盖文件内模型校验和最小父结构校验，但未走 `config.LoadStacks` / `ResolveStackSetUserRefs` 跨文件链路；如后续继续增强，可单独补充端到端片段装配测试。
- 全量测试中 `internal/generator/sub` golden mismatch 是独立既有问题，建议另起任务处理。
