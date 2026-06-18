# CLI 命令规格

生成日期：2026-06-17

本文定义 Go 版 `proxystack` 必须兼容的命令、参数、默认值和副作用边界。实现阶段不得把只读命令改成会落盘或调用服务管理器的命令。

## 1. 全局约定

二进制入口：

- `proxystack-agent`
- `proxystack-sub`
- `ps-agent`，等价于 `proxystack-agent`
- `ps-sub`，等价于 `proxystack-sub`

通用约定：

- `proxystack-agent` 通过全局 `--base-dir DIR` 指定环境目录，默认 `/opt/proxystack`。
- agent 全局配置文件固定为 `<base-dir>/config.yaml`，不再提供 `-c/--config`。
- `proxystack-sub` 通过全局 `--base-dir DIR` 指定环境目录，默认 `/opt/proxystack`；sub root 固定为 `<base-dir>/sub`。
- `proxystack-sub` 通过全局 `--listen HOST:PORT` 覆盖订阅 HTTP 监听地址，默认 `0.0.0.0:3003`；`serve --host/--port` 可进一步覆盖 host 或 port。
- 服务管理器通过全局 `--service-manager auto|systemd|launchd` 指定，默认 `auto`；Linux 解析为 `systemd`，macOS 解析为 `launchd`，其他平台需要显式支持后才能使用 `auto`。
- CLI 日志消息使用英文，面向用户的错误摘要可以使用中文。
- 外部命令必须使用参数数组执行，禁止拼接 shell 字符串。
- 默认不自动提权，权限不足时失败并给出明确提示。

## 2. 副作用分类

| 分类 | 含义 | 命令 |
| --- | --- | --- |
| 只读 | 不写文件，不调用服务管理器，不启动 HTTP 服务 | `version`、`list`、`validate`、`check`、`render *`、`doctor`、`sub validate-inputs` |
| 写配置 | 写 `config.yaml`、`sub/config.yaml` 或 `stacks/*.yaml` | `ps-agent init`、`config`、`add`、`clone`、`member add/remove`、`remove`、`ps-sub init` |
| 写 runtime | 写 `runtime/generated`、`runtime/manifest.json` 或 `publish` | `start`、`restart`、`sub export`、`export`、`import` |
| 服务管理器 | 调用 `systemctl`/`journalctl` 或 `launchctl`/`log` | `start`、`stop`、`restart`、`status`、`logs`、`enable`、`disable`、`service *` |
| 下载/安装 | 写 `downloads`、`bin`、`geo` 或 `.venv` | `install`、`update` |
| HTTP 运行 | 启动长期运行进程 | `proxystack-sub serve` |

`check` 必须只做完整编译和 diff 预览，不能写 `runtime`，不能调用服务管理器。

`start sub` 必须只操作本地订阅服务，不能读取 `config.yaml` 和 `stacks/*.yaml`，不能创建 `runtime/generated`。

## 3. `proxystack-agent` 命令

### 3.1 `init`

```bash
ps-agent [--base-dir DIR] init [--external-host HOST] [--force]
```

职责：

- 创建 base dir、标准目录、默认 `config.yaml` 和初始 `sub/config.yaml`。
- 优先读取内置 `agent-config.yaml` 模板。
- `external_host` 未传时保留注释示例。
- 已存在 `config.yaml` 时默认不覆盖。

副作用：

- 可写 `config.yaml`。
- 可创建标准目录。
- 不下载依赖，不安装系统服务文件。

验收：

- 不传 `--force` 时不得覆盖既有 `config.yaml`。
- 创建的目录权限应符合部署规格。
- 缺少内置模板时可使用代码内置默认值。

### 3.2 `setup`

```bash
ps-agent [--base-dir DIR] setup [--external-host HOST] [--force] [--start]
```

职责：

- 依次执行幂等 `init`、`install all`、`service install`。
- 已有 `config.yaml` 且未传 `--force` 时不覆盖配置，只补齐标准目录和缺失的 sub 默认配置后继续。
- 传入 `--start` 时继续执行 `start`。

副作用：

