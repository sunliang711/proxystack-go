# 配置与数据 Schema 规格

生成日期：2026-06-17

本文定义 Go 版必须实现的配置和数据契约。字段名以 YAML/JSON 文件中的 snake_case 为准；Go struct 名称可按 Go 命名规范转换。

## 1. Schema 严格度

| Schema | 是否允许未知字段 | 原因 |
| --- | --- | --- |
| `GlobalConfig` | 允许 | 当前 Python 领域模型允许扩展字段，避免破坏用户配置 |
| `Stack`、`Xrelay`、`Clash`、`Inbound`、`Outbound`、`Upstream`、`Group` | 允许 | 保留未来扩展和用户附加字段 |
| `SubServerConfig`、`ManagedConfig` | 禁止 | 订阅服务自身配置，误写字段应 fail fast |
| `SubscriptionInput`、`SubscriptionNode`、`SubscriptionAuth` | 禁止 | agent/sub 之间的传输契约 |
| `SubscriptionIndex`、`SubscriptionAccess` | 禁止 | HTTP 鉴权和内存索引契约 |
| `BundleManifest` | 禁止 | zip 安全校验契约 |
| `NativeBackupManifest` | 禁止 | 配置备份契约 |

Go 实现建议：

- 领域配置模型用 `Extra map[string]any` 或保留 `yaml.Node` 中未知字段。
- 传输契约模型使用严格解码，发现未知字段直接返回错误。
- 所有错误都要带文件路径或 schema 名称，便于用户定位。

## 2. 全局配置 `config.yaml`

文件位置：由 `psctl --base-dir DIR` 决定，固定为 `<base-dir>/config.yaml`，默认 `/opt/proxystack/config.yaml`。

| 字段 | 类型 | 必填 | 默认值 | 校验 |
| --- | --- | --- | --- | --- |
| `version` | int | 是 | `1` | 只能为 `1` |
| `paths` | object | 否 | 见下表 | 每个路径可为绝对路径或相对 `--base-dir` |
| `external_host` | string | 否 | 无 | `sub export` 前必须设置；可为域名或公网 IP |
| `subscription` | object | 否 | `{source: local}` | `source` 只能为 `local` |
| `users` | list | 否 | `[]` | 全局订阅用户档案，供 stack `user_refs` 引用 |
| `port_ranges` | object | 是 | 无 | 所有范围为 `start-end`，且 `start <= end` |
| `defaults` | object | 否 | 见下文 | 日志级别、Xray API/stats/policy 默认值 |
| `security` | object | 否 | 见下文 | 安全开关 |
| `install` | object | 否 | 见下文 | 安装源配置 |

### 2.1 `paths`

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `bin` | `bin` | mihomo/xray 二进制目录 |
| `geo` | `geo` | geo 数据目录 |
| `stacks` | `stacks` | stack 配置目录 |
| `runtime` | `runtime` | manifest、锁文件和运行时目录 |
| `generated` | `runtime/generated` | Xray/mihomo/sub 生成文件目录 |
| `publish` | `publish` | 订阅包和备份包输出目录 |
| `downloads` | `downloads` | 下载缓存目录 |
| `sub` | `sub` | 本地订阅服务数据目录 |

解析规则：

- 绝对路径原样使用。
- 相对路径按 `--base-dir` 拼接。
- `sub` 不得等于 `runtime` 或 `stacks`，避免 agent/sub 写入边界混淆。

### 2.2 `port_ranges`

| 字段 | 用途 |
| --- | --- |
| `xrelay_inbound` | `add` 自动分配 xrelay inbound 端口 |
| `clash_socks` | `add` 自动分配 mihomo socks listener 端口 |
| `clash_http` | `add` 自动分配 mihomo HTTP listener 端口 |
| `xray_api_range` | `add` 自动分配 Xray API 端口 |
| `clash_controller` | `add` 自动分配 mihomo controller 端口 |

端口范围支持 YAML 字符串：

```yaml
xrelay_inbound: 24000-24999
```

