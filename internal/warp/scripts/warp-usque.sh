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

# Настройки из панели (пишутся до запуска установщика): лимит задержки,
# интервал сторожа, цель проверки и автосмена IP.
[ -f /etc/usque/warp.env ] && . /etc/usque/warp.env
MAX_LATENCY="${MAX_LATENCY:-5}"

# Telegram DC / MTProto ranges (IPv4)
TG_NETS="91.108.0.0/16 149.154.160.0/20 95.161.64.0/20 185.76.151.0/24 185.64.92.0/22 91.105.192.0/23 194.169.233.0/24 194.102.202.0/24"

apt-get update -y
DEBIAN_FRONTEND=noninteractive apt-get install -y cron curl unzip iproute2 iptables ca-certificates dnsutils openssl 2>/dev/null || \
  DEBIAN_FRONTEND=noninteractive apt-get install -y cron curl unzip iproute2 iptables ca-certificates openssl
systemctl enable --now cron 2>/dev/null || systemctl enable --now crond 2>/dev/null || true

wg-quick down warp 2>/dev/null || true
systemctl disable --now wg-quick@warp 2>/dev/null || true
systemctl stop usque 2>/dev/null || true
ip link set dev warp down 2>/dev/null || true
ip link del dev warp 2>/dev/null || true
ip rule del to 91.108.0.0/16 table $TABLE priority 100 2>/dev/null || true
ip rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true

if [ ! -x "$USQUE_BIN" ]; then
  TMP_DIR=$(mktemp -d)
  case "$(uname -m)" in
    x86_64|amd64)
      USQUE_ARCH=amd64
      EXPECTED_HASH="4117e20695078af9c11edecd1a826c009bbc7ea0b7f64458612b4198910bc313"
      ;;
    aarch64|arm64)
      USQUE_ARCH=arm64
      EXPECTED_HASH="c88b061c2a567f30813d7505637d1fb6fe7ec5898b5b61fd05122409fa5ad925"
      ;;
    *)
      echo "ERROR: usque не поддерживает архитектуру $(uname -m)" >&2
      exit 1
      ;;
  esac
  curl -fsSL -o "$TMP_DIR/usque.zip" \
    "https://github.com/Diniboy1123/usque/releases/download/v${USQUE_VER}/usque_${USQUE_VER}_linux_${USQUE_ARCH}.zip"

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
    c="$(curl -4 --interface "$iface" -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/loc=/{print $2}' || true)"
  else
    c="$(curl -4 -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/loc=/{print $2}' || true)"
  fi
  c="$(echo "$c" | tr -d '\r\n ')"
  if ! [[ "$c" =~ ^[A-Za-z]{2}$ ]]; then
    if [ -n "$iface" ]; then
      c="$(curl -4 --interface "$iface" -s --max-time 5 https://ipinfo.io/country 2>/dev/null || true)"
    else
      c="$(curl -4 -s --max-time 5 https://ipinfo.io/country 2>/dev/null || true)"
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
      ip="$(curl -4 --interface "$iface" -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/ip=/{print $2}' || true)"
      ip="$(echo "$ip" | tr -d '\r\n ')"
    fi
  else
    ip="$(curl -4 -s --max-time 5 https://api.ipify.org 2>/dev/null || true)"
    ip="$(echo "$ip" | tr -d '\r\n ')"
    if ! [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      ip="$(curl -4 -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | awk -F= '/ip=/{print $2}' || true)"
      ip="$(echo "$ip" | tr -d '\r\n ')"
    fi
  fi
  if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "$ip"
  else
    echo "не удалось определить"
  fi
}