- 可写配置和标准目录。
- 可下载安装 mihomo/xray/geo。
- 可写系统服务文件目录；systemd 后端为 `/etc/systemd/system`，launchd 后端为 `/Library/LaunchDaemons`。

验收：

- 任一步失败时命令失败，并显示失败步骤。
- `install all` 不包含 `self`。
- 服务管理器权限不足时不能吞错。

### 3.3 `add`

```bash
ps-agent [--base-dir DIR] add NAME [--template pair|auto-url-test|load-balance] [--from-file FILE] [--members a,b] [--allocate-ports|--keep-template-ports] [--edit|--no-edit] [--editor CMD]
```

职责：

- 创建 `stacks/<name>.yaml`。
- 默认模板为 `pair`。
- 默认自动分配 xrelay inbound、Xray API、clash socks、clash HTTP、clash controller 端口。
- 内置模板中的 vmess 占位 UUID 必须替换成随机 UUID。
- `--from-file` 保留输入文件中的凭据和 UUID。

副作用：

- 只写新 stack 文件。
- 不写 runtime。
- 不调用服务管理器。

验收：

- 目标 stack 已存在时失败。
- `--keep-template-ports` 仍要检查端口合法、唯一和系统占用。
- `--members` 只对 auto 模板生效。

### 3.4 `config`

```bash
ps-agent [--base-dir DIR] config [NAME] [--editor CMD] [--check-only]
```

职责：

- 不带 `NAME` 时编辑全局 `config.yaml`。
- 带 `NAME` 时编辑对应 `stacks/<name>.yaml`。
- 保存后必须重新校验。

副作用：

- 可写被编辑配置文件。
- 如果 stack 配置变化且相关服务处于 active，采用现有行为：检测 active 服务，检查所需二进制，apply runtime plan，重启 active services。

验收：

- 编辑器退出失败时命令失败。
- 保存后校验失败时必须给出错误，不可静默接受非法配置。
- `--check-only` 只校验目标文件，不启动编辑器。
- stack 变更后只有 active 组件会被重启；inactive 组件不重启。

### 3.5 `list`

```bash
ps-agent [--base-dir DIR] list [--verbose] [--check-system-ports]
```

职责：

- 列出 stack 文件、enabled、role、生成状态、运行状态、xrelay endpoint 和 clash endpoint。
- 端口后缀 `(L)` 表示 loopback，`(*)` 表示非 loopback。

副作用：

- 默认只读。
- 传 `--check-system-ports` 时可做系统端口探测，但不得写文件。

验收：

- 输出顺序按 stack 名稳定排序。
- disabled stack 仍应展示，但生命周期默认不作用于它。

### 3.6 `clone`

```bash
ps-agent [--base-dir DIR] clone SOURCE TARGET [--allocate-ports]
```

职责：

- 复制 source stack 为 target stack。
- 改写顶层 `name`。
- 第一段等于 source 且指向自身资源的 ref 改为 target。
- 指向其他 stack 的 ref 保持不变。

副作用：

- 只写 target stack 文件。

验收：

- target 已存在时失败。
- 不传 `--allocate-ports` 且端口冲突时拒绝写入。
- 明文凭据默认保持不变。

### 3.7 `member`

```bash
ps-agent [--base-dir DIR] member list STACK
ps-agent [--base-dir DIR] member add STACK MEMBER
ps-agent [--base-dir DIR] member remove STACK MEMBER
```

职责：

- 维护 auto/load-balance stack 的 `xrelay-socks5` 成员。

副作用：

- `list` 只读。
- `add/remove` 只写目标 auto stack 文件。

验收：

- 普通 edge stack 必须拒绝。
- member stack 必须存在名为 `relay` 的 socks5 inbound。
- `add` 同步 upstream 和相关代理组。
- `remove` 清理 upstream 和相关代理组中的 proxy。

### 3.8 `remove`

```bash
ps-agent [--base-dir DIR] remove NAME [--purge]
```

职责：

- 删除或归档 `stacks/<name>.yaml`。
- `--purge` 清理 manifest 中该 stack 对应生成文件。

