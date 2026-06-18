# Go 版部署说明

## 组件边界

- `proxystack-agent` / `ps-agent`：管理 agent 配置、stack、runtime 生成物、systemd 和核心下载。
- `proxystack-sub` / `ps-sub`：只消费 `sub/config.yaml` 与 `sub/inputs/`，提供订阅 HTTP 服务。
- `ps-sub` 不读取 agent `config.yaml` 或 `stacks/*.yaml`。

## 本地 agent 部署

推荐使用 Go 二进制 bootstrap：

```bash
sudo scripts/install-agent.sh
sudo /usr/local/bin/ps-agent install all
sudo /usr/local/bin/ps-agent service install
```

脚本只做：

- 创建 `proxystack:proxystack` 用户和托管目录。
- `go build` 生成 `proxystack-agent` 与 `proxystack-sub`。
- 将 CLI 链接到 `/usr/local/bin`。
- 可选执行 `ps-agent init` 与 `ps-agent service install`。

脚本不安装 mihomo、xray-core 或 geo 数据；这些仍由 `ps-agent install all` 管理。

## 本地 sub-only 部署

```bash
sudo scripts/install-sub-local.sh \
  --import-bundle /opt/proxystack/publish/sub-bundle.zip \
  --install-systemd \
  --start
```

该脚本会安装 Go CLI、准备 `/opt/proxystack/sub`，并可选导入订阅发布包。sub 服务运行期只依赖：

```text
/opt/proxystack/sub/config.yaml
/opt/proxystack/sub/inputs/
```

独立 sub-only 部署只需要传入独立 base dir，例如：

```bash
ps-sub --base-dir /data/sub-only init
ps-sub --base-dir /data/sub-only config
ps-sub --base-dir /data/sub-only config check
ps-sub --base-dir /data/sub-only import /path/to/sub-bundle.zip
ps-sub --base-dir /data/sub-only serve
```

使用系统服务时：

```bash
sudo ps-sub --base-dir /data/sub-only service install
sudo ps-sub --base-dir /data/sub-only start
sudo ps-sub --base-dir /data/sub-only status
sudo ps-sub --base-dir /data/sub-only logs -f
```

此时 sub root 为 `/data/sub-only/sub`。

## Docker sub 部署

镜像只包含 `proxystack-sub`，不包含 mihomo/xray：

```bash
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack/sub
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack/sub/inputs
sudo install -o 10001 -g 10001 -m 0640 /path/to/sub-config.yaml /opt/proxystack/sub/config.yaml
docker compose -f docker-compose.sub.yml up -d --build
```

如果宿主机已安装同架构 `ps-sub`，也可以先生成默认配置后再调整 owner：

```bash
sudo ps-sub --base-dir /opt/proxystack init
sudo chown -R 10001:10001 /opt/proxystack/sub
```

或使用脚本：

```bash
sudo scripts/deploy-sub-docker.sh --build
```

默认安全参数：

- 非 root 用户 `10001:10001`。
- `read_only: true` / `--read-only`。
- `cap_drop: ALL` / `--cap-drop ALL`。
- `no-new-privileges:true`。
- `/data` volume 持久化 host base dir，容器内运行 `proxystack-sub --base-dir /data serve`。
- `/tmp` 使用受限 tmpfs。

## Python 版迁移说明

旧 Python venv 部署中的 `.venv` 不再是 Go 版运行依赖。迁移时建议：

1. 保留原 `/opt/proxystack/config.yaml`、`stacks/`、`sub/config.yaml` 和 `sub/inputs/`。
2. 执行 `scripts/install-agent.sh --no-init` 安装 Go CLI。
3. 执行 `ps-agent validate`。
4. 执行 `ps-agent check` 预览 runtime 变化。
5. 使用 `ps-agent service install` 重新写入 Go 版 systemd unit。

不建议直接复用旧 Python venv 内 console scripts。
