# Go 版编码与工程约定

生成日期：2026-06-17

本文基于 Go 后端、安全和 API 规则裁剪，结合本项目实际约束形成。

## 1. 项目结构

采用：

```text
cmd/
internal/
pkg/
configs/
templates/
tests/
scripts/
docs/
```

要求：

- `cmd/*/main.go` 只负责启动。
- CLI 参数绑定放 `internal/cli`。
- 业务能力放 `internal/*`。
- 可复用但不含业务的工具放 `pkg/*`。

## 2. 分层约定

本项目不是数据库型 Web 服务，因此不强制 Handler-Service-Repository 三层，但要保持职责边界：

- HTTP handler 只做路由、参数、鉴权和响应。
- 生成器只接收已解析模型，不自行读文件。
- runtime 只负责 diff、manifest 和文件写入。
- systemd/install 只处理系统交互，不理解业务规则。

## 3. 命名

- 包名小写、简短、无下划线。
- `ID`、`URL`、`HTTP` 全大写。
- 错误变量命名为 `ErrXxx`。
- 常量名按导出与否使用 PascalCase 或 camelCase。
- 禁止否定命名，如 `IsNotValid`。

## 4. Struct tag

字段顺序：

```go
json -> yaml -> mapstructure -> binding/validate
```

本项目常用：

```go
json:"user_id" yaml:"user_id" validate:"required"
```

不对外暴露字段使用：

```go
json:"-" yaml:"-"
```

## 5. 配置

- 业务代码禁止散落调用 Viper。
- YAML 解码后一次性构造强类型配置。
- 默认值补齐和校验集中在 `internal/config` 和 `internal/domain/validation`。
- 配置非法时 fail fast。

## 6. 日志

- 使用 Zerolog。
- 日志消息使用英文。
- 字段结构化输出。
- 错误日志必须带 `err` 字段。
- 禁止记录 password、token、secret、完整 uuid、完整订阅内容。

示例：

```text
Subscription inputs reloaded input_dir=/opt/proxystack/sub/inputs inputs=3 sources=3 nodes=4 users=1
```

## 7. 并发

- watcher、HTTP state 使用 `sync.RWMutex` 或 `atomic.Value`。
- goroutine 必须有 context 或 stop channel。
- Ticker 必须 stop。
- HTTP server 必须优雅关停。

## 8. 外部命令

必须使用：

```go
exec.CommandContext(ctx, "systemctl", "status", unit)
```

禁止：

```go
exec.Command("sh", "-c", "systemctl status "+unit)
```

## 9. 安全

- 下载 URL 必须做 SSRF 防护。
- 归档必须做路径穿越校验。
- 普通远端下载必须 sha256。
- 文件替换必须原子。
- 生产代码禁止 `fmt.Println` 和 `log.Println`，CLI 输出经统一 writer。

## 10. 测试

- 优先表驱动。
- 使用 `testify/require` 和 `testify/assert`。
- fake systemd、fake downloader、fake clock、fake port checker。
- 不连接真实外部服务。
- 不要求 root。