副作用：

- 可写 `stacks`。
- 传 `--purge` 时可删除 generated 文件并更新 manifest。

验收：

- 默认不删除 runtime 生成文件。
- 不停止 systemd 服务，除非后续明确扩展。

### 3.9 `validate`

```bash
ps-agent [--base-dir DIR] validate [TARGET] [--skip-system-ports]
```

职责：

- 校验全局配置、stack 文件、端口、ref、rules、mode、安全约束和凭据格式。

副作用：只读。

验收：

- target 可为全部、stack、`xrelay/name`、`clash/name`。
- 错误需要聚合输出，不能只报第一条。

### 3.10 `check`

```bash
ps-agent [--base-dir DIR] check [TARGET] [--skip-system-ports]
```

职责：

- 执行完整编译，对比现有 manifest，输出 create/update/delete/no-change 和建议重启服务。

副作用：只读。

验收：

- 不创建 `runtime/generated`。
- 不写 manifest。
- 不调用服务管理器。

### 3.11 `render`

```bash
ps-agent [--base-dir DIR] render model [--skip-system-ports]
ps-agent [--base-dir DIR] render xrelay STACK [--skip-system-ports]
ps-agent [--base-dir DIR] render clash STACK [--skip-system-ports]
ps-agent [--base-dir DIR] render sub [--input-dir DIR] [--skip-system-ports]
```

职责：

- `model` 输出解析后的中间模型。
- `xrelay` 输出指定 stack 的 Xray JSON。
- `clash` 输出指定 stack 的 mihomo YAML。
- `sub` 输出订阅 index；传 `--input-dir` 时读取外部 inputs 合并。

副作用：只读。

验收：

- 输出必须稳定。
- `render sub --input-dir` 不读取 stack。

### 3.12 生命周期命令

```bash
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] start [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] stop [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] restart [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] status [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] logs [TARGET] [--follow|-f]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] enable [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] disable [TARGET]
```

target 规则：

- 空或 `all`：全部 enabled stack。
- `NAME`：该 stack 的 xray + clash。
- `xrelay/NAME`：只操作 xray。
- `clash/NAME`：只操作 mihomo。
- `sub`：只操作本地订阅服务。

副作用：

- `start/restart` 可写 runtime/generated 和 manifest，并调用服务管理器。
- `stop/status/logs/enable/disable` 只调用服务管理器，不写 runtime。

验收：

- `start/restart` 调服务管理器前必须检查所需二进制存在且可执行。
- 生命周期命令默认跳过系统端口占用检查。
- `logs NAME -f` 对该 stack 的 xray 和 clash 服务使用一次日志查询调用；systemd 后端使用 `journalctl`，launchd 后端使用 `log`。

### 3.13 `service`

```bash
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] service install [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] service uninstall [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] service start|stop|restart|status|enable|disable [TARGET]
ps-agent [--base-dir DIR] [--service-manager auto|systemd|launchd] service logs|log [TARGET] [--follow|-f]
```

职责：

- `install/uninstall` 管理 systemd unit 或 launchd plist。
- 其他子命令是服务管理器 wrapper。

副作用：

- `install/uninstall` 写系统服务文件目录。
- 其他子命令只调用服务管理器。

验收：

- `install all` 在 systemd 后端安装三个 unit 模板，在 launchd 后端按当前 enabled stack 渲染 plist。
- `sub` 只安装或操作订阅服务。
- 服务管理器错误必须保留 stdout/stderr 摘要。

### 3.14 `install/update/version`

```bash
ps-agent [--base-dir DIR] install mihomo|xray|geo|all [--version V] [--source SOURCE] [--sha256 HASH] [--archive-member NAME]
ps-agent [--base-dir DIR] update mihomo|xray|geo|all [--version V] [--source SOURCE] [--sha256 HASH] [--archive-member NAME]
ps-agent [--base-dir DIR] update self [--wheel FILE|PACKAGE_SPEC] [--sha256 HASH]
ps-agent version [mihomo|xray|geo]
```

输出：

```text
ps-agent
  version: <git tag>
  commit: <short hash>
  build_datetime: <UTC RFC3339>
```

