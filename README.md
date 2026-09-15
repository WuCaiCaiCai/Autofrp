# Autofrp

在 NAT 型 VPS 上部署 frp 服务端（frps）的交互式脚本，用于将内网服务（Minecraft、Emby 等）暴露至公网。

脚本仅运行于服务端（VPS），无需安装于内网主机。

## 工作原理

1. 内网主机运行 frpc，主动连接 VPS 上的 frps，建立长连接隧道。
2. 外部用户访问 VPS 的公网地址。
3. frps 将流量经隧道转发至 frpc，再由 frpc 转发至内网服务。

NAT 型 VPS 不具备公网入站能力，须由服务商控制台提供端口映射。脚本不修改服务商面板，仅登记映射关系并据此计算对外地址。

配置分为两部分：

- **服务端（VPS）**：脚本生成 `frps.toml` 并启动服务。
- **客户端（内网主机）**：脚本输出 `frpc.toml`，复制至内网主机运行 frpc。

## 部署

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh
sudo bash autof.sh
```

首次运行进入配置向导，结束时提示安装为 `autof` 命令。安装后直接执行：

```bash
sudo autof
```

### 配置向导流程

1. **环境探测**：识别公网 IPv4 / IPv6、NAT 类型及对外地址。
2. **对外方式**：仅 IPv4、仅 IPv6 或两者（依据探测结果给出选项）。
3. **端口映射**：设置控制端口 `bindPort`，并登记服务商分配的外部端口。
4. **认证**：设置 `auth.token`（可自动生成）与 TLS。
5. **服务端信息**：昵称、Dashboard、HTTP 建站。
6. **服务配置**：添加 Minecraft、Emby 或自定义服务。
7. **确认与生成**：预览并保存 `frps.toml` 与 `frpc.toml`。
8. **安装启动**：下载 frps、注册 systemd、执行自检。
9. **结果输出**：服务列表、访问地址、运行状态。

## 前置操作：配置端口映射

端口映射须在服务商网页控制台完成，脚本不代为设置。

1. 登录服务商控制台，进入「端口转发 / 端口映射 / NAT」页面。
2. 新建映射：**外部端口**（公网）→ **内部端口**（VPS 监听）。
3. 将「外部 → 内部」登记至向导第 3 步。

示例：外部 `20002` → 内部 `25565`，则外部用户访问 `公网IP:20002`。

## 完整示例：Minecraft 内网穿透

场景：VPS 为 NAT 型并运行 frps；内网主机运行 Minecraft 服务端（监听 `25565`）。

### 步骤 1：配置端口映射（服务商控制台）

| 外部端口 | 内部端口 | 用途 |
| --- | --- | --- |
| `20000` | `7000` | frps 控制端口，frpc 连接使用 |
| `20002` | `25565` | Minecraft 数据端口 |

### 步骤 2：部署服务端（VPS）

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh
sudo bash autof.sh
```

向导填写：

| 步骤 | 填写内容 |
| --- | --- |
| 1 对外方式 | 仅 IPv4 |
| 2 控制端口 | `bindPort = 7000`；外部端口 `20000` |
| 3 认证 | 回车自动生成 token；TLS 选 `y` |
| 4 服务端信息 | 填写昵称；Dashboard 可选 |
| 5 服务配置 | 选 Minecraft Java；远端端口 `25565`；外部端口 `20002`；返回 |
| 6 确认 | `y` |
| 7 生成 | 复制输出的 `frpc.toml` |
| 8 启动 | `y` |

### 步骤 3：部署客户端（内网主机）

