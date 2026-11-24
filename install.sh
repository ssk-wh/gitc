#!/usr/bin/env bash
# gitc 一键安装（Linux / macOS）
# 用法：curl -fsSL https://raw.githubusercontent.com/ssk-wh/gitc/master/install.sh | bash
set -euo pipefail

REPO="${GITC_REPO:-ssk-wh/gitc}"
BIN_DIR="${GITC_INSTALL_DIR:-$HOME/.local/bin}"

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

install_binary() {
  mkdir -p "$BIN_DIR"
  install -m 0755 "$1" "$BIN_DIR/gitc"
  if ! case ":$PATH:" in *":$BIN_DIR:"*) false;; *) true;; esac; then
    :
  else
    local wrote=0
    for rc in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
      [ -f "$rc" ] || continue
      if ! grep -qF "gitc installer" "$rc"; then
        printf '\n# gitc installer\nexport PATH="%s:$PATH"\n' "$BIN_DIR" >> "$rc"
      fi
      wrote=1
    done
    if [ "$wrote" = 0 ]; then
      printf '\n# gitc installer\nexport PATH="%s:$PATH"\n' "$BIN_DIR" >> "$HOME/.profile"
    fi
    say "已将 $BIN_DIR 加入 PATH（重新打开终端或 source 对应 rc 文件后生效）"
  fi
  say "已安装: $BIN_DIR/gitc"
  "$BIN_DIR/gitc" --version || true
}

# 优先从 GitHub Release 下载
say "查询最新 release…"
rel="$(curl -fsSL -H 'User-Agent: gitc-installer' "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null || true)"
tag="$(printf '%s' "$rel" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')"

if [ -n "$tag" ]; then
  name="gitc-${os}-${arch}"
  url="https://github.com/$REPO/releases/download/$tag/$name"
  say "下载 $name ($tag)…"
  if curl -fL --retry 3 -o "$tmp/$name" "$url"; then
    if curl -fsSL -o "$tmp/SHA256SUMS" "https://github.com/$REPO/releases/download/$tag/SHA256SUMS" 2>/dev/null; then
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
git clone --depth 1 "https://github.com/$REPO" "$tmp/src" >/dev/null 2>&1
( cd "$tmp/src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$tmp/gitc" . )
install_binary "$tmp/gitc"