验收：

- `install all` 和 `update all` 不包含 `self`。
- `install` 目标已存在时跳过。
- `update` 强制重新下载或替换。
- 普通远端 URL 必须提供 sha256。
- 托管源支持 `auto`、`github`；`r2` 未配置时返回明确错误。

### 3.15 `sub` 子命令

```bash
ps-agent [--base-dir DIR] sub export [STACK] [-o OUTPUT] [--summary|--dry-run]
ps-agent [--base-dir DIR] sub export-config sub|premium_sub|surge_sub USER
ps-agent sub validate-inputs --input-dir DIR
```

职责：

- `export` 生成订阅发布包。
- `export-config` 输出指定用户的订阅文本。
- `validate-inputs` 只校验并汇总 inputs。

副作用：

- `export` 可写 publish 目录。
- `--summary` 或 `--dry-run` 不写 zip。
- 其他子命令只读。

验收：

- `sub export` 缺少 `external_host` 时失败。
- 指定 stack 时默认输出 `<stack>-sub-bundle.zip`。
- 不直接写 `sub/inputs`。

### 3.16 `export/import`

```bash
ps-agent [--base-dir DIR] export [-o OUTPUT]
ps-agent [--base-dir DIR] import BACKUP [--force]
```

职责：

- 原生 agent 配置备份与恢复。

副作用：

- `export` 写 publish 或指定路径。
- `import` 写 `config.yaml` 和 `stacks/*.yaml`。

验收：

- 原生 backup 不能被 `ps-sub import` 接受。
- 订阅 bundle 不能被 `ps-agent import` 当作 native backup。

### 3.17 `doctor/ipinfo`

```bash
ps-agent [--base-dir DIR] doctor
ps-agent [--base-dir DIR] ipinfo STACK [--family all|ipv4|ipv6] [--timeout SECONDS]
```

职责：

- `doctor` 检查目录权限、二进制版本、systemd unit、端口占用和配置引用。
- `ipinfo` 通过该 stack 的 mihomo socks listener 和 `curl` 查询出口 IP。

副作用：

- `doctor` 只读。
- `ipinfo` 只读，但会发起外部 HTTP 请求。

验收：

- `ipinfo --family` 兼容值为 `all`、`ipv4`、`ipv6`；如提供 `4/6` 可作为 Go 版兼容别名，但文档主值保持现有命名。
- `ipinfo --timeout` 默认 `8.0` 秒。
- `ipinfo` 不是 mihomo REST API。
- IPv4/IPv6 默认来源和 fallback 与 Python 版一致。

## 4. `proxystack-sub` 命令

全局路径入口：

- `proxystack-sub` 通过全局 `--base-dir DIR` 指定环境目录，默认 `/opt/proxystack`。
- sub root 固定为 `<base-dir>/sub`。
- sub config 固定为 `<base-dir>/sub/config.yaml`。
- inputs 固定为 `<base-dir>/sub/inputs`。
- 不提供 `--config` 或 `--data-dir`。
- 监听地址可通过全局 `--listen HOST:PORT` 覆盖，默认 `0.0.0.0:3003`。
- 服务管理器通过全局 `--service-manager auto|systemd|launchd` 指定，默认 `auto`。

### 4.1 `init`

```bash
ps-sub [--base-dir DIR] init [--force]
```

职责：

- 幂等创建 `<base-dir>/sub`、`<base-dir>/sub/inputs` 和 `<base-dir>/sub/config.yaml`。
- 默认不覆盖既有 `<base-dir>/sub/config.yaml`。
- `--force` 会重写默认 sub config。

副作用：

- 只写 `<base-dir>/sub` 相关目录和配置。

验收：

- 不读取 agent `config.yaml`。
- 不读取或创建 `stacks/`、`runtime/`、`publish/`。
- 默认配置必须可被 `ps-sub config check`、`ps-sub config show` 和 `ps-sub serve` 加载。

### 4.2 `version`

```bash
ps-sub version
```

输出：

