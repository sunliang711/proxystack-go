# ADR-0002：订阅模板兼容需在实现前拍板

日期：2026-06-17

## 状态

已接受。

## 背景

Python 版订阅系统使用 Jinja2，用户可以用 `.j2` 文件覆盖默认模板。Go 原生 `text/template` 与 Jinja2 语法不兼容。

## 决策

首期采用 Go 进程内 Jinja2-compatible renderer，首选 `github.com/flosch/pongo2/v6`。

兼容范围：

- 默认三份 `.j2` 模板必须直接渲染并与 Python golden 一致。
- 用户覆盖模板只要使用项目文档公开的上下文变量和 `yaml_block` filter，就必须兼容。
- 不支持的 Jinja2 扩展语法必须明确报错，不可静默忽略。

不采用：

- 不使用 Python helper，避免 Go 版运行时依赖 Python。
- 不直接切换 Go `text/template`，避免破坏用户 `.j2` 覆盖模板。

## 后果

优点：

- 不引入 Python runtime。
- 默认模板和常见用户覆盖模板可保持兼容。
- 订阅服务 Docker 镜像更简单。

代价：

- 需要验证 `pongo2` 与当前 Jinja2 模板语法差异。
- 需要实现 `yaml_block` filter 和严格未定义变量行为。
- 极少数依赖完整 Jinja2 扩展的用户模板可能需要调整，但必须收到明确错误。
