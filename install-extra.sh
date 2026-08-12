#!/bin/bash
# install-extra.sh — converts an installed 3x-ui/EX3-UI panel into the latest
# EX3-UI build: replaces the panel binary and installs the extra cores
# (qwdtt + olcRTC). The database and configs are left untouched.
# Usage: bash <(curl -Ls https://raw.githubusercontent.com/Bebrik2283555/Ex3-ui/main/install-extra.sh)

set -e

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

[[ $EUID -ne 0 ]] && echo -e "${red}[ERR] Run as root.${plain}" && exit 1

XUI_DIR="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
BIN_DIR="$XUI_DIR/bin"
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
[[ ! -s "$TMP/x-ui.tar.gz" ]] && echo -e "${red}[ERR] Downloaded archive is empty.${plain}" && exit 1
tar -xzf "$TMP/x-ui.tar.gz" -C "$TMP"

SRC="$TMP/x-ui"
if [[ ! -f "$SRC/x-ui" ]]; then
    echo -e "${red}[ERR] Release archive is missing the x-ui binary.${plain}"
    exit 1
fi

mkdir -p "$XUI_DIR" "$BIN_DIR"

# Replace the panel binary. The running panel executes its binary in place, so
# a direct write fails with ETXTBSY; copy to a temp name and mv (rename) over
# the live path — the old inode keeps running, the new binary applies on restart.
cp -f "$SRC/x-ui" "$XUI_DIR/x-ui.new"
chmod 755 "$XUI_DIR/x-ui.new"
mv -f "$XUI_DIR/x-ui.new" "$XUI_DIR/x-ui"
echo -e "${green}[INF] Installed panel binary ($TAG) -> $XUI_DIR/x-ui${plain}"

NEED=("extra-qwdtt" "extra-olcrtc")
for f in "${NEED[@]}"; do
    if [[ -f "$SRC/bin/$f" ]]; then
        cp -f "$SRC/bin/$f" "$BIN_DIR/$f"
        chmod 755 "$BIN_DIR/$f"
        echo -e "${green}[INF] Installed $f -> $BIN_DIR/$f${plain}"
    else
        echo -e "${yellow}[WARN] $f not present in the release archive, skipping.${plain}"
    fi
done

# Restart the panel so the new binary and cores are picked up.
if command -v systemctl > /dev/null 2>&1 && systemctl is-active x-ui > /dev/null 2>&1; then
    systemctl restart x-ui
    echo -e "${green}[INF] x-ui restarted.${plain}"
else
    echo -e "${green}[INF] Restart the panel to load the new binary and cores (e.g. systemctl restart x-ui).${plain}"
fi

echo
echo -e "${green}Done.${plain}"
echo "The panel is now EX3-UI ${TAG}. Open it: Extra cores (Доп. ядра), upload the cores' configs and start them."
