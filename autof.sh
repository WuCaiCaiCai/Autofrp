#!/usr/bin/env bash
# Autofrp - NAT VPS frps 一键配置脚本
# Copyright (c) 2026 Wu Cai (WuCaiCaiCai)
# SPDX-License-Identifier: MIT
# 非官方项目，与 fatedier/frp 无隶属关系；frp 版权归其作者所有，遵循 Apache-2.0。
set -u

APP="autof"
VERSION="1.1.0"
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

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'
  C_BLUE=$'\033[34m'; C_MAGENTA=$'\033[35m'; C_CYAN=$'\033[36m'
  C_BOLD=$'\033[1m'; C_DIM=$'\033[2m'; C_RST=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""; C_MAGENTA=""
  C_CYAN=""; C_BOLD=""; C_DIM=""; C_RST=""
fi

info()  { printf '%s[*]%s %s\n' "$C_CYAN" "$C_RST" "$*" >&2; }
ok()    { printf '%s[+]%s %s\n' "$C_GREEN" "$C_RST" "$*" >&2; }
warn()  { printf '%s[!]%s %s\n' "$C_YELLOW" "$C_RST" "$*" >&2; }
err()   { printf '%s[x]%s %s\n' "$C_RED" "$C_RST" "$*" >&2; }
title() { printf '\n%s▌%s %s%s\n' "$C_CYAN" "$C_BOLD" "$*" "$C_RST" >&2; }
hr()    { printf '%s%s%s\n' "$C_DIM" "$RULE_LIGHT" "$C_RST" >&2; }
die()   { err "$*"; exit 1; }

menu_item() { printf '  %s%s）%s %s\n' "$C_CYAN$C_BOLD" "$1" "$C_RST" "$2"; }

require_root() { [ "$(id -u)" = "0" ] || die "该操作需要 root 权限，请用 sudo 运行"; }

ask() {
  local prompt="$1" def="${2-}" ans=""
  if [ -n "$def" ]; then
    read -rp "${C_CYAN}${prompt}${C_RST} [${C_DIM}${def}${C_RST}]: " ans || true
  else
    read -rp "${C_CYAN}${prompt}${C_RST}: " ans || true
  fi
  printf '%s' "${ans:-$def}"
}

confirm() {
  local ans=""
  read -rp "${C_CYAN}$1${C_RST} [y/N]: " ans || true
  case "$ans" in y|Y|yes|YES) return 0;; *) return 1;; esac
}

confirm_yes() {
  local ans=""
  read -rp "${C_CYAN}$1${C_RST} [Y/n]: " ans || true
  case "$ans" in n|N|no|NO) return 1;; *) return 0;; esac
}

is_uint() { case "$1" in ''|*[!0-9]*) return 1;; *) return 0;; esac; }
valid_port() { is_uint "$1" && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }
gen_token() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 24; }
pause_key() { [ -t 0 ] || return 0; read -rp "${C_DIM}按回车继续...${C_RST}" _ || true; }

load_state() {
  CONTROL_PORT=""
  TOKEN=""
  FRP_VERSION=""
  PUBLIC_IP=""
  PUBLIC_IP6=""
  V4_STATUS=""
  V6_STATUS=""
  IP_PREF=""
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
    printf 'IP_PREF=%q\n' "$IP_PREF"
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
  if [ -z "$IP_PREF" ]; then
    if [ -n "$PUBLIC_IP" ]; then IP_PREF=4
    elif [ -n "$PUBLIC_IP6" ]; then IP_PREF=6
    else IP_PREF=both; fi
  fi
}

public_addr() {
  case "$IP_PREF" in
    6) if [ -n "$PUBLIC_IP6" ]; then printf '%s' "$PUBLIC_IP6"; else printf '%s' "$PUBLIC_IP"; fi;;
    *) if [ -n "$PUBLIC_IP" ]; then printf '%s' "$PUBLIC_IP"; else printf '%s' "$PUBLIC_IP6"; fi;;
  esac
}

public_addr_alt() {
  [ "$IP_PREF" = "both" ] || return 0
  [ -n "$PUBLIC_IP" ] && [ -n "$PUBLIC_IP6" ] && printf '%s' "$PUBLIC_IP6"
}

