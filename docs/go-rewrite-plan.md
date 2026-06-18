# proxystack Go 重写方案

生成日期：2026-06-17

目标目录：`/Users/eagle/Sync/proxy/proxystack-go`

本方案最初用于 Go 重写的设计和任务拆解。当前仓库已经包含 Go module、源码实现和测试，本文件保留为架构背景与目标边界说明。

## 1. 需求理解

本次目标是把当前 Python 项目 `proxystack` 用 Go 在 `../proxystack-go` 中重新实现一遍。当前仓库已按该目标完成 Go module、源码骨架、T01-T18 实现和测试。

已确认边界：

- 业务目标：保持现有 agent 与 sub 能力，用 Go 重写为可替代版本。
- 影响范围：目标项目为 `/Users/eagle/Sync/proxy/proxystack-go`。
- 接口形态：CLI 为主，订阅服务 HTTP 为辅；不新增管理 Web UI 或管理 HTTP API。
- 关键规则：配置格式、生成结果、订阅包、备份包、服务管理文件、运行目录边界尽量兼容现有 Python 版。

## 2. 当前项目能力盘点

当前项目是“配置编译器 + 本地 agent + 订阅 HTTP 服务”的组合。

核心输入：

- `/opt/proxystack/config.yaml`
- `/opt/proxystack/stacks/*.yaml`
- `/opt/proxystack/sub/config.yaml`
- `/opt/proxystack/sub/inputs/*.yaml|*.yml|*.json`

核心能力：

| 模块 | 当前能力 |
| --- | --- |
| 配置模型 | 全局配置、stack 配置、订阅服务配置、订阅 input、订阅 bundle、原生 backup 均有 schema 与校验 |
| 校验 | stack 名称与文件名一致、端口范围、端口全局唯一、系统端口占用、ref 存在性、依赖环、公开 socks/http 鉴权、vmess/SS/SS2022 凭据 |
| 引用图 | 支持 `xrelay outbound -> clash socks listener`，以及 `clash xrelay-socks5 upstream -> xrelay inbound`，并生成服务依赖顺序 |
| Xray 生成器 | 稳定输出 JSON，支持 vmess、shadowsocks、socks5、http inbound；支持 clash/socks5/http/direct outbound；支持 api/stats/policy |
| mihomo 生成器 | 稳定输出 YAML，支持 socks/http listener、raw upstream、xrelay-socks5 upstream、select/url-test/load-balance/fallback、默认 rules profile |
| 订阅生成器 | 从 `xrelay.inbounds[].sub == true` 生成 subscription input/index，不读取 clash 内部配置 |
| 订阅格式 | Clash、Premium Clash、Surge 三类输出；Surge 支持 `#!MANAGED-CONFIG` |
| 订阅服务 | 支持 `/health`、`/sub`、`/premium_sub`、`/surge_sub`，token query/path 鉴权，启动加载 inputs，运行期 watcher reload |
| CLI | `ps-agent` 与 `ps-sub`，覆盖 init/setup/add/config/list/remove/clone/member/check/start/restart/status/logs/doctor/install/update/export/import/sub export/sub validate-inputs/service 等 |
| 服务管理 | Linux 生成并安装 `proxystack-xray@.service`、`proxystack-clash@.service`、`proxystack-sub.service`；macOS 生成 launchd plist |
| 安装更新 | mihomo、xray、geo 下载/校验/原子替换；托管源 GitHub/R2 fallback；self update；远端 URL SSRF 防护 |
| 备份发布 | 原生 agent backup 与订阅 bundle 分离；zip manifest/hash/path 安全校验 |
| 测试 | golden tests 覆盖 Xray/mihomo/subscription；CLI、systemd、install、subserver、fixtures、部署脚本均有测试 |

## 3. Go 版目标边界

Go 版需要保持以下兼容契约：