或结构化对象：

```yaml
xrelay_inbound:
  start: 24000
  end: 24999
```

端口必须在 `1-65535` 范围内。

### 2.3 `defaults`

| 字段 | 默认值 | 校验 |
| --- | --- | --- |
| `defaults.clash.mode` | `Rule` | `Rule`、`Global`、`Direct` |
| `defaults.clash.loglevel` | `info` | `debug`、`info`、`warning`、`error`、`silent` |
| `defaults.clash.rule_profile` | `default` | 首期只支持 `default` |
| `defaults.xrelay.loglevel` | `warning` | `debug`、`info`、`warning`、`error`、`none` |
| `defaults.xrelay.api.enabled` | `true` | bool |
| `defaults.xrelay.api.tag` | `api` | 标识符 |
| `defaults.xrelay.api.listen` | `127.0.0.1:10085` | 只能 loopback |
| `defaults.xrelay.api.services` | `[StatsService]` | 非空字符串列表 |
| `defaults.xrelay.stats.enabled` | `true` | bool |
| `defaults.xrelay.policy.enabled` | `true` | bool |

### 2.4 `security`

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `require_auth_for_public_socks_http` | `true` | 非回环 socks/http inbound 必须带鉴权 |
| `allow_noauth_public` | `false` | 是否允许公开 noauth，默认禁止 |

首期不得通过环境变量或隐式配置关闭安全能力。

### 2.5 `install`

每个目标结构一致：

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `version` | string | `latest` | 托管源版本 |
| `source` | string | `auto` | `auto`、`github`、本地文件、`file://`、`http(s)://`；`r2` 未配置时返回错误 |
| `sha256` | string | 无 | 64 位 hex 摘要 |
| `archive_member` | string | 无 | zip/tar 中指定成员 |

目标：

- `install.mihomo`
- `install.xray`
- `install.geo`

## 3. Stack 配置 `stacks/<name>.yaml`

| 字段 | 类型 | 必填 | 默认值 | 校验 |
| --- | --- | --- | --- | --- |
| `name` | string | 是 | 无 | 必须与文件名一致 |
| `enabled` | bool | 否 | `true` | disabled stack 不参与默认生命周期 |
| `role` | string | 否 | `edge` | `edge`、`auto` |
| `labels` | string list | 否 | `[]` | 用于展示 |
| `xrelay` | object | 是 | 无 | 见下文 |
| `clash` | object | 是 | 无 | 见下文 |

## 4. Xrelay Schema

### 4.1 `xrelay`

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `enabled` | bool | 否 | `true` | false 时不生成 Xray |
| `loglevel` | string | 否 | 继承全局 | Xray loglevel |
| `api` | object | 否 | 继承全局 | Xray API |
| `stats` | object | 否 | 继承全局 | Xray stats |
| `policy` | object | 否 | 继承全局 | Xray policy |
| `outbound` | object | 是 | 无 | egress 定义 |
| `inbounds` | list | 是 | 无 | 至少一个 inbound |

### 4.2 Inbound 通用字段

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `name` | string | 是 | 无 | stack 内唯一 |
| `protocol` | string | 是 | 无 | `vmess`、`shadowsocks`、`socks5`、`http` |
| `listen` | string | 否 | `0.0.0.0` | host，不含端口 |
| `port` | int | 是 | 无 | `1-65535` |
| `udp` | bool | 否 | `false` | 适用于 vmess/shadowsocks/socks5；显式配置 true/false 时写入订阅 |
| `auth` | object | 协议相关 | 无 | socks/http 鉴权 |
| `user` | string | 单用户订阅需要 | 无 | 订阅用户 |
| `server` | string | 否 | `external_host` | 订阅 server 覆盖 |
| `remark` | string | 否 | 无 | 订阅基础备注；最终展示名见生成器规格 |
| `display_template` | string | 否 | 无 | 订阅节点展示名模板；见生成器规格模板语法 |
| `region` | string | 否 | 无 | 两位大写字母 |
| `tag` | string | 否 | `<protocol>:<port>:<name>` | Xray tag 和订阅 tag 基础 |
| `sub` | bool | 是 | 无 | 是否进入订阅；必须显式声明，避免订阅暴露语义被默认值悄悄改变 |
| `network` | string | vmess 必填 | 无 | vmess network |
| `method` | string | shadowsocks 必填 | 无 | shadowsocks method |
| `cipher` | string | shadowsocks 兼容 | 无 | 等价于 method |
| `password` | string | shadowsocks 必填 | 无 | SS 密码 |
| `user_refs` | list | vmess/shadowsocks 推荐 | 无 | 引用 `config.yaml users` 的订阅用户档案 |
| `users` | list | 否 | 无 | legacy/in-memory 多用户凭据；新配置推荐使用 `user_refs` |

