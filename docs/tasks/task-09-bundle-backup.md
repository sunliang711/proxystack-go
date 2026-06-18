# T09 订阅 bundle 与原生 backup

## 目标

迁移订阅发布包和原生配置备份。

## 范围

- Subscription bundle write/import。
- Native backup export/import。
- Manifest/hash/path 校验。
- 原子写入。

## 输入文档

- [schema-spec.md](../schema-spec.md)
- [generator-spec.md](../generator-spec.md)
- 源项目 `src/proxystack/generator/sub/config.py`
- 源项目 `src/proxystack/generator/backup/config.py`

## 交付物

- bundle 写入和导入模块。
- native backup 模块。
- zip 安全测试。

## 实现步骤

1. 实现 bundle manifest。
2. 实现 input 文件打包。
3. 实现 zip 成员路径校验。
4. 实现 manifest hash 校验。
5. 实现 `replace-all` 先校验后替换。
6. 实现 native backup manifest。
7. 实现 schema 混用拒绝。

## 验收标准

- bundle 和 native backup schema 互相拒绝。
- manifest 文件集合必须完全匹配。
- hash mismatch 不写入。
- 路径穿越、反斜杠、未知成员拒绝。
- `replace-all` 全部校验成功后才清理旧 input。
- backup 不包含 runtime/sub inputs。

## 依赖

- T07

## 风险

- zip 安全不可只测 happy path。
