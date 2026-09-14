#!/bin/sh
# Стенд: как наша нода выглядит для DPI и для сканера.
#
# Оценки «хорошо» в protocol.md §10 — наши собственные. Этот скрипт заменяет их
# измерением: пишет трафик до ноды в pcap, прогоняет запись через nDPI и Zeek,
# а саму ноду щупает так, как это делают сканеры. Запускать на Linux с той
# стороны, откуда ходят покупатели — с VPS в России или с домашней машины.
#
#   sh scripts/stand.sh -server node.example.com:443 -pubkey <ключ ноды> \
#       [-sni cover.example.com] [-reality-pbk … -reality-sid …] [-quic] \n#       [-key <приватный ключ покупателя>]
#
# Что нужно на машине: tcpdump, curl, openssl, marvia-client (собранный из
# репозитория или из релиза), и хотя бы одно из: ndpiReader, zeek с пакетом
# ja4 (zkg install ja4). Чего нет — тот шаг пропускается с пометкой, а не
# падает: даже один pcap уже полезен.
#
# nDPI собирай из исходников (ветка 4.10+): пакет libndpi-bin в Ubuntu 22.04 —
# это 4.2 2022 года, он не знает ни ECH, ни ALPS и метит любой современный
# Chrome как подозрительный, а JA4 не считает вовсе.
#
# Скрипт ничего не меняет ни на ноде, ни на машине. Без -key ключ клиента
# временный, и нода с базой покупателей его не знает: туннель не поднимется,
# а сайт-прикрытие ответит — ровно как чужому. Чтобы записать трафик внутри
# туннеля, дай ключ настоящего покупателя: он в его ссылке marvia://КЛЮЧ@…

set -eu

say()  { printf '%s\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$1"; PROBLEMS=$((PROBLEMS + 1)); }
note() { printf '  \033[2m·\033[0m %s\n' "$1"; }
skip() { printf '  \033[33m−\033[0m %s\n' "$1"; }
head_() { printf '\n\033[1m%s\033[0m\n' "$1"; }
die()  { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 1; }

PROBLEMS=0
SERVER=''
PUBKEY=''
SNI=''
RPBK=''
RSID=''
QUIC=0
KEY=''
OUT=${OUT:-stand-$(date +%Y%m%d-%H%M%S)}
CLIENT=${CLIENT:-}

while [ $# -gt 0 ]; do
	case "$1" in
	-server) SERVER=$2; shift 2 ;;
	-pubkey) PUBKEY=$2; shift 2 ;;
	-sni) SNI=$2; shift 2 ;;
	-reality-pbk) RPBK=$2; shift 2 ;;
	-reality-sid) RSID=$2; shift 2 ;;
	-quic) QUIC=1; shift ;;
	-key) KEY=$2; shift 2 ;;
	-out) OUT=$2; shift 2 ;;
	*) die "неизвестный аргумент: $1" ;;
	esac
done
[ -n "$SERVER" ] || die 'нужен -server host:port'
[ -n "$PUBKEY" ] || die 'нужен -pubkey — публичный ключ ноды'

