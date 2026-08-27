# CLI 命令规格

生成日期：2026-06-17

本文定义 Go 版 `proxystack` 必须兼容的命令、参数、默认值和副作用边界。实现阶段不得把只读命令改成会落盘或调用服务管理器的命令。

## 1. 全局约定

二进制入口：

- `psctl`
- `pssub`

通用约定：

- 安装脚本保留 `ps-agent`、`ps-sub` 兼容软链接，文档和新生成服务文件统一使用 `psctl`、`pssub`。
- `psctl` 通过全局 `--base-dir DIR` 指定环境目录，默认 `/opt/proxystack`。
- agent 全局配置文件固定为 `<base-dir>/config.yaml`，不再提供 `-c/--config`。
- `pssub` 通过全局 `--base-dir DIR` 指定独立环境目录，默认 `/opt/proxystack-sub`；sub root 固定为 `<base-dir>`。
- `pssub` 通过全局 `--listen HOST:PORT` 覆盖订阅 HTTP 监听地址，默认 `0.0.0.0:3003`；`serve --host/--port` 可进一步覆盖 host 或 port。
- 服务管理器通过全局 `--service-manager auto|systemd|launchd` 指定，默认 `auto`；Linux 解析为 `systemd`，macOS 解析为 `launchd`，其他平台需要显式支持后才能使用 `auto`。
- CLI 日志消息使用英文，面向用户的错误摘要可以使用中文。
- 外部命令必须使用参数数组执行，禁止拼接 shell 字符串。
- 默认不自动提权，权限不足时失败并给出明确提示。

## 2. 副作用分类

| 分类 | 含义 | 命令 |
| --- | --- | --- |
| 只读 | 不写文件，不调用服务管理器，不启动 HTTP 服务 | `version`、`list`、`validate`、`check`、`render *`、`doctor`、`sub validate-inputs`、`user list` |
| 写 agent 配置 | 写 `config.yaml` 或 `stacks/*.yaml` | `psctl setup local`、`psctl setup all`、`psctl setup`、`config`、`add`、`clone`、`member add/remove`、`remove` |
| 写 sub 配置 | 写 `<sub-base-dir>/config.yaml` | `pssub setup local`、`pssub setup all`、`pssub setup` |
| 写 runtime | 写 `runtime/generated`、`runtime/manifest.json` 或 `publish` | `start`、`restart`、`sub export`、`export`、`import`、`user enable/disable` |
| 服务管理器 | 调用 `systemctl`/`journalctl` 或 `launchctl`/`log` | `setup local`、`setup all`、`setup`、`start`、`stop`、`restart`、`status`、`logs`、`enable`、`disable`、`service *` |
| 下载/安装 | 写 `downloads`、`bin`、`geo` 或 `.venv` | `psctl setup deps`、`psctl setup all`、`psctl setup`、`update` |
| HTTP 运行 | 启动长期运行进程 | `pssub serve` |

`check` 必须只做完整编译和 diff 预览，不能写 `runtime`，不能调用服务管理器。

订阅服务生命周期必须通过 `pssub start|stop|restart|status|logs|enable|disable` 管理；`psctl` 生命周期命令不提供订阅服务 target。

## 3. `psctl` 命令

### 3.1 `setup local`

```bash
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] setup local [--external-host HOST] [--force]
```

职责：

- 创建 base dir、标准目录和带注释说明的默认 `config.yaml`。
- 使用代码内置默认配置内容。
- `external_host` 未传时写为空值，后续可编辑。
- 已存在 `config.yaml` 时默认不覆盖，只补齐标准目录。
- 安装或更新 agent stack 服务文件。
- 不下载 mihomo/xray/geo。
- 不启动或 enable 服务。

副作用：

- 可写 `config.yaml`。
- 可创建标准目录。
- 可写系统服务文件目录；systemd 后端为 `/etc/systemd/system`，launchd 后端为 `/Library/LaunchDaemons`。

验收：

- 不传 `--force` 时不得覆盖既有 `config.yaml`。
- 创建的目录权限应符合部署规格。
- 默认配置内容必须可通过 strict 校验。
- 服务管理器权限不足时不能吞错。

### 3.2 `setup`

