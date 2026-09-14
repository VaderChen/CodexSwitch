#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
cd "$SCRIPT_DIR/src"

export CGO_ENABLED=1
OUT="$SCRIPT_DIR/dist/CodexSwitch"

mkdir -p "$SCRIPT_DIR/dist"
echo "正在編譯 CodexSwitch…"
go build -trimpath -o "$OUT" .
chmod +x "$OUT"
echo "編譯完成：$OUT"
