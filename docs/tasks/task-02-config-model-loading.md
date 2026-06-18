# T02 配置模型与加载

## 目标

迁移全局配置、stack 配置、sub 配置和基础 YAML/JSON 加载能力。

## 范围

- 强类型配置结构。
- YAML loader。
- 默认值补齐。
- 路径解析。
- strict/allow extra 策略。

## 输入文档

- [schema-spec.md](../schema-spec.md)
- [conventions.md](../conventions.md)
- 源项目 `src/proxystack/domain/models.py`
- 源项目 `src/proxystack/config/loader.py`
- `tests/fixtures/example-project`

## 交付物

- `internal/domain`
- `internal/config`
- 配置加载单元测试。
- fixtures 加载测试。

## 实现步骤

1. 定义 `GlobalConfig`、`Stack`、`Xrelay`、`Clash`、`SubServerConfig` 等结构。
2. 实现端口范围、路径和监听地址解析。
3. 实现默认值补齐。
4. 实现领域模型允许 unknown fields，传输模型 strict decode。
5. 加载 `tests/fixtures/example-project`。

## 验收标准

- 示例 `config.yaml` 和所有 stack 可加载。
- 字段名保持 snake_case。
- `GlobalConfig`、`Stack` 允许扩展字段。
- `SubServerConfig`、Subscription 传输模型 strict。
- 不执行跨 stack 校验，不生成配置文件。

## 依赖

- T01

## 风险

- Go 零值不能替代 Python 默认值，必须显式补齐。
- strict/allow extra 一刀切会导致兼容或安全问题。