net_pref_label() {
  case "$IP_PREF" in
    4) printf '仅 IPv4';; 6) printf '仅 IPv6';; both) printf '均可';; *) printf '未设置';;
  esac
}

RULE_HEAVY="$(printf '═%.0s' {1..60})"
RULE_LIGHT="$(printf '─%.0s' {1..60})"

draw_header() {
  local st v4mark="" v6mark="" pid="" sname stype slport spublic sremark
  if pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1; then
    st="${C_GREEN}● 运行中${C_RST}"
    pid=" ${C_DIM}（PID $(pgrep -f '/usr/local/bin/frps' | head -1)）${C_RST}"
  else
    st="${C_RED}● 已停止${C_RST}"
  fi
  case "$IP_PREF" in
    6) v6mark=" ${C_GREEN}◀ 首选${C_RST}";;
    both) v4mark=" ${C_GREEN}◀ 首选${C_RST}"; v6mark=" ${C_DIM}（备选）${C_RST}";;
    *) v4mark=" ${C_GREEN}◀ 首选${C_RST}";;
  esac
  printf '%s\n' "${C_CYAN}${RULE_HEAVY}${C_RST}"
  printf '  %sAutofrp%s  ·  frps 服务端  %sv%s%s\n' "$C_BOLD" "$C_RST" "$C_DIM" "$VERSION" "$C_RST"
  printf '%s\n' "${C_CYAN}${RULE_HEAVY}${C_RST}"
  printf '  %sIPv4%s  %s  %s%s\n' "$C_CYAN" "$C_RST" "${V4_STATUS:-未探测}" "${PUBLIC_IP:-}" "$v4mark"
  printf '  %sIPv6%s  %s  %s%s\n' "$C_CYAN" "$C_RST" "${V6_STATUS:-未探测}" "${PUBLIC_IP6:-}" "$v6mark"
  printf '  %s状态%s  %s%s\n' "$C_CYAN" "$C_RST" "$st" "$pid"
  printf '  %s控制端口%s %s    %s网络偏好%s %s\n' "$C_CYAN" "$C_RST" "${CONTROL_PORT:-未设置}" "$C_CYAN" "$C_RST" "$(net_pref_label)"
  if [ -s "$SVC_FILE" ]; then
    printf '  %s── 穿透服务 ──%s\n' "$C_DIM" "$C_RST"
    while IFS='|' read -r sname stype slport spublic sremark; do
      [ -n "$sname" ] || continue
      printf '  %-14s %s%-4s%s %s → %s\n' "$sname" "$C_CYAN" "$stype" "$C_RST" "$slport" "$spublic"
    done < "$SVC_FILE"
  fi
  printf '%s\n' "${C_CYAN}${RULE_HEAVY}${C_RST}"
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

svc_read() {
  local line
  line="$(sed -n "${1}p" "$SVC_FILE")"
  IFS='|' read -r S_NAME S_TYPE S_LPORT S_PUBLIC S_REMARK <<< "$line"
  [ -n "$S_NAME" ]
}

svc_list() {
  if [ ! -s "$SVC_FILE" ]; then warn "暂无服务，请在「管理服务」中按 a 添加"; return 0; fi
  printf '  %s%-4s %-14s %-5s %-9s %-9s %s%s\n' "$C_BOLD" "序号" "名称" "类型" "本机端口" "对外端口" "备注" "$C_RST"
  hr
  local i=0 name type lport public remark tcolor
  while IFS='|' read -r name type lport public remark; do
    [ -n "$name" ] || continue
    i=$((i + 1))
    case "$type" in tcp) tcolor="$C_GREEN";; udp) tcolor="$C_YELLOW";; *) tcolor="";; esac
    printf '  %-4s %-14s %s%-5s%s %-9s %-9s %s\n' "$i" "$name" "$tcolor" "$type" "$C_RST" "$lport" "$public" "$remark"
  done < "$SVC_FILE"
}

svc_pick() {
  [ -s "$SVC_FILE" ] || { warn "暂无服务，请在「管理服务」中按 a 添加"; return 1; }
  svc_list >&2
  local n; n="$(ask '输入序号（留空取消）')"
  [ -n "$n" ] || return 1
  is_uint "$n" || { err "序号非法"; return 1; }
  [ "$n" -ge 1 ] && [ "$n" -le "$(svc_count)" ] || { err "序号超出范围"; return 1; }
  printf '%s' "$n"
}

