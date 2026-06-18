# T12 runtime plan、manifest 与只读命令

## 目标

实现 `validate/render/check` 和 runtime diff，为 `start/restart` 提供 apply 前置计划。

## 范围

- RuntimePlan。
- GeneratedFile。
- FileChange。
- Manifest。
- 只读命令。

## 输入文档

- [generator-spec.md](../generator-spec.md)
- [cli-spec.md](../cli-spec.md)
- 源项目 RuntimePlan 相关逻辑。

## 交付物

- `internal/runtime`
- `validate/render/check` 命令。
- manifest diff 测试。

## 实现步骤

1. 实现 generated file 收集。
2. 实现 manifest read/write。
3. 实现 sha256 diff。
4. 实现 `check` 输出。
5. 实现 `render` 命令。
6. 实现 `validate` 命令。

## 验收标准

- `check` 不写文件、不操作 systemd。
- 多次生成 hash 不变。
- `generated_at` 合理复用。
- scope 删除只影响目标服务。
- 未变化文件不改写但可修复 metadata。

## 依赖

- T05
- T06
- T07

## 风险

- manifest 时间戳和文件排序不稳定会导致频繁重启。
