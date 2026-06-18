# T03 配置校验矩阵

## 目标

迁移跨字段、跨 stack 和安全校验，形成统一校验入口。

## 范围

- 单文件字段校验。
- 跨 stack 端口与 ref 前置校验。
- 安全校验。
- 错误聚合。

## 输入文档

- [schema-spec.md](../schema-spec.md)
- [testing-acceptance-matrix.md](../testing-acceptance-matrix.md)
- 源项目 `src/proxystack/domain/validation.py`

## 交付物

- `internal/domain/validation`
- 校验错误结构。
- 表驱动负面测试。

## 实现步骤

1. 实现 stack 文件名与 `name` 一致校验。
2. 实现端口合法性和唯一性校验。
3. 实现系统端口检查接口，并提供 fake port checker。
4. 实现公开 socks/http noauth 校验。
5. 实现日志级别、vmess、shadowsocks、SS2022 校验。
6. 实现 rules target 校验。

## 验收标准

- stack 文件名与 `name` 不一致时报错。
- 端口重复时报错。
- 系统端口占用可通过 fake checker 测试。
- 非回环 socks/http noauth 默认失败。
- vmess 多用户重复 user/uuid/email/tag 失败。
- SS2022 base64 key 长度错误失败。
- `Inbound.sub` 缺失失败。
- `clash.controller.secret` 缺失或空字符串失败。

## 依赖

- T02

## 风险

- 不能只依赖 struct tag，必须覆盖跨字段和跨 stack 规则。
