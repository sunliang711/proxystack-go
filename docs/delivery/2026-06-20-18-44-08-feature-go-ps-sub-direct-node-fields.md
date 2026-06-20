# ps-sub direct 节点字段兼容

## 任务背景

`ps-sub` 现有订阅 input 使用严格 `sub.Node` schema，默认拒绝 `tls`、`skip-cert-verify`、`servername`、`ws-opts` 等 Clash/Mihomo 原生字段。用户需要在部分节点上直接维护远程 Xray 服务端对应的客户端连接字段，同时继续兼容从 `ps-agent` 导入的规范 input。

## 实现方案

- 在 `nodes[]` 上新增 `direct: true` 开关，默认 `false`。
- `direct: false` 保持严格字段校验，避免旧 input 静默吞掉误写字段。
- `direct: true` 允许节点级自定义字段，并保留原始 YAML/JSON 字段。
- Index JSON 输出会把 direct 节点的原始字段合并回结果，避免中间索引丢失 `tls`、`ws-opts` 等字段。
- Clash/Premium Clash 渲染时，direct 节点按原始字段输出为 proxy，并补齐 vmess 默认 `alterId: 0` 和 `cipher: auto`。
- Surge 渲染时只输出确认支持的节点；direct vmess 仅支持 raw/tcp 与 ws/websocket，兼容映射 `tls`、`skip-cert-verify`、`servername`、`ws-opts.path` 和 `ws-opts.headers`。
- Surge 不支持或不确认的节点会从 `[Proxy]` 与策略组中移除，并记录 warning。

## 文件变更

- `internal/generator/sub/types.go`：新增 `Node.Direct`、direct 解码逻辑、raw 字段保留、Index JSON 保留、direct 未知协议兼容和规范化 YAML 输出。
- `internal/generator/sub/render.go`：改为用 YAML 节点渲染 Clash proxy，新增 direct Clash 直通、Surge 支持性过滤和 direct vmess 扩展映射。
- `internal/generator/sub/sub_test.go`：补充 direct 严格校验、auth 嵌套严格校验、Clash 字段直通、Surge 映射、Surge 跳过 unsupported 节点、JSON/YAML 保留和布尔解析测试。
- `templates/sub/surge.conf.j2`：Surge 策略组改用过滤后的 proxy 列表，避免引用被跳过节点。
- `docs/schema-spec.md`：补充 `direct` 字段和行为说明。

## 配置与依赖变更

- 无新增依赖。
- 无全局配置变更。

## 测试结果

- `go test ./internal/generator/sub -run 'TestDirectNodeRendersSurgeVmessExtensions|TestDirectNodeRendersSurgeWebSocketAlias|TestDirectNodeSkipsUnsupportedSurgeNodes|TestDirectNodeRendersCustomClashFields|TestDirectJSONInputKeepsCustomFields|TestDirectYAMLBoolVariants'`：通过。
- `go test ./internal/generator/sub -run 'TestInputYAMLMatchesGolden|TestIndexJSONMatchesGolden|TestLoadInputContentRejectsUnknownNodeFieldWithoutDirect|TestLoadInputContentRejectsUnknownAuthFieldWithoutDirect|TestLoadJSONInputContentRejectsUnknownAuthFieldWithoutDirect|TestDirectNode|TestDirectJSONInputKeepsCustomFields|TestDirectYAMLBoolVariants'`：通过。
- `git diff --check`：通过。
- `go test ./...`：未完全通过；失败点为既有 `TestRenderSubscriptionsMatchGolden` 中 Surge ADS 组 golden 与模板实际输出差异，和本次 direct 节点能力无关。

## 独立 Review 结果

- 第一轮独立 review 发现 1 个阻断问题：自定义 `UnmarshalYAML/UnmarshalJSON` 后，`direct=false` 的 `auth` 嵌套未知字段会被静默忽略。
- 已修复：对 `auth` 子对象补回 YAML/JSON 严格字段校验，并增加对应回归测试。
- 第二轮独立 review 结论：未发现需要继续修改的问题。

## 风险与后续建议

- Surge 映射当前只覆盖已确认支持的 vmess raw/ws 字段；VLESS 以及 vmess grpc/h2/http/xhttp/quic/kcp 等不确认的组合会被跳过。
- 如后续确认 Surge 支持更多协议或传输层，可按协议逐步补充 Surge 映射和测试。
