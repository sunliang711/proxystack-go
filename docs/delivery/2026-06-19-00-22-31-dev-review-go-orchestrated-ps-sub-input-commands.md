# ps-sub input 命令开发与评审交付

## 任务背景

`ps-sub` 现有能力只覆盖 `init/config/import/clear/serve/doctor`，无法直接查看、校验或编辑 `<base-dir>/sub/inputs/` 下的订阅 input 文件。用户要求使用两个 agent 完成实现和独立 review。

## 编排方案

- T1 worker：实现 `ps-sub input` 命令组和测试。
- T2 review：独立审查本次变更的行为、安全和测试缺口。
- 主 Agent：集成实现、修复 review 阻断项、补文档并运行测试。

## 实现方案

新增 `ps-sub input` 命令组：

- `input list`：列出 input 文件、source、nodes、users、generated_at。
- `input show SOURCE`：默认输出脱敏后的规范 YAML。
- `input show SOURCE --raw`：输出原始文件内容。
- `input show SOURCE --show-secrets`：摘要输出保留敏感字段。
- `input validate [SOURCE]`：校验单个 input 或全量合并校验。
- `input edit SOURCE`：通过临时文件编辑，校验通过后原子替换。
- `input remove SOURCE`：删除单个 input 文件。

关键安全规则：

- SOURCE 只允许解析到 `<base-dir>/sub/inputs` 下的 `.yaml`、`.yml`、`.json` 普通文件。
- 默认 `show` 脱敏 `uuid`、`password`、`auth.password`。
- 拒绝 `inputs` 目录为 symlink。
- 拒绝最终 input 文件为 symlink。
- `edit` 保存前执行 schema 校验和单文件合并校验，避免保存重复代理名等运行期不可用配置。

## 文件变更

- `internal/cli/sub/root.go`：注册 `input` 命令组。
- `internal/cli/sub/input_commands.go`：新增 input list/show/validate/edit/remove 实现。
- `internal/cli/sub/root_test.go`：补 help 分组断言。
- `internal/cli/sub/input_commands_test.go`：新增 input 命令测试。
- `README.md`：补常用 input 命令示例。
- `docs/cli-spec.md`：补 `ps-sub input` 命令规格、副作用和验收规则。

## Review 问题与处理

独立 review 首轮发现 2 个阻断项和 1 个警告：

- 阻断：`inputs` 目录 symlink 可导致单文件命令越界读写。已新增 `ensureSubInputDir`，拒绝 symlink 或非目录。
- 阻断：`input edit` 写回前未执行合并校验，可能保存重复代理名。已新增 `validateSubInputContent`，写回前执行 `LoadInputContent` 和 `MergeInputs`。
- 警告：全量 `input validate` 绕过 CLI 安全扫描。已改为 `loadSubInputsFromSafeDir`，复用安全扫描。

复核结论：未发现新的阻断问题，首轮问题已闭合。

## 测试结果

- `go test ./internal/cli/sub`：通过。
- `go test ./...`：通过。

新增测试覆盖：

- input list 摘要输出。
- show 默认脱敏、raw 输出、show-secrets 输出。
- 单文件 validate 和全量 validate 重复节点检查。
- edit 非法内容不覆盖。
- edit 重复代理名不覆盖。
- edit 合法内容写回并保留 mode。
- remove 路径穿越拒绝。
- inputs 目录 symlink 拒绝。
- input 文件 symlink 单文件拒绝且全量 validate 不读取。

## 风险与后续建议

- `--raw` 和 `--show-secrets` 会按显式请求输出敏感字段，符合命令语义。
- 当前拒绝 `inputs` 目录本身和最终 input 文件 symlink，不递归拒绝 `base-dir` 或 `sub` 父级路径中的 symlink；如后续威胁模型要求物理路径必须完整落在真实 base-dir 内，可追加父路径组件级校验。