install_api_helper() {
  cat > /usr/local/bin/warp-api-host.sh <<'APIEOF'
#!/bin/bash
# Keeps api.cloudflareclient.com pinned in /etc/hosts only to addresses that
# present a certificate valid for that name. WARP endpoint IPs (162.159.192.0/24,
# 188.114.96.0/22) are not API hosts on every network: some providers answer
# them with a foreign certificate and usque registration dies with a TLS error.
#
# A loopback pin (::1 api.cloudflareclient.com) is deliberately not used: on a
# host that serves TLS on [::1]:443 (e.g. a panel inbound) it makes the client
# talk to that listener instead of Cloudflare.
set -uo pipefail

NAME=api.cloudflareclient.com
IPS_FILE=/etc/usque/api_ips.txt
MODE="${1:-probe}"

log() { echo "$(date '+%F %T') [api-host] $*"; }

clean_hosts() {
  # The temp file must live outside /etc: the panel sandbox mounts /etc
  # read-only and reopens only /etc/hosts, so a temp beside it fails and the
  # cleanup would silently do nothing (pins then pile up on every rotation).
  local tmp
  tmp=$(mktemp /tmp/ex3-hosts.XXXXXX 2>/dev/null) || return 0
  awk -v n="$NAME" '{ keep = 1; for (i = 2; i <= NF; i++) if ($i == n) keep = 0; if (keep) print }' \
    /etc/hosts > "$tmp" 2>/dev/null || { rm -f "$tmp"; return 0; }
  if [ -s "$tmp" ]; then cat "$tmp" > /etc/hosts; fi
  rm -f "$tmp"
}

pin_hosts() {
  local ip="${1:-}"
  clean_hosts
  [ -z "$ip" ] && return 0
  if ! [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log "некорректный IP: $ip"
    return 1
  fi
  echo "$ip $NAME" >> /etc/hosts
}

probe_ip() {
  local ip="$1" code
  [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
  code=$(curl -4 -s -o /dev/null -w '%{http_code}' --max-time 4 --resolve "$NAME:443:$ip" \
    "https://$NAME/v0a4471/reg" 2>/dev/null || true)
  code=$(printf '%s' "$code" | tr -dc '0-9' | tail -c 3)
  case "$code" in 2??|3??|4??) return 0 ;; esac
  return 1
}

collect_candidates() {
  {
    getent ahostsv4 "$NAME" 2>/dev/null | awk '{print $1}'
    for dns in 1.1.1.1 8.8.8.8 9.9.9.9; do
      command -v dig >/dev/null 2>&1 && dig +short +time=2 +tries=1 @"$dns" "$NAME" A 2>/dev/null
    done
    # Anycast API addresses first, the WARP endpoint pool as a last resort: it
    # only survives the certificate check where the network really routes it.
    echo "104.16.192.82"
    echo "104.16.194.82"
    echo "104.16.24.84"
    echo "162.159.192.1"
    echo "162.159.192.2"
    echo "188.114.96.1"
  } | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | awk '!seen[$0]++'
}

probe() {
  local ip verified=()
  while read -r ip; do
    if probe_ip "$ip"; then
      verified+=("$ip")
      [ "${#verified[@]}" -ge 3 ] && break
    fi
  done < <(collect_candidates)
  mkdir -p "$(dirname "$IPS_FILE")" 2>/dev/null || true
  if [ "${#verified[@]}" -eq 0 ]; then
    pin_hosts ""
    rm -f "$IPS_FILE"
    log "нет IP с валидным сертификатом — работаем через системный DNS"
    return 1
  fi
  clean_hosts
  for ip in "${verified[@]}"; do echo "$ip $NAME" >> /etc/hosts; done
  printf '%s\n' "${verified[@]}" > "$IPS_FILE"
  log "API IP: ${verified[*]}"
}

case "$MODE" in
  probe) probe ;;
  clear) pin_hosts "" ;;
  pin) pin_hosts "${2:-}" ;;
  *) echo "usage: $0 probe|clear|pin <ip>" >&2; exit 2 ;;
esac
APIEOF
  chmod +x /usr/local/bin/warp-api-host.sh
}

setup_cf_api_host() {
  echo "--- Проверка доступности Cloudflare API (api.cloudflareclient.com) ---"
  install_api_helper
  /usr/local/bin/warp-api-host.sh probe || true
  WORKING_API_IPS=()
  if [ -s "$USQUE_DIR/api_ips.txt" ]; then
    mapfile -t WORKING_API_IPS < "$USQUE_DIR/api_ips.txt"
  fi
}

