# psctl user 临时启停交付记录

## 任务背景

用户问「运行中的 xray 能否动态加载配置，从而不重启服务禁用指定用户」。确认 Xray 没有配置文件级 reload，但 gRPC `HandlerService` 支持运行期增删用户后，用户定下了功能边界：

- 只要 `enable` / `disable` / `list` 三个子命令，新增和修改用户仍然改配置文件。
- `TARGET` 可选，语义与已有生命周期命令一致。
- 禁用状态存 `runtime/disabled.json`，不回写 `config.yaml` 和 `stacks/*.yaml`。
- 订阅内容保持原样，clash 入口不管，不做 `--live` 对账选项。

首版实现后由三个独立 agent 分别做正确性、安全、设计一致性 review，用户选择「全修」，并另外拍板两项：默认开启 `HandlerService` 并加 `doctor` 检查、`disable` 时提示不可启停的入口。

## 编排方案

- 主 Agent：负责全部实现、测试、文档和交付记录。
- review agent 三个（并行，只读）：
  - 正确性：边界情况、状态一致性、`UsersOnlyChange` 判断严密性、测试断言有效性。
  - 安全：socks5/http 免认证降级隔离、凭据泄露、参数注入、`HandlerService` 攻击面、禁用语义可靠性。
  - 设计一致性：仓库既有约定、抽象归属、过度设计与缺失、文档完整性。

## 实现方案

### 状态与生成

- 新增 `internal/userstate`：`runtime/disabled.json` 的读写，条目 `{user, stack, since}`，`stack` 始终具体，无通配语义；缺文件等价空状态，版本不符 fail fast。
- `domain.StackSet` 增加 `DisabledUsers`（`json:"-" yaml:"-"`，不进 manifest hash），`XrayAPIConfig` 增加 `HasService`。
- 新增 `internal/generator/xray/users.go`：`SupportsUserToggle` 只认 vmess/shadowsocks；`applyDisabledUsers` 在生成时过滤，某 inbound 用户全被禁时报错而不是生成空 `clients`；`DumpsInboundUserPatch` 复用主渲染路径产出 `adu` 载荷。
- 禁用状态由 `runtime.BuildPlan` 和 `psctl render xray` **显式加载**，不进 `config.LoadStacks`：`BuildOptions.DisabledUsers` 是一个和既有 `Now func() time.Time` 同风格的注入缝，默认从磁盘读，`psctl user` 用它按尚未落盘的目标状态预演。

### 热应用

- 新增 `internal/xrayapi`：调用受管 `bin/xray` 的 `api rmu`/`api adu`。不引 xray-core gRPC stub，因为其依赖树对配置管理 CLI 过重，且编译期固定的 protobuf 会与用户实际安装的 xray 版本脱节。
- 成功与否按输出的 `Removed/Added N user(s) in total.` 判定并要求 `N >= 1`；这两个子命令对单用户失败只 `fmt.Println` 后继续，进程照样退出 0。
- inbound tag 和 email 以 `-` 开头时拒绝执行：email 是裸位置参数，会被 Go `flag` 当成 flag 解析。
- `adu` 的 patch 含 UUID/密码，写在已是 `0750` 的 `runtime/` 而不是共享 `/tmp`。

### 写盘守卫

- `runtime.UsersOnlyPlan` 要求 plan 的差异**恰好**等于本次启停：`clients` 以外的部分必须完全一致，`clients` 的增删也必须与要热应用的 `(inbound tag, email)` 逐一对上。只判「差异是否局限于 clients」会把别处未重启的用户增删一起写盘并刷新 manifest，而热应用不带它们，运行中实例就此和磁盘分叉且漂移检测失效。
- 逐 stack 预演，无关 stack 的未重启改动不挡住其它 stack。
- 顺序是先算目标状态 → 预演校验 → 全过后才落盘、写文件、热应用；任何一步失败都不留下已写入的状态。

### 命令与提示

- `internal/cli/agent/user_commands.go`：`user list/disable/enable`，挂在配置管理分组。
- `clash/NAME` 显式拒绝；热应用失败逐条给出 `psctl restart xray/NAME`（生命周期命令只接受一个 TARGET）。
- `disable` 提示同 scope 内引用该用户但不能启停的入口：socks5/http inbound（共享账号）、同名 Clash listener 账号（独立凭据）。
- `disabled.json` 里引用了已不存在 stack/用户的陈旧条目，`user list` 报出、`user enable` 可清除。
- `doctor` 陈述每个 stack 的 `HandlerService` 状态（开与不开都是合法配置，只作 check 不判失败），并把陈旧禁用条目报成 issue。

