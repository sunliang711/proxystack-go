# T16 diagnostics/ipinfo

## 目标

迁移出口 IP 查询能力。

## 范围

- mihomo socks listener 查找。
- curl 命令构建。
- IPv4/IPv6 来源选择。
- fallback 和进度输出。

## 输入文档

- [cli-spec.md](../cli-spec.md)
- 源项目 `src/proxystack/diagnostics/ipinfo.py`
- 源项目 `tests/test_ipinfo.py`

## 交付物

- `internal/diagnostics`
- `ps-agent ipinfo` 命令。
- fake command runner tests。

## 实现步骤

1. 从 stack 找到 mihomo socks listener。
2. 构造 curl proxy URL。
3. 实现 IPv4/IPv6 来源列表。
4. 实现超时和 fallback。
5. 实现进度输出。

## 验收标准

- `--family all|ipv4|ipv6` 兼容。
- `--timeout` 默认 `8.0` 秒。
- wildcard 和 IPv6 host 归一化一致。
- 失败后尝试下一来源。
- 不误用 mihomo REST API。

## 依赖

- T04

## 风险

- curl 命令必须使用参数数组，不能拼 shell。