register_usque_config() {
  local target_conf="$1"
  local run_user="${2:-root}"
  local ok=0 out="" try_ip
  local ips=()

  rm -f "$target_conf" 2>/dev/null || true
  if [ -s "$USQUE_DIR/api_ips.txt" ]; then
    mapfile -t ips < "$USQUE_DIR/api_ips.txt"
  fi

  # Verified API addresses first; an empty pin means the plain system resolver.
  local -a pins=("${ips[@]}")
  pins+=("")

  for try_ip in "${pins[@]}"; do
    if [ -n "$try_ip" ]; then
      echo "  попытка регистрации через $try_ip..."
      /usr/local/bin/warp-api-host.sh pin "$try_ip" >/dev/null 2>&1 || true
    else
      echo "  попытка регистрации через системный DNS..."
      /usr/local/bin/warp-api-host.sh clear >/dev/null 2>&1 || true
    fi

    if [ "$run_user" = "root" ]; then
      out=$(GODEBUG="tlsmlkem=0,tlskyber=0" $USQUE_BIN register -a -c "$target_conf" 2>&1 || true)
    else
      out=$(su -s /bin/bash "$run_user" -c "GODEBUG=tlsmlkem=0,tlskyber=0 $USQUE_BIN register -a -c $target_conf" 2>&1 || true)
    fi

    if [ -s "$target_conf" ]; then
      ok=1
      echo "  регистрация успешно завершена (${try_ip:-системный DNS})"
      if [ -n "$try_ip" ] && [ -s "$USQUE_DIR/api_ips.txt" ]; then
        grep -vxF "$try_ip" "$USQUE_DIR/api_ips.txt" > "$USQUE_DIR/api_ips.txt.tmp" 2>/dev/null || true
        echo "$try_ip" | cat - "$USQUE_DIR/api_ips.txt.tmp" > "$USQUE_DIR/api_ips.txt" 2>/dev/null || true
        rm -f "$USQUE_DIR/api_ips.txt.tmp" 2>/dev/null || true
      fi
      break
    fi
    short_err="$(echo "$out" | grep -iE 'timeout|failed to register|refused|error' | tail -1)"
    [ -z "$short_err" ] && short_err="$out"
    echo "  попытка через ${try_ip:-системный DNS} не удалась ($short_err)"
    rm -f "$target_conf" 2>/dev/null || true
    sleep 1
  done

  if [ "$ok" -ne 1 ]; then
    /usr/local/bin/warp-api-host.sh clear >/dev/null 2>&1 || true
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
BREAKOUT="162.159.198.1"
[ -s /etc/usque/endpoint ] && BREAKOUT="$BREAKOUT $(tr '\n' ' ' < /etc/usque/endpoint)"
for k in endpoint_v4 endpoint_h2_v4; do
  ip=$(sed -n 's/.*"'"$k"'"[[:space:]]*:[[:space:]]*"\([0-9.]*\)".*/\1/p' /etc/usque/config.json 2>/dev/null | head -1)
  [ -n "$ip" ] && BREAKOUT="$BREAKOUT $ip"
done
BREAKOUT=$(printf '%s\n' $BREAKOUT | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | awk '!seen[$0]++')

# Реальный IPv4-шлюз, чтобы трафик до WARP не ушёл в туннель (петля).
gw=$(ip -4 route show default | awk '/default/{print $3; exit}')
if [ -n "$gw" ]; then
  : > /run/warp-breakout-ips
  for ip in $BREAKOUT; do
    ip route replace "$ip"/32 via "$gw" 2>/dev/null || true
    echo "$ip" >> /run/warp-breakout-ips
  done
fi

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
EOF
  chmod +x "$USQUE_DIR/up.sh"

  cat > "$USQUE_DIR/down.sh" <<'EOF'
#!/bin/bash
TABLE=10000
NETS="91.108.0.0/16 149.154.160.0/20 95.161.64.0/20 185.76.151.0/24 185.64.92.0/22 91.105.192.0/23 194.169.233.0/24 194.102.202.0/24"
for n in $NETS; do ip rule del to $n table $TABLE priority 100 2>/dev/null || true; done
ip rule del fwmark $TABLE table $TABLE priority 99 2>/dev/null || true
ip route flush table $TABLE 2>/dev/null || true
for ip in 162.159.198.1 162.159.198.2 $(cat /etc/usque/endpoint /run/warp-breakout-ips 2>/dev/null); do
  [ -n "$ip" ] && ip route del "$ip"/32 2>/dev/null || true
done
rm -f /run/warp-breakout-ips
iptables -t nat -D POSTROUTING -o warp -j MASQUERADE 2>/dev/null || true
iptables -t mangle -D POSTROUTING -o warp -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null || true

ip link set dev warp down 2>/dev/null || true
ip link del dev warp 2>/dev/null || true
EOF
  chmod +x "$USQUE_DIR/down.sh"

  cat > /usr/local/bin/warp-run.sh <<'RUNEOF'
#!/bin/bash
# IPv4-only usque runner: exits on EOF/TUN drops so systemd restarts cleanly
# instead of hanging in reconnect loops.
set -u
STATE=/etc/usque/transport
STAMP=/run/warp-transport.last
END_FILE=/etc/usque/endpoint
EP_FAIL=/run/warp-endpoint.fail
FAIL_COUNT_FILE=/run/warp-fail-count
EGRESS_TRIES_FILE=/run/warp-egress-tries
LOG=/var/log/warp-watch.log
IFACE=warp
SNI=consumer-masque.cloudflareclient.com
H2_SNI="${WARP_SNI:-vk.com}"
EP_POOL="${WARP_EP_POOL:-${WARP_H2_POOL:-162.159.198.23 162.159.198.42 162.159.198.49 162.159.198.31 162.159.198.13 162.159.198.22 162.159.198.9 162.159.198.3 162.159.198.6 162.159.198.7 162.159.198.11 162.159.198.10}}"
GRACE="${WARP_TRANSPORT_GRACE:-25}"
COOLDOWN="${WARP_TRANSPORT_COOLDOWN:-300}"
EP_COOLDOWN="${WARP_ENDPOINT_COOLDOWN:-600}"
TRANSPORT_FAILS_FILE=/run/warp-transport-fails
MAX_EP_TRIES="${WARP_MAX_EP_TRIES:-3}"
MAX_EGRESS_TRIES="${WARP_MAX_EGRESS_TRIES:-10}"

transport=$(tr -d '[:space:]' < "$STATE" 2>/dev/null || true)
[ "$transport" = quic ] || transport=http2

log() { echo "$(date '+%F %T') [transport] $*" >> "$LOG"; }

cfg_get() { sed -n 's/.*"'"$1"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /etc/usque/config.json 2>/dev/null | head -1; }
cfg_set() {
  if grep -q "\"$1\"" /etc/usque/config.json 2>/dev/null; then
    sed -i -E 's/("'"$1"'"[[:space:]]*:[[:space:]]*")[^"]*"/\1'"$2"'"/' /etc/usque/config.json 2>/dev/null || true
  fi
}
tls_ok() {
  local probe_sni="$SNI"
  [ "$transport" = http2 ] && probe_sni="$H2_SNI"
  timeout 3 openssl s_client -connect "$1:443" -servername "$probe_sni" </dev/null 2>/dev/null \
    | openssl x509 -noout -subject 2>/dev/null | grep -q 'masque\.cloudflareclient\.com'
}

endpoint_usable() {
  local ip="$1" ts now
  now=$(date +%s)
  ts=$(awk -v ip="$ip" '$1==ip{print $2}' "$EP_FAIL" 2>/dev/null | tail -1)
  case "$ts" in ''|*[!0-9]*) ts=0;; esac
  [ $((now - ts)) -ge "$EP_COOLDOWN" ] || return 1
  tls_ok "$ip"
}

