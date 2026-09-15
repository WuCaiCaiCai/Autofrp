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
NAT_FILE="$STATE_DIR/nat_ports.conf"
SVC_FILE="$STATE_DIR/services.conf"
FRPS_CONF="$STATE_DIR/frps.toml"
CLIENT_DIR="$STATE_DIR/clients"

if [ -t 1 ]; then
  C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'
  C_BLUE=$'\033[36m'; C_BOLD=$'\033[1m'; C_RST=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""; C_BOLD=""; C_RST=""
fi

info()  { printf '%s[*]%s %s\n' "$C_BLUE" "$C_RST" "$*"; }
ok()    { printf '%s[+]%s %s\n' "$C_GREEN" "$C_RST" "$*"; }
warn()  { printf '%s[!]%s %s\n' "$C_YELLOW" "$C_RST" "$*"; }
err()   { printf '%s[x]%s %s\n' "$C_RED" "$C_RST" "$*" >&2; }
title() { printf '\n%s== %s ==%s\n' "$C_BOLD" "$*" "$C_RST"; }
step()  { printf '\n%s>>> %s%s\n' "$C_BOLD" "$*" "$C_RST"; }
hr()    { printf '%s\n' "------------------------------------------------------------"; }
die()   { err "$*"; exit 1; }

require_root() {
  [ "$(id -u)" = "0" ] || die "该操作需要 root 权限，请用 sudo 运行"
}

ask() {
  local prompt="$1" def="${2-}" ans=""
  if [ -n "$def" ]; then
    read -rp "$prompt [$def]: " ans || true
  else
    read -rp "$prompt: " ans || true
  fi
  printf '%s' "${ans:-$def}"
}

confirm() {
  local ans=""
  read -rp "$1 [y/N]: " ans || true
  case "$ans" in y|Y|yes|YES) return 0;; *) return 1;; esac
}

is_uint() { case "$1" in ''|*[!0-9]*) return 1;; *) return 0;; esac; }

valid_port() {
  is_uint "$1" && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]
}

gen_token() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 24; }

load_state() {
  NICKNAME="我的 frps"
  EXPOSE_MODE="v4"
  BIND_ADDR="0.0.0.0"
  BIND_PORT="7000"
  KCP_PORT=""
  QUIC_PORT=""
  TOKEN=""
  TLS_FORCE="false"
  DASH_ENABLE="true"
  DASH_ADDR="127.0.0.1"
  DASH_PORT="7500"
  DASH_USER="admin"
  DASH_PASS=""
  VHOST_HTTP=""
  VHOST_HTTPS=""
  SUBDOMAIN_HOST=""
  MAX_PORTS="0"
  LOG_LEVEL="info"
  LOG_TO="console"
  FRP_VERSION=""
  PUBLIC_IP=""
  PUBLIC_IP6=""
  PUBLIC_ADDR=""
  V4_STATUS=""
  V6_STATUS=""
  AVAIL_V4=0
  AVAIL_V6=0
  mkdir -p "$STATE_DIR" "$CLIENT_DIR" 2>/dev/null
  touch "$NAT_FILE" "$SVC_FILE" 2>/dev/null
  [ -f "$STATE_FILE" ] && . "$STATE_FILE"
}

save_state() {
  mkdir -p "$STATE_DIR" 2>/dev/null
  {
    printf 'NICKNAME=%q\n' "$NICKNAME"
    printf 'EXPOSE_MODE=%q\n' "$EXPOSE_MODE"
    printf 'BIND_ADDR=%q\n' "$BIND_ADDR"
    printf 'BIND_PORT=%q\n' "$BIND_PORT"
    printf 'KCP_PORT=%q\n' "$KCP_PORT"
    printf 'QUIC_PORT=%q\n' "$QUIC_PORT"
    printf 'TOKEN=%q\n' "$TOKEN"
    printf 'TLS_FORCE=%q\n' "$TLS_FORCE"
    printf 'DASH_ENABLE=%q\n' "$DASH_ENABLE"
    printf 'DASH_ADDR=%q\n' "$DASH_ADDR"
    printf 'DASH_PORT=%q\n' "$DASH_PORT"
    printf 'DASH_USER=%q\n' "$DASH_USER"
    printf 'DASH_PASS=%q\n' "$DASH_PASS"
    printf 'VHOST_HTTP=%q\n' "$VHOST_HTTP"
    printf 'VHOST_HTTPS=%q\n' "$VHOST_HTTPS"
    printf 'SUBDOMAIN_HOST=%q\n' "$SUBDOMAIN_HOST"
    printf 'MAX_PORTS=%q\n' "$MAX_PORTS"
    printf 'LOG_LEVEL=%q\n' "$LOG_LEVEL"
    printf 'LOG_TO=%q\n' "$LOG_TO"
    printf 'FRP_VERSION=%q\n' "$FRP_VERSION"
    printf 'PUBLIC_IP=%q\n' "$PUBLIC_IP"
    printf 'PUBLIC_IP6=%q\n' "$PUBLIC_IP6"
    printf 'PUBLIC_ADDR=%q\n' "$PUBLIC_ADDR"
    printf 'V4_STATUS=%q\n' "$V4_STATUS"
    printf 'V6_STATUS=%q\n' "$V6_STATUS"
    printf 'AVAIL_V4=%q\n' "$AVAIL_V4"
    printf 'AVAIL_V6=%q\n' "$AVAIL_V6"
  } > "$STATE_FILE"
}

fetch_ip() {
  local fam="$1" urls u out
  if [ "$fam" = "4" ]; then
    urls=("https://ipv4.icanhazip.com" "https://api.ipify.org" "https://ipv4.ident.me")
  else
    urls=("https://ipv6.icanhazip.com" "https://api6.ipify.org" "https://ipv6.ident.me")
  fi
  for u in "${urls[@]}"; do
    out="$(curl -"$fam" -fsS --connect-timeout 4 --max-time 6 "$u" 2>/dev/null | tr -d '[:space:]')"
    if [ -n "$out" ]; then printf '%s' "$out"; return 0; fi
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

is_cgnat4() {
  case "$1" in
    100.6[4-9].*|100.[7-9][0-9].*|100.1[01][0-9].*|100.12[0-7].*) return 0;;
    *) return 1;;
  esac
}

