# 订阅节点 display_template 命名策略交付记录

## 任务背景

用户希望调整 ps-sub 订阅节点显示名生成策略，避免默认拼接 user、端口和原始 remark 后导致节点名过长，并支持通过模板自定义最终订阅节点名。

## 实现方案

- 新增 `display_template` 配置项，配置层面使用 display 命名，生成的 subscription input 仍写入 `nodes[].remark`。
- `display_template` 非空时，使用模板渲染结果作为订阅节点显示名。
- `display_template` 为空且显式 `remark` 非空时，直接使用 `remark` 原值。
- `display_template` 和显式 `remark` 都为空时，使用 `{stack} {protocol}` 作为默认显示名。
- 多用户节点优先使用 `users[].display_template`，未配置时继承 inbound 级 `display_template`。
- 手工 input 中已有 `nodes[].remark` 不做二次改写。

模板使用 Go `text/template` 语法，支持变量：

- `{{.stack}}`
- `{{.protocol}}`
- `{{.port}}`
- `{{.user}}`
- `{{.remark}}`

支持函数：

- `toUpper`
- `toLower`
- `trim`
- `replace`

模板结果会 trim；语法错误、未知变量或渲染后为空时生成失败。

## 文件变更

- `internal/domain/models.go`：新增 inbound 级和 users 级 `display_template` 字段。
- `internal/generator/sub/input.go`：更新 `subscriptionRemark` 规则，并新增模板渲染逻辑和函数白名单。
- `internal/generator/sub/sub_test.go`：新增单测覆盖显式 remark、默认名、模板变量、模板函数、用户级模板覆盖和非法模板失败。
- `docs/schema-spec.md`：同步新增配置字段说明。
- `docs/generator-spec.md`：同步订阅节点字段映射和模板语法说明。

## 配置与依赖变更

- 新增可选配置项：`display_template`。
- 无数据库、缓存、外部服务和 `go.mod` 依赖变更。

## 测试结果

- 通过：`go test ./internal/generator/sub -run 'TestRenderStackInputUsesNewRemarkStrategy|TestRenderStackInputRejectsBadDisplayTemplate|TestRenderStackInputKeepsExplicitUDPFalse|TestRenderStackInputKeepsVmessTransportOptions|TestInputYAMLMatchesGolden'`
- 通过：`go test ./internal/config ./internal/domain/validation`
- 已知未通过：`go test ./internal/generator/sub` 中 `TestRenderSubscriptionsMatchGolden` 的 Surge golden 与当前 Tailscale/ADS 输出不一致；该差异与本次 remark 规则调整无关。

## 风险与后续建议

- 默认名只包含 stack 和 protocol，同一用户下若同 stack 存在多个相同协议且都未配置 remark 或 display_template，合并时会因重复 proxy name 失败。建议这类场景显式配置不同 `remark` 或 `display_template`。
- 模板函数采用白名单，当前只支持大小写、trim 和 replace；后续如需要更多格式化能力，可按需补充。