pick_endpoint() {
  local skip="$1" ip
  for ip in $EP_POOL; do
    [ "$ip" = "$skip" ] && continue
    if endpoint_usable "$ip"; then echo "$ip"; return 0; fi
  done
  return 1
}

# Round-robin to the next MASQUE IP without poisoning EP_FAIL on a Russian
# Anycast ECMP roll (e.g. DME vs ARN), because the same IP hashes differently
# on the next ephemeral source port.
pick_next_endpoint() {
  local cur="$1" seen=0 ip first=""
  for ip in $EP_POOL; do
    [ -z "$first" ] && first="$ip"
    if [ "$seen" -eq 1 ] && [ "$ip" != "$cur" ] && endpoint_usable "$ip"; then
      echo "$ip"; return 0
    fi
    [ "$ip" = "$cur" ] && seen=1
  done
  pick_endpoint "$cur" || echo "${cur:-$first}"
}

mark_ep_fail() {
  local bad="$1" now="$2"
  [ -n "$bad" ] || return 0
  awk -v now="$now" -v ttl="$EP_COOLDOWN" '$2 + 0 >= now - ttl' "$EP_FAIL" 2>/dev/null > "$EP_FAIL.tmp" || true
  printf '%s %s\n' "$bad" "$now" >> "$EP_FAIL.tmp"
  mv -f "$EP_FAIL.tmp" "$EP_FAIL" 2>/dev/null || true
}

rotate_on_failure() {
  local cur_ep="$1" now last next t_fails
  touch /run/warp-rotated
  now=$(date +%s)
  last=$(cat "$STAMP" 2>/dev/null || echo 0)
  case "$last" in ''|*[!0-9]*) last=0;; esac
  t_fails=$(cat "$TRANSPORT_FAILS_FILE" 2>/dev/null || echo 0)
  case "$t_fails" in ''|*[!0-9]*) t_fails=0;; esac
  t_fails=$((t_fails + 1))
  echo "$t_fails" > "$TRANSPORT_FAILS_FILE"

  mark_ep_fail "$cur_ep" "$now"
  next=$(pick_endpoint "$cur_ep" || true)
  if [ -n "$next" ] && [ "$t_fails" -lt "$MAX_EP_TRIES" ]; then
    cfg_set endpoint_h2_v4 "$next"
    cfg_set endpoint_v4 "$next"
    echo "$next" > "$END_FILE"
    log "$transport endpoint $cur_ep failed ($t_fails/$MAX_EP_TRIES) - switching to $next"
    return 0
  fi
  if [ $((now - last)) -lt "$COOLDOWN" ]; then
    if [ -n "$next" ]; then
      cfg_set endpoint_h2_v4 "$next"
      cfg_set endpoint_v4 "$next"
      echo "$next" > "$END_FILE"
      log "$transport endpoint $cur_ep failed; cooldown active - trying $next"
      return 0
    fi
    log "$transport passes no traffic; cooldown active - waiting"
    return 0
  fi
  rm -f "$EP_FAIL"
  echo 0 > "$TRANSPORT_FAILS_FILE"
  if [ "$transport" = http2 ]; then next=quic; else next=http2; fi
  echo "$next" > "$STATE"
  echo "$now" > "$STAMP"
  log "$transport passes no traffic - rotating to $next"
}

if [ "$transport" = http2 ]; then cfg_key=endpoint_h2_v4; else cfg_key=endpoint_v4; fi
cur=$(cfg_get "$cfg_key")
if [ -n "$cur" ] && case " $EP_POOL " in *" $cur "*) true;; *) false;; esac && endpoint_usable "$cur"; then
  ep="$cur"
else
  ep=$(pick_endpoint "" || true)
