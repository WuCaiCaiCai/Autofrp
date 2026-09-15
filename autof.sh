#!/usr/bin/env bash
# Autofrp - NAT VPS frps 一键配置脚本
# Copyright (c) 2026 Wu Cai (WuCaiCaiCai)
# SPDX-License-Identifier: MIT
# 非官方项目，与 fatedier/frp 无隶属关系；frp 版权归其作者所有，遵循 Apache-2.0。
set -u

APP="autof"
SELF_URL="${AUTOF_SELF_URL:-https://raw.githubusercontent.com/WuCaiCaiCai/Autofrp/main/autof.sh}"

if [ "$(id -u)" = "0" ] || [ -w /etc ]; then
  STATE_DIR="/etc/autof"
else
  STATE_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/autof"
fi
STATE_FILE="$STATE_DIR/state.conf"
SVC_FILE="$STATE_DIR/services.conf"
FRPS_CONF="$STATE_DIR/frps.toml"
CLIENT_DIR="$STATE_DIR/clients"
MIGRATE_FLAG="$STATE_DIR/.migrated_v4"

if [ -t 1 ]; then
  C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'
  C_BLUE=$'\033[36m'; C_BOLD=$'\033[1m'; C_RST=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""; C_BOLD=""; C_RST=""
fi

info()  { printf '%s[*]%s %s\n' "$C_BLUE" "$C_RST" "$*" >&2; }
ok()    { printf '%s[+]%s %s\n' "$C_GREEN" "$C_RST" "$*" >&2; }
warn()  { printf '%s[!]%s %s\n' "$C_YELLOW" "$C_RST" "$*" >&2; }
err()   { printf '%s[x]%s %s\n' "$C_RED" "$C_RST" "$*" >&2; }
title() { printf '\n%s== %s ==%s\n' "$C_BOLD" "$*" "$C_RST" >&2; }
hr()    { printf '%s\n' "------------------------------------------------------------" >&2; }
die()   { err "$*"; exit 1; }

require_root() { [ "$(id -u)" = "0" ] || die "该操作需要 root 权限，请用 sudo 运行"; }

ask() {
  local prompt="$1" def="${2-}" ans=""
  if [ -n "$def" ]; then read -rp "$prompt [$def]: " ans || true; else read -rp "$prompt: " ans || true; fi
  printf '%s' "${ans:-$def}"
}

confirm() {
  local ans=""
  read -rp "$1 [y/N]: " ans || true
  case "$ans" in y|Y|yes|YES) return 0;; *) return 1;; esac
}

is_uint() { case "$1" in ''|*[!0-9]*) return 1;; *) return 0;; esac; }
valid_port() { is_uint "$1" && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }
gen_token() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 24; }
pause_key() { [ -t 0 ] || return 0; read -rp "按回车继续..." _ || true; }

load_state() {
  CONTROL_PORT=""
  TOKEN=""
  FRP_VERSION=""
  PUBLIC_IP=""
  PUBLIC_IP6=""
  V4_STATUS=""
  V6_STATUS=""
  mkdir -p "$STATE_DIR" "$CLIENT_DIR" 2>/dev/null
  touch "$SVC_FILE" 2>/dev/null
  [ -f "$STATE_FILE" ] && . "$STATE_FILE"
  migrate_old
}

save_state() {
  mkdir -p "$STATE_DIR" 2>/dev/null
  {
    printf 'CONTROL_PORT=%q\n' "$CONTROL_PORT"
    printf 'TOKEN=%q\n' "$TOKEN"
    printf 'FRP_VERSION=%q\n' "$FRP_VERSION"
    printf 'PUBLIC_IP=%q\n' "$PUBLIC_IP"
    printf 'PUBLIC_IP6=%q\n' "$PUBLIC_IP6"
    printf 'V4_STATUS=%q\n' "$V4_STATUS"
    printf 'V6_STATUS=%q\n' "$V6_STATUS"
  } > "$STATE_FILE"
}

