#!/bin/bash
set -euo pipefail

USQUE_BIN=/usr/local/bin/usque
USQUE_VER=4.2.1
USQUE_DIR=/etc/usque
USQUE_CONF=$USQUE_DIR/config.json
IFACE=warp
TABLE=10000

# Telegram DC / MTProto ranges (IPv4)
TG_NETS="91.108.0.0/16 149.154.160.0/20 95.161.64.0/20 185.76.151.0/24 185.64.92.0/22 91.105.192.0/23 194.169.233.0/24 194.102.202.0/24"

echo "[1/6] Инструменты..."
apt-get update -y
DEBIAN_FRONTEND=noninteractive apt-get install -y curl unzip iproute2 iptables ca-certificates

echo "[2/6] Старый WireGuard-WARP убираем (на этом хостере его payload режется DPI)..."
wg-quick down warp 2>/dev/null || true
systemctl disable --now wg-quick@warp 2>/dev/null || true
ip rule del to 91.108.0.0/16 table $TABLE priority 100 2>/dev/null || true

echo "[3/6] usque (WARP через MASQUE/QUIC)..."
if [ ! -x "$USQUE_BIN" ]; then
  curl -fsSL -o /tmp/usque.zip \
    "https://github.com/Diniboy1123/usque/releases/download/v${USQUE_VER}/usque_${USQUE_VER}_linux_amd64.zip"
  unzip -o /tmp/usque.zip -d /tmp/usque >/dev/null
  install -m 0755 /tmp/usque/usque "$USQUE_BIN"
  rm -rf /tmp/usque /tmp/usque.zip
fi
$USQUE_BIN version 2>/dev/null || true

echo "[4/6] Регистрация WARP (IPv4-фикс для API)..."
mkdir -p "$USQUE_DIR"
# usque — Go-бинарник, /etc/gai.conf игнорирует: форсим IPv4 для API Cloudflare.
if ! grep -q 'api\.cloudflareclient\.com' /etc/hosts 2>/dev/null; then
  IP4="$(getent ahostsv4 api.cloudflareclient.com | awk 'NR==1{print $1}')"
  [ -n "$IP4" ] && echo "$IP4 api.cloudflareclient.com" >> /etc/hosts
fi

if [ ! -f "$USQUE_CONF" ]; then
  ok=0
  for i in 1 2 3 4 5; do
    if $USQUE_BIN register -a -c "$USQUE_CONF"; then ok=1; break; fi
    echo "  register: попытка $i не удалась, повтор через 3с..."; sleep 3
  done
  [ "$ok" = 1 ] && [ -f "$USQUE_CONF" ] || { echo "usque register FAILED"; exit 1; }
fi

echo "[5/6] Маршруты, служба, ротация и сторож..."

# Хуки + systemd-юнит. Вызывается и при первом запуске, и после очистки каталога.
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
iptables -t nat -C POSTROUTING -o warp -j MASQUERADE 2>/dev/null || \
  iptables -t nat -A POSTROUTING -o warp -j MASQUERADE
EOF
  chmod +x "$USQUE_DIR/up.sh"

  cat > "$USQUE_DIR/down.sh" <<'EOF'
#!/bin/bash
TABLE=10000
NETS="91.108.0.0/16 149.154.160.0/20 95.161.64.0/20 185.76.151.0/24 185.64.92.0/22 91.105.192.0/23 194.169.233.0/24 194.102.202.0/24"
for n in $NETS; do ip rule del to $n table $TABLE priority 100 2>/dev/null || true; done
ip route flush table $TABLE 2>/dev/null || true
ip route del 162.159.198.1/32 2>/dev/null || true
ip route del 162.159.198.2/32 2>/dev/null || true
iptables -t nat -D POSTROUTING -o warp -j MASQUERADE 2>/dev/null || true
EOF
  chmod +x "$USQUE_DIR/down.sh"

  cat > /etc/systemd/system/usque.service <<EOF
[Unit]
Description=Cloudflare WARP via usque (MASQUE/QUIC)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$USQUE_BIN nativetun -c $USQUE_CONF -n $IFACE --always-reconnect --keepalive-period 10s --on-connect $USQUE_DIR/up.sh --on-disconnect $USQUE_DIR/down.sh
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
}

