# proxystack Go 重写任务分解

生成日期：2026-06-17

本文件只描述后续实现任务，不包含实现代码。

独立任务执行文档见 [tasks/README.md](tasks/README.md)，每个任务文件包含目标、范围、输入文档、交付物、实现步骤、验收标准、依赖和风险。

## 总体阶段

1. M1：Go 项目骨架、配置模型、引用图完成。
2. M2：Xray/mihomo/sub 三类生成器 golden 通过。
3. M3：agent CLI 生命周期、manifest、fake systemd runner 通过。
4. M4：sub HTTP 服务、watcher、bundle/import/export 通过。
5. M5：install/update、backup、ipinfo、部署脚本完成。
6. M6：用当前 Python fixtures/golden 做端到端对照，确认 Go 版可替代。

## T01 项目骨架与命令入口

- 目标：创建 Go module、两个二进制入口和基础命令树。
- 输入：`docs/cli-spec.md`、源项目 `pyproject.toml` 中的 console scripts。
- 输出：`cmd/ps-agent`、`cmd/ps-sub`、Cobra 根命令、版本命令、基础日志初始化。
- 验收标准：`ps-agent --help`、`ps-sub --help`、`ps-agent version`、`ps-sub version` 可运行；不包含业务逻辑。
- 依赖：无。

## T02 配置模型与加载

- 目标：迁移全局配置、stack 配置、sub 配置和基础 YAML 加载。
- 输入：`docs/schema-spec.md`、`src/proxystack/domain/models.py`、`src/proxystack/config/loader.py`、fixtures。
- 输出：Go struct、YAML loader、默认值补齐、路径解析。
- 验收标准：`tests/fixtures/example-project/config.yaml` 和所有 stack 可加载；字段名保持 snake_case；允许 extra 与 forbid extra 的模型符合矩阵。
- 依赖：T01。

## T03 配置校验矩阵

- 目标：迁移跨字段、跨 stack 和安全校验。
- 输入：`src/proxystack/domain/validation.py`、现有 config loader 和 validation tests。
- 输出：validator 注册、自定义校验函数、聚合错误结构。
- 验收标准：覆盖 stack 文件名与 `name` 不一致、端口重复、系统端口占用、公开 socks/http noauth、日志级别非法、vmess 多用户重复、SS2022 key 长度错误、rules target 缺失。
- 依赖：T02。

## T04 引用图与依赖计划

- 目标：构建 ref parser、endpoint index、服务 DAG 和 target scope。
- 输入：`docs/generator-spec.md`、`docs/cli-spec.md`、`src/proxystack/graph`。
- 输出：ReferenceGraph、DependencyPlan、TargetScope。
- 验收标准：`usa1/usa2/auto` 依赖排序与 Python 一致；缺失 ref、协议不匹配、disabled stack/组件、循环依赖都失败；`0.0.0.0` 内部连接归一化为 `127.0.0.1`。
- 依赖：T03。

## T05 Xray 生成器

- 目标：生成稳定 Xray JSON。
- 输入：`docs/generator-spec.md`、`src/proxystack/generator/xray/config.py`、`tests/golden/xray`。
- 输出：Xray config 结构、渲染函数、格式化输出。
- 验收标准：所有 Xray golden 逐字节一致；覆盖 api/stats/policy、vmess 多用户、shadowsocks/SS2022、多 outbound 类型。
- 依赖：T04。

## T06 mihomo 生成器

- 目标：生成稳定 mihomo YAML。
- 输入：`docs/generator-spec.md`、`src/proxystack/generator/mihomo/config.py`、`tests/golden/mihomo`。
- 输出：listener、proxy、proxy-groups、rules profile 生成器。
- 验收标准：mihomo golden 固定输出一致；`users: []` 与缺省 users 语义不混淆；mixed listener 显式拒绝；rules target 校验完整。
- 依赖：T04。

## T07 订阅 input/index 与模板渲染

- 目标：迁移订阅节点生成、合并和三类订阅渲染。
- 输入：`docs/generator-spec.md`、`docs/template-compat-spec.md`、`src/proxystack/generator/sub/config.py`、`src/proxystack/templates/sub/*.j2`、`tests/golden/sub`。
- 输出：SubscriptionInput、SubscriptionIndex、Clash/Premium Clash/Surge 渲染。
- 验收标准：input/index/三类订阅 golden 一致；订阅不含 clash 内部配置；重复 node id 和同用户重复 proxy name 失败；地区分组和 Surge managed config 行为一致。
- 依赖：T03。

## T08 模板兼容任务

- 目标：明确并实现用户自定义 `.j2` 模板兼容策略。
- 输入：`docs/template-compat-spec.md`、模板查找顺序、`tests/test_sub_generator.py` 中模板覆盖用例。
- 输出：模板加载器、覆盖顺序、坏模板错误处理。
- 验收标准：查找顺序保持为 `config.templates_dir/sub`、`config.templates_dir`、`data_dir/templates/sub`、内置模板；坏模板 HTTP 返回 503；不得无声明破坏现有 `.j2` 自定义模板。
- 依赖：T07。

## T09 订阅 bundle 与原生 backup

