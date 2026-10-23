#!/bin/bash
# install-extra.sh — converts an installed 3x-ui/EX3-UI panel into the latest
# EX3-UI build: replaces the panel binary and installs the extra cores
# (qWDTT, olcRTC, CSQTT, TUIC). The database and configs are left untouched.
# Usage: bash <(curl -Ls https://raw.githubusercontent.com/Bebrik2283555/Ex3-ui/main/install-extra.sh)
# Offline: XUI_LOCAL_ARCHIVE=/root/x-ui-linux-amd64.tar.gz bash install-extra.sh

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

LOCAL_ARCHIVE="${XUI_LOCAL_ARCHIVE:-}"
TAG="local"
if [[ -n "$LOCAL_ARCHIVE" ]]; then
    if [[ ! -f "$LOCAL_ARCHIVE" ]]; then
        echo -e "${red}[ERR] Local archive not found: $LOCAL_ARCHIVE${plain}"
        exit 1
    fi
    cp -f "$LOCAL_ARCHIVE" "$TMP/x-ui.tar.gz"
    echo -e "${green}[INF] Using local archive: $LOCAL_ARCHIVE${plain}"
else
    TAG=$(curl -Ls --retry 3 --connect-timeout 10 "https://api.github.com/repos/${GH_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [[ -z "$TAG" ]]; then
        echo -e "${red}[ERR] Failed to fetch the latest release tag from ${GH_REPO}.${plain}"
        exit 1
    fi
    URL="https://github.com/${GH_REPO}/releases/download/${TAG}/x-ui-linux-${CPU_ARCH}.tar.gz"
    echo -e "${green}[INF] Downloading ${URL} ...${plain}"
    if ! curl -fLR --retry 3 --connect-timeout 15 -o "$TMP/x-ui.tar.gz" "$URL"; then
        echo -e "${red}[ERR] Failed to download the release archive.${plain}"
        exit 1
    fi
    if [[ ! -s "$TMP/x-ui.tar.gz" ]]; then
        echo -e "${red}[ERR] Downloaded archive is empty.${plain}"
        exit 1
    fi

    # Verify the sidecar published next to the archive when it exists. Old
    # releases without .sha256 still install with a warning.
    SUMS="$TMP/x-ui.tar.gz.sha256"
    CODE=$(curl -sL --retry 3 --retry-delay 3 --connect-timeout 15 --max-time 60 -o "$SUMS" -w '%{http_code}' "${URL}.sha256" || true)
    if [[ "$CODE" == "200" ]]; then
        EXPECTED=$(awk 'NR == 1 {print $1}' "$SUMS")
        ACTUAL=$(sha256sum "$TMP/x-ui.tar.gz" | awk '{print $1}')
        rm -f "$SUMS"
        if [[ ! "$EXPECTED" =~ ^[0-9a-f]{64}$ || "$EXPECTED" != "$ACTUAL" ]]; then
            echo -e "${red}[ERR] Checksum mismatch: expected ${EXPECTED:-<none>}, got ${ACTUAL}.${plain}"
            exit 1
        fi
        echo -e "${green}[INF] Checksum verified: ${ACTUAL}${plain}"
    elif [[ "$CODE" == "404" ]]; then
        rm -f "$SUMS"
        echo -e "${yellow}[WARN] No .sha256 published for ${TAG}, skipping verification.${plain}"
    else
        rm -f "$SUMS"
        echo -e "${red}[ERR] Failed to download the checksum (HTTP ${CODE:-unknown}).${plain}"
        exit 1
    fi
fi
[[ ! -s "$TMP/x-ui.tar.gz" ]] && echo -e "${red}[ERR] Archive is empty.${plain}" && exit 1
tar -xzf "$TMP/x-ui.tar.gz" -C "$TMP"

SRC="$TMP/x-ui"
if [[ ! -f "$SRC/x-ui" ]]; then
    echo -e "${red}[ERR] Release archive is missing the x-ui binary.${plain}"
    exit 1
fi

mkdir -p "$XUI_DIR" "$BIN_DIR"

# Stop the panel first: replacing a running sidecar binary can fail with
# ETXTBSY, and systemd stops the child cores together with the panel.
was_active=0
if command -v systemctl > /dev/null 2>&1 && systemctl is-active x-ui > /dev/null 2>&1; then
    was_active=1
    systemctl stop x-ui
    echo -e "${green}[INF] x-ui stopped.${plain}"
fi

# Replace the panel binary. Copy to a temp name and mv (rename) over the live
# path so the old inode can finish if a process still holds it.
cp -f "$SRC/x-ui" "$XUI_DIR/x-ui.new"
chmod 755 "$XUI_DIR/x-ui.new"
mv -f "$XUI_DIR/x-ui.new" "$XUI_DIR/x-ui"
echo -e "${green}[INF] Installed panel binary ($TAG) -> $XUI_DIR/x-ui${plain}"

for f in "extra-qwdtt" "extra-olcrtc"; do
    src=""
    if [[ -f "$SRC/bin/${f}-${CPU_ARCH}" ]]; then
        src="$SRC/bin/${f}-${CPU_ARCH}"
    elif [[ -f "$SRC/bin/$f" ]]; then
        src="$SRC/bin/$f"
    fi
    if [[ -n "$src" ]]; then
        cp -f "$src" "$BIN_DIR/${f}-${CPU_ARCH}"
        chmod 755 "$BIN_DIR/${f}-${CPU_ARCH}"
        cp -f "$src" "$BIN_DIR/$f"
        chmod 755 "$BIN_DIR/$f"
        echo -e "${green}[INF] Installed $f -> $BIN_DIR${plain}"
    else
        echo -e "${yellow}[WARN] $f not present in the release archive, skipping.${plain}"
    fi
done

# CSQTT sidecar: the archive names it csqtt-linux-<arch>; the panel accepts
# either that or a plain bin/csqtt.
CSQTT_SRC=""
if [[ -f "$SRC/bin/csqtt-linux-${CPU_ARCH}" ]]; then
    CSQTT_SRC="$SRC/bin/csqtt-linux-${CPU_ARCH}"
elif [[ -f "$SRC/bin/csqtt" ]]; then
    CSQTT_SRC="$SRC/bin/csqtt"
fi
if [[ -n "$CSQTT_SRC" ]]; then
    cp -f "$CSQTT_SRC" "$BIN_DIR/csqtt-linux-${CPU_ARCH}"
    chmod 755 "$BIN_DIR/csqtt-linux-${CPU_ARCH}"
    cp -f "$CSQTT_SRC" "$BIN_DIR/csqtt"
    chmod 755 "$BIN_DIR/csqtt"
    echo -e "${green}[INF] Installed csqtt -> $BIN_DIR${plain}"
else
    echo -e "${yellow}[WARN] csqtt not present in the release archive, skipping.${plain}"
fi

# TUIC sidecar (internal/tuic): the archive may ship it as bin/tuic-server
# (upstream layout) or bin/tuic-server-linux-<arch>; the panel accepts either,
# so install both names.
TUCI_SRC=""
if [[ -f "$SRC/bin/tuic-server" ]]; then
    TUCI_SRC="$SRC/bin/tuic-server"
elif [[ -f "$SRC/bin/tuic-server-linux-${CPU_ARCH}" ]]; then
    TUCI_SRC="$SRC/bin/tuic-server-linux-${CPU_ARCH}"
fi
if [[ -n "$TUCI_SRC" ]]; then
    cp -f "$TUCI_SRC" "$BIN_DIR/tuic-server"
    chmod 755 "$BIN_DIR/tuic-server"
    cp -f "$TUCI_SRC" "$BIN_DIR/tuic-server-linux-${CPU_ARCH}"
    chmod 755 "$BIN_DIR/tuic-server-linux-${CPU_ARCH}"
    echo -e "${green}[INF] Installed tuic-server -> $BIN_DIR${plain}"
else
    echo -e "${yellow}[WARN] tuic-server not present in the release archive, skipping.${plain}"
fi

# Restart the panel only if it was running before this script stopped it.
if [[ $was_active -eq 1 ]] && command -v systemctl > /dev/null 2>&1; then
    systemctl start x-ui
    echo -e "${green}[INF] x-ui started.${plain}"
else
    echo -e "${green}[INF] Restart the panel to load the new binary and cores (e.g. systemctl restart x-ui).${plain}"
fi

echo
echo -e "${green}Done.${plain}"
echo "The panel is now EX3-UI ${TAG}. Open it: Extra cores (Доп. ядра), upload the cores' configs and start them."