fi
if [ -n "$ep" ]; then
  cfg_set endpoint_h2_v4 "$ep"
  cfg_set endpoint_v4 "$ep"
  echo "$ep" > "$END_FILE"
  log "$transport endpoint $ep (IPv4 TLS ok)"
fi

args=(nativetun -c /etc/usque/config.json -n "$IFACE"
      --always-reconnect --no-tunnel-ipv6
      --keepalive-period 10s --on-connect /etc/usque/up.sh)
[ "$transport" = http2 ] && args+=(--http2 -s "$H2_SNI")

usque_pid=""
monitor_pid=""
log_pid=""
log_fifo=/run/warp-usque.fifo
cleanup_child() {
  [ -n "$monitor_pid" ] && kill "$monitor_pid" 2>/dev/null || true
  if [ -n "$usque_pid" ]; then
    kill "$usque_pid" 2>/dev/null || true
    wait "$usque_pid" 2>/dev/null || true
  fi
  [ -n "$log_pid" ] && wait "$log_pid" 2>/dev/null || true
  rm -f "$log_fifo" /run/warp-usque.pid
}
trap 'cleanup_child; exit 0' TERM INT

is_http_ok() { case "$1" in 2??|3??|401|403) return 0;; *) return 1;; esac; }
probe_code() { curl --interface "$IFACE" -4 -s -o /dev/null -w '%{http_code}' --max-time "$2" "$1" 2>/dev/null || true; }

rm -f /run/warp-healthy /run/warp-rotated /run/warp-usque.pid "$log_fifo"
mkfifo "$log_fifo"
# Disable Go PQ X25519MLKEM768 so ClientHello stays single-packet (<400B) and
# does not trigger DPI/TSPU TCP-443 blackholing after ~3 KB.
GODEBUG="tlsmlkem=0,tlskyber=0" /usr/local/bin/usque "${args[@]}" >"$log_fifo" 2>&1 &
usque_pid=$!
echo "$usque_pid" > /run/warp-usque.pid

(
  while IFS= read -r line; do
    printf '%s\n' "$line" >&2
    case "$line" in
      *"not pollable"*)
        pid=$(cat /run/warp-usque.pid 2>/dev/null || true)
        [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
        ;;
    esac
  done < "$log_fifo"
) &
log_pid=$!

(
  i=0
  while [ "$i" -lt "$GRACE" ]; do
    kill -0 "$usque_pid" 2>/dev/null || exit 0
    if [ -e "/sys/class/net/$IFACE" ] && curl --interface "$IFACE" -4 -s -o /dev/null --max-time 3 https://1.1.1.1/ 2>/dev/null; then
      tme_code=$(probe_code "https://t.me/" 2)
      if is_http_ok "$tme_code"; then
        yt_code=$(probe_code "https://www.youtube.com/generate_204" 2)
      else
        yt_code="skip"
      fi
      if is_http_ok "$tme_code" && is_http_ok "$yt_code"; then
        echo 0 > "$FAIL_COUNT_FILE"
        echo 0 > "$TRANSPORT_FAILS_FILE"
        echo 0 > "$EGRESS_TRIES_FILE"
        touch /run/warp-healthy
        exit 0
      fi
      etries=$(cat "$EGRESS_TRIES_FILE" 2>/dev/null || echo 0)
      case "$etries" in ''|*[!0-9]*) etries=0;; esac
      if [ "$etries" -lt "$MAX_EGRESS_TRIES" ]; then
        etries=$((etries + 1))
        echo "$etries" > "$EGRESS_TRIES_FILE"
        echo 0 > "$FAIL_COUNT_FILE"
        echo 0 > "$TRANSPORT_FAILS_FILE"
        next=$(pick_next_endpoint "$ep")
        cfg_set endpoint_h2_v4 "$next"
        cfg_set endpoint_v4 "$next"
        echo "$next" > "$END_FILE"
        touch /run/warp-rotated
        log "$transport endpoint $ep has filtered egress (t.me=${tme_code:-000} yt=${yt_code:-000}) - rerolling ($etries/$MAX_EGRESS_TRIES) -> $next"
        kill "$usque_pid" 2>/dev/null || true
        exit 0
      fi
      log "egress still filtered after $MAX_EGRESS_TRIES tries; keeping working $transport tunnel on $ep"
      echo 0 > "$FAIL_COUNT_FILE"
      echo 0 > "$TRANSPORT_FAILS_FILE"
      echo 0 > "$EGRESS_TRIES_FILE"
      touch /run/warp-healthy
      exit 0
    fi
    sleep 1
    i=$((i + 1))
  done
  kill -0 "$usque_pid" 2>/dev/null || exit 0
  echo 0 > "$FAIL_COUNT_FILE"
  echo 0 > "$EGRESS_TRIES_FILE"
  rotate_on_failure "$ep"
  kill "$usque_pid" 2>/dev/null || true
) &
monitor_pid=$!

