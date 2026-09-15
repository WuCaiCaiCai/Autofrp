# Autofrp

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

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh \
  && sudo bash autof.sh
```

首次运行会询问是否安装为命令 `autof`，同意后以后直接：

```bash
sudo autof
```

也可以直接管道运行：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh)
```

## 自安装 / 自更新

脚本顶部已配置好 `SELF_URL` 指向本仓库：

```bash
SELF_URL="${AUTOF_SELF_URL:-https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh}"
```

- `autof self-install`：把脚本安装到 `/usr/local/bin/autof`
- `autof self-update`：从本仓库拉取最新脚本并覆盖自身
- 也可用环境变量临时覆盖：`AUTOF_SELF_URL=... autof self-update`

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

## 版权与免责声明

- **本项目非官方**：Autofrp 是第三方辅助脚本，与 [fatedier/frp](https://github.com/fatedier/frp) 项目及其作者**没有任何隶属、赞助或背书关系**。
- **frp 版权**：`frp` / `frps` / `frpc` 的著作权归其原作者所有，遵循 [Apache License 2.0](https://github.com/fatedier/frp/blob/master/LICENSE)。本脚本不包含、不修改、不重新分发 frp 的源代码或二进制，仅在运行时从官方 GitHub Release 下载，相关权利与许可请以官方仓库为准。
- **本脚本许可**：本仓库自身的代码采用 [MIT License](LICENSE)。
- **免责**：本脚本按「现状」提供，不提供任何明示或暗示的担保。使用本脚本进行端口映射、内网穿透等操作时，请自行确保符合当地法律法规、云服务商/网络服务商的条款，以及你所有服务（如 Minecraft、Emby）的授权与许可。因使用本脚本产生的任何后果由使用者自行承担。
