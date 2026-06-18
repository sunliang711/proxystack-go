# 实现执行手册

生成日期：2026-06-17

本文用于后续真正开始写 Go 代码时执行。当前阶段不包含代码实现。

## 1. 开工前检查

执行前确认：

- 模板兼容决策已写入 [template-compat-spec.md](template-compat-spec.md)。
- golden 对照策略已确认。
- Go module 名称已确认。
- 是否保留 `proxystack-agent`/`proxystack-sub` 长命令和 `ps-agent`/`ps-sub` 短命令。

## 2. 初始化建议

建议 module：

```text
module proxystack-go
```

首批依赖建议：

- `github.com/spf13/cobra`
- `github.com/gin-gonic/gin`
- `github.com/rs/zerolog`
- `github.com/go-playground/validator/v10`
- `gopkg.in/yaml.v3`
- `github.com/stretchr/testify`

谨慎引入：

- `go.uber.org/fx`：仅在 HTTP server 生命周期或大型组合有实际收益时使用。
- `github.com/spf13/viper`：仅用于显式配置入口，不做隐式环境变量合并。
- `github.com/flosch/pongo2/v6`：用于 Go 进程内兼容 `.j2` 订阅模板；必须先用默认模板和覆盖模板完成 spike。

## 3. 包边界

| 包 | 职责 | 禁止 |
| --- | --- | --- |
| `internal/domain` | 领域模型、枚举、基础方法 | 读写文件、调用系统命令 |
| `internal/config` | 加载 YAML、默认值、路径解析 | 生成 runtime 文件 |
| `internal/domain/validation` | 校验聚合 | 写文件 |
| `internal/graph` | ref 和依赖图 | 生成 JSON/YAML |
| `internal/generator/xray` | Xray JSON | 读取磁盘 |
| `internal/generator/mihomo` | mihomo YAML | 读取磁盘 |
| `internal/generator/sub` | 订阅 input/index/bundle/template | 读取 agent stack 以外信息 |
| `internal/runtime` | runtime plan、manifest、原子写文件 | 调用 systemd |
| `internal/systemd` | systemctl/journalctl/unit | 解析业务配置 |
| `internal/install` | 下载、校验、安装 | 调 systemd |
| `internal/subserver` | HTTP、state、watcher | 读取 agent config |
| `internal/cli` | 参数绑定和用户输出 | 承载业务逻辑 |

## 4. 错误模型

建议错误类型：

- `ConfigError`
- `ValidationError`
- `ReferenceError`
- `GeneratorError`
- `RuntimePlanError`
- `SystemdError`
- `InstallError`
- `SubscriptionError`

要求：

- 错误包装保留原因链。
- CLI 输出用户友好摘要。
- 日志可记录错误链，但不得包含敏感原文。

## 5. 时间和随机数

所有需要稳定测试的逻辑必须注入：

- Clock。
- UUID generator。
- Port checker。
- Command runner。
- Downloader。

禁止在生成器中直接调用 `time.Now()` 或 `uuid.New()` 导致 golden 不稳定。

## 6. 文件 IO

写文件统一使用工具函数：

```text
WriteFileIfChanged(path, content, mode, owner)
WriteFileAtomic(path, content, mode, owner)
```

要求：

- 内容相同不改写。
- 内容不同原子替换。
- 写后修复权限。
- 失败时保留旧文件。

## 7. 测试执行顺序

每个 PR 至少执行：

```bash
go test ./...
```

涉及安全下载或依赖变更时执行：

```bash
govulncheck ./...
```

生成器 PR 必须执行 golden 更新检查，但不能自动改 golden，除非用户明确确认行为变化。

## 8. 与 Python 版对照

建议保留一个对照脚本或 Make target：

```text
make compare-python-golden
```

行为：

- 用同一 fixtures 运行 Python 版生成器。
- 用 Go 版生成器输出到临时目录。
- 比较 golden 或字段级结构。

## 9. 交付标准

每个阶段交付必须包含：

- 修改摘要。
- 涉及文件。
- 测试命令和结果。
- 与 Python 行为差异。
- 未完成风险。