```text
ps-sub
  version: <git tag>
  commit: <short hash>
  build_datetime: <UTC RFC3339>
```

副作用：只读。

### 4.3 `config`

```bash
ps-sub [--base-dir DIR] config
ps-sub [--base-dir DIR] config show [--show-secrets]
ps-sub [--base-dir DIR] config check
```

职责：

- `config` 编辑 `<base-dir>/sub/config.yaml`，保存后立即 strict 校验。
- `config show` 打印有效 sub config，默认脱敏 token。
- `config check` 只校验 sub config。

副作用：

- `config` 可写 `sub/config.yaml`。
- `config show` 和 `config check` 只读。

验收：

- 不读取 agent `config.yaml`。
- YAML 中不支持 `data_dir` 字段。
- 运行时 DataDir 由 `--base-dir` 注入，不输出到 config YAML。
- 编辑后配置非法时不得覆盖原文件。
- `config show` 只有传 `--show-secrets` 时才打印 token 明文。

### 4.4 `import`

```bash
ps-sub [--base-dir DIR] import BUNDLE [--replace-all]
```

职责：

- 校验订阅 bundle，并把 inputs 原子写入 `<base-dir>/sub/inputs`。

副作用：

- 写 `sub/inputs`。

验收：

- 先完整校验 zip、manifest、hash、schema、重复节点，再写入。
- `--replace-all` 只在全部校验通过后清理旧 managed input。
- native backup 必须拒绝。

### 4.5 `clear`

```bash
ps-sub [--base-dir DIR] clear
```

职责：

- 清空 managed inputs。

副作用：

- 删除 `sub/inputs` 中由 import 管理的 input 文件。

验收：

- 不删除 `sub/config.yaml`。
- 不读取 agent 配置。

### 4.6 `serve`

```bash
ps-sub [--base-dir DIR] [--listen HOST:PORT] serve [--host HOST] [--port PORT]
```

职责：

- 启动订阅 HTTP 服务。
- 加载 `<base-dir>/sub/config.yaml` 和 `<base-dir>/sub/inputs`。
- 启动 watcher，运行期 reload。

副作用：

- 长期运行 HTTP server。
- 运行期只读 inputs，除日志外不写 agent 目录。

验收：

- 启动阶段输入非法时启动失败。

### 4.7 `service install`

```bash
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] service install
```

职责：

- 只安装订阅服务对应的 systemd unit 或 launchd plist。
- unit/plist 中固化当前 `--base-dir`，运行命令为 `ps-sub --base-dir DIR serve`。

副作用：

- 写系统服务文件目录。

验收：

- 不读取 agent `config.yaml`。
- 不读取或创建 `stacks/`、`runtime/`、`publish/`。
- 不安装 xray/mihomo 相关服务文件。

### 4.8 生命周期命令

```bash
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] start
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] stop
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] restart
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] status
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] logs [--follow|-f]
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] enable
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] disable
```

职责：

- 只操作订阅服务自身。

副作用：

- 调用 systemctl/journalctl 或 launchctl/log。

验收：

- 不读取 agent `config.yaml`。
- 不读取 `stacks/` 或写 `runtime`。
- `logs -f` 支持跟随日志。
- 运行期 reload 失败保留上一份可用内存索引。
- 日志不得打印 token/password。

### 4.9 `doctor`

```bash
ps-sub [--base-dir DIR] [--service-manager auto|systemd|launchd] doctor
```

职责：

- 检查 `<base-dir>/sub/config.yaml` 是否存在且可通过 strict 校验。
- 检查 `<base-dir>/sub/inputs` 是否可按运行时逻辑合并为订阅索引。
- 检查 `<base-dir>/sub` 目录树权限和 owner 是否符合订阅服务运行要求。
- 检查订阅服务对应的 systemd unit 或 launchd plist 是否存在。

副作用：

- 只读。

验收：

- 不读取 agent `config.yaml`。
- 不读取 `stacks/` 或写 `runtime`。
- 输出格式与 `ps-agent doctor` 保持一致：通过项打印 `OK`，问题项打印 `ISSUE`，存在问题时返回非零退出。
