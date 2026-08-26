#!/bin/sh
#
# Установка панели Veil на чистый сервер.
#
#   ./install-panel.sh --domain panel.example.com [--port 443] [--email ты@example.com]
#
# Бинарники берутся сами: скрипт определяет разрядность сервера, скачивает
# архив, сверяет контрольную сумму и ставит. Компилятор не нужен — в этом весь
# смысл: продавец покупает сервер и продаёт доступ, не касаясь кода.
#
# Откуда качать, можно задать:
#   --from https://…/veil_linux_amd64.tar.gz   готовый архив
#   --bin-dir /путь                            уже распакованные бинарники
#
# Ноды ставятся иначе: панель выдаёт готовую строку, и ей скачивать заранее
# ничего не надо. Панель — единственное место, куда бинарники приезжают сами.

set -eu

DOMAIN=''
PORT=443
EMAIL=''
DIR=/opt/veil
BIN_DIR=''
FROM=''
REPO='jytt8u/veil'

say() { printf '%s\n' "$*"; }
die() { printf '\nустановка прервана: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
	--domain) DOMAIN="${2:-}"; shift 2 ;;
	--port) PORT="${2:-}"; shift 2 ;;
	--email) EMAIL="${2:-}"; shift 2 ;;
	--bin-dir) BIN_DIR="${2:-}"; shift 2 ;;
	--from) FROM="${2:-}"; shift 2 ;;
	--repo) REPO="${2:-}"; shift 2 ;;
	*) die "непонятный ключ $1" ;;
	esac
done

# ---------------------------------------------------------------- проверки

[ "$(id -u)" = 0 ] || die 'нужны права root'
command -v systemctl >/dev/null 2>&1 || die 'на этой системе нет systemd, автозапуск настроить нечем'
command -v curl >/dev/null 2>&1 || die 'нет curl. Поставь: apt-get install -y curl'
command -v tar >/dev/null 2>&1 || die 'нет tar. Поставь: apt-get install -y tar'

[ -n "$DOMAIN" ] || die 'не задан домен: --domain panel.example.com'

if [ -d "$DIR" ]; then
	die "$DIR уже существует. Здесь, похоже, уже стоит панель. Внутри база с подписчиками — снеси её осознанно: systemctl disable --now veil-panel && rm -rf $DIR"
fi

# Домен обязан вести сюда. Проверяем до всего остального: Let's Encrypt
# ограничивает число неудачных проверок, и упереться в этот предел из-за
# неверной записи DNS — обидный способ потерять час.
say "проверяю, что $DOMAIN ведёт на этот сервер"

MY_IP=$(curl -fsS --max-time 10 https://api.ipify.org 2>/dev/null || true)
DOMAIN_IP=$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk '{print $1; exit}')

[ -n "$DOMAIN_IP" ] || die "у домена $DOMAIN нет записи A. Заведи её и подожди, пока разойдётся"

if [ -n "$MY_IP" ] && [ "$DOMAIN_IP" != "$MY_IP" ]; then
	die "$DOMAIN ведёт на $DOMAIN_IP, а этот сервер $MY_IP. Поправь запись A и подожди, пока разойдётся"
fi

# Порт 80 нужен для проверки владения доменом. Панель займёт его сама.
if command -v ss >/dev/null 2>&1; then
	for p in 80 "$PORT"; do
		if ss -tln 2>/dev/null | awk '{print $4}' | sed 's/.*://' | grep -qx "$p"; then
			if [ "$p" = 80 ]; then
				die 'порт 80 занят. Он нужен, чтобы Let'"'"'s Encrypt проверил владение доменом. Освободи его: ss -tlnp | grep :80'
			fi
			die "порт $p занят. Возьми другой: --port 8443"
		fi
	done
fi

# ---------------------------------------------------------- откуда бинарники

WORK=''
cleanup() { [ -n "$WORK" ] && rm -rf "$WORK"; }
trap cleanup EXIT

if [ -z "$BIN_DIR" ]; then
	# Рядом со скриптом уже лежат? Так бывает, когда человек скачал архив и
	# распаковал его руками — тогда качать второй раз незачем.
	HERE=$(cd "$(dirname "$0")" && pwd)
	if [ -f "$HERE/veil-panel" ]; then
		BIN_DIR="$HERE"
	fi
fi