```bash
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] setup [all] [--external-host HOST] [--force]
psctl [--base-dir DIR] setup deps
```

职责：

- 不带参数时等价于 `setup all`。
- `setup all` 依次执行幂等 `setup local`、`setup deps`。
- `setup deps` 只安装 mihomo/xray/geo 等联网依赖，等价于托管依赖目标 `all`。
- `setup deps` 不安装 `self`，不写 service 文件，不启动或 enable 服务。

副作用：

- `setup all` 和默认 `setup` 具备 `setup local` 与 `setup deps` 的全部副作用。
- `setup deps` 可下载安装 mihomo/xray/geo。

验收：

- 任一步失败时命令失败，并显示失败步骤。
- `setup deps` 已安装目标存在时跳过。
- `setup deps` 不包含 `self`。
- setup 命令不得隐式启动服务。

### 3.3 `add`

```bash
psctl [--base-dir DIR] add NAME [--template pair|auto-url-test|load-balance] [--from-file FILE] [--members a,b] [--allocate-ports|--keep-template-ports] [--edit|--no-edit] [--editor CMD]
```

职责：

- 创建 `stacks/<name>.yaml`。
- 默认模板为 `pair`。
- 默认自动分配 xray inbound、Xray API、clash socks、clash HTTP、clash controller 端口。
- 内置模板中的 vmess 占位 UUID 必须替换成随机 UUID。
- `--from-file` 保留输入文件中的凭据和 UUID。
- 默认打开编辑器，初始内容为已完成改名、UUID 替换和端口分配的候选 YAML；`--no-edit` 用于脚本化场景，直接校验并写入。
- 交互编辑使用 `stacks/<name>.yaml.draft` 草稿；校验通过后原子写入真实文件并删除草稿，校验失败时真实文件不变且草稿保留，错误信息会提示草稿路径。

副作用：

- 只写新 stack 文件；交互编辑失败时可留下相邻草稿文件。
- 不写 runtime。
- 不调用服务管理器。

验收：

- 目标 stack 已存在时失败。
- `--keep-template-ports` 仍要检查端口合法、唯一和系统占用。
- `--members` 只对 auto 模板生效。

### 3.3.1 `example`

```bash
psctl example [config|stack|xray|clash] [SECTION] [TYPE]
```

职责：

- 输出可复制的 config 或 stack YAML 配置片段到 stdout。
- 不带参数时输出 usage 和当前支持的全部片段说明。
- 支持按 area、section 或具体 type 逐级筛选片段。
- 当前覆盖 config users，stack role，xray api/stats/policy/loglevel/auth/inbound/outbound，以及 clash mode/loglevel/controller/listener/upstream/group/rules。
- 精确到 `TYPE` 时输出纯 YAML；只筛选到 area 或 section 时输出带注释的候选片段清单。

副作用：只读。

验收：

- 不读取 `--base-dir` 下的配置文件。
- 不写任何文件。
- `--help` 和不带参数输出必须包含全部可用片段说明。

### 3.4 `config`

```bash
psctl [--base-dir DIR] config [NAME] [--editor CMD] [--check-only]
```

职责：

- 不带 `NAME` 时编辑全局 `config.yaml`。
- 带 `NAME` 时编辑对应 `stacks/<name>.yaml`。
- 保存后必须重新校验。
- 编辑真实配置前先写相邻草稿：`config.yaml.draft` 或 `stacks/<name>.yaml.draft`；若草稿已存在，下一次编辑继续使用该草稿。
- 校验通过后原子替换真实文件并删除草稿；校验失败时真实文件不变且草稿保留，错误信息会提示草稿路径。

副作用：

- 可写被编辑配置文件；校验失败时可留下相邻草稿文件。
- 如果 stack 配置变化且相关服务处于 active，采用现有行为：检测 active 服务，检查所需二进制，apply runtime plan，重启 active services。

验收：

- 编辑器退出失败时命令失败。
- 保存后校验失败时必须给出错误，不可静默接受非法配置。
- `--check-only` 只校验目标文件，不启动编辑器。
- stack 变更后只有 active 组件会被重启；inactive 组件不重启。

### 3.5 `list`

```bash
psctl [--base-dir DIR] list [--verbose] [--check-system-ports]
```

职责：