# Ротация WARP-IP: используется и при первичном подборе, и сторожем.
# Только регистрация + рестарт службы + проверка TG. Никаких apt/cron.
install_rotate_script() {
  cat > /usr/local/bin/warp-rotate.sh <<'ROTEOF'
#!/bin/bash
USQUE_BIN=/usr/local/bin/usque
USQUE_DIR=/etc/usque
USQUE_CONF=/etc/usque/config.json
IFACE=warp
LOG=/var/log/warp-watch.log
MAX_TRY="${MAX_TRY:-8}"
# Максимальная допустимая задержка до api.telegram.org через туннель (сек).
# Всё медленнее считается "плохим IP" и ротируется, даже если код 2xx/3xx.
MAX_LATENCY="${MAX_LATENCY:-5}"
log() { echo "$(date '+%F %T') [rotate] $*" >> "$LOG"; }

# Печатает "<http_code> <time_total>" (time_total в секундах, с точкой).
probe() {
  curl --interface "$IFACE" -4 -sS -o /dev/null \
    -w '%{http_code} %{time_total}' --max-time 20 https://api.telegram.org/ 2>/dev/null \
    || echo "000 999"
}
# $1=code $2=time_total
good() {
  case "$1" in 2??|3??|401|403) ;; *) return 1;; esac
  awk -v t="$2" -v m="$MAX_LATENCY" 'BEGIN{exit !(t <= m)}'
}

read -r code t < <(probe)
if good "$code" "$t"; then
  log "TG уже доступен (code=$code, ${t}s) — ротация не нужна"
  exit 0
fi

attempt=0
while [ "$attempt" -lt "$MAX_TRY" ]; do
  attempt=$((attempt + 1))
  log "попытка $attempt/$MAX_TRY: новая регистрация"
  systemctl stop usque 2>/dev/null || true
  sleep 1
  ip link del "$IFACE" 2>/dev/null || true
  rm -f "$USQUE_DIR"/config.json* 2>/dev/null || true
  mkdir -p "$USQUE_DIR"
  ok=0
  for i in 1 2 3 4 5; do
    if $USQUE_BIN register -a -c "$USQUE_CONF" >>"$LOG" 2>&1; then ok=1; break; fi
    log "register: попытка $i не удалась"; sleep 3
  done
  if [ "$ok" != 1 ] || [ ! -f "$USQUE_CONF" ]; then log "register FAILED"; continue; fi
  systemctl start usque
  for j in $(seq 1 25); do
    if [ -e /sys/class/net/"$IFACE" ] && ping -I "$IFACE" -c 1 -W 2 1.1.1.1 >/dev/null 2>&1; then break; fi
    sleep 2
  done
  read -r code t < <(probe)
  if good "$code" "$t"; then
    log "успех: TG доступен через новый IP (попытка $attempt, code=$code, ${t}s)"
    exit 0
  fi
  log "новый IP не годится (code=${code:-000}, ${t}s, лимит ${MAX_LATENCY}s)"
done
log "не удалось подобрать рабочий IP за $MAX_TRY попыток"
exit 1
ROTEOF
  chmod +x /usr/local/bin/warp-rotate.sh
}

# Сторож: каждые 5 минут. Туннель лёг -> рестарт usque. Telegram лёг при живом
# туннеле -> сразу ротация IP (рестарт не меняет IP и только рвёт сессии).
install_watch_cron() {
  cat > /usr/local/bin/warp-watch.sh <<'WATCHEOF'
#!/bin/bash
# Сторож WARP-туннеля (usque).
#  - туннель не отвечает 2 проверки подряд   -> рестарт usque;
#  - Telegram недоступен 3 проверки подряд   -> ротация WARP-IP;
#  - Telegram отвечает, но медленно (>=MAX_LATENCY) 3 проверки подряд -> ротация.
IFACE=warp
LOG=/var/log/warp-watch.log
STATE=/run/warp-watch.state
NEUTRAL=https://1.1.1.1/
TG=https://api.telegram.org/
MAX_LATENCY="${MAX_LATENCY:-5}"

exec 9>/run/warp-watch.lock
flock -n 9 || exit 0

log() { echo "$(date '+%F %T') $*" >> "$LOG"; }
get_code() {
  curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code}' --max-time 10 "$1" 2>/dev/null || true
}
probe_tg() {
  curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code} %{time_total}' \
    --max-time 20 "$TG" 2>/dev/null || echo "000 999"
}
is_ok() { case "$1" in 2??|3??|401|403) return 0;; *) return 1;; esac; }
is_fast() { awk -v t="$1" -v m="$MAX_LATENCY" 'BEGIN{exit !(t <= m)}'; }

fails_tunnel=0; fails_tg=0; fails_slow=0; last_action=0
[ -f "$STATE" ] && . "$STATE" 2>/dev/null || true

now=$(date +%s)
neutral=$(get_code "$NEUTRAL")
read -r tg t < <(probe_tg)

