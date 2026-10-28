#!/bin/bash
# 編出 Windows 與 Linux（x86、ARM）的執行檔到 dist/。在一台 Linux 電腦上就能全部編好，不用 cgo。
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$HOME/.local/go/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
mkdir -p dist
for target in windows/amd64 linux/amd64 linux/arm64; do
  os=${target%/*}
  arch=${target#*/}
  out="dist/local-ai-helper-$os-$arch"
  flags="-s -w -X main.version=$VERSION"
  if [ "$os" = windows ]; then
    out="$out.exe"
    flags="$flags -H=windowsgui" # 不跳出命令列視窗
  fi
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$flags" -o "$out" ./cmd/local-ai-helper
  echo "$out"
done