- 列出 stack 文件、enabled、role、生成状态、运行状态、xray endpoint 和 clash endpoint。
- 端口后缀 `(L)` 表示 loopback，`(*)` 表示非 loopback。

副作用：

- 默认只读。
- 传 `--check-system-ports` 时可做系统端口探测，但不得写文件。

验收：

- 输出顺序按 stack 名稳定排序。
- disabled stack 仍应展示，但生命周期默认不作用于它。

### 3.6 `clone`

```bash
psctl [--base-dir DIR] clone SOURCE TARGET [--allocate-ports] [--edit|--no-edit] [--editor CMD]
```

职责：

- 复制 source stack 为 target stack。
- 改写顶层 `name`。
- 默认自动分配 target 的新端口；可用 `--allocate-ports=false` 保留候选内容中的端口，但端口冲突会拒绝写入。
- 默认打开编辑器；`--no-edit` 用于脚本化场景，直接校验并写入。
- 只改写真正 `ref` 字段中指向 source 自身资源的 ref，例如 `source.clash.socks` 或 `source.relay`。
- 指向其他 stack 的 ref 保持不变。
- 普通字符串字段如 `server`、`Host` 即使包含 source 名称也不得改写。
- 交互编辑使用 `stacks/<target>.yaml.draft` 草稿；校验通过后原子写入真实文件并删除草稿，校验失败时真实文件不变且草稿保留，错误信息会提示草稿路径。

副作用：

- 只写 target stack 文件；交互编辑失败时可留下相邻草稿文件。

验收：

- target 已存在时失败。
- 端口冲突时拒绝写入。
- 明文凭据默认保持不变。

### 3.7 `member`

```bash
psctl [--base-dir DIR] member list STACK
psctl [--base-dir DIR] member add STACK MEMBER
psctl [--base-dir DIR] member remove STACK MEMBER
```

职责：

- 维护 auto/load-balance stack 的 `xray-socks5` 成员。

副作用：

- `list` 只读。
- `add/remove` 只写目标 auto stack 文件。

验收：

- 普通 edge stack 必须拒绝。
- member stack 必须存在名为 `relay` 的 socks5 inbound。
- `add` 同步 upstream 和相关代理组。
- `remove` 清理 upstream 和相关代理组中的 proxy。

### 3.7.1 `user`

```bash
psctl [--base-dir DIR] user list [TARGET]
psctl [--base-dir DIR] user disable USER [TARGET]
psctl [--base-dir DIR] user enable USER [TARGET]
```

职责：

- 临时启停已配置的用户，不重启服务。新增、删除和修改用户仍然改配置文件。
- 只覆盖 vmess 和 shadowsocks inbound：socks5/http 是单账号 inbound，摘掉账号会退化成免认证入口。
- `TARGET` 省略表示全部 stack，支持 `NAME` 和 `xray/NAME`；`clash/NAME` 必须显式拒绝。
- 状态按 `(stack, user)` 记录，同一 user 的所有 profile 一起启停。

副作用：

- `list` 只读。
- `disable/enable` 写 `runtime/disabled.json`，重新生成受影响 stack 的 `runtime/generated/xray/*.json` 并更新 manifest，然后调用受管 `bin/xray` 的 `api rmu`/`api adu` 热应用。
- 不写 `config.yaml` 和 `stacks/*.yaml`，不调用服务管理器。

验收：