- 保持现有用户配置格式：`config.yaml`、`stacks/*.yaml`、`sub/config.yaml`、订阅 input、订阅 bundle、原生 backup。
- 保持现有命令入口：`ps-agent`、`ps-sub`。
- 保持 agent/sub 数据边界：`ps-sub` 不读取 `config.yaml`、`stacks/`、`runtime/`。
- 保持订阅边界：订阅只来自 `xrelay.inbounds[]` 中 `sub: true` 的节点，不把 clash upstream、groups、rules、controller 写入订阅。
- 保持生成边界：`start` 写 runtime/generated 和 manifest，但不隐式生成订阅发布包；`sub export` 才生成发布包。
- 保持安全边界：下载、归档、服务管理、日志、token、目录权限和安装更新行为不能弱化。

不在首期范围内：

- 不实现代理协议核心，不替代 Xray/mihomo。
- 不引入数据库，GORM 不适用。
- 不做 Web UI、管理 HTTP API；除已实现的 launchd 后端外，不扩展其他服务管理平台适配。
- 不在首期实现 mihomo REST API 代理组切换。
- 不自动导入旧 `clash`、`xrelay`、`clashsub` 或旧 `proxy-stack` 目录。
- 不在 shell/bootstrap 脚本里下载 mihomo/xray/geo，仍由 agent 命令管理。

## 4. 推荐技术栈

默认遵循 Go 后端规则中的技术栈，但本项目是文件型 CLI 项目，落地时需要控制复杂度。

| 场景 | 推荐 |
| --- | --- |
| CLI | `spf13/cobra` + `pflag` |
| HTTP 订阅服务 | Gin |
| 日志 | Zerolog，日志消息使用英文，敏感字段脱敏 |
| 配置解码 | `gopkg.in/yaml.v3` 解码到强类型 struct |
| 配置管理 | 当前实现直接使用 Cobra flag 与 YAML loader，不引入 Viper |
| 校验 | 手写校验和聚合错误为主，覆盖跨字段、跨 stack 规则 |
| 依赖组织 | 当前实现优先显式构造对象，不引入 Fx |
| JSON | 标准库 `encoding/json`，必要时用稳定结构保证字段顺序 |
| YAML 输出 | `yaml.v3` + 必要的 ordered writer，保证生成文件稳定 |
| 模板 | 首期采用 Go 进程内 Jinja2-compatible renderer，首选 `github.com/flosch/pongo2/v6`，保留现有 `.j2` 覆盖模板能力 |
| 文件监控 | `fsnotify`，保留 polling fallback |
| 测试 | 标准 `testing` + `testify/require/assert` |
| 压缩包 | 标准库 `archive/zip`、`archive/tar`、`compress/gzip` |
| 下载 | 标准库 `net/http`，保留 DNS 私网拒绝、重定向控制、sha256 校验 |

模板兼容决策：首期使用 Go 进程内 Jinja2-compatible renderer，首选 `github.com/flosch/pongo2/v6`，不引入 Python runtime，不直接切换到 Go `text/template`。默认模板和使用公开上下文/`yaml_block` filter 的用户 `.j2` 覆盖模板必须兼容；不支持语法必须明确报错。

## 5. 当前目录结构

当前实现主体结构如下：

```text
proxystack-go/
  cmd/
    ps-agent/
      main.go
    ps-sub/
      main.go
  internal/
    agentconfig/
      templates/
    cli/
      agent/
      sub/
    config/
    domain/
    graph/
    generator/
      xray/
      mihomo/
      sub/
      backup/
    service/
    subserver/
    systemd/
    install/
    diagnostics/
    runtime/
    testutil/
  templates/
    embed.go
    sub/
      clash.yaml.j2
      premium-clash.yaml.j2
      surge.conf.j2
  tests/
    fixtures/
    golden/
  scripts/
  docs/
```

## 6. Python 到 Go 模块映射

