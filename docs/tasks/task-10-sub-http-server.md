# T10 sub HTTP 服务

## 目标

实现 `ps-sub serve` 的 HTTP 服务和内存索引。

## 范围

- Gin routes。
- SubscriptionState。
- Token 鉴权。
- Watcher reload。
- 错误响应。

## 输入文档

- [http-subserver-spec.md](../http-subserver-spec.md)
- [template-compat-spec.md](../template-compat-spec.md)
- 源项目 `src/proxystack/subserver`

## 交付物

- `internal/subserver`
- `internal/cli/sub` 的 serve/import/config/clear。
- HTTP route tests。
- watcher tests。

## 实现步骤

1. 实现 sub config loader。
2. 实现 state snapshot 和 reload。
3. 实现 Gin routes。
4. 实现 token path/query 鉴权。
5. 实现 Surge managed config URL。
6. 实现 fsnotify 和 polling fallback。

## 验收标准

- `/health` 返回 index 状态。
- `/sub/:user?token=` 兼容。
- `/sub/:token/:user` 兼容。
- Premium Clash 和 Surge 路由兼容。
- token 缺失 401。
- token 错误 403。
- 用户不存在 404。
- 模板错误 503。
- 运行期 reload 失败保留旧 index。
- 不读取 agent config 或 stack。

## 依赖

- T08
- T09

## 风险

- watcher goroutine 必须可停止，避免测试泄漏。