HOST=${SERVER%:*}
PORT=${SERVER##*:}
[ -n "$SNI" ] || SNI=$HOST

# ─────────────────────────────────────────────────────────────── инструменты

head_ 'Инструменты'
have() { command -v "$1" >/dev/null 2>&1; }

if [ -z "$CLIENT" ]; then
	for c in ./bin/marvia-client ./marvia-client marvia-client; do
		if [ -x "$c" ] || have "$c"; then CLIENT=$c; break; fi
	done
fi
[ -n "$CLIENT" ] || die 'не нашёл marvia-client: собери go build -o bin/ ./... или задай CLIENT=путь'
ok "клиент: $CLIENT"

for t in tcpdump curl openssl; do
	if have "$t"; then ok "$t"; else die "нет $t — без него стенд не работает"; fi
done
NDPI=''
for c in ndpiReader ndpi_reader; do have "$c" && NDPI=$c && break; done
[ -n "$NDPI" ] && ok "nDPI: $NDPI" || skip 'nDPI не найден — классификатор пропущу (apt install ndpi-bin)'
ZEEK=''
have zeek && ZEEK=zeek
[ -n "$ZEEK" ] && ok 'zeek' || skip 'zeek не найден — JA4 пропущу (zkg install ja4)'

[ "$(id -u)" = 0 ] || note 'не root: tcpdump может не получить интерфейс — тогда запусти под sudo'

mkdir -p "$OUT"
say "результаты: $OUT/"

# Адрес ноды — числом: pcap фильтруем по нему, а не по имени.
IP=$(getent ahostsv4 "$HOST" 2>/dev/null | awk 'NR==1{print $1}' || true)
[ -n "$IP" ] || IP=$HOST
note "нода: $IP:$PORT, SNI $SNI"

# ─────────────────────────────────────────────────────────────── запись

head_ 'Запись трафика до ноды'

# Интерфейс — тот, через который ходим к ноде, а не any: с -i any tcpdump пишет
# заголовки Linux SLL2, которые ndpiReader 4.x не читает вовсе — молча
# пропускает файл, и проверка ниже «не находила проблем» на пустом отчёте.
IFACE=$(ip route get "$IP" 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="dev") {print $(i+1); exit}}')
[ -n "$IFACE" ] || IFACE=any
note "интерфейс: $IFACE"
tcpdump -i "$IFACE" -w "$OUT/node.pcap" "host $IP and port $PORT" >/dev/null 2>&1 &
DUMP=$!
sleep 1
kill -0 "$DUMP" 2>/dev/null || die 'tcpdump не запустился (нужен root?)'

set -- -server "$SERVER" -pubkey "$PUBKEY" -sni "$SNI" -listen 127.0.0.1:18080
[ -n "$RPBK" ] && set -- "$@" -reality-pbk "$RPBK" -reality-sid "$RSID"
[ -n "$KEY" ] && set -- "$@" -key "$KEY"
"$CLIENT" "$@" >"$OUT/client.log" 2>&1 &
CPID=$!
sleep 3
if ! kill -0 "$CPID" 2>/dev/null; then
	kill "$DUMP" 2>/dev/null || true
	bad 'клиент не поднялся — смотри client.log'
	sed 's/^/    /' "$OUT/client.log" | tail -5
	exit 1
fi
ok 'туннель поднят'

# Трафик через туннель: несколько сайтов, одна скачка покрупнее. Не наш
# сайт-прикрытие — цель другая: как выглядят обычные страницы внутри туннеля.
for u in https://www.wikipedia.org/ https://www.cloudflare.com/ https://github.com/ \
	https://speed.cloudflare.com/__down?bytes=25000000; do
	if curl -fsS --max-time 60 --socks5-hostname 127.0.0.1:18080 -o /dev/null "$u"; then
		ok "$u"
	else
		bad "не прошло: $u"
	fi
done
kill "$CPID" 2>/dev/null || true
sleep 1
kill "$DUMP" 2>/dev/null || true
wait "$DUMP" 2>/dev/null || true
ok "записано: $OUT/node.pcap ($(du -h "$OUT/node.pcap" | cut -f1))"

# ─────────────────────────────────────────────────────────────── зондирование

head_ 'Как нода отвечает чужим'

# Браузер без нашей метки: должен увидеть сайт-прикрытие с его сертификатом.
if printf 'GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n' "$SNI" \
	| openssl s_client -connect "$IP:$PORT" -servername "$SNI" -quiet 2>"$OUT/probe-tls.err" >"$OUT/probe-tls.out"; then :; fi
if grep -q 'HTTP/1' "$OUT/probe-tls.out"; then
	ok "TLS без метки: сайт-прикрытие отвечает ($(head -1 "$OUT/probe-tls.out" | tr -d '\r'))"
else
	bad 'TLS без метки: сайт-прикрытие не ответил — смотри probe-tls.out/.err'
fi
issuer=$(openssl s_client -connect "$IP:$PORT" -servername "$SNI" </dev/null 2>/dev/null | openssl x509 -noout -issuer 2>/dev/null || true)
[ -n "$issuer" ] && note "сертификат: $issuer"

