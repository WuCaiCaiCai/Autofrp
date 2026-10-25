#!/usr/bin/env bash
# Autofrp v1.1.3 - 远程 VPS 与本地服务器内网穿透 (frps/frpc) & Caddy 快速反代一键部署脚本
# 全面适配: Debian / Ubuntu / Deepin / UOS / Fedora / RHEL / Rocky / Alma / Arch / Manjaro / openSUSE / Alpine
# Copyright (c) 2026 Wu Cai (WuCaiCaiCai)
# SPDX-License-Identifier: MIT
set -euo pipefail

APP="autof"
VERSION="1.1.3"
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
    *) return 1 ;;
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

# 校验目标二进制确实是本版本的 Autofrp（防止误用旧版 Bash 脚本或其他文件）
verify_binary() {
  local bin="$1"
  [ -f "$bin" ] || return 1
  chmod +x "$bin" 2>/dev/null || true
  "$bin" version 2>/dev/null | grep -q "Autofrp v${VERSION}" || return 1
  return 0
}

# 读取当前已安装版本（旧版 Bash 脚本没有 version 子命令，会返回空）
installed_version() {
  local bin="$TARGET_BIN"
  [ -x "$bin" ] || return 1
  "$bin" version 2>/dev/null | sed -n 's/^Autofrp v\([^ ]*\).*$/\1/p' | head -1
}

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
  local out_bin="$1" arch
  if ! arch="$(detect_arch)"; then
    warn "预编译包仅提供 amd64 / arm64 架构，当前为: $(uname -m)"
    return 1
  fi
  local asset="autof_linux_${arch}"
  local raw_url="https://github.com/${REPO}/releases/download/v${VERSION}/${asset}"
  local urls=(
    "$raw_url"
    "https://ghfast.top/$raw_url"
    "https://gh-proxy.com/$raw_url"
  )
  for u in "${urls[@]}"; do
    info "正在拉取预编译二进制 (${arch}): $u"
    if curl -fsSL --connect-timeout 8 --max-time 120 -o "$out_bin" "$u" 2>/dev/null && verify_binary "$out_bin"; then
      return 0
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

  # 已安装且版本一致时直接复用（可用 AUTOF_FORCE_REINSTALL=1 强制重装）
  local cur_ver=""
  cur_ver="$(installed_version || true)"
  if [ "${AUTOF_FORCE_REINSTALL:-0}" != "1" ] && [ "$cur_ver" = "$VERSION" ]; then
    printf '%s' "$dest"
    return 0
  fi
  [ -n "$cur_ver" ] && info "检测到已安装 v${cur_ver}，准备更新到 v${VERSION} ..."

  local tmp_bin
  tmp_bin="$(mktemp)"
  local built=0
  if build_from_local_source "$tmp_bin" && verify_binary "$tmp_bin"; then
    built=1
  elif download_release_binary "$tmp_bin" && verify_binary "$tmp_bin"; then
    built=1
  fi

  if [ "$built" = "1" ]; then
    install -m 0755 "$tmp_bin" "$dest"
    rm -f "$tmp_bin"
    fix_selinux_context "$dest"
    ok "$APP v${VERSION} 已安装: $dest"
    printf '%s' "$dest"
    return 0
  fi
  rm -f "$tmp_bin"

  # 兜底：目标位置已有正确版本可继续使用
  if verify_binary "$dest"; then
    warn "下载/编译失败，但 $dest 已是 v${VERSION}，继续使用现有程序"
    printf '%s' "$dest"
    return 0
  fi

  if [ -n "$cur_ver" ]; then
    die "获取 Autofrp v${VERSION} 失败。当前 $TARGET_BIN 是旧版本 v${cur_ver}，为避免执行旧逻辑已拒绝使用。
  请检查服务器网络后重试；若确认要清理旧版本，请执行：
    sudo rm -f $TARGET_BIN
  然后重新运行本脚本。"
  fi

  die "获取 Autofrp v${VERSION} 失败（未找到 v${VERSION} 的发布产物或网络不可达）。
  请检查: https://github.com/${REPO}/releases/tag/v${VERSION}
  如在国内服务器，可尝试设置镜像地址后重试，或手动下载静态二进制：https://github.com/WuCaiCaiCai/Autofrp/releases"
}

main() {
  local bin
  bin="$(ensure_binary)"
  exec "$bin" "$@"
}

main "$@"