detect_env() {
  OS_NAME="$(uname -s)"
  ARCH_RAW="$(uname -m)"
  LOCAL_V4=()
  LOCAL_V6=()
  local ip
  while IFS= read -r ip; do [ -n "$ip" ] && LOCAL_V4+=("$ip"); done < <(
    ip -4 addr show scope global 2>/dev/null | awk '/inet /{sub(/\/.*/,"",$2); print $2}'
  )
  while IFS= read -r ip; do [ -n "$ip" ] && LOCAL_V6+=("$ip"); done < <(
    ip -6 addr show scope global 2>/dev/null | awk '/inet6 /{sub(/\/.*/,"",$2); print $2}' | grep -viE '^(fe80|fc|fd)'
  )
  PUBLIC_IP="$(fetch_ip 4 2>/dev/null || true)"
  PUBLIC_IP6="$(fetch_ip 6 2>/dev/null || true)"

  local has_public4=false has_cgnat=false
  for ip in "${LOCAL_V4[@]:-}"; do
    [ -n "$ip" ] || continue
    if ! is_private4 "$ip"; then has_public4=true; fi
    if is_cgnat4 "$ip"; then has_cgnat=true; fi
  done

  if [ "$has_public4" = true ]; then
    V4_STATUS="公网 IPv4"
  elif [ -n "$PUBLIC_IP" ]; then
    if [ "$has_cgnat" = true ]; then V4_STATUS="NAT IPv4 (CGNAT)"; else V4_STATUS="NAT IPv4 (私网)"; fi
  else
    V4_STATUS="无 IPv4"
  fi

  if [ "${#LOCAL_V6[@]}" -gt 0 ]; then
    V6_STATUS="公网 IPv6"
  elif [ -n "$PUBLIC_IP6" ]; then
    V6_STATUS="NAT IPv6"
  else
    V6_STATUS="无 IPv6"
  fi

  if [ -z "$PUBLIC_IP6" ] && [ "${#LOCAL_V6[@]}" -gt 0 ]; then
    PUBLIC_IP6="${LOCAL_V6[0]}"
  fi

  if [ -n "$PUBLIC_IP" ]; then
    PUBLIC_ADDR="$PUBLIC_IP"
  elif [ -n "$PUBLIC_IP6" ]; then
    PUBLIC_ADDR="[$PUBLIC_IP6]"
  else
    PUBLIC_ADDR=""
  fi

  AVAIL_V4=0
  AVAIL_V6=0
  [ -n "$PUBLIC_IP" ] && AVAIL_V4=1
  { [ "${#LOCAL_V6[@]}" -gt 0 ] || [ -n "$PUBLIC_IP6" ]; } && AVAIL_V6=1
}

print_env_report() {
  title "本机环境探测"
  printf '  %-14s %s\n' "系统/架构" "$OS_NAME / $ARCH_RAW"
  printf '  %-14s %s\n' "本地 IPv4" "${LOCAL_V4[*]:-无}"
  printf '  %-14s %s\n' "本地 IPv6" "${LOCAL_V6[*]:-无}"
  printf '  %-14s %s\n' "出口 IPv4" "${PUBLIC_IP:-无}"
  printf '  %-14s %s\n' "出口 IPv6" "${PUBLIC_IP6:-无}"
  hr
  printf '  %-14s %s\n' "IPv4 判定" "$V4_STATUS"
  printf '  %-14s %s\n' "IPv6 判定" "$V6_STATUS"
  printf '  %-14s %s\n' "对外接入地址" "${PUBLIC_ADDR:-未知}"
  if command -v ss >/dev/null 2>&1; then
    local ports
    ports="$(ss -tulnH 2>/dev/null | awk '{print $5}' | sed 's/.*://' | sort -n -u | tr '\n' ' ')"
    printf '  %-14s %s\n' "已监听端口" "${ports:-无}"
  fi
  if [ -z "$PUBLIC_ADDR" ]; then
    warn "未能获取出口公网 IP，可能是网络受限；生成配置时请手动填写对外地址。"
  fi
}

nat_external_for() {
  awk -F'|' -v p="$1" '$2==p{print $1; exit}' "$NAT_FILE" 2>/dev/null
}

nat_internal_exists() {
  awk -F'|' -v p="$1" '$2==p{f=1} END{exit !f}' "$NAT_FILE" 2>/dev/null
}

nat_remove_internal() {
  local internal="$1" tmp
  tmp="$(mktemp)"
  awk -F'|' -v p="$internal" '$2!=p' "$NAT_FILE" > "$tmp" && mv "$tmp" "$NAT_FILE"
}

nat_list() {
  if [ ! -s "$NAT_FILE" ]; then
    warn "暂无已登记的端口映射"
    return 0
  fi
  printf '  %-4s %-10s %-10s %s\n' "序号" "外部端口" "内部端口" "备注"
  hr
  local i=0 line
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    i=$((i + 1))
    printf '  %-4s %-10s %-10s %s\n' "$i" \
      "$(printf '%s' "$line" | cut -d'|' -f1)" \
      "$(printf '%s' "$line" | cut -d'|' -f2)" \
      "$(printf '%s' "$line" | cut -d'|' -f3)"
  done < "$NAT_FILE"
}

