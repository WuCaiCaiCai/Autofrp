# Autofrp v2.0

**远程 VPS** 与 **本地服务器** 一体化的 **内网穿透 (`frps` / `frpc`) + Caddy 快速反代** 管理工具。  
采用 **Go 单二进制 + 内嵌响应式 WebUI** 架构，零运行时依赖，全面适配 **Debian / Ubuntu / Deepin (深度) / UOS / Fedora / RHEL / Rocky / Arch Linux / Alpine** 等主流 Linux 发行版。

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

## 一键拉取与快速部署（远程 VPS / 本地服务器通用）

### 1. 一键安装并直接启动 WebUI 网页控制台（推荐）

**国际 / 通用线路：**

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh && sudo bash autof.sh web-install
```

**国内服务器镜像加速：**

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh && sudo bash autof.sh web-install
```

执行后会自动完成依赖自检、安装 `/usr/local/bin/autof` 并注册启动 `autof` WebUI 后台服务，终端直接输出访问地址与登录凭据：

```text
[+] Autofrp WebUI 后台服务已启动！
    浏览器访问: http://你的服务器IP:7500
    登录用户名: admin
    登录密  码: xxxxxxxxxxxx
```

### 2. 一键拉取并进入终端交互菜单

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh && sudo bash autof.sh
```

安装完成后，后续随时在终端直接执行：

```bash
sudo autof
```

### 3. 各 Linux 发行版原生安装包支持

GitHub Releases 自动构建全架构静态二进制（`amd64` / `arm64` / `armv7` / `386` / `riscv64` / `loong64`）及各发行版安装包：

- **Debian / Ubuntu / Deepin (深度) / UOS**：
  ```bash
  curl -fsSLO https://github.com/WuCaiCaiCai/Autofrp/releases/latest/download/autof_2.0.0_linux_amd64.deb
  sudo dpkg -i autof_2.0.0_linux_amd64.deb
  ```
- **Fedora / RHEL / CentOS / Rocky / AlmaLinux**：
  ```bash
  sudo dnf install -y https://github.com/WuCaiCaiCai/Autofrp/releases/latest/download/autof-2.0.0-1.x86_64.rpm
  ```
- **Arch Linux / Manjaro**：
  ```bash
  curl -fsSLO https://github.com/WuCaiCaiCai/Autofrp/releases/latest/download/PKGBUILD && makepkg -si
  ```

---

## 核心特性

1. **单一可执行文件 + 内置响应式 WebUI**
   - Go 原生 `embed.FS` 内嵌前端，编译产物仅一个零依赖二进制文件 `autof`。
   - 顶部支持一键切换 **「全部模块」 / 「远程 VPS 模式 (`frps + Caddy`)」 / 「本地服务器模式 (`frpc + Caddy`)」**。
2. **双端独立管理 + 连接码一键导入导出**
   - **远程 VPS 端**：可视化管理 `frps` 控制端口、Token、端口白名单策略（支持「不限制」「端口段」「严格白名单」），一键导出 **`autof://` 连接码** 或标准 **`frpc.toml`**。
   - **本地服务器端**：粘贴 **`autof://` 连接码** 或 `frpc.toml` 即可一键完成客户端配置；修改本地/局域网穿透规则自动触发 **`frpc reload` 零中断热重载**。
3. **Caddy 快速反代全场景整合**
   - **穿透服务一键联动域名反代**：添加穿透规则时直接填写域名，自动生成并同步 Caddy 反代规则与 HTTPS 证书。
   - **通用快速反代**：支持 `域名`、`域名:非标端口`（适合无 80/443 的 NAT VPS）、`:纯端口` 转发至本机或局域网任意 `IP:Port`，原生支持 WebSocket 与跳过后端 HTTPS 自签证书校验（`tls_insecure_skip_verify`）。
   - **局域网 / 无域名反代**：支持纯 HTTP 端口反代与 `tls internal` 局域网自签名 HTTPS。
   - **DNS API 证书申请（可选）**：支持 Cloudflare、阿里云 AliDNS、腾讯云 DNSPod、DuckDNS，支持泛域名（`*.example.com`）及无 80/443 端口或纯内网签发真实 SSL 证书。
