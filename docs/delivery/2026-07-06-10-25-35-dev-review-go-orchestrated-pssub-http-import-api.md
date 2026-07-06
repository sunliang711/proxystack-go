# pssub HTTP 导入接口交付记录

## 任务背景

用户希望 `pssub serve` 提供 HTTP 接口，用于导入 `psctl sub export` 生成的订阅 bundle。最终方案为：不使用 token，通过 `config.yaml` 开关启用，启用后启动独立 localhost/loopback admin listener，便于通过 SSH 隧道调用。

## 编排方案

- 开发 agent：实现配置模型、HTTP 导入接口、测试和文档。
- review agent：独立审查安全边界、listener 生命周期、上传限制、数据写入和文档一致性。
- 主 Agent：整合开发结果、修复 review 问题、复审到无阻断/警告问题后交付。

## 实现方案

- 新增 `import_api` 配置：
  - `enabled` 默认 `false`。
  - `listen` 默认 `127.0.0.1:3004`，启用时必须是 loopback 或 `localhost`。
  - `max_bundle_bytes` 默认 `67108864`，同时限制上传 zip 大小和解压后的 input 总大小。
- `pssub serve` 在 `import_api.enabled=true` 时启动独立 admin HTTP server。
- 新增 `POST /admin/import-bundle?replace_all=true|false`：
  - 只接受本机回环 `RemoteAddr`，不信任代理头。
  - 上传 body 先写入 `<base-dir>/.imports/*.zip` 临时文件，处理完成后删除。
  - 使用 mutex 串行化导入请求。
  - 复用订阅 bundle 校验和导入逻辑，只接受 `proxystack.sub-bundle`。
  - 导入成功后立即 `Reload()`，reload 成功才返回 200。
- bundle 读取增加解压侧防护：
  - `manifest.json` 解压后最大 1MiB。
  - HTTP import 路径通过 `ExtractBundleInputsWithLimit` 限制 input 解压总大小。
  - 原 CLI `pssub import` 保持原签名和行为。

## 文件与配置变更

- `internal/config/transport.go`
- `internal/config/loader_test.go`
- `internal/generator/sub/bundle.go`
- `internal/subserver/server.go`
- `internal/subserver/server_test.go`
- `docs/schema-spec.md`
- `docs/http-subserver-spec.md`
- `docs/cli-spec.md`
- `docs/deployment.md`

未新增依赖，未修改 `psctl sub export` bundle 格式。

## 测试结果

- `gofmt`：通过。
- `git diff --check`：通过。
- `go test ./internal/config ./internal/subserver ./internal/cli/sub -count=1`：通过。
- `go test ./internal/generator/sub -run 'TestBundle|TestWriteBundle|TestExtractBundle' -count=1`：通过。
- 开发 agent 执行过 `go test ./internal/cli/sub ./internal/generator/sub -count=1`：`internal/cli/sub` 通过，`internal/generator/sub` 仍失败在既有 `TestRenderSubscriptionsMatchGolden`，差异为 Tailscale 占位注释和 ADS group 输出，本次未修改订阅模板或 golden。

## 评审问题清单与处理结果

- 解压后 input 总大小未受 `max_bundle_bytes` 约束：已新增 `ExtractBundleInputsWithLimit` 并用于 HTTP import。
- `manifest.json` 未受解压大小限制：已增加 1MiB manifest 限制和测试。
- `serve` 文档副作用仍写只读 inputs：已修正为开启 import_api 时会写 `.imports` 和 `inputs`。
- schema 中 `listen` 默认值仍为 `0.0.0.0:3003`：已同步为 `127.0.0.1:3003`。
- 缺少 `replace_all=true` 成功路径、真实 listener 启动、临时文件清理和 listener 失败清理测试：已补充。

复审结论：未发现阻断问题，未发现警告问题，最终差异可以进入交付。

## 风险与后续建议

- HTTP import 不使用 token，安全边界依赖 config 开关、loopback listener 和 `RemoteAddr` 回环校验；部署反代时不要暴露 `/admin/*`。
- 推荐通过 SSH 隧道调用 `127.0.0.1:3004`。
- 全量 `internal/generator/sub` golden mismatch 是既有问题，建议另起任务处理。