add_service() {
  local type="$1" def_lport="$2" name="$3" ask_name="${4:-}"
  local lport public
  [ "$ask_name" = "yes" ] && name="$(ask '名称' "$(svc_unique_name "$name")")"
  printf '  本机端口：服务实际监听的端口（如 MC 的 25565）\n' >&2
  lport="$(ask '本机端口' "$def_lport")"
  valid_port "$lport" || { err "端口非法"; return 1; }
  printf '  对外端口：玩家连接用的公网端口（服务商网页端请映射 对外→对外）\n' >&2
  public="$(ask '对外端口' "$lport")"
  valid_port "$public" || { err "端口非法"; return 1; }
  svc_append "$name" "$type" "$lport" "$public" ""
  ok "已添加服务: $name"
  printf '  → 玩家连接: %s:%s\n' "$(public_addr)" "$public" >&2
}

svc_detail() {
  local n="$1"
  svc_read "$n" || { err "读取服务失败"; return 1; }
  title "服务: $S_NAME"
  printf '  %-10s %s\n' "类型" "$S_TYPE"
  printf '  %-10s 127.0.0.1:%s\n' "本机" "$S_LPORT"
  printf '  %-10s %s\n' "对外端口" "$S_PUBLIC"
  printf '  %-10s %s\n' "玩家连接" "$(public_addr):$S_PUBLIC"
  [ -n "$S_REMARK" ] && printf '  %-10s %s\n' "备注" "$S_REMARK"
  printf '\n'
  print_block "该服务 frpc 片段" "$(gen_frpc "$n")"
  printf '  1）编辑   2）删除   0）返回\n'
  local c; c="$(ask '请选择' '0')"
  case "$c" in
    1) svc_edit "$n" ;;
    2) svc_del "$n" ;;
  esac
}

svc_manage() {
  while true; do
    [ -t 1 ] && clear
    draw_header
    title "管理服务"
    if [ -s "$SVC_FILE" ]; then
      svc_list >&2
    else
      printf '  （暂无服务）\n' >&2
    fi
    printf '\n'
    printf '  输入序号=查看/编辑/删除    a=添加服务    回车=返回\n' >&2
    local c; c="$(ask '请选择')"
    printf '\n'
    case "$c" in
      "") return 0 ;;
      a|A) add_menu ;;
      *[!0-9]*) warn "无效选项"; pause_key ;;
      *)
        [ "$c" -ge 1 ] && [ "$c" -le "$(svc_count)" ] || { warn "序号超出范围"; pause_key; continue; }
        svc_detail "$c"
        ;;
    esac
  done
}

svc_edit() {
  local n="${1:-}"
  if [ -z "$n" ]; then n="$(svc_pick)" || return 0; fi
  is_uint "$n" || { err "序号非法"; return 1; }
  [ "$n" -ge 1 ] && [ "$n" -le "$(svc_count)" ] || { err "序号超出范围"; return 1; }
  svc_read "$n" || { err "读取服务失败"; return 1; }
  local name="$S_NAME" type="$S_TYPE" lport="$S_LPORT" public="$S_PUBLIC" remark="$S_REMARK" v
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
  if [ -z "$n" ]; then n="$(svc_pick)" || return 0; fi
  is_uint "$n" || { err "序号非法"; return 1; }
  [ "$n" -ge 1 ] && [ "$n" -le "$(svc_count)" ] || { err "序号超出范围"; return 1; }
  sed -i "${n}d" "$SVC_FILE"
  ok "已删除序号 $n"
  apply_config
}

add_menu() {
  while true; do
    title "添加穿透服务"
    menu_item 1 'Minecraft Java     （TCP，本机 25565）'
    menu_item 2 'Minecraft Bedrock  （UDP，本机 19132）'
    menu_item 3 '自定义 TCP'
    menu_item 4 '自定义 UDP'
    menu_item 0 '返回'
    local c; c="$(ask '请选择' '0')"
    printf '\n'
    case "$c" in
      1) add_service tcp 25565 mc-java ;;
      2) add_service udp 19132 mc-bedrock ;;
      3) add_service tcp "" tcp yes ;;
      4) add_service udp "" udp yes ;;
      0) return 0 ;;
      *) warn "无效选项" ;;
    esac
    apply_config
    pause_key
  done
}