# Мусор в TCP: веб-сервер отвечает 400 или молчит и закрывает — но не висит.
if printf 'мусор\r\n\r\n' | timeout 5 openssl s_client -connect "$IP:$PORT" -servername "$SNI" -quiet >/dev/null 2>&1; then rc=0; else rc=$?; fi
if [ "$rc" = 124 ]; then
	bad 'мусор по TCP: нода держит соединение дольше 5 секунд — веб-сервер так не делает'
else
	ok 'мусор по TCP: соединение закрыто, не повисло'
fi

# Чужой QUIC: пакет с несуществующей версией. Молчание — правильно; ответ
# Version Negotiation выдал бы стек ноды.
if [ "$QUIC" = 1 ]; then
	if have nc; then
		printf '\300\336\255\276\357\010dcid-000\010scid-000' > "$OUT/probe-quic.bin"
		head -c 1180 /dev/zero >> "$OUT/probe-quic.bin"
		if timeout 2 nc -u -w 1 "$IP" "$PORT" < "$OUT/probe-quic.bin" > "$OUT/probe-quic.out" 2>/dev/null; then :; fi
		if [ -s "$OUT/probe-quic.out" ]; then
			bad 'чужой QUIC: нода ответила — это отпечаток стека'
		else
			ok 'чужой QUIC: тишина'
		fi
	else
		skip 'nc не найден — чужой QUIC не проверен'
	fi
fi

# ─────────────────────────────────────────────────────────────── классификаторы

head_ 'Что видит классификатор'

if [ -n "$NDPI" ]; then
	"$NDPI" -i "$OUT/node.pcap" -v 1 > "$OUT/ndpi.txt" 2>&1 || true
	# Строки с нашей нодой: какой протокол им присвоен.
	grep -E "$IP:$PORT" "$OUT/ndpi.txt" | sed 's/^/    /' | head -20
	# Пустой отчёт — не «проблем нет», а «не посмотрели»: так было с pcap
	# в формате SLL2, который nDPI пропускает молча.
	if ! grep -qE "$IP:$PORT" "$OUT/ndpi.txt"; then
		bad 'nDPI не увидел ни одного потока до ноды — смотри ndpi.txt'
	elif grep -E "$IP:$PORT" "$OUT/ndpi.txt" | grep -qiE 'unknown|proxy|vpn|tor|wireguard'; then
		bad 'nDPI видит не TLS/HTTP: смотри ndpi.txt'
	else
		ok 'nDPI: TLS к сайту-прикрытию, как задумано'
	fi
	# Отпечаток и риски — глазами: JA4 сверяют с базой известных браузеров, а
	# «Susp Extn» у старого nDPI значит лишь, что расширение новее его самого
	# (ALPS 17613 и ECH — у настоящего Chrome они же).
	"$NDPI" -i "$OUT/node.pcap" -v 2 2>/dev/null | grep -E "$IP:$PORT" | grep -oE "[(JA4|JA3C|Risk|Risk Info|ECH)[^]]*]" | sed 's/^/    /' | head -6
else
	skip 'nDPI пропущен'
fi

if [ -n "$ZEEK" ]; then
	( cd "$OUT" && "$ZEEK" -C -r node.pcap ja4 2>/dev/null || "$ZEEK" -C -r node.pcap 2>/dev/null ) || true
	if [ -f "$OUT/ssl.log" ]; then
		if grep -q 'ja4' "$OUT/ssl.log"; then
			ok 'zeek: JA4 записан в ssl.log — сравни с текущим Chrome на ja4db.com'
			awk -F'\t' 'NR>8 && $0 !~ /^#/ {print "    " $0}' "$OUT/ssl.log" | cut -c1-160 | head -5
		else
			skip 'zeek без пакета ja4: отпечатка нет, но ssl.log есть'
		fi
	else
		skip 'zeek не оставил ssl.log'
	fi
else
	skip 'zeek пропущен'
fi

# ─────────────────────────────────────────────────────────────── итог

head_ 'Итог'
if [ "$PROBLEMS" = 0 ]; then
	say 'Проблем не найдено. Запись и отчёты — в каталоге выше; их можно приложить к issue.'
else
	say "Проблем: $PROBLEMS. Смотри пометки выше и файлы в $OUT/."
fi