migrate_old() {
  [ -f "$MIGRATE_FLAG" ] && return 0
  local tmp line nf name type lip lport a b c d e remark public
  [ -z "$CONTROL_PORT" ] && CONTROL_PORT="${CONTROL_OUT:-${CONTROL_IN:-${BIND_PORT:-}}}"
  if [ -s "$SVC_FILE" ]; then
    tmp="$(mktemp)"
    while IFS= read -r line; do
      [ -n "$line" ] || continue
      nf="$(awk -F'|' '{print NF}' <<< "$line")"
      if [ "$nf" -le 5 ]; then printf '%s\n' "$line" >> "$tmp"; continue; fi
      if [ "$nf" -ge 10 ]; then
        IFS='|' read -r name type lip lport a b c d e remark <<< "$line"
        public="$b"; [ -n "$public" ] || public="$a"
      else
        IFS='|' read -r name type lip lport a b c d remark <<< "$line"
        public="$a"
      fi
      case "$type" in tcp|udp) ;; *) continue;; esac
      [ -n "$public" ] || continue
      printf '%s|%s|%s|%s|%s\n' "$name" "$type" "$lport" "$public" "$remark" >> "$tmp"
    done < "$SVC_FILE"
    mv "$tmp" "$SVC_FILE"
  fi
  save_state
  touch "$MIGRATE_FLAG" 2>/dev/null
}

fetch_ip() {
  local fam="$1" urls u out
  if [ "$fam" = "4" ]; then urls=("https://ipv4.icanhazip.com" "https://api.ipify.org" "https://ipv4.ident.me")
  else urls=("https://ipv6.icanhazip.com" "https://api6.ipify.org" "https://ipv6.ident.me"); fi
  for u in "${urls[@]}"; do
    out="$(curl -"$fam" -fsS --connect-timeout 4 --max-time 6 "$u" 2>/dev/null | tr -d '[:space:]')"
    [ -n "$out" ] && { printf '%s' "$out"; return 0; }
  done
  return 1
}

is_private4() {
  case "$1" in
    10.*|192.168.*|169.254.*) return 0;;
    172.1[6-9].*|172.2[0-9].*|172.3[01].*) return 0;;
    100.6[4-9].*|100.[7-9][0-9].*|100.1[01][0-9].*|100.12[0-7].*) return 0;;
    *) return 1;;
  esac
}

detect_env() {
  local ip has_public4=false
  LOCAL_V4=(); LOCAL_V6=()
  while IFS= read -r ip; do [ -n "$ip" ] && LOCAL_V4+=("$ip"); done < <(
    ip -4 addr show scope global 2>/dev/null | awk '/inet /{sub(/\/.*/,"",$2); print $2}')
  while IFS= read -r ip; do [ -n "$ip" ] && LOCAL_V6+=("$ip"); done < <(
    ip -6 addr show scope global 2>/dev/null | awk '/inet6 /{sub(/\/.*/,"",$2); print $2}' | grep -viE '^(fe80|fc|fd)')
  PUBLIC_IP="$(fetch_ip 4 2>/dev/null || true)"
  PUBLIC_IP6="$(fetch_ip 6 2>/dev/null || true)"
  for ip in "${LOCAL_V4[@]:-}"; do
    [ -n "$ip" ] || continue
    ! is_private4 "$ip" && has_public4=true
  done
  if [ "$has_public4" = true ]; then V4_STATUS="公网 IPv4"
  elif [ -n "$PUBLIC_IP" ]; then V4_STATUS="NAT IPv4"
  else V4_STATUS="无 IPv4"; fi
  if [ "${#LOCAL_V6[@]}" -gt 0 ]; then V6_STATUS="公网 IPv6"
  elif [ -n "$PUBLIC_IP6" ]; then V6_STATUS="NAT IPv6"
  else V6_STATUS="无 IPv6"; fi
  [ -z "$PUBLIC_IP6" ] && [ "${#LOCAL_V6[@]}" -gt 0 ] && PUBLIC_IP6="${LOCAL_V6[0]}"
}