gen_frpc_proxy() {
  svc_read "$1" || return 1
  [ -n "$S_REMARK" ] && echo "# $S_REMARK"
  echo "# 玩家连接: $(public_addr):$S_PUBLIC"
  echo "[[proxies]]"
  echo "name = \"$S_NAME\""
  echo "type = \"$S_TYPE\""
  echo "localIP = \"127.0.0.1\""
  echo "localPort = $S_LPORT"
  echo "remotePort = $S_PUBLIC"
}

gen_frpc_header() {
  echo "# frpc 客户端配置（在内网机器上运行 frpc）"
  echo "serverAddr = \"$(public_addr)\""
  echo "serverPort = $CONTROL_PORT"
  echo "auth.method = \"token\""
  [ -n "$TOKEN" ] && echo "auth.token = \"$TOKEN\""
  echo "transport.tls.enable = true"
  local alt; alt="$(public_addr_alt)"
  [ -n "$alt" ] && echo "# 备选 IPv6 地址: $alt"
  echo
}

gen_frpc() {
  gen_frpc_header
  if [ -n "${1:-}" ]; then
    gen_frpc_proxy "$1"; echo
    return 0
  fi
  local i=1 n; n="$(svc_count)"
  while [ "$i" -le "$n" ]; do gen_frpc_proxy "$i"; echo; i=$((i + 1)); done
}

gen_frps() {
  echo "# frps 服务端配置（在 VPS 上运行 frps）"
  case "$IP_PREF" in
    6)    echo "bindAddr = \"::\"" ;;
    both) echo "bindAddr = \"::\"  # 双栈监听（需 net.ipv6.bindv6only=0）" ;;
    *)    echo "bindAddr = \"0.0.0.0\"" ;;
  esac
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

print_frpc() {
  [ -s "$SVC_FILE" ] || { warn "暂无服务，请先在「管理服务」中添加"; return 1; }
  print_block "frpc.toml（复制到内网机器运行 frpc -c frpc.toml）" "$(gen_frpc)"
}

save_configs() {
  mkdir -p "$STATE_DIR" "$CLIENT_DIR" 2>/dev/null
  gen_frps > "$FRPS_CONF"
  gen_frpc > "$CLIENT_DIR/frpc.toml"
}

apply_config() {
  save_configs
  if pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1 && command -v systemctl >/dev/null 2>&1; then
    systemctl restart frps 2>/dev/null && info "已重启 frps 以应用新配置"
  fi
}

setup_control() {
  title "设置控制端口"
  printf '  控制端口是 frpc 连接 frps 用的，和具体服务无关，只需要一个。\n' >&2
  printf '  请在服务商网页端把它映射为「端口 → 同端口」。\n' >&2
  local v t
  v="$(ask '控制端口' "${CONTROL_PORT:-7000}")"
  valid_port "$v" || v=7000
  CONTROL_PORT="$v"
  t="$(ask 'auth token（留空自动生成）' "$TOKEN")"
  [ -n "$t" ] || t="$(gen_token)"
  TOKEN="$t"
  save_state
  ok "控制端口: $CONTROL_PORT"
}

setup_net_pref() {
  title "网络偏好"
  printf '  当前: %s\n' "$(net_pref_label)"
  printf '  IPv4: %s    IPv6: %s\n' "${PUBLIC_IP:-无}" "${PUBLIC_IP6:-无}"
  menu_item 1 '仅 IPv4'
  menu_item 2 '仅 IPv6'
  menu_item 3 '均可（优先 IPv4，注释 IPv6）'
  menu_item 0 '返回'
  local c; c="$(ask '请选择' '0')"
  case "$c" in
    1) [ -n "$PUBLIC_IP" ] || { warn "未检测到 IPv4 地址"; return 1; }; IP_PREF=4;;
    2) [ -n "$PUBLIC_IP6" ] || { warn "未检测到 IPv6 地址"; return 1; }; IP_PREF=6;;
    3) { [ -n "$PUBLIC_IP" ] || [ -n "$PUBLIC_IP6" ]; } || { warn "未检测到公网地址"; return 1; }; IP_PREF=both;;
    *) return 0;;
  esac
  save_state
  apply_config
  ok "网络偏好已设为: $(net_pref_label)"
}