set +e
wait "$usque_pid"
rc=$?
set -e
kill "$monitor_pid" 2>/dev/null || true
wait "$monitor_pid" 2>/dev/null || true
wait "$log_pid" 2>/dev/null || true
rm -f "$log_fifo" /run/warp-usque.pid

if [ ! -f /run/warp-healthy ] && [ ! -f /run/warp-rotated ]; then
  echo 0 > "$EGRESS_TRIES_FILE"
  fails=$(cat "$FAIL_COUNT_FILE" 2>/dev/null || echo 0)
  case "$fails" in ''|*[!0-9]*) fails=0;; esac
  fails=$((fails + 1))
  echo "$fails" > "$FAIL_COUNT_FILE"
  if [ "$fails" -ge 2 ]; then
    rotate_on_failure "$ep"
    echo 0 > "$FAIL_COUNT_FILE"
  fi
fi
rm -f /run/warp-healthy /run/warp-rotated
exit "$rc"
RUNEOF
  chmod +x /usr/local/bin/warp-run.sh
  [ -f /etc/usque/transport ] || echo http2 > /etc/usque/transport

  cat > /etc/systemd/system/usque.service <<EOF
[Unit]
Description=Cloudflare WARP via usque (IPv4 MASQUE)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
ExecStartPre=/bin/sh -c '/etc/usque/down.sh 2>/dev/null || ip link del dev warp 2>/dev/null || true'
ExecStart=/usr/local/bin/warp-run.sh
ExecStopPost=/bin/sh -c '/etc/usque/down.sh 2>/dev/null || ip link del dev warp 2>/dev/null || true'
Restart=always
RestartSec=1
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

MAX_TRY="${MAX_TRY:-6}"
MAX_LATENCY="${MAX_LATENCY:-5}"
CHECK_TARGET="${CHECK_URL:-https://api.telegram.org/}"

log() {
  if [ "${ROTATE_LOG_TO_WATCH:-1}" = "1" ]; then
    echo "$(date '+%F %T') [rotate] $*" | tee -a "$LOG"
  else
    echo "$(date '+%F %T') [rotate] $*"
  fi
}

# Rotation must not leave the tunnel down: the previous config is still valid.
fail() {
  log "$*"
  [ -s "$USQUE_CONF" ] && systemctl start usque 2>/dev/null || true
  exit 1
}

probe() {
  if ! curl --interface "$IFACE" -4 -s -o /dev/null --max-time 2 https://1.1.1.1/ 2>/dev/null; then
    echo "000 999"
    return
  fi
  local res
  res=$(curl --interface "$IFACE" -4 -sS -o /dev/null \
    -w '%{http_code} %{time_total}' --max-time "$MAX_LATENCY" "$CHECK_TARGET" 2>/dev/null) || res="000 999"
  printf '%s\n' "${res:-000 999}"
}

good() {
  case "$1" in 2??|3??|401|403) ;; *) return 1;; esac
  awk -v t="$2" -v m="$MAX_LATENCY" 'BEGIN{exit !(t <= m)}'
}

