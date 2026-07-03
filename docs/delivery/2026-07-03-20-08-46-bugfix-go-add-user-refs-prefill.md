# psctl add 用户引用预填修复记录

## 问题背景

执行 `psctl add usa001` 时，内置 stack 模板固定引用 `user1/default`。当实际 `config.yaml users` 中只有其它用户时，候选 stack 在写入前校验失败。

## 根因分析

- 根因位置：`internal/agentconfig` 的 add 候选模板生成流程。
- 问题类型：配置引用逻辑。
- 触发条件：内置模板中的 `user_refs` 仍保留默认 `user1/default`，但全局配置没有该用户档案。
- 为什么会发生：模板没有根据当前 `config.yaml users` 改写默认用户引用。

## 修复方案

- 内置模板生成后，将默认 `user1/default` 引用改写为 `config.yaml users` 中的第一个用户档案。
- 如果 `config.yaml users` 为空，`psctl add` 直接报错，提示先配置全局用户。
- 交互式 `psctl add` 使用未最终校验的草稿候选，用户保存后再执行完整校验；`--no-edit` 仍立即校验并写入。
- `--from-file` 保留原文件中的用户引用，不做自动改写。

## 验证结果

- 通过：`go test ./internal/agentconfig ./internal/cli/agent`
- 通过：`git diff --check`
- 手工验证：`config.yaml users[0]` 为 `alice/mobile` 时，`psctl add usa001 --no-edit` 生成的 stack 预填 `alice/mobile`。
- 手工验证：`config.yaml` 没有 `users` 时，`psctl add usa001 --no-edit` 返回 `config users is required`。

## 风险与后续建议

- 当前自动选择第一个用户档案，适合单用户或默认用户排在首位的配置。多用户场景如果需要选择其它用户，仍可在交互式草稿中修改后保存。