public_addr() {
  if [ -n "$PUBLIC_IP" ]; then printf '%s' "$PUBLIC_IP"; else printf '%s' "$PUBLIC_IP6"; fi
}

RULE_LINE="$(printf '═%.0s' {1..60})"

draw_header() {
  local st
  if pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1; then
    st="运行中 (PID $(pgrep -f '/usr/local/bin/frps' | head -1))"
  else
    st="已停止"
  fi
  printf '%s\n' "$RULE_LINE"
  printf '  Autofrp  ·  frps 服务端\n'
  printf '%s\n' "$RULE_LINE"
  printf '  IPv4   %s  %s\n' "${V4_STATUS:-未探测}" "${PUBLIC_IP:-}"
  printf '  IPv6   %s  %s\n' "${V6_STATUS:-未探测}" "${PUBLIC_IP6:-}"
  printf '  frps   %s\n' "$st"
  printf '  控制端口 %s\n' "${CONTROL_PORT:-未设置}"
  printf '%s\n' "$RULE_LINE"
}

svc_count() { wc -l < "$SVC_FILE" 2>/dev/null || echo 0; }

svc_unique_name() {
  local base="$1" name="$1" n=1
  while awk -F'|' -v x="$name" '$1==x{f=1} END{exit !f}' "$SVC_FILE" 2>/dev/null; do
    n=$((n + 1)); name="${base}-${n}"
  done
  printf '%s' "$name"
}

svc_append() { printf '%s|%s|%s|%s|%s\n' "$1" "$2" "$3" "$4" "$5" >> "$SVC_FILE"; }

svc_list() {
  if [ ! -s "$SVC_FILE" ]; then warn "暂无服务，请先「添加服务」"; return 0; fi
  printf '  %-4s %-14s %-5s %-9s %-9s %s\n' "序号" "名称" "类型" "本机端口" "对外端口" "备注"
  hr
  local i=0 name type lport public remark
  while IFS='|' read -r name type lport public remark; do
    [ -n "$name" ] || continue
    i=$((i + 1))
    printf '  %-4s %-14s %-5s %-9s %-9s %s\n' "$i" "$name" "$type" "$lport" "$public" "$remark"
  done < "$SVC_FILE"
}

add_service() {
  local type="$1" def_lport="$2" name="$3" ask_name="${4:-}"
  local lport public
  [ "$ask_name" = "yes" ] && name="$(ask '名称' "$(svc_unique_name "$name")")"
  printf '  本机端口：服务实际监听的端口（如 MC 的 25565）\n' >&2
  lport="$(ask '本机端口' "$def_lport")"
  valid_port "$lport" || { err "端口非法"; return 1; }
  printf '  对外端口：玩家连接用的公网端口（服务商网页端请映射 对外->对外）\n' >&2
  public="$(ask '对外端口' "$lport")"
  valid_port "$public" || { err "端口非法"; return 1; }
  svc_append "$name" "$type" "$lport" "$public" ""
  ok "已添加服务: $name"
  printf '  → 玩家连接: %s:%s\n' "$(public_addr)" "$public" >&2
}

svc_view() {
  svc_list || return 0
  local n; n="$(ask '输入序号查看详情(留空返回)')"
  [ -n "$n" ] || return 0
  is_uint "$n" || { err "序号非法"; return 1; }
  [ "$n" -ge 1 ] && [ "$n" -le "$(svc_count)" ] || { err "序号超出范围"; return 1; }
  local line name type lport public remark
  line="$(sed -n "${n}p" "$SVC_FILE")"
  IFS='|' read -r name type lport public remark <<< "$line"
  title "服务: $name"
  printf '  %-10s %s\n' "类型" "$type"
  printf '  %-10s 127.0.0.1:%s\n' "本机" "$lport"
  printf '  %-10s %s\n' "对外端口" "$public"
  printf '  %-10s %s\n' "玩家连接" "$(public_addr):$public"
  [ -n "$remark" ] && printf '  %-10s %s\n' "备注" "$remark"
  printf '\n'
  print_block "该服务完整 frpc.toml" "$(gen_frpc_single "$n")"
  printf '  1) 编辑   2) 删除   0) 返回\n'
  local c; c="$(ask '请选择' '0')"
  case "$c" in
    1) svc_edit "$n" ;;
    2) svc_del "$n" ;;
  esac
}

