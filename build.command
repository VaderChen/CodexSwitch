#!/bin/zsh
set -euo pipefail
SCRIPT_DIR="${0:A:h}"
SRC_DIR="$SCRIPT_DIR/src"
APP_DIR="$SCRIPT_DIR/dist/CodexSwitch.app"
CONTENTS="$APP_DIR/Contents"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"
cp "$SRC_DIR/assets/CodexSwitch.icns" "$CONTENTS/Resources/CodexSwitch.icns"
rm -f "$CONTENTS/MacOS/CodexSwitch" "$CONTENTS/Info.plist"
printf '%s\n' "正在編譯 CodexSwitch…"
(cd "$SRC_DIR" && CGO_ENABLED=1 go build -trimpath -o "$CONTENTS/MacOS/CodexSwitch" .)
cat > "$CONTENTS/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>CodexSwitch</string>
<key>CFBundleDisplayName</key><string>CodexSwitch</string>
<key>CFBundleIdentifier</key><string>com.vader.codexswitch</string>
<key>CFBundleVersion</key><string>1.0.0</string>
<key>CFBundleShortVersionString</key><string>1.0.0</string>
<key>CFBundleIconFile</key><string>CodexSwitch.icns</string>
<key>CFBundleExecutable</key><string>CodexSwitch</string>
<key>LSMinimumSystemVersion</key><string>12.0</string>
</dict></plist>
PLIST
chmod +x "$CONTENTS/MacOS/CodexSwitch"
printf '%s\n' "編譯完成：$APP_DIR"
