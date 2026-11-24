#!/usr/bin/env bash
# 构建 gitc（macOS / Linux；可选交叉编译）
set -euo pipefail
# 未在 PATH 中时回退常见安装位置
if ! command -v go >/dev/null 2>&1; then
  for c in /usr/local/go/bin /usr/lib/go/bin /opt/go/bin /d/tools/go/bin; do
    if [ -x "$c/go" ]; then export PATH="$c:$PATH"; break; fi
  done
fi
command -v go >/dev/null 2>&1 || { echo "未找到 go，请安装 Go 1.22+" >&2; exit 1; }
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/dist"
mkdir -p "$out"
cd "$root"
export CGO_ENABLED=0

build() {
  local goos="$1" goarch="$2" name="gitc"
  [ "$goos" = "windows" ] && name="gitc.exe"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags '-s -w' -o "$out/gitc-$goos-$goarch/$name" .
  echo "构建完成: $out/gitc-$goos-$goarch/$name"
}

if [ "${1:-}" = "all" ]; then
  build windows amd64
  build darwin amd64
  build darwin arm64
  build linux amd64
  build linux arm64
else
  go build -trimpath -ldflags '-s -w' -o "$out/gitc" .
  echo "构建完成: $out/gitc"
fi
