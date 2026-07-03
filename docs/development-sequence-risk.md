# 开发顺序与风险控制

生成日期：2026-06-17

本文用于指导 Go 版实际开发顺序，避免先写 CLI 或 systemd 导致核心契约散落。

## 1. 核心原则

- 先冻结 schema，再写命令。
- 先完成只读编译器，再完成会落盘的 runtime apply。
- 先完成 fake runner，再接真实 systemd。
- 先跑通 golden，再做安装脚本和 Docker。
- 所有写操作必须建立在只读 plan 的结果之上。

## 2. 推荐阶段

### P1 文档契约冻结

输入：

- `go-rewrite-plan.md`
- `schema-spec.md`
- `generator-spec.md`
- `cli-spec.md`
- `testing-acceptance-matrix.md`

产出：

- 确认模板兼容策略。
- 确认逐字节一致范围。
- 确认 Go 依赖清单。

风险：

- 如果模板兼容未拍板，订阅服务实现会反复返工。

### P2 Schema + Validation

范围：

- `internal/domain`
- `internal/config`
- `internal/domain/validation`

只允许实现：

- 强类型 struct。
- YAML/JSON decode。
- 默认值补齐。
- 校验聚合。

禁止：

- 写 CLI 写操作。
- 写 systemd。
- 写下载器。

验收：

- fixtures 可加载。
- 负面校验矩阵通过。

### P3 Reference Graph + Read-only Compiler

范围：

- `internal/graph`
- `internal/generator/xray`
- `internal/generator/mihomo`
- `internal/generator/sub`

只允许实现：

- ref 解析。
- 依赖图。
- 生成器。
- `render` 命令可作为薄 wrapper。

禁止：

- 写 runtime。
- 调 systemd。

验收：

- Xray/mihomo/sub golden 通过。
- 多次生成 hash 不变。

### P4 Runtime Plan + Manifest

范围：

- `internal/runtime`
- `internal/cli/agent` 的 `check`

只允许实现：

- runtime plan。
- manifest read/diff/write 逻辑。
- 原子文件写入工具。

禁止：

- 真实 systemd。

验收：

- `check` 不落盘。
- `start` 可在 fake runner 下写 runtime。
- generated_at 复用策略固定。

### P5 CLI 写配置命令

范围：

- `setup local`
- `add`
- `config`
- `clone`
- `member`
- `remove`

前置：

- Schema 和 validation 已稳定。
- 端口分配和 YAML round-trip 方案已确定。

风险：

- `add/clone/member` 很容易破坏 YAML 注释和用户可读性；首期可接受固定格式输出，但必须稳定。

### P6 Systemd + Service Lifecycle

范围：

- `internal/systemd`
- `start/stop/restart/status/logs/enable/disable`
- `service *`

前置：

- runtime plan 已完成。
- fake runner 已完成。

验收：

- fake runner 单测全覆盖。
- 真实 systemd 只作为手工验收。

### P7 Subscription Server

范围：

- `internal/subserver`
- `pssub config/import/clear/serve`

前置：

- subscription input/index/bundle 已完成。
- 模板兼容策略已落定。

风险：

- 不能读取 agent config。
- reload 失败必须保留旧索引。

### P8 Install/Update/Diagnostics

范围：

- `internal/install`
- `internal/diagnostics`

前置：

- config install schema 已完成。

风险：

- 下载安全最容易被简化，必须先写负面测试。

### P9 Scripts + Docker

范围：

- `scripts`
- Dockerfile
- compose

前置：

- 二进制入口稳定。
- sub serve 稳定。

验收：

- Shell 只 bootstrap。
- Docker sub 不包含 xray/mihomo。

## 3. 高风险依赖关系

| 风险 | 错误顺序 | 正确顺序 |
| --- | --- | --- |
| CLI 写操作反复返工 | 先写 `add/clone`，后补 schema | 先冻结 schema/validation，再写配置命令 |
| `start` 行为不稳定 | 先调用 systemd，后补 manifest | 先 runtime plan + fake runner，再接 systemd |
| 生成文件频繁重启 | 先实现 apply，后补 hash | 先 golden/hash 稳定性测试 |
| sub 服务越界读取 | 复用 agent config loader | 单独实现 sub config loader |
| install 安全回退 | 先跑通下载 happy path | 先写 SSRF/path/hash 负面测试 |

## 4. 开发分支建议

每个阶段使用独立短分支或 PR：

1. `codex/go-docs-contract`
2. `codex/go-schema-validation`
3. `codex/go-generators`
4. `codex/go-runtime-plan`
5. `codex/go-cli-config`
6. `codex/go-systemd-lifecycle`
7. `codex/go-subserver`
8. `codex/go-install-diagnostics`
9. `codex/go-deployment`

合并前必须满足：

- 当前阶段测试通过。
- 不引入下一阶段副作用。
- review 阻断项清零。

## 5. 每阶段交付格式

每个阶段结束时更新：

- `docs/PROGRESS.md`
- 对应任务文档状态。
- 测试结果。
- 未完成风险。

交付摘要至少包含：

- 完成内容。
- 修改文件。
- 测试命令。
- 与 Python 行为差异。
- 后续阻塞。