svc_edit() {
  local n="${1:-}"
  if [ -z "$n" ]; then
    svc_list || return 0
    n="$(ask '输入要编辑的序号(留空取消)')"
    [ -n "$n" ] || return 0
  fi
  is_uint "$n" || { err "序号非法"; return 1; }
  [ "$n" -ge 1 ] && [ "$n" -le "$(svc_count)" ] || { err "序号超出范围"; return 1; }
  local line name type lport public remark v
  line="$(sed -n "${n}p" "$SVC_FILE")"
  IFS='|' read -r name type lport public remark <<< "$line"
  v="$(ask '名称' "$name")"; name="$v"
  v="$(ask '本机端口' "$lport")"; valid_port "$v" && lport="$v"
  v="$(ask '对外端口' "$public")"; valid_port "$v" && public="$v"
  v="$(ask '备注' "$remark")"; remark="$v"
  local newline="$name|$type|$lport|$public|$remark"
  local tmp; tmp="$(mktemp)"
  awk -v n="$n" -v nl="$newline" 'NR==n{print nl; next}{print}' "$SVC_FILE" > "$tmp" && mv "$tmp" "$SVC_FILE"
  ok "已更新服务: $name"
  apply_config
}

svc_del() {
  local n="${1:-}"
  if [ -z "$n" ]; then
    svc_list || return 0
    n="$(ask '输入要删除的序号(留空取消)')"
    [ -n "$n" ] || return 0
  fi
  is_uint "$n" || { err "序号非法"; return 1; }
  [ "$n" -ge 1 ] && [ "$n" -le "$(svc_count)" ] || { err "序号超出范围"; return 1; }
  sed -i "${n}d" "$SVC_FILE"
  ok "已删除序号 $n"
  apply_config
}

add_menu() {
  while true; do
    title "添加服务"
    printf '  1) Minecraft Java    (TCP 25565)\n'
    printf '  2) Minecraft Bedrock (UDP 19132)\n'
    printf '  3) Emby              (TCP 8096)\n'
    printf '  4) 自定义 TCP\n'
    printf '  5) 自定义 UDP\n'
    printf '  0) 返回\n'
    local c; c="$(ask '请选择' '0')"
    printf '\n'
    case "$c" in
      1) add_service tcp 25565 mc-java ;;
      2) add_service udp 19132 mc-bedrock ;;
      3) add_service tcp 8096 emby ;;
      4) add_service tcp "" tcp yes ;;
      5) add_service udp "" udp yes ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
    apply_config
    pause_key
  done
}

gen_frpc_proxy() {
  local n="$1" name type lport public remark line
  line="$(sed -n "${n}p" "$SVC_FILE")"
  IFS='|' read -r name type lport public remark <<< "$line"
  [ -n "$name" ] || return 1
  [ -n "$remark" ] && echo "# $remark"
  echo "# 玩家连接: $(public_addr):$public"
  echo "[[proxies]]"
  echo "name = \"$name\""
  echo "type = \"$type\""
  echo "localIP = \"127.0.0.1\""
  echo "localPort = $lport"
  echo "remotePort = $public"
}

gen_frpc_header() {
  echo "# frpc 客户端配置（在内网机器上运行 frpc）"
  echo "serverAddr = \"$(public_addr)\""
  echo "serverPort = $CONTROL_PORT"
  echo "auth.method = \"token\""
  [ -n "$TOKEN" ] && echo "auth.token = \"$TOKEN\""
  echo "transport.tls.enable = true"
  echo
}

