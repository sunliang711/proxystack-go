# ADR-0001：Go 重写优先保持兼容

日期：2026-06-17

## 状态

已接受为规划默认决策，待实现阶段复核。

## 背景

当前 Python 版 `proxystack` 已有较完整的配置格式、CLI 语义、生成器输出、订阅 bundle、native backup、systemd unit 和测试 golden。Go 重写的主要价值是用 Go 重新实现，而不是重新设计产品。

## 决策

Go 版优先保持兼容：

- 配置文件格式兼容。
- 命令名称和 target scope 兼容。
- Xray/mihomo/sub 生成输出稳定兼容。
- bundle/native backup schema 兼容。
- agent/sub 数据边界兼容。
- systemd unit 行为兼容。

任何 breaking change 必须单独记录 ADR 和迁移说明。

## 后果

优点：

- 用户迁移成本低。
- 可以用现有 fixtures/golden 做强验收。
- 便于分阶段替换。

代价：

- Go 实现需要复刻部分 Python/Pydantic 行为。
- YAML 和 Jinja2 模板兼容会增加实现难度。
- 部分 Go 风格改良需要让位于兼容性。
