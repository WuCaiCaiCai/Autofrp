# Autofrp

在 **NAT 型 VPS** 上一键部署 frp 服务端（`frps`），把内网服务（Minecraft 等）暴露到公网。
全中文交互引导，脚本只跑在 **服务端（VPS）** 上，内网机器不用装。

```
玩家 ──► 公网IP:对外端口 ──► VPS(frps) ◄── frpc ── 内网机器(Minecraft)
```

## 特性

- 一条命令起 `frps`：自动下载并注册 systemd 开机自启。
- 内置 **Minecraft Java**（TCP `25565`）与 **Bedrock**（UDP `19132`）预设，也可自定义 TCP/UDP。
- 自动生成 `frps.toml` 与 `frpc.toml`：服务端配置写盘，客户端配置一键打印，复制即用。
- 自动探测公网 IPv4/IPv6，支持「仅 IPv4 / 仅 IPv6 / 均可」双栈。
- 一条命令完全卸载。

## 快速开始

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh
sudo bash autof.sh
```

首次运行会引导设置**控制端口**；若未检测到 `frps`，会询问是否自动下载并启动。结束时可安装为 `autof` 命令，之后直接：

```bash
sudo autof
```

## 主界面

```
════════════════════════════════════════════════════════════
  Autofrp  ·  frps 服务端
════════════════════════════════════════════════════════════
  IPv4  NAT IPv4  108.165.122.146 ◀ 首选
  IPv6  NAT IPv6  2602:f9f3:3000::185 （备选）
  状态  ● 运行中
  控制端口 30085    网络偏好 均可
  ── 穿透服务 ──
  mc-java        tcp  25565 → 25565
  mc-bedrock     udp  19132 → 19132
════════════════════════════════════════════════════════════

  1) 添加穿透服务
  2) 管理服务 / 打印 frpc 配置
  3) 安装 / 启动 frps
  4) frps 管理（启动·停止·重启·状态·日志·控制端口·网络偏好）
  5) 卸载
  0) 退出