- 禁用状态跨重启存活：生成 Xray 配置时按 `runtime/disabled.json` 过滤用户。
- 顺序必须是先算目标状态、预演 plan 并校验，全部通过后才落盘、写生成文件、热应用。任何一步校验失败都不能留下已写入的状态。
- 待写入的生成结果必须**恰好**等于本次启停造成的 `clients` 增删：既不能有 `clients` 以外的差异，也不能夹带别处未重启的用户增删（那些不会被热应用，写盘会让运行中实例和磁盘分叉且此后漂移检测失效）。不满足时拒绝写入任何文件并提示改用 `psctl restart`。
- 逐 stack 预演，某个无关 stack 的未重启改动不能挡住其它 stack 的启停。
- 拒绝禁用某个 inbound 的最后一个启用用户，并区分两种局面：配置里本来就只有这一个用户时提示「only user」并指向改 stack 文件；其他用户已被禁用时提示「last enabled user」并列出可以先启用哪些。
- 热应用成功与否按 `xray api` 输出的 `Removed/Added N user(s) in total.` 判定，不能只看退出码：这两个子命令对单用户失败只打印不改退出码。
- 热应用失败（服务未运行、`xray.api.services` 缺少 `HandlerService`、计数为 0 等）只告警，命令仍然成功，并逐条给出可直接执行的 `psctl restart xray/NAME`。
- 传给 `xray api` 的 inbound tag 和 email 不能以 `-` 开头，否则会被 flag 解析吃掉。
- `disable` 时提示同 scope 内引用了该用户但不能启停的入口（socks5/http inbound、同名 Clash listener 账号）。
- `disabled.json` 里引用了已不存在 stack/用户的陈旧条目，`user list` 和 `doctor` 要报出来，`user enable` 可以清除。
- 订阅内容和 clash 入口不受启停影响。

### 3.8 `remove`

```bash
psctl [--base-dir DIR] remove NAME [--purge]
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
psctl [--base-dir DIR] validate [TARGET] [--skip-system-ports]
```

职责：

- 校验全局配置、stack 文件、端口、ref、rules、mode、安全约束和凭据格式。

副作用：只读。

验收：

- target 可为全部、stack、`xray/name`、`clash/name`。
- 错误需要聚合输出，不能只报第一条。

### 3.10 `check`

```bash
psctl [--base-dir DIR] check [TARGET] [--skip-system-ports]
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
psctl [--base-dir DIR] render model [--skip-system-ports]
psctl [--base-dir DIR] render xray STACK [--skip-system-ports]
psctl [--base-dir DIR] render clash STACK [--skip-system-ports]
psctl [--base-dir DIR] render sub [--input-dir DIR] [--skip-system-ports]
psctl [--base-dir DIR] export-config sub|premium_sub|surge_sub USER
```

职责：

- `model` 输出解析后的中间模型。
- `xray` 输出指定 stack 的 Xray JSON。
- `clash` 输出指定 stack 的 mihomo YAML。
- `sub` 输出订阅 index；传 `--input-dir` 时读取外部 inputs 合并。
- `export-config` 输出指定用户的订阅文本。

副作用：只读。

验收：

- 输出必须稳定。
- `render sub --input-dir` 不读取 stack。
- `export-config` 不写 publish 目录或 pssub inputs。

### 3.12 生命周期命令

```bash
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] start [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] stop [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] restart [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] status [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] logs [TARGET] [--follow|-f]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] enable [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] disable [TARGET]
```

target 规则：

- 空：全部 enabled stack。
- `NAME`：该 stack 的 xray + clash。
- `xray/NAME`：只操作 xray。
- `clash/NAME`：只操作 mihomo。

副作用：

- `start/restart` 可写 runtime/generated 和 manifest，并调用服务管理器。
- `stop/status/logs/enable/disable` 只调用服务管理器，不写 runtime。

验收：

- `start/restart` 调服务管理器前必须检查所需二进制存在且可执行。
- 生命周期命令默认跳过系统端口占用检查。
- `logs NAME -f` 对该 stack 的 xray 和 clash 服务使用一次日志查询调用；systemd 后端使用 `journalctl`，launchd 后端使用 `log`。
- `start/stop/restart/enable/disable` 执行前必须输出将要操作的 stack 组件和底层服务名；没有匹配服务时输出明确提示。
- `all` 和 `sub` 不作为保留 target；如存在同名 stack，按普通 stack 名处理。

### 3.13 `service`

```bash
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] service install [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] service uninstall [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] service start|stop|restart|status|enable|disable [TARGET]
psctl [--base-dir DIR] [--service-manager auto|systemd|launchd] service logs|log [TARGET] [--follow|-f]
```

职责：

- `install/uninstall` 管理 systemd unit 或 launchd plist。
- 其他子命令是服务管理器 wrapper。

副作用：

- `install/uninstall` 写系统服务文件目录。
- 其他子命令只调用服务管理器。

验收：

- 不传 target 时只安装或卸载 agent stack 服务文件；不得安装或卸载订阅服务文件。
- `all` 和 `sub` 不作为保留 target；如存在同名 stack，按普通 stack 名处理。
- 服务管理器错误必须保留 stdout/stderr 摘要。

