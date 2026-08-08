#!/bin/bash
# install-extra.sh — installs the extra cores (qwdtt + olcRTC) for the EX3-UI
# panel on top of an already installed 3x-ui/EX3-UI panel, without touching
# the existing database or configs.
#
# Sources:
#   1. local release folder (install -m 755 install-extra.sh on the VPS,
#      then cd into the folder and run ./install-extra.sh) — fastest, uses the
#      bundled bin/ directory;
#   2. GitHub release (bash <(curl -Ls .../install-extra.sh)) — downloads the
#      matching x-ui-linux-<arch>.tar.gz and installs the extra cores from it.

set -e

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

[[ $EUID -ne 0 ]] && echo -e "${red}[ERR] Run as root.${plain}" && exit 1

XUI_DIR="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
BIN_DIR="$XUI_DIR/bin"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GH_REPO="Bebrik2283555/Ex3-ui"

if [[ ! -f "$XUI_DIR/x-ui" ]]; then
    echo -e "${red}[ERR] Panel binary not found at $XUI_DIR/x-ui. Is the panel installed?${plain}"
    exit 1
fi

arch() {
    case "$(uname -m)" in
        x86_64 | x64 | amd64) echo 'amd64' ;;
        armv8* | armv8 | arm64 | aarch64) echo 'arm64' ;;
        *) echo "" ;;
    esac
}

mkdir -p "$BIN_DIR"

NEED=("extra-qwdtt" "extra-olcrtc")
LOCAL_OK=1
for f in "${NEED[@]}"; do
    [[ -f "$SCRIPT_DIR/bin/$f" ]] || LOCAL_OK=0
done

if [[ $LOCAL_OK == 1 ]]; then
    echo -e "${green}[INF] Found a local release folder — installing extra cores from it.${plain}"
    for f in "${NEED[@]}"; do
        cp -f "$SCRIPT_DIR/bin/$f" "$BIN_DIR/$f"
        chmod 755 "$BIN_DIR/$f"
        echo -e "${green}[INF] Installed $f -> $BIN_DIR/$f${plain}"
    done
else
    CPU_ARCH="$(arch)"
    [[ -z "$CPU_ARCH" ]] && echo -e "${red}[ERR] Unsupported CPU architecture!${plain}" && exit 1

    TMP="$(mktemp -d)"
    trap 'rm -rf "$TMP"' EXIT

    TAG=$(curl -Ls --retry 3 --connect-timeout 10 "https://api.github.com/repos/${GH_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [[ -z "$TAG" ]]; then
        echo -e "${red}[ERR] Failed to fetch the latest release tag from ${GH_REPO}.${plain}"
        exit 1
    fi
    URL="https://github.com/${GH_REPO}/releases/download/${TAG}/x-ui-linux-${CPU_ARCH}.tar.gz"
    echo -e "${green}[INF] Downloading ${URL} ...${plain}"
    curl -fLR --retry 3 --connect-timeout 15 -o "$TMP/x-ui.tar.gz" "$URL"
    tar -xzf "$TMP/x-ui.tar.gz" -C "$TMP"
    SRC_BIN="$TMP/x-ui/bin"
    for f in "${NEED[@]}"; do
        if [[ ! -f "$SRC_BIN/$f" ]]; then
            echo -e "${yellow}[WARN] $f not present in the release archive, skipping.${plain}"
            continue
        fi
        cp -f "$SRC_BIN/$f" "$BIN_DIR/$f"
        chmod 755 "$BIN_DIR/$f"
        echo -e "${green}[INF] Installed $f -> $BIN_DIR/$f${plain}"
    done
fi

# Stop the panel so the new core files are picked up cleanly, then restart it.
if command -v systemctl > /dev/null 2>&1 && systemctl is-active x-ui > /dev/null 2>&1; then
    systemctl restart x-ui
    echo -e "${green}[INF] x-ui restarted.${plain}"
else
    echo -e "${green}[INF] Restart the panel to load the cores (e.g. systemctl restart x-ui).${plain}"
fi

echo
echo -e "${green}Done.${plain}"
echo "Open the panel: Extra cores (Доп. ядра), upload the cores' configs and start them."