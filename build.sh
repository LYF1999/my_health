#!/bin/bash
set -e

APP_NAME="MyHealth"
VERSION="1.0.0"

case "$(uname -s)" in
  Darwin)
    echo "==> 构建 macOS 版本..."
    go build -o "${APP_NAME}" .

    echo "==> 打包 .app bundle..."
    BUNDLE="dist/${APP_NAME}.app"
    rm -rf dist
    mkdir -p "${BUNDLE}/Contents/MacOS"
    mkdir -p "${BUNDLE}/Contents/Resources"
    cp "${APP_NAME}" "${BUNDLE}/Contents/MacOS/"

    cat > "${BUNDLE}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key><string>com.yifei.myhealth</string>
    <key>CFBundleName</key><string>${APP_NAME}</string>
    <key>CFBundleExecutable</key><string>${APP_NAME}</string>
    <key>CFBundleVersion</key><string>${VERSION}</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>LSUIElement</key><true/>
</dict>
</plist>
PLIST

    rm -f "${APP_NAME}"
    echo "==> 完成: dist/${APP_NAME}.app"
    echo "   运行: open dist/${APP_NAME}.app"
    echo "   安装: cp -r dist/${APP_NAME}.app /Applications/"
    ;;

  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    echo "==> 构建 Windows 版本..."
    mkdir -p dist
    go build -ldflags "-H windowsgui" -o "dist/${APP_NAME}.exe" .
    echo "==> 完成: dist/${APP_NAME}.exe"
    ;;

  *)
    echo "不支持的平台: $(uname -s)"
    exit 1
    ;;
esac