4. **原服务器进程自动探测、在线编辑与一键吞并迁移（支持 NPM / Nginx / 原版 Caddy / 原版 frp）**
   - 自动扫描宿主机与 Docker 容器中正在运行的 **Nginx Proxy Manager (NPM)**、**Nginx / OpenResty**、**原版 Caddy**、**原版 `frps`/`frpc`**、**Lucky / Traefik** 等进程、监听端口及挂载配置文件。
   - **在线查看与编辑原配置**：无需 SSH 翻找目录，直接在 WebUI 在线编辑原配置文件并重载原服务（自动生成 `.autof.bak` 备份）。
   - **一键吞并迁移 + 清理老服务（带确认弹窗）**：
     - 自动解析原版或 Docker 版 NPM / Nginx / Caddy 的所有反代域名、后端地址以及 **Let's Encrypt / 自定义 SSL 证书**。
     - 在确认窗口展示完整的待迁移站点、证书清单与清理步骤；确认后自动将证书复制到 `/etc/autof/certs/<域名>/`、将原配置备份到 `/etc/autof/backup/`、停止并禁用老服务/容器释放 `80/443/7000` 端口，并立即由 Autofrp 无缝接管全部流量。

---

## 典型使用流程

### 场景 1：已有 NPM / Nginx / 原版 Caddy 的服务器一键吞并接管

1. 在服务器上执行一键命令启动 WebUI 并登录。
2. 界面顶部会自动弹出黄色提示条 **「🔎 检测到服务器存在原有反代或穿透服务」**，点击进入 **「4. 原服务探测与吞并」**。
3. 如仅需微调老配置，点击 **「✏️ 查看/编辑配置」** 即可在线修改并重载；如需统一由 Autofrp 接管，点击 **「🚀 一键吞并迁移」**。
4. 在弹出的 **「吞并迁移与清理老服务确认窗口」** 中核对识别出的域名、上游地址与 SSL 证书列表，点击 **「✅ 确认吞并并接管服务」** 即可完成零等待 HTTPS 迁移与老服务清理。

### 场景 2：远程 VPS 部署 `frps` + 本地服务器 `frpc` 穿透联动反代

1. **远程 VPS 端**：在 WebUI 的 **「1. 穿透服务端 (frps)」** 添加穿透服务（内置 Minecraft Java/Bedrock、Web、NAS、SSH、RDP 预设），如需绑定域名直接填写「联动 Caddy 域名反代」，启动 `frps` 与 `Caddy`，点击 **「📋 导出客户端连接码」**。
2. **本地服务器端**：在本地服务器 WebUI 的 **「2. 穿透客户端 (frpc)」** 点击 **「📥 一键导入连接码 / frpc.toml」**，粘贴连接码并启动 `frpc` 即可连通。

---

## 命令行参考 (CLI)

```bash
sudo autof                          # 进入终端交互主菜单
sudo autof web-install              # 安装并启动 WebUI 后台服务 (systemd / OpenRC)
sudo autof web --listen :7500       # 前台运行 WebUI 服务
sudo autof status                   # 查看系统发行版、网络、组件状态及 WebUI 登录账号密码
sudo autof detect                   # 扫描服务器原有进程与配置 (NPM / Nginx / 原版 Caddy / frp)
sudo autof takeover <服务ID>        # 交互式预览确认并吞并迁移老服务配置与证书
sudo autof list                     # 查看所有穿透规则与 Caddy 反代列表
sudo autof export                   # 导出客户端 autof:// 连接码与 frpc.toml
sudo autof import "autof://..."     # 在本地服务器一键导入连接码或 frpc.toml 文件
sudo autof gen frps|frpc|caddy|code # 打印生成的配置文件或连接码
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
| `/etc/autof/certs/<域名>/` | 从 NPM / Nginx / 原服务迁移过来的 SSL 证书 (`fullchain.pem` & `privkey.pem`) |
| `/etc/autof/backup/` | 吞并老服务前自动归档的原配置文件备份 |
| `/usr/local/bin/autof` | Autofrp 主程序（含内嵌 WebUI） |

---

## 版权与免责声明

- **非官方项目**：Autofrp 为第三方辅助管理工具，与 [fatedier/frp](https://github.com/fatedier/frp) 及 [caddyserver/caddy](https://github.com/caddyserver/caddy) 无隶属关系。
- **组件版权**：`frp` 与 `Caddy` 均遵循 Apache-2.0 许可，二进制文件在运行时按需从官方源下载。
- **本项目许可**：[MIT License](LICENSE)。
