#!/bin/bash
# uninstall-extra.sh — removes the extra cores (qwdtt + olcRTC) from the panel.
# Deletes their binaries from the panel bin folder. Run as root.

set -e

red='\033[0;31m'
green='\033[0;32m'
plain='\033[0m'

if [[ $EUID -ne 0 ]]; then
    echo -e "${red}[ERR] Run as root.${plain}"
    exit 1
fi

XUI_DIR="${XUI_MAIN_FOLDER:=/usr/local/x-ui}"
BIN_DIR="$XUI_DIR/bin"

if command -v systemctl > /dev/null 2>&1 && systemctl is-active x-ui > /dev/null 2>&1; then
    systemctl stop x-ui
    echo -e "${green}[INF] x-ui stopped.${plain}"
fi

for core in extra-qwdtt extra-olcrtc; do
    if [[ -f "$BIN_DIR/$core" ]]; then
        rm -f "$BIN_DIR/$core"
        echo -e "${green}[INF] Removed $BIN_DIR/$core${plain}"
    fi
done

if command -v systemctl > /dev/null 2>&1; then
    systemctl start x-ui
    echo -e "${green}[INF] x-ui started.${plain}"
fi

echo -e "${green}Done. Extra cores removed.${plain}"