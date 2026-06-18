# ps-sub base-dir 路径模型交付文档

## 任务背景

用户要求 `ps-sub` 也使用 `--base-dir` 路径模型，不再通过 `--config` 或 `--data-dir` 指定订阅服务路径，同时保留独立 sub-only 部署能力。

## 编排方案

- T1 实现 agent：修改 `ps-sub` CLI、sub config 运行模型、systemd、Docker/sub-only 部署脚本、测试和文档。
- T2 review agent：独立审查 `ps-sub --base-dir` 相关改动，关注旧路径入口残留、运行时 DataDir 注入、systemd/Docker 路径一致性和测试缺口。
- 主控 agent：复核实现、补充部署脚本 dry-run 测试、执行最终验证并整理交付。

## 实现方案

- `ps-sub` 移除全局 `--config` 和 `--data-dir`。
- `ps-sub` 新增全局 `--base-dir`，默认 `/opt/proxystack`。
- sub root 固定为 `<base-dir>/sub`。
- sub config 固定为 `<base-dir>/sub/config.yaml`。
- inputs 固定为 `<base-dir>/sub/inputs`。
- `SubServerConfig.DataDir` 保留为运行时字段，不再从 YAML 读取或输出；YAML 中出现 `data_dir` 会 strict reject。
- `ps-agent init` 生成的 `sub/config.yaml` 不再包含 `data_dir`。
- systemd sub unit 改为 `ps-sub --base-dir <base-dir> serve`。
- Docker sub-only 部署改为挂载 host base dir 到 `/data`，容器内执行 `ps-sub --base-dir /data serve`。

## 文件与配置变更

- CLI 与配置模型：`internal/cli/sub/root.go`、`internal/config/transport.go`
- agent 初始化与 systemd：`internal/agentconfig/commands.go`、`internal/systemd/runner.go`
- 部署入口：`scripts/install-sub-local.sh`、`scripts/deploy-sub-docker.sh`、`Dockerfile.sub`、`docker-compose.sub.yml`
- 测试：`internal/cli/sub/root_test.go`、`internal/config/loader_test.go`、`internal/systemd/runner_test.go`、`internal/deployment/deployment_test.go`、`tests/e2e_test.go`
- 文档：`docs/cli-spec.md`、`docs/http-subserver-spec.md`、`docs/deployment.md`、`docs/schema-spec.md`、`docs/testing-acceptance-matrix.md`、`docs/install-systemd-security-spec.md`

## 测试结果

- 通过：`go test ./internal/cli/sub ./internal/config ./internal/subserver ./internal/systemd ./tests`
- 通过：`go test ./internal/deployment`
- 通过：`go test ./...`
- 通过：`git diff --check`
- 手工验证：`go run ./cmd/ps-sub --help`
- 手工验证：`go run ./cmd/ps-sub --base-dir <tmp> config`

## 评审问题清单与处理

- 阻断问题：无。
- 建议补充 Docker sub-only dry-run 路径测试：已补充 `TestDockerSubDeployDryRunUsesBaseDir`，验证 host base dir 映射到 `/data`，容器内通过 `/data/sub` 工作。
- 历史 delivery 文档中仍保留旧 `ps-sub --config/--data-dir` 描述：判定为历史记录，不作为当前用户手册；当前规格和部署文档已更新。

## 风险与后续建议

- 这是破坏性变更，旧 `ps-sub --config`、`ps-sub --data-dir` 和 `sub/config.yaml` 中的 `data_dir` 都不再可用。
- 现有部署升级前需要删除 `sub/config.yaml` 中的 `data_dir` 字段，并重新执行 `ps-agent --base-dir <dir> service install sub` 写入新版 systemd unit。
- 独立 sub-only 部署继续支持，推荐形态为 `ps-sub --base-dir /data/sub-only serve`，实际运行目录为 `/data/sub-only/sub`。