1. 从 [frp Releases](https://github.com/fatedier/frp/releases) 下载对应平台压缩包，解压得到 `frpc`（Windows 为 `frpc.exe`）。
2. 将 `frpc.toml` 置于 `frpc` 同目录，内容如下：

   ```toml
   serverAddr = "VPS公网IP"
   serverPort = 20000
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

3. 启动：

   ```bash
   # Linux / macOS
   ./frpc -c frpc.toml
   # Windows
   frpc.exe -c frpc.toml
   ```

   输出 `start proxy success` 表示隧道建立成功。

4. 常驻运行：Linux 使用 `nohup` 或 systemd；Windows 使用任务计划程序。

若 Minecraft 与 frpc 不在同一主机，将 `localIP` 改为 Minecraft 主机的内网地址。

### 步骤 4：验证

1. VPS 执行 `sudo autof list`，确认 `mc-java` 状态为「已登记」。
2. VPS 执行 `sudo autof status`，确认 frps 运行且控制端口监听。
3. 外部用户连接 `公网IP:20002`。

### 端口对照

| 位置 | 值 | 配置位置 |
| --- | --- | --- |
| Minecraft 本地监听 | `25565` | 内网主机的 Minecraft 服务端 |
| frpc `localPort` | `25565` | 内网主机的 `frpc.toml` |
| frpc `remotePort` | `25565` | 内网主机的 `frpc.toml`（frps 监听） |
| 端口映射 | `20002 → 25565` | 服务商控制台 |
| frpc `serverPort` | `20000` | 内网主机的 `frpc.toml`（bindPort 的外部端口） |
| 外部访问 | `公网IP:20002` | 提供给用户 |

## 其他服务

- **Emby**：向导第 5 步启用 HTTP 建站，第 6 步选择 Emby 并填写域名；将域名解析至 VPS 公网 IP。
- **自定义服务**：向导第 6 步支持 TCP / UDP / HTTP / HTTPS / STCP / XTCP / TCPMUX。

## 命令

```bash
sudo autof              # 配置向导（首次使用）
sudo autof menu         # 管理菜单
sudo autof list         # 服务列表、访问地址与状态
sudo autof status       # 服务端运行状态
sudo autof detect       # 仅探测网络环境
sudo autof install      # 安装并启动 frps
sudo autof logs         # 查看服务端日志
sudo autof restart      # 重启服务端
sudo autof uninstall    # 卸载服务端
sudo autof self-update  # 更新脚本自身
```

## 故障排查

1. **端口映射**：确认服务商控制台已放行外部端口，且脚本中「外部 → 内部」登记正确。
2. **认证**：确认服务端与客户端配置中的 `auth.token` 一致。
3. **服务端状态**：执行 `sudo autof status` 与 `sudo autof logs` 检查报错。

其他：域名建站须将域名解析至 VPS 公网 IP；防火墙与安全组须放行对应端口。

## 常见问题

### 控制端口 `bindPort` 是否必需？为何至少需要两条映射？

必需，且控制端口最为关键：

- **控制端口 `bindPort`**：frpc 从内网主动连接 frps 的入口。认证、心跳及连接建立均经由该端口。缺失则 frpc 无法连接，隧道无法建立。
- **数据端口 `remotePort`**：frps 监听该端口，外部用户经映射后的外部端口接入。

两者在 frps 上分别监听，端口不可重复，因此不可合并，也不可省略控制端口。NAT 型 VPS 对应两条映射：

| 外部端口 | 内部端口 | 用途 |
| --- | --- | --- |
| `20000` | `7000`（bindPort） | frpc 连接 frps |
| `20002` | `25565`（remotePort） | 外部用户访问 Minecraft |

若 VPS 具备公网 IP，则无需端口映射，但仍须在防火墙放行 `bindPort`。

### 内部端口与外部端口是否必须相同？

不必。仅需「控制台映射的内部端口」等于「frpc 的 `remotePort`」，外部端口可任意指定。详见上文端口对照表。

## 文件位置

| 路径 | 说明 |
| --- | --- |
| `/etc/autof/` | 全部配置与生成的 toml |
| `/etc/autof/clients/` | 客户端配置 `frpc-v4.toml` / `frpc-v6.toml` |
| `/usr/local/bin/frps` | frps 程序 |
| `/etc/systemd/system/frps.service` | 开机自启服务 |

非 root 运行时，配置目录回退至 `~/.config/autof`。

## 环境要求

- Linux，bash 4+
- 依赖 `curl`、`ip`、`ss`、`tar`（缺失时自动尝试安装）
- 安装 frps 需要 root 权限

## 版权与免责声明

- **非官方项目**：Autofrp 为第三方辅助脚本，与 [fatedier/frp](https://github.com/fatedier/frp) 及其作者无任何隶属、赞助或背书关系。
- **frp 版权**：`frp` / `frps` / `frpc` 著作权归原作者所有，遵循 [Apache License 2.0](https://github.com/fatedier/frp/blob/master/LICENSE)。本脚本不包含、不修改、不重新分发 frp 的源代码或二进制，仅在运行时从官方 GitHub Release 下载。
- **本脚本许可**：[MIT License](LICENSE)。
- **免责**：脚本按「现状」提供，不提供任何担保。进行端口映射、内网穿透时，请自行确保符合当地法律法规、服务商条款及所运行服务（如 Minecraft、Emby）的授权许可，后果自负。