```

## 菜单说明

- **添加穿透服务**：`Minecraft Java`（TCP，本机 `25565`）/ `Minecraft Bedrock`（UDP，本机 `19132`）/ `自定义 TCP` / `自定义 UDP`。端口可改，对外端口默认与本机相同；加完直接告诉你玩家连接地址。
- **管理服务 / 打印 frpc 配置**：列表里输入序号可查看 / 编辑 / 删除单个服务；输入 `a` 一次性打印完整 `frpc.toml`，复制到内网机器即可。
- **安装 / 启动 frps**：下载 frps（GitHub + 镜像回退）并注册 systemd 启动。
- **frps 管理**：启动、停止、重启、状态、日志、修改控制端口、网络偏好。
- **卸载**：完全卸载（服务 + 程序 + 配置 + `autof` 命令）。

## 端口规则

| 项 | 说明 |
| --- | --- |
| 控制端口 | `frpc` 连接 `frps` 用，全局一个，和具体服务无关 |
| 本机端口 | 内网服务实际监听的端口（如 MC Java 的 `25565`） |
| 对外端口 | 玩家 / 外部连接用的公网端口，默认等于本机端口 |

- `frps` 在 VPS 上监听**对外端口**，玩家连接地址固定为 `公网IP:对外端口`。
- NAT VPS：在**服务商网页端**把控制端口和每个对外端口都映射为 `端口 -> 同端口`。
- 有独立公网 IP 的 VPS 无需网页端映射。

## 网络偏好（IPv4 / IPv6 / 均可）

在「frps 管理 → 网络偏好」或命令行设置，会同时影响 `frps` 监听与 `frpc` 连接地址：

| 偏好 | `frps` 的 `bindAddr` | `frpc` 的 `serverAddr` |
| --- | --- | --- |
| 仅 IPv4 | `0.0.0.0` | IPv4 公网地址 |
| 仅 IPv6 | `::` | IPv6 公网地址 |
| 均可 | `::`（双栈） | 优先 IPv4，并注释一行 IPv6 备选 |

> 「均可」的双栈监听依赖内核 `net.ipv6.bindv6only=0`（Linux 默认）。

## 完整示例：Minecraft

场景：VPS 是 NAT 型、跑 `frps`；内网机器跑 Minecraft（Java `25565` / Bedrock `19132`），想让玩家连。

### 1. 服务商网页端设置映射（都是 `X -> X`）

| 外部端口 | 内部端口 | 用途 |
| --- | --- | --- |
| `30085` | `30085` | 控制端口 |
| `30008` | `30008` | Minecraft Java |

### 2. VPS 上运行脚本

```bash
sudo autof
```

- 首次引导：控制端口填 `30085`。
- `1) 添加穿透服务` → `1) Minecraft Java`：本机端口 `25565`，对外端口 `30008`。
- `2) 管理服务 / 打印 frpc 配置` → 输入 `a`：复制打印出的 `frpc.toml`。
- `3) 安装 / 启动 frps`。

> Bedrock 同理：选 `2) Minecraft Bedrock`（UDP `19132`），再在网页端加一条对应映射。

### 3. 内网机器上运行 frpc

1. 从 [frp Releases](https://github.com/fatedier/frp/releases) 下载对应平台压缩包，解压得到 `frpc`。
2. 把复制的 `frpc.toml` 放到同目录，内容大致如下：

   ```toml
   # frpc 客户端配置（在内网机器上运行 frpc）
   serverAddr = "108.165.122.146"
   serverPort = 30085
   auth.method = "token"
   auth.token = "脚本生成的token"
   transport.tls.enable = true

   # 玩家连接: 108.165.122.146:30008
   [[proxies]]
   name = "mc-java"
   type = "tcp"
   localIP = "127.0.0.1"
   localPort = 25565
   remotePort = 30008
   ```

3. 启动：`./frpc -c frpc.toml`（Windows：`frpc.exe -c frpc.toml`），看到 `start proxy success` 即成功。

### 4. 验证

玩家连接 **`公网IP:30008`**。

## 命令行

```bash
sudo autof              # 主界面
sudo autof list         # 服务列表
sudo autof gen frps     # 打印 frps.toml
sudo autof gen frpc     # 打印 frpc.toml
sudo autof install      # 安装并启动 frps
sudo autof net 4        # 网络偏好：仅 IPv4（6=仅 IPv6，both=均可）
sudo autof start        # 启动
sudo autof status       # 运行状态
sudo autof restart      # 重启
sudo autof stop         # 停止
sudo autof logs         # 日志
sudo autof uninstall    # 完全卸载
sudo autof self-update  # 更新脚本自身
```

## 常见问题

**端口能连上，但进不去服务？**
`nc -vz 公网IP 对外端口` 能连上只说明 `frps` 通了，问题出在 `frpc -> 服务`。本脚本固定 `localIP = 127.0.0.1`，所以 **`frpc` 必须和服务跑在同一台机器（或同一容器）**。若 `frpc` 在容器、服务在宿主机，需把 `localIP` 改成宿主机地址。

**`frpc.toml` 里 `remotePort` 和玩家端口是什么关系？**
相等。`remotePort = 对外端口 = 玩家连接端口`。

**报错 `json: unknown field "allowPorts"`？**
把 `frps.toml` 当成客户端配置用了。`frpc` 只能用 `autof gen frpc` 的输出。

**看日志**：`sudo autof logs`。

## 文件位置

| 路径 | 说明 |
| --- | --- |
| `/etc/autof/state.conf` | 控制端口、token、网络偏好 |
| `/etc/autof/services.conf` | 服务列表 |
| `/etc/autof/frps.toml` | 生成的 frps 配置 |
| `/etc/autof/clients/frpc.toml` | 生成的 frpc 配置 |
| `/usr/local/bin/frps` | frps 程序 |
| `/usr/local/bin/autof` | 本脚本（安装为命令后） |
| `/etc/systemd/system/frps.service` | 开机自启 |

非 root 运行时配置目录回退到 `~/.config/autof`。

## 环境要求

- Linux，bash 4+
- 依赖 `curl`、`ip`、`tar`（缺失时自动尝试安装）
- 安装 `frps` 需要 root

## 版权与免责声明

- **非官方项目**：Autofrp 为第三方辅助脚本，与 [fatedier/frp](https://github.com/fatedier/frp) 及其作者无任何隶属、赞助或背书关系。
- **frp 版权**：`frp` / `frps` / `frpc` 著作权归原作者所有，遵循 [Apache License 2.0](https://github.com/fatedier/frp/blob/master/LICENSE)。本脚本不包含、不修改、不重新分发 frp 的源代码或二进制，仅在运行时从官方 GitHub Release 下载。
- **本脚本许可**：[MIT License](LICENSE)。
- **免责**：脚本按「现状」提供，不提供任何担保。进行端口映射、内网穿透时，请自行确保符合当地法律法规、服务商条款及所运行服务的授权许可，后果自负。
