# mihomo 固定版本下载 metadata 匹配修复说明

## 问题背景

`ps-agent install all` 安装 mihomo 时，如果配置使用固定版本且省略 `v`，例如：

```yaml
install:
  mihomo:
    version: 1.19.27
    source: auto
```

删除已安装 binary 后再次执行安装，旧逻辑会生成缺少 `v` 的 GitHub release URL。

## 根因分析

首次安装如果走 `latest`，代码会读取 GitHub release metadata，并直接使用上游返回的 `browser_download_url`，URL 中天然带有正确的 `v`。

固定版本路径不会读取 metadata，而是在本地用配置值拼接 URL：

```text
https://github.com/MetaCubeX/mihomo/releases/download/<version>/<asset>
```

因此 `version: 1.19.27` 会被原样拼成缺少 `v` 的 tag 和 asset 名。更深一层的问题是，GitHub release 资产名并不是稳定协议，继续手动拼 URL 也容易遗漏 `compatible`、平台命名变化或上游资产名调整。

## 修复方案

固定版本改为读取 GitHub release tag metadata：

- 按候选 tag 下载对应 release metadata，避免受 releases 列表分页限制。
- mihomo / xray 双向兼容 `v` 前缀，例如 `1.19.27` 会优先查询 `v1.19.27`，`v1.19.27` 也会回退查询 `1.19.27`。
- 从 release assets 中选择真实 `browser_download_url`。
- mihomo 在 Linux amd64 平台选择资产时要求资产名包含 `compatible`，避免下载到兼容性不符合预期的 binary。
- xray 和 geo 也统一从 metadata 中选择资产 URL，不再手动拼接 release URL。

## 文件变更

- `internal/install/install.go`
  - 新增固定版本 GitHub release tag metadata 解析。
  - 新增 release tag 候选逻辑，兼容省略 `v` 的配置。
  - 新增 release asset 选择逻辑，mihomo Linux amd64 必须匹配 `compatible`。
- `internal/install/install_test.go`
  - 补充固定版本通过 metadata 选择真实 URL 的回归测试。
  - 补充配置带 `v` 但 metadata tag 不带 `v` 的回归测试。
  - 补充 mihomo Linux amd64 选择 `compatible` 资产的回归测试。
  - 补充 mihomo Linux amd64 拒绝非 `compatible` 资产的回归测试。
  - 补充 xray 和 geo 的 metadata 资产选择测试。

## 验证结果

已通过：

```bash
go test ./internal/install
go test ./...
```

## 风险

该修复会让固定版本 GitHub 托管源先请求 release tag metadata。普通 URL source 不受影响；`latest` 保持既有 metadata 解析路径。固定版本如果 tag 不存在或对应平台没有兼容资产，会返回清晰错误。
