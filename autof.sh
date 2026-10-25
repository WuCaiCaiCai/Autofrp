#!/usr/bin/env bash
# Autofrp v2.0.0 - 远程 VPS 与本地服务器内网穿透 (frps/frpc) & Caddy 快速反代一键引导脚本
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

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l|armv7) echo "arm" ;;
    *) die "不支持的 CPU 架构: $(uname -m)" ;;
  esac
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
    info "尝试下载预编译二进制: $u"
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
  local dest="$TARGET_BIN"
  if [ "$(id -u)" != "0" ] && [ ! -w /usr/local/bin ]; then
    dest="$SCRIPT_DIR/$APP"
  fi

  local tmp_bin
  tmp_bin="$(mktemp)"
  if build_from_local_source "$tmp_bin" || download_release_binary "$tmp_bin"; then
    install -m 0755 "$tmp_bin" "$dest"
    rm -f "$tmp_bin"
    ok "$APP 已就绪: $dest"
    printf '%s' "$dest"
    return 0
  fi
  rm -f "$tmp_bin"

  if [ -x "$dest" ]; then
    printf '%s' "$dest"
    return 0
  fi
  die "无法获取或编译 $APP 二进制文件，请确认网络畅通或已安装 Go 1.22+"
}

main() {
  local bin
  bin="$(ensure_binary)"
  exec "$bin" "$@"
}

main "$@"
