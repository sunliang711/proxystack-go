# install-agent latest 下载校验修复记录

## 问题背景

执行安装脚本下载 `sunliang711/proxystack-go v0.1.21 linux/amd64` 时，`proxystack-go_linux_amd64.tar.gz` 的 SHA256 校验失败。

## 根因分析

- 根因位置：release 二进制下载流程中的 `install_release_binaries`。
- 问题类型：下载地址稳定性。
- 触发条件：安装参数使用默认 `latest`，脚本解析出真实 tag 后只用于日志展示，实际归档和 `SHA256SUMS` 仍从 `latest/download` 别名下载。
- 为什么会发生：`latest/download` 在 release 更新或 CDN 缓存窗口中可能返回与当前 `SHA256SUMS` 不一致的资产。

## 修复方案

- 新增 `download_version`，当 `latest` 已解析为真实 tag 后，归档和 `SHA256SUMS` 都固定从真实 tag 下载。
- 保持资产名生成规则不变，避免改变已有 release 包命名兼容性。
- 同步修复 `scripts/install-agent.sh`、`scripts/install-sub-local.sh` 和 `scripts/lib/common.sh` 中的同源逻辑。

## 验证结果

- 通过：`bash -n scripts/install-agent.sh scripts/install-sub-local.sh scripts/lib/common.sh`
- 通过：`shellcheck scripts/install-agent.sh scripts/install-sub-local.sh scripts/lib/common.sh`
- 通过：mock 验证 `latest -> v0.1.21` 后，下载 URL 固定为 `/releases/download/v0.1.21/...`。
- 手工验证：GitHub v0.1.21 的 `proxystack-go_linux_amd64.tar.gz` 与 `SHA256SUMS` 当前匹配。

## 风险与后续建议

- 修复只影响 `latest` 下载路径；显式指定版本的下载行为保持不变。