### 4.3 Auth

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | `noauth`、`password` |
| `username` | string | password 必填 | 用户名 |
| `password` | string | password 必填 | 密码 |

公开 socks/http 约束：

- `listen` 为 `0.0.0.0`、公网 IP 或非 loopback host 且 `auth.type=noauth` 时默认失败。
- `security.allow_noauth_public=true` 时才允许显式风险放行。

### 4.4 InboundUser

| 字段 | 类型 | 必填 | 适用 |
| --- | --- | --- | --- |
| `user` | string | 是 | vmess、shadowsocks 多用户 |
| `uuid` | string | vmess 必填 | vmess |
| `password` | string | shadowsocks 多用户必填 | shadowsocks |
| `method` | string | 否 | 传统 shadowsocks |
| `cipher` | string | 否 | 传统 shadowsocks |
| `remark` | string | 否 | 订阅基础备注 |
| `display_template` | string | 否 | 订阅节点展示名模板；覆盖 inbound 级模板 |
| `tag` | string | 否 | 订阅 tag 覆盖 |
| `email` | string | 否 | Xray 用户统计 |

新配置推荐使用 `config.yaml users` + `xrelay.inbounds[].user_refs`。`InboundUser` 仍作为展开后的内部模型和 legacy 配置入口保留。

### 4.4.1 Global UserProfile 与 user_refs

`config.yaml` 支持全局 `users` 列表，唯一键为 `(user, profile)`；`profile` 为空时按 `default` 处理。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `user` | string | 是 | 订阅用户入口 |
| `profile` | string | 否 | 同一 user 的凭据档案名 |
| `uuid` | string | vmess 引用时必填 | VMess 用户 UUID |
| `password` | string | shadowsocks 引用时必填 | Shadowsocks 用户密码 |
| `email` | string | 否 | Xray 用户统计 email |
| `remark` | string | 否 | 订阅节点备注默认值 |
| `display_template` | string | 否 | 订阅节点展示名模板默认值 |
| `tag` | string | 否 | 订阅 tag 覆盖 |

`xrelay.inbounds[].user_refs` 引用全局用户档案，支持字符串简写：

```yaml
user_refs: [alice]
```

也支持对象写法，并可覆盖 `uuid/password/email/remark/display_template/tag`：

```yaml
user_refs:
  - user: alice
    profile: tokyo
    remark: Tokyo VMess
```

同一个 inbound 内 `user_refs` 不能与旧 `user` / `users` 同时配置。

### 4.5 Outbound

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | `clash`、`socks5`、`http`、`direct` |
| `ref` | string | clash 必填 | 例如 `usa1.clash.socks` |
| `server` | string | socks5/http 必填 | 外部代理地址 |
| `port` | int | socks5/http 必填 | 外部代理端口 |
| `username` | string | 否 | 外部代理用户名 |
| `password` | string | 否 | 外部代理密码 |
| `private_direct` | bool | 否 | 仅支持 socks5/http；为 true 时 Xray 私网目标直连 |

`direct` 不需要额外字段。

## 5. Clash Schema

