# proxystack-go 规划进度

生成日期：2026-06-17

## 当前状态

- 当前阶段：T01-T18 已完成实现、测试和独立复审，剩余真实环境手工验收。
- 当前目录：`/Users/eagle/Sync/proxy/proxystack-go`
- 当前产物：规划文档、Go module、T01-T18 源码实现、部署脚本、Docker sub 资产、测试与交付文档。
- 尚未执行：真实 systemd/下载源/root owner 修复、真实 Docker build/run、真实网络 ipinfo 的部署环境手工验收。

## 已完成

- 启动方案 agent，完成当前 Python 项目的只读调研和 Go 重写方案初稿。
- 启动 review agent，完成审查基线和方案复审。
- 合并 review 意见，生成最终方案、任务拆解和 review 报告。
- 补齐命令级、字段级、生成器、测试矩阵、开发顺序、模板兼容、HTTP subserver、安装/systemd 安全、实现手册、编码约定和 ADR 文档。
- 根据复审意见修订模板兼容决策、schema 必填项、CLI flag 兼容和任务输入来源。
- 将总任务拆解为 `docs/tasks/` 下 18 个独立任务文档，每个任务包含目标、范围、输入文档、交付物、实现步骤、验收标准、依赖和风险。
- 已完成 T01-T05：Go module、配置模型、校验矩阵、引用图、Xray 生成器。
- 已完成 T06-T10：mihomo 生成器、订阅 input/index、模板兼容、bundle/backup、sub HTTP 服务。
- 已完成 T11-T15：agent 配置命令、runtime plan/manifest、systemd 生命周期与 unit/metadata、install/update/self update。
- 已完成 T16-T18：diagnostics/ipinfo、Go 部署脚本与 Docker sub、端到端迁移验收测试。

## 文档清单

```text
docs/
  README.md
  go-rewrite-plan.md
  task-breakdown.md
  cli-spec.md
  schema-spec.md
  generator-spec.md
  template-compat-spec.md
  http-subserver-spec.md
  install-systemd-security-spec.md
  deployment.md
  testing-acceptance-matrix.md
  development-sequence-risk.md
  implementation-guide.md
  conventions.md
  adr-0001-compatibility-first.md
  adr-0002-template-compatibility.md
  review-report.md
  PROGRESS.md
  tasks/
    README.md
    task-01-project-bootstrap.md
    task-02-config-model-loading.md
    task-03-config-validation.md
    task-04-reference-graph.md
    task-05-xray-generator.md
    task-06-mihomo-generator.md
    task-07-subscription-generator.md
    task-08-template-compat.md
    task-09-bundle-backup.md
    task-10-sub-http-server.md
    task-11-agent-config-commands.md
    task-12-runtime-plan-manifest.md
    task-13-service-lifecycle.md
    task-14-systemd-units-permissions.md
    task-15-install-update.md
    task-16-diagnostics-ipinfo.md
    task-17-deployment-scripts-docker.md
    task-18-e2e-acceptance.md
```

## 后续入口

建议下一步按 `docs/tasks/task-16-diagnostics-ipinfo.md` 实现 diagnostics/ipinfo，再进入部署脚本与端到端验收。
