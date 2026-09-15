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
OLD_NAT_FILE="$STATE_DIR/nat_ports.conf"
FRPS_CONF="$STATE_DIR/frps.toml"
CLIENT_DIR="$STATE_DIR/clients"
MIGRATE_FLAG="$STATE_DIR/.migrated_v3"

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
pause_key() { [ -t 0 ] || return 0; read -rp "按回车返回..." _ || true; }

load_state() {
  CONTROL_IN="7000"
  CONTROL_OUT=""
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
    printf 'CONTROL_IN=%q\n' "$CONTROL_IN"
    printf 'CONTROL_OUT=%q\n' "$CONTROL_OUT"
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
  local e tmp line nf name type lip lport p5 dom sec mux rem remote external
  [ -n "${BIND_PORT:-}" ] && CONTROL_IN="$BIND_PORT"
  if [ -z "$CONTROL_OUT" ] && [ -f "$OLD_NAT_FILE" ]; then
    e="$(awk -F'|' -v p="$CONTROL_IN" '$2==p{print $1; exit}' "$OLD_NAT_FILE" 2>/dev/null)"
    [ -n "$e" ] && CONTROL_OUT="$e"
  fi
  if [ -s "$SVC_FILE" ]; then
    tmp="$(mktemp)"
    while IFS= read -r line; do
      [ -n "$line" ] || continue
      nf="$(awk -F'|' '{print NF}' <<< "$line")"
      if [ "$nf" -ge 10 ]; then printf '%s\n' "$line" >> "$tmp"; continue; fi
      IFS='|' read -r name type lip lport p5 dom sec mux rem <<< "$line"
      remote=""; external=""
      if [ "$type" = "tcp" ] || [ "$type" = "udp" ]; then
        e="$(awk -F'|' -v p="$p5" '$2==p{print $1; exit}' "$OLD_NAT_FILE" 2>/dev/null)"
        if [ -n "$e" ]; then
          remote="$p5"; external="$e"
        else
          e="$(awk -F'|' -v p="$p5" '$1==p{print $2; exit}' "$OLD_NAT_FILE" 2>/dev/null)"
          if [ -n "$e" ]; then remote="$e"; external="$p5"; else remote="$p5"; external="$p5"; fi
        fi
      fi
      printf '%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n' "$name" "$type" "$lip" "$lport" "$remote" "$external" "$dom" "$sec" "$mux" "$rem" >> "$tmp"
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
  printf '  控制   内部 %s  对外 %s\n' "${CONTROL_IN:-未设置}" "${CONTROL_OUT:-未设置}"
  printf '%s\n' "$RULE_LINE"
}

svc_unique_name() {
  local base="$1" name="$1" n=1
  while awk -F'|' -v x="$name" '$1==x{f=1} END{exit !f}' "$SVC_FILE" 2>/dev/null; do
    n=$((n + 1)); name="${base}-${n}"
  done
  printf '%s' "$name"
}

