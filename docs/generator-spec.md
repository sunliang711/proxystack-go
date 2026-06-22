# 生成器与 Runtime 规格

生成日期：2026-06-17

本文定义 Go 版生成器、manifest 和 runtime plan 的精确行为。

## 1. 总体原则

输入：

- agent 生成器只读取 `config.yaml` 和 `stacks/*.yaml`。
- `render sub --input-dir` 和 `ps-sub serve` 只读取 inputs 和 sub config。

输出：

- Xray JSON：`<generated>/xray/<stack>.json`
- mihomo YAML：`<generated>/mihomo/<stack>.yaml`
- 订阅 input：`<generated>/sub/inputs/<source>.yaml`
- 订阅 index：`<generated>/sub/index.json`
- runtime manifest：`<runtime>/manifest.json`
- 订阅发布包：`<publish>/sub-bundle.zip` 或 `<publish>/<stack>-sub-bundle.zip`

稳定性要求：

- 同一份输入多次生成，输出内容必须稳定。
- 未变化文件不改写。
- 生成前必须完整校验。
- `start` 不隐式生成订阅发布包。
- `sub export` 不直接写 ps-sub `inputs`。

## 2. Runtime Plan

Runtime Plan 是 `check/start/restart` 的共同中间结果。

字段：

- `config`：解析后的全局配置。
- `stack_set`：全局配置和 enabled stack 合并后的模型。
- `scope`：target 解析后的组件集合。
- `generated_files`：期望存在的生成文件。
- `changes`：与上次 manifest 或磁盘状态对比后的变化。
- `dependency_plan`：服务启动/重启顺序。

FileChange action：

| action | 含义 |
| --- | --- |
| `create` | 目标文件不存在，需要写入 |
| `update` | 目标文件存在但 sha256 不同，需要写入 |
| `delete` | manifest 中存在但本次 scope 下不应再存在，需要删除 |
| `no-change` | 内容相同，不写入 |

`check` 只输出 Runtime Plan，不执行 apply。

`start/restart` 执行 apply：

1. 校验配置。
2. 解析 target。
3. 检查所需二进制存在且可执行。
4. 生成文件内容。
5. 计算 diff。
6. 原子写入变化文件。
7. 写入 manifest。
8. 调用 systemd。

## 3. Manifest

路径：`<runtime>/manifest.json`

```json
{
  "manifest_version": 1,
  "config_hash": "...",
  "stack_hashes": {
    "usa1": "..."
  },
  "generated_at": "2026-06-05T12:00:00+08:00",
  "files": [
    {
      "path": "/opt/proxystack/runtime/generated/xray/usa1.json",
      "relative_path": "generated/xray/usa1.json",
      "sha256": "...",
      "service": "proxystack-xray@usa1.service"
    }
  ]
}
```

规则：

- `manifest_version` 固定为 `1`。
- `config_hash` 来自规范化后的全局配置输入。
- `stack_hashes` 只包含参与本次生成的 stack。
- `files` 按 `relative_path` 稳定排序。
- `generated_at` 在文件内容无变化时应复用旧 manifest 时间，避免无意义差异。
- manifest 写入必须原子替换。

服务重启判断：

- 只有 action 为 `create`、`update`、`delete` 的 FileChange 关联服务进入 changed services。
- `no-change` 不触发重启。
- target scope 外的服务不得因为本次操作被重启。

## 4. Xray 生成器

每个 enabled 且 `xrelay.enabled=true` 的 stack 生成一个 JSON。

输出顺序建议：

1. `log`
2. `api`
3. `stats`
4. `policy`
5. `inbounds`
6. `outbounds`

### 4.1 log

`log.loglevel` 来源：

1. `stack.xrelay.loglevel`
2. `defaults.xrelay.loglevel`
3. `warning`

### 4.2 api/stats/policy

`api.enabled=true` 时生成：

- `api.tag`
- `api.listen`
- `api.services`