gen_frpc() {
  local i n
  gen_frpc_header
  n="$(svc_count)"; i=1
  while [ "$i" -le "$n" ]; do gen_frpc_proxy "$i"; echo; i=$((i + 1)); done
}

gen_frpc_single() {
  gen_frpc_header
  gen_frpc_proxy "$1"
  echo
}

gen_frps() {
  echo "# frps 服务端配置（在 VPS 上运行 frps）"
  echo "bindAddr = \"0.0.0.0\""
  echo "bindPort = $CONTROL_PORT"
  echo "auth.method = \"token\""
  [ -n "$TOKEN" ] && echo "auth.token = \"$TOKEN\""
  local ports
  ports="$(awk -F'|' '($2=="tcp"||$2=="udp") && $4!=""{print $4}' "$SVC_FILE" 2>/dev/null | grep -E '^[0-9]+$' | sort -n -u)"
  if [ -n "$ports" ]; then
    echo "allowPorts = ["
    local p
    for p in $ports; do echo "  { single = $p },"; done
    echo "]"
  fi
}

print_block() {
  local name="$1" content="$2"
  title "$name"
  hr
  printf '%s\n' "$content"
  hr
}

save_frps_config() { mkdir -p "$STATE_DIR" 2>/dev/null; gen_frps > "$FRPS_CONF"; }
save_client_configs() { mkdir -p "$CLIENT_DIR" 2>/dev/null; gen_frpc > "$CLIENT_DIR/frpc.toml"; }

apply_config() {
  save_frps_config
  save_client_configs
  if pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1 && command -v systemctl >/dev/null 2>&1; then
    systemctl restart frps 2>/dev/null && info "已重启 frps 以应用新配置"
  fi
}

setup_control() {
  title "首次配置：设置控制端口"
  printf '  控制端口是 frpc 连接 frps 用的，和具体服务无关，只需要一个。\n' >&2
  printf '  请在服务商网页端把它映射为「端口 -> 同端口」。\n' >&2
  local v t
  v="$(ask '控制端口' "${CONTROL_PORT:-7000}")"
  valid_port "$v" || v=7000
  CONTROL_PORT="$v"
  t="$(ask 'auth token(留空自动生成)' "$TOKEN")"
  [ -n "$t" ] || t="$(gen_token)"
  TOKEN="$t"
  save_state
  ok "控制端口: $CONTROL_PORT"
}

server_menu() {
  while true; do
    [ -t 1 ] && clear
    draw_header
    printf '\n  1) 安装/更新并启动 frps\n'
    printf '  2) 停止\n'
    printf '  3) 重启\n'
    printf '  4) 状态\n'
    printf '  5) 日志\n'
    printf '  6) 修改控制端口\n'
    printf '  0) 返回\n'
    local c; c="$(ask '请选择' '0')"
    printf '\n'
    case "$c" in
      1) install_frps ;;
      2) service_ctl stop ;;
      3) service_ctl restart ;;
      4) service_ctl status ;;
      5) service_ctl logs ;;
      6) setup_control ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
    pause_key
  done
}

preview_menu() {
  while true; do
    [ -t 1 ] && clear
    draw_header
    printf '\n  1) 预览 frps.toml\n'
    printf '  2) 预览 frpc.toml\n'
    printf '  3) 保存到文件\n'
    printf '  0) 返回\n'
    local c; c="$(ask '请选择' '0')"
    printf '\n'
    case "$c" in
      1) print_block "frps.toml" "$(gen_frps)" ;;
      2) print_block "frpc.toml" "$(gen_frpc)" ;;
      3) save_frps_config; save_client_configs; ok "已保存: $FRPS_CONF 与 $CLIENT_DIR/frpc.toml" ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
    pause_key
  done
}

arch_name() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64;; aarch64|arm64) echo arm64;;
    armv7l|armv7) echo arm;; i386|i686) echo 386;; *) echo "";;
  esac
}

