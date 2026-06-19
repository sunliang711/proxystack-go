# ps-sub input external_host 管理能力

## 任务背景

`ps-sub` 的 input 文件原先要求每个 `nodes[].server` 都是最终生效值。用户希望同时支持两类维护方式：

- 批量修改现有 input 的所有节点 host。
- 在 input 文件级增加 `external_host`，作为节点 `server` 缺失或为空时的默认值。

本次不引入 `external_host_mode`，避免运行时语义隐式覆盖局部 server。

## 多 Agent 拆分方案

- T1 Worker：实现 `ps-sub input set-host HOST [SOURCE] [--all]`，负责 CLI、写回、参数校验和命令测试。
- T2 Worker：实现 input 文件级 `external_host` 默认值，负责 generator/sub schema、加载合并逻辑、测试和 schema 文档。
- Review Agent：独立审查本次 host 相关文件，重点检查默认值应用路径、局部 server 优先、批量写回的原子性和测试覆盖。

## 实现方案

- `sub.Input` 新增可选字段 `external_host`。
- input 加载、校验和合并前统一应用文件级默认值：仅当 `nodes[].server` 缺失或为空时补齐，不覆盖已有局部 server。
- `InputToYAML` 在 `external_host` 非空时稳定输出字段，顺序为 `generated_at` 后、`nodes` 前。
- 新增 `ps-sub input set-host HOST [SOURCE] [--all]`：
  - `HOST` trim 后不能为空。
  - `SOURCE` 与 `--all` 互斥，且必须选择其一。
  - 单文件模式只改指定 input，全量模式扫描全部安全 input 文件。
  - 写回前先完成 strict decode、单文件 merge 校验和全量 merge 校验。
  - 已有文件级 `external_host` 时同步更新，避免默认值残留旧 host。
  - YAML 使用稳定输出，JSON 使用 `json.MarshalIndent` 规范回写。

## 文件与配置变更

- `internal/generator/sub/types.go`：新增 `Input.ExternalHost`、默认值补齐逻辑和 YAML 输出。
- `internal/generator/sub/input.go`：加载与合并路径应用文件级默认值。
- `internal/generator/sub/sub_test.go`：补旧文件兼容、默认值补齐、局部 server 优先、缺失 server 失败和 YAML 输出测试。
- `internal/cli/sub/input_commands.go`：新增 `set-host` 命令和批量写回实现。
- `internal/cli/sub/input_commands_test.go`：补单文件、全量、参数错误、失败不写回、unchanged、JSON 回写、`external_host` 同步测试。
- `docs/cli-spec.md`：补 `input set-host` 命令规格、副作用和验收。
- `docs/schema-spec.md`：补 input 文件级 `external_host` 和 `server` 条件必填说明。
- `docs/generator-spec.md`：补合并前应用文件级默认值的规则。

## 测试结果

- `go test ./internal/generator/sub`：通过。
- `go test ./internal/cli/sub`：通过。
- `go test ./internal/cli/sub ./internal/generator/sub`：通过。
- `git diff --check`：通过。
- `go test ./...`：未完全通过；失败集中在 `internal/deployment` 的既有安装脚本测试，断言 `install-sub-local.sh` 应包含 `ps-agent`，但该脚本是 sub-only，只安装 `ps-sub`。该失败与本次 input host 功能无关。

## 评审结论

Review Agent 未发现阻断问题。原始建议包括：

- 补 JSON input 的 `set-host` 回写用例。
- 补文件级 `external_host` 存在且节点缺失 `server` 时的 `set-host` 行为用例。

上述两个测试缺口已在最终代码中补齐。

## 风险与后续建议

- 当前 `go test ./...` 仍受 `internal/deployment` 的非本次范围断言阻塞，建议后续单独修正 sub-only 安装脚本测试预期。
- 工作区存在若干与本次 host 功能无关的未提交改动，最终交付时应按文件范围区分处理。