`stats.enabled=true` 时生成 `stats: {}`。

`policy.enabled=true` 时生成 levels 和 system。显式字段覆盖默认统计字段。

API listen 必须是 loopback。

### 4.3 inbounds

通用 tag：

```text
<protocol>:<port>:<name>
```

vmess：

- 一个 inbound 对应多个 client。
- `settings.clients[].id` 来自 `users[].uuid`。
- `alterId` 固定 `0`。
- `email` 来自 `users[].email`，缺省使用 `users[].user`。

shadowsocks：

- `settings.method` 来自 `method` 或 `cipher`。
- `settings.password` 来自 inbound password。
- 多用户写入 `settings.users[]`。
- SS2022 订阅密码使用 `ServerPassword:UserPassword`，但 Xray 配置仍按 Xray 规则输出 server/user password。

socks5/http：

- `auth.type=noauth` 时生成 noauth。
- `auth.type=password` 时生成 accounts。

### 4.4 outbound

所有 xrelay outbound tag 固定为：

```text
egress-<stack>
```

`type=clash`：

- ref 必须指向目标 stack 的唯一 socks listener。
- 如果目标 listener listen 为 `0.0.0.0` 或 `::`，内部连接地址归一化为 `127.0.0.1`。
- 如果目标 listener `users` 非空，使用第一个用户作为 Xray socks outbound 账号。

`type=socks5` 和 `type=http`：

- 输出外部代理 server/port。
- username/password 可选。

`type=direct`：

- 输出 freedom outbound。

## 5. mihomo 生成器

每个 enabled 且 `clash.enabled=true` 的 stack 生成一个 YAML。

基础字段：

- `allow-lan: false`
- `mode`
- `log-level`
- `ipv6: true`
- `listeners`
- `external-controller`
- `secret`
- `proxies`
- `proxy-groups`
- `rules`

### 5.1 listeners

P0 必须使用高级 `listeners`。

- socks listener 必须存在且最多一个。
- HTTP listener 最多一个。
- mixed listener 必须拒绝。
- `users` 缺失、空数组、非空数组三种语义必须区分。

### 5.2 upstreams

`raw`：

- `config` 原样写入 `proxies`。
- `name` 以 upstream name 为准，覆盖 config 内 name。

`xrelay-socks5`：

- ref 解析到目标 xrelay socks5 inbound。
- server 使用 `127.0.0.1`，即使 inbound listen 是 `0.0.0.0`。
- port 使用 inbound port。
- username/password 来自 inbound auth。
- udp 跟随 inbound udp。

### 5.3 groups

支持：

- `select`
- `url-test`
- `load-balance`
- `fallback`

校验：

- group proxies 必须引用已存在 proxy、group 或内置策略。
- `url-test`、`load-balance`、`fallback` 必须有 `url` 和 `interval`。
- load-balance 缺省 strategy 为 `consistent-hashing`。

### 5.4 rules

生成顺序：

1. `rules.extra`
2. default profile 规则
3. `MATCH,<rules.final>`

default profile：

```text
DOMAIN-SUFFIX,local,DIRECT
DOMAIN,localhost,DIRECT
IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
MATCH,<rules.final>
```

## 6. 订阅生成器

订阅只来自 enabled stack 的 `xrelay.inbounds[] where sub == true`。

不读取：

- `clash.upstreams`
- `clash.groups`
- `clash.rules`
- `clash.mode`
- `clash.controller`

### 6.1 节点字段映射

| 字段 | 来源 |
| --- | --- |
| `server` | inbound.server 或 global external_host |
| `port` | inbound.port |
| `user` | inbound.user 或 users[].user |
| `tag` | inbound.tag 或生成 tag；多用户可用 users[].tag 覆盖 |
| `remark` | `{user}@{stack}-{protocol}:{port}-{remark}` |
| `region` | inbound.region |
| `udp` | inbound 显式配置；仅 vmess/shadowsocks/socks5 支持，true/false 均会传递 |
| `auth` | 协议凭据 |

