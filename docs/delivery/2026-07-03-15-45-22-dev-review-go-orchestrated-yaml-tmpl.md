# agentconfig 模板扩展名改为 yaml.tmpl

## 任务背景

`internal/agentconfig/templates` 下的内置 stack 模板和 snippet 片段包含 Go `text/template` 语法，继续使用 `.yaml` 扩展名会让编辑器按纯 YAML 校验并产生误报。本次将这些模板文件改为 `.yaml.tmpl`，只调整模板源文件命名和读取路径，不改变渲染后的用户配置文件格式。

## 编排方案

- 实现 agent：负责模板文件重命名、Go 代码引用同步和聚焦测试。
- review agent：独立审查本次未提交变更，重点检查 embed、ReadFile、snippet 路径表和运行时输出文件名是否被误改。
- 主 Agent：整合结果、补充残留路径检查、机械内容比对、验证和交付文档。

## 实现方案

- 将 `internal/agentconfig/templates` 下 51 个 `.yaml` 模板文件重命名为 `.yaml.tmpl`。
- 更新 `internal/agentconfig/stack_document.go` 中 stack 模板的 `go:embed` pattern、`ReadFile` 路径和错误上下文文件名。
- 更新 `internal/agentconfig/snippet_templates.go` 中所有内置 snippet 定义路径。
- 同步更新少量直接引用这些内置模板路径的既有文档。
- 保持运行时生成和读取的用户 stack 文件仍为 `stacks/<name>.yaml`。

## 文件与配置变更

- `internal/agentconfig/templates/**/*.yaml` -> `internal/agentconfig/templates/**/*.yaml.tmpl`
- `internal/agentconfig/stack_document.go`
- `internal/agentconfig/snippet_templates.go`
- `docs/delivery/2026-06-22-16-34-12-feature-go-udp-subscription-output.md`
- `docs/delivery/2026-07-01-16-44-32-feature-go-xray-private-direct.md`
- `docs/review/2026-06-22-review-go-udp-subscription-output.md`

## 测试结果

- `go test -count=1 ./internal/agentconfig ./internal/cli/agent`：通过。
- `git diff --check`：通过。
- `go test ./...`：失败在 `internal/generator/sub/sub_test.go:841` 的订阅 golden mismatch；该失败来自根目录 `templates/sub/*.j2` 渲染输出差异，本次未修改订阅模板和订阅生成器，未纳入本次修复范围。

## 评审结论

review agent 未发现阻断问题或明确代码问题。重点核查通过：

- `go:embed` 已覆盖 `.yaml.tmpl` stack 模板。
- 内置 stack 模板读取路径已同步。
- snippet 路径表已全部指向 `.yaml.tmpl`。
- 用户运行时 stack 输出仍保持 `<name>.yaml`。
- 51 个新 `.yaml.tmpl` 文件与对应旧 `.yaml` 文件内容一致。
- `internal/agentconfig/templates` 下已无裸 `.yaml` 模板文件。
- 旧内置模板路径残留搜索未命中。

## 风险与后续建议

- 新 `.yaml.tmpl` 文件当前在 Git 中表现为未跟踪文件；提交时必须和旧 `.yaml` 删除一起纳入。
- 全量测试的订阅 golden mismatch 建议另起任务处理，避免和本次模板扩展名重构混在一起。
