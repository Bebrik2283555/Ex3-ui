#!/bin/bash
set -euo pipefail
touch /run/warp-install.lock
trap 'rm -f /run/warp-install.lock' EXIT

USQUE_BIN=/usr/local/bin/usque
USQUE_VER=4.2.1
USQUE_DIR=/etc/usque
USQUE_CONF=$USQUE_DIR/config.json
IFACE=warp
TABLE=10000

# Telegram DC / MTProto ranges (IPv4)
TG_NETS="91.108.0.0/16 149.154.160.0/20 95.161.64.0/20 185.76.151.0/24 185.64.92.0/22 91.105.192.0/23 194.169.233.0/24 194.102.202.0/24"

apt-get update -y
DEBIAN_FRONTEND=noninteractive apt-get install -y cron curl unzip iproute2 iptables ca-certificates dnsutils 2>/dev/null || \
  DEBIAN_FRONTEND=noninteractive apt-get install -y cron curl unzip iproute2 iptables ca-certificates
systemctl enable --now cron 2>/dev/null || systemctl enable --now crond 2>/dev/null || true

wg-quick down warp 2>/dev/null || true
systemctl disable --now wg-quick@warp 2>/dev/null || true
systemctl stop usque 2>/dev/null || true
ip link set dev warp down 2>/dev/null || true
ip link del dev warp 2>/dev/null || true
ip rule del to 91.108.0.0/16 table $TABLE priority 100 2>/dev/null || true
ip rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip -6 rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip6tables -D OUTPUT -m mark --mark $TABLE ! -o warp -j REJECT 2>/dev/null || true

if [ ! -x "$USQUE_BIN" ]; then
  TMP_DIR=$(mktemp -d)
  curl -fsSL -o "$TMP_DIR/usque.zip" \
    "https://github.com/Diniboy1123/usque/releases/download/v${USQUE_VER}/usque_${USQUE_VER}_linux_amd64.zip"
    
  EXPECTED_HASH="4117e20695078af9c11edecd1a826c009bbc7ea0b7f64458612b4198910bc313"
  ACTUAL_HASH=$(sha256sum "$TMP_DIR/usque.zip" | awk '{print $1}')
  if [ "$ACTUAL_HASH" != "$EXPECTED_HASH" ]; then
    echo "ERROR: Checksum mismatch for usque.zip!" >&2
    rm -rf "$TMP_DIR"
    exit 1
  fi

  unzip -o "$TMP_DIR/usque.zip" -d "$TMP_DIR" >/dev/null
  install -m 0755 "$TMP_DIR/usque" "$USQUE_BIN"
  rm -rf "$TMP_DIR"
fi
$USQUE_BIN version 2>/dev/null || true

mkdir -p "$USQUE_DIR"

get_country() {
  local iface="${1:-}"
  local c=""
  if [ -n "$iface" ]; then
    c="$(curl --interface "$iface" -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/loc=/{print $2}' || true)"
  else
    c="$(curl -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/loc=/{print $2}' || true)"
  fi
  c="$(echo "$c" | tr -d '\r\n ')"
  if ! [[ "$c" =~ ^[A-Za-z]{2}$ ]]; then
    if [ -n "$iface" ]; then
      c="$(curl --interface "$iface" -s --max-time 5 https://ipinfo.io/country 2>/dev/null || true)"
    else
      c="$(curl -s --max-time 5 https://ipinfo.io/country 2>/dev/null || true)"
    fi
    c="$(echo "$c" | tr -d '\r\n ')"
  fi
  if [[ "$c" =~ ^[A-Za-z]{2}$ ]]; then
    echo "$c"
  else
    echo ""
  fi
}

