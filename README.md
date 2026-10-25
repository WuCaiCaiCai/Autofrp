# Autofrp v2.0

**远程 VPS** 与 **本地服务器** 一体化的 **内网穿透 (`frps` / `frpc`) + Caddy 快速反代** 管理工具。  
采用 **Go 单二进制 + 内嵌响应式 WebUI** 架构，零第三方运行时依赖，兼顾直观的网页控制台与命令行交互。

```text
                       ┌────────────────── 远程 VPS ──────────────────┐     ┌─────────── 本地服务器 / NAS ───────────┐
外部用户 (HTTPS/域名) ──► Caddy (自动证书/反代) ──► 127.0.0.1:穿透端口  │     │                                        │
                       │                                    ▲         │     │                                        │
游戏玩家 (TCP/UDP)    ──► 公网IP:对外端口       ──►        frps ◄─────┼─────┼── frpc ◄── 本机/局域网服务 (192.168.x.x)│
                       └──────────────────────────────────────────────┘     │    ▲                                   │
                                                                            │  Caddy (局域网反代 / 内网自签 / DNS证书)│
                                                                            └────────────────────────────────────────┘
```

---

## 核心特性

- **单一可执行文件 + 内置 WebUI**：Go 原生 `embed.FS` 打包前端，编译后仅一个 `autof` 二进制文件，开箱即用。
- **双端统一管理（两端独立运行 + 一键导入导出）**：
  - **远程 VPS 端**：一键安装并守护 `frps` + `Caddy`，管理穿透服务与域名反代，一键导出 **`autof://` 连接码** 或标准 **`frpc.toml`**。
  - **本地服务器端**：一键安装并守护 `frpc` + `Caddy`，直接粘贴 **`autof://` 连接码** 或 `frpc.toml` 即可完成对接；增删改本地/局域网穿透隧道自动执行 **`frpc reload` 零中断热重载**。
- **Caddy 快速反代深度整合**：
  - **穿透服务一键联动反代**：添加穿透规则时可直接填写域名，自动生成并同步对应的 Caddy 反代规则与 HTTPS 证书配置。
  - **通用快速反代**：支持 `域名`、`域名:非标端口`（适合无 80/443 的 NAT VPS）、`:纯端口` 转发到本机或局域网任意 `IP:Port`，原生支持 WebSocket 与跳过后端 HTTPS 自签证书校验（`tls_insecure_skip_verify`）。
  - **局域网 / 无域名反代**：支持纯 HTTP 端口反代与 `tls internal` 局域网自签名 HTTPS。
  - **DNS API 证书申请（可选）**：支持 Cloudflare、阿里云 AliDNS、腾讯云 DNSPod、DuckDNS，支持泛域名（`*.example.com`）及无 80/443 端口或纯内网环境签发真实受信任 SSL 证书。
- **灵活的端口白名单策略**：支持「不限制（默认推荐，本地新增穿透无需改 VPS）」、「指定端口段（适合 NAT VPS）」与「严格白名单」三种模式。
- **无损兼容旧版**：自动检测并迁移 v1 版本的 `/etc/autof/state.conf` 与 `services.conf`。

---

## 快速开始

### 方式一：一键脚本安装 / 编译运行

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh
sudo bash autof.sh
```

或在仓库源码目录下直接构建：

```bash
go build -trimpath -ldflags="-s -w" -o autof ./cmd/autof
sudo install -m 0755 autof /usr/local/bin/autof
```

### 方式二：启动 WebUI 网页控制台（推荐）

```bash
# 注册并启动 autof WebUI 后台服务（默认监听 0.0.0.0:7500）
sudo autof web-install
```

终端会直接输出访问地址、默认用户名（`admin`）与自动生成的随机密码：

```text
[+] Autofrp WebUI 后台服务已启动！
    浏览器访问: http://你的IP:7500
    登录用户名: admin
    登录密  码: xxxxxxxxxxxx
