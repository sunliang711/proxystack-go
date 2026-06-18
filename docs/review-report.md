# Go 重写方案 Review 结论

生成日期：2026-06-17

参与 agent：

- 方案 agent：负责只读调研当前项目并输出 Go 重写方案。
- review agent：负责独立审查方案、风险和任务拆分。

## 1. Review 发现

### 阻断问题

1. 订阅模板兼容策略必须明确。

当前 Python 版支持用户通过 `templates_dir` 或 `<data_dir>/templates/sub/` 覆盖 `clash.yaml.j2`、`premium-clash.yaml.j2`、`surge.conf.j2`。Go 版不能在未声明 breaking change 的情况下直接换成 Go `text/template`。

处理结果：已在方案中把 `.j2` 兼容策略列为实现前关键决策，并新增 T08 模板兼容任务。

2. 输出一致性边界必须写死。

如果 mihomo YAML、订阅 input、manifest 等只要求“语义等价”，会破坏 hash、manifest 和重启判断。

处理结果：已在方案中补充“输出兼容标准”，明确生成文件、manifest、unit 等必须稳定输出。

### 高风险问题

1. schema extra 策略不能一刀切。

当前领域模型允许扩展字段，订阅 input/bundle 等契约模型更严格。Go 版如果全部 strict 或全部 allow 都会带来兼容或安全风险。

处理结果：已补充 schema 严格度矩阵。

2. CLI 生命周期任务过粗。

原 T09 范围过大，容易把只读命令、副作用命令、YAML 编辑命令、runtime plan 和 systemd wrapper 混在一起。

处理结果：已拆分为 T11 配置编辑、T12 runtime plan、T13 服务生命周期、T14 systemd unit 与权限。

3. 安装更新安全细节需要完整验收。

Go 版不能只实现“下载一个文件”，必须保留托管源 fallback、慢速切源、sha256、归档安全、事务回滚和 install/update 差异。

处理结果：已在 T15 中补充完整行为矩阵。

### 中风险问题

1. Fx/Viper 对文件型 CLI 项目可能过度工程。

处理结果：方案调整为 Gin/Zerolog/Validator 保持默认，Fx/Viper 谨慎使用，不让隐式配置合并污染现有 YAML 行为。

2. sub HTTP 路由兼容细节需要显式验收。

处理结果：T10 已列出 path token 和 query token 两套路径，以及 401/403/404/503 错误行为。

3. systemd 与文件权限不止 unit 内容。

处理结果：T14 已补充目录、配置、二进制、geo 文件的 mode 和 owner 验收。

## 2. 最终建议

- 先按文档完成架构确认，不要直接进入实现。
- 实现前优先确定模板兼容策略，这是当前最大的兼容性决策。
- 第一批实现应从配置模型、校验、引用图和 golden 对照开始，避免 CLI 先行导致行为散落。
- 所有生成器必须用当前 Python fixtures/golden 作为验收基准。
- `check/render/validate/list` 等只读命令必须有“不落盘、不操作 systemd”的测试。

## 3. Review 结论

方案经 review 后已补齐主要兼容边界，可以作为后续 Go 重写的任务书基础。剩余需要用户或实现负责人确认的关键决策是：是否要求完全兼容现有 `.j2` 用户模板。如果要求兼容，模板渲染方案必须先落定；如果允许 breaking change，需要单独提供模板迁移说明和验收用例。
