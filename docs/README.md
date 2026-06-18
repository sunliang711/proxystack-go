# proxystack-go 开发文档索引

生成日期：2026-06-17

本目录是 Go 版 `proxystack` 的实现契约、历史规划和维护说明。当前代码已经完成 T01-T18，维护时应同时对照本文档、`internal/` 实现和 `tests/` 验收用例。

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
| [install-systemd-security-spec.md](install-systemd-security-spec.md) | 安装更新、服务管理、权限和安全规则 |
| [deployment.md](deployment.md) | Go 版本地和 Docker 部署说明 |
| [testing-acceptance-matrix.md](testing-acceptance-matrix.md) | 自动化测试、golden、手工验收矩阵 |
| [development-sequence-risk.md](development-sequence-risk.md) | 推荐开发顺序和风险控制 |
| [implementation-guide.md](implementation-guide.md) | 实现执行手册 |
| [conventions.md](conventions.md) | Go 版编码、包边界和日志约定 |
| [review-report.md](review-report.md) | 初始方案 review 结论 |
| [PROGRESS.md](PROGRESS.md) | 当前规划进度 |

## 当前实现确认

1. 模板兼容决策已落地：Go 进程内 Jinja2-compatible renderer，使用 `github.com/flosch/pongo2/v6`。
2. 生成输出以 `tests/golden/` 和字段级测试为准；无用户确认时不得自动改 golden。
3. Go 版默认运行不依赖 Python runtime；`update self` 是显式自更新路径，会调用 `python3 -m pip install --upgrade`。
4. 服务管理器支持 `auto|systemd|launchd`，`auto` 在 Linux 使用 systemd，在 macOS 使用 launchd。

## 维护推荐顺序

1. 阅读相关规格文档和对应 `internal/` 包实现。
2. 对照 `tests/golden/`、单元测试和端到端测试确认现有行为。
3. 修改行为时同步更新规格文档、部署说明和测试。
4. 涉及服务管理、下载、归档、权限或 token 的改动必须补充安全负面用例。

## 当前状态

T01-T18 已完成实现、测试和独立复审；真实 systemd/launchd、Docker build/run、真实下载源和真实网络 ipinfo 仍需部署环境手工验收。
