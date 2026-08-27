# 安装、服务管理与安全规格

生成日期：2026-06-17

本文定义 Go 版安装更新、服务管理和安全边界。

## 1. 目录权限

默认用户和组：

```text
proxystack:proxystack
```

权限：

| 路径 | 权限 | owner |
| --- | --- | --- |
| `/opt/proxystack` | `0750` | `proxystack:proxystack` |
| Go binary 目录 | `0750` | `proxystack:proxystack` |
| `bin/` | `0750` | `proxystack:proxystack` |
| `geo/` | `0750` | `proxystack:proxystack` |
| `downloads/` | `0750` | `proxystack:proxystack` |
| `stacks/` | `2770` | `*:proxystack` |
| `runtime/` | `2770` | `*:proxystack` |
| `runtime/generated/`、`runtime/generated/xray`、`runtime/generated/mihomo` | `2770` | `*:proxystack` |
| `publish/` | `2770` | `*:proxystack` |
| `sub/` | `2770` | `*:proxystack` |
| `sub/` 下所有子目录 | `2770` | `*:proxystack` |
| `sub/` 下所有普通文件 | `0640` | `*:proxystack` |
| `config.yaml` | `0640` | `proxystack:proxystack` |
| `stacks/*.yaml` | `0640` | `*:proxystack` |
| `bin/mihomo`、`bin/xray` | `0750` | `proxystack:proxystack` |
| `geo/*` | `0640` | `proxystack:proxystack` |
| `runtime/manifest.json` | `0640` | `*:proxystack` |
| `runtime/disabled.json` | `0640` | `*:proxystack` |

未变化生成文件也允许修复 metadata。

### 组可写目录

`stacks/`、`runtime/`（含 `generated/`）、`publish/`、`sub/` 是 `2770`：加入 `proxystack` 组的运维账号不用 sudo 就能改 stack 配置、跑 `psctl start/restart`、用 `psctl user` 启停用户。

- **setgid 是配套必需项。** 组成员创建的文件默认带自己的主组，服务账号会读不到；setgid 让目录下新建的文件和子目录统一继承 `proxystack` 组。手工修权限必须用 `chmod 2770` 而不是 `chmod 770`。
- **普通文件不给组写位。** 写文件一律「建临时文件 + rename」，只需要目录写权限。
- **owner 一栏写 `*`**：文件可能由运维账号、root（sudo）或服务账号任一方创建，owner 本来就不唯一；决定服务读不读得到的是组。`RepairStandardMetadata` 以 root 运行时仍会把 owner 归一到 `proxystack:proxystack`，`doctor` 只校验组。

### 为什么 base dir、`bin/`、`geo/`、`downloads/` 不组可写

`rename(2)` 只检查**父目录**的写权限，文件自身的 mode 和 owner 完全不参与。所以父目录一旦组可写，组成员就能把 `bin/` 整个改名再建一个自己的，`bin/xray` 上的 `0750` 形同虚设——而那些二进制是 systemd 以 `proxystack` 身份执行的，等于把服务账号的代码执行权交给组成员。

装二进制和 geo 数据本来就是 `sudo psctl install/setup deps`，把这几个目录留在组外代价为零。`config.yaml` 在 base dir 下，因此**改全局配置仍需 sudo**；改 stack 配置不需要。

### 授权边界

`proxystack` 组成员 ≈ 代理服务配置和运行时数据的完全控制权：能增删用户、改路由、改 stack 配置。不能替换受管二进制，不能改 unit 文件（在 `/etc` 下，root-only），也不能借此提权到 root。

不需要这个能力就不要往组里加人——组默认只有服务账号自己。

### 存量升级

已有部署是 `0750`。升级后跑一次 `sudo psctl setup local`（幂等）即可把权限修到位；订阅服务同理跑 `sudo pssub setup local`。在此之前 `psctl doctor` 会报 `path mode mismatch`。

## 2. install/update 行为

目标：

- `mihomo`
- `xray`
- `geo`
- `all`
- `self` 仅用于 `update`

`install`：

- 目标已存在时跳过。
- 不安装系统服务文件。
- 不重启服务。

`update`：

- 强制重新下载或替换。
- 不自动重启服务。

`all`：

- 只展开为 `mihomo`、`xray`、`geo`。
- 不包含 `self`。

## 3. 下载源

托管源：

- `auto`
- `github`
- `r2`：预留源，未配置镜像域名时返回错误

普通源：

- 本地文件。
- `file://`
- `http://`
- `https://`

