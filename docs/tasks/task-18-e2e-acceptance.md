# T18 端到端迁移验收

## 目标

确认 Go 版可替代 Python 版。

## 范围

- 端到端主流程。
- 生成物对照。
- 越界读写检查。
- 手工 systemd 验收。

## 输入文档

- [testing-acceptance-matrix.md](../testing-acceptance-matrix.md)
- 当前 fixtures。
- 当前 golden。
- 主流程测试。

## 交付物

- E2E 测试。
- 迁移验收报告。
- Python/Go 对照结果。

## 实现步骤

1. 构建临时 base dir。
2. 执行 `setup local -> add -> validate -> check`。
3. 使用 fake binary 和 fake systemd 执行 `start`。
4. 执行 `sub export`。
5. 执行 `pssub import`。
6. 启动 `pssub serve`。
7. 请求三类订阅。
8. 对照 Python golden 和行为边界。

## 验收标准

- 主流程跑通。
- Go 版不会读取或写入越界目录。
- 关键生成物与 Python 版对照通过。
- sub 服务不读取 agent config 和 stacks。
- bundle/native backup schema 互相拒绝。
- 手工 systemd 验收通过。

## 依赖

- T01-T17

## 风险

- E2E 不能依赖真实网络或真实 systemd；真实 systemd 只做独立手工验收。
