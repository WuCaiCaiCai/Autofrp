# Autofrpc

一键配置 NAT 小鸡上的 **frps（frp 服务端）** 的交互式脚本。

针对「便宜 NAT VPS 只能映射少量端口，存在外部端口 ↔ 内部端口映射」的场景，帮你：

- 探测本机网络环境（公网 IPv4 / NAT IPv4 / 公网 IPv6 / NAT IPv6）
- 管理服务商给的 **NAT 端口映射**
- 生成 **frps.toml**（服务端）
- 生成 **frpc.toml**（给家里机器/客户端复制）
- 一键下载安装 frps 并注册 systemd 开机自启
- 安装成可复用命令 `autof`，之后不用再重新拉脚本

> 适用场景：NAT 小鸡跑 frps，家里的机器跑 frpc，把 Minecraft / Emby 等内网服务穿透出去。

## 一键安装

把仓库上传到 GitHub 后，把下面的 `YOUR_GITHUB_NAME` 换成你的用户名：

```bash
curl -fsSL https://raw.githubusercontent.com/YOUR_GITHUB_NAME/Autofrpc/main/autof.sh -o autof.sh \
  && sudo bash autof.sh
```

首次运行会询问是否安装为命令 `autof`，同意后以后直接：

```bash
sudo autof
```

也可以直接管道运行（需要先把脚本里的 `SELF_URL` 改成你自己的仓库地址）：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/YOUR_GITHUB_NAME/Autofrpc/main/autof.sh)
```

## 配置 SELF_URL（用于自安装 / 自更新）

打开 `autof.sh`，把顶部这一行改成你的仓库地址：

```bash
SELF_URL="${AUTOF_SELF_URL:-https://raw.githubusercontent.com/YOUR_GITHUB_NAME/Autofrpc/main/autof.sh}"
```

之后即可使用 `autof self-install` 和 `autof self-update`。

## 使用

```bash
sudo autof                 # 交互主菜单（推荐）
sudo autof detect          # 仅探测网络环境
sudo autof nat             # 管理 NAT 端口映射
sudo autof gen frps        # 打印 frps.toml
sudo autof gen frpc        # 打印 frpc.toml（客户端）
sudo autof save            # 保存 frps.toml 与 frpc.toml
sudo autof install         # 下载安装 frps 并注册 systemd
sudo autof status          # 查看运行状态
sudo autof restart         # 重启
sudo autof logs            # 查看日志
sudo autof stop            # 停止
sudo autof uninstall       # 卸载服务与二进制
sudo autof self-install    # 安装为 /usr/local/bin/autof
sudo autof self-update     # 更新自身
```

## 主菜单

```
1) 重新探测环境
2) frps 基础设置          bindPort / token / TLS / Dashboard
3) NAT 端口映射管理       外部端口 <-> 内部端口，支持自动补齐建议
4) 服务穿透配置           MC Java / MC Bedrock / Emby / 自定义
5) 高级设置               KCP / QUIC / vhost / subDomainHost / 日志
6) 生成并预览 frps.toml
7) 生成并预览 frpc.toml   （给客户端复制）
8) 保存配置到磁盘
9) 安装/更新 frps 并启动
10) 接入信息汇总
11) 安装为 autof 命令
12) frps 运行状态/日志
```

## 支持的穿透类型

| 类型 | 说明 | 典型用途 |
| --- | --- | --- |
| tcp | TCP 端口转发 | Minecraft Java (25565) |
| udp | UDP 端口转发 | Minecraft Bedrock (19132) |
| http | 域名建站 | Emby (8096) |
| https | HTTPS 域名建站 | 带证书的站点 |
| stcp | 安全隧道 | 不暴露公网端口的点对点 |
| xtcp | 点对点穿透 | 流量不经 frps 中转 |
| tcpmux | 多路复用 | 单端口复用多服务 |

## 工作流程

1. **探测环境**：判断公网 IPv4 / NAT IPv4 / 公网 IPv6 / NAT IPv6，给出对外接入地址。
2. **配 NAT 映射**：按服务商分配的外部端口，录入「外部 → 内部」映射，脚本会校验重复与端口范围。
3. **配 frps**：设置 bindPort、token、TLS、面板等；`allowPorts` 自动收敛到已映射的内部端口。
4. **配服务**：添加 MC / Emby 等，选择内部远端端口（须在 NAT 映射内）。
5. **生成配置**：直接打印可复制的 `frps.toml` 和 `frpc.toml`，也可保存到磁盘。
6. **安装启动**：下载 frps、写配置、注册 systemd 并启动。

## 接入信息示例

假设出口公网 IP 为 `1.2.3.4`，服务商映射 `20002 -> 25565`：

- 玩家连接：`1.2.3.4:20002`（外部端口）
- frpc 客户端配置里 `remotePort = 25565`（内部端口）
- frpc 连接 frps 的地址：`1.2.3.4:<bindPort 对应的外部端口>`

## 文件与目录

| 路径 | 说明 |
| --- | --- |
| `/etc/autof/state.conf` | 基础设置 |
| `/etc/autof/nat_ports.conf` | NAT 端口映射表 |
| `/etc/autof/services.conf` | 服务穿透配置 |
| `/etc/autof/frps.toml` | 生成的 frps 配置 |
| `/etc/autof/clients/frpc.toml` | 生成的客户端配置 |
| `/usr/local/bin/frps` | frps 二进制 |
| `/etc/systemd/system/frps.service` | systemd 服务 |

非 root 运行时配置目录回退到 `~/.config/autof`。

## 环境要求

- Linux，bash 4+
- 依赖：`curl`、`ip`、`ss`、`tar`（缺失时会尝试用 apt / yum / apk 安装）
- 安装 frps 需要 root；仅探测和生成配置则不需要

## 更新

```bash
sudo autof self-update
```

或重新执行一键安装命令覆盖 `/usr/local/bin/autof`。

## 卸载

```bash
sudo autof uninstall       # 移除 frps 服务与二进制
sudo rm -rf /etc/autof     # 移除配置（谨慎）
sudo rm -f /usr/local/bin/autof
```