### 默认值

- `defaults.xray.api.services` 默认加 `HandlerService`，同步改种子 `config.yaml` 模板和 per-stack api snippet（这两处原本显式写死 `[StatsService]`，会盖掉默认值）。
- 已实测确认：存量安装的 `config.yaml` 和 stack 文件都钉着 `[StatsService]`，用新二进制渲染结果**无差异**，不会触发额外重启；要热生效需要手改并 restart 一次。

## 文件与配置变更

新增：

- `internal/userstate/disabled.go`、`disabled_test.go`
- `internal/xrayapi/client.go`、`client_test.go`
- `internal/runtime/usersdiff.go`、`usersdiff_test.go`
- `internal/generator/xray/users.go`、`users_test.go`
- `internal/cli/agent/user_commands.go`、`user_commands_test.go`
- `docs/delivery/2026-08-27-15-20-00-feature-go-psctl-user-toggle.md`

修改：

- `internal/domain/models.go`（`DisabledUserSet`、`StackSet.DisabledUsers`、`XrayAPIConfig.HasService`、默认 services）
- `internal/runtime/plan.go`（`BuildOptions.DisabledUsers` 注入缝、`LoadDisabledUsers`）
- `internal/generator/xray/config.go`（`RenderConfig` 里过滤）
- `internal/cli/agent/root.go`、`runtime_commands.go`、`diagnostics_commands.go`
- `internal/systemd/runner.go`（`disabled.json` 注册进 `StandardMetadataRules`）
- `internal/agentconfig/commands.go`、`templates/snippets/xray/api/default.yaml.tmpl`
- `tests/fixtures/example-project/config.yaml`、`tests/golden/xray/{usa1,usa2,auto}.json`
- `docs/cli-spec.md`、`schema-spec.md`、`generator-spec.md`、`install-systemd-security-spec.md`

未新增依赖。`internal/config/loader.go` 相对首版实现已恢复原样。

## 测试结果

- `go build ./...`：通过。
- `go vet ./...`：通过。
- `gofmt -l`：新增和修改的文件全部干净（`internal/agentconfig/commands.go`、`internal/domain/models.go`、`internal/graph/references.go` 的未对齐在 HEAD 上即已存在，本次未动）。
- `go test ./...`：除既有 `internal/generator/sub` golden mismatch（Tailscale 占位与 ADS 组，`git stash` 后在干净树上同样失败）外全部通过。
- 真机联调（`socks33`，Debian 6.1 / Xray 26.3.27）：在 `/tmp` 起一次性 xray 实例（独立高位端口 30085/34000/34100），不触碰该机上的生产 stack；测试后已清理，生产服务状态与联调前一致。
  - `xray api rmu` 对「用户不存在」和「tag 不存在」都返回 `Removed 0 user(s) in total.` 且 **exit 0**，确认退出码不能当成功信号；成功时为 `Removed 1 user(s) in total.`。
  - `xray api adu` 接受 `DumpsInboundUserPatch` 生成的载荷形状，输出 `Added 1 user(s) in total.`；统计行前会有 VMess 弃用警告，正则按行首匹配不受影响。
  - `psctl user disable/enable` 全流程：状态落盘、配置重新生成、`xray api inbounduser` 确认运行中实例的用户随之增删。
  - 假成功检测：先手工 `rmu` 摘掉用户，再跑 `psctl user disable`，xray 报 0 个成功且 exit 0，psctl 正确判为失败并输出 xray 原始错误和 `psctl restart xray/usa1`。
  - 跨重启存活：用重新生成的配置重启实例后，被禁用户不再出现。
  - 写盘守卫：改端口造成非用户差异后再 `disable`，exit 1 且 `disabled.json` 未写入任何条目。
  - 生产配置上的只读命令：`user list` 正确列出 7 个 stack 的 vmess 和 shadowsocks 用户；`doctor` 报出全部 stack 未启用 HandlerService、`psctl user` 需要 restart 才生效。
  - 顺带印证：该机所有存量 stack 都显式钉着 `services: [StatsService]`，默认值改动对存量安装确实是 no-op。
- 手工验证（本机，用假 xray 脚本覆盖真实调用路径）：
  - `psctl user disable user2`：状态落盘、生成配置去掉该用户、`Applied live`、socks5 跨协议提示。
  - 假 xray 返回 `Removed 0 user(s) in total.` 且 exit 0：识别为失败并给出原始输出和 `psctl restart xray/usa1`。
  - `psctl doctor`：输出 HandlerService 取舍说明与陈旧条目 issue。
  - 模拟存量安装（`config.yaml` 与 stack 均钉 `[StatsService]`）：`render xray` 输出无差异。

