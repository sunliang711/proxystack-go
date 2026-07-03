# Xray 私网直连能力交付记录

## 任务背景

用户希望在单独使用 Xray 替代 Clash upstream 的场景下，避免本机、局域网和 CGNAT 目标地址继续走上游代理。

## 实现方案

- 新增 `xray.outbound.private_direct` 可选配置项。
- 仅允许 `type: socks5` 和 `type: http` 使用 `private_direct`。
- 保持 `type: clash` 默认由 Clash/mihomo 负责规则分流，不在 Xray 层重复生成私网直连规则。
- 开启后 Xray 追加一个 `freedom` outbound，并生成 `routing.rules` 将本机、私网、CGNAT、IPv6 ULA 和链路本地地址转到该 outbound。

## 文件变更

- `internal/domain/models.go`：新增 `PrivateDirect` 字段和使用范围校验。
- `internal/generator/xray/config.go`：新增 routing 结构、私网直连 outbound 和路由生成逻辑。
- `internal/generator/xray/config_test.go`：补充开启场景 golden 测试和误用校验测试。
- `tests/golden/xray/socks-private-direct.json`：新增 Xray 私网直连 golden。
- `internal/agentconfig/templates/snippets/xray/outbound/*.yaml.tmpl`：补充示例注释。
- `docs/schema-spec.md`、`docs/generator-spec.md`：补充配置和生成行为说明。

## 配置与依赖变更

- 新增可选配置：`xray.outbound.private_direct`。
- 无数据库、缓存、外部服务和 `go.mod` 依赖变更。

## 测试结果

- 通过：`go test ./internal/generator/xray`
- 通过：`go test ./internal/generator/xray ./internal/config ./internal/domain/validation`
- 全量：`go test ./...` 未通过，失败点为 `internal/generator/sub` 的 Surge golden 与当前 Tailscale/ADS 输出不一致；该失败与本次 Xray 私网直连改动无关。

## 风险与后续建议

- `private_direct` 默认关闭，避免改变既有 Xray-only 代理出口行为。
- 若后续希望 Xray-only 成为主要模式，可考虑把私网直连规则集抽成可配置 CIDR/domain 列表。