### 3.14 `setup deps/update/version`

```bash
psctl [--base-dir DIR] setup deps
psctl [--base-dir DIR] update mihomo|xray|geo|all [--version V] [--source SOURCE] [--sha256 HASH] [--archive-member NAME]
psctl [--base-dir DIR] update self [--wheel FILE|PACKAGE_SPEC] [--sha256 HASH]
psctl version [mihomo|xray|geo]
```

输出：

```text
psctl
  version: <git tag>
  commit: <short hash>
  build_datetime: <UTC RFC3339>
```

验收：

- `setup deps` 和 `update all` 不包含 `self`。
- `setup deps` 目标已存在时跳过。
- `update` 强制重新下载或替换。
- 普通远端 URL 必须提供 sha256。
- 托管源支持 `auto`、`github`；`r2` 未配置时返回明确错误。

### 3.15 `sub` 子命令

```bash
psctl [--base-dir DIR] sub export [STACK] [-o OUTPUT] [--summary|--dry-run]
psctl sub validate-inputs --input-dir DIR
```

职责：

- `export` 生成订阅发布包。
- `validate-inputs` 只校验并汇总 inputs。

副作用：

- `export` 可写 publish 目录。
- `--summary` 或 `--dry-run` 不写 zip。
- 其他子命令只读。

验收：

- `sub export` 缺少 `external_host` 时失败。
- 指定 stack 时默认输出 `<stack>-sub-bundle.zip`。
- 不直接写 pssub `inputs`。

### 3.16 `export/import`

```bash
psctl [--base-dir DIR] export [-o OUTPUT]
psctl [--base-dir DIR] import BACKUP [--force]
```

职责：

- 原生 agent 配置备份与恢复。

副作用：

- `export` 写 publish 或指定路径。
- `import` 写 `config.yaml` 和 `stacks/*.yaml`。

验收：

- 原生 backup 不能被 `pssub import` 接受。
- 订阅 bundle 不能被 `psctl import` 当作 native backup。

### 3.17 `doctor/ipinfo`

```bash
psctl [--base-dir DIR] doctor
psctl [--base-dir DIR] ipinfo STACK [--family all|ipv4|ipv6] [--timeout SECONDS]
```

职责：

- `doctor` 检查目录权限（含 setgid 位）、文件所属组、二进制版本、systemd unit、端口占用、配置引用，以及用户启停相关状态。受管目录是组可写的，owner 可能是运维账号、root 或服务账号，因此只校验组不校验 owner。
- `ipinfo` 通过该 stack 的 mihomo socks listener 和 `curl` 查询出口 IP。

副作用：

- `doctor` 只读。
- `ipinfo` 只读，但会发起外部 HTTP 请求。

验收：

- `ipinfo --family` 兼容值为 `all`、`ipv4`、`ipv6`；如提供 `4/6` 可作为 Go 版兼容别名，但文档主值保持现有命名。
- `ipinfo --timeout` 默认 `8.0` 秒。
- `ipinfo` 不是 mihomo REST API。
- IPv4/IPv6 默认来源和 fallback 与 Python 版一致。
- `setup local` 在 root 和非 root 两条路径上都要提示把运维账号加入 `proxystack` 组；root 路径取 `SUDO_USER`，已在组内或直接以 root 登录时不提示。
- `doctor` 必须陈述每个 stack 的 `HandlerService` 状态：开启时说明它是本机无鉴权的用户/inbound 管理面，未开启时说明 `psctl user` 需要 `psctl restart` 才生效。两者都是合法配置，只作为 check 输出，不能让 `doctor` 判失败。
- `runtime/disabled.json` 里引用了已不存在用户的陈旧条目必须报成 issue。

## 4. `pssub` 命令

全局路径入口：

- `pssub` 通过全局 `--base-dir DIR` 指定独立环境目录，默认 `/opt/proxystack-sub`。
- sub root 固定为 `<base-dir>`。
- sub config 固定为 `<base-dir>/config.yaml`。
- inputs 固定为 `<base-dir>/inputs`。
- templates 默认目录为 `<base-dir>/templates`。
- 不提供 `--config` 或 `--data-dir`。
- 监听地址可通过全局 `--listen HOST:PORT` 覆盖，默认 `0.0.0.0:3003`。
- 服务管理器通过全局 `--service-manager auto|systemd|launchd` 指定，默认 `auto`。

