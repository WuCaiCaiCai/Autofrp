#!/usr/bin/env bash
# Autofrp v2.0.0 - 远程 VPS 与本地服务器内网穿透 (frps/frpc) & Caddy 快速反代一键部署脚本
# 全面适配: Debian / Ubuntu / Deepin / UOS / Fedora / RHEL / Rocky / Alma / Arch / Manjaro / openSUSE / Alpine
# Copyright (c) 2026 Wu Cai (WuCaiCaiCai)
# SPDX-License-Identifier: MIT
set -euo pipefail

APP="autof"
VERSION="2.0.0"
REPO="WuCaiCaiCai/Autofrp"
TARGET_BIN="/usr/local/bin/$APP"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'
  C_CYAN=$'\033[36m'; C_BOLD=$'\033[1m'; C_RST=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_CYAN=""; C_BOLD=""; C_RST=""
fi

info() { printf '%s[*]%s %s\n' "$C_CYAN" "$C_RST" "$*" >&2; }
ok()   { printf '%s[+]%s %s\n' "$C_GREEN" "$C_RST" "$*" >&2; }
warn() { printf '%s[!]%s %s\n' "$C_YELLOW" "$C_RST" "$*" >&2; }
die()  { printf '%s[x]%s %s\n' "$C_RED" "$C_RST" "$*" >&2; exit 1; }

detect_distro() {
  DISTRO_ID="linux"
  DISTRO_LIKE=""
  DISTRO_NAME="Linux"
  if [ -r /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    DISTRO_ID="${ID:-linux}"
    DISTRO_LIKE="${ID_LIKE:-}"
    DISTRO_NAME="${PRETTY_NAME:-Linux}"
  fi
}

ensure_base_deps() {
  local missing=()
  for cmd in curl tar gzip; do
    command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
  done
  [ "${#missing[@]}" -eq 0 ] && return 0

  if [ "$(id -u)" != "0" ]; then
    die "缺少基础依赖 (${missing[*]})，请使用 sudo 运行以自动安装"
  fi

  detect_distro
  info "正在为 ${DISTRO_NAME} 安装基础依赖: ${missing[*]} ..."
  local combined="${DISTRO_ID} ${DISTRO_LIKE}"
  if command -v apt-get >/dev/null 2>&1 || [[ "$combined" =~ (debian|ubuntu|deepin|uos|kali|mint) ]]; then
    DEBIAN_FRONTEND=noninteractive apt-get update -qq >/dev/null 2>&1 || true
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl tar gzip ca-certificates >/dev/null 2>&1
  elif command -v dnf >/dev/null 2>&1 || [[ "$combined" =~ (fedora|rhel|centos|rocky|almalinux|opencloudos|anolis|openeuler) ]]; then
    if command -v dnf >/dev/null 2>&1; then
      dnf install -y -q curl tar gzip ca-certificates >/dev/null 2>&1
    else
      yum install -y -q curl tar gzip ca-certificates >/dev/null 2>&1
    fi
  elif command -v pacman >/dev/null 2>&1 || [[ "$combined" =~ (arch|manjaro|endeavouros) ]]; then
    pacman -Sy --noconfirm --needed curl tar gzip ca-certificates >/dev/null 2>&1
  elif command -v zypper >/dev/null 2>&1 || [[ "$combined" =~ suse ]]; then
    zypper --non-interactive install curl tar gzip ca-certificates >/dev/null 2>&1
  elif command -v apk >/dev/null 2>&1 || [[ "$combined" =~ alpine ]]; then
    apk add --no-cache curl tar gzip ca-certificates >/dev/null 2>&1
  else
    warn "未能识别包管理器，请手动安装: ${missing[*]}"
  fi
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l|armv7|armhf) echo "arm" ;;
    i386|i686) echo "386" ;;
    riscv64) echo "riscv64" ;;
    loongarch64|loong64) echo "loong64" ;;
    *) die "不支持的 CPU 架构: $(uname -m)" ;;
  esac
}

fix_selinux_context() {
  local bin_path="$1"
  [ -f "$bin_path" ] || return 0
  if command -v restorecon >/dev/null 2>&1; then
    restorecon -F "$bin_path" >/dev/null 2>&1 || true
  elif command -v chcon >/dev/null 2>&1; then
    chcon -t bin_t "$bin_path" >/dev/null 2>&1 || true
  fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-.}")" 2>/dev/null && pwd || pwd)"

build_from_local_source() {
  local out_bin="$1"
  if [ -f "$SCRIPT_DIR/go.mod" ] && [ -f "$SCRIPT_DIR/cmd/autof/main.go" ] && command -v go >/dev/null 2>&1; then
    info "检测到本地 Go 源码，正在编译 $APP v$VERSION ..."
    (cd "$SCRIPT_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out_bin" ./cmd/autof)
    return 0
  fi
  return 1
}

download_release_binary() {
  local out_bin="$1"
  local arch
  arch="$(detect_arch)"
  local asset="autof_linux_${arch}"
  local raw_url="https://github.com/${REPO}/releases/latest/download/${asset}"
  local urls=(
    "$raw_url"
    "https://ghfast.top/$raw_url"
    "https://gh-proxy.com/$raw_url"
  )
  for u in "${urls[@]}"; do
    info "正在拉取预编译二进制 ($arch): $u"
    if curl -fsSL --connect-timeout 8 --max-time 120 -o "$out_bin" "$u" 2>/dev/null; then
      chmod 0755 "$out_bin"
      if "$out_bin" version >/dev/null 2>&1; then
        return 0
      fi
    fi
  done
  return 1
}

ensure_binary() {
  ensure_base_deps

  local dest="$TARGET_BIN"
  if [ "$(id -u)" != "0" ] && [ ! -w /usr/local/bin ]; then
    dest="$SCRIPT_DIR/$APP"
  else
    mkdir -p /usr/local/bin 2>/dev/null || true
  fi

  local tmp_bin
  tmp_bin="$(mktemp)"
  if build_from_local_source "$tmp_bin" || download_release_binary "$tmp_bin"; then
    install -m 0755 "$tmp_bin" "$dest"
    rm -f "$tmp_bin"
    fix_selinux_context "$dest"
    ok "$APP 已安装就绪: $dest"
    printf '%s' "$dest"
    return 0
  fi
  rm -f "$tmp_bin"

  if [ -x "$dest" ]; then
    fix_selinux_context "$dest"
    printf '%s' "$dest"
    return 0
  fi
  die "无法下载或编译 $APP 二进制文件，请确认网络畅通或已安装 Go 1.22+"
}

main() {
  local bin
  bin="$(ensure_binary)"
  exec "$bin" "$@"
}

main "$@"