svc_append() { printf '%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$8" "$9" "${10}" >> "$SVC_FILE"; }

svc_list() {
  if [ ! -s "$SVC_FILE" ]; then warn "暂无服务"; return 0; fi
  printf '  %-4s %-12s %-6s %-8s %-8s %-8s %s\n' "序号" "名称" "类型" "本机" "内部" "对外" "备注"
  hr
  local i=0 name type lip lport remote external dom sec mux rem
  while IFS='|' read -r name type lip lport remote external dom sec mux rem; do
    [ -n "$name" ] || continue
    i=$((i + 1))
    [ -n "$remote" ] || remote="-"
    [ -n "$external" ] || external="-"
    printf '  %-4s %-12s %-6s %-8s %-8s %-8s %s\n' "$i" "$name" "$type" "$lport" "$remote" "$external" "$rem"
  done < "$SVC_FILE"
}

svc_count() { wc -l < "$SVC_FILE" 2>/dev/null || echo 0; }

PICK=""
svc_pick() {
  svc_list || return 1
  local n; n="$(ask '请输入序号(留空取消)')"
  [ -n "$n" ] || return 1
  is_uint "$n" || { err "序号非法"; return 1; }
  [ "$n" -ge 1 ] && [ "$n" -le "$(svc_count)" ] || { err "序号超出范围"; return 1; }
  PICK="$n"
}

svc_detail() {
  svc_pick || return 1
  local n="$PICK"
  local line name type lip lport remote external dom sec mux rem
  line="$(sed -n "${n}p" "$SVC_FILE")"
  IFS='|' read -r name type lip lport remote external dom sec mux rem <<< "$line"
  title "服务: $name"
  printf '  %-10s %s\n' "类型" "$type"
  printf '  %-10s %s\n' "本机" "$lip:$lport"
  case "$type" in
    tcp|udp)
      printf '  %-10s %s\n' "VPS 内部" "$remote"
      printf '  %-10s %s\n' "对外端口" "$external"
      printf '  %-10s %s\n' "玩家连接" "$(public_addr):$external"
      printf '  %-10s %s\n' "NAT 映射" "外部 $external -> 内部 $remote"
      ;;
    stcp|xtcp) printf '  %-10s %s\n' "secretKey" "$sec";;
  esac
  [ -n "$rem" ] && printf '  %-10s %s\n' "备注" "$rem"
  printf '\n'
  print_block "该服务完整 frpc.toml (可直接复制)" "$(gen_frpc_single "$n")"
}

ask_port_model() {
  printf '    本机端口：你机器上服务实际监听的端口（如 MC 的 25565）\n' >&2
  L_PORT="$(ask '本机端口' "${L_PORT:-}")"
  valid_port "$L_PORT" || { err "本机端口非法"; return 1; }
  printf '    对外端口：玩家连接用的公网端口（服务商网页端映射出来的那个）\n' >&2
  E_PORT="$(ask '对外端口' "${E_PORT:-$L_PORT}")"
  valid_port "$E_PORT" || { err "对外端口非法"; return 1; }
  printf '    VPS 内部端口：frps 监听的端口(remotePort)，默认同对外；\n' >&2
  printf '    若服务商映射是「外部 30008 -> 内部 25565」，这里就填 25565\n' >&2
  R_PORT="$(ask 'VPS 内部端口(remotePort)' "$E_PORT")"
  valid_port "$R_PORT" || { err "内部端口非法"; return 1; }
}

svc_add_port() {
  local type="$1" def_lport="$2" label="$3"
  L_PORT="$def_lport"; E_PORT=""; R_PORT=""
  ask_port_model || return 1
  svc_append "$(svc_unique_name "$label")" "$type" "127.0.0.1" "$L_PORT" "$R_PORT" "$E_PORT" "" "" "" "$label"
  ok "已添加: $label"
  printf '  链路: 玩家 -> %s:%s --(NAT)--> VPS:%s --(frp)--> 127.0.0.1:%s\n' \
    "$(public_addr)" "$E_PORT" "$R_PORT" "$L_PORT" >&2
}

svc_preset_mc_java() { svc_add_port tcp 25565 "mc-java"; }
svc_preset_mc_bedrock() { svc_add_port udp 19132 "mc-bedrock"; }

svc_preset_emby() {
  L_PORT="8096"; E_PORT=""; R_PORT=""
  ask_port_model || return 1
  svc_append "$(svc_unique_name emby)" "tcp" "127.0.0.1" "$L_PORT" "$R_PORT" "$E_PORT" "" "" "" "Emby"
  ok "已添加: emby (TCP 转发)"
  printf '  链路: 玩家 -> %s:%s --(NAT)--> VPS:%s --(frp)--> 127.0.0.1:%s\n' \
    "$(public_addr)" "$E_PORT" "$R_PORT" "$L_PORT" >&2
}

