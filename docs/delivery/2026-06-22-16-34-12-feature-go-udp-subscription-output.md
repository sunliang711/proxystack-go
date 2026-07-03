# ps-agent / ps-sub UDP 订阅字段支持

## 任务背景

`ps-agent` 的 `inbounds[].udp` 之前只能在值为 `true` 时导出到 `ps-sub` input，无法区分未配置和显式 `udp: false`。同时 `ps-sub` Clash/Premium Clash 输出只对 socks5/shadowsocks 写出 UDP 字段，未覆盖 vmess。

## 实现方案

- 根据 Mihomo 文档中 proxy 通用字段和协议示例，当前默认订阅节点按 `vmess`、`shadowsocks`、`socks5` 支持 UDP 处理，`http` 不接受显式 `udp`。
- `domain.Inbound` 增加显式 `udp` 字段检测，只有配置文件写了 `udp` 时才导出给订阅 input。
- `ps-agent` 导出的订阅 input 保留显式 `udp: true` 和 `udp: false`。
- `ps-sub` 默认节点校验拒绝不支持 UDP 的协议，并在 Clash/Premium Clash proxy 中输出对应 `udp` 布尔值。

## 文件变更

- `internal/domain/models.go`：新增 `UDPConfigured` 和 `SupportsInboundUDPProtocol`，并在 inbound 校验中拒绝不支持 UDP 的显式配置。
- `internal/generator/sub/input.go`：导出订阅 input 时保留显式 UDP 布尔值。
- `internal/generator/sub/types.go`：订阅 input 默认节点增加 UDP 协议支持校验。
- `internal/generator/sub/render.go`：Clash/Premium Clash 输出支持 vmess UDP 字段。
- `internal/config/loader_test.go`、`internal/generator/sub/sub_test.go`：补充 `udp: false` 传递和不支持协议拒绝测试。
- `internal/agentconfig/templates/*.yaml.tmpl`、`internal/agentconfig/examples.go`：配置模板和 CLI 示例补充 vmess `udp: true`。
- `docs/schema-spec.md`、`docs/generator-spec.md`：更新 inbound、SubscriptionNode 和订阅字段映射的 UDP 字段说明。

## 测试结果

- `go test ./internal/config ./internal/domain ./internal/generator/sub -run 'TestLoadStackRejectsUDPForHTTPInbound|TestRenderStackInputKeepsExplicitUDPFalse|TestLoadInputContentRejectsUDPForHTTPNode' -count=1`：通过。
- `go test ./internal/config ./internal/domain/validation -count=1`：通过。
- `go test ./...`：未完全通过；失败集中在 `internal/generator/sub.TestRenderSubscriptionsMatchGolden` 的既有 Surge golden 差异，当前模板输出的 `ADS` 策略组包含地区组和代理名，而 golden 中不包含。该差异与本次 UDP 改动无关。

## 风险与后续建议

- 本次未改变默认行为：未配置 `udp` 的节点仍不会在订阅中输出 UDP 字段。
- Surge 输出仍沿用现有能力，只在 socks5 且 `udp: true` 时输出 `udp-relay=true`，没有新增显式 false 语法映射。
- 建议后续单独核对 Surge `ADS` 策略组的模板期望并更新 golden 或修正模板。
