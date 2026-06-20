# ps-sub direct 节点字段兼容 Review

## 审查范围

- `internal/generator/sub/types.go`
- `internal/generator/sub/render.go`
- `internal/generator/sub/sub_test.go`
- `docs/schema-spec.md`
- `docs/delivery/2026-06-20-18-44-08-feature-go-ps-sub-direct-node-fields.md`

## 审查方式

- 使用独立 agent 对当前 diff 做只读 review。
- 第一轮 review 后按问题修复，再提交同一独立 agent 复审。

## 第一轮问题

### 阻断

- `direct=false` 节点只恢复了 node 顶层未知字段校验，但 `auth` 嵌套未知字段会被 `value.Decode` / `json.Unmarshal` 静默忽略，破坏原有严格输入契约。

### 修复

- 新增 `authKnownFields`。
- YAML strict 路径在 `rejectUnknownNodeYAMLFields` 中校验 `auth` 子 mapping。
- JSON strict 路径在 `rejectUnknownNodeJSONFields` 后校验 `auth` 子对象。
- 新增 YAML/JSON 两个回归测试覆盖 `auth.extra`。

## 第二轮结论

- 独立 agent 复审结论：未发现需要继续修改的问题。

## Surge 支持性过滤追加 Review

### 第一轮问题

- 警告：公共 `BuildTemplateContext` 会在 Clash/Premium Clash 渲染时提前构建 Surge 上下文并输出 unsupported Surge warning。
- 建议：补充日志安全断言，避免 warning 泄露 server、uuid、password 等敏感字段。

### 修复

- 将过滤后的 Surge 上下文移动到 `RenderSurgeSubscription` 中构建。
- 公共上下文保留未过滤的地区分组，避免影响 Premium Clash。
- 测试增加 Clash 渲染不输出 Surge warning，以及 warning 日志不包含敏感字段的断言。

### 第二轮问题

- 阻断：公共上下文中 `surge_region_groups` 置空会导致 Premium Clash 地区组丢失。

### 修复

- `BuildTemplateContext` 恢复未过滤的 `RenderSurgeRegionGroups(nodes)`。
- `RenderSurgeSubscription` 单独覆盖过滤后的 `surge_region_groups`、`surge_proxy_names` 和 `surge_proxy_lines`。

### 第三轮问题

- 建议：模板上下文字段文档缺少新增的 `surge_proxy_names`。

### 修复

- 在 `docs/template-compat-spec.md` 和 `docs/generator-spec.md` 中补充 `surge_proxy_names`。

### 最终结论

- 独立 agent 最终复审结论：未发现需要继续修改的问题。

## 本地验证

- `go test ./internal/generator/sub -run 'TestLoadInputContentRejectsUnknownNodeFieldWithoutDirect|TestLoadInputContentRejectsUnknownAuthFieldWithoutDirect|TestLoadJSONInputContentRejectsUnknownAuthFieldWithoutDirect|TestDirectNode|TestDirectJSONInputKeepsCustomFields|TestDirectYAMLBoolVariants'`：通过。
- `go test ./internal/generator/sub -run 'TestInputYAMLMatchesGolden|TestIndexJSONMatchesGolden|TestLoadInputContentRejectsUnknownNodeFieldWithoutDirect|TestLoadInputContentRejectsUnknownAuthFieldWithoutDirect|TestLoadJSONInputContentRejectsUnknownAuthFieldWithoutDirect|TestDirectNode|TestDirectJSONInputKeepsCustomFields|TestDirectYAMLBoolVariants'`：通过。
- `go test ./internal/generator/sub -run 'TestDirectNodeRendersSurgeVmessExtensions|TestDirectNodeRendersSurgeWebSocketAlias|TestDirectNodeSkipsUnsupportedSurgeNodes|TestDirectNodeRendersCustomClashFields|TestDirectJSONInputKeepsCustomFields|TestDirectYAMLBoolVariants'`：通过。
- `git diff --check`：通过。
- `go test ./...`：未完全通过；失败点为既有 `TestRenderSubscriptionsMatchGolden` 中 Surge ADS 组 golden 与模板实际输出差异，和本次 direct 节点能力无关。
