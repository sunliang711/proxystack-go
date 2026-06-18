# T15 install/update 与自更新

## 目标

迁移 mihomo/xray/geo 下载、安装、更新和 self update。

## 范围

- install request。
- managed source。
- 下载器。
- 归档解包。
- 原子替换。
- 自更新 runner。

## 输入文档

- [install-systemd-security-spec.md](../install-systemd-security-spec.md)
- 源项目 `src/proxystack/install/service.py`
- 源项目 `tests/test_install.py`

## 交付物

- `internal/install`
- 安装更新 CLI。
- fake downloader tests。
- 安全负面测试。

## 实现步骤

1. 实现 target 展开。
2. 实现 managed source URL 生成。
3. 实现普通 URL SSRF 防护。
4. 实现 sha256 校验。
5. 实现 gzip/zip/tar 解包。
6. 实现原子替换和回滚。
7. 实现 self update runner。

## 验收标准

- 托管源 `auto/github/r2` fallback。
- 慢速切源。
- `.gz` 解压。
- geo 多文件事务回滚。
- `install` 跳过已存在。
- `update` 强制替换。
- `all` 不包含 `self`。
- 普通远端 URL 必须 sha256。
- 私网/本机/metadata 下载目标拒绝。

## 依赖

- T02

## 风险

- 不要把 systemd 重启混进 install/update。