### 5.1 `clash`

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `enabled` | bool | 否 | `true` | false 时不生成 mihomo |
| `mode` | string | 否 | 继承全局 | `Rule`、`Global`、`Direct` |
| `loglevel` | string | 否 | 继承全局 | mihomo log-level |
| `controller` | object | 是 | 无 | external-controller |
| `listeners` | object | 是 | 无 | socks/http listener |
| `upstreams` | list | 是 | 无 | proxies 来源 |
| `groups` | list | 是 | 无 | proxy-groups |
| `rules` | object | 否 | `{profile: default, final: AllProxy}` | rules |

### 5.2 Controller

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `listen` | string | 是 | `host:port` |
| `secret` | string | 是 | mihomo controller secret，必须非空 |

### 5.3 Listener

P0 支持：

- `listeners.socks`：最多 1 个，必须存在。
- `listeners.http`：最多 1 个，可选。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | listener 名称 |
| `listen` | string | 是 | host |
| `port` | int | 是 | port |
| `users` | list 或 null | 否 | null 表示跟随全局 authentication，`[]` 表示跳过认证，非空表示 listener 认证 |

`listeners.mixed` 必须在校验阶段拒绝，不能静默转换。

### 5.4 Upstream

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | proxy name |
| `type` | string | 是 | `raw`、`xrelay-socks5` |
| `config` | map | raw 必填 | 原样写入 mihomo proxy，并覆盖 name |
| `ref` | string | xrelay-socks5 必填 | `<stack>.<inbound>` |

P0 不支持 `xrelay-http`。

### 5.5 Group

| 字段 | 类型 | 必填 | 默认值 |
| --- | --- | --- | --- |
| `name` | string | 是 | 无 |
| `type` | string | 是 | `select`、`url-test`、`load-balance`、`fallback` |
| `proxies` | list | 是 | 无 |
| `url` | string | url-test/load-balance/fallback 必填 | 无 |
| `interval` | int | url-test/load-balance/fallback 必填 | 无 |
| `strategy` | string | load-balance 可选 | `consistent-hashing` |

`proxies` 可引用：

- 已存在 upstream proxy。
- 已存在 group。
- 内置 `DIRECT`、`REJECT`。

### 5.6 Rules

| 字段 | 类型 | 必填 | 默认值 |
| --- | --- | --- | --- |
| `profile` | string | 否 | `default` |
| `final` | string | 否 | `AllProxy` |
| `extra` | string list | 否 | `[]` |

校验：

- `profile` 首期只支持 `default`。
- `final` 必须引用内置策略、proxy 或 group。
- `extra` 每条规则最后一个目标必须存在。

## 6. 订阅服务配置 `config.yaml`

默认路径：`/opt/proxystack-sub/config.yaml`。实际路径由 `pssub --base-dir DIR` 决定，固定为 `<base-dir>/config.yaml`；运行时 sub root 固定为 `<base-dir>`。

| 字段 | 类型 | 必填 | 默认值 |
| --- | --- | --- | --- |
| `listen` | string | 否 | `0.0.0.0:3003` |
| `access` | object | 否 | `{type: none}` |
| `templates_dir` | path | 否 | 无 |
| `watch_interval` | float | 否 | `2.0` |
| `watch_debounce` | float | 否 | `0.3` |
| `managed_config` | object | 否 | 见下文 |

### 6.1 Access

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | `none`、`token` |
| `token` | string | token 模式必填 | 访问令牌 |

token 只存放在 sub config 中，不写入 bundle 和 input。

### 6.2 ManagedConfig

| 字段 | 类型 | 默认值 | 校验 |
| --- | --- | --- | --- |
| `enabled` | bool | `true` | bool |
| `public_base_url` | string | 无 | 只允许 http/https，不允许 query/fragment |
| `interval` | int | `86400` | `>0` |
| `strict` | bool | `true` | bool |

## 7. Subscription Input

文件扩展名：`.yaml`、`.yml`、`.json`