```

---

## 典型使用流程

### 场景 1：远程 VPS 部署 `frps` + 联动 Caddy 反代内网 Web / NAS

1. 在 **远程 VPS** 打开 WebUI（切换顶部模式为「远程 VPS 模式」）。
2. 在 **「1. 穿透服务端 (frps)」** 点击 **「+ 添加穿透服务」**：
   - 选择预设（如 `Web 网站`、`NAS 存储` 或 `Minecraft Java`）。
   - 若是 Web/NAS 服务，在下方 **「联动 Caddy 域名反代」** 填入你的域名（如 `nas.example.com` 或 NAT VPS 非标端口 `nas.example.com:8443`），保存后会自动同步生成 Caddy 反代规则。
3. 点击顶部状态卡中的 `frps` 与 `Caddy` 的 **「启动/安装」**。
4. 点击 **「📋 导出客户端连接码 / frpc.toml」**，复制生成的 `autof://...` 连接码。

### 场景 2：本地服务器一键导入并启动 `frpc` + 本地局域网反代

1. 在 **本地服务器 / NAS** 上安装 `autof` 并打开 WebUI（切换顶部模式为「本地服务器模式」）。
2. 在 **「2. 穿透客户端 (frpc)」** 点击 **「📥 一键导入连接码 / frpc.toml」**，粘贴刚才复制的 `autof://...` 连接码，点击「解析并应用配置」。
3. 点击顶部 `frpc` 状态卡的 **「启动/安装」**，即可连上远程 VPS！后续在本地新增或修改穿透隧道（支持填写局域网其他设备 IP 如 `192.168.1.50`），保存即自动热重载，不断开已有连接。
4. 如需在本地局域网统一反代各容器端口，进入 **「3. Caddy 快速反代」** 添加规则，可选择「纯 HTTP」、「局域网自签 HTTPS (`tls internal`)」或配置 Cloudflare DNS Token 申请真实域名证书。

---

## 命令行参考 (CLI)

```bash
sudo autof                          # 进入终端交互主菜单
sudo autof web-install              # 安装并启动 WebUI 后台服务 (systemd)
sudo autof web --listen :7500       # 前台运行 WebUI 服务
sudo autof status                   # 查看网络、组件状态及 WebUI 登录账号密码
sudo autof list                     # 查看所有穿透规则与 Caddy 反代列表
sudo autof export                   # 导出客户端 autof:// 连接码与 frpc.toml
sudo autof import "autof://..."     # 在本地服务器一键导入连接码或 frpc.toml 文件
sudo autof gen frps                 # 打印 frps.toml
sudo autof gen frpc                 # 打印导出用 frpc.toml
sudo autof gen frpc-client          # 打印本机客户端 frpc.toml
sudo autof gen caddy                # 打印生成的 Caddyfile
sudo autof gen code                 # 打印 autof:// 连接码
sudo autof install all              # 一键安装 frps + frpc + caddy + web 服务
sudo autof start|stop|restart|reload|logs [frps|frpc|caddy|autof]
sudo autof net 4|6|both             # 设置网络偏好（仅 IPv4 / 仅 IPv6 / 双栈）
sudo autof pwd [新密码]             # 查看或重置 WebUI 登录密码
sudo autof uninstall                # 完全卸载（停止服务并清理配置与程序）
```

---

## 文件与目录结构

| 路径 | 说明 |
| --- | --- |
| `/etc/autof/config.json` | 统一结构化配置文件（含网络、WebUI、frps、frpc、Caddy 规则） |
| `/etc/autof/frps.toml` | 自动生成的 `frps` 服务端配置 |
| `/etc/autof/frpc.toml` | 自动生成的本地 `frpc` 客户端运行配置 |
| `/etc/autof/clients/frpc.toml` | 自动生成的供导出客户端配置 |
| `/etc/autof/Caddyfile` | 自动生成并经语法校验的 `Caddyfile` |
| `/usr/local/bin/autof` | Autofrp 主程序（含内嵌 WebUI） |
| `/usr/local/bin/frps` & `frpc` | 自动下载安装的 frp 服务端与客户端 |
| `/usr/local/bin/caddy` | 自动下载安装的 Caddy 程序 |

> 非 root 用户运行时配置目录自动回退至 `~/.config/autof`（或通过环境变量 `AUTOF_STATE_DIR` 指定）。

---

## 版权与免责声明

- **非官方项目**：Autofrp 为第三方辅助管理工具，与 [fatedier/frp](https://github.com/fatedier/frp) 及 [caddyserver/caddy](https://github.com/caddyserver/caddy) 无隶属关系。
- **组件版权**：`frp` 遵循 Apache-2.0 许可，`Caddy` 遵循 Apache-2.0 许可，二进制文件均在运行时按需从官方源下载。
- **本项目许可**：[MIT License](LICENSE)。
