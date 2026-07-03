# setup 命令拆分交付记录

## 任务背景

- `psctl` 与 `pssub` 是两个独立 binary，不存在 `psctl pssub setup`。
- `psctl` 需要提供 `setup local`、`setup deps`、`setup all` 和默认 `setup`。
- `pssub` 需要提供 `setup local`、`setup all` 和默认 `setup`。
- 旧顶层 `init`、`install` 命令需要移除。
- setup 命令必须幂等，service 文件只安装，不自动启动或 enable。

## 多 Agent 分工

- 开发 agent：实现 CLI 命令拆分、移除旧入口并更新核心测试。
- review agent：独立审查命令语义、脚本组合、文档一致性和测试缺口。
- 主 Agent：集成开发结果，补齐脚本/文档/测试，处理 review 反馈并完成最终验证。

## 实现摘要

- `psctl setup` 默认等价于 `psctl setup all`。
- `psctl setup all` 固定执行 `local -> deps`。
- `psctl setup local` 执行本地目录/配置初始化、metadata 修复和 agent stack service 文件安装，不下载依赖，不启动服务。
- `psctl setup deps` 通过现有 installer 安装 `xray`、`mihomo`、`geo`，目标固定为 `all`，不安装 service 文件。
- `pssub setup` 默认等价于 `pssub setup all`。
- `pssub setup all` 当前等价于 `pssub setup local`。
- `pssub setup local` 初始化 sub 目录/配置并安装 pssub service 文件，不启动服务。
- 移除了 `psctl init`、`psctl install`、`pssub init` 顶层入口。
- 同步更新 README、CLI spec、部署文档、任务文档和安装脚本。

## 文件变更

- Go CLI：`internal/cli/agent/*`、`internal/cli/sub/*`
- 脚本：`scripts/install-agent.sh`、`scripts/install-sub-local.sh`
- 文档：`README.md`、`docs/cli-spec.md`、`docs/deployment.md` 及相关任务/验收文档
- 测试：CLI、deployment、E2E 相关测试

## 测试结果

- `go test ./internal/cli/agent ./internal/cli/sub ./internal/deployment ./tests` 通过。
- `bash -n scripts/install-agent.sh scripts/install-sub-local.sh` 通过。
- `go test ./...` 未全量通过，失败点为 `internal/generator/sub TestRenderSubscriptionsMatchGolden` 的 Surge golden 差异，未触及相关生成器、模板或 golden 文件。

## Review 结论

- 开发后独立 review 提出 1 个脚本组合警告和 2 个文档建议。
- 已补充脚本 dry-run 测试并更新文档。
- 复审结果：未发现阻断或警告问题。

## 风险与后续

- `pssub setup local` 现在会安装 service 文件，部署文档已明确需要管理员权限。
- `tests/e2e_test.go` 当前直接调用 `agentconfig.InitProject` 准备初始配置，后续如需要更完整覆盖 CLI `setup local`，可单独引入可注入 fake service manager 的 E2E 流程。
