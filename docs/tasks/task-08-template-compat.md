# T08 模板兼容任务

## 目标

实现 Go 进程内 Jinja2-compatible renderer，兼容现有 `.j2` 覆盖模板能力。

## 范围

- `pongo2` spike。
- 模板查找顺序。
- `yaml_block` filter。
- 未定义变量 fail fast。
- 坏模板错误映射。

## 输入文档

- [template-compat-spec.md](../template-compat-spec.md)
- [generator-spec.md](../generator-spec.md)
- `tests/test_sub_generator.py` 中模板覆盖用例。

## 交付物

- 模板 renderer。
- 模板查找器。
- 模板兼容测试。

## 实现步骤

1. 用 `pongo2` 直接渲染三份默认 `.j2` 模板。
2. 实现 `yaml_block` filter。
3. 模拟 Jinja2 `StrictUndefined` 行为。
4. 实现模板查找顺序。
5. 覆盖坏模板和缺模板错误。

## 验收标准

- 默认三份 `.j2` 模板直接渲染成功。
- `templates_dir/sub` 覆盖优先级最高。
- `templates_dir` 覆盖次之。
- `data_dir/templates/sub` 覆盖第三。
- 坏模板在 HTTP 中返回 503。
- 不引入 Python runtime。

## 依赖

- T07

## 风险

- `pongo2` 与 Jinja2 语法差异需要通过 spike 提前暴露。