server_menu() {
  while true; do
    [ -t 1 ] && clear
    draw_header
    title "frps 管理"
    menu_item 1 '停止'
    menu_item 2 '重启'
    menu_item 3 '状态'
    menu_item 4 '日志'
    menu_item 5 '修改控制端口'
    menu_item 6 '网络偏好（IPv4 / IPv6 / 均可）'
    menu_item 0 '返回'
    local c; c="$(ask '请选择' '0')"
    printf '\n'
    case "$c" in
      1) service_ctl stop ;;
      2) service_ctl restart ;;
      3) service_ctl status ;;
      4) service_ctl logs ;;
      5) setup_control ;;
      6) setup_net_pref ;;
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
  [ -n "$ver" ] || ver="$(ask '请输入 frp 版本号（如 0.61.1）')"
  [ -n "$ver" ] || die "未指定 frp 版本"
  FRP_VERSION="$ver"; save_state
  local dir; dir="$(download_frp "$ver" "$arch")" || die "frp 下载失败"
  install -m 0755 "$dir/frps" /usr/local/bin/frps || die "安装 frps 失败"
  rm -rf "$(dirname "$dir")"
  ok "frps 已安装（v$ver）"
  save_configs
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

start_service() {
  local pid; pid="$(pgrep -f '/usr/local/bin/frps' | head -1)"
  if [ -n "$pid" ]; then ok "frps 已在运行（PID $pid）"; return 0; fi
  require_root
  if [ ! -x /usr/local/bin/frps ]; then install_frps; return $?; fi
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl start frps && ok "已启动" || err "启动失败，查看: journalctl -u frps -e"
  else
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
    start) systemctl start frps && ok "已启动";;
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

download_self() {
  [ -n "$SELF_URL" ] || return 1
  curl -fsSL --connect-timeout 10 --max-time 60 "$SELF_URL" -o "$1" 2>/dev/null || return 1
  bash -n "$1" || return 1
}

remote_version() {
  [ -n "$SELF_URL" ] || return 1
  curl -fsSL --connect-timeout 4 --max-time 10 "$SELF_URL" 2>/dev/null \
    | sed -n 's/^VERSION="\(.*\)"$/\1/p' | head -1
}

self_install() {
  require_root
  local target="/usr/local/bin/$APP" src="" tmp=""
  if [ -n "$SELF_URL" ]; then
    tmp="$(mktemp)"
    if download_self "$tmp"; then src="$tmp"; else warn "从远端下载失败"; fi
  fi
  if [ -z "$src" ] && [ -f "$0" ]; then src="$0"; fi
  if [ -z "$src" ]; then [ -n "$tmp" ] && rm -f "$tmp"; die "无法获取脚本来源"; fi
  [ "$(readlink -f "$src")" = "$target" ] && { [ -n "$tmp" ] && rm -f "$tmp"; ok "已安装在 $target"; return 0; }
  install -m 0755 "$src" "$target" || { [ -n "$tmp" ] && rm -f "$tmp"; die "安装失败"; }
  [ -n "$tmp" ] && rm -f "$tmp"
  ok "已安装为命令: $target"
}

self_update() {
  require_root
  local tmp; tmp="$(mktemp)"
  download_self "$tmp" || { rm -f "$tmp"; die "下载或校验失败"; }
  install -m 0755 "$tmp" "/usr/local/bin/$APP" || { rm -f "$tmp"; die "写入失败"; }
  rm -f "$tmp"
  ok "已更新到最新版本"
}

check_update() {
  [ "$(id -u)" = "0" ] || return 0
  [ -n "$SELF_URL" ] || return 0
  [ -f "$0" ] && [ "$(readlink -f "$0")" = "/usr/local/bin/$APP" ] || return 0
  local rv; rv="$(remote_version)" || return 0
  [ -n "$rv" ] && [ "$rv" != "$VERSION" ] || return 0
  local newest; newest="$(printf '%s\n%s\n' "$VERSION" "$rv" | sort -V | tail -1)"
  [ "$newest" = "$rv" ] || return 0
  info "发现新版本 $rv（当前 v$VERSION）"
  confirm_yes "是否现在更新？" && self_update
}

