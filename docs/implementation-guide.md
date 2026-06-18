# 实现执行手册

生成日期：2026-06-17

本文用于维护当前 Go 版实现。早期规划内容已按当前代码实际落地情况更新。

## 1. 维护前检查

执行前确认：

- 模板兼容决策已写入 [template-compat-spec.md](template-compat-spec.md)。
- golden 对照策略已确认。
- Go module 名称为 `github.com/eagle/proxystack-go`。
- 已保留 `proxystack-agent`/`proxystack-sub` 长命令和 `ps-agent`/`ps-sub` 短命令。
- 服务管理器支持 `auto|systemd|launchd`，相关改动需要同时覆盖 `internal/service`、`internal/systemd` 和 CLI。

## 2. 当前 module 与依赖

当前 module：

```text
module github.com/eagle/proxystack-go
```

当前直接依赖：

- `github.com/spf13/cobra`
- `github.com/gin-gonic/gin`
- `github.com/rs/zerolog`
- `github.com/flosch/pongo2/v6`
- `github.com/fsnotify/fsnotify`
- `gopkg.in/yaml.v3`
- `github.com/stretchr/testify`

当前未直接引入：

- `go.uber.org/fx`。
- `github.com/spf13/viper`。

`github.com/go-playground/validator/v10` 目前由 Gin 间接引入，业务 schema 和跨 stack 校验主要由代码中的显式校验函数完成。

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
| `internal/runtime` | runtime plan、manifest、原子写文件 | 调用服务管理器 |
| `internal/service` | systemd/launchd 统一服务管理接口 | 生成 runtime、下载核心 |
| `internal/systemd` | systemctl/journalctl/unit | 解析业务配置、处理 launchd |
| `internal/install` | 下载、校验、安装 | 调服务管理器 |
| `internal/subserver` | HTTP、state、watcher | 读取 agent config |
| `internal/cli` | 参数绑定和用户输出 | 承载业务逻辑 |

## 4. 错误模型

建议错误类型：

- `ConfigError`
- `ValidationError`
- `ReferenceError`
- `GeneratorError`
- `RuntimePlanError`
- `ServiceManagerError`
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

如需重新对照 Python 版，可临时补充脚本或 Make target；当前仓库 Makefile 只提供 `build` 和 `build-linux`：

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
