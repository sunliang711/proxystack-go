# 配置草稿编辑与 clone 默认交互交付记录

## 任务背景

本次任务针对 `ps-agent` / `ps-sub` 中会进入编辑器的配置命令统一引入草稿编辑能力，避免用户在编辑后因 YAML、端口、ref 或 input 合并校验失败而丢失修改内容。

同时调整 `ps-agent add` 和 `ps-agent clone` 的交互默认值：

- `ps-agent add NAME` 默认打开编辑器，`--no-edit` 用于脚本化写入。
- `ps-agent clone SOURCE TARGET` 默认分配新端口并打开编辑器，`--no-edit` 用于脚本化写入。
- clone 只改写真正 schema ref，避免误改普通域名、Host 或 raw config 中的普通 `ref` 字段。

## 多 Agent 分工

- 实现 agent：Faraday，负责代码实现、文档更新和目标测试。
- Review agent：Maxwell，负责只读调研、正式 review 和阻断项复审。
- 主 Agent：负责需求收敛、集成检查、补充草稿错误提示、修复 review 阻断项、运行验证与交付汇总。

## 实现方案

### 通用草稿编辑

新增 `internal/cli/draftedit`，提供：

- 相邻草稿路径：`<target>.draft`。
- 已存在草稿优先复用，便于用户继续上次未通过校验的编辑内容。
- 编辑后先执行调用方传入的完整校验。
- 校验通过后调用提交函数原子落盘，并删除草稿。
- 编辑器失败、读草稿失败、校验失败或提交失败时保留草稿，并在错误中输出 `draft preserved: <path>`。

### Agent 配置命令

- `ps-agent add`：默认生成候选 stack 草稿并打开编辑器；`--no-edit` 走原程序化写入。
- `ps-agent clone`：默认分配新端口并打开编辑器；候选内容通过草稿校验后才创建 target。
- `ps-agent config [NAME]`：由临时文件编辑改为相邻草稿编辑，成功后仍保持原有自动重启 active stack 的行为。

### Sub 配置与 input 命令

- `ps-sub config`：使用 `<base-dir>/config.yaml.draft` 草稿。
- `ps-sub input edit`：使用 `<input-file>.draft` 草稿，并执行单文件与全量替换合并校验。
- `ps-sub input clone`：使用 `<target-file>.draft` 草稿，校验通过后才创建 target input。

### Clone ref 改写

原实现会递归改写所有形如 ref 的 scalar，容易误改普通字符串。现改为路径感知：

- 只改写 `xrelay.outbound.ref`，且 outbound type 为 `clash`。
- 只改写 `clash.upstreams[].ref`，且 upstream type 为 `xrelay-socks5`。
- 不进入 raw upstream `config` 区域，不改 `server`、`Host` 或普通 `ref` 字段。

## 文件变更

- `internal/cli/draftedit/draftedit.go`：新增通用草稿编辑器。
- `internal/cli/agent/config_commands.go`：接入 `add`、`clone`、`config` 草稿编辑和默认交互行为。
- `internal/agentconfig/commands.go`：新增 add/clone 候选 stack 构建能力。
- `internal/agentconfig/stack_document.go`：拆分 YAML 编码逻辑，修复 clone ref 改写范围。
- `internal/cli/sub/root.go`：`ps-sub config` 接入草稿编辑。
- `internal/cli/sub/input_commands.go`：`input edit/clone` 接入草稿编辑和替换式全量合并校验。
- `docs/cli-spec.md`：同步命令默认行为、草稿副作用和验收规则。
- 相关测试文件：补充 add/clone 默认编辑、草稿保留、错误提示、ref 精准改写等测试。

## 验证结果

通过：

```bash
go test ./internal/cli/draftedit ./internal/agentconfig ./internal/cli/agent ./internal/cli/sub
git diff --check
```

手工验证：

- `ps-agent add usa1 --editor true` 默认进入编辑器，写入 stack 后删除 `.draft`。
- `ps-agent clone usa1 usa2 --editor true` 默认分配新端口。
- clone 后 `server: usa1.example.com`、`Host: usa1.example.com` 保持不变。

未全量通过：

```bash
go test ./...
```

失败点为既有 `internal/generator/sub TestRenderSubscriptionsMatchGolden` golden 差异：`ADS` proxy group 实际输出包含 `🌐 其他地区, Manual Relay`，与本次草稿编辑和 agent clone 改动无关。

## Review 结论

正式 review 发现 1 个阻断问题：

- clone 只限定 key 为 `ref` 仍不够精确，raw upstream `config.ref` 或嵌套 `headers.ref` 可能被误改。

处理结果：

- 已改为路径感知 schema ref 改写。
- 已补测试覆盖 raw config 中普通 `ref` 和嵌套普通 `ref` 不被改写。
- Review agent 复审通过，未发现新的阻断问题。

## 风险与后续建议

- 草稿采用相邻 `<target>.draft` 文件，不会被当前 `stacks/*.yaml` 或 input `.yaml/.yml/.json` 扫描逻辑读取。
- 已存在草稿会被复用，这是恢复未完成编辑的预期行为；用户如需重新生成候选内容，需要先删除对应 `.draft`。
- 后续可考虑增加显式 `--resume` 或 `--discard-draft`，让草稿恢复/丢弃流程更直观。