```yaml
input_schema: proxystack.subscription-input
input_version: 1
source: usa1
generated_at: "2026-06-05T12:00:00+08:00"
external_host: proxy.example.com
nodes: []
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `input_schema` | string | 推荐 | `proxystack.subscription-input`；缺失时按兼容 v1 输入读取 |
| `input_version` | int | 是 | `1` |
| `source` | string | 是 | 来源 stack 或手工来源 |
| `generated_at` | string | 是 | ISO 时间 |
| `external_host` | string | 否 | 文件级订阅 server 缺省值；仅在 `nodes[].server` 缺失或为空时使用，不覆盖局部 server |
| `nodes` | list | 是 | 订阅节点 |

### 7.1 SubscriptionNode

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 全局唯一 |
| `user` | string | 是 | HTTP 路由过滤用户 |
| `direct` | bool | 否 | 默认 `false`；为 `true` 时允许节点携带 Clash/Mihomo 原生字段并在 Clash/Premium Clash 输出中直通 |
| `protocol` | string | 是 | 默认节点支持 `vmess`、`shadowsocks`、`socks5`、`http`；`direct: true` 可由 `type` 推导为 Clash/Mihomo 原生协议 |
| `server` | string | 条件必填 | 客户端连接地址；有值时优先于文件级 `external_host`，否则要求文件级 `external_host` 有值 |
| `port` | int | 是 | 客户端连接端口 |
| `tag` | string | 是 | 唯一 tag |
| `remark` | string | 是 | 展示名 |
| `region` | string | 否 | 两位大写字母 |
| `auth` | object | 协议相关 | 凭据 |
| `network` | string | vmess 需要 | vmess network |
| `method` | string | shadowsocks 需要 | shadowsocks method |
| `udp` | bool | 否 | 适用于 vmess/shadowsocks/socks5；显式 true/false 会输出到 Clash/Premium Clash |

合并规则：

- 加载 input 时先用文件级 `external_host` 补齐缺失或空的 `nodes[].server`。
- 文件按文件名排序。
- `nodes[].id` 全局唯一。
- 同一个 `user` 下 `proxy name` 不允许重复。
- 不同 user 可以存在相同 proxy name。
- 默认节点严格拒绝未知字段；`direct: true` 节点允许节点级自定义字段，Clash/Premium Clash 按原字段输出。
- Surge 只输出已确认支持的节点；当前对 vmess 仅支持 raw/tcp 和 ws/websocket，并对 `tls`、`skip-cert-verify`、`servername`、`ws-opts.path` 和 `ws-opts.headers` 做兼容映射；不支持或不确认的节点会被跳过并记录 warning。

## 8. Subscription Index

```json
{
  "access": {"type": "none"},
  "sources": [],
  "users": {},
  "generated_at": "..."
}
```

字段：

- `access`：只来自 pssub `config.yaml`，agent 预览默认 `none`。
- `sources`：来源摘要，稳定排序。
- `users`：按用户分组后的节点列表。
- `generated_at`：索引生成时间；测试可注入固定时钟。

## 9. Bundle Manifest

```json
{
  "bundle_schema": "proxystack.sub-bundle",
  "bundle_version": 1,
  "source": "all",
  "generated_at": "...",
  "inputs_sha256": {
    "usa1.yaml": "..."
  },
  "template_version": "builtin-v1",
  "access": {"type": "none"}
}
```

约束：

- zip 只允许 `manifest.json` 和 `inputs/*.yaml|*.yml|*.json`。
- `inputs_sha256` 的 key 必须与 zip 中 inputs 文件集合完全一致。
- hash mismatch 时不能写入任何 input。
- `access` 当前固定为 `none`，导入时不能用它配置 token。

## 10. Native Backup Manifest

用途：agent 配置备份恢复。

约束：

- schema 与 bundle manifest 不同，必须互相拒绝。
- 只包含 `config.yaml` 和 `stacks/*.yaml`。
- 不包含 runtime/generated、manifest、downloads、pssub inputs。
- 导入前先完整校验 manifest、hash、schema 和目标覆盖策略。
