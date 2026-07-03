# T07 订阅 input/index 与模板渲染

## 目标

迁移订阅节点生成、合并和三类订阅输出。

## 范围

- SubscriptionInput。
- SubscriptionIndex。
- Clash/Premium Clash/Surge 渲染。
- 节点合并和重复校验。

## 输入文档

- [generator-spec.md](../generator-spec.md)
- [template-compat-spec.md](../template-compat-spec.md)
- 源项目 `src/proxystack/generator/sub/config.py`
- 源项目 `src/proxystack/templates/sub/*.j2`
- `tests/golden/sub`

## 交付物

- `internal/generator/sub`
- subscription golden tests。

## 实现步骤

1. 定义 SubscriptionInput、Node、Index、Access。
2. 从 `sub: true` 的 xray inbound 生成节点。
3. 实现 vmess 多用户节点。
4. 实现 shadowsocks/SS2022 多用户节点。
5. 实现 input 文件合并。
6. 实现三类订阅输出。

## 验收标准

- input/index/Clash/Premium Clash/Surge golden 一致。
- 订阅不含 clash 内部配置。
- 重复 node id 失败。
- 同用户重复 proxy name 失败。
- 不同用户可有相同 proxy name。
- Surge 地区组和 managed config 行为一致。

## 依赖

- T03

## 风险

- 订阅输出包含凭据，日志和错误中不得泄漏。
