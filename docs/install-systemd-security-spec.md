# 安装、systemd 与安全规格

生成日期：2026-06-17

本文定义 Go 版安装更新、systemd 管理和安全边界。

## 1. 目录权限

默认用户和组：

```text
proxystack:proxystack
```

权限：

| 路径 | 权限 | owner |
| --- | --- | --- |
| `/opt/proxystack` | `0750` | `proxystack:proxystack` |
| `.venv/` 或 Go binary 目录 | `0750` | `proxystack:proxystack` |
| `bin/` | `0750` | `proxystack:proxystack` |
| `geo/` | `0750` | `proxystack:proxystack` |
| `runtime/` | `0750` | `proxystack:proxystack` |
| `publish/` | `0750` | `proxystack:proxystack` |
| `downloads/` | `0750` | `proxystack:proxystack` |
| `sub/` | `0750` | `proxystack:proxystack` |
| `sub/` 下所有子目录 | `0750` | `proxystack:proxystack` |
| `sub/` 下所有普通文件 | `0640` | `proxystack:proxystack` |
| `config.yaml` | `0640` | `proxystack:proxystack` |
| `stacks/*.yaml` | `0640` | `proxystack:proxystack` |
| `bin/mihomo`、`bin/xray` | `0750` | `proxystack:proxystack` |
| `geo/*` | `0640` | `proxystack:proxystack` |

未变化生成文件也允许修复 metadata。

## 2. install/update 行为

目标：

- `mihomo`
- `xray`
- `geo`
- `all`
- `self` 仅用于 `update`

`install`：

- 目标已存在时跳过。
- 不安装 systemd unit。
- 不重启服务。

`update`：

- 强制重新下载或替换。
- 不自动重启服务。
- 输出建议服务计划。

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

## 5. systemd unit

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
ExecStart=/usr/local/bin/ps-sub --base-dir /opt/proxystack serve
```

`ReadWritePaths`：

- xray/clash 仅包含 runtime 相关目录。
- sub 仅包含 sub data dir。

## 6. systemctl/journalctl

调用方式：

- 必须使用参数数组。
- 禁止 `sh -c`。

错误处理：

- 返回非零时展示 stdout/stderr 摘要。
- 权限错误不可吞掉。
- `systemctl status` inactive 返回码 3 不一定表示 CLI 失败，应按当前 Python 行为兼容。

多 unit logs：

```bash
journalctl -u proxystack-xray@usa1.service -u proxystack-clash@usa1.service --no-pager -n 100 -f
```

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
