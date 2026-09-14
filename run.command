#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
cd "$SCRIPT_DIR"

pkill -x CodexSwitch >/dev/null 2>&1 || true
"$SCRIPT_DIR/build.command"

nohup "$SCRIPT_DIR/dist/CodexSwitch" >"$SCRIPT_DIR/dist/CodexSwitch.log" 2>&1 &
disown
# 關閉由 .command 開啟的 Terminal 視窗
( sleep 1; osascript -e 'tell application "Terminal" to close front window' ) >/dev/null 2>&1 &
exit 0