svc_add_custom() {
  title "其他自定义"
  echo "  1) TCP   2) UDP   3) STCP   4) XTCP"
  local c; c="$(ask '类型' '1')"
  local type lip lport remote external sec name
  case "$c" in
    1) type=tcp;; 2) type=udp;; 3) type=stcp;; 4) type=xtcp;;
    *) warn "无效类型"; return 1;;
  esac
  name="$(ask '名称' "$(svc_unique_name "$type")")"
  lip="$(ask '本机地址' '127.0.0.1')"
  case "$type" in
    tcp|udp)
      L_PORT=""; E_PORT=""; R_PORT=""
      ask_port_model || return 1
      svc_append "$name" "$type" "$lip" "$L_PORT" "$R_PORT" "$E_PORT" "" "" "" ""
      ;;
    stcp|xtcp)
      printf '    本机端口：服务实际监听端口\n' >&2
      lport="$(ask '本机端口')"
      valid_port "$lport" || { err "本机端口非法"; return 1; }
      sec="$(ask 'secretKey' "$(gen_token)")"
      svc_append "$name" "$type" "$lip" "$lport" "" "" "" "$sec" "" ""
      ;;
  esac
  ok "已添加: $name"
}

svc_edit() {
  svc_pick || return 1
  local n="$PICK"
  local line name type lip lport remote external dom sec mux rem v
  line="$(sed -n "${n}p" "$SVC_FILE")"
  IFS='|' read -r name type lip lport remote external dom sec mux rem <<< "$line"
  v="$(ask '名称' "$name")"; name="$v"
  v="$(ask '本机地址' "$lip")"; lip="$v"
  printf '    本机端口：服务实际监听端口\n' >&2
  v="$(ask '本机端口' "$lport")"; valid_port "$v" && lport="$v"
  case "$type" in
    tcp|udp)
      printf '    对外端口：玩家连接用；VPS 内部端口：frps 监听的 remotePort\n' >&2
      v="$(ask '对外端口' "$external")"; valid_port "$v" && external="$v"
      v="$(ask 'VPS 内部端口(remotePort)' "$remote")"; valid_port "$v" && remote="$v"
      ;;
    stcp|xtcp)
      v="$(ask 'secretKey' "$sec")"; sec="$v"
      ;;
  esac
  v="$(ask '备注' "$rem")"; rem="$v"
  local newline="$name|$type|$lip|$lport|$remote|$external|$dom|$sec|$mux|$rem"
  local tmp; tmp="$(mktemp)"
  awk -v n="$n" -v nl="$newline" 'NR==n{print nl; next}{print}' "$SVC_FILE" > "$tmp" && mv "$tmp" "$SVC_FILE"
  save_frps_config
  save_client_configs
  ok "已更新: $name"
}

svc_del() {
  svc_pick || return 1
  local n="$PICK"
  sed -i "${n}d" "$SVC_FILE"
  save_frps_config
  save_client_configs
  ok "已删除序号 $n"
}

svc_menu() {
  while true; do
    [ -t 1 ] && clear
    draw_header
    svc_list
    printf '\n  1) Minecraft Java    2) Minecraft Bedrock   3) Emby\n'
    printf '  4) 其他自定义(TCP/UDP/STCP/XTCP)\n'
    printf '  5) 查看服务详情      6) 编辑服务      7) 删除服务\n'
    printf '  0) 返回\n'
    local c; c="$(ask '请选择' '0')"
    printf '\n'
    case "$c" in
      1) svc_preset_mc_java ;;
      2) svc_preset_mc_bedrock ;;
      3) svc_preset_emby ;;
      4) svc_add_custom ;;
      5) svc_detail ;;
      6) svc_edit ;;
      7) svc_del ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
    save_frps_config
    save_client_configs
    pause_key
  done
}

