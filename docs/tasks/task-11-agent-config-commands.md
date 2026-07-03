# T11 agent 配置编辑与模板命令

## 目标

实现 `setup local/add/config/list/remove/clone/member` 等配置管理命令。

## 范围

- 默认配置生成。
- stack 模板。
- 端口自动分配。
- YAML 写入。
- member 管理。

## 输入文档

- [cli-spec.md](../cli-spec.md)
- [schema-spec.md](../schema-spec.md)
- 源项目 `src/proxystack/cli/lifecycle.py`
- 源项目模板文件。

## 交付物

- agent 配置命令。
- 模板文件 embed。
- 配置命令测试。

## 实现步骤

1. 实现 `setup local` 的默认配置生成。
2. 实现 `add` 和端口分配。
3. 实现 `config --check-only` 和编辑器调用。
4. 实现 `list`。
5. 实现 `clone`。
6. 实现 `member list/add/remove`。
7. 实现 `remove`。

## 验收标准

- `setup local` 不覆盖已有文件除非 force。
- `add` 随机替换 vmess UUID。
- `clone --allocate-ports` 只改目标 stack。
- member 命令只允许 auto/load-balance stack。
- `config` 修改 active stack 后按现有行为重启 active 组件。

## 依赖

- T02
- T04

## 风险

- YAML round-trip 可能丢注释；首期需要稳定输出并保留可读性。
