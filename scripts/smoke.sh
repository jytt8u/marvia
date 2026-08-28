#!/usr/bin/env bash
#
# Локальная сквозная проверка: нода и клиент на localhost, самоподписанный
# сертификат, настоящий сайт в качестве прикрытия.
#
# Проверяет три вещи:
#   1. трафик пользователя ходит через туннель;
#   2. мультиплексирование работает — соединений до ноды меньше, чем запросов;
#   3. посторонний, пришедший на ноду, видит сайт-прикрытие, а не разрыв.
#
# Запуск:  bash scripts/smoke.sh

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
NODE_ADDR="127.0.0.1:8443"
SOCKS_ADDR="127.0.0.1:1080"
COVER="https://example.com"
SNI="cover.local"

SRV=""
CLI=""

cleanup() {
  [ -n "$SRV" ] && kill "$SRV" 2>/dev/null
  [ -n "$CLI" ] && kill "$CLI" 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

cd "$ROOT" || exit 1

echo "== сборка =="
go build -o bin/ ./... || exit 1

KEYS="$(./bin/marvia-keygen -quiet)" || KEYS="$(./bin/marvia-keygen.exe -quiet)" || exit 1
PRIV="$(printf '%s\n' "$KEYS" | sed -n 1p)"
PUB="$(printf '%s\n' "$KEYS" | sed -n 2p)"

# Бинарники на Windows называются с .exe — берём то, что собралось.
SERVER_BIN="./bin/marvia-node"; [ -x "$SERVER_BIN" ] || SERVER_BIN="./bin/marvia-node.exe"
CLIENT_BIN="./bin/marvia-client"; [ -x "$CLIENT_BIN" ] || CLIENT_BIN="./bin/marvia-client.exe"

echo "== запуск ноды =="
"$SERVER_BIN" -listen "$NODE_ADDR" -key "$PRIV" \
  -tls-self-signed "$SNI" -cover "$COVER" >"$WORK/node.log" 2>&1 &
SRV=$!

# Ждём, пока нода начнёт слушать. Повторы делает сам curl.
curl -sk --retry 20 --retry-connrefused --retry-delay 1 --max-time 40 \
  -o /dev/null "https://$NODE_ADDR/" || { echo "нода не поднялась"; cat "$WORK/node.log"; exit 1; }

echo "== запуск клиента =="
"$CLIENT_BIN" -listen "$SOCKS_ADDR" -server "$NODE_ADDR" -pubkey "$PUB" \
  -sni "$SNI" -insecure >"$WORK/client.log" 2>&1 &
CLI=$!

echo
echo "== 1. трафик через туннель =="
curl -s --retry 20 --retry-connrefused --retry-delay 1 --max-time 40 \
  -o /dev/null -w 'первый запрос: HTTP %{http_code}\n' \
  -x "socks5h://$SOCKS_ADDR" "$COVER/" || { echo "туннель не работает"; cat "$WORK/client.log"; exit 1; }

echo
echo "== 2. мультиплексирование: 12 параллельных запросов =="
PIDS=""
for i in $(seq 12); do
  curl -s --max-time 30 -o /dev/null -w '%{http_code} ' \
    -x "socks5h://$SOCKS_ADDR" "$COVER/?n=$i" &
  PIDS="$PIDS $!"
done
# Ждём только запросы, но не ноду с клиентом: голый wait подвис бы навсегда.
for pid in $PIDS; do wait "$pid"; done
echo

REQUESTS="$(grep -c ' -> ' "$WORK/node.log")"
SESSIONS="$(grep -c 'сессия открыта' "$WORK/node.log")"
echo "запросов пользователя: $REQUESTS"
echo "соединений до ноды:    $SESSIONS  (без мультиплексирования было бы $REQUESTS)"

echo
echo "== 3. что видит посторонний =="
curl -sk --max-time 20 -o "$WORK/probe.html" -w 'HTTP %{http_code}\n' "https://$NODE_ADDR/"
head -c 100 "$WORK/probe.html"; echo

echo
if [ "$SESSIONS" -lt "$REQUESTS" ] && grep -qi "example domain" "$WORK/probe.html"; then
  echo "ИТОГ: всё в порядке"
else
  echo "ИТОГ: что-то не так, смотри логи в $WORK"
  trap - EXIT
  exit 1
fi
