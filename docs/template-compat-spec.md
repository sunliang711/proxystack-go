# 订阅模板兼容规格

生成日期：2026-06-17

本文定义 Go 版兼容现有 `.j2` 订阅模板的最终决策和验收标准。

## 1. 当前行为

Python 版使用 Jinja2 渲染三类模板：

- `clash.yaml.j2`
- `premium-clash.yaml.j2`
- `surge.conf.j2`

用户可通过以下路径覆盖模板：

1. `pssub config.yaml` 中 `templates_dir/sub/<template>`
2. `pssub config.yaml` 中 `templates_dir/<template>`
3. `<data_dir>/templates/sub/<template>`
4. 包内默认模板

模板上下文：

- `user`
- `generated_at`
- `sources`
- `nodes`
- `proxies`
- `proxy_names`
- `proxy_groups`
- `clash_rules`
- `surge_proxy_lines`
- `surge_proxy_names`
- `surge_region_groups`
- `surge_rules`
- `test_url`
- `surge_skip_proxy`
- `surge_proxylist_icon_url`
- `surge_auto_icon_url`
- `managed_config_url`
- `managed_config_interval`
- `managed_config_strict`

filter：

- `yaml_block`

错误行为：

- 缺模板或坏模板在 HTTP 请求中返回 503。
- 命令行渲染时返回非零退出码。

## 2. 首期决策

首期采用方案 A：在 Go 进程内实现 Jinja2 兼容渲染层，首选依赖 `github.com/flosch/pongo2/v6`，不引入 Python runtime。

兼容目标：

- 默认三份 `.j2` 模板必须直接渲染并与 Python golden 一致。
- 用户覆盖模板只要使用本文档列出的上下文变量和 `yaml_block` filter，就必须兼容。
- 不承诺完整 Jinja2 生态的所有扩展语法；不支持的语法必须在加载或渲染时返回明确错误，不能静默降级。
- Docker sub 镜像不需要 Python。

依赖影响：

- 新增 Go 依赖 `github.com/flosch/pongo2/v6`，用途仅限订阅模板渲染。
- 必须实现自定义 `yaml_block` filter。
- 必须配置未定义变量为错误，模拟 Jinja2 `StrictUndefined` 的 fail fast 行为。

## 3. 可选实现路线

### 方案 A：内嵌或依赖 Jinja2 兼容引擎（已选）

优点：

- 最大程度兼容现有用户模板。
- 文档和用户习惯不变。

缺点：

- Go 原生生态没有官方 Jinja2。
- 第三方兼容库可能不完整。
- 需要验证 `StrictUndefined` 和 `yaml_block` 行为。

验收：

- 当前三份默认 `.j2` 模板可直接渲染。
- 用户覆盖模板测试通过。
- 未定义变量应失败。
- `yaml_block` 输出与 Python 一致。

### 方案 B：外部 Python helper 渲染

优点：

- 兼容性最高。

缺点：

- Go 版仍依赖 Python runtime。
- 部署和 Docker sub 镜像复杂。
- 不符合“用 Go 实现一遍”的纯度预期。

验收：

- helper 不读取 agent config。
- helper 错误映射为 CLI 非零或 HTTP 503。
- Docker 镜像包含必要 Python 依赖。

### 方案 C：迁移到 Go `text/template`

优点：

- Go 原生、部署简单。
- 性能和可维护性好。

缺点：

- 破坏现有 `.j2` 用户自定义模板。

必须补充：

- breaking change 文档。
- `.j2` 到 `.tmpl` 的迁移指南。
- 内置模板迁移工具或至少提供前后对照。

验收：

- 默认输出与 golden 一致。
- 旧 `.j2` 覆盖模板被检测并给出明确错误，不可静默忽略。

## 4. 本项目默认要求

Go 版不得静默放弃 `.j2` 覆盖模板能力。

最终决策记录：

```text
模板兼容决策：A，Go 进程内 Jinja2-compatible renderer
首选依赖：github.com/flosch/pongo2/v6
是否 breaking change：否，面向 documented template subset 保持兼容
迁移策略：无需迁移；不支持语法返回明确错误
验收用例：默认模板 golden、templates_dir 覆盖、data_dir 覆盖、未定义变量、坏模板 HTTP 503
```

## 5. HTTP 错误行为

模板错误统一映射：

```json
{
  "error": {
    "code": "template_error",
    "message": "subscription template unavailable"
  }
}
```

HTTP 状态码：503。

日志要求：

- 可以记录模板路径和错误类型。
- 不记录 token、password、完整订阅内容。
