# proxystack-go 开发文档索引

生成日期：2026-06-17

本目录是 Go 版 `proxystack` 的开发契约。后续实现应优先以这些文档为准，再回看 Python 源码补细节。

## 文档入口

| 文档 | 用途 |
| --- | --- |
| [go-rewrite-plan.md](go-rewrite-plan.md) | 总体架构、目标边界、模块映射和核心风险 |
| [task-breakdown.md](task-breakdown.md) | 分阶段任务拆解 |
| [tasks/README.md](tasks/README.md) | 可独立领取的任务文档索引 |
| [cli-spec.md](cli-spec.md) | CLI 命令、参数、副作用和验收 |
| [schema-spec.md](schema-spec.md) | 配置与传输 schema 字段级契约 |
| [generator-spec.md](generator-spec.md) | Xray/mihomo/sub 生成器、manifest 和 runtime 规则 |
| [template-compat-spec.md](template-compat-spec.md) | `.j2` 模板兼容决策和验收 |
| [http-subserver-spec.md](http-subserver-spec.md) | `ps-sub serve` HTTP 路由、鉴权和 watcher |
| [install-systemd-security-spec.md](install-systemd-security-spec.md) | 安装更新、systemd、权限和安全规则 |
| [deployment.md](deployment.md) | Go 版本地和 Docker 部署说明 |
| [testing-acceptance-matrix.md](testing-acceptance-matrix.md) | 自动化测试、golden、手工验收矩阵 |
| [development-sequence-risk.md](development-sequence-risk.md) | 推荐开发顺序和风险控制 |
| [implementation-guide.md](implementation-guide.md) | 实现执行手册 |
| [conventions.md](conventions.md) | Go 版编码、包边界和日志约定 |
| [review-report.md](review-report.md) | 初始方案 review 结论 |
| [PROGRESS.md](PROGRESS.md) | 当前规划进度 |

## 开发前必须确认

1. 模板兼容决策已确定：Go 进程内 Jinja2-compatible renderer，首选 `pongo2`。
2. mihomo YAML 是否要求与 Python 版逐字节一致，还是固定 Go 输出格式后做字段级一致。
3. Go 版不保留 Python helper；默认目标是不依赖 Python runtime。

## 开发推荐顺序

1. 阅读 [schema-spec.md](schema-spec.md) 和 [testing-acceptance-matrix.md](testing-acceptance-matrix.md)。
2. 按 [tasks/README.md](tasks/README.md) 选择任务并阅读对应任务文档。
3. 实现 schema 与 validation。
4. 实现 graph 与三类生成器。
5. 实现 runtime plan 和 manifest。
6. 实现 CLI 写配置命令。
7. 实现 systemd、subserver、install 和部署脚本。

## 当前状态

T01-T18 已完成实现、测试和独立复审；真实 systemd、Docker build/run 和真实网络 ipinfo 仍需部署环境手工验收。