安全规则：

- 普通远端 URL 必须提供 sha256。
- DNS 解析到私网、本机、link-local、metadata 地址时拒绝。
- HTTP redirect 到禁止地址时拒绝。
- 不允许未识别 scheme。
- source 是目录时拒绝。

## 4. 归档处理

支持：

- `.gz`
- `.zip`
- `.tar`
- `.tar.gz`

安全规则：

- 拒绝绝对路径。
- 拒绝 `..`。
- 拒绝反斜杠路径。
- 拒绝 symlink 指向逃逸路径。
- 二进制归档无法唯一识别成员时要求 `archive_member`。

替换规则：

- 先写临时文件。
- 校验 sha256。
- 设置权限。
- 原子 rename。
- geo 多文件替换失败时回滚旧文件。

## 5. 服务管理文件

服务管理器通过 CLI 全局 `--service-manager auto|systemd|launchd` 指定，默认 `auto`。`auto` 在 Linux 解析为 `systemd`，在 macOS 解析为 `launchd`。

### 5.1 systemd unit

unit 文件：

- `/etc/systemd/system/proxystack-xray@.service`
- `/etc/systemd/system/proxystack-clash@.service`
- `/etc/systemd/system/proxystack-sub.service`

hardening：

```ini
User=proxystack
Group=proxystack
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
```

xray：

```ini
ExecStart=/opt/proxystack/bin/xray run -config /opt/proxystack/runtime/generated/xray/%i.json
```

mihomo：

```ini
ExecStart=/opt/proxystack/bin/mihomo -d /opt/proxystack/runtime/mihomo/%i -f /opt/proxystack/runtime/generated/mihomo/%i.yaml
```

sub：

```ini
ExecStart=/usr/local/bin/pssub --base-dir /opt/proxystack-sub serve
```

`psctl` 和 `pssub` 独立部署，订阅服务 unit 可由 `pssub setup local` 或 `pssub service install` 生成，默认 base dir 为 `/opt/proxystack-sub`。

`ReadWritePaths`：

- xray/clash 仅包含 runtime 相关目录。
- sub 仅包含 sub data dir。

systemd 后端写入 unit 文件后执行 `systemctl daemon-reload`，避免新 unit 或更新后的 unit 未被 systemd 感知。

### 5.2 launchd plist

plist 文件默认写入：

```text
/Library/LaunchDaemons
```

label：

- `com.proxystack.sub`
- `com.proxystack.xray.<stack>`
- `com.proxystack.mihomo.<stack>`

plist 行为：

- `RunAtLoad=false`，安装 plist 后不会隐式自启。
- 不固定写入 `UserName` 或 `GroupName`。
- `sub` 运行命令为 `/usr/local/bin/pssub --base-dir <base-dir> serve`，默认 `<base-dir>` 为 `/opt/proxystack-sub`。
- xray/mihomo 使用 `<base-dir>/bin`、`runtime/generated` 和 `runtime/mihomo` 中的实际路径。
- `setup local` 写入服务文件时会清理目标范围内不再期望的旧 proxystack plist。
- `uninstall` 删除 plist 前会先对已加载 job 执行 `bootout`。

## 6. systemctl/journalctl 与 launchctl/log

调用方式：

- 必须使用参数数组。
- 禁止 `sh -c`。

错误处理：

- 返回非零时展示 stdout/stderr 摘要。
- 权限错误不可吞掉。
- `systemctl status` inactive 返回码 3 不一定表示 CLI 失败，应按当前 Python 行为兼容。
- `launchctl print` 返回服务未加载时应按未运行处理，其他错误保留 stdout/stderr 摘要。

多 unit logs：

```bash
journalctl -u proxystack-xray@usa1.service -u proxystack-clash@usa1.service --no-pager -n 100 -f
```

launchd 日志通过 `log show` 或 `log stream` 查询，并按 proxystack 相关进程名过滤。

## 7. 日志脱敏

禁止记录：

- token。
- password。
- uuid 完整值。
- mihomo controller secret。
- 完整订阅内容。
- 下载 URL 中的认证信息。

允许记录：

- 文件路径。
- 服务名。
- hash 前 8 位。
- 错误类型。
- 统计数量。

## 8. 供应链安全

新增 Go 依赖必须说明：

- 用途。
- 是否可用标准库替代。
- 是否为成熟维护项目。
- 是否进入生产路径。

实现完成后建议执行：

```bash
go test ./...
govulncheck ./...
```
