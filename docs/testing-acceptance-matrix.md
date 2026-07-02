# 测试与验收矩阵

生成日期：2026-06-17

本文定义 Go 版完整开发时必须迁移的测试范围、golden 对照策略和手工验收项。

## 1. 测试原则

- 优先迁移现有 Python fixtures 和 golden，而不是重新设计样例。
- 生成类输出优先逐字节一致；无法逐字节一致时，必须先固定 Go 输出格式，再做字段级一致。
- 外部系统必须 fake：systemd、下载器、curl、端口检测、时钟、文件 watcher。
- 默认测试不得要求 root、真实 systemd、真实网络、真实 mihomo/xray。
- 所有安全负面用例必须覆盖：SSRF、路径穿越、hash mismatch、schema 混用、公开 noauth、SS2022 key 错误。

## 2. Golden 对照范围

| 输出 | 对照文件 | 验收方式 |
| --- | --- | --- |
| Xray JSON | `tests/golden/xray/*.json` | 逐字节一致 |
| mihomo YAML | `tests/golden/mihomo/*.yaml` | 逐字节一致；如 yaml.v3 格式不同，必须固定 Go 格式并做字段级一致测试 |
| subscription input | `tests/golden/sub/input.yaml` | 逐字节一致 |
| subscription index | `tests/golden/sub/index.json` | 逐字节一致 |
| Clash 订阅 | `tests/golden/sub/clash.yaml` | 逐字节一致 |
| Premium Clash 订阅 | `tests/golden/sub/premium-clash.yaml` | 逐字节一致 |
| Surge 订阅 | `tests/golden/sub/surge.txt` | 逐字节一致 |
| systemd unit | `internal/systemd` 测试期望文本 | 逐字节一致 |
| launchd plist | `internal/service` 测试期望文本 | 逐字节一致 |
| bundle manifest | 从 Python 测试样例抽取 | 字段级一致，时间可注入固定值 |
| native backup manifest | 从 Python 测试样例抽取 | 字段级一致，时间可注入固定值 |

## 3. Fixtures

必须复制或等价使用：

```text
tests/fixtures/example-project/config.yaml
tests/fixtures/example-project/stacks/usa1.yaml
tests/fixtures/example-project/stacks/usa2.yaml
tests/fixtures/example-project/stacks/auto.yaml
tests/fixtures/sub/manual.yaml
```

测试中所有 agent 文件路径应通过临时目录传入 `--base-dir`，不得写 `/opt/proxystack`。

## 4. 包级测试矩阵

| Go 包 | 对应 Python 测试 | 必测内容 |
| --- | --- | --- |
| `internal/config` | `test_config_loader.py`、`test_task11_config_matrix.py` | 加载默认值、路径解析、stack 文件名与 name、schema strict/allow |
| `internal/domain/validation` | `domain/validation.py` 相关用例 | 端口唯一、公开 noauth、系统端口 fake、日志级别、规则目标 |
| `internal/graph` | `test_config_loader.py`、`test_mihomo_generator.py` | ref 解析、endpoint index、循环依赖、disabled 过滤、内部地址归一化 |
| `internal/generator/xray` | `test_xray_generator.py` | golden、api/stats/policy、vmess 多用户、SS2022、outbound |
| `internal/generator/mihomo` | `test_mihomo_generator.py` | golden、listeners、raw/xrelay-socks5 upstream、groups/rules、mixed 拒绝 |
| `internal/generator/sub` | `test_sub_generator.py`、`test_subscription_golden.py` | input/index、模板渲染、重复节点、bundle 安全、Surge 地区组 |
| `internal/subserver` | `test_subserver.py` | `/health`、订阅路由、token、503/404、reload 保留旧索引、watcher |
| `internal/cli/agent` | `test_cli.py`、`test_task11_cli_matrix.py` | 命令参数、target scope、只读命令不落盘、错误摘要 |
| `internal/runtime` | `test_task11_main_flow.py` | runtime plan、manifest diff、generated_at 复用、start/restart apply |
| `internal/systemd` | `test_systemd.py` | fake runner、unit 内容、journalctl 多 unit、inactive status |
| `internal/service` | systemd/launchd 服务管理扩展用例 | service-manager auto 选择、launchd plist、launchctl/log 调用、stale plist 清理 |
| `internal/install` | `test_install.py` | install/update/all/self、托管源 fallback、sha256、归档、回滚、SSRF |
| `internal/diagnostics` | `test_ipinfo.py` | curl 参数、IPv4/IPv6 来源、fallback、进度输出 |
| `scripts` / Docker | `test_task12_deployment_scripts.py`、`test_task11_docker_deployment.py` | bootstrap、sub Docker 安全参数、只做 sub 服务 |

