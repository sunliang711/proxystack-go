# T01-T05 Go 实现与复审交付

生成时间：2026-06-17 17:54:18

## 任务背景

根据 `docs/tasks/task-01-project-bootstrap.md` 至 `docs/tasks/task-05-xray-generator.md`，完成 proxystack Go 重写的首批基础能力：

- T01：Go module、`ps-agent` / `ps-sub` 命令入口、version 命令和基础日志。
- T02：配置领域模型、YAML 加载、默认值、路径解析、领域模型 allow unknown 与传输模型 strict decode。
- T03：跨 stack 校验、端口唯一性、系统端口检查抽象、安全校验和错误聚合。
- T04：ref parser、endpoint index、服务依赖 DAG、target scope 和 dependency plan。
- T05：稳定 Xray JSON 生成器，并对齐 Python golden。

## 编排方案

主 Agent 负责实现、集成和验证。实现完成后启动独立 review agent 做只读代码审查；根据审查阻断项修复后复审，直到 review agent 给出通过结论。

## 实现方案

- 新增 Cobra CLI 骨架，`cmd/*/main.go` 仅负责启动命令树。
- 新增强类型配置模型，使用 YAML 自定义解码保留默认值语义和显式 false/0 语义。
- 领域配置模型默认允许 unknown fields；`SubServerConfig`、`AccessConfig`、`ManagedConfig` 等传输模型显式 strict。
- 新增跨 stack 校验入口，支持 fake/noop/TCP port checker。
- 新增引用图索引和依赖计划，disabled stack/component 不进入默认运行期校验和图索引。
- 新增 Xray 生成器，使用结构体字段顺序保证 JSON 输出稳定。

## 文件与配置变更

- 新增 `go.mod`、`go.sum`。
- 新增 `cmd/ps-agent`、`cmd/ps-sub`。
- 新增 `internal/cli`、`internal/version`。
- 新增 `internal/domain`、`internal/config`、`internal/domain/validation`。
- 新增 `internal/graph`、`internal/generator/xray`、`internal/testutil`。
- 新增 `tests/fixtures/example-project` 和 `tests/golden/xray`。
- 新增对应 Go 单元测试。

## 测试结果

- `go test ./...`：通过。
- `go build ./...`：通过。
- `go run ./cmd/ps-agent --help`：通过。
- `go run ./cmd/ps-agent version`：通过。
- `go run ./cmd/ps-sub version`：通过。

## 评审问题与处理

独立 review agent 第一轮发现 3 个阻断问题：

- `managed_config.enabled/strict` 显式 `false` 被默认值覆盖。已改为指针 bool 并补测试。
- `SubServerConfig` 缺少值校验。已补 listen、access、managed URL、interval/watch 校验。
- disabled stack/component 仍参与公开 noauth 和端口校验。已过滤 disabled stack/component 并补测试。

第二轮发现 strict decode 对 `access.extra` 未覆盖。已为 `AccessConfig` 增加 unknown field 检查并补测试。

最终复审结论：通过，无需继续修改。

## 风险与后续建议

- T01-T05 已完成，T06 之后的 mihomo、订阅、runtime、systemd、install/update 等能力仍未实现。
- `GlobalConfig`/`Stack` 目前允许并忽略 unknown fields，未做 round-trip 保留；后续如需要配置重写，应补充 Extra 或 YAML AST 保留能力。
