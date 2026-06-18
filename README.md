# proxystack-go

`proxystack-go` 是 Go 版 proxystack，提供两个命令行入口：

- `ps-agent`：管理 agent 配置、stack、runtime 生成物、核心下载和服务生命周期。
- `ps-sub`：独立运行订阅 HTTP 服务，只读取 `<base-dir>/sub/config.yaml` 和 `<base-dir>/sub/inputs/`。

默认工作目录为 `/opt/proxystack`。如需使用其他目录，所有命令都可以通过 `--base-dir DIR` 指定。

## 安装

### 从 Release 安装

Linux/systemd 环境推荐使用安装脚本：

```bash
sudo scripts/install-agent.sh
sudo /usr/local/bin/ps-agent --base-dir /opt/proxystack setup
```

固定版本、指定 Release 仓库或从本地源码构建：

```bash
sudo scripts/install-agent.sh --version v1.2.3
sudo scripts/install-agent.sh --repo OWNER/REPO
sudo scripts/install-agent.sh --source /path/to/proxystack-go
```

如果希望初始化、安装依赖和服务文件后直接启动已启用服务：

```bash
sudo /usr/local/bin/ps-agent --base-dir /opt/proxystack setup --start
```

脚本会安装 `ps-agent` 和 `ps-sub` 到 `/usr/local/bin`。mihomo、xray-core 和 geo 数据由 `ps-agent setup` 或 `ps-agent install all` 管理。

### 从源码构建

本项目使用 Go `1.25.0`。

```bash
make build
./ps-agent version
./ps-sub version
```

构建 Linux 静态二进制：

```bash
make build-linux
```

### 只部署订阅服务

订阅服务可独立部署，不需要 agent 的 `config.yaml` 或 `stacks/`：

```bash
sudo scripts/install-sub-local.sh \
  --import-bundle /opt/proxystack/publish/sub-bundle.zip \
  --install-systemd \
  --start
```

也可以使用 Docker 运行 `ps-sub`：

```bash
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack/sub
sudo install -d -o 10001 -g 10001 -m 0750 /opt/proxystack/sub/inputs
sudo install -o 10001 -g 10001 -m 0640 /path/to/sub-config.yaml /opt/proxystack/sub/config.yaml
docker compose -f docker-compose.sub.yml up -d --build
```

## 配置

### agent 配置

初始化默认目录和配置：

```bash
sudo ps-agent --base-dir /opt/proxystack init --external-host proxy.example.com
```

主要文件和目录：

| 路径 | 说明 |
| --- | --- |
| `/opt/proxystack/config.yaml` | agent 全局配置 |
| `/opt/proxystack/stacks/` | stack 配置目录 |
| `/opt/proxystack/runtime/` | runtime manifest、锁文件和生成物 |
| `/opt/proxystack/publish/` | 订阅包和备份包输出目录 |
| `/opt/proxystack/sub/` | 本地订阅服务配置和 inputs |

`config.yaml` 中常用字段：

```yaml
version: 1
external_host: proxy.example.com

port_ranges:
  xrelay_inbound: 24000-24999
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999

security:
  require_auth_for_public_socks_http: true
  allow_noauth_public: false
```

`external_host` 用于生成订阅节点地址，执行 `ps-agent sub export` 前必须配置。更多字段见 [配置与数据 Schema](docs/schema-spec.md)。

### stack 配置

创建 stack：

```bash
sudo ps-agent --base-dir /opt/proxystack add usa1 --template pair
sudo ps-agent --base-dir /opt/proxystack add auto --template auto-url-test --members usa1
```

编辑全局配置或指定 stack：

```bash
sudo ps-agent --base-dir /opt/proxystack config
sudo ps-agent --base-dir /opt/proxystack config usa1
```

校验配置并预览 runtime 变化：

```bash
sudo ps-agent --base-dir /opt/proxystack validate
sudo ps-agent --base-dir /opt/proxystack check
```

### 订阅服务配置

初始化订阅目录：

```bash
sudo ps-sub --base-dir /opt/proxystack init
```

订阅服务配置固定为 `<base-dir>/sub/config.yaml`。不要在该 YAML 中写 `data_dir`，运行数据目录固定由 `--base-dir` 推导为 `<base-dir>/sub`：

```yaml
listen: 0.0.0.0:3003
access:
  type: token
  token: change-me
managed_config:
  enabled: true
  public_base_url: https://sub.example.com
  interval: 86400
  strict: true
```

查看和校验订阅服务配置：

```bash
sudo ps-sub --base-dir /opt/proxystack config show
sudo ps-sub --base-dir /opt/proxystack config check
```

如果编辑配置时报 `field data_dir not found`，删除 `sub/config.yaml` 中的 `data_dir` 字段，并在命令中通过 `--base-dir` 指定目录：

```bash
sudo ps-sub --base-dir /opt/proxystack config
sudo ps-sub --base-dir /opt/proxystack serve
```

## 使用

### agent 常用流程

```bash
# 初始化配置、安装 mihomo/xray/geo 并安装服务文件
sudo ps-agent --base-dir /opt/proxystack setup

# 创建或调整 stack 后先校验和预览
sudo ps-agent --base-dir /opt/proxystack validate
sudo ps-agent --base-dir /opt/proxystack check

# 启动和查看服务
sudo ps-agent --base-dir /opt/proxystack start
sudo ps-agent --base-dir /opt/proxystack status
sudo ps-agent --base-dir /opt/proxystack logs -f

# 生成订阅发布包
sudo ps-agent --base-dir /opt/proxystack sub export
```

### 订阅服务常用流程

```bash
# 导入 agent 生成的订阅包
sudo ps-sub --base-dir /opt/proxystack import /opt/proxystack/publish/sub-bundle.zip

# 查询、校验或编辑订阅 input
sudo ps-sub --base-dir /opt/proxystack input list
sudo ps-sub --base-dir /opt/proxystack input show manual
sudo ps-sub --base-dir /opt/proxystack input validate
sudo ps-sub --base-dir /opt/proxystack input edit manual

# 前台运行订阅服务
sudo ps-sub --base-dir /opt/proxystack serve

# 或安装为系统服务后运行
sudo ps-sub --base-dir /opt/proxystack service install
sudo ps-sub --base-dir /opt/proxystack start
sudo ps-sub --base-dir /opt/proxystack status
```

HTTP 路由：

| 路由 | 说明 |
| --- | --- |
| `GET /health` | 健康检查 |
| `GET /sub/:token/:user` | Clash 订阅 |
| `GET /premium_sub/:token/:user` | Premium Clash 订阅 |
| `GET /surge_sub/:token/:user` | Surge 订阅 |

`access.type=none` 时也支持不带 token 的订阅地址，但公网部署建议使用 `access.type=token`。

### 诊断和备份

```bash
sudo ps-agent --base-dir /opt/proxystack doctor
sudo ps-agent --base-dir /opt/proxystack ipinfo usa1
sudo ps-agent --base-dir /opt/proxystack export

sudo ps-sub --base-dir /opt/proxystack doctor
```

## 更多文档

- [开发文档索引](docs/README.md)
- [部署说明](docs/deployment.md)
- [CLI 命令规格](docs/cli-spec.md)
- [配置与数据 Schema](docs/schema-spec.md)
- [订阅 HTTP 服务规格](docs/http-subserver-spec.md)