nat_add() {
  local ext int label
  info "外部端口请先在服务商网页控制台做好转发，这里只登记对应关系"
  ext="$(ask '外部端口(服务商网页端设置的公网端口)')"
  valid_port "$ext" || { err "外部端口非法"; return 1; }
  int="$(ask '内部端口(本机监听)')"
  valid_port "$int" || { err "内部端口非法"; return 1; }
  if nat_internal_exists "$int"; then
    warn "内部端口 $int 已登记: 外部 $(nat_external_for "$int")"
    confirm "仍要覆盖吗?" || return 1
    nat_remove_internal "$int"
  fi
  label="$(ask '备注/用途' '未命名')"
  printf '%s|%s|%s\n' "$ext" "$int" "$label" >> "$NAT_FILE"
  ok "已登记: 外部 $ext -> 内部 $int ($label)"
}

nat_del() {
  nat_list || return 0
  local n; n="$(ask '要删除的序号(留空取消)')"
  [ -n "$n" ] || return 0
  is_uint "$n" || { err "序号非法"; return 1; }
  local total; total="$(wc -l < "$NAT_FILE")"
  [ "$n" -ge 1 ] && [ "$n" -le "$total" ] || { err "序号超出范围"; return 1; }
  sed -i "${n}d" "$NAT_FILE"
  ok "已删除序号 $n"
}

map_internal_port() {
  local internal="$1" label="$2" ext
  if nat_internal_exists "$internal"; then
    info "内部 $internal ($label) 已登记 -> 外部 $(nat_external_for "$internal")"
    confirm "要修改这个登记吗?" || return 0
  fi
  ext="$(ask "$label 的外部端口(服务商网页端设置的，留空跳过)")"
  [ -n "$ext" ] || return 0
  valid_port "$ext" || { err "端口非法"; return 1; }
  nat_remove_internal "$internal"
  printf '%s|%s|%s\n' "$ext" "$internal" "$label" >> "$NAT_FILE"
  ok "已登记: 外部 $ext -> 内部 $internal ($label)"
}

nat_autofill() {
  local item int label
  local need=("$BIND_PORT:frps控制端口")
  [ "$DASH_ENABLE" = true ] && need+=("$DASH_PORT:frps面板")
  [ -n "$VHOST_HTTP" ] && need+=("$VHOST_HTTP:HTTP建站")
  [ -n "$VHOST_HTTPS" ] && need+=("$VHOST_HTTPS:HTTPS建站")
  local line
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    int="$(printf '%s' "$line" | cut -d'|' -f5)"
    [ -n "$int" ] && need+=("$int:服务 $(printf '%s' "$line" | cut -d'|' -f1)")
  done < "$SVC_FILE"
  for item in "${need[@]}"; do
    int="${item%%:*}"
    label="${item#*:}"
    if nat_internal_exists "$int"; then
      info "内部 $int 已登记为外部 $(nat_external_for "$int") ($label)"
    else
      warn "内部 $int ($label) 尚未登记映射，请确认服务商网页端已放行"
      confirm "现在登记它的外部端口吗?" && map_internal_port "$int" "$label"
    fi
  done
}

svc_unique_name() {
  local base="$1"
  local name="$base"
  local n=1
  while awk -F'|' -v x="$name" '$1==x{f=1} END{exit !f}' "$SVC_FILE" 2>/dev/null; do
    n=$((n + 1)); name="${base}-${n}"
  done
  printf '%s' "$name"
}

svc_append() {
  printf '%s|%s|%s|%s|%s|%s|%s|%s|%s\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$8" "$9" >> "$SVC_FILE"
}

svc_list() {
  if [ ! -s "$SVC_FILE" ]; then
    warn "暂无服务穿透配置"
    return 0
  fi
  printf '  %-4s %-14s %-6s %-16s %-8s %-10s %s\n' "序号" "名称" "类型" "本地" "本地口" "外部口" "备注"
  hr
  local i=0 line name type lip lport rport domains secret mux remark ext
  while IFS='|' read -r name type lip lport rport domains secret mux remark; do
    [ -n "$name" ] || continue
    i=$((i + 1))
    ext=""
    case "$type" in
      tcp|udp) ext="$(nat_external_for "$rport")"; [ -n "$ext" ] || ext="未登记";;
      http|https) ext="${VHOST_HTTP:-未启用}";;
    esac
    printf '  %-4s %-14s %-6s %-16s %-8s %-10s %s\n' "$i" "$name" "$type" "$lip" "$lport" "$ext" "$remark"
  done < "$SVC_FILE"
}

svc_del() {
  svc_list || return 0
  local n; n="$(ask '要删除的序号(留空取消)')"
  [ -n "$n" ] || return 0
  is_uint "$n" || { err "序号非法"; return 1; }
  local total; total="$(wc -l < "$SVC_FILE")"
  [ "$n" -ge 1 ] && [ "$n" -le "$total" ] || { err "序号超出范围"; return 1; }
  sed -i "${n}d" "$SVC_FILE"
  ok "已删除序号 $n"
}

svc_add_port() {
  local type="$1" name lip lport rport remark
  name="$(ask '代理名称' "$(svc_unique_name "$type")")"
  lip="$(ask '本机服务地址' '127.0.0.1')"
  lport="$(ask '本机服务端口')"
  valid_port "$lport" || { err "本地端口非法"; return 1; }
  rport="$(ask 'frps 内部远端端口' "$lport")"
  valid_port "$rport" || { err "远端端口非法"; return 1; }
  if [ "$EXPOSE_MODE" != "v6" ]; then
    if ! nat_internal_exists "$rport"; then
      warn "内部端口 $rport 还没登记映射，请先确认服务商网页端已放行"
      confirm "现在登记它的外部端口吗?" && map_internal_port "$rport" "$name"
    fi
  fi
  remark="$(ask '备注(可留空)' '')"
  svc_append "$name" "$type" "$lip" "$lport" "$rport" "" "" "" "$remark"
  ok "已添加 $type 服务: $name ($lip:$lport -> frps:$rport)"
}

