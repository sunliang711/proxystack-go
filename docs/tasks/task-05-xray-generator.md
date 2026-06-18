# T05 Xray 生成器

## 目标

生成稳定 Xray JSON，并对齐 Python golden。

## 范围

- log。
- api/stats/policy。
- vmess、shadowsocks、socks5、http inbound。
- clash/socks5/http/direct outbound。

## 输入文档

- [generator-spec.md](../generator-spec.md)
- 源项目 `src/proxystack/generator/xray/config.py`
- `tests/golden/xray`

## 交付物

- `internal/generator/xray`
- Xray JSON golden tests。

## 实现步骤

1. 定义 Xray 输出结构或稳定 map writer。
2. 实现 log 默认值。
3. 实现 api/stats/policy。
4. 实现 inbound 渲染。
5. 实现 outbound 渲染。
6. 对齐 golden。

## 验收标准

- 所有 `tests/golden/xray/*.json` 逐字节一致。
- API listen 只允许 loopback。
- vmess 多用户输出 clients。
- SS2022 规则正确。
- clash outbound 使用目标 socks listener，必要时携带第一个用户。

## 依赖

- T04

## 风险

- JSON 字段顺序和缩进需要稳定。
