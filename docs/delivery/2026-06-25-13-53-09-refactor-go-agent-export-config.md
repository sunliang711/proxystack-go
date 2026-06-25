# ps-agent export-config 命令迁移

## 背景

`ps-agent sub export-config` 实际职责是从 agent 配置和 stacks 渲染指定用户的订阅文本，不写订阅发布包，也不操作 `ps-sub` inputs。为避免和 `sub export` 发布包职责混在一起，本次将入口移动到 `ps-agent export-config`。

## 变更内容

- 新增顶层命令：`ps-agent [--base-dir DIR] export-config sub|premium_sub|surge_sub USER`。
- 移除旧入口：`ps-agent sub export-config ...` 不再注册。
- `ps-agent sub` 空参数时显示帮助，未知参数不再静默成功。
- 更新 `docs/cli-spec.md` 中的命令规格。

## 行为保持策略

- 复用原有 `renderSubConfig` 渲染逻辑。
- 不改变订阅文本格式、模板解析、agent 配置加载和 stack 解析行为。
- 不改变 `ps-agent sub export`、`ps-agent sub validate-inputs` 和 `ps-sub` 行为。

## 验证结果

- `go test ./internal/cli/agent`：通过。
- `go test ./...`：未完全通过，失败点为 `internal/generator/sub` 的 `TestRenderSubscriptionsMatchGolden`，Surge golden 中 `ADS` 代理组期望值与实际渲染结果不一致；该失败与本次命令迁移无关。

## 风险

- 旧命令路径不再兼容，依赖 `ps-agent sub export-config` 的脚本需要改为 `ps-agent export-config`。
