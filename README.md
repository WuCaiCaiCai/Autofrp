# Autofrp

在 NAT 型 VPS 上部署 frp 服务端（frps）的交互式脚本，用于把内网服务（Minecraft、Emby 等）暴露到公网。

脚本只跑在**服务端（VPS）**上，不用装在内网机器。

## 两个概念，互不干扰

1. **frps 控制**：`frpc` 连接 `frps` 的入口，只和穿透本身有关，与具体服务无关。
   - 内部端口 `bindPort`：VPS 上监听（默认 `7000`，可改）。
   - 对外端口：服务商网页端映射出来的端口（如 `30085`，可改），`frpc` 用它连接。
2. **服务**：一个要被访问的内网服务（如 Minecraft），涉及三个端口，**全部可自定义**：
   - **本机端口**：服务实际监听的端口（如 `25565`）。
   - **VPS 内部端口**：`frps` 监听的端口，也就是 `frpc` 的 `remotePort`（如 `25565`）。
   - **对外端口**：玩家连接用的公网端口（如 `30008`）。

> 服务商网页端的映射规则是 **对外端口 → 内部端口**。例如映射 `30008 → 30008`，则对外=内部=30008；映射 `30008 → 25565`，则对外 `30008`、内部 `25565`。脚本里按实际填即可，两个端口可以不同。

## 部署

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh
sudo bash autof.sh
```

首次运行会先让你**设置 frps 控制**，然后进主界面。结束时提示安装为 `autof` 命令，之后直接：

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
  控制   内部 7000  对外 30085
════════════════════════════════════════════════════════════

  1) 设置 frps 控制
  2) 服务管理
  3) 预览/生成配置
  4) 服务端控制
  5) 接入检查
  6) 卸载 Autofrp
  0) 退出
```

- **设置 frps 控制**：填 `bindPort`（内部）与控制端口对外映射、`auth token`。
- **服务管理**：`Minecraft Java` / `Minecraft Bedrock` / `Emby` / `其他自定义`（TCP/UDP/STCP/XTCP）；可查看详情、编辑、删除。每个服务依次填**本机端口、对外端口、VPS 内部端口**，全部可自定义。
- **预览/生成配置**：预览或保存 `frps.toml`、`frpc.toml`。
- **服务端控制**：安装/更新并启动、停止、重启、状态、日志、卸载。
- **接入检查**：显示 frps 监听端口、每个服务的玩家连接地址与服务商应设置的映射。
- **卸载 Autofrp**：删除 frps 服务/程序、配置目录与 `autof` 命令。

## 完整示例：Minecraft 内网穿透

场景：VPS 是 NAT 型、跑 frps；内网机器跑 Minecraft（监听 `25565`）。演示 **对外 `30008` 映射到内部 `25565`**。

### 1. 服务商网页端设置两条映射

| 外部端口 | 内部端口 | 用途 |
| --- | --- | --- |
| `30085` | `7000` | frps 控制端口（frpc 连接） |
| `30008` | `25565` | Minecraft 数据端口 |

### 2. VPS 上运行脚本

```bash
sudo autof
```

- `1) 设置 frps 控制`：内部端口 `7000`，控制端口对外映射 `30085`，token 回车自动生成。
- `2) 服务管理` → `1) Minecraft Java`：本机端口 `25565`，对外端口 `30008`，VPS 内部端口 `25565`。
- `3) 预览/生成配置` → `2) 预览 frpc.toml`：复制结果。
- `4) 服务端控制` → `1) 安装/更新并启动`。

### 3. 内网机器上运行 frpc

1. 从 [frp Releases](https://github.com/fatedier/frp/releases) 下载对应平台压缩包，解压得到 `frpc`。
2. 把上一步复制的 `frpc.toml` 放同目录，内容大致如下：

   ```toml
   # frpc 客户端配置（在内网机器上运行 frpc）
   serverAddr = "108.165.122.146"
   serverPort = 30085
   auth.method = "token"
   auth.token = "向导生成的token"
   transport.tls.enable = true

   [[proxies]]
   name = "mc-java"
   type = "tcp"
   localIP = "127.0.0.1"
   localPort = 25565
   remotePort = 25565
   ```

3. 启动：`./frpc -c frpc.toml`（Windows：`frpc.exe -c frpc.toml`），看到 `start proxy success` 即成功。

### 4. 验证

玩家连接 **`VPS公网IP:30008`**。

## 端口对照

| 位置 | 值 | 配置位置 |
| --- | --- | --- |
| Minecraft 本地监听 | `25565` | 内网机器的 Minecraft |
| 服务 本机端口 | `25565` | 服务管理里填写 |
| 服务 VPS 内部端口 = frpc `remotePort` | `25565` | 服务管理里填写 |
| 服务 对外端口（玩家连接） | `30008` | 服务管理里填写 |
| 服务商映射 | `30008 → 25565` | 服务商控制台 |
| frps 控制 内部端口 `bindPort` | `7000` | 设置 frps 控制 |
| frpc `serverPort` | `30085` | 控制端口对外映射 |
| 玩家连接 | `公网IP:30008` | 提供给玩家 |

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
sudo autof uninstall    # 完全卸载(服务/程序/配置/autof 命令)
sudo autof uninstall-keep  # 仅移除 frps，保留配置
sudo autof self-update  # 更新脚本自身
```

### 卸载

```bash
sudo autof uninstall       # 完全卸载：停服务、删 frps、删配置目录、删 autof 命令
sudo autof uninstall-keep  # 只删 frps 程序与服务，保留 /etc/autof 配置
```

也可在主界面 `4) 服务端控制` 里选择 `6) 仅移除 frps` 或 `7) 完全卸载`。

## 故障排查

1. **连不上**：确认服务商网页端映射规则是「对外端口 → 内部端口」，且与脚本里填的**对外端口、VPS 内部端口**一致；控制端口同理（对外 → `bindPort`）。
2. **token 不一致**：服务端与客户端配置里的 `auth.token` 必须相同。
3. **报错 `json: unknown field "allowPorts"`**：把 `frps.toml` 当成客户端配置用了。`frpc` 只能用 `autof gen frpc` 输出的配置（只含 `serverAddr`/`serverPort`/`auth`/`[[proxies]]`）。
4. **端口能连但进不去服务（如 MC）**：`nc -vz 公网IP 对外端口` 能连上，说明隧道通了；问题在 `frpc → 服务`。多半是 `localIP` 不对：
   - frpc 与服务在同一台机器 → `127.0.0.1`。
   - **frpc 跑在 Docker 容器里**（如 MSLX）→ `127.0.0.1` 指容器自己，要填宿主机内网 IP 或 `host.docker.internal`，或让容器用 `host` 网络。
   - 服务在另一台机器 → 填那台机器的内网 IP。
5. **看日志**：`sudo autof logs`。

## 文件位置

| 路径 | 说明 |
| --- | --- |
| `/etc/autof/state.conf` | 控制端口、token 等 |
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
