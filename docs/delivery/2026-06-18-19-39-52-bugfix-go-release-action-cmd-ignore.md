# GitHub Action release 构建找不到 cmd 目录

## 问题背景

GitHub Action release 构建执行 `go build ./cmd/ps-agent` 时报错：

```text
stat /home/runner/work/proxystack-go/proxystack-go/cmd/ps-agent: directory not found
```

## 根因分析

`.gitignore` 中的 `ps-agent` 和 `ps-sub` 是未锚定规则，会匹配任意层级的同名路径，因此 `cmd/ps-agent/` 和 `cmd/ps-sub/` 被忽略，没有进入 git 追踪文件列表。Action checkout tag 后只包含已追踪文件，所以远端构建环境缺少 CLI 入口目录。

## 修复方案

- 将 `.gitignore` 规则从 `ps-agent`、`ps-sub` 改为 `/ps-agent`、`/ps-sub`。
- 这样仍会忽略仓库根目录下的本地构建产物，同时允许 `cmd/ps-agent/main.go` 和 `cmd/ps-sub/main.go` 被提交。

## 文件与配置变更

- `.gitignore`：收窄二进制产物忽略规则。
- `cmd/ps-agent/main.go`、`cmd/ps-sub/main.go`：修复后会作为待提交源码出现在 git 状态中。

## 验证结果

- `git check-ignore -v cmd/ps-agent/main.go cmd/ps-sub/main.go`：不再匹配忽略规则。
- `go test -count=1 ./cmd/ps-agent ./cmd/ps-sub ./internal/cli ./internal/version`：通过。
- `go build -trimpath -o <tmp>/ps-agent ./cmd/ps-agent`：通过。
- `go build -trimpath -o <tmp>/ps-sub ./cmd/ps-sub`：通过。
- `go test ./...`：通过。

## 风险与后续建议

- 已发布或已推送的旧 tag 不会自动包含新提交的 `cmd` 入口文件；需要提交本修复后重新打 tag，或发布新的语义化 tag。
