# Go 版部署说明

## 组件边界

- `psctl`：管理 agent 配置、stack、runtime 生成物、服务管理器和核心下载。
- `pssub`：只消费自身 base dir 下的 `config.yaml` 与 `inputs/`，提供订阅 HTTP 服务。
- `pssub` 不读取 agent `config.yaml` 或 `stacks/*.yaml`。

## 本地 agent 部署

推荐先使用 Go 二进制 bootstrap 安装 CLI，再由 `psctl setup` 完成 agent 初始化、核心安装和服务文件安装：

```bash
sudo scripts/install-agent.sh
sudo /usr/local/bin/psctl --base-dir /opt/proxystack setup
```

脚本默认从当前项目的 GitHub Release 仓库 `sunliang711/proxystack-go` 下载并安装当前平台二进制到 `/usr/local/bin`，默认版本为 `latest`。如需固定版本、显式指定仓库或使用本地源码构建：

```bash
sudo scripts/install-agent.sh --version v1.2.3
sudo scripts/install-agent.sh --repo OWNER/REPO
sudo scripts/install-agent.sh --source /path/to/proxystack-go
```

如需安装后立即生成 runtime 并启动服务，可继续显式执行：

```bash
sudo /usr/local/bin/psctl --base-dir /opt/proxystack setup
sudo /usr/local/bin/psctl --base-dir /opt/proxystack start
```

脚本只做：

- 创建 `proxystack:proxystack` 用户和托管目录。
- 默认下载 GitHub Release 中的 `proxystack-go_<os>_<arch>.tar.gz` 兼容别名；指定固定版本时下载 `proxystack-go_<version>_<os>_<arch>.tar.gz`，其中 `<os>` 为 `linux` 或 `macos`，并用 `SHA256SUMS` 校验；传入 `--source` 时改为本地 `go build`；如需改用其他仓库，可传入 `--repo OWNER/REPO` 或设置 `PROXYSTACK_RELEASE_REPO`。
- 将 `psctl` 和 `pssub` 安装到 `/usr/local/bin`，并保留 `ps-agent`、`ps-sub` 兼容软链接。
- 可选执行 `psctl setup local`。

脚本本身不安装 mihomo、xray-core 或 geo 数据；这些由 `psctl setup deps` 或 `psctl setup` 管理。

CLI 服务管理器支持：

```bash
psctl --service-manager auto|systemd|launchd ...
pssub --service-manager auto|systemd|launchd ...
```

`auto` 在 Linux 使用 systemd，在 macOS 使用 launchd。当前 shell bootstrap 脚本面向 Linux/systemd；macOS 可直接使用已构建的 CLI 与 `--service-manager launchd` 安装 plist。

## 本地 sub-only 部署

```bash
sudo scripts/install-sub-local.sh \
  --import-bundle /opt/proxystack/publish/sub-bundle.zip \
  --start
```

该脚本会安装 Go CLI 到 `/usr/local/bin`、准备 `/opt/proxystack-sub`，并可选导入订阅发布包。默认同样从当前项目的 GitHub Release 仓库 `sunliang711/proxystack-go` 下载，可通过 `--version v1.2.3` 固定版本，通过 `--repo OWNER/REPO` 显式指定仓库，或通过 `--source /path/to/proxystack-go` 使用本地源码构建。sub 服务运行期只依赖：

```text
/opt/proxystack-sub/config.yaml
/opt/proxystack-sub/inputs/
```

独立 sub-only 部署默认使用 `/opt/proxystack-sub`。`setup local` 会安装系统服务文件，需要管理员权限；后续配置、导入和前台运行可按目录 owner 执行，例如：

```bash
sudo pssub --base-dir /opt/proxystack-sub setup local
pssub --base-dir /opt/proxystack-sub config
pssub --base-dir /opt/proxystack-sub config check
pssub --base-dir /opt/proxystack-sub import /path/to/sub-bundle.zip
pssub --base-dir /opt/proxystack-sub serve
```

使用系统服务时：

```bash
sudo pssub --base-dir /opt/proxystack-sub setup local
sudo pssub --base-dir /opt/proxystack-sub start
sudo pssub --base-dir /opt/proxystack-sub status
sudo pssub --base-dir /opt/proxystack-sub logs -f
```

此时 sub root 为 `/opt/proxystack-sub`。

## Docker sub 部署

镜像只包含 `pssub`，不包含 mihomo/xray：

```bash
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack-sub
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack-sub/inputs
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack-sub/templates
sudo install -o 10001 -g 10001 -m 0640 /path/to/sub-config.yaml /opt/proxystack-sub/config.yaml
docker compose -f docker-compose.sub.yml up -d --build
```

如果宿主机已安装同架构 `pssub`，也可以先生成默认配置后再调整 owner：

```bash
sudo pssub --base-dir /opt/proxystack-sub setup local
sudo chown -R 10001:10001 /opt/proxystack-sub
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
- `/data` volume 持久化 host base dir，容器内运行 `pssub --base-dir /data serve`。
- `/tmp` 使用受限 tmpfs。

## Python 版迁移说明

旧 Python venv 部署中的 `.venv` 不再是 Go 版运行依赖。迁移时建议：

1. 保留原 `/opt/proxystack/config.yaml` 和 `stacks/`；将旧 `/opt/proxystack/sub/config.yaml` 与 `sub/inputs/` 迁移到 `/opt/proxystack-sub/config.yaml` 和 `/opt/proxystack-sub/inputs/`。
2. 执行 `scripts/install-agent.sh --no-setup-local` 安装 Go CLI。
3. 执行 `psctl validate`。
4. 执行 `psctl check` 预览 runtime 变化。
5. 使用 `psctl setup local` 重新写入 Go 版服务文件；Linux 默认写入 systemd unit，macOS 可使用 `--service-manager launchd` 写入 launchd plist。

不建议直接复用旧 Python venv 内 console scripts。