### 4.1 `setup`

```bash
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] setup [local|all] [--force]
```

职责：

- 不带参数时等价于 `setup all`。
- `setup all` 当前等价于 `setup local`。
- 幂等创建 `<base-dir>`、`<base-dir>/inputs`、`<base-dir>/templates` 和 `<base-dir>/config.yaml`。
- 默认不覆盖既有 `<base-dir>/config.yaml`。
- `--force` 会重写默认 sub config。
- 安装或更新 pssub 服务文件。
- 不下载任何依赖。
- 不启动或 enable 服务。

副作用：

- 可写 `<base-dir>` 下的 pssub 目录和配置。
- 可写系统服务文件目录；systemd 后端为 `/etc/systemd/system`，launchd 后端为 `/Library/LaunchDaemons`。

验收：

- 不读取 agent `config.yaml`。
- 不读取或创建 `stacks/`、`runtime/`、`publish/`。
- 默认配置必须可被 `pssub config check`、`pssub config show` 和 `pssub serve` 加载。
- setup 命令不得隐式启动服务。

### 4.2 `version`

```bash
pssub version
```

输出：

```text
pssub
  version: <git tag>
  commit: <short hash>
  build_datetime: <UTC RFC3339>
```

副作用：只读。

### 4.3 `config`

```bash
pssub [--base-dir DIR] config
pssub [--base-dir DIR] config show [--show-secrets]
pssub [--base-dir DIR] config check
```

职责：

- `config` 编辑 `<base-dir>/config.yaml`，保存后立即 strict 校验。
- `config show` 打印有效 sub config，默认脱敏 token。
- `config check` 只校验 sub config。
- `config` 使用 `<base-dir>/config.yaml.draft` 草稿；校验通过后原子替换真实文件并删除草稿，校验失败时真实文件不变且草稿保留，错误信息会提示草稿路径。

副作用：

- `config` 可写 `<base-dir>/config.yaml`；校验失败时可留下相邻草稿文件。
- `config show` 和 `config check` 只读。

验收：

- 不读取 agent `config.yaml`。
- YAML 中不支持 `data_dir` 字段。
- 运行时 DataDir 由 `--base-dir` 注入，不输出到 config YAML。
- 编辑后配置非法时不得覆盖原文件。
- `config show` 只有传 `--show-secrets` 时才打印 token 明文。

### 4.4 `import`

```bash
pssub [--base-dir DIR] import BUNDLE [--replace-all]
```

职责：

- 校验订阅 bundle，并把 inputs 原子写入 `<base-dir>/inputs`。

副作用：

- 写 `<base-dir>/inputs`。

验收：

- 先完整校验 zip、manifest、hash、schema、重复节点，再写入。
- `--replace-all` 只在全部校验通过后清理旧 managed input。
- native backup 必须拒绝。

### 4.5 `clear`

```bash
pssub [--base-dir DIR] clear
```

职责：

- 清空 managed inputs。

副作用：

- 删除 `<base-dir>/inputs` 中由 import 管理的 input 文件。

验收：

- 不删除 `<base-dir>/config.yaml`。
- 不读取 agent 配置。

### 4.6 `input`

```bash
pssub [--base-dir DIR] input list
pssub [--base-dir DIR] input show SOURCE [--raw] [--show-secrets]
pssub [--base-dir DIR] input validate [SOURCE]
pssub [--base-dir DIR] input edit SOURCE [--editor CMD]
pssub [--base-dir DIR] input clone SOURCE TARGET [--editor CMD]
pssub [--base-dir DIR] input set-host HOST [SOURCE] [--all]
pssub [--base-dir DIR] input remove SOURCE
```

职责：