svc_add_vhost() {
  local type="$1" name lip lport domains remark
  name="$(ask '代理名称' "$(svc_unique_name "$type")")"
  lip="$(ask '本机服务地址' '127.0.0.1')"
  lport="$(ask '本机服务端口' "$([ "$type" = http ] && echo 8096 || echo 443)")"
  valid_port "$lport" || { err "本地端口非法"; return 1; }
  domains="$(ask '自定义域名(多个用逗号分隔)')"
  [ -n "$domains" ] || { err "域名不能为空"; return 1; }
  [ -n "$VHOST_HTTP" ] || VHOST_HTTP="$lport"
  remark="$(ask '备注(可留空)' '')"
  svc_append "$name" "$type" "$lip" "$lport" "" "$domains" "" "" "$remark"
  ok "已添加 $type 服务: $name -> $domains"
}

svc_add_secret() {
  local type="$1" name lip lport secret remark
  name="$(ask '代理名称' "$(svc_unique_name "$type")")"
  lip="$(ask '本机服务地址' '127.0.0.1')"
  lport="$(ask '本机服务端口')"
  valid_port "$lport" || { err "本地端口非法"; return 1; }
  secret="$(ask 'secretKey' "$(gen_token)")"
  remark="$(ask '备注(可留空)' '')"
  svc_append "$name" "$type" "$lip" "$lport" "" "" "$secret" "" "$remark"
  ok "已添加 $type 服务: $name"
}

svc_add_tcpmux() {
  local name lip lport domains mux remark
  name="$(ask '代理名称' "$(svc_unique_name tcpmux)")"
  lip="$(ask '本机服务地址' '127.0.0.1')"
  lport="$(ask '本机服务端口')"
  valid_port "$lport" || { err "本地端口非法"; return 1; }
  domains="$(ask 'customDomains(用于路由)')"
  [ -n "$domains" ] || { err "customDomains 不能为空"; return 1; }
  mux="$(ask 'multiplexer' 'httpconnect')"
  remark="$(ask '备注(可留空)' '')"
  svc_append "$name" "tcpmux" "$lip" "$lport" "" "$domains" "" "$mux" "$remark"
  ok "已添加 tcpmux 服务: $name"
}

svc_preset_mc_java() {
  local rport remark
  rport="$(ask 'frps 内部远端端口' '25565')"
  valid_port "$rport" || { err "端口非法"; return 1; }
  [ "$EXPOSE_MODE" != "v6" ] && ! nat_internal_exists "$rport" && confirm "登记它的外部端口吗?" && map_internal_port "$rport" "MC Java"
  remark="$(ask '备注(可留空)' 'Minecraft Java')"
  svc_append "$(svc_unique_name mc-java)" "tcp" "127.0.0.1" "25565" "$rport" "" "" "" "$remark"
  ok "已添加 Minecraft Java (TCP 25565 -> frps:$rport)"
}

svc_preset_mc_bedrock() {
  local rport remark
  rport="$(ask 'frps 内部远端端口' '19132')"
  valid_port "$rport" || { err "端口非法"; return 1; }
  [ "$EXPOSE_MODE" != "v6" ] && ! nat_internal_exists "$rport" && confirm "登记它的外部端口吗?" && map_internal_port "$rport" "MC Bedrock"
  remark="$(ask '备注(可留空)' 'Minecraft Bedrock')"
  svc_append "$(svc_unique_name mc-bedrock)" "udp" "127.0.0.1" "19132" "$rport" "" "" "" "$remark"
  ok "已添加 Minecraft Bedrock (UDP 19132 -> frps:$rport)"
}

svc_preset_emby() {
  local lport domains remark
  lport="$(ask 'Emby 本机端口' '8096')"
  valid_port "$lport" || { err "端口非法"; return 1; }
  domains="$(ask 'Emby 访问域名')"
  [ -n "$domains" ] || { err "域名不能为空"; return 1; }
  [ -n "$VHOST_HTTP" ] || VHOST_HTTP="$lport"
  [ "$EXPOSE_MODE" != "v6" ] && ! nat_internal_exists "$VHOST_HTTP" && confirm "登记建站端口 $VHOST_HTTP 的外部端口吗?" && map_internal_port "$VHOST_HTTP" "HTTP建站"
  remark="$(ask '备注(可留空)' 'Emby')"
  svc_append "$(svc_unique_name emby)" "http" "127.0.0.1" "$lport" "" "$domains" "" "" "$remark"
  ok "已添加 Emby (HTTP $lport -> $domains)"
}

svc_menu() {
  while true; do
    title "添加服务"
    echo "  1) Minecraft Java (TCP 25565)"
    echo "  2) Minecraft Bedrock (UDP 19132)"
    echo "  3) Emby (HTTP 建站)"
    echo "  4) 自定义 TCP"
    echo "  5) 自定义 UDP"
    echo "  6) 自定义 HTTP"
    echo "  7) 自定义 HTTPS"
    echo "  8) STCP 安全隧道"
    echo "  9) XTCP 点对点"
    echo " 10) TCPMUX"
    echo " 11) 查看已添加"
    echo " 12) 删除服务"
    echo "  0) 完成返回"
    local c; c="$(ask '请选择' '0')"
    case "$c" in
      1) svc_preset_mc_java ;;
      2) svc_preset_mc_bedrock ;;
      3) svc_preset_emby ;;
      4) svc_add_port tcp ;;
      5) svc_add_port udp ;;
      6) svc_add_vhost http ;;
      7) svc_add_vhost https ;;
      8) svc_add_secret stcp ;;
      9) svc_add_secret xtcp ;;
      10) svc_add_tcpmux ;;
      11) svc_list ;;
      12) svc_del ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
  done
}

collect_allow_ports() {
  {
    awk -F'|' '$2!=""{print $2}' "$NAT_FILE" 2>/dev/null
    awk -F'|' '$5!=""{print $5}' "$SVC_FILE" 2>/dev/null
  } | grep -E '^[0-9]+$' | sort -n -u
}

