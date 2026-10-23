#!/bin/bash
# uninstall-extra.sh — removes the extra cores (qWDTT, olcRTC, CSQTT, TUIC)
# from the panel. Deletes their binaries from the panel bin folder. Run as root.

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

was_active=0
if command -v systemctl > /dev/null 2>&1 && systemctl is-active x-ui > /dev/null 2>&1; then
    was_active=1
    systemctl stop x-ui
    echo -e "${green}[INF] x-ui stopped.${plain}"
fi

for core in extra-qwdtt extra-qwdtt-amd64 extra-qwdtt-arm64 \
    extra-olcrtc extra-olcrtc-amd64 extra-olcrtc-arm64 \
    csqtt csqtt-linux-amd64 csqtt-linux-arm64 \
    tuic-server tuic-server-linux-amd64 tuic-server-linux-arm64; do
    if [[ -f "$BIN_DIR/$core" ]]; then
        rm -f "$BIN_DIR/$core"
        echo -e "${green}[INF] Removed $BIN_DIR/$core${plain}"
    fi
done

# Legacy TUIC layout used a bin/tuic directory with generated JSON configs.
if [[ -d "$BIN_DIR/tuic" ]]; then
    rm -rf "$BIN_DIR/tuic"
    echo -e "${green}[INF] Removed $BIN_DIR/tuic${plain}"
fi

if [[ $was_active -eq 1 ]] && command -v systemctl > /dev/null 2>&1; then
    systemctl start x-ui
    echo -e "${green}[INF] x-ui started.${plain}"
else
    echo -e "${green}[INF] x-ui was not active; leaving it stopped.${plain}"
fi

echo -e "${green}Done. Extra cores removed.${plain}"