if is_ok "$neutral" && is_ok "$tg" && is_fast "$t"; then
  if [ "${fails_tunnel:-0}" != 0 ] || [ "${fails_tg:-0}" != 0 ] || [ "${fails_slow:-0}" != 0 ]; then
    log "снова всё доступно (tunnel=$neutral tg=$tg ${t}s)"
  fi
  echo "fails_tunnel=0 fails_tg=0 fails_slow=0 last_action=$last_action" > "$STATE"
  exit 0
fi

if ! is_ok "$neutral"; then
  fails_tunnel=$((fails_tunnel + 1)); fails_tg=0; fails_slow=0
  log "туннель НЕ отвечает (code=${neutral:-000}) попытка=$fails_tunnel"
elif ! is_ok "$tg"; then
  fails_tg=$((fails_tg + 1)); fails_slow=0
  log "туннель OK (${neutral}), Telegram нет (code=${tg:-000}) попытка=$fails_tg"
else
  # код хороший, но медленно
  fails_slow=$((fails_slow + 1)); fails_tg=0; fails_tunnel=0
  log "Telegram отвечает, но медленно (${t}s >= ${MAX_LATENCY}s) попытка=$fails_slow"
fi

if [ $((now - last_action)) -lt 180 ]; then
  echo "fails_tunnel=$fails_tunnel fails_tg=$fails_tg fails_slow=$fails_slow last_action=$last_action" > "$STATE"
  exit 0
fi

# Туннель лёг 2 проверки подряд: рестарт usque.
if [ "$fails_tunnel" -ge 2 ]; then
  log "туннель мёртв ($fails_tunnel), перезапускаю usque"
  systemctl restart usque
  sleep 20
  neutral=$(get_code "$NEUTRAL"); read -r tg t < <(probe_tg)
  if is_ok "$neutral" && is_ok "$tg" && is_fast "$t"; then
    log "после рестарта OK (tunnel=$neutral tg=$tg ${t}s)"
    echo "fails_tunnel=0 fails_tg=0 fails_slow=0 last_action=$now" > "$STATE"
    exit 0
  fi
  log "рестарт не помог, ротирую WARP-IP"
  [ -x /usr/local/bin/warp-rotate.sh ] && bash /usr/local/bin/warp-rotate.sh >/dev/null 2>&1
  echo "fails_tunnel=0 fails_tg=0 fails_slow=0 last_action=$now" > "$STATE"
  exit 0
fi

# Забанен (3 нет) или стабильно медленный (3 медленно): ротируем IP.
if [ "$fails_tg" -ge 3 ] || [ "$fails_slow" -ge 3 ]; then
  log "плохой IP (tg_fails=$fails_tg slow_fails=$fails_slow) — ротирую WARP-IP"
  [ -x /usr/local/bin/warp-rotate.sh ] && bash /usr/local/bin/warp-rotate.sh >/dev/null 2>&1
  echo "fails_tunnel=0 fails_tg=0 fails_slow=0 last_action=$now" > "$STATE"
  exit 0
fi

echo "fails_tunnel=$fails_tunnel fails_tg=$fails_tg fails_slow=$fails_slow last_action=$last_action" > "$STATE"
WATCHEOF
  chmod +x /usr/local/bin/warp-watch.sh
  tmp=$(mktemp)
  crontab -l 2>/dev/null | grep -v 'warp-watch.sh' > "$tmp" || true
  echo '*/5 * * * * /usr/local/bin/warp-watch.sh' >> "$tmp"
  crontab "$tmp"
  rm -f "$tmp"
}

setup_routes_and_service
install_rotate_script
install_watch_cron

systemctl enable --now usque
sleep 8

echo "[6/6] Первичный подбор незаблокированного WARP-IP..."
MAX_LATENCY="${MAX_LATENCY:-5}" bash /usr/local/bin/warp-rotate.sh || true

read -r CODE t < <(curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code} %{time_total}' --max-time 20 https://api.telegram.org/ 2>/dev/null || echo "000 999")
echo "  api.telegram.org через $IFACE -> HTTP $CODE (${t}s, лимит ${MAX_LATENCY}s)"
CODE_P=$(curl -4 -sS -o /dev/null -w '%{http_code}' --max-time 12 https://api.telegram.org/ || true)
echo "  api.telegram.org policy-route -> HTTP $CODE_P"
echo "  сторож: $(crontab -l 2>/dev/null | grep warp-watch || echo 'НЕ УСТАНОВЛЕН')"
echo "  ротация: /usr/local/bin/warp-rotate.sh"
echo "  лог:    tail -f /var/log/warp-watch.log"
