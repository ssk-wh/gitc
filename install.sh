#!/usr/bin/env bash
# gitc 一键安装（Linux / macOS）
# 用法：curl -fsSL https://raw.githubusercontent.com/ssk-wh/gitc/master/install.sh | bash
set -euo pipefail

REPO="${GITC_REPO:-ssk-wh/gitc}"
BIN_DIR="${GITC_INSTALL_DIR:-$HOME/.local/bin}"
MIRROR="${GITC_MIRROR:-https://gh-proxy.com/}"

say() { printf '%s\n' "$*"; }
die() { printf '错误: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Linux*)  os=linux ;;
  Darwin*) os=darwin ;;
  *) die "不支持的系统: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "不支持的架构: $(uname -m)" ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

download() { # url dest
  if curl -fL --retry 2 --max-time 300 -o "$2" "$1" 2>/dev/null; then
    return 0
  fi
  say "直连失败，尝试镜像下载…"
  curl -fL --retry 2 --max-time 300 -o "$2" "${MIRROR}${1}"
}

latest_tag() {
  local u rel
  u="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null || true)"
  case "$u" in
    */releases/tag/*) printf '%s' "${u##*/}"; return 0 ;;
  esac
  rel="$(curl -fsSL -H 'User-Agent: gitc-installer' "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null || true)"
  printf '%s' "$rel" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/'
}

install_binary() {
  mkdir -p "$BIN_DIR"
  install -m 0755 "$1" "$BIN_DIR/gitc"
  if [ -z "${GITC_INSTALL_DIR:-}" ]; then
    case ":$PATH:" in
      *":$BIN_DIR:"*) ;;
      *)
        local rc
        for rc in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
          [ -f "$rc" ] || continue
          if ! grep -qF "gitc installer" "$rc"; then
            printf '\n# gitc installer\nexport PATH="%s:$PATH"\n' "$BIN_DIR" >> "$rc"
          fi
        done
        say "已将 $BIN_DIR 加入 PATH（重新打开终端或 source 对应 rc 文件后生效）"
        ;;
    esac
  fi
  say "已安装: $BIN_DIR/gitc"
  "$BIN_DIR/gitc" --version || true
}

tag="${GITC_VERSION:-}"
if [ -z "$tag" ]; then
  say "查询最新 release…"
  tag="$(latest_tag || true)"
fi

if [ -n "$tag" ]; then
  name="gitc-${os}-${arch}"
  base="https://github.com/$REPO/releases/download/$tag"
  say "下载 $name ($tag)…"
  if download "$base/$name" "$tmp/$name"; then
    if download "$base/SHA256SUMS" "$tmp/SHA256SUMS"; then
      ( cd "$tmp"
        if command -v sha256sum >/dev/null 2>&1; then
          grep " $name\$" SHA256SUMS | sha256sum -c -
        else
          grep " $name\$" SHA256SUMS | shasum -a 256 -c -
        fi )
    fi
    install_binary "$tmp/$name"
    exit 0
  fi
  say "release 下载失败，尝试从源码构建…"
fi

# 回退：源码构建（需要 git + go）
command -v git >/dev/null 2>&1 || die "没有可用的 release，且未安装 git"
command -v go >/dev/null 2>&1 || die "没有可用的 release，且未安装 Go"
say "从源码构建…"
git clone --depth 1 "https://github.com/$REPO" "$tmp/src" >/dev/null 2>&1 ||
  git clone --depth 1 "git@github.com:$REPO.git" "$tmp/src" >/dev/null 2>&1 ||
  die "无法克隆仓库 $REPO"
( cd "$tmp/src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$tmp/gitc" . )
install_binary "$tmp/gitc"
