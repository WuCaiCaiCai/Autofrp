<div align="center">

# Autofrp

**内网穿透 + Caddy 快速反代，一个二进制搞定远程 VPS 与本地服务器**

[![Release](https://img.shields.io/github/v/release/WuCaiCaiCai/Autofrp?style=flat-square&color=38bdf8&label=release)](https://github.com/WuCaiCaiCai/Autofrp/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/WuCaiCaiCai/Autofrp/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/WuCaiCaiCai/Autofrp/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/WuCaiCaiCai/Autofrp?style=flat-square&color=22c55e)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/WuCaiCaiCai/Autofrp?style=flat-square&color=00add8&label=go)](go.mod)
[![Platform](https://img.shields.io/badge/platform-linux%20amd64%20%7C%20arm64-38bdf8?style=flat-square)](#安装方式)
[![Downloads](https://img.shields.io/github/downloads/WuCaiCaiCai/Autofrp/total?style=flat-square&color=a855f7)](https://github.com/WuCaiCaiCai/Autofrp/releases)

[快速开始](#快速开始) · [核心特性](#核心特性) · [使用场景](#典型使用场景) · [CLI 命令](#cli-命令) · [常见问题](#常见问题)

</div>

---

Autofrp 把 **frp 内网穿透（`frps` / `frpc`）** 与 **Caddy 反向代理** 合并成一套带 WebUI 的管理工具：

- 在**远程 VPS** 上管 `frps` + `Caddy`，把内网服务以域名 + 自动 HTTPS 暴露出去；
- 在**本地服务器 / NAS** 上管 `frpc` + `Caddy`，一条连接码即可完成对接；
- 已经跑着 **Nginx Proxy Manager / Nginx / 原版 Caddy / 原版 frp**？可在线编辑原配置，或**一键吞并迁移**（连同 SSL 证书与老服务清理）。

编译产物是**单个静态二进制**，前端资源通过 `embed.FS` 内嵌，无运行时依赖。

---

## 快速开始

### 方式一：一键安装 WebUI 控制台（推荐）

**国际线路**

```bash
curl -fsSL https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh && sudo bash autof.sh web-install
```

**国内加速**

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh -o autof.sh && sudo bash autof.sh web-install
```

脚本会自动识别发行版、安装基础依赖、下载匹配架构的静态二进制到 `/usr/local/bin/autof`，并注册 `autof` 后台服务。完成后终端直接输出访问信息：

```text
[+] autof v1.1.6 已安装: /usr/local/bin/autof
[+] Autofrp WebUI 后台服务已启动！
    访问地址（按需选择）:
      本机     http://127.0.0.1:7500
      局域网   http://192.168.1.10:7500
      公网     http://你的公网IP:7500
    登录用户名: admin
    登录密  码: xxxxxxxxxxxx
```

> 密码也可随时用 `sudo autof pwd` 查看或重置；只想要终端界面则执行 `sudo bash autof.sh`（不带 `web-install`）。

### 方式二：直接下载静态二进制

> Release **只发布静态二进制**（`amd64` / `arm64`）与 `checksums.txt`，不提供 deb/rpm/apk 打包；所有发行版通用。

```bash
# amd64（arm64 机器把文件名换成 autof_linux_arm64）
sudo curl -fsSL -o /usr/local/bin/autof \
  https://github.com/WuCaiCaiCai/Autofrp/releases/latest/download/autof_linux_amd64
sudo chmod +x /usr/local/bin/autof
sudo autof web-install
```

如需校验完整性：

```bash
curl -fsSL https://github.com/WuCaiCaiCai/Autofrp/releases/latest/download/checksums.txt
sha256sum /usr/local/bin/autof
```

<sub>`frps` / `frpc` / `caddy` 不随包分发，在首次启动时按需自动下载。</sub>

### 方式三：源码编译

```bash
git clone https://github.com/WuCaiCaiCai/Autofrp.git
cd Autofrp
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o autof ./cmd/autof
sudo install -m 0755 autof /usr/local/bin/autof
```

---

## 核心特性

<table>
<tr><td width="50%" valign="top">

**🖥 单二进制 + 内嵌 WebUI**
Go `embed.FS` 打包前端，零依赖；侧边栏式控制台，支持 `#/setup`、`#/caddy` 等深链接与移动端自适应。

**🚀 首次引导 + 旧版继承**
三步初始化向导（角色 / 网络 / 组件）完成后可选一键启动；v1.x 的 `state.conf` / `services.conf` 自动迁移并跳过向导。

**🔗 双端独立 + 连接码**
VPS 端导出 `autof://` 连接码或 `frpc.toml`，本地端粘贴即完成对接。

**♻️ 零中断热重载**
改本地隧道走 `frpc reload`；改反代走 `caddy validate` + `caddy reload`，不断开已有连接。

</td><td width="50%" valign="top">

**🔐 Caddy 全场景反代**
自动公网 HTTPS、非标端口、局域网自签（`tls internal`）、Cloudflare / 阿里云 / DNSPod / DuckDNS 的 DNS-01 泛域名证书。

**🧬 吞并老服务（Compose 安全）**
自动识别 NPM / Nginx / 原版 Caddy / frp，提取站点与 SSL 证书迁移到 `/etc/autof/`；compose 管理的容器只 stop，不删容器、不改 compose。

**📜 证书管理**
内置 acme.sh（DNS-01），可申请泛域名 `*.example.com`、查看到期天数、手动/自动续期，并导出 PEM 给 NPM 等使用。

**💾 备份与迁移**
一键打包配置、证书、令牌与生成文件；换机后导入恢复，自动归档旧配置，不改 systemd / Docker。

**🧩 多发行版适配**
Debian / Deepin / Fedora / Arch / Alpine 等；自动处理 SELinux 上下文、OpenRC 与无 systemd 环境。

</td></tr>
</table>

### 架构示意

```text
                        ┌─────────────────── 远程 VPS ───────────────────┐   ┌────────── 本地服务器 / NAS ──────────┐
 外部用户 ── HTTPS ────► │  Caddy（自动证书 / 反代）                      │   │                                      │
                        │      │                                         │   │                                      │
                        │      ▼                                         │   │                                      │
                        │  127.0.0.1:远程端口 ◄── frps ◄────────────────┼───┼── frpc ◄── 127.0.0.1 / 192.168.x.x   │
 游戏玩家 ── TCP/UDP ──► │  公网IP:对外端口                           │   │      ▲                               │
                        └────────────────────────────────────────────────┘   │  Caddy（局域网反代 / 自签 / DNS证书）│
                                                                             └──────────────────────────────────────┘
```

---

## 典型使用场景

### 场景一：远程 VPS + 本地服务，域名 HTTPS 对外

1. **VPS**：打开 WebUI →「1. 穿透服务端 (frps)」→ 添加服务（Minecraft Java/Bedrock、Web、NAS、SSH、RDP 预设），需要域名时直接填「联动 Caddy 域名反代」→ 启动 `frps` 与 `Caddy`。
2. **导出**：点「📋 导出客户端连接码 / frpc.toml」复制 `autof://...`。
3. **本地服务器**：WebUI →「2. 穿透客户端 (frpc)」→「📥 一键导入连接码」→ 启动 `frpc`。

之后 VPS 上的 Caddy 会以 `https://你的域名` 反代到该穿透端口，证书自动管理。

### 场景二：服务器已有 NPM / Nginx，想统一接管

1. WebUI 顶部出现黄色提示「检测到服务器存在原有反代或穿透服务」，进入「4. 原服务探测与吞并」。
2. 只想改配置 →「✏️ 查看/编辑配置」，在线保存并重载原服务（自动留 `.autof.bak` 备份）。
3. 想统一接管 →「🚀 一键吞并迁移」，确认窗口会列出识别到的域名、上游地址、SSL 证书与清理步骤；确认后：
   - 证书复制到 `/etc/autof/certs/<域名>/`，原配置备份到 `/etc/autof/backup/`；
   - 停止并禁用老服务 / 老容器，释放 `80` / `443` / `7000`；
   - Autofrp 的 Caddy / frp 立即接管流量。

### 端口与 NAT 提示

| 项 | 说明 |
| --- | --- |
| 控制端口 | `frpc` 连接 `frps` 使用，全局一个（默认 `7000`） |
| 本机端口 | 内网服务实际监听端口（如 MC `25565`） |
| 对外端口 | 外部访问的公网端口，默认等于本机端口 |
| 端口白名单 | 支持「不限制（推荐）」「端口段（适合 NAT VPS）」「严格列表」三种策略 |

> NAT 型 VPS 需在服务商后台把控制端口与对外端口映射为「端口 → 同端口」。

---

## 证书管理（泛域名 / 自动续期）

证书页使用 **acme.sh + DNS-01** 申请，首次使用会自动下载 acme.sh 到 `/etc/autof/acme/`。

1. 先到「设置 → Caddy 全局设置」选择 DNS 服务商并填写 API 凭据（Cloudflare / 阿里云 / DNSPod / DuckDNS）。
2. 「证书管理 → 申请证书」填写域名，例如 `example.com, *.example.com`，选择密钥类型与输出目录。
3. 证书文件会写入输出目录（默认 `/etc/autof/certs/<域名>/`）：`fullchain.pem` + `privkey.pem`。
4. 面板每 12 小时检查一次续期；也可以点「续期」立即更新。续期后若 Caddy 规则引用了该目录会自动 reload。
5. 删除证书：面板证书列表点「删除」（可勾选同时删除文件），或命令行：

   ```bash
   sudo autof cert delete <证书ID或域名>              # 默认删除文件
   sudo autof cert delete <证书ID或域名> --keep-files # 仅移除托管记录
   sudo autof cert delete <证书ID或域名> --force      # 被 Caddy 规则引用时强制删除
   ```

> 面板的 HTTPS 与 Caddy 反代都会直接读取「证书管理」里的证书：设置页可选「证书管理中的证书」，Caddy 规则选「自定义证书」时也可从下拉框选择。

> **给 Docker 里的 NPM**：NPM 的证书存在自己的数据库里，不能靠替换文件生效。本工具提供「导出 PEM」，复制到 NPM 的 *SSL Certificates → Add → Custom Certificate* 即可；续期后需要重新粘贴一次。不会修改 NPM 数据库、容器或 compose。

| 服务商 | 凭据填写 |
| --- | --- |
| Cloudflare | Token 填 API Token（Zone.DNS Edit 权限），Secret 可留空或填 Account ID |
| 阿里云 | Token 填 AccessKeyId，Secret 填 AccessKeySecret |
| DNSPod | Token 填 DP_Id，Secret 填 DP_Key |
| DuckDNS | Token 填 DuckDNS Token |

## 备份与迁移

「设置 → 备份与迁移」可导出 `.tar.gz`，包含：

- `config.json`（可选是否包含 Token / 密码）
- `frps.toml` / `frpc.toml` / `Caddyfile`
- `certs/` 与 `tls/`（私钥跟随敏感信息选项）
- 可选旧版 `state.conf` / `services.conf` 与历史备份目录

恢复时只会写入 `/etc/autof`，并在写入前把当前配置归档到 `backup/pre-restore-<时间>`；不会修改 systemd 单元、Docker 容器或 compose 文件。可勾选「保留当前面板账号密码」避免恢复后无法登录。

```bash
sudo autof backup ./my-backup.tar.gz          # 含密钥
sudo autof backup ./share.tar.gz --no-secrets # 脱敏，便于分享
sudo autof restore ./my-backup.tar.gz --keep-creds
sudo autof cert list                          # 查看证书与剩余天数
```

## Compose 容器说明

吞并 Docker 中的 NPM / Caddy / frp 时：

- 如果容器由 `docker compose` 管理（含 `com.docker.compose.project` 标签），面板**只执行 `docker stop`**；
- **不会** `docker rm`、不会修改 restart 策略、不会编辑 compose 文件，同文件里的其他服务不受影响；
- 面板会显示 compose 项目、服务名与文件路径；如需永久移除，请自行编辑 compose 后执行 `docker compose up -d`。

## 面板 HTTPS（用 Caddy 反代）与 Cloudflare

面板本身只监听 HTTP（默认 `0.0.0.0:7500`），HTTPS 统一交给 Caddy 处理：

1. 「Caddy 反代 → 反代本面板」，填一个域名（如 `panel.example.com`）。
2. 证书模式选「自动公网 HTTPS」（80/443 可用）或「DNS API 证书」（NAT / 无 80 端口）。
3. 保存后访问 `https://panel.example.com`，证书由 Caddy 自动续期。
4. 建议安全组只放行 80/443，把 7500 限制为内网或仅本机访问。

**Cloudflare 注意事项**：

- 开了橙色云朵后，`HTTP-01` / `TLS-ALPN-01` 会被 CF 代理拦截，域名证书请用 **DNS-01**（本工具已内置 Cloudflare Token）或手动上传 **CF 源证书**到 Caddy 规则（证书模式选「自定义证书」）。
- SSL/TLS 模式建议 `Full` 或 `Full (strict)`。
- 可叠加 CF Access，只允许指定邮箱/域名访问面板。

## CLI 命令

```bash
sudo autof                          # 终端交互主菜单
sudo autof web-install              # 安装并启动 WebUI 后台服务
sudo autof web --listen :7500       # 前台运行 WebUI
sudo autof status                   # 系统 / 网络 / 组件状态与登录凭据
sudo autof detect                   # 扫描服务器原有进程与配置
sudo autof takeover <服务ID>        # 预览确认并吞并迁移老服务
sudo autof list                     # 穿透与反代规则列表
sudo autof export                   # 导出 autof:// 连接码与 frpc.toml
sudo autof import "<连接码|文件>"   # 导入客户端配置
sudo autof gen frps|frpc|caddy|code # 打印生成的配置
sudo autof install frps|frpc|caddy|web|all
sudo autof start|stop|restart|reload|logs [frps|frpc|caddy|autof]
sudo autof net 4|6|both             # 网络偏好（IPv4 / IPv6 / 双栈）
sudo autof pwd [新密码]             # 查看或重置 WebUI 密码
sudo autof backup [文件]            # 导出配置/证书备份（--no-secrets 脱敏）
sudo autof restore <文件>           # 从备份恢复（--keep-creds 保留面板密码）
sudo autof cert list                # 查看证书与剩余天数
sudo autof cert delete <ID|域名>    # 删除证书（--keep-files / --force）
sudo autof uninstall                # 完全卸载
```

---

## 文件位置

| 路径 | 说明 |
| --- | --- |
| `/etc/autof/config.json` | 统一配置（网络、WebUI、frps、frpc、Caddy 规则） |
| `/etc/autof/frps.toml` | `frps` 服务端配置 |
| `/etc/autof/frpc.toml` | 本机 `frpc` 运行配置 |
| `/etc/autof/Caddyfile` | 经语法校验的 Caddy 配置 |
| `/etc/autof/certs/<域名>/` | 迁移来的 `fullchain.pem` 与 `privkey.pem` |
| `/etc/autof/backup/` | 吞并前的原配置备份与恢复前归档 |
| `/etc/autof/acme/` | acme.sh 与账户数据（泛域名证书） |
| `/usr/local/bin/autof` | 主程序（含内嵌 WebUI） |

> 非 root 运行时回退到 `~/.config/autof`，也可用 `AUTOF_STATE_DIR` 指定。
> 旧版 v1.1.x 的 `state.conf` / `services.conf` 会在首次启动时**自动无损迁移**。

---

## 常见问题

<details>
<summary><b>一键命令下载失败 / 装成了旧版本？</b></summary>

v1.0.0 起脚本会强校验版本，下载失败会明确报错而**不会**静默复用旧 `autof`。若提示存在旧版本：

```bash
sudo rm -f /usr/local/bin/autof
# 重新执行一键命令
```

</details>

<details>
<summary><b>Caddy 起不来，提示 80/443 被占用？</b></summary>

多为系统里已有 Nginx / NPM / 旧 Caddy。进入 WebUI「4. 原服务探测与吞并」查看监听进程，或：

```bash
sudo autof detect
```

</details>

<details>
<summary><b>能连上端口但进不去服务？</b></summary>

端口可连通只说明 `frps` 正常，问题多在 `frpc → 服务`。确认 `localIP` 指向正确（默认 `127.0.0.1`，容器/跨机器场景需改为实际地址），并查看 `sudo autof logs frpc`。

</details>

<details>
<summary><b>服务日志在哪看？</b></summary>

WebUI「5. 日志与系统设置」，或 `sudo autof logs frps|frpc|caddy`。

</details>

---

## 免责声明

- **非官方项目**：Autofrp 与 [fatedier/frp](https://github.com/fatedier/frp)、[caddyserver/caddy](https://github.com/caddyserver/caddy) 无隶属关系。
- **组件版权**：`frp` 与 `Caddy` 遵循 Apache-2.0，二进制在运行时从官方源按需下载，本项目不重新分发。
- **本项目许可**：[MIT License](LICENSE)。
- 端口映射与内网穿透请遵守当地法律法规、服务商条款及目标服务授权。
