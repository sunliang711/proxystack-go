# T06 mihomo 生成器

## 目标

生成稳定 mihomo YAML，并对齐 Python golden。

## 范围

- 基础字段。
- socks/http advanced listeners。
- raw 和 xray-socks5 upstream。
- proxy-groups。
- rules profile。

## 输入文档

- [generator-spec.md](../generator-spec.md)
- 源项目 `src/proxystack/generator/mihomo/config.py`
- `tests/golden/mihomo`

## 交付物

- `internal/generator/mihomo`
- mihomo YAML golden tests。

## 实现步骤

1. 实现稳定 YAML writer。
2. 实现基础字段和 loglevel。
3. 实现 socks/http listener，区分缺省 users、`users: []`、非空 users。
4. 实现 raw upstream。
5. 实现 xray-socks5 upstream。
6. 实现 groups 和 rules。

## 验收标准

- 所有 `tests/golden/mihomo/*.yaml` 固定输出一致。
- mixed listener 显式拒绝。
- `users: []` 语义不丢失。
- `rules.extra` 在 default profile 之前。
- `MATCH,<rules.final>` 始终存在。

## 依赖

- T04

## 风险

- YAML 格式不稳定会影响 manifest hash 和用户 diff。
