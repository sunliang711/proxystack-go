# T16-T18 交付文档

生成时间：2026-06-17 20:27:21

## 任务背景

本次完成任务 16-18：

- T16：迁移 `ipinfo` 出口 IP 诊断能力。
- T17：补齐 Go 二进制部署脚本、sub 本地部署脚本和 Docker sub 部署资产。
- T18：增加端到端迁移验收测试，验证 Go 版主流程可替代 Python 版核心路径。

## 实现方案

### T16 diagnostics/ipinfo

- 新增 `internal/diagnostics`，实现 mihomo socks listener 查找、curl 参数数组构建、IPv4/IPv6 来源选择、fallback、响应解析和进度输出。
- 新增 `ps-agent ipinfo STACK --family all|ipv4|ipv6 --timeout SECONDS`。
- 单测使用 fake curl runner，不访问真实网络。

### sub export 验收入口

- 新增 `ps-agent sub export [STACK]`，支持默认写入 publish 目录、`--summary` 和 `--dry-run`。
- 新增 `ps-agent sub validate-inputs --input-dir DIR`。
- 新增 `ps-agent sub export-config sub|premium_sub|surge_sub USER`。
- `sub export` 缺少 `external_host` 时 fail fast，不直接写 `sub/inputs`。

### T17 部署脚本与 Docker

- 新增 `scripts/install-agent.sh`、`scripts/install-sub-local.sh`、`scripts/deploy-sub-docker.sh` 和 `scripts/lib/common.sh`。
- 脚本使用 Go binary bootstrap，不再安装 Python venv。
- Dockerfile 只构建 `ps-sub`，镜像不包含 mihomo/xray。
- Docker/compose 默认非 root、read-only、`cap_drop: ALL`、`no-new-privileges:true`，并持久化 `/data`。

### T18 E2E 验收

- 新增 E2E 测试覆盖 `init -> add -> validate -> check -> start -> sub export -> ps-sub import -> ps-sub serve -> HTTP subscription`。
- 使用 fake `systemctl`/`journalctl` 和 fake mihomo/xray，避免依赖真实 systemd、root 和真实代理核心。
- 在启动 `ps-sub serve` 前移动 agent `config.yaml` 和 `stacks/`，验证 sub 服务不读取 agent 配置和 stack。

## 文件变更

- 新增：`internal/diagnostics/ipinfo.go`
- 新增：`internal/cli/agent/diagnostics_commands.go`
- 新增：`internal/cli/agent/sub_commands.go`
- 修改：`internal/cli/agent/root.go`
- 修改：`internal/cli/sub/root.go`
- 新增：`scripts/lib/common.sh`
- 新增：`scripts/install-agent.sh`
- 新增：`scripts/install-sub-local.sh`
- 新增：`scripts/deploy-sub-docker.sh`
- 新增：`Dockerfile.sub`
- 新增：`docker-compose.sub.yml`
- 新增：`docs/deployment.md`
- 新增/修改测试：`internal/diagnostics/ipinfo_test.go`、`internal/cli/agent/diagnostics_commands_test.go`、`internal/deployment/deployment_test.go`、`tests/e2e_test.go`

## 测试结果

- `go test ./...`：通过。
- `shellcheck scripts/lib/common.sh scripts/install-agent.sh scripts/install-sub-local.sh scripts/deploy-sub-docker.sh`：通过。
- `bash -n scripts/lib/common.sh scripts/install-agent.sh scripts/install-sub-local.sh scripts/deploy-sub-docker.sh`：通过。
- dry-run 样例：
  - `scripts/install-agent.sh --dry-run ...`：通过。
  - `scripts/install-sub-local.sh --dry-run ...`：通过。
  - `scripts/deploy-sub-docker.sh --dry-run --build ...`：通过。

## 评审问题与处理

独立 review 第一轮发现：

- 阻断：部署脚本路径保护未拒绝 `/opt` 和 `..`。已修复并补测试。
- 警告：自定义 user/group/bin-dir 与固定 systemd unit 不一致。已在 systemd 模式 fail fast。
- 警告：Docker 文档推荐 `--pull` 但默认镜像是本地标签。已新增 `--build` 并更新文档。
- 警告：compose 前置数据目录和 config 准备不清晰。已更新文档。
- 建议：Docker 缺少 `no-new-privileges`。已补脚本和 compose。

独立 review 复审结论：未发现明确问题。

## 风险与后续建议

- Docker 镜像尚未在真实 Docker 环境 build/run。
- E2E 使用 fake systemd 和 fake mihomo/xray，真实 systemd 单元加载、权限和服务启动仍需手工验收。
- `ipinfo` 单测不访问真实网络，真实 curl 出口建议在部署环境手工执行。