gen_frps() {
  echo "bindAddr = \"$BIND_ADDR\""
  echo "bindPort = $BIND_PORT"
  [ -n "$KCP_PORT" ] && echo "kcpBindPort = $KCP_PORT"
  [ -n "$QUIC_PORT" ] && echo "quicBindPort = $QUIC_PORT"
  echo "transport.tls.force = $TLS_FORCE"
  echo "auth.method = \"token\""
  [ -n "$TOKEN" ] && echo "auth.token = \"$TOKEN\""
  if [ "$DASH_ENABLE" = true ]; then
    echo "webServer.addr = \"$DASH_ADDR\""
    echo "webServer.port = $DASH_PORT"
    [ -n "$DASH_USER" ] && echo "webServer.user = \"$DASH_USER\""
    [ -n "$DASH_PASS" ] && echo "webServer.password = \"$DASH_PASS\""
  fi
  [ -n "$VHOST_HTTP" ] && echo "vhostHTTPPort = $VHOST_HTTP"
  [ -n "$VHOST_HTTPS" ] && echo "vhostHTTPSPort = $VHOST_HTTPS"
  [ -n "$SUBDOMAIN_HOST" ] && echo "subDomainHost = \"$SUBDOMAIN_HOST\""
  echo "maxPortsPerClient = $MAX_PORTS"
  local ports; ports="$(collect_allow_ports)"
  if [ -n "$ports" ]; then
    echo "allowPorts = ["
    local p
    for p in $ports; do echo "  { single = $p },"; done
    echo "]"
  fi
  echo "udpPacketSize = 1500"
  echo "log.to = \"$LOG_TO\""
  echo "log.level = \"$LOG_LEVEL\""
}

domains_to_toml() {
  local d="$1" out="" x
  IFS=',' read -ra arr <<< "$d"
  for x in "${arr[@]}"; do
    x="${x// /}"
    [ -n "$x" ] && out="${out}\"${x}\", "
  done
  printf '[%s]' "${out%, }"
}

gen_frpc() {
  local fam="${1:-4}" server_addr server_port
  if [ "$fam" = "6" ]; then
    server_addr="$PUBLIC_IP6"
    server_port="$BIND_PORT"
  else
    server_addr="$PUBLIC_IP"
    server_port="$(nat_external_for "$BIND_PORT")"
    [ -n "$server_port" ] || server_port="$BIND_PORT"
  fi
  [ -n "$server_addr" ] || server_addr="你的frps公网地址"
  echo "serverAddr = \"$server_addr\""
  echo "serverPort = $server_port"
  echo "auth.method = \"token\""
  [ -n "$TOKEN" ] && echo "auth.token = \"$TOKEN\""
  echo "transport.tls.enable = true"
  echo
  local name type lip lport rport domains secret mux remark
  while IFS='|' read -r name type lip lport rport domains secret mux remark; do
    [ -n "$name" ] || continue
    [ -n "$remark" ] && echo "# $remark"
    echo "[[proxies]]"
    echo "name = \"$name\""
    echo "type = \"$type\""
    case "$type" in
      tcp|udp)
        echo "localIP = \"$lip\""
        echo "localPort = $lport"
        echo "remotePort = $rport"
        ;;
      http|https)
        echo "localIP = \"$lip\""
        echo "localPort = $lport"
        [ -n "$domains" ] && echo "customDomains = $(domains_to_toml "$domains")"
        ;;
      stcp|xtcp)
        echo "secretKey = \"$secret\""
        echo "localIP = \"$lip\""
        echo "localPort = $lport"
        ;;
      tcpmux)
        echo "multiplexer = \"$mux\""
        echo "localIP = \"$lip\""
        echo "localPort = $lport"
        [ -n "$domains" ] && echo "customDomains = $(domains_to_toml "$domains")"
        ;;
    esac
    echo
  done < "$SVC_FILE"
}

print_block() {
  local name="$1" content="$2"
  title "$name (可直接复制)"
  hr
  printf '%s\n' "$content"
  hr
}

save_frps_config() {
  mkdir -p "$STATE_DIR" 2>/dev/null
  gen_frps > "$FRPS_CONF"
  ok "已写入 $FRPS_CONF"
}

save_client_configs() {
  mkdir -p "$CLIENT_DIR" 2>/dev/null
  case "$EXPOSE_MODE" in
    v4) gen_frpc 4 > "$CLIENT_DIR/frpc-v4.toml"; ok "已写入 $CLIENT_DIR/frpc-v4.toml" ;;
    v6) gen_frpc 6 > "$CLIENT_DIR/frpc-v6.toml"; ok "已写入 $CLIENT_DIR/frpc-v6.toml" ;;
    both)
      gen_frpc 4 > "$CLIENT_DIR/frpc-v4.toml"
      gen_frpc 6 > "$CLIENT_DIR/frpc-v6.toml"
      ok "已写入 $CLIENT_DIR/frpc-v4.toml 与 frpc-v6.toml"
      ;;
  esac
}

port_listening() {
  ss -tlnH 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$1$"
}

http_code() { curl -s -o /dev/null -w '%{http_code}' --max-time 4 "$1" 2>/dev/null; }

run_selftest() {
  title "自检"
  local pass=0 fail=0 code
  if pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1; then
    ok "frps 进程运行中"; pass=$((pass + 1))
  else
    err "frps 未运行"; fail=$((fail + 1))
  fi
  if port_listening "$BIND_PORT"; then
    ok "控制端口 $BIND_PORT 正在监听"; pass=$((pass + 1))
  else
    err "控制端口 $BIND_PORT 未监听"; fail=$((fail + 1))
  fi
  if [ "$DASH_ENABLE" = true ]; then
    code="$(http_code "http://127.0.0.1:$DASH_PORT")"
    if [ -n "$code" ] && [ "$code" != "000" ]; then
      ok "面板可访问 (HTTP $code)"; pass=$((pass + 1))
    else
      warn "面板未响应"; fail=$((fail + 1))
    fi
  fi
  hr
  if [ "$fail" = 0 ]; then ok "自检通过 ($pass 项)"; else warn "自检完成: $pass 项通过, $fail 项异常"; fi
}