## 5. CLI 副作用测试

每个命令至少有一个副作用边界测试。

| 命令 | 不允许发生的事 |
| --- | --- |
| `validate` | 写任何文件、调用服务管理器 |
| `check` | 写 `runtime/generated`、写 manifest、调用服务管理器 |
| `render *` | 写 runtime、调用服务管理器 |
| `list` | 写文件；默认不做系统端口检测 |
| `doctor` | 写文件、修复权限 |
| `pssub start` | 读取 agent config 或 stack、创建 generated、写 agent manifest |
| `sub export --summary` | 写 zip |
| `pssub serve` | 读取 agent config 或 stack |
| `pssub import` | 接受 native backup |
| `psctl import` | 接受 subscription bundle |

## 6. 安全负面测试

必须覆盖：

- HTTP/HTTPS 下载解析到 `127.0.0.1`、私网、link-local、metadata 地址时拒绝。
- 下载重定向到私网地址时拒绝。
- 普通远端 URL 缺 sha256 时拒绝。
- sha256 mismatch 不写目标。
- zip/tar 成员包含绝对路径、`..`、反斜杠时拒绝。
- bundle manifest hash mismatch 不写 input。
- bundle schema 和 native backup schema 混用拒绝。
- sub reload 时新 input 非法，旧 index 保留。
- `access.type=token` 时无 token 返回 401，错误 token 返回 403。
- 非回环 socks/http noauth 默认失败。
- SS2022 base64 key 长度错误失败。

## 7. 手工验收矩阵

手工验收只在实现完成后执行，不作为日常单测前置。

1. 本机临时目录完整流程：

```bash
psctl --base-dir ./tmp init
psctl --base-dir ./tmp add usa1 --no-edit
psctl --base-dir ./tmp validate
psctl --base-dir ./tmp check
```

2. fake 二进制 + fake systemd 完整流程：

```bash
psctl --base-dir ./tmp start
psctl --base-dir ./tmp status
psctl --base-dir ./tmp sub export
pssub --base-dir ./tmp import ./tmp/publish/sub-bundle.zip
pssub --base-dir ./tmp serve
```

3. HTTP 订阅请求：

```bash
curl http://127.0.0.1:3003/health
curl http://127.0.0.1:3003/sub/<token>/alice
curl http://127.0.0.1:3003/premium_sub/<token>/alice
curl http://127.0.0.1:3003/surge_sub/<token>/alice
```

4. Linux systemd 环境：

```bash
sudo psctl service install
sudo psctl start usa1
sudo psctl status usa1
sudo psctl logs usa1 --follow
```

5. macOS launchd 环境：

```bash
sudo psctl --service-manager launchd service install
sudo psctl --service-manager launchd start usa1
sudo psctl --service-manager launchd status usa1
sudo psctl --service-manager launchd logs usa1 --follow
```

## 8. 覆盖率要求

- 生成器、schema、bundle、install 安全逻辑必须有表驱动单测。
- CLI 只要求关键路径和副作用边界，不追求 help 文案逐字节一致。
- watcher 需要覆盖 fsnotify 触发和 polling fallback；平台差异可用接口 fake。
- 端到端测试至少覆盖一次 `init -> add -> validate -> check -> start -> sub export -> pssub import -> pssub serve -> HTTP subscription`。
