# ps-sub input clone 命令交付

## 任务背景

`ps-sub input` 已支持查看、校验、编辑、改 host 和删除 input，但缺少从既有 input 派生新文件的安全入口。直接复制文件容易保留重复的 `node.id` 或同用户代理名，导致后续全量合并失败。

## 实现方案

- 新增 `ps-sub input clone SOURCE TARGET [--editor CMD]`。
- `TARGET` 不带扩展名时沿用源 input 扩展名。
- clone 初始内容会把 `input.source` 默认改成目标文件 basename。
- 命令默认进入编辑器，编辑完成后先做单文件 strict 校验，再把现有 inputs 加上目标 clone 做全量合并校验。
- 目标文件已存在、路径不安全、编辑器失败或校验失败时均不写入目标文件。

## 文件变更

- `internal/cli/sub/input_commands.go`：注册并实现 `input clone`。
- `internal/cli/sub/input_commands_test.go`：新增 clone 成功、未修改重复拒绝、目标存在拒绝测试。
- `docs/cli-spec.md`：补充 clone 命令规格、副作用和验收规则。
- `README.md`：补充常用流程示例。

## 配置与依赖变更

- 无新增配置。
- 无新增依赖。

## 测试结果

- `go test ./internal/cli/sub`：通过。
- `go test ./...`：未通过，失败点为 `internal/generator/sub` 的 `TestRenderSubscriptionsMatchGolden`，Surge golden 中 `ADS` 代理组期望值与实际渲染结果不一致；该失败与本次 `ps-sub input clone` 改动无关。

## 风险与后续建议

- clone 会继承源 input 的凭据内容，这是复制命令的预期行为；命令不打印敏感字段。
- 因默认编辑后才写入，非交互脚本应显式传 `--editor` 指向可自动修改临时文件的命令。