get_ip() {
  local iface="${1:-}"
  local ip=""
  if [ -n "$iface" ]; then
    ip="$(curl --interface "$iface" -4 -s --max-time 5 https://api.ipify.org 2>/dev/null || true)"
    ip="$(echo "$ip" | tr -d '\r\n ')"
    if ! [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      ip="$(curl --interface "$iface" -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/ip=/{print $2}' || true)"
      ip="$(echo "$ip" | tr -d '\r\n ')"
    fi
  else
    ip="$(curl -4 -s --max-time 5 https://api.ipify.org 2>/dev/null || true)"
    ip="$(echo "$ip" | tr -d '\r\n ')"
    if ! [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      ip="$(curl -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/ip=/{print $2}' || true)"
      ip="$(echo "$ip" | tr -d '\r\n ')"
    fi
  fi
  if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "$ip"
  else
    echo "не удалось определить"
  fi
}

setup_cf_api_host() {
  echo "--- Проверка доступности Cloudflare API (api.cloudflareclient.com) ---"
  sed -i 's/ff02::2 ip6-allrouters.*/ff02::2 ip6-allrouters/' /etc/hosts 2>/dev/null || true
  sed -i '/api\.cloudflareclient\.com/d' /etc/hosts 2>/dev/null || true
  [ -n "$(tail -c1 /etc/hosts 2>/dev/null)" ] && echo "" >> /etc/hosts

  local candidates=(
    "162.159.192.1"
    "162.159.192.2"
    "188.114.96.1"
    "188.114.97.1"
    "188.114.98.1"
    "188.114.99.1"
    "162.159.193.1"
    "162.159.195.1"
    "104.16.192.82"
    "104.16.194.82"
    "104.16.24.84"
  )
  for ip in $(getent ahostsv4 api.cloudflareclient.com 2>/dev/null | awk '{print $1}' | sort -u); do
    candidates+=("$ip")
  done
  for dns in 1.1.1.1 8.8.8.8 9.9.9.9; do
    if command -v dig >/dev/null 2>&1; then
      for ip in $(dig +short @"$dns" api.cloudflareclient.com A 2>/dev/null); do
        [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && candidates+=("$ip")
      done
    fi
  done

  local unique_candidates=()
  for ip in "${candidates[@]}"; do
    if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      local seen=0
      for u in "${unique_candidates[@]}"; do
        if [ "$u" = "$ip" ]; then seen=1; break; fi
      done
      [ "$seen" -eq 0 ] && unique_candidates+=("$ip")
    fi
  done

  WORKING_API_IPS=()
  for ip in "${unique_candidates[@]}"; do
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 4 --resolve "api.cloudflareclient.com:443:$ip" "https://api.cloudflareclient.com/v0a4471/reg" 2>/dev/null || true)
    code=$(echo "$code" | tr -d '\r\n ' | tail -c 3)
    if [[ "$code" =~ ^[2-4][0-9][0-9]$ ]]; then
      WORKING_API_IPS+=("$ip")
      echo "  найден доступный IP для API: $ip (HTTP $code)"
      [ "${#WORKING_API_IPS[@]}" -ge 3 ] && break
    fi
  done

  if [ "${#WORKING_API_IPS[@]}" -gt 0 ]; then
    printf "%s\n" "${WORKING_API_IPS[@]}" > "$USQUE_DIR/api_ips.txt"
    sed -i '/api\.cloudflareclient\.com/d' /etc/hosts 2>/dev/null || true
    echo "${WORKING_API_IPS[0]} api.cloudflareclient.com" >> /etc/hosts
    echo "::1 api.cloudflareclient.com" >> /etc/hosts
  else
    echo "  внимание: ни один IP из пулов не ответил за 4с, пробуем дефолтные пулы Cloudflare"
    WORKING_API_IPS=("162.159.192.1" "162.159.192.2" "188.114.96.1")
    printf "%s\n" "${WORKING_API_IPS[@]}" > "$USQUE_DIR/api_ips.txt"
    sed -i '/api\.cloudflareclient\.com/d' /etc/hosts 2>/dev/null || true
    echo "162.159.192.1 api.cloudflareclient.com" >> /etc/hosts
    echo "::1 api.cloudflareclient.com" >> /etc/hosts
  fi
}

register_usque_config() {
  local target_conf="$1"
  local run_user="${2:-root}"
  local ok=0
  local out=""

  rm -f "$target_conf" 2>/dev/null || true

  for try_ip in "${WORKING_API_IPS[@]}"; do
    echo "  попытка регистрации через $try_ip..."
    sed -i '/api\.cloudflareclient\.com/d' /etc/hosts 2>/dev/null || true
    echo "$try_ip api.cloudflareclient.com" >> /etc/hosts
    echo "::1 api.cloudflareclient.com" >> /etc/hosts

    if [ "$run_user" = "root" ]; then
      out=$($USQUE_BIN register -a -c "$target_conf" 2>&1 || true)
    else
      out=$(su -s /bin/bash "$run_user" -c "$USQUE_BIN register -a -c $target_conf" 2>&1 || true)
    fi

    if [ -s "$target_conf" ]; then
      ok=1
      echo "  регистрация успешно завершена ($try_ip)"
      if [ -f "$USQUE_DIR/api_ips.txt" ]; then
        grep -vxF "$try_ip" "$USQUE_DIR/api_ips.txt" > "$USQUE_DIR/api_ips.txt.tmp" 2>/dev/null || true
        echo "$try_ip" | cat - "$USQUE_DIR/api_ips.txt.tmp" > "$USQUE_DIR/api_ips.txt" 2>/dev/null || true
        rm -f "$USQUE_DIR/api_ips.txt.tmp" 2>/dev/null || true
      fi
      break
    fi
    short_err="$(echo "$out" | grep -iE 'timeout|failed to register|refused|error' | tail -1)"
    [ -z "$short_err" ] && short_err="$out"
    echo "  попытка через $try_ip не удалась ($short_err)"
    sleep 1
  done

  if [ "$ok" -ne 1 ]; then
    echo "  пробуем регистрацию через системный DNS..."
    sed -i '/api\.cloudflareclient\.com/d' /etc/hosts 2>/dev/null || true
    rm -f "$target_conf" 2>/dev/null || true
    if [ "$run_user" = "root" ]; then
      out=$($USQUE_BIN register -a -c "$target_conf" 2>&1 || true)
    else
      out=$(su -s /bin/bash "$run_user" -c "$USQUE_BIN register -a -c $target_conf" 2>&1 || true)
    fi
    if [ -s "$target_conf" ]; then
      ok=1
      echo "  регистрация успешно завершена через системный DNS"
    fi
  fi

  if [ "$ok" -ne 1 ]; then
    echo "ОШИБКА: Не удалось зарегистрировать WARP через Cloudflare API!"
    echo "Вывод usque: $out"
    return 1
  fi
  return 0
}

setup_cf_api_host

if [ ! -s "$USQUE_CONF" ]; then
  echo "--- Регистрация WARP (MASQUE/QUIC) ---"
  register_usque_config "$USQUE_CONF" "root" || exit 1
fi

setup_routes_and_service() {
  cat > "$USQUE_DIR/up.sh" <<'EOF'
#!/bin/bash
TABLE=10000
NETS="91.108.0.0/16 149.154.160.0/20 95.161.64.0/20 185.76.151.0/24 185.64.92.0/22 91.105.192.0/23 194.169.233.0/24 194.102.202.0/24"
BREAKOUT="162.159.198.1 162.159.198.2"

# Реальный шлюз, чтобы QUIC до WARP не ушёл в туннель (петля).
gw=$(ip route show default | awk '/default/{print $3; exit}')
[ -n "$gw" ] && for ip in $BREAKOUT; do
  ip route replace "$ip"/32 via "$gw" 2>/dev/null || true
done

ip route replace default dev warp table $TABLE
for n in $NETS; do
  ip rule del to $n table $TABLE priority 100 2>/dev/null || true
  ip rule add to $n table $TABLE priority 100
done
ip rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip rule add fwmark $TABLE table $TABLE priority 99
iptables -t nat -C POSTROUTING -o warp -j MASQUERADE 2>/dev/null || \
  iptables -t nat -A POSTROUTING -o warp -j MASQUERADE
iptables -t mangle -C POSTROUTING -o warp -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null || \
  iptables -t mangle -A POSTROUTING -o warp -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu

# Маршрутизация IPv6 через warp
ip -6 route replace default dev warp table $TABLE 2>/dev/null || true
ip -6 rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip -6 rule add fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip6tables -t nat -C POSTROUTING -o warp -j MASQUERADE 2>/dev/null || \
  ip6tables -t nat -A POSTROUTING -o warp -j MASQUERADE 2>/dev/null || true

# Блокировка утечки IPv6: пакеты с меткой туннеля не должны уходить через интерфейсы сервера
ip6tables -C OUTPUT -m mark --mark $TABLE ! -o warp -j REJECT 2>/dev/null || \
  ip6tables -A OUTPUT -m mark --mark $TABLE ! -o warp -j REJECT 2>/dev/null || true
EOF
  chmod +x "$USQUE_DIR/up.sh"

  cat > "$USQUE_DIR/down.sh" <<'EOF'
#!/bin/bash
TABLE=10000
NETS="91.108.0.0/16 149.154.160.0/20 95.161.64.0/20 185.76.151.0/24 185.64.92.0/22 91.105.192.0/23 194.169.233.0/24 194.102.202.0/24"
for n in $NETS; do ip rule del to $n table $TABLE priority 100 2>/dev/null || true; done
ip rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip route flush table $TABLE 2>/dev/null || true
ip route del 162.159.198.1/32 2>/dev/null || true
ip route del 162.159.198.2/32 2>/dev/null || true
iptables -t nat -D POSTROUTING -o warp -j MASQUERADE 2>/dev/null || true
iptables -t mangle -D POSTROUTING -o warp -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null || true

ip -6 rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip -6 route flush table $TABLE 2>/dev/null || true
ip6tables -t nat -D POSTROUTING -o warp -j MASQUERADE 2>/dev/null || true
ip6tables -D OUTPUT -m mark --mark $TABLE ! -o warp -j REJECT 2>/dev/null || true

ip link set dev warp down 2>/dev/null || true
ip link del dev warp 2>/dev/null || true
EOF
  chmod +x "$USQUE_DIR/down.sh"

  cat > /etc/systemd/system/usque.service <<EOF
[Unit]
Description=Cloudflare WARP via usque (MASQUE/QUIC)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=/bin/sh -c 'ip link del dev warp 2>/dev/null || true'
ExecStart=$USQUE_BIN nativetun -c $USQUE_CONF -n $IFACE --always-reconnect --keepalive-period 10s --on-connect $USQUE_DIR/up.sh --on-disconnect $USQUE_DIR/down.sh
ExecStopPost=/bin/sh -c 'ip link del dev warp 2>/dev/null || true'
Restart=always
RestartSec=3
TimeoutStopSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
}

install_rotate_script() {
  cat > /usr/local/bin/warp-rotate.sh <<'ROTEOF'
#!/bin/bash
exec 8>/run/warp-rotate.lock
flock -n 8 || exit 0
trap 'rm -f /run/warp-rotate.lock' EXIT

USQUE_BIN=/usr/local/bin/usque
USQUE_DIR=/etc/usque
USQUE_CONF=/etc/usque/config.json
IFACE=warp
LOG=/var/log/warp-watch.log
[ -f /etc/usque/warp.env ] && . /etc/usque/warp.env

MAX_TRY="${MAX_TRY:-20}"
MAX_LATENCY="${MAX_LATENCY:-5}"
CHECK_TARGET="${CHECK_URL:-https://api.telegram.org/}"

log() {
  if [ "${ROTATE_LOG_TO_WATCH:-1}" = "1" ]; then
    echo "$(date '+%F %T') [rotate] $*" | tee -a "$LOG"
  else
    echo "$(date '+%F %T') [rotate] $*"
  fi
}

probe() {
  if ! curl --interface "$IFACE" -4 -s -o /dev/null --max-time 2 https://1.1.1.1/ 2>/dev/null; then
    echo "000 999"
    return
  fi
  curl --interface "$IFACE" -4 -sS -o /dev/null \
    -w '%{http_code} %{time_total}\n' --max-time "$MAX_LATENCY" "$CHECK_TARGET" 2>/dev/null \
    || echo "000 999"
}

good() {
  case "$1" in 2??|3??|401|403) ;; *) return 1;; esac
  awk -v t="$2" -v m="$MAX_LATENCY" 'BEGIN{exit !(t <= m)}'
}

FORCE=0
[ "${1:-}" = "--force" ] && FORCE=1

if [ "$FORCE" -eq 0 ]; then
  read -r code t < <(probe) || true
  if good "${code:-000}" "${t:-999}"; then
    log "Рабочий IP уже активен (code=$code, ${t}s) — ротация не нужна"
    exit 0
  fi
fi

attempt=0
while [ "$attempt" -lt "$MAX_TRY" ]; do
  attempt=$((attempt + 1))
  log "попытка $attempt/$MAX_TRY: подбор рабочего IP"
  systemctl stop usque 2>/dev/null || true
  sleep 1
  ip link del "$IFACE" 2>/dev/null || true
  ok=0
  ips=()
  [ -f "$USQUE_DIR/api_ips.txt" ] && mapfile -t ips < "$USQUE_DIR/api_ips.txt" 2>/dev/null || true
  [ "${#ips[@]}" -eq 0 ] && ips=("162.159.192.1" "162.159.192.2" "188.114.96.1" "104.16.24.84")
  for try_ip in "${ips[@]}"; do
    sed -i '/api\.cloudflareclient\.com/d' /etc/hosts 2>/dev/null || true
    echo "$try_ip api.cloudflareclient.com" >> /etc/hosts
    echo "::1 api.cloudflareclient.com" >> /etc/hosts
    rm -f "$USQUE_DIR/config.json.tmp" 2>/dev/null || true
    out=$($USQUE_BIN register -a -c "$USQUE_DIR/config.json.tmp" 2>&1 || true)
    if [ -s "$USQUE_DIR/config.json.tmp" ]; then
      mv -f "$USQUE_DIR/config.json.tmp" "$USQUE_CONF"
      ok=1
      if [ -f "$USQUE_DIR/api_ips.txt" ]; then
        grep -vxF "$try_ip" "$USQUE_DIR/api_ips.txt" > "$USQUE_DIR/api_ips.txt.tmp" 2>/dev/null || true
        echo "$try_ip" | cat - "$USQUE_DIR/api_ips.txt.tmp" > "$USQUE_DIR/api_ips.txt" 2>/dev/null || true
        rm -f "$USQUE_DIR/api_ips.txt.tmp" 2>/dev/null || true
      fi
      break
    fi
    short_err="$(echo "$out" | grep -iE 'timeout|failed to register|refused|error' | tail -1)"
    [ -z "$short_err" ] && short_err="$(echo "$out" | tail -1)"
    log "регистрация ($try_ip) не удалась ($short_err)"
    sleep 1
  done
  if [ "$ok" != 1 ] || [ ! -s "$USQUE_CONF" ]; then log "регистрация не удалась"; continue; fi
  systemctl start usque

  for j in $(seq 1 5); do
    if [ -e /sys/class/net/"$IFACE" ] && curl --interface "$IFACE" -4 -s -o /dev/null --max-time 1 https://1.1.1.1/ 2>/dev/null; then
      break
    fi
    sleep 1
  done

  read -r code t < <(probe) || true
  if good "${code:-000}" "${t:-999}"; then
    new_ip="$(curl --interface "$IFACE" -4 -s --max-time 5 https://api.ipify.org 2>/dev/null || true)"
    if ! [[ "$new_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      new_ip="$(curl --interface "$IFACE" -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/ip=/{print $2}' || true)"
    fi
    new_ip="$(echo "$new_ip" | tr -d '\r\n ')"
    if ! [[ "$new_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then new_ip="unknown"; fi
    log "успех: рабочий IP $new_ip (попытка $attempt, code=$code, ${t}s)"
    exit 0
  fi
  log "IP не отвечает или превышен лимит (code=${code:-000}, ${t}s, лимит ${MAX_LATENCY}s)"
done
log "не удалось подобрать рабочий IP за $MAX_TRY попыток"
exit 1
ROTEOF
  chmod +x /usr/local/bin/warp-rotate.sh
}

install_watch_cron() {
  [ -f /etc/usque/warp.env ] && . /etc/usque/warp.env
  CHECK_INTERVAL="${CHECK_INTERVAL:-5}"
  cat > /usr/local/bin/warp-watch.sh <<'WATCHEOF'
#!/bin/bash
# Сторож WARP-туннеля (usque).
#  - туннель не отвечает 2 проверки подряд   -> рестарт usque;
#  - проверка недоступна 3 проверки подряд   -> подбор рабочего IP (если включена автосмена);
#  - проверка медленная (>=MAX_LATENCY) 3 проверки подряд -> подбор рабочего IP.
[ -f /etc/usque/warp.env ] && . /etc/usque/warp.env
IFACE=warp
LOG=/var/log/warp-watch.log
STATE=/run/warp-watch.state
NEUTRAL=https://1.1.1.1/
TABLE=10000
CHECK_TARGET="${CHECK_URL:-https://api.telegram.org/}"
MAX_LATENCY="${MAX_LATENCY:-5}"
AUTO_ROTATE="${AUTO_ROTATE:-1}"

exec 9>/run/warp-watch.lock
flock -n 9 || exit 0

# Не запускаться во время установки или ротации
if [ -f /run/warp-install.lock ] || [ -f /run/warp-rotate.lock ] || pgrep -f "warp-rotate.sh" >/dev/null 2>&1; then
  exit 0
fi

# Если сервис остановлен пользователем — не перезапускать
if ! systemctl is-active --quiet usque 2>/dev/null; then
  exit 0
fi

log() { echo "$(date '+%F %T') $*" >> "$LOG"; }
get_code() {
  curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code}' --max-time 10 "$1" 2>/dev/null || true
}
probe_target() {
  curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code} %{time_total}\n' \
    --max-time "$((MAX_LATENCY + 2))" "$CHECK_TARGET" 2>/dev/null || echo "000 999"
}
is_ok() { case "$1" in 2??|3??|401|403) return 0;; *) return 1;; esac; }
is_fast() { awk -v t="$1" -v m="$MAX_LATENCY" 'BEGIN{exit !(t <= m)}'; }

fails_tunnel=0; fails_target=0; fails_slow=0; last_action=0
[ -f "$STATE" ] && . "$STATE" 2>/dev/null || true

now=$(date +%s)
neutral=$(get_code "$NEUTRAL")
read -r code t < <(probe_target) || true

if is_ok "$neutral" && is_ok "$code" && is_fast "$t"; then
  log "проверка OK: туннель=$neutral цель=$code (${t}s, лимит ${MAX_LATENCY}s)"
  echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$last_action" > "$STATE"
  exit 0
fi

if ! is_ok "$neutral"; then
  fails_tunnel=$((fails_tunnel + 1)); fails_target=0; fails_slow=0
  log "туннель НЕ отвечает (code=${neutral:-000}) попытка=$fails_tunnel"
elif ! is_ok "$code"; then
  fails_target=$((fails_target + 1)); fails_slow=0
  log "туннель OK (${neutral}), цель недоступна (code=${code:-000}) попытка=$fails_target"
else
  fails_slow=$((fails_slow + 1)); fails_target=0; fails_tunnel=0
  log "цель отвечает медленно (${t}s >= ${MAX_LATENCY}s) попытка=$fails_slow"
fi

if [ $((now - last_action)) -lt 180 ]; then
  echo "fails_tunnel=$fails_tunnel fails_target=$fails_target fails_slow=$fails_slow last_action=$last_action" > "$STATE"
  exit 0
fi

# Туннель лёг 2 проверки подряд: рестарт usque.
if [ "$fails_tunnel" -ge 2 ]; then
  log "туннель не отвечает ($fails_tunnel), перезапускаю usque"
  systemctl restart usque
  sleep 10
  neutral=$(get_code "$NEUTRAL"); read -r code t < <(probe_target) || true
  if is_ok "$neutral" && is_ok "$code" && is_fast "$t"; then
    log "после рестарта рабочий IP активен (туннель=$neutral цель=$code ${t}s)"
    echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$now" > "$STATE"
    exit 0
  fi
  if [ "$AUTO_ROTATE" = "1" ]; then
    log "рестарт не помог, подбираю рабочий IP"
    [ -x /usr/local/bin/warp-rotate.sh ] && bash /usr/local/bin/warp-rotate.sh >/dev/null 2>&1
  else
    log "рестарт не помог, автосмена IP отключена"
  fi
  echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$now" > "$STATE"
  exit 0
fi

# Недоступен (3 нет) или стабильно медленный (3 медленно): подбираем рабочий IP.
if [ "$fails_target" -ge 3 ] || [ "$fails_slow" -ge 3 ]; then
  if [ "$AUTO_ROTATE" = "1" ]; then
    log "нерабочий IP (fails=$fails_target slow_fails=$fails_slow) — подбираю рабочий IP"
    [ -x /usr/local/bin/warp-rotate.sh ] && bash /usr/local/bin/warp-rotate.sh >/dev/null 2>&1
  else
    log "нерабочий IP (fails=$fails_target slow_fails=$fails_slow), автосмена выключена — перезапускаю usque"
    systemctl restart usque
  fi
  echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$now" > "$STATE"
  exit 0
fi

echo "fails_tunnel=$fails_tunnel fails_target=$fails_target fails_slow=$fails_slow last_action=$last_action" > "$STATE"
WATCHEOF
  chmod +x /usr/local/bin/warp-watch.sh
  touch /var/log/warp-watch.log
  chmod 0644 /var/log/warp-watch.log

  # systemd timer
  cat > /etc/systemd/system/warp-watch.service <<'EOF'
[Unit]
Description=Cloudflare WARP Tunnel Watchdog
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/warp-watch.sh
EOF

  cat > /etc/systemd/system/warp-watch.timer <<EOF
[Unit]
Description=Run Cloudflare WARP Tunnel Watchdog periodically

[Timer]
OnBootSec=1min
OnUnitActiveSec=${CHECK_INTERVAL}min
AccuracySec=10s

[Install]
WantedBy=timers.target
EOF

  systemctl daemon-reload
  systemctl enable --now warp-watch.timer 2>/dev/null || true

  # Резервный запуск через cron (если systemd-таймер не поддерживается)
  (crontab -l 2>/dev/null | grep -v "warp-watch.sh" || true; echo "*/${CHECK_INTERVAL} * * * * /usr/local/bin/warp-watch.sh") | crontab - 2>/dev/null || true
}

setup_routes_and_service
install_rotate_script
install_watch_cron

systemctl enable --now usque 2>/dev/null || true
sleep 3

echo "--- Подбор рабочего WARP IP ---"
ROTATE_LOG_TO_WATCH=0 MAX_LATENCY="${MAX_LATENCY:-5}" bash /usr/local/bin/warp-rotate.sh --force || true

WARP_IP="$(get_ip "$IFACE")"
WARP_COUNTRY="$(get_country "$IFACE")"
echo "  WARP IP: $WARP_IP${WARP_COUNTRY:+ (страна: $WARP_COUNTRY)}"

CHECK_TARGET="${CHECK_URL:-https://api.telegram.org/}"
read -r CODE t < <(curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code} %{time_total}\n' --max-time 6 "$CHECK_TARGET" 2>/dev/null || echo "000 999") || true
echo "  $CHECK_TARGET → HTTP ${CODE:-000} (${t:-0}s, лимит ${MAX_LATENCY:-5}s)"

CHECK_INTERVAL="${CHECK_INTERVAL:-5}"
echo "--- Сторож WARP ---"
echo "  сторож установлен: каждые ${CHECK_INTERVAL} мин"

# Прогон сторожа сразу для начального заполнения лога
bash /usr/local/bin/warp-watch.sh 2>/dev/null || true

LOG=/var/log/warp-watch.log
touch "$LOG" 2>/dev/null || true
{
  echo ""
  echo "========================================="
  echo "  Установка WARP завершена $(date '+%F %T')"
  echo "  WARP IP: $WARP_IP${WARP_COUNTRY:+ (страна: $WARP_COUNTRY)}"
  echo "  Сторож: каждые ${CHECK_INTERVAL} мин, цель: ${CHECK_TARGET:-https://api.telegram.org/}"
  echo "========================================="
  echo ""
} >> "$LOG"

echo "--- Установка WARP успешно завершена ---"
