#!/bin/zsh
set -euo pipefail
SCRIPT_DIR="${0:A:h}"
SRC_DIR="$SCRIPT_DIR/src"
BUILD_VERSION="${CODEX_SWITCH_VERSION:-$(TZ=Asia/Taipei date '+1.%y.%m%d build %H%M')}"
if [[ ! "$BUILD_VERSION" =~ '^1\.[0-9]{2}\.[0-9]{4} build [0-9]{4}$' ]]; then
 print -u2 '錯誤：版本格式必須為 1.YY.MMDD build HHmm'
 exit 1
fi
VERSION_NUMBER="${BUILD_VERSION% build *}"
BUILD_TIME="${BUILD_VERSION##* build }"
DIST_ROOT="$SCRIPT_DIR/dist"
DIST_BACKUP="$SCRIPT_DIR/dist.bak"
if [[ -L "$DIST_ROOT" || ( -e "$DIST_ROOT" && ! -d "$DIST_ROOT" ) ]]; then
 print -u2 '錯誤：dist 必須是一般目錄，不可為符號連結。'
 exit 1
fi
if [[ -e "$DIST_BACKUP" || -L "$DIST_BACKUP" ]]; then
 print -u2 '錯誤：dist.bak 已存在，請先確認或還原上次建置的備份。'
 exit 1
fi
# 在替換既有產物前完成工具鏈檢查與整個建置。
(cd "$SRC_DIR" && go list -m >/dev/null)
BUILD_STAGE="$(mktemp -d "$SCRIPT_DIR/.codexswitch-build.XXXXXX")"
trap '/bin/rm -rf -- "$BUILD_STAGE"' EXIT
mkdir -p "$BUILD_STAGE/dist"
APP_DIR="$BUILD_STAGE/dist/CodexSwitch.app"
CONTENTS="$APP_DIR/Contents"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"
cp "$SRC_DIR/assets/CodexSwitch.icns" "$CONTENTS/Resources/CodexSwitch.icns"
rm -f "$CONTENTS/MacOS/CodexSwitch" "$CONTENTS/Info.plist"
printf '%s\n' "正在編譯 CodexSwitch…"
(cd "$SRC_DIR" && CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-X main.appVersion=$VERSION_NUMBER -X main.appBuild=$BUILD_TIME" -o "$CONTENTS/MacOS/CodexSwitch" .)
cat > "$CONTENTS/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleName</key><string>CodexSwitch</string>
<key>CFBundleDisplayName</key><string>CodexSwitch</string>
<key>CFBundleIdentifier</key><string>com.vader.codexswitch</string>
<key>CFBundleVersion</key><string>$VERSION_NUMBER</string>
<key>CFBundleShortVersionString</key><string>$VERSION_NUMBER</string>
<key>CodexSwitchDisplayVersion</key><string>$BUILD_VERSION</string>
<key>CFBundleIconFile</key><string>CodexSwitch.icns</string>
<key>CFBundleExecutable</key><string>CodexSwitch</string>
<key>LSMinimumSystemVersion</key><string>12.0</string>
</dict></plist>
PLIST
chmod +x "$CONTENTS/MacOS/CodexSwitch"
# Remove AppleDouble metadata files created on external macOS volumes; they break codesign/DMG packaging.
find "$APP_DIR" -name '._*' -type f -delete
find "$APP_DIR" -name '._*' -type d -prune -exec rmdir {} + 2>/dev/null || true
/usr/bin/plutil -lint "$CONTENTS/Info.plist" >/dev/null
if [[ -d "$DIST_ROOT" ]]; then
 /bin/mv -- "$DIST_ROOT" "$DIST_BACKUP"
fi
if ! /bin/mv -- "$BUILD_STAGE/dist" "$DIST_ROOT"; then
 if [[ -d "$DIST_BACKUP" && ! -e "$DIST_ROOT" && ! -L "$DIST_ROOT" ]]; then
  /bin/mv -- "$DIST_BACKUP" "$DIST_ROOT" || print -u2 '還原失敗，請保留 dist.bak 備份。'
 fi
 print -u2 '錯誤：無法替換 dist，請檢查檔案權限。'
 exit 1
fi
if [[ -d "$DIST_BACKUP" ]]; then /bin/rm -rf -- "$DIST_BACKUP"; fi
printf '%s\n' "編譯完成：$DIST_ROOT/CodexSwitch.app"
