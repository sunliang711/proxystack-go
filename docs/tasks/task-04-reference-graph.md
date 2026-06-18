# T04 引用图与依赖计划

## 目标

构建 ref parser、endpoint index、服务 DAG 和 target scope。

## 范围

- xrelay outbound ref。
- clash upstream ref。
- target scope。
- 服务依赖排序。

## 输入文档

- [generator-spec.md](../generator-spec.md)
- [cli-spec.md](../cli-spec.md)
- 源项目 `src/proxystack/graph`

## 交付物

- `internal/graph`
- `ReferenceGraph`
- `DependencyPlan`
- `TargetScope`
- 图相关测试。

## 实现步骤

1. 定义 ref 语法和解析错误。
2. 建立 xrelay inbound、clash listener、upstream 和 group 索引。
3. 实现 disabled stack/组件过滤。
4. 实现服务 DAG。
5. 实现循环依赖检测。
6. 实现 target scope 解析。

## 验收标准

- `usa1/usa2/auto` 依赖顺序与 Python 一致。
- 缺失 ref 失败。
- 协议不匹配失败。
- disabled stack/组件不参与默认 lifecycle。
- 循环依赖失败。
- `0.0.0.0` 内部连接归一化为 `127.0.0.1`。

## 依赖

- T03

## 风险

- target scope 和 dependency plan 如果拆得不清，会影响后续 runtime 和 service 命令。
