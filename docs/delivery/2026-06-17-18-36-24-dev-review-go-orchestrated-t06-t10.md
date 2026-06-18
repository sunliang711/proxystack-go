# T06-T10 Go 实现与复审交付

生成时间：2026-06-17 18:36:24 CST

## 任务背景

根据 `docs/tasks/task-06-mihomo-generator.md` 至 `docs/tasks/task-10-sub-http-server.md`，完成 Go 版生成器、订阅发布包和订阅 HTTP 服务：

- T06：稳定 mihomo YAML 生成器，对齐 Python golden。
- T07：订阅 input/index 生成与合并。
- T08：兼容 `.j2` 覆盖模板的订阅渲染。
- T09：subscription bundle 与 native backup 的 zip 安全校验、hash 校验和导入恢复。
- T10：`ps-sub` HTTP 服务、token 鉴权、watcher 和 CLI 子命令。

## 编排方案

主 Agent 负责实现、集成和验证。完成后启动独立 review agent 做只读代码审查；根据审查结果修复阻断项和警告项，再复审直到通过。

## 实现摘要

- 新增 `internal/generator/mihomo`，支持 listener、raw upstream、xrelay-socks5 upstream、proxy-groups 和 rules profile。
- 新增 `internal/generator/sub`，支持 input/index、Clash/Premium/Surge 模板渲染、bundle 写入与导入。
- 新增 `internal/generator/backup`，支持 native backup 写出、读取和恢复。
- 新增 `internal/subserver`，实现 `/health`、`/sub`、`/premium_sub`、`/surge_sub`、token 鉴权、reload 失败保留旧 index 和 watcher。
- 扩展 `ps-sub` CLI：`serve`、`import`、`config`、`clear`，并支持 `--config`、`--data-dir`、`--listen`、`serve --host`、`serve --port`。
- 新增默认模板、订阅 fixture、mihomo/sub golden 和相关单元测试。

## 评审问题与处理

独立 review agent 第一轮发现 3 个阻断问题和 2 个警告：

- `ps-sub serve --data-dir DIR` 未读取 `DIR/config.yaml`。已改为先解析 `--data-dir`，未显式 `--config` 时读取 `<data-dir>/config.yaml`，并补命令测试。
- bundle replace-all 和 native restore 存在中途失败后半更新风险。bundle 已改为 staging 目录整体 swap；native restore 已改为 staged 替换 `config.yaml` 和 `stacks/`，并在失败时回滚 config。
- 模板 StrictUndefined 预检存在 for 变量泄漏和 `if not foo` 误判。已改为按模板 token 顺序维护 for 作用域，并补测试。
- `ps-sub serve` 缺少规格要求的 `--host`、`--port`。已补 CLI flag 和解析测试。
- mihomo default profile 文档与 Python golden 冲突。已按 T06 验收目标同步 `docs/generator-spec.md`，保持生成输出与 golden 一致。

最终复审结论：通过，无需继续修改。

## 验证结果

- `go test ./...`：通过。
- `go build ./...`：通过。
- `go test -race ./...`：通过。
- `go run ./cmd/ps-sub serve --help`：通过，确认 `--host` 和 `--port` 可见。
- 独立 review agent 复审也运行并通过 `go test ./...`、`go build ./...`、`go test -race ./...` 和 CLI 烟测。

## 风险与后续建议

- T06-T10 已完成；后续任务仍需继续实现 agent runtime、service/systemd、install/update 和端到端验收。
- `.j2` 兼容层面向文档声明的模板子集，不承诺完整 Jinja2 生态扩展语法；不支持语法会在加载或渲染时返回明确错误。