show_status() {
  title "服务端状态"
  printf '  %-12s %s\n' "昵称" "$NICKNAME"
  printf '  %-12s %s\n' "对外方式" "$EXPOSE_MODE"
  if pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1; then
    ok "frps 运行中 (PID $(pgrep -f '/usr/local/bin/frps' | head -1))"
  else
    warn "frps 未运行"
  fi
  if port_listening "$BIND_PORT"; then ok "控制端口 $BIND_PORT 监听中"; else warn "控制端口 $BIND_PORT 未监听"; fi
  list_services
}

list_services() {
  title "服务列表"
  if [ ! -s "$SVC_FILE" ]; then
    warn "暂无服务"
    return 0
  fi
  printf '  %-4s %-14s %-6s %-24s %-10s %s\n' "序号" "名称" "类型" "用户访问地址" "状态" "备注"
  hr
  local i=0 name type lip lport rport domains secret mux remark access state ext
  while IFS='|' read -r name type lip lport rport domains secret mux remark; do
    [ -n "$name" ] || continue
    i=$((i + 1))
    case "$type" in
      tcp|udp)
        ext="$(nat_external_for "$rport")"
        if [ "$EXPOSE_MODE" = "v6" ]; then
          access="[${PUBLIC_IP6:-?}]:$rport"; state="直连"
        elif [ -n "$ext" ]; then
          access="${PUBLIC_IP:-?}:$ext"; state="已登记"
        else
          access="${PUBLIC_IP:-?}:$rport"; state="未登记"
        fi
        ;;
      http|https)
        access="$domains"; state="建站:$VHOST_HTTP"
        ;;
      *)
        access="-"; state="隧道"
        ;;
    esac
    printf '  %-4s %-14s %-6s %-24s %-10s %s\n' "$i" "$name" "$type" "$access" "$state" "$remark"
  done < "$SVC_FILE"
}

arch_name() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64;;
    aarch64|arm64) echo arm64;;
    armv7l|armv7) echo arm;;
    i386|i686) echo 386;;
    *) echo "";;
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
      printf '%s' "$dir"
      return 0
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
  if [ -z "$ver" ]; then
    info "查询最新 frp 版本..."
    ver="$(latest_frp_version)"
  fi
  [ -n "$ver" ] || ver="$(ask '请输入 frp 版本号(如 0.61.1)')"
  [ -n "$ver" ] || die "未指定 frp 版本"
  FRP_VERSION="$ver"
  save_state

  local dir; dir="$(download_frp "$ver" "$arch")" || die "frp 下载失败，请检查网络或手动安装"
  install -m 0755 "$dir/frps" /usr/local/bin/frps || die "安装 frps 失败"
  rm -rf "$(dirname "$dir")"
  ok "frps 已安装到 /usr/local/bin/frps (v$ver)"

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
    if systemctl is-active --quiet frps; then
      ok "frps 已通过 systemd 启动"
    else
      err "frps 启动失败，请查看: journalctl -u frps -e"
    fi
  else
    warn "未检测到 systemd，使用 nohup 启动"
    pkill -f '/usr/local/bin/frps' 2>/dev/null || true
    nohup /usr/local/bin/frps -c "$FRPS_CONF" >"$STATE_DIR/frps.log" 2>&1 &
    sleep 1
    if pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1; then
      ok "frps 已在后台启动，日志: $STATE_DIR/frps.log"
    else
      err "frps 启动失败"
    fi
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
      systemctl stop frps 2>/dev/null
      systemctl disable frps 2>/dev/null
      rm -f /etc/systemd/system/frps.service
      systemctl daemon-reload
      rm -f /usr/local/bin/frps
      ok "已卸载 frps 服务与二进制(配置保留在 $STATE_DIR)"
      ;;
  esac
}

self_install() {
  require_root
  local target="/usr/local/bin/$APP" src=""
  if [ -f "$0" ]; then
    src="$0"
  elif [ -n "$SELF_URL" ]; then
    info "从 $SELF_URL 下载脚本..."
    local tmp; tmp="$(mktemp)"
    curl -fsSL --connect-timeout 10 --max-time 60 "$SELF_URL" -o "$tmp" || die "下载失败，请检查 SELF_URL"
    src="$tmp"
  else
    die "无法获取脚本来源，请先保存脚本后执行: bash autof.sh self-install"
  fi
  if [ "$(readlink -f "$src")" = "$target" ]; then
    ok "已经安装在 $target"
    return 0
  fi
  install -m 0755 "$src" "$target" || die "安装失败"
  ok "已安装为命令: $target"
  info "以后直接运行: $APP"
}

self_update() {
  require_root
  [ -n "$SELF_URL" ] || die "未设置 SELF_URL"
  local target="/usr/local/bin/$APP" tmp
  tmp="$(mktemp)"
  info "更新中: $SELF_URL"
  curl -fsSL --connect-timeout 10 --max-time 60 "$SELF_URL" -o "$tmp" || die "下载失败"
  bash -n "$tmp" || die "下载的脚本语法校验失败，已放弃更新"
  install -m 0755 "$tmp" "$target" || die "写入失败"
  rm -f "$tmp"
  ok "已更新到最新版本: $target"
}

maybe_install_prompt() {
  local target="/usr/local/bin/$APP"
  [ "$(id -u)" = "0" ] || return 0
  if [ -f "$0" ] && [ "$(readlink -f "$0")" = "$target" ]; then return 0; fi
  if [ ! -f "$0" ] && [ -z "$SELF_URL" ]; then return 0; fi
  if confirm "是否把本脚本安装为命令 $APP (方便以后直接运行)?"; then
    self_install
  fi
}

