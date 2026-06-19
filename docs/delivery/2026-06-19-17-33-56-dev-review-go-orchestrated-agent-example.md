# ps-agent example 配置片段命令交付记录

## 任务背景

为 `ps-agent` 增加配置帮助命令，用户可以通过 `ps-agent example ...` 将 stack 配置片段输出到 stdout，便于复制到配置文件中。

## 实现方案

- 新增 `ps-agent example [stack|xrelay|clash] [SECTION] [TYPE]`。
- 不带参数时输出 usage 和全部支持片段说明。
- 精确到 `TYPE` 时输出纯 YAML。
- 只筛选到 area 或 section 时输出带注释的候选片段，并提示用户选择其中一个。

## 文件变更

- `internal/agentconfig/examples.go`：新增 stack 配置片段库。
- `internal/cli/agent/example_commands.go`：新增 Cobra 命令、usage、筛选和输出逻辑。
- `internal/cli/agent/example_commands_test.go`：新增 example 命令测试。
- `internal/cli/agent/root.go`：注册 example 命令。
- `internal/cli/agent/root_test.go`：补充 root help 断言。
- `docs/cli-spec.md`：补充 example 命令规格。

## 覆盖范围

- `stack role`：`edge`、`auto`
- `xrelay loglevel`：`debug`、`info`、`warning`、`error`、`none`
- `xrelay auth`：`noauth`、`password`
- `xrelay inbound`：`vmess`、`shadowsocks`、`socks5`、`http`
- `xrelay outbound`：`clash`、`socks5`、`http`、`direct`
- `clash mode`：`rule`、`global`、`direct`
- `clash loglevel`：`debug`、`info`、`warning`、`error`、`silent`
- `clash listener`：`socks`、`http`
- `clash upstream`：`xrelay-socks5`、`raw`、`raw-shadowsocks`、`raw-socks5`、`raw-http`
- `clash group`：`select`、`url-test`、`load-balance`、`fallback`
- `clash rules`：`default`

## 测试结果

- `go test ./internal/cli/agent`：通过
- `go test ./...`：通过
- 手工验证：
  - `go run ./cmd/ps-agent example --help`
  - `go run ./cmd/ps-agent example xrelay inbound vmess`
  - `go run ./cmd/ps-agent example xrelay outbound`
  - `go run ./cmd/ps-agent example xrelay outbound direct`
  - `go run ./cmd/ps-agent example clash upstream raw-http`

## 独立评审

独立 review agent 发现一个非阻断警告：section 级输出多个候选片段时，用户可能误以为整段都能直接粘贴。已修复为多候选输出顶部提示“选择其中一个”，并在每个片段注释里给出精确命令。

复核结论：无阻断问题，无新增明显风险。

## 残余风险

- 片段库与 schema 枚举目前是手工同步，后续 schema 增减时需要同步更新 example 片段。
- 当前测试主要覆盖 CLI 输出形态，尚未把每个片段自动注入父级结构做完整 domain validate。
