#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
cd "$SCRIPT_DIR"

"$SCRIPT_DIR/build.command"
pkill -x CodexSwitch >/dev/null 2>&1 || true

nohup open -a "$SCRIPT_DIR/dist/CodexSwitch.app" --args >"$SCRIPT_DIR/dist/CodexSwitch.log" 2>&1 &
disown
# 關閉由 .command 開啟的 Terminal 視窗
( sleep 1; osascript -e 'tell application "Terminal" to close front window' ) >/dev/null 2>&1 &
exit 0