maybe_install_prompt() {
  [ "$(id -u)" = "0" ] || return 0
  [ -f "$0" ] && [ "$(readlink -f "$0")" = "/usr/local/bin/$APP" ] && return 0
  [ ! -f "$0" ] && [ -z "$SELF_URL" ] && return 0
  confirm "是否安装为命令 $APP（以后直接运行）?" && self_install
}

ensure_frps() {
  pgrep -f '/usr/local/bin/frps' >/dev/null 2>&1 && return 0
  if [ "$(id -u)" != "0" ]; then
    warn "frps 未运行：安装/启动需要 root，请用 sudo 运行"
    return 0
  fi
  local msg="未检测到 frps，是否现在自动下载并启动？"
  [ -x /usr/local/bin/frps ] && msg="frps 未运行，是否现在启动？"
  confirm_yes "$msg" && start_service
}

first_setup() {
  title "首次配置"
  printf '  已自动检测到网络：\n' >&2
  printf '    IPv4  %s  %s\n' "${V4_STATUS:-无}" "${PUBLIC_IP:-}" >&2
  printf '    IPv6  %s  %s\n' "${V6_STATUS:-无}" "${PUBLIC_IP6:-}" >&2
  printf '\n' >&2
  local def
  case "$IP_PREF" in 4) def=1;; 6) def=2;; both) def=3;; *) def=1;; esac
  printf '  请选择网络偏好（回车=推荐 %s）\n' "$(net_pref_label)" >&2
  menu_item 1 '仅 IPv4'
  menu_item 2 '仅 IPv6'
  menu_item 3 '均可（优先 IPv4，注释 IPv6）'
  local c; c="$(ask '请选择' "$def")"
  case "$c" in
    1) if [ -n "$PUBLIC_IP" ]; then IP_PREF=4; else warn "未检测到 IPv4，保持 $(net_pref_label)"; fi;;
    2) if [ -n "$PUBLIC_IP6" ]; then IP_PREF=6; else warn "未检测到 IPv6，保持 $(net_pref_label)"; fi;;
    3) IP_PREF=both;;
  esac
  save_state
  printf '\n' >&2
  setup_control
}

main_screen() {
  load_state
  detect_env
  save_state
  if [ -z "$CONTROL_PORT" ]; then
    first_setup
  fi
  maybe_install_prompt
  check_update
  ensure_frps
  local first=1
  while true; do
    if [ -t 1 ] && [ "$first" = 0 ]; then clear; fi
    first=0
    draw_header
    printf '\n'
    menu_item 1 '启动服务'
    menu_item 2 '管理服务'
    menu_item 3 '打印客户端配置（frpc）'
    menu_item 4 'frps 管理'
    menu_item 5 '卸载'
    menu_item 6 '退出'
    local c; c="$(ask '请选择')"
    case "$c" in
      1) start_service ;;
      2) svc_manage ;;
      3) print_frpc; pause_key ;;
      4) server_menu ;;
      5) uninstall_all && return 0 ;;
      6) return 0 ;;
      *) warn "无效选项" ;;
    esac
  done
}

usage() {
  cat <<EOF
$APP - NAT 小鸡 frps 一键工具（v$VERSION）

用法:
  $APP                 主界面
  $APP list            查看服务列表
  $APP gen frps        打印 frps.toml
  $APP gen frpc        打印 frpc.toml
  $APP install         安装并启动 frps
  $APP net 4|6|both    设置网络偏好（IPv4 / IPv6 / 均可）
  $APP start|stop|restart|status|logs
  $APP uninstall       完全卸载（服务/程序/配置/命令）
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
      load_state; detect_env
      case "${2:-frps}" in
        frps) gen_frps ;;
        frpc) gen_frpc ;;
        *) die "未知类型: ${2:-}" ;;
      esac
      ;;
    install) install_frps ;;
    net)
      load_state; detect_env; save_state
      case "${2:-}" in
        4|6|both) IP_PREF="${2}"; save_state; apply_config; ok "网络偏好: $(net_pref_label)";;
        "") setup_net_pref;;
        *) die "用法: $APP net 4|6|both";;
      esac
      ;;
    start|stop|restart|status|logs) service_ctl "$1" ;;
    uninstall) uninstall_all ;;
    self-install) self_install ;;
    self-update) self_update ;;
    help|-h|--help) usage ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
