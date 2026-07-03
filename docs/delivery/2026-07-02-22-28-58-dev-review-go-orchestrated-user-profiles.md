# config users 与 user_refs 交付记录

## 任务背景

用户希望把订阅用户信息从 stack 内抽到 `config.yaml users`，由 stack inbound 通过 `user_refs` 引用，并采用 `user + profile` 的方式支持同名用户的不同凭据。新方案不要求兼容老配置中的 user 信息，但 `psctl add` 模板和 `psctl example` 子命令必须提供新的配置模板。

本次按双 agent 流程执行：开发 agent 负责实现主功能，review agent 负责独立复审；主线程根据复审结果补齐问题并再次验证。

## 实现方案

- 在 `config.yaml` 新增全局 `users` 档案，唯一键为 `(user, profile)`，未配置 profile 时默认 `default`。
- 在 stack inbound 新增 `user_refs`，支持字符串简写和对象写法；对象写法可以覆盖 `uuid/password/email/remark/display_template/tag` 等字段。
- 加载 stack 后先把 `user_refs` 展开为内部 `InboundUser`，再进入 Xray、订阅和跨 stack 校验流程。
- `display_template` 优先级为 `user_refs[].display_template`、`config.yaml users[].display_template`、inbound 级 `display_template`、`remark`、默认名称。
- 模板上下文支持 `.stack/.inbound/.protocol/.port/.user/.profile/.remark`。
- 同一订阅 user 下最终节点名不能重复，包含全局用户模板展开后的节点。
- `psctl add` 默认配置模板、`psctl example config users default` 和 inbound 示例都改为展示 `users + user_refs`。

## 文件变更

- `internal/domain/models.go`、`internal/domain/user_refs.go`：新增全局用户档案、`user_refs` 模型、展开和协议凭据校验。
- `internal/config/loader.go`：加载 stack 后统一展开 `user_refs` 并执行跨 stack 校验。
- `internal/generator/sub/input.go`、`internal/generator/sub/types.go`、`internal/generator/xray/config.go`：生成前幂等展开 `user_refs`，并按新优先级生成订阅节点。
- `internal/domain/validation/validation.go`：新增同一订阅用户下节点名重复检查。
- `internal/agentconfig/*`、`internal/cli/agent/example_commands.go`、`internal/agentconfig/templates/snippets/*`：更新 add/example 模板。
- `docs/schema-spec.md`、`docs/generator-spec.md`：同步配置 schema 与生成规则说明。
- `tests/fixtures/example-project/*`：示例工程改用全局 users 与 user_refs。

## Review 处理

review agent 首轮发现的文档、错误提示和测试覆盖建议均已处理：

- shadowsocks snippet 描述改为全局 users 和 user_refs。
- schema 文档补充 `user_refs`，旧 `users` 标为 legacy/in-memory。
- VMess/Shadowsocks 错误提示改为指向 `user_refs`。
- 增加 config users 重复 `(user, profile)` 测试。
- 增加全局用户 `display_template` 触发重复节点名检测测试。

复审结论：无阻断问题，可以交付。

## 测试结果

- 通过：`git diff --check`
- 通过：`go test ./internal/domain ./internal/config ./internal/domain/validation ./internal/generator/xray ./internal/agentconfig ./internal/cli/agent`
- 通过：`go test ./internal/generator/sub -run 'TestRenderStackInput|TestLoadInput|TestMergeInputs|TestDirectNode|TestRenderSurge|TestRenderClash|TestRenderPremium'`
- 已知未通过：`go test ./...` 仍失败在 `internal/generator/sub TestRenderSubscriptionsMatchGolden`，差异是既有 Surge golden 中 Tailscale 注释块和 `ADS` 组代理列表，与本次 user_refs 改动无关。

## 风险与后续建议

- `ValidateStackSet` 当前依赖调用方传入已展开的 `StackSet`；生产加载和生成路径已覆盖。后续如果有新工具直接调用 validation，需要先执行 `ResolveStackSetUserRefs`，或把 resolve 收敛到 validation 内部。
- legacy `user/users` 路径仍作为内部模型入口保留，模板和示例已切到新方案。