## 评审问题清单与处理结果

正确性 agent：

- `xray api rmu/adu` 单用户失败也 exit 0，退出码被当成功信号：已修，改为解析 `Removed/Added N user(s) in total.` 并要求 `N >= 1`。
- `UsersOnlyChange` 放行无关的 clients 变更：已修，改为要求差异恰好等于本次启停。
- 状态先落盘导致校验失败时半应用、且重跑被幂等判断挡掉：已修，改为先校验后落盘。
- 打印的 `psctl restart usa1 usa2` 是非法命令（`MaximumNArgs(1)`）：已修，逐条输出。
- `internal/xrayapi` 零测试且无 fake 缝：已补 10 个测试（假 xray 脚本固定 argv 形状、计数判定、拒绝前导 `-`），并加 `newXrayAPIClient` 包级缝。
- 陈旧条目不可见不可删：已补 `user list` 提示、`user enable` 清除、`doctor` issue。
- 版本错误信息缺路径：已补。
- `disabled.json` 不在 `StandardMetadataRules`：已注册。
- 双次读取状态导致快照不一致：已合并为 `resolveUserScope` 内一次读取。
- `PROFILE` 列暗示不存在的粒度：已在 `Long` 和 schema-spec 说明状态按 `(stack, user)` 记录。

安全 agent：

- 默认开 `HandlerService` 等于本机无鉴权管理面：按用户决定保持默认开，`doctor` 陈述取舍。
- 禁用在校验失败时 fail-open：同上，已随顺序调整修复。
- `email` 未校验可注入位置参数：已在 `xrayapi` 侧拒绝前导 `-` 和空值。
- 跨协议入口无提示：已加 warning。
- 临时文件在共享 `/tmp`：已改到 `runtime/`。
- 确认无缺口：socks5/http 免认证降级隔离（三层协议过滤）、shadowsocks 退回共享密码（双层 fail-loud）、`api.listen` 强制 loopback、多 stack 端口冲突 fail-closed、无 shell 注入、`disabled.json` 权限与原子写。

设计一致性 agent：

- 缺 `docs/delivery/` 交付记录：即本文。
- `LoadStacks` 副作用放大失败面并强制「先写后校验」：已改为 `BuildOptions` 注入缝。
- `disabled.json` 未进 metadata 规则和安全规格权限表：已补。
- `UsersOnlyPlan` 用路径字符串判断 xray：已改用 `FileChange.Service`。
- `EnabledInboundUsers` 是无人调用的导出：已降为不导出。
- `Since` 未走时钟缝：已加 `userToggleNow`。
- help 中英混排、缺 TARGET 规则表、`clash/NAME` 会给误导性错误：已全部改为英文并补 TARGET 说明与显式拒绝。
- `generator-spec.md` 的输入描述失效、`schema-spec.md` 缺 `disabled.json` 定义：已补。
- 判定为不需处理：`writeFileAtomic` 重复（仓库现有 7 份同实现副本，抽取属独立的全仓库清理）；3.7.1 编号、命令分组、README 不列配置管理子命令均合群。
- 更正一处 agent 判断：该 agent 称默认值改动会让所有存量 stack 的生成配置变化并触发全量重启，并以 golden diff 为证据。golden 变化实为本次同步修改测试 fixture 所致；已实测存量安装渲染结果无差异。

## 风险与后续建议

- 真机联调已在 Xray 26.3.27 上完成（见测试结果）。若目标机器上的 xray 版本差异较大，仍建议首次使用时盯一次输出。
- 若 xray 未来改动 `Removed/Added N user(s) in total.` 的措辞，计数解析会失配并报「did not report a user count」。这是刻意的 fail-loud 取舍：宁可误报失败（状态已落盘，restart 即收敛），也不能把「什么都没删」当成删除成功。
- 无文件锁，并发的 `psctl user` 调用是 read-modify-write，`writeFileAtomic` 保证不损坏但会丢更新。仓库全局没有锁机制，若后续要支持并发运维操作，应作为独立任务统一引入。
- 删除 `runtime/disabled.json` 会让所有禁用静默恢复（fail-open），而文件损坏是 fail-closed，两个方向不一致。当前依赖 `doctor` 的陈旧条目检查兜底。
- native backup 不含 `disabled.json`，换机恢复后禁用状态会丢。已在 schema-spec 写明；若需要随备份迁移，应作为 backup schema 的独立变更。
- 被禁用户的订阅仍然发放节点（明示设计）。这意味着「临时停用」而非「彻底吊销」；需要吊销时仍应改配置文件并 restart。