- 目标：迁移订阅发布包和原生配置备份。
- 输入：`src/proxystack/generator/sub/config.py`、`src/proxystack/generator/backup/config.py`、相关测试。
- 输出：bundle write/import、native backup export/import、manifest/hash/path 校验。
- 验收标准：schema 混用拒绝；manifest 文件集合完全匹配；hash mismatch 不写入；路径穿越、反斜杠、未知成员拒绝；`replace-all` 先整体校验再替换；backup 不包含 runtime/sub inputs。
- 依赖：T07。

## T10 sub HTTP 服务

- 目标：实现 `ps-sub serve` 的 HTTP 服务和内存索引。
- 输入：`docs/http-subserver-spec.md`、`src/proxystack/subserver/app.py`、`state.py`、`watcher.py`、`config.py`。
- 输出：Gin routes、SubscriptionState、watcher、token 鉴权。
- 验收标准：`/health`、`/sub/:user?token=`、`/sub/:token/:user`、`/premium_sub`、`/surge_sub` 兼容；token 缺失 401、错误 403、无用户 404、模板错误 503；运行期 reload 失败保留上一版内存索引。
- 依赖：T08、T09。

## T11 agent 配置编辑与模板命令

- 目标：实现 `init/add/config/list/remove/clone/member`。
- 输入：`docs/cli-spec.md`、`docs/schema-spec.md`、`src/proxystack/cli/lifecycle.py`、模板文件。
- 输出：默认配置生成、stack 模板、端口自动分配、成员维护、配置编辑入口。
- 验收标准：`init` 不覆盖已有文件除非 force；`add` 随机替换 vmess UUID；`clone --allocate-ports` 只改目标 stack；member 命令只允许 auto/load-balance stack。
- 依赖：T02、T04。

## T12 runtime plan、manifest 与只读命令

- 目标：实现 `validate/render/check` 和 runtime diff。
- 输入：`docs/generator-spec.md`、`RuntimePlan` 相关 Python 逻辑。
- 输出：生成文件计划、manifest diff、只读预览。
- 验收标准：`check` 不写文件、不操作 systemd；多次生成 hash 不变；`generated_at` 合理复用；scope 删除只影响目标服务；未变化文件不改写但可修复 metadata。
- 依赖：T05、T06、T07。

## T13 服务生命周期与 target scope

- 目标：实现 `start/stop/restart/status/logs/enable/disable/service`。
- 输入：`docs/cli-spec.md`、`src/proxystack/systemd/service.py`、fake runner 测试。
- 输出：target scope 解析、systemd runner、日志查看、服务 wrapper。
- 验收标准：订阅服务生命周期由 `ps-sub` 管理；代理目标启动前检查二进制；`journalctl -f` 多 unit 一次调用；inactive status 退出码 3 不当作失败。
- 依赖：T12。

## T14 systemd unit 与权限

- 目标：迁移 unit 模板、hardening 和文件权限修复。
- 输入：`docs/install-systemd-security-spec.md`、源项目 `docs/deployment.md`、systemd 相关测试。
- 输出：unit renderer、install/uninstall、metadata 修复工具。
- 验收标准：unit 只引用 generated 文件；`proxystack-sub.service` 只传 sub config；目录 `0750`、配置文件 `0640`、二进制 `0750`、geo `0640`；owner 为 `proxystack:proxystack` 时可修复。
- 依赖：T13。

## T15 install/update 与自更新

- 目标：迁移 mihomo/xray/geo 下载、安装、更新和 self update。
- 输入：`docs/install-systemd-security-spec.md`、`src/proxystack/install/service.py`、`tests/test_install.py`。
- 输出：install request、managed source、下载器、归档解包、原子替换、自更新 runner。
- 验收标准：托管源 `auto/github/r2` fallback；慢速切源；`.gz` 解压；geo 多文件事务回滚；`install` 跳过已存在而 `update` 强制替换；`all` 不包含 `self`；普通远端 URL 必须 sha256；私网/本机/metadata 下载目标拒绝。
- 依赖：T02。

## T16 diagnostics/ipinfo

- 目标：迁移出口 IP 查询。
- 输入：`src/proxystack/diagnostics/ipinfo.py`、`tests/test_ipinfo.py`。
- 输出：通过 mihomo socks listener 调用 curl 的诊断逻辑。
- 验收标准：IPv4/IPv6 来源选择、失败后尝试下一来源、进度输出、wildcard 和 IPv6 host 归一化一致；不误用 mihomo REST API。
- 依赖：T04。

## T17 部署脚本与 Docker sub

- 目标：迁移或重写安装脚本、sub 本地部署和 Docker 部署。
- 输入：`docs/install-systemd-security-spec.md`、`scripts/`、`Dockerfile.sub`、`docker-compose.sub.yml`、源项目 `docs/deployment.md`。
- 输出：Go binary bootstrap 脚本、sub-only local deploy、Dockerfile/compose。
- 验收标准：Shell 只做 bootstrap；sub 镜像不包含 xray/mihomo；Docker 默认非 root、read-only、`cap_drop: ALL`、持久化 `/data`。
- 依赖：T10、T15。

## T18 端到端迁移验收

- 目标：确认 Go 版可替代 Python 版。
- 输入：`docs/testing-acceptance-matrix.md`、当前 fixtures、golden、主流程测试。
- 输出：端到端测试矩阵和迁移验收报告。
- 验收标准：跑通 `init -> add -> validate -> check -> start -> sub export -> ps-sub import -> ps-sub serve -> HTTP subscription`；确认 Go 版不会读取或写入越界目录；关键生成物与 Python 版对照通过。
- 依赖：全部任务。
