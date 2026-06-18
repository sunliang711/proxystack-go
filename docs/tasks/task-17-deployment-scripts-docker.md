# T17 部署脚本与 Docker sub

## 目标

迁移或重写安装脚本、sub 本地部署和 Docker 部署。

## 范围

- Go binary bootstrap 脚本。
- sub-only 本地部署。
- Dockerfile。
- docker-compose。

## 输入文档

- [install-systemd-security-spec.md](../install-systemd-security-spec.md)
- 源项目 `scripts/`
- 源项目 `Dockerfile.sub`
- 源项目 `docker-compose.sub.yml`
- 源项目 `docs/deployment.md`

## 交付物

- `scripts/install-agent.sh`
- `scripts/install-sub-local.sh`
- `scripts/deploy-sub-docker.sh`
- Dockerfile/compose。

## 实现步骤

1. 设计 Go binary 安装目录。
2. 改写 agent bootstrap。
3. 改写 sub local install。
4. 改写 Dockerfile.sub。
5. 保留安全运行参数。

## 验收标准

- Shell 只做 bootstrap。
- sub 镜像不包含 xray/mihomo。
- Docker 默认非 root。
- Docker 默认 read-only。
- Docker `cap_drop: ALL`。
- `/data` 持久化。

## 依赖

- T10
- T15

## 风险

- Go binary 发布方式和旧 Python venv 部署不同，需要在部署文档中明确迁移路径。