if [ -z "$BIN_DIR" ]; then
	case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "разрядность $(uname -m) не поддерживается: собери бинарники сам и укажи --bin-dir" ;;
	esac

	[ -n "$FROM" ] || FROM="https://github.com/$REPO/releases/latest/download/veil_linux_$ARCH.tar.gz"

	WORK=$(mktemp -d)
	say "скачиваю ядро для $ARCH"

	if ! curl -fsSL --max-time 300 "$FROM" -o "$WORK/veil.tar.gz"; then
		say ''
		say "не скачалось: $FROM"
		say ''
		say 'Если репозиторий закрытый, готовые сборки по ссылке недоступны.'
		say 'Скачай архив со страницы релизов вручную, распакуй и запусти оттуда,'
		say 'либо укажи прямой адрес: --from https://…/veil_linux_'"$ARCH"'.tar.gz'
		die 'нет откуда взять бинарники'
	fi

	# Сумму сверяем, когда есть с чем: подменённый архив на сервере с правами
	# root — это не «неудобство», а чужой доступ ко всем покупателям.
	SUMS="${FROM%/*}/SHA256SUMS"
	if curl -fsSL --max-time 60 "$SUMS" -o "$WORK/SHA256SUMS" 2>/dev/null &&
		command -v sha256sum >/dev/null 2>&1; then
		WANT=$(awk -v f="veil_linux_$ARCH.tar.gz" '$2 == f || $2 == "*"f {print $1}' "$WORK/SHA256SUMS" | head -1)
		if [ -n "$WANT" ]; then
			GOT=$(sha256sum "$WORK/veil.tar.gz" | awk '{print $1}')
			[ "$WANT" = "$GOT" ] || die "контрольная сумма архива не сошлась. Ожидалась $WANT, получена $GOT"
			say 'контрольная сумма сошлась'
		fi
	else
		say 'ВНИМАНИЕ: контрольную сумму сверить не с чем, ставлю как есть'
	fi

	mkdir -p "$WORK/bin"
	tar -xzf "$WORK/veil.tar.gz" -C "$WORK/bin"
	BIN_DIR="$WORK/bin"
fi

for f in veil-panel veil-server veil-keygen; do
	[ -f "$BIN_DIR/$f" ] || die "в $BIN_DIR нет $f"
done

# ---------------------------------------------------------------- установка

say "ставлю панель в $DIR"
mkdir -p "$DIR/dist" "$DIR/acme"
umask 077

cp "$BIN_DIR/veil-panel" "$DIR/veil-panel"
cp "$BIN_DIR/veil-server" "$DIR/dist/veil-server"
cp "$BIN_DIR/veil-keygen" "$DIR/dist/veil-keygen"
chmod 755 "$DIR/veil-panel" "$DIR/dist/veil-server" "$DIR/dist/veil-keygen"

ADMIN_TOKEN=$("$DIR/veil-panel" -new-token)
[ -n "$ADMIN_TOKEN" ] || die 'не выпустился админский токен'

# Токен уезжает в файл окружения, а не в строку запуска: в строке его видел
# бы любой пользователь системы через ps.
printf 'VEIL_ADMIN_TOKEN=%s\n' "$ADMIN_TOKEN" > "$DIR/env"
chmod 600 "$DIR/env"

BASE="https://$DOMAIN"
if [ "$PORT" != "443" ]; then
	BASE="https://$DOMAIN:$PORT"
fi

ACME_EMAIL=''
[ -n "$EMAIL" ] && ACME_EMAIL=" -acme-email $EMAIL"

id -u veil >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin veil
chown -R veil:veil "$DIR"

cat > /etc/systemd/system/veil-panel.service <<UNITEOF
[Unit]
Description=Veil panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=veil
Group=veil
WorkingDirectory=$DIR
EnvironmentFile=$DIR/env
ExecStart=$DIR/veil-panel -listen 0.0.0.0:$PORT -db $DIR/panel.db -dist $DIR/dist -sub-base $BASE -acme-domain $DOMAIN -acme-cache $DIR/acme$ACME_EMAIL
Restart=on-failure
RestartSec=3

# Панель не root: права нужны только чтобы занять порты ниже 1024 — сам порт
# панели и 80-й для проверки владения доменом.
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
PrivateTmp=yes
ProtectHome=yes
ProtectSystem=strict
ReadWritePaths=$DIR

[Install]
WantedBy=multi-user.target
UNITEOF

systemctl daemon-reload
systemctl enable --now veil-panel >/dev/null 2>&1

# ---------------------------------------------------------------- проверка

say "жду сертификат"

# Первое обращение по https заставляет панель заказать сертификат. Пока он не
# получен, соединение не устанавливается — поэтому пробуем несколько раз.
i=0
OK=no
while [ "$i" -lt 40 ]; do
	if curl -fsS --max-time 10 "$BASE/healthz" -o /dev/null 2>/dev/null; then
		OK=yes
		break
	fi
	i=$((i + 1))
	sleep 3
done

if [ "$OK" != yes ]; then
	say ''
	say 'панель не отвечает по https. Последние строки журнала:'
	journalctl -u veil-panel -n 20 --no-pager -o cat || true
	die 'сертификат не получен'
fi

# Токен показывается один раз, поэтому отдельно и с воздухом вокруг: в конце
# длинной простыни вывода его проглядывают.
say ''
say ''
say '  ┌──────────────────────────────────────────────'
say '  │  АДМИНСКИЙ ТОКЕН — сохрани сейчас'
say '  │'
say "  │  $ADMIN_TOKEN"
say '  │'
say '  │  Им ты входишь в панель. Больше он показан не будет,'
say "  │  но лежит в $DIR/env"
say '  └──────────────────────────────────────────────'
say ''
say "  панель      $BASE"
say ''
say '  Добавить первую ноду: открой панель, вкладка «Ноды» → «Добавить ноду».'
say '  Панель выдаст готовую строку для нового сервера.'
say ''
say "  журнал      journalctl -u veil-panel -f"
say "  снести      systemctl disable --now veil-panel && rm -rf $DIR"
say "              внимание: в $DIR лежит база со всеми подписчиками"
say ''
