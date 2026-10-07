#!/bin/bash
# Baut AgentStatus.app (universal: arm64 + x86_64) mit Panel und CLI.
# Aufruf: scripts/build-app.sh [--sign "Developer ID Application: …"] [--out DIR]
# Ohne --sign: ad-hoc signiert. Notarisierung macht release.sh.
set -euo pipefail

SIGN=""
OUT=dist
while [ $# -gt 0 ]; do
  case "$1" in
    --sign) SIGN="${2:?--sign braucht eine Identitaet}"; shift 2 ;;
    --out) OUT="${2:?--out braucht ein Verzeichnis}"; shift 2 ;;
    *) echo "usage: $0 [--sign IDENTITY] [--out DIR]" >&2; exit 2 ;;
  esac
done

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
VERSION=$(tr -d '[:space:]' < VERSION)
BUILD=$(git rev-list --count HEAD 2>/dev/null || echo 1)
case "$OUT" in /*) ;; *) OUT="$ROOT/$OUT" ;; esac
app="$OUT/AgentStatus.app"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# 1. CLI, zwei Architekturen, dann lipo
for arch in arm64 amd64; do
  GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" -o "$tmp/agentstatus-$arch" ./cmd/agentstatus
done
lipo -create "$tmp/agentstatus-arm64" "$tmp/agentstatus-amd64" -output "$tmp/agentstatus"

# 2. Panel universal
cd panel
swift build -c release --arch arm64 --arch x86_64
bin=$(swift build -c release --arch arm64 --arch x86_64 --show-bin-path)
if [ ! -f icon/AgentStatus.icns ] || [ icon/render.swift -nt icon/AgentStatus.icns ]; then
  swift icon/render.swift
  iconutil -c icns icon/AgentStatus.iconset -o icon/AgentStatus.icns
fi

# 3. Bundle
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers" "$app/Contents/Resources"
cp icon/AgentStatus.icns "$app/Contents/Resources/"
cp "$bin/AgentStatusPanel" "$app/Contents/MacOS/AgentStatus"
# Nicht in MacOS/: APFS ist case-insensitive, agentstatus wuerde AgentStatus ueberschreiben.
cp "$tmp/agentstatus" "$app/Contents/Helpers/agentstatus"
cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleIdentifier</key><string>com.timohone.agentstatus</string>
  <key>CFBundleIconFile</key><string>AgentStatus</string>
  <key>CFBundleName</key><string>AgentStatus</string>
  <key>CFBundleExecutable</key><string>AgentStatus</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleVersion</key><string>$BUILD</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>NSHumanReadableCopyright</key><string>© 2026 Timo Haldi · MIT License</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PLIST

# Vor dem Signieren pruefen: richtige Dateien, universal
panel_bin="$app/Contents/MacOS/AgentStatus"
cli_bin="$app/Contents/Helpers/agentstatus"
[ -f "$panel_bin" ] && [ -f "$cli_bin" ] || { echo "Panel oder CLI fehlt im Bundle" >&2; exit 1; }
if cmp -s "$panel_bin" "$cli_bin"; then echo "Panel und CLI sind dieselbe Datei" >&2; exit 1; fi
otool -L "$panel_bin" | grep -q AppKit.framework || { echo "Panel linkt kein AppKit" >&2; exit 1; }
for p in "$panel_bin" "$cli_bin"; do
  archs=$(lipo -archs "$p")
  case " $archs " in *" arm64 "*) ;; *) echo "$p: arm64 fehlt ($archs)" >&2; exit 1 ;; esac
  case " $archs " in *" x86_64 "*) ;; *) echo "$p: x86_64 fehlt ($archs)" >&2; exit 1 ;; esac
done
# Signieren, innere Programme zuerst
if [ -n "$SIGN" ]; then
  for t in "$app/Contents/Helpers/agentstatus" "$app/Contents/MacOS/AgentStatus" "$app"; do
    codesign --force --options runtime --timestamp --sign "$SIGN" "$t"
  done
else
  codesign --force --sign - "$app/Contents/Helpers/agentstatus"
  codesign --force --sign - "$app/Contents/MacOS/AgentStatus"
  codesign --force --sign - "$app"
fi
codesign --verify --deep --strict --verbose=2 "$app"

echo "$app"