wizard_expose() {
  step "第 1 步 / 选择对外方式"
  if [ "$AVAIL_V4" = 1 ] && [ "$AVAIL_V6" = 1 ]; then
    echo "  检测到 IPv4 与 IPv6 都可用:"
    echo "    1) 仅 IPv4 (走 NAT 端口映射)"
    echo "    2) 仅 IPv6 (公网直连，无需映射)"
    echo "    3) 两者都要"
    local c; c="$(ask '请选择' '1')"
    case "$c" in 2) EXPOSE_MODE="v6";; 3) EXPOSE_MODE="both";; *) EXPOSE_MODE="v4";; esac
  elif [ "$AVAIL_V4" = 1 ]; then
    EXPOSE_MODE="v4"; info "仅检测到 IPv4，使用 NAT 端口映射模式"
  elif [ "$AVAIL_V6" = 1 ]; then
    EXPOSE_MODE="v6"; info "仅检测到 IPv6，使用公网直连模式"
  else
    EXPOSE_MODE="v4"; warn "未检测到可用公网出口，按 IPv4 模式配置，稍后请手动填写地址"
  fi
  case "$EXPOSE_MODE" in
    v4) BIND_ADDR="0.0.0.0";;
    v6|both) BIND_ADDR="::";;
  esac
  save_state
}

wizard_ports() {
  step "第 2 步 / 端口设置"
  local v
  v="$(ask 'frps 控制端口 bindPort(内部)' "$BIND_PORT")"
  valid_port "$v" && BIND_PORT="$v"
  if [ "$EXPOSE_MODE" = "v6" ]; then
    info "IPv6 直连无需端口映射，客户端直接用 [公网IPv6]:$BIND_PORT"
  else
    warn "端口映射由你在服务商网页控制台设置，本脚本只做登记"
    map_internal_port "$BIND_PORT" "frps控制端口"
  fi
  save_state
}

wizard_auth() {
  step "第 3 步 / 认证"
  local t
  t="$(ask 'auth token(留空自动生成)' "$TOKEN")"
  [ -n "$t" ] || t="$(gen_token)"
  TOKEN="$t"
  ok "token: $TOKEN"
  if confirm "强制 TLS 加密(推荐)?"; then TLS_FORCE=true; else TLS_FORCE=false; fi
  save_state
}

wizard_serverinfo() {
  step "第 4 步 / 服务端信息"
  NICKNAME="$(ask '给这台服务端起个昵称' "$NICKNAME")"
  if confirm "启用 Dashboard 面板?"; then
    DASH_ENABLE=true
    DASH_PORT="$(ask '面板端口(内部)' "$DASH_PORT")"
    DASH_USER="$(ask '面板用户名' "$DASH_USER")"
    DASH_PASS="$(ask '面板密码' "${DASH_PASS:-$(gen_token)}")"
    [ "$EXPOSE_MODE" != "v6" ] && map_internal_port "$DASH_PORT" "面板"
  else
    DASH_ENABLE=false
  fi
  if confirm "启用 HTTP 建站(Emby 等域名访问)?"; then
    VHOST_HTTP="$(ask 'vhostHTTPPort(内部)' "${VHOST_HTTP:-8096}")"
    SUBDOMAIN_HOST="$(ask '主域名(留空不使用子域名)' "$SUBDOMAIN_HOST")"
    [ "$EXPOSE_MODE" != "v6" ] && map_internal_port "$VHOST_HTTP" "HTTP建站"
  fi
  save_state
}

wizard_services() {
  step "第 5 步 / 添加要穿透的服务"
  info "按提示添加服务，完成后选 0 返回"
  svc_menu
  if [ ! -s "$SVC_FILE" ]; then warn "还没有添加任何服务"; fi
  save_state
}

wizard_review() {
  step "第 6 步 / 确认配置"
  printf '  %-12s %s\n' "昵称" "$NICKNAME"
  printf '  %-12s %s\n' "对外方式" "$EXPOSE_MODE"
  printf '  %-12s %s\n' "控制端口" "$BIND_PORT"
  printf '  %-12s %s\n' "token" "$TOKEN"
  printf '  %-12s %s\n' "面板" "$([ "$DASH_ENABLE" = true ] && echo "开启($DASH_PORT)" || echo 关闭)"
  hr
  nat_list
  svc_list
  hr
  confirm "确认以上配置正确吗?"
}

wizard_generate() {
  step "第 7 步 / 生成配置"
  print_block "frps.toml" "$(gen_frps)"
  case "$EXPOSE_MODE" in
    v4) print_block "frpc.toml (IPv4 客户端)" "$(gen_frpc 4)" ;;
    v6) print_block "frpc.toml (IPv6 客户端)" "$(gen_frpc 6)" ;;
    both)
      print_block "frpc.toml (IPv4 客户端)" "$(gen_frpc 4)"
      print_block "frpc.toml (IPv6 客户端)" "$(gen_frpc 6)"
      ;;
  esac
  save_frps_config
  save_client_configs
}

wizard() {
  load_state
  title "Autofrp 配置向导"
  detect_env
  save_state
  print_env_report

  if [ -s "$SVC_FILE" ]; then
    if confirm "检测到已有服务配置，是否清空重新开始?"; then
      : > "$SVC_FILE"
      : > "$NAT_FILE"
      ok "已清空旧配置"
    fi
  fi

  wizard_expose
  wizard_ports
  wizard_auth
  wizard_serverinfo
  wizard_services

  if ! wizard_review; then
    warn "已取消，配置未保存"
    return 1
  fi
  save_state

  wizard_generate

  if confirm "现在安装并启动 frps 服务端吗?"; then
    install_frps
    run_selftest
  fi

  show_status
  echo
  ok "配置完成。以后输入 autof 可进入管理菜单。"
  maybe_install_prompt
}