# A dead tunnel is not a dead WARP IP: restarting lets warp-run.sh flip the
# transport (QUIC -> HTTP/2 over TCP/443) before any registration is spent.
restore_tunnel() {
  local i
  systemctl restart usque 2>/dev/null || true
  for i in $(seq 1 40); do
    sleep 1
    if curl --interface "$IFACE" -4 -s -o /dev/null --max-time 2 https://1.1.1.1/ 2>/dev/null; then
      return 0
    fi
  done
  return 1
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

if ! curl --interface "$IFACE" -4 -s -o /dev/null --max-time 2 https://1.1.1.1/ 2>/dev/null; then
  restore_tunnel || true
  if [ "$FORCE" -eq 0 ]; then
    read -r code t < <(probe) || true
    if good "${code:-000}" "${t:-999}"; then
      log "туннель восстановлен без регистрации (code=$code, ${t}s)"
      exit 0
    fi
  fi
fi

API_HELPER=/usr/local/bin/warp-api-host.sh
MAX_REG_FAILS="${MAX_REG_FAILS:-2}"

# Один проход регистрации: проверенные IP из api_ips.txt, затем системный DNS.
# Каждый пин ставится в /etc/hosts только после проверки сертификата, поэтому
# заведомо чужой ответ (например, перехват провайдера) больше не попадает в hosts.
try_register() {
  local try_ip
  local -a ips=()
  if [ -s "$USQUE_DIR/api_ips.txt" ]; then
    mapfile -t ips < "$USQUE_DIR/api_ips.txt"
  fi
  for try_ip in "${ips[@]}" ""; do
    if [ -n "$try_ip" ]; then
      [ -x "$API_HELPER" ] && "$API_HELPER" pin "$try_ip" >/dev/null 2>&1 || true
    else
      [ -x "$API_HELPER" ] && "$API_HELPER" clear >/dev/null 2>&1 || true
    fi
    rm -f "$USQUE_DIR/config.json.tmp" 2>/dev/null || true
    out=$(GODEBUG="tlsmlkem=0,tlskyber=0" $USQUE_BIN register -a -c "$USQUE_DIR/config.json.tmp" 2>&1 || true)
    if [ -s "$USQUE_DIR/config.json.tmp" ]; then
      mv -f "$USQUE_DIR/config.json.tmp" "$USQUE_CONF"
      if [ -n "$try_ip" ]; then
        { printf '%s\n' "$try_ip"; grep -vxF "$try_ip" "$USQUE_DIR/api_ips.txt" 2>/dev/null || true; } \
          > "$USQUE_DIR/api_ips.txt.tmp" && mv -f "$USQUE_DIR/api_ips.txt.tmp" "$USQUE_DIR/api_ips.txt"
      fi
      log "регистрация успешна (${try_ip:-системный DNS})"
      return 0
    fi
    short_err="$(echo "$out" | grep -iE 'timeout|failed to register|refused|certificate|error' | tail -1)"
    [ -z "$short_err" ] && short_err="$(echo "$out" | tail -1)"
    log "регистрация (${try_ip:-системный DNS}) не удалась ($short_err)"
    # Cloudflare принял запрос и отказал (rate limit / 1015). Перебор остальных
    # IP не поможет и только продлит бан — выходим сразу.
    if echo "$out" | grep -qiE '1015|rate.?limit|too many requests|parse error response'; then
      return 2
    fi
  done
  return 1
}

attempt=0
reg_fails=0
while [ "$attempt" -lt "$MAX_TRY" ]; do
  attempt=$((attempt + 1))
  log "попытка $attempt/$MAX_TRY: подбор рабочего IP"
  systemctl stop usque 2>/dev/null || true
  sleep 1
  ip link del "$IFACE" 2>/dev/null || true

  # Список из api_ips.txt мог устареть: перед каждой попыткой перепроверяем.
  if [ -x "$API_HELPER" ]; then
    "$API_HELPER" probe >/dev/null 2>&1 || true
  fi
  try_register
  rc=$?
  if [ "$rc" -ne 0 ]; then
    reg_fails=$((reg_fails + 1))
    [ -x "$API_HELPER" ] && "$API_HELPER" clear >/dev/null 2>&1 || true
    if [ "$rc" -eq 2 ]; then
      date +%s > /run/warp-ratelimit 2>/dev/null || true
      fail "Cloudflare временно ограничил регистрацию — повторите смену IP через несколько минут"
    fi
    if [ "$reg_fails" -ge "$MAX_REG_FAILS" ]; then
      fail "регистрация не удалась $reg_fails раз(а) подряд — Cloudflare API недоступен, дальше не пробую"
    fi
    sleep 3
    continue
  fi
  reg_fails=0
  rm -f /run/warp-ratelimit 2>/dev/null || true
  systemctl start usque

  # Даём warp-run.sh время переключить транспорт, если UDP/443 у провайдера закрыт.
  for j in $(seq 1 40); do
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
fail "не удалось подобрать рабочий IP за $MAX_TRY попыток"
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
  curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code}' --max-time "${2:-10}" "$1" 2>/dev/null || true
}
probe_target() {
  local res
  res=$(curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code} %{time_total}' \
    --max-time "$((MAX_LATENCY + 2))" "$CHECK_TARGET" 2>/dev/null) || res="000 999"
  printf '%s\n' "${res:-000 999}"
}
is_ok() { case "$1" in 2??|3??|401|403) return 0;; *) return 1;; esac; }
is_fast() { awk -v t="$1" -v m="$MAX_LATENCY" 'BEGIN{exit !(t <= m)}'; }
egress_ok() {
  egress_detail="ok"
  case "$CHECK_TARGET" in
    https://api.telegram.org*|https://t.me*) ;;
    *) return 0 ;;
  esac
  local tme_c="$code" yt_c
  case "$CHECK_TARGET" in
    https://api.telegram.org*)
      tme_c=$(get_code "https://t.me/" 3)
      if ! is_ok "$tme_c"; then
        egress_detail="t.me=${tme_c:-000}"
        return 1
      fi
      ;;
  esac
  yt_c=$(get_code "https://www.youtube.com/generate_204" 3)
  if ! is_ok "$yt_c"; then
    egress_detail="t.me=${tme_c:-000},yt=${yt_c:-000}"
    return 1
  fi
  egress_detail="t.me=${tme_c},yt=${yt_c}"
  return 0
}

# Cloudflare rate-limits registrations; rotating inside the window only extends
# the ban, and warp-run.sh can try the other transport meanwhile.
rotate_now() {
  local stamp age
  stamp=$(cat /run/warp-ratelimit 2>/dev/null || echo 0)
  case "$stamp" in ''|*[!0-9]*) stamp=0;; esac
  age=$(( $(date +%s) - stamp ))
  if [ "$stamp" -gt 0 ] && [ "$age" -lt 1800 ]; then
    log "смена IP на паузе: Cloudflare ограничил регистрацию ${age}s назад"
    return 0
  fi
  [ -x /usr/local/bin/warp-rotate.sh ] && bash /usr/local/bin/warp-rotate.sh >/dev/null 2>&1
}