vmess 多用户：

- 每个 user 生成一个节点。
- uuid 来自 `users[].uuid`。

shadowsocks 多用户：

- 每个 user 生成一个节点。
- SS2022 节点密码为 `ServerPassword:UserPassword`。

socks5/http：

- `sub=true` 时订阅节点必须携带账号密码。

### 6.2 input/index

`render_stack_input`：

- 指定 stack 时只生成该 stack input。
- all export 时按 stack 拆分多个 input。

`merge_inputs`：

- 合并前先应用 input 文件级 `external_host`，仅补齐缺失或空的 `nodes[].server`，不覆盖局部 server。
- 文件名排序。
- 校验重复 node id。
- 校验同 user 下重复 proxy name。
- 运行期 reload 失败保留旧 index。

### 6.3 模板

必须保持现有模板查找顺序：

1. `templates_dir/sub/<template>`
2. `templates_dir/<template>`
3. `<data_dir>/templates/sub/<template>`
4. 内置模板

默认模板名：

- `clash.yaml.j2`
- `premium-clash.yaml.j2`
- `surge.conf.j2`

模板上下文：

- `user`
- `generated_at`
- `sources`
- `nodes`
- `proxies`
- `proxy_names`
- `proxy_groups`
- `clash_rules`
- `surge_proxy_lines`
- `surge_proxy_names`
- `surge_region_groups`
- `surge_rules`
- `test_url`
- `surge_skip_proxy`
- `surge_proxylist_icon_url`
- `surge_auto_icon_url`
- Surge 额外：`managed_config_url`、`managed_config_interval`、`managed_config_strict`

兼容要求：

- Go 版不能无声明破坏用户 `.j2` 模板。
- 如果不实现 Jinja2 兼容，必须提供模板迁移工具和 breaking change 文档。

## 7. Bundle 生成和导入

bundle 结构：

```text
sub-bundle.zip
  manifest.json
  inputs/<stack>.yaml
```

写入规则：

- 先生成所有 input。
- 计算每个 input sha256。
- 写 manifest。
- zip 内路径使用 POSIX slash。

导入规则：

1. 打开 zip。
2. 校验成员路径。
3. 校验 manifest schema 和 version。
4. 校验 input 文件集合与 manifest 一致。
5. 校验 sha256。
6. 校验每个 input schema。
7. 校验合并后无重复节点或 proxy name 冲突。
8. 全部成功后原子写入。

禁止：

- 绝对路径。
- `..`。
- 反斜杠路径。
- 未知成员。
- native backup schema。

## 8. `ps-sub serve` 运行时

启动流程：

1. 加载 sub config。
2. 应用 CLI 显式 override。
3. 扫描 inputs。
4. 构建内存 index。
5. 启动 Gin HTTP server。
6. 启动 watcher。

路由：

- `GET /health`
- `GET /sub/:user?token=...`
- `GET /sub/:token/:user`
- `GET /premium_sub/:user?token=...`
- `GET /premium_sub/:token/:user`
- `GET /surge_sub/:user?token=...`
- `GET /surge_sub/:token/:user`

错误：

- index 不可用：503
- token 缺失：401
- token 错误：403
- 用户不存在：404
- 模板错误：503

错误响应结构：

```json
{
  "error": {
    "code": "not_found",
    "message": "subscription not found"
  }
}
```

健康检查：

```json
{
  "status": "ok",
  "index": true,
  "users": ["alice"]
}
```

## 9. 文件写入规范

所有生成文件写入：

- 先写同目录临时文件。
- fsync 后 rename。
- 写入后修复权限和 owner。
- 内容不变时不改写，但可修复 metadata。

权限：

- 目录：`0750`
- 配置文件：`0640`
- 代理核心二进制：`0750`
- geo 数据：`0640`
