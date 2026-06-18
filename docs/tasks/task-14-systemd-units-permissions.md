# T14 systemd unit 与权限

## 目标

迁移 unit 模板、hardening 和文件权限修复。

## 范围

- unit renderer。
- unit install/uninstall。
- 权限和 owner 修复。

## 输入文档

- [install-systemd-security-spec.md](../install-systemd-security-spec.md)
- 源项目 `docs/deployment.md`
- 源项目 systemd 测试。

## 交付物

- unit 模板。
- 权限工具。
- unit golden 或字段级测试。

## 实现步骤

1. 实现 xray unit。
2. 实现 mihomo unit。
3. 实现 sub unit。
4. 实现 `service install/uninstall`。
5. 实现 metadata 修复工具。

## 验收标准

- unit 只引用 generated 文件。
- `proxystack-sub.service` 只传 sub config。
- 目录 `0750`。
- 配置文件 `0640`。
- 二进制 `0750`。
- geo `0640`。
- owner 为 `proxystack:proxystack` 时可修复。

## 依赖

- T13

## 风险

- 权限修复需要在无 root 测试中 fake，不应要求真实 chown。