fails_tunnel=0; fails_target=0; fails_slow=0; last_action=0
[ -f "$STATE" ] && . "$STATE" 2>/dev/null || true

now=$(date +%s)
neutral=$(get_code "$NEUTRAL" 4)
read -r code t < <(probe_target) || true
egress_detail="ok"
egress_passed=1
if is_ok "$neutral" && is_ok "$code" && is_fast "$t"; then
  egress_ok || egress_passed=0
fi

if is_ok "$neutral" && is_ok "$code" && is_fast "$t" && [ "$egress_passed" -eq 1 ]; then
  log "проверка OK: туннель=$neutral цель=$code (${t}s, выход $egress_detail, лимит ${MAX_LATENCY}s)"
  echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$last_action" > "$STATE"
  exit 0
fi

if ! is_ok "$neutral"; then
  fails_tunnel=$((fails_tunnel + 1)); fails_target=0; fails_slow=0
  log "туннель НЕ отвечает (code=${neutral:-000}) попытка=$fails_tunnel"
elif ! is_ok "$code"; then
  fails_target=$((fails_target + 1)); fails_slow=0
  log "туннель OK (${neutral}), цель недоступна (code=${code:-000}) попытка=$fails_target"
elif [ "$egress_passed" -eq 0 ]; then
  fails_target=$((fails_target + 1)); fails_slow=0
  log "туннель OK (${neutral}), фильтрация выхода ($egress_detail) попытка=$fails_target"
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
  # warp-run.sh перебирает ECMP-выход или транспорт за ~25-35s.
  sleep 25
  neutral=$(get_code "$NEUTRAL" 4); read -r code t < <(probe_target) || true
  if is_ok "$neutral" && is_ok "$code" && is_fast "$t" && egress_ok; then
    log "после рестарта рабочий IP активен (туннель=$neutral цель=$code ${t}s $egress_detail)"
    echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$now" > "$STATE"
    exit 0
  fi
  if [ "$AUTO_ROTATE" = "1" ]; then
    log "рестарт не помог, подбираю рабочий IP"
    rotate_now
  else
    log "рестарт не помог, автосмена IP отключена"
  fi
  echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$now" > "$STATE"
  exit 0
fi

# Цель или выход недоступны (2 раза подряд) либо стабильно медленно (3): сначала рестарт для смены ECMP-узла.
if [ "$fails_target" -ge 2 ] || [ "$fails_slow" -ge 3 ]; then
  log "проблема выхода/задержки (fails=$fails_target slow=$fails_slow) — перезапускаю usque"
  systemctl restart usque
  sleep 25
  neutral=$(get_code "$NEUTRAL" 4); read -r code t < <(probe_target) || true
  if is_ok "$neutral" && is_ok "$code" && is_fast "$t" && egress_ok; then
    log "после рестарта маршрут восстановлен (туннель=$neutral цель=$code ${t}s $egress_detail)"
    echo "fails_tunnel=0 fails_target=0 fails_slow=0 last_action=$now" > "$STATE"
    exit 0
  fi
  if [ "$AUTO_ROTATE" = "1" ]; then
    log "рестарт не помог (fails=$fails_target slow_fails=$fails_slow) — подбираю рабочий IP"
    rotate_now
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
  if systemctl enable --now warp-watch.timer 2>/dev/null; then
    # Таймер systemd основной: резервный cron только дублировал бы проверки.
    { crontab -l 2>/dev/null || true; } | grep -v "warp-watch.sh" | crontab - 2>/dev/null || true
  else
    { crontab -l 2>/dev/null | grep -v "warp-watch.sh" || true; echo "*/${CHECK_INTERVAL} * * * * /usr/local/bin/warp-watch.sh"; } | crontab - 2>/dev/null || true
  fi
}

setup_routes_and_service
install_rotate_script
install_watch_cron

systemctl enable --now usque 2>/dev/null || true
for _ in $(seq 1 35); do
  [ -f /run/warp-healthy ] && break
  sleep 1
done

echo "--- Подбор рабочего WARP IP ---"
ROTATE_LOG_TO_WATCH=0 MAX_LATENCY="${MAX_LATENCY:-5}" bash /usr/local/bin/warp-rotate.sh || true

WARP_IP="$(get_ip "$IFACE")"
WARP_COUNTRY="$(get_country "$IFACE")"
echo "  WARP IP: $WARP_IP${WARP_COUNTRY:+ (страна: $WARP_COUNTRY)}"

CHECK_TARGET="${CHECK_URL:-https://api.telegram.org/}"
RES=$(curl --interface "$IFACE" -4 -sS -o /dev/null -w '%{http_code} %{time_total}' --max-time 6 "$CHECK_TARGET" 2>/dev/null) || RES="000 999"
read -r CODE t <<< "${RES:-000 999}"
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