latest_frp_version() {
  curl -fsS --connect-timeout 5 --max-time 10 https://api.github.com/repos/fatedier/frp/releases/latest 2>/dev/null \
    | grep -o '"tag_name": *"v[^"]*"' | head -1 | sed 's/.*"v\(.*\)"/\1/'
}

download_frp() {
  local ver="$1" arch="$2" base tmp u dir
  base="frp_${ver}_linux_${arch}.tar.gz"
  tmp="$(mktemp -d)"
  local urls=(
    "https://github.com/fatedier/frp/releases/download/v${ver}/${base}"
    "https://ghfast.top/https://github.com/fatedier/frp/releases/download/v${ver}/${base}"
    "https://gh-proxy.com/https://github.com/fatedier/frp/releases/download/v${ver}/${base}"
  )
  for u in "${urls[@]}"; do
    info "尝试下载: $u"
    if curl -fL --connect-timeout 10 --max-time 180 -o "$tmp/$base" "$u" 2>/dev/null; then
      tar -xzf "$tmp/$base" -C "$tmp" 2>/dev/null || { warn "解压失败"; continue; }
      dir="$(find "$tmp" -maxdepth 1 -type d -name "frp_${ver}_linux_${arch}" | head -1)"
      [ -n "$dir" ] || { warn "未找到解压目录"; continue; }
      printf '%s' "$dir"; return 0
    fi
  done
  return 1
}

install_frps() {
  require_root
  load_state
  local arch; arch="$(arch_name)"
  [ -n "$arch" ] || die "不支持的架构: $(uname -m)"
  local ver="$FRP_VERSION"
  [ -n "$ver" ] || { info "查询最新 frp 版本..."; ver="$(latest_frp_version)"; }
  [ -n "$ver" ] || ver="$(ask '请输入 frp 版本号(如 0.61.1)')"
  [ -n "$ver" ] || die "未指定 frp 版本"
  FRP_VERSION="$ver"; save_state
  local dir; dir="$(download_frp "$ver" "$arch")" || die "frp 下载失败"
  install -m 0755 "$dir/frps" /usr/local/bin/frps || die "安装 frps 失败"
  rm -rf "$(dirname "$dir")"
  ok "frps 已安装 (v$ver)"
  save_frps_config
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    cat > /etc/systemd/system/frps.service <<EOF
[Unit]
Description=frp server (frps)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/frps -c $FRPS_CONF
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable frps >/dev/null 2>&1
    systemctl restart frps
    sleep 1
    if systemctl is-active --quiet frps; then ok "frps 已启动"; else err "frps 启动失败，查看: journalctl -u frps -e"; fi
  else
    warn "未检测到 systemd，使用 nohup 启动"
    pkill -f '/usr/local/bin/frps' 2>/dev/null || true
    nohup /usr/local/bin/frps -c "$FRPS_CONF" >"$STATE_DIR/frps.log" 2>&1 &
    sleep 1
    pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1 && ok "frps 已后台启动" || err "frps 启动失败"
  fi
}

service_ctl() {
  local action="$1"
  if ! command -v systemctl >/dev/null 2>&1; then
    warn "无 systemd，请手动管理: pkill -f /usr/local/bin/frps"
    return 0
  fi
  case "$action" in
    status) systemctl status frps --no-pager;;
    restart) systemctl restart frps && ok "已重启";;
    stop) systemctl stop frps && ok "已停止";;
    logs) journalctl -u frps -e --no-pager;;
  esac
}

uninstall_all() {
  require_root
  title "卸载 Autofrp"
  printf '  将删除：frps 服务与程序、配置目录 %s、命令 /usr/local/bin/%s\n' "$STATE_DIR" "$APP" >&2
  confirm "确认卸载?" || { info "已取消"; return 1; }
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop frps 2>/dev/null || true
    systemctl disable frps 2>/dev/null || true
    rm -f /etc/systemd/system/frps.service
    systemctl daemon-reload 2>/dev/null || true
  fi
  pkill -f '/usr/local/bin/frps' 2>/dev/null || true
  rm -f /usr/local/bin/frps
  [ -n "$STATE_DIR" ] && [ "$STATE_DIR" != "/" ] && rm -rf "$STATE_DIR"
  rm -f "/usr/local/bin/$APP"
  ok "已卸载完成"
  return 0
}

