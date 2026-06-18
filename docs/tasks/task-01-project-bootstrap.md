# T01 项目骨架与命令入口

## 目标

创建 Go module、两个二进制入口和基础命令树，为后续实现提供最小可运行骨架。

## 范围

- `cmd/ps-agent`
- `cmd/ps-sub`
- 基础 CLI root command。
- 版本命令。
- 基础日志初始化。

## 输入文档

- [cli-spec.md](../cli-spec.md)
- 源项目 `pyproject.toml` 中的 console scripts。
- [conventions.md](../conventions.md)

## 交付物

- `go.mod`
- `cmd/ps-agent/main.go`
- `cmd/ps-sub/main.go`
- `internal/cli/agent`
- `internal/cli/sub`
- 最小版本信息模块。

## 实现步骤

1. 初始化 Go module。
2. 引入 Cobra 和 Zerolog。
3. 建立 `ps-agent` 和 `ps-sub` root command。
4. 实现 `version` 命令。
5. 建立统一 exit code 和错误输出入口。
6. 增加最小 smoke test。

## 验收标准

- `ps-agent --help` 可运行。
- `ps-sub --help` 可运行。
- `ps-agent version` 可运行。
- `ps-sub version` 可运行。
- `cmd/*/main.go` 不包含业务逻辑。
- 不创建配置文件，不写 runtime，不调用 systemd。

## 依赖

无。

## 风险

- 不要在 T01 里提前实现业务命令细节。
- 不要引入尚未确认用途的依赖。
