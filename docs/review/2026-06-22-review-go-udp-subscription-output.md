# UDP 订阅字段支持代码评审

## 审查范围

- `ps-agent` `xray.inbounds[].udp` 显式 `true/false` 导出链路。
- `ps-sub` Clash/Premium Clash proxy 的 `udp` 输出与协议校验。
- UDP 相关配置模板、CLI 示例、fixture 和文档同步。
- 当前工作区未提交 diff 中与 UDP 订阅字段相关的测试。

## 审查基线

- Go 代码评审 SOP：安全、规范、质量三维度。
- 项目既有配置加载、订阅 input、Clash/Premium Clash 渲染模式。
- 已知历史失败：`internal/generator/sub.TestRenderSubscriptionsMatchGolden` 的 Surge `ADS` golden 差异，不纳入本次问题。

## 独立 Agent 审查结论

独立 review agent `Kuhn` 未发现阻断或警告问题。

核对结果：

- `Inbound.UnmarshalYAML` 记录显式字段，`UDPConfigured` 可区分未配置和显式 `false`。
- `applyInboundUDP` 会把显式 `udp:true/false` 原样写入 `ps-sub` 节点。
- `Node.Validate` 会拒绝 `http` 节点显式配置 `udp`。
- `RenderClashProxy` 对 `vmess`、`shadowsocks`、`socks5` 输出 `udp`。
- `schema-spec`、`generator-spec`、`internal/agentconfig/templates/*.yaml`、`internal/agentconfig/examples.go` 和示例 fixture 已同步。
- 测试覆盖了显式 `udp:false` 传递、HTTP 不支持协议拒绝、CLI example 输出。

## 本地验证

- `git diff --check`：通过。
- `go test ./internal/agentconfig -count=1`：通过。
- `go test ./internal/cli/agent -run TestAgentExample -count=1`：通过。
- `go test ./internal/config ./internal/domain/validation -count=1`：通过。
- `go test ./internal/agentconfig ./internal/cli/agent ./internal/config ./internal/domain/validation ./internal/generator/sub -run 'TestAgentExample|TestLoadConfigAndStacksAcceptExamples|TestLoadStackRejectsUDPForHTTPInbound|TestRenderStackInputKeepsExplicitUDPFalse|TestLoadInputContentRejectsUDPForHTTPNode' -count=1`：通过。
- `go test ./...`：未完全通过；仍失败于既有 `internal/generator/sub.TestRenderSubscriptionsMatchGolden` 的 Surge `ADS` golden 差异，与本次 UDP 改动无关。

## 问题清单

未发现阻断或警告问题。

## 残余风险

- Surge 输出仍沿用现有行为，仅 socks5 且 `udp:true` 时输出 `udp-relay=true`，未新增显式 `false` 的 Surge 语法映射。
- 全量测试受既有 Surge golden 漂移影响，仍建议后续单独修复或更新该 golden。