| Python 模块 | Go 包建议 |
| --- | --- |
| `proxystack.domain.models` | `internal/domain` |
| `proxystack.domain.validation` | `internal/domain/validation` |
| `proxystack.config.loader` | `internal/config` |
| `proxystack.graph.references` | `internal/graph` |
| `proxystack.graph.dependencies` | `internal/graph` |
| `proxystack.generator.xray` | `internal/generator/xray` |
| `proxystack.generator.mihomo` | `internal/generator/mihomo` |
| `proxystack.generator.sub` | `internal/generator/sub` |
| `proxystack.generator.backup` | `internal/generator/backup` |
| `proxystack.subserver.config` | `internal/config` |
| `proxystack.subserver.state` | `internal/subserver` |
| `proxystack.subserver.watcher` | `internal/subserver` |
| `proxystack.subserver.app` | `internal/subserver` |
| `proxystack.cli.agent` | `internal/cli/agent` |
| `proxystack.cli.sub` | `internal/cli/sub` |
| `proxystack.cli.lifecycle` | `internal/runtime` + `internal/cli/agent` + `internal/service` |
| `proxystack.systemd.service` | `internal/systemd` + `internal/service` |
| `proxystack.install.service` | `internal/install` |
| `proxystack.diagnostics.ipinfo` | `internal/diagnostics` |
| `proxystack.logging` | `internal/cli/runtime` 与 `internal/cli/sub` |
| `src/proxystack/templates` | `internal/agentconfig/templates` 与 `templates/`，通过 `embed` 打包 |

## 7. 输出兼容标准

必须逐字节一致或明确证明逐字段一致后固定输出格式：

- Xray JSON。
- mihomo YAML。
- subscription input YAML。
- subscription index JSON。
- Clash/Premium Clash/Surge 订阅文本。
- bundle manifest。
- native backup manifest。
- systemd unit 文件。
- runtime manifest。

可只做语义兼容：

- CLI help 文案。
- 进度输出和错误摘要中的非关键措辞。
- 日志格式中的字段顺序，但字段含义、敏感信息脱敏和错误级别必须一致。

## 8. Schema 严格度矩阵

| 模型类别 | 策略 | 说明 |
| --- | --- | --- |
| 领域配置模型，如 `GlobalConfig`、`Stack`、`Inbound`、`ClashConfig` | 允许保留未知字段 | 对齐当前 Pydantic `extra="allow"`，避免破坏用户扩展字段 |
| 订阅服务配置 `SubServerConfig`、`ManagedConfig` | 严格禁止未知字段 | 避免服务配置误写后静默生效失败 |
| 订阅 input、subscription access、subscription index | 严格禁止未知字段 | 这是 agent/sub 之间的契约格式 |
| bundle manifest | 严格禁止未知字段 | 关系到导入安全和 hash 校验 |
| native backup manifest | 严格禁止未知字段 | 避免和订阅 bundle schema 混用 |
| install request / 下载源解析 | 严格禁止未知字段 | 避免错误 source 或安全策略被忽略 |

## 9. 关键风险

| 风险 | 影响 | 应对 |
| --- | --- | --- |
| 模板语法不兼容 | 用户自定义订阅模板失效 | 首期使用 Go 进程内 Jinja2-compatible renderer，默认模板和公开模板子集必须通过验收 |
| YAML 输出顺序不稳定 | manifest 误判变更，导致频繁重启 | golden tests + 多次生成 hash 不变 |
| Pydantic 隐含校验遗漏 | 配置兼容性和安全规则回退 | 将 Python 负面用例迁移为 Go table tests |
| CLI 副作用边界混乱 | `check/render` 意外写文件或操作服务 | runtime plan 和 apply 分离，所有只读命令有落盘检测 |
| install/update 安全弱化 | SSRF、路径穿越或损坏已安装文件 | 保留 DNS 私网拒绝、sha256、归档成员校验、失败回滚 |
| sub 服务读取越界 | 泄露 stack/clash 内部配置 | sub app 只注入 data_dir 与 sub config，不接触 agent config |
| `ipinfo` 误设计为 mihomo REST API | 行为偏离现状 | 保持通过 mihomo socks listener + curl 查询 |
