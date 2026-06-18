# 任务索引

生成日期：2026-06-17

本目录把 [task-breakdown.md](../task-breakdown.md) 拆成可独立执行的开发任务。每个任务都只描述目标、范围和验收标准，不包含实现代码。

## 执行顺序

| 任务 | 名称 | 依赖 |
| --- | --- | --- |
| [T01](task-01-project-bootstrap.md) | 项目骨架与命令入口 | 无 |
| [T02](task-02-config-model-loading.md) | 配置模型与加载 | T01 |
| [T03](task-03-config-validation.md) | 配置校验矩阵 | T02 |
| [T04](task-04-reference-graph.md) | 引用图与依赖计划 | T03 |
| [T05](task-05-xray-generator.md) | Xray 生成器 | T04 |
| [T06](task-06-mihomo-generator.md) | mihomo 生成器 | T04 |
| [T07](task-07-subscription-generator.md) | 订阅 input/index 与模板渲染 | T03 |
| [T08](task-08-template-compat.md) | 模板兼容任务 | T07 |
| [T09](task-09-bundle-backup.md) | 订阅 bundle 与原生 backup | T07 |
| [T10](task-10-sub-http-server.md) | sub HTTP 服务 | T08、T09 |
| [T11](task-11-agent-config-commands.md) | agent 配置编辑与模板命令 | T02、T04 |
| [T12](task-12-runtime-plan-manifest.md) | runtime plan、manifest 与只读命令 | T05、T06、T07 |
| [T13](task-13-service-lifecycle.md) | 服务生命周期与 target scope | T12 |
| [T14](task-14-systemd-units-permissions.md) | systemd unit 与权限 | T13 |
| [T15](task-15-install-update.md) | install/update 与自更新 | T02 |
| [T16](task-16-diagnostics-ipinfo.md) | diagnostics/ipinfo | T04 |
| [T17](task-17-deployment-scripts-docker.md) | 部署脚本与 Docker sub | T10、T15 |
| [T18](task-18-e2e-acceptance.md) | 端到端迁移验收 | 全部任务 |

## 开工建议

优先完成 T01-T04，冻结模型、校验和引用图后再进入生成器和 CLI 写操作。实现阶段每完成一个任务，应更新 [PROGRESS.md](../PROGRESS.md)。
