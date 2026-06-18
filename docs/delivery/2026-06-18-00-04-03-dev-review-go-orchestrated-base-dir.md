# ps-agent base-dir 路径模型简化交付文档

## 任务背景

用户希望不再同时维护 `-c/--config` 和配置文件内 `base_dir` 两套路径入口，改为由 `--base-dir` 唯一决定 agent 环境目录。

## 多 Agent 编排

- T1 实现 agent：完成 `ps-agent` CLI、配置加载、native backup、测试、脚本和文档修改。
- T2 review agent：独立审查当前未提交变更，重点检查旧 `--config/base_dir` 残留、路径一致性、native backup 和测试缺口。
- 主控 agent：集成实现结果，处理 review 建议，补充回归测试并完成最终验证。

## 实现方案

- `ps-agent` 根命令移除全局 `-c/--config`，新增全局 `--base-dir`，默认 `/opt/proxystack`。
- agent 配置路径固定为 `<base-dir>/config.yaml`。
- `config.yaml` 不再生成或解析 `base_dir` 字段。
- `domain.GlobalConfig.BaseDir` 保留为运行时字段，`yaml:"-"`，由配置加载过程按配置文件目录注入。
- `init`、`setup`、`install`、`service`、生命周期、订阅导出、诊断和原生备份命令统一通过 `--base-dir` 推导配置路径。
- native backup 导出和恢复会剥离旧版 `base_dir` 字段，恢复目标由当前 `--base-dir` 决定。
- 后续变更已将 `ps-sub` 也统一到 `--base-dir` 路径模型。

## 文件与配置变更

- CLI 与配置模型：`internal/cli/agent/*`、`internal/agentconfig/commands.go`、`internal/config/loader.go`、`internal/domain/models.go`
- 备份恢复：`internal/generator/backup/config.go`
- 测试与 fixtures：`internal/cli/agent/*_test.go`、`internal/agentconfig/commands_test.go`、`internal/config/loader_test.go`、`internal/generator/backup/config_test.go`、`internal/systemd/runner_test.go`、`tests/e2e_test.go`、`tests/fixtures/example-project/config.yaml`
- 脚本与文档：`scripts/install-agent.sh`、`scripts/install-sub-local.sh`、`docs/cli-spec.md`、`docs/schema-spec.md`、`docs/deployment.md`、`docs/testing-acceptance-matrix.md`、`docs/tasks/task-18-e2e-acceptance.md`

## 测试结果

- 通过：`go test ./internal/cli/agent ./internal/generator/backup ./internal/systemd`
- 通过：`go test ./internal/agentconfig ./internal/config ./internal/runtime ./tests`
- 通过：`go test ./...`
- 通过：`git diff --check`
- 手工验证：`go run ./cmd/ps-agent --help`
- 手工验证：`go run ./cmd/ps-agent --base-dir <tmp> init --external-host proxy.example.com`

## 评审结论

Review agent 未发现阻断问题。建议补充的 legacy native backup、custom `--base-dir` 聚合路径、旧 `--config` 负例测试已由主控 agent 补齐并验证通过。

## 风险与后续建议

- 这是破坏性 CLI 变更，外部脚本和自动化需要从 `--config` 迁移到 `--base-dir`。
- 当前保留对旧 native backup 中 `base_dir` 字段的剥离能力，用于恢复历史备份，但新导出的备份不再包含该字段。