gen_frpc_proxy() {
  local n="$1" line name type lip lport remote external dom sec mux rem
  line="$(sed -n "${n}p" "$SVC_FILE")"
  IFS='|' read -r name type lip lport remote external dom sec mux rem <<< "$line"
  [ -n "$name" ] || return 1
  [ -n "$rem" ] && echo "# $rem"
  echo "[[proxies]]"
  echo "name = \"$name\""
  echo "type = \"$type\""
  case "$type" in
    tcp|udp)
      echo "localIP = \"$lip\""
      echo "localPort = $lport"
      echo "remotePort = $remote"
      ;;
    stcp|xtcp)
      echo "secretKey = \"$sec\""
      echo "localIP = \"$lip\""
      echo "localPort = $lport"
      ;;
  esac
}

gen_frpc_header() {
  echo "# frpc 客户端配置（在内网机器上运行 frpc）"
  echo "serverAddr = \"$(public_addr)\""
  echo "serverPort = $CONTROL_OUT"
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
  echo "bindPort = $CONTROL_IN"
  echo "auth.method = \"token\""
  [ -n "$TOKEN" ] && echo "auth.token = \"$TOKEN\""
  local ports
  ports="$(awk -F'|' '($2=="tcp"||$2=="udp") && $5!=""{print $5}' "$SVC_FILE" 2>/dev/null | grep -E '^[0-9]+$' | sort -n -u)"
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

control_menu() {
  title "设置 frps 控制"
  printf '  控制端口是 frpc 连接 frps 的入口，与具体服务无关。\n' >&2
  printf '  内部端口在 VPS 上监听；对外端口由服务商网页端映射，frpc 用它连接。\n' >&2
  local v
  v="$(ask 'frps 内部端口 bindPort' "$CONTROL_IN")"
  valid_port "$v" && CONTROL_IN="$v"
  v="$(ask '控制端口对外映射(frpc 连接用)' "$CONTROL_OUT")"
  valid_port "$v" && CONTROL_OUT="$v"
  v="$(ask 'auth token(留空自动生成)' "$TOKEN")"
  [ -n "$v" ] || v="$(gen_token)"
  TOKEN="$v"
  save_state
  ok "已保存控制设置: 内部 $CONTROL_IN / 对外 $CONTROL_OUT"
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
    uninstall)
      systemctl stop frps 2>/dev/null; systemctl disable frps 2>/dev/null
      rm -f /etc/systemd/system/frps.service; systemctl daemon-reload
      rm -f /usr/local/bin/frps
      ok "已卸载 frps（配置保留在 $STATE_DIR）";;
  esac
}

server_menu() {
  while true; do
    [ -t 1 ] && clear
    draw_header
    printf '\n  1) 安装/更新并启动\n'
    printf '  2) 停止\n'
    printf '  3) 重启\n'
    printf '  4) 状态\n'
    printf '  5) 日志\n'
    printf '  6) 卸载\n'
    printf '  0) 返回\n'
    local c; c="$(ask '请选择' '0')"
    printf '\n'
    case "$c" in
      1) install_frps ;;
      2) service_ctl stop ;;
      3) service_ctl restart ;;
      4) service_ctl status ;;
      5) service_ctl logs ;;
      6) service_ctl uninstall ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
    pause_key
  done
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
  while true; do
    [ -t 1 ] && clear
    draw_header
    if [ -z "$CONTROL_OUT" ]; then
      warn "尚未设置 frps 控制端口，请先完成设置"
      control_menu
      [ -z "$CONTROL_OUT" ] && { warn "未设置控制端口，退出"; return 1; }
    fi
    printf '\n  1) 设置 frps 控制\n'
    printf '  2) 服务管理\n'
    printf '  3) 预览/生成配置\n'
    printf '  4) 服务端控制\n'
    printf '  0) 退出\n'
    local c; c="$(ask '请选择' '0')"
    case "$c" in
      1) control_menu ;;
      2) svc_menu ;;
      3) preview_menu ;;
      4) server_menu ;;
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
  $APP status|restart|stop|logs|uninstall
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
    status|restart|stop|logs|uninstall) service_ctl "$1" ;;
    self-install) self_install ;;
    self-update) self_update ;;
    help|-h|--help) usage ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
