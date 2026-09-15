# Autofrp

在 NAT 型 VPS 上部署 frp 服务端（frps）的交互式脚本，用于把内网服务（Minecraft、Emby 等）暴露到公网。

脚本只跑在**服务端（VPS）**上，不用装在内网机器。全中文引导，照着填就行。

## 端口规则（超简单）

**所有端口都内外一致**，服务商网页端一律映射 `端口 -> 同端口`。

| 项 | 数量 | 说明 |
| --- | --- | --- |
| 控制端口 | 1 个 | `frpc` 连接 `frps` 用，和具体服务无关 |
| 服务 | 每个 2 个 | **本机端口**（服务实际监听）+ **对外端口**（玩家连接用） |

玩家连接地址永远是 `公网IP:对外端口`。`frpc` 配置里的 `remotePort` 就等于对外端口。

## 部署

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh
sudo bash autof.sh
```

首次运行会引导你设置**控制端口**，然后进主界面。结束时提示安装为 `autof` 命令，之后直接：

```bash
sudo autof
```

## 主界面

```
════════════════════════════════════════════════════════════
  Autofrp  ·  frps 服务端
════════════════════════════════════════════════════════════
  IPv4   NAT IPv4  108.165.122.146
  IPv6   NAT IPv6  2602:f9f3:3000::185
  frps   运行中 / 已停止
  控制端口 30085
════════════════════════════════════════════════════════════

  1) 添加服务
  2) 查看服务
  3) 服务端控制
  4) 预览配置
  5) 卸载
  0) 退出
```

- **添加服务**：`Minecraft Java` / `Minecraft Bedrock` / `Emby` / `自定义 TCP` / `自定义 UDP`。填「本机端口」和「对外端口」即可，加完直接告诉你玩家连接地址。
- **查看服务**：列表 → 输入序号看详情与该服务完整 `frpc.toml`，可编辑/删除。
- **服务端控制**：安装/更新并启动、停止、重启、状态、日志、修改控制端口。
- **预览配置**：`frps.toml` / `frpc.toml` / 保存到文件。
- **卸载**：完全卸载（服务 + 程序 + 配置 + `autof` 命令）。

## 完整示例：Minecraft 内网穿透

场景：VPS 是 NAT 型、跑 frps；内网机器跑 Minecraft（监听 `25565`），想让玩家连。

### 1. 服务商网页端设置两条映射（都是 `X -> X`）

| 外部端口 | 内部端口 | 用途 |
| --- | --- | --- |
| `30085` | `30085` | 控制端口 |
| `30008` | `30008` | Minecraft |

### 2. VPS 上运行脚本

```bash
sudo autof
```

- 首次引导：控制端口填 `30085`。
- `1) 添加服务` → `1) Minecraft Java`：本机端口 `25565`，对外端口 `30008`。
- `4) 预览配置` → `2) 预览 frpc.toml`：复制结果。
- `3) 服务端控制` → `1) 安装/更新并启动 frps`。

### 3. 内网机器上运行 frpc

1. 从 [frp Releases](https://github.com/fatedier/frp/releases) 下载对应平台压缩包，解压得到 `frpc`。
2. 把复制的 `frpc.toml` 放同目录，内容大致如下：

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

## 命令

```bash
sudo autof              # 主界面
sudo autof list         # 服务列表
sudo autof gen frps     # 打印 frps.toml
sudo autof gen frpc     # 打印 frpc.toml
sudo autof install      # 安装并启动 frps
sudo autof status       # 运行状态
sudo autof restart      # 重启
sudo autof stop         # 停止
sudo autof logs         # 日志
sudo autof uninstall    # 完全卸载
sudo autof self-update  # 更新脚本自身
```

## 常见问题

**为什么 `frpc.toml` 里 `remotePort` 不等于玩家端口？**
它俩本来就相等。`remotePort = 对外端口 = 玩家连接端口`。

**端口能连但进不去服务？**
`nc -vz 公网IP 对外端口` 能连上只说明 frps 通了；问题在 `frpc -> 服务`。本脚本固定 `localIP = 127.0.0.1`，所以 **frpc 必须和服务跑在同一台机器（或同一容器）**。若 frpc 在容器里、服务在宿主机，需要把 `localIP` 改成宿主机地址。

**报错 `json: unknown field "allowPorts"`？**
把 `frps.toml` 当成客户端配置用了。`frpc` 只能用 `autof gen frpc` 的输出。

**看日志**：`sudo autof logs`。

## 文件位置

| 路径 | 说明 |
| --- | --- |
| `/etc/autof/state.conf` | 控制端口、token |
| `/etc/autof/services.conf` | 服务列表 |
| `/etc/autof/frps.toml` | 生成的 frps 配置 |
| `/etc/autof/clients/frpc.toml` | 生成的 frpc 配置 |
| `/usr/local/bin/frps` | frps 程序 |
| `/etc/systemd/system/frps.service` | 开机自启 |

非 root 运行时配置目录回退到 `~/.config/autof`。

## 环境要求

- Linux，bash 4+
- 依赖 `curl`、`ip`、`tar`（缺失时自动尝试安装）
- 安装 frps 需要 root

## 版权与免责声明

- **非官方项目**：Autofrp 为第三方辅助脚本，与 [fatedier/frp](https://github.com/fatedier/frp) 及其作者无任何隶属、赞助或背书关系。
- **frp 版权**：`frp` / `frps` / `frpc` 著作权归原作者所有，遵循 [Apache License 2.0](https://github.com/fatedier/frp/blob/master/LICENSE)。本脚本不包含、不修改、不重新分发 frp 的源代码或二进制，仅在运行时从官方 GitHub Release 下载。
- **本脚本许可**：[MIT License](LICENSE)。
- **免责**：脚本按「现状」提供，不提供任何担保。进行端口映射、内网穿透时，请自行确保符合当地法律法规、服务商条款及所运行服务的授权许可，后果自负。