self_install() {
  require_root
  local target="/usr/local/bin/$APP" src=""
  if [ -f "$0" ]; then src="$0"
  elif [ -n "$SELF_URL" ]; then
    local tmp; tmp="$(mktemp)"
    curl -fsSL --connect-timeout 10 --max-time 60 "$SELF_URL" -o "$tmp" || die "下载失败"
    src="$tmp"
  else die "无法获取脚本来源"; fi
  [ "$(readlink -f "$src")" = "$target" ] && { ok "已安装在 $target"; return 0; }
  install -m 0755 "$src" "$target" || die "安装失败"
  ok "已安装为命令: $target"
}

self_update() {
  require_root
  [ -n "$SELF_URL" ] || die "未设置 SELF_URL"
  local tmp; tmp="$(mktemp)"
  curl -fsSL --connect-timeout 10 --max-time 60 "$SELF_URL" -o "$tmp" || die "下载失败"
  bash -n "$tmp" || die "下载的脚本语法校验失败"
  install -m 0755 "$tmp" "/usr/local/bin/$APP" || die "写入失败"
  rm -f "$tmp"
  ok "已更新到最新版本"
}

maybe_install_prompt() {
  [ "$(id -u)" = "0" ] || return 0
  [ -f "$0" ] && [ "$(readlink -f "$0")" = "/usr/local/bin/$APP" ] && return 0
  [ ! -f "$0" ] && [ -z "$SELF_URL" ] && return 0
  confirm "是否安装为命令 $APP (以后直接运行)?" && self_install
}

main_screen() {
  load_state
  detect_env
  save_state
  if [ -z "$CONTROL_PORT" ]; then
    setup_control
  fi
  while true; do
    [ -t 1 ] && clear
    draw_header
    if ! pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1; then
      warn "frps 未运行：选「3) 安装/启动 frps」下载并启动"
    fi
    printf '\n  1) 添加服务\n'
    printf '  2) 查看服务\n'
    printf '  3) 安装/启动 frps\n'
    printf '  4) 服务端控制\n'
    printf '  5) 预览配置\n'
    printf '  6) 卸载\n'
    printf '  0) 退出\n'
    local c; c="$(ask '请选择' '0')"
    case "$c" in
      1) add_menu ;;
      2) svc_view ;;
      3) install_frps ;;
      4) server_menu ;;
      5) preview_menu ;;
      6) uninstall_all && return 0 ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
  done
}

usage() {
  cat <<EOF
$APP - NAT 小鸡 frps 一键工具

用法:
  $APP                 主界面
  $APP list            查看服务列表
  $APP gen frps        打印 frps.toml
  $APP gen frpc        打印 frpc.toml
  $APP install         安装并启动 frps
  $APP status|restart|stop|logs
  $APP uninstall       完全卸载(服务/程序/配置/命令)
  $APP self-install    安装为 /usr/local/bin/$APP
  $APP self-update     更新脚本自身
  $APP help            显示帮助

配置目录: $STATE_DIR
EOF
}

main() {
  case "${1:-}" in
    "") main_screen ;;
    menu|manage) main_screen ;;
    list) load_state; svc_list ;;
    gen)
      load_state
      case "${2:-frps}" in
        frps) gen_frps ;;
        frpc) gen_frpc ;;
        *) die "未知类型: ${2:-}" ;;
      esac
      ;;
    install) install_frps ;;
    status|restart|stop|logs) service_ctl "$1" ;;
    uninstall) uninstall_all ;;
    self-install) self_install ;;
    self-update) self_update ;;
    help|-h|--help) usage ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
