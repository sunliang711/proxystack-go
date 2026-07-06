# 订阅 HTTP 服务规格

生成日期：2026-06-17

本文定义 `pssub serve` 的 HTTP 行为、鉴权、状态管理、导入接口和 watcher 语义。

## 1. 服务边界

`pssub` 只允许读取：

- `config.yaml`
- `inputs/*.yaml|*.yml|*.json`
- 可选模板目录

禁止读取：

- agent `config.yaml`
- `stacks/*.yaml`
- `runtime/manifest.json`
- clash upstream、rules、controller 配置

`import_api.enabled=true` 时允许写入：

- `<base-dir>/.imports/*.zip` 临时上传文件，处理完成后删除。
- `<base-dir>/inputs`，仅通过订阅 bundle 导入流程写入。

## 2. 启动流程

1. 解析全局 `--base-dir` 以及 `--listen`、`--host`、`--port`。
2. 将 sub root 固定为 `<base-dir>`，sub config 固定为 `<base-dir>/config.yaml`。
3. 加载 sub config；如果默认文件不存在，使用默认配置。
4. 应用 CLI override。
5. 扫描 `<base-dir>/inputs`。
6. 校验所有 input。
7. 构建内存 index。
8. 创建订阅 Gin HTTP server。
9. 创建 watcher。
10. 监听订阅 HTTP 地址。
11. `import_api.enabled=true` 时监听独立 admin HTTP 地址，只绑定 `import_api.listen`。
12. 启动 watcher 和 HTTP server。

启动阶段任何 input 非法，服务启动失败。
如果 admin listener 启动失败，必须关闭已打开的订阅 listener 并停止 watcher。

## 3. 状态管理

推荐结构：

- `SubscriptionState`
- 内部使用 `sync.RWMutex` 或 `atomic.Value`。
- `snapshot()` 返回不可变 index 副本或只读引用。
- reload 只有在完整加载成功后才替换 index。

健康状态：

- `loaded`
- `users`
- `last_error`

reload 失败：

- 保留上一份可用 index。
- 更新 `last_error`。
- 日志记录错误类型和 input 目录。

## 4. 路由

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/health` | 健康检查 |
| GET | `/sub/:user` | Clash，query token 兼容 |
| GET | `/sub/:token/:user` | Clash，path token 推荐 |
| GET | `/premium_sub/:user` | Premium Clash，query token 兼容 |
| GET | `/premium_sub/:token/:user` | Premium Clash，path token 推荐 |
| GET | `/surge_sub/:user` | Surge，query token 兼容 |
| GET | `/surge_sub/:token/:user` | Surge，path token 推荐 |
| POST | `/admin/import-bundle?replace_all=true|false` | 仅 admin listener；导入订阅 bundle |

## 5. 鉴权

`access.type=none`：

- 不校验 token。
- 生产公网部署不推荐。

`access.type=token`：

- 支持 path token。
- 支持 query token。
- 缺失 token 返回 401。
- token 不匹配返回 403。

token 来源只来自 sub config，不来自 bundle manifest。

导入接口：

- 不使用 token。
- 仅在 `import_api.enabled=true` 时启动独立 admin listener。
- `import_api.listen` 必须是 loopback 或 `localhost`。
- handler 仍必须基于 `RemoteAddr` 做回环校验，不信任 `X-Forwarded-For` 或 `X-Real-IP`。

## 6. 响应

### 6.1 `/health`

成功：

```json
{
  "status": "ok",
  "index": true,
  "users": ["alice"]
}
```

存在 reload 错误：

```json
{
  "status": "error",
  "index": true,
  "users": ["alice"],
  "last_error": "..."
}
```

### 6.2 订阅响应

成功：

- Content-Type：`text/plain; charset=utf-8`
- body 为对应订阅文本。

错误：

```json
{
  "error": {
    "code": "not_found",
    "message": "subscription not found"
  }
}
```

错误码：

| HTTP | code | 场景 |
| --- | --- | --- |
| 401 | `unauthorized` | token 缺失 |
| 403 | `forbidden` | token 错误 |
| 404 | `not_found` | 用户不存在或无节点 |
| 503 | `index_unavailable` | index 不可用 |
| 503 | `template_error` | 模板不可用 |

### 6.3 `/admin/import-bundle`

请求：

- 方法：`POST`
- Query：`replace_all=true|false`，缺省等价于 `false`
- Body：`psctl sub export` 生成的 zip bundle
- Content-Type：不强制
- 上传 zip 大小和解压后的 input 总大小均受 `import_api.max_bundle_bytes` 限制

成功：

```json
{
  "status": "ok",
  "source": "all",
  "generated_at": "2026-06-05T12:00:00+08:00",
  "inputs": 1,
  "written": ["usa1.yaml"],
  "replaced": [],
  "removed": [],
  "replace_all": false,
  "reloaded": true
}
```

处理规则：

- 上传体先写入 `<base-dir>/.imports/*.zip`，目录权限建议 `0700`，临时文件权限建议 `0600`。
- 处理完成后删除临时 zip。
- 只接受 `proxystack.sub-bundle`。
- 复用 bundle 导入逻辑完整校验 zip、manifest、hash、解压后 input 总大小和 inputs 后再写入。
- 成功写入后必须立即 `Reload()`，reload 成功才返回 200。
- 并发导入必须串行化，避免同时替换 inputs。

错误码：

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | `bad_request` | `replace_all` 非 `true|false` |
| 400 | `invalid_bundle` | zip、manifest、schema、hash 或 input 校验失败 |
| 403 | `forbidden` | `RemoteAddr` 非回环地址 |
| 413 | `payload_too_large` | 超过 `max_bundle_bytes` |
| 500 | `upload_failed` | 临时文件写入失败 |
| 500 | `import_failed` | 导入阶段出现非校验类错误 |
| 500 | `reload_failed` | 写入后 reload 失败 |

## 7. Surge Managed Config

`managed_config.enabled=true` 时，Surge 订阅第一行输出：

```text
#!MANAGED-CONFIG <url> interval=<seconds> strict=<true|false>
```

URL 规则：

- `public_base_url` 为空：使用当前请求 URL。
- 配置 `public_base_url` 且有 token：使用 `/surge_sub/:token/:user`。
- 配置 `public_base_url` 且无 token：使用 `/surge_sub/:user`。

`public_base_url` 必须是 http/https URL，不能包含 query 或 fragment。

## 8. Watcher

优先：

- Linux 使用 fsnotify/inotify。

fallback：

- polling，间隔来自 `watch_interval`。

触发文件：

- `*.yaml`
- `*.yml`
- `*.json`

忽略：

- 临时文件。
- 非 input 扩展名。
- 属性变化。

防抖：

- 保存完成或原子替换后等待 `watch_debounce` 再 reload。

## 9. 日志

启动日志应包含：

- data_dir（运行时固定为 `<base-dir>`）
- input_dir
- listen
- access type
- template source
- input/source/node/user 数量

示例：

```text
Subscription server loaded: data_dir=/opt/proxystack-sub input_dir=/opt/proxystack-sub/inputs listen=0.0.0.0:3003 access=token inputs=3 sources=3 nodes=4 users=1
```

禁止记录：

- token 明文。
- password 明文。
- 完整订阅内容。