manage_menu() {
  load_state
  detect_env
  save_state
  print_env_report
  while true; do
    title "autof 管理菜单"
    echo "  1) 重新探测环境"
    echo "  2) 查看服务列表/状态"
    echo "  3) 添加服务"
    echo "  4) 登记 NAT 端口映射"
    echo "  5) 修改基础设置(端口/token/面板/昵称)"
    echo "  6) 高级设置(KCP/QUIC/vhost/日志)"
    echo "  7) 生成并预览配置"
    echo "  8) 保存配置到磁盘"
    echo "  9) 安装/更新 frps 并启动"
    echo " 10) 自检"
    echo " 11) 重启/停止/日志"
    echo " 12) 重新运行配置向导"
    echo " 13) 将本脚本安装为 autof 命令"
    echo "  0) 退出"
    local c; c="$(ask '请选择' '0')"
    case "$c" in
      1) detect_env; save_state; print_env_report ;;
      2) show_status ;;
      3) svc_menu; save_state ;;
      4) nat_menu ;;
      5) basic_menu ;;
      6) advanced_menu ;;
      7) print_block "frps.toml" "$(gen_frps)"; [ "$EXPOSE_MODE" = "v6" ] && print_block "frpc.toml (v6)" "$(gen_frpc 6)" || print_block "frpc.toml (v4)" "$(gen_frpc 4)" ;;
      8) save_frps_config; save_client_configs ;;
      9) install_frps ;;
      10) run_selftest ;;
      11) service_menu ;;
      12) wizard ;;
      13) self_install ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
  done
}

basic_menu() {
  local v
  title "基础设置"
  NICKNAME="$(ask '昵称' "$NICKNAME")"
  v="$(ask 'bindPort 控制端口(内部)' "$BIND_PORT")"; valid_port "$v" && BIND_PORT="$v"
  v="$(ask 'auth token' "$TOKEN")"; TOKEN="$v"
  v="$(ask '强制 TLS (true/false)' "$TLS_FORCE")"; case "$v" in true|false) TLS_FORCE="$v";; esac
  if confirm "启用 Dashboard 面板? (当前: $DASH_ENABLE)"; then
    DASH_ENABLE=true
    DASH_PORT="$(ask '面板端口(内部)' "$DASH_PORT")"
    DASH_USER="$(ask '面板用户名' "$DASH_USER")"
    DASH_PASS="$(ask '面板密码' "$DASH_PASS")"
  else
    DASH_ENABLE=false
  fi
  save_state
  ok "基础设置已保存"
}

advanced_menu() {
  local v
  title "高级设置"
  v="$(ask 'KCP 端口(留空关闭)' "$KCP_PORT")"; KCP_PORT="$v"
  v="$(ask 'QUIC 端口(留空关闭)' "$QUIC_PORT")"; QUIC_PORT="$v"
  v="$(ask 'vhostHTTPPort(留空关闭建站)' "$VHOST_HTTP")"; VHOST_HTTP="$v"
  v="$(ask 'vhostHTTPSPort(留空关闭)' "$VHOST_HTTPS")"; VHOST_HTTPS="$v"
  v="$(ask 'subDomainHost(留空不使用子域名)' "$SUBDOMAIN_HOST")"; SUBDOMAIN_HOST="$v"
  v="$(ask 'maxPortsPerClient(0为不限)' "$MAX_PORTS")"; is_uint "$v" && MAX_PORTS="$v"
  v="$(ask '日志级别(trace/debug/info/warn/error)' "$LOG_LEVEL")"; LOG_LEVEL="$v"
  save_state
  ok "高级设置已保存"
}

nat_menu() {
  while true; do
    title "NAT 端口映射登记 (先在服务商网页端设置好，再填这里)"
    echo "  1) 登记一条映射 (外部 -> 内部)"
    echo "  2) 查看已登记映射"
    echo "  3) 删除登记"
    echo "  4) 检查 frps 端口是否都已登记"
    echo "  0) 返回"
    local c; c="$(ask '请选择' '0')"
    case "$c" in
      1) nat_add ;;
      2) nat_list ;;
      3) nat_del ;;
      4) nat_autofill ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
  done
}

service_menu() {
  while true; do
    title "服务端控制"
    echo "  1) 查看状态"
    echo "  2) 重启"
    echo "  3) 停止"
    echo "  4) 查看日志"
    echo "  5) 卸载服务端"
    echo "  0) 返回"
    local c; c="$(ask '请选择' '0')"
    case "$c" in
      1) service_ctl status ;;
      2) service_ctl restart ;;
      3) service_ctl stop ;;
      4) service_ctl logs ;;
      5) service_ctl uninstall ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
  done
}

usage() {
  cat <<EOF
$APP - NAT 小鸡 frps 一键工具

用法:
  $APP                 配置向导(推荐，首次使用)
  $APP menu            管理菜单
  $APP detect          仅探测本机网络环境
  $APP list            查看服务列表与状态
  $APP status          查看服务端状态
  $APP nat             登记 NAT 端口映射
  $APP gen frps        打印 frps.toml
  $APP gen frpc        打印 frpc.toml(客户端)
  $APP save            保存 frps.toml 与 frpc.toml
  $APP install         下载安装 frps 并注册 systemd
  $APP status|restart|stop|logs|uninstall
  $APP self-install    安装为 /usr/local/bin/$APP
  $APP self-update     从 SELF_URL 拉取并更新自身
  $APP help            显示本帮助

配置目录: $STATE_DIR
EOF
}

main() {
  case "${1:-}" in
    "") wizard ;;
    wizard) wizard ;;
    menu|manage) manage_menu ;;
    detect) load_state; detect_env; save_state; print_env_report ;;
    list) load_state; list_services ;;
    status) load_state; show_status ;;
    nat) load_state; nat_menu ;;
    gen)
      load_state
      case "${2:-frps}" in
        frps) gen_frps ;;
        frpc) gen_frpc "${3:-4}" ;;
        *) die "未知类型: ${2:-}" ;;
      esac
      ;;
    save) load_state; save_frps_config; save_client_configs ;;
    install) install_frps ;;
    restart|stop|logs|uninstall) service_ctl "$1" ;;
    self-install) self_install ;;
    self-update) self_update ;;
    help|-h|--help) usage ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