- `list` 列出 `<base-dir>/inputs` 中的 input 文件、source、nodes、users 和 generated_at。
- `show` 打印单个 input，默认输出脱敏后的规范 YAML；`--raw` 输出原始文件内容；`--show-secrets` 仅影响非 raw 输出。
- `validate` 严格校验单个 input，或对全部 inputs 执行合并校验。
- `edit` 使用 `<source-file>.draft` 草稿编辑单个 input，保存前必须 strict decode，并通过单文件校验和包含现有 inputs 的全量合并校验。
- `clone` 复制单个 input 为新目标，默认打开编辑器；`TARGET` 不带扩展名时沿用源文件扩展名；写入前必须 strict decode 并通过包含现有 inputs 的全量合并校验。
- `edit`/`clone` 校验通过后原子替换或创建真实文件并删除草稿；校验失败时真实文件不变且草稿保留，错误信息会提示草稿路径，下一次编辑继续使用草稿。
- `set-host` 把目标 input 的所有 `nodes[].server` 写成 trim 后的 `HOST`；指定 `SOURCE` 时只修改单文件，传 `--all` 时扫描全部安全 input 文件；`SOURCE` 与 `--all` 互斥且必须选择其一。
- `remove` 删除单个 input 文件。

副作用：

- `list`、`show`、`validate` 只读。
- `edit` 可写目标 input 文件；校验失败时可留下相邻草稿文件。
- `clone` 可写新目标 input 文件，不覆盖既有文件；编辑器退出或校验失败时不得写入目标文件，但可留下新目标的相邻草稿文件。
- `set-host` 可写目标 input 文件，或在 `--all` 模式写多个 input 文件；写回前必须 strict decode 并通过合并校验，没有实际变化时只输出 unchanged。
- `remove` 可删除目标 input 文件。

验收：

- SOURCE 只能解析为 `<base-dir>/inputs` 下的 `.yaml`、`.yml` 或 `.json` 普通文件，不允许路径穿越。
- `clone` 的 TARGET 只能解析为 `<base-dir>/inputs` 下尚不存在的 `.yaml`、`.yml` 或 `.json` 普通文件，不允许路径穿越。
- `show` 默认不得输出 password、token、uuid 等敏感值。
- `edit` 校验失败时不得覆盖原文件，且必须保留草稿。
- `clone` 编辑后未改掉重复 `node.id` 或同用户重复代理名时必须失败，不得创建目标文件，且必须保留草稿。
- `set-host` 校验失败时不得覆盖原文件。
- `validate` 全量模式必须发现重复 `node.id` 和同用户重复代理名。
- 不读取 agent `config.yaml`。

### 4.7 `serve`

```bash
pssub [--base-dir DIR] [--listen HOST:PORT] serve [--host HOST] [--port PORT]
```

职责：

- 启动订阅 HTTP 服务。
- 加载 `<base-dir>/config.yaml` 和 `<base-dir>/inputs`。
- 启动 watcher，运行期 reload。

副作用：

- 长期运行 HTTP server。
- 运行期只读 inputs，除日志外不写 agent 目录。

验收：

- 启动阶段输入非法时启动失败。

### 4.8 `service install`

```bash
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] service install
```

职责：

- 只安装订阅服务对应的 systemd unit 或 launchd plist。
- unit/plist 中固化当前 `--base-dir`，运行命令为 `pssub --base-dir DIR serve`。

副作用：

- 写系统服务文件目录。

验收：

- 不读取 agent `config.yaml`。
- 不读取或创建 `stacks/`、`runtime/`、`publish/`。
- 不安装 xray/mihomo 相关服务文件。

### 4.9 生命周期命令

```bash
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] start
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] stop
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] restart
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] status
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] logs [--follow|-f]
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] enable
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] disable
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

### 4.10 `doctor`

```bash
pssub [--base-dir DIR] [--service-manager auto|systemd|launchd] doctor
```

职责：

- 检查 `<base-dir>/config.yaml` 是否存在且可通过 strict 校验。
- 检查 `<base-dir>/inputs` 是否可按运行时逻辑合并为订阅索引。
- 检查 `<base-dir>` 目录树权限和 owner 是否符合订阅服务运行要求。
- 检查订阅服务对应的 systemd unit 或 launchd plist 是否存在。

副作用：

- 只读。

验收：

- 不读取 agent `config.yaml`。
- 不读取 `stacks/` 或写 `runtime`。
- 输出格式与 `psctl doctor` 保持一致：通过项打印 `OK`，问题项打印 `ISSUE`，存在问题时返回非零退出。
