#!/bin/sh
#
# Установка панели Marvia на чистый сервер.
#
#   ./install-panel.sh --domain panel.example.com [--port 443] [--email ты@example.com]
#
# Бинарники берутся сами: скрипт определяет разрядность сервера, скачивает
# архив, сверяет контрольную сумму и ставит. Компилятор не нужен — в этом весь
# смысл: продавец покупает сервер и продаёт доступ, не касаясь кода.
#
# Откуда качать, можно задать:
#   --from https://…/marvia_linux_amd64.tar.gz   готовый архив
#   --bin-dir /путь                            уже распакованные бинарники
#
# Ноды ставятся иначе: панель выдаёт готовую строку, и ей скачивать заранее
# ничего не надо. Панель — единственное место, куда бинарники приезжают сами.

set -eu

DOMAIN=''
PORT=443
EMAIL=''
DIR=/opt/marvia
MIGRATED=0
BIN_DIR=''
FROM=''
BEHIND_PROXY=0

# Сборки лежат отдельно от исходников: репозиторий с кодом закрыт, а качать
# панель должен уметь любой продавец, ничего у нас не спрашивая. Здесь нет ни
# строчки исходников — только собранные бинарники и суммы к ним.
REPO='jytt8u/marvia'

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
	--behind-proxy) BEHIND_PROXY=1; shift ;;
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
	die "$DIR уже существует. Здесь, похоже, уже стоит панель. Внутри база с подписчиками — снеси её осознанно: systemctl disable --now marvia-panel && rm -rf $DIR"
fi

# Домен обязан вести сюда.
#
# Проверяем до всего остального и особенно до переезда. Let's Encrypt
# ограничивает число неудачных проверок, но дороже другое: прерваться после
# того, как каталог уже переехал, — значит оставить панель лежать. Так и
# случилось на боевом сервере, поэтому порядок здесь именно такой.
say "проверяю, что $DOMAIN ведёт на этот сервер"

MY_IP=$(curl -fsS --max-time 10 https://api.ipify.org 2>/dev/null || true)
DOMAIN_IP=$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk '{print $1; exit}')

[ -n "$DOMAIN_IP" ] || die "у домена $DOMAIN нет записи A. Заведи её и подожди, пока разойдётся"

# Несовпадение бывает двух видов. Или запись правда не та — тогда сертификат
# не получить, и продолжать бессмысленно. Или домен спрятан за CDN, и тогда
# так и должно быть: проверка владения доменом проходит насквозь до панели.
# Отличить одно от другого отсюда нельзя, поэтому решает человек.
if [ -n "$MY_IP" ] && [ "$DOMAIN_IP" != "$MY_IP" ] && [ "$BEHIND_PROXY" != 1 ]; then
	die "$DOMAIN ведёт на $DOMAIN_IP, а этот сервер $MY_IP.
Поправь запись A и подожди, пока разойдётся.
Если домен спрятан за Cloudflare и так и задумано — повтори с ключом --behind-proxy"
fi

# Переезд со старого имени: гасим старую панель, но пока ничего не двигаем.
#
# До переименования панель жила в /opt/veil под службой veil-panel, и внутри
# лежит база со всеми покупателями продавца. Оставить её там и поставить рядом
# чистую — значит молча отобрать у человека бизнес.
#
# Останов нужен раньше проверки портов: старая панель держит и 80, и порт
# панели, и без этого проверка ниже сочтёт их занятыми. Файл службы пока
# оставляем — если дальше что-то не сложится, панель можно поднять обратно
# одной командой.
OLD_DIR=/opt/veil
if [ -d "$OLD_DIR" ]; then
	say "нашёл старую установку в $OLD_DIR"
	systemctl stop veil-panel >/dev/null 2>&1 || true
	MIGRATED=1
fi

# Порт 80 нужен для проверки владения доменом. Панель займёт его сама.
if command -v ss >/dev/null 2>&1; then
	for p in 80 "$PORT"; do
		if ss -tln 2>/dev/null | awk '{print $4}' | sed 's/.*://' | grep -qx "$p"; then
			[ "$MIGRATED" = 1 ] && say "старая панель остановлена; поднять обратно: systemctl start veil-panel"
			if [ "$p" = 80 ]; then
				die 'порт 80 занят. Он нужен, чтобы Let'"'"'s Encrypt проверил владение доменом. Освободи его: ss -tlnp | grep :80'
			fi
			die "порт $p занят. Возьми другой: --port 8443"
		fi
	done
fi

# Проверки пройдены — теперь можно двигать. Копию держим на месте, пока
# человек не убедится, что всё поднялось.
if [ "$MIGRATED" = 1 ]; then
	say "переношу $OLD_DIR вместе с базой"

	systemctl disable veil-panel >/dev/null 2>&1 || true
	rm -f /etc/systemd/system/veil-panel.service

	cp -a "$OLD_DIR" "$OLD_DIR.before-marvia"
	mv "$OLD_DIR" "$DIR"

	# Бинарники со старыми именами: панель перезапишется ниже, а ноду и
	# генератор ключей она раздаёт из dist по именам — их надо убрать, иначе
	# в каталоге будут лежать две пары и установщик ноды возьмёт не ту.
	rm -f "$DIR/veil-panel" "$DIR/dist/veil-server" "$DIR/dist/veil-keygen"

	# Приложения покупателям панель тоже раздаёт по именам.
	[ -f "$DIR/dist/veil-android.apk" ] && mv "$DIR/dist/veil-android.apk" "$DIR/dist/marvia-android.apk"
	[ -f "$DIR/dist/veil-windows.exe" ] && mv "$DIR/dist/veil-windows.exe" "$DIR/dist/marvia-windows.exe"
fi

# ---------------------------------------------------------- откуда бинарники

WORK=''
cleanup() { [ -n "$WORK" ] && rm -rf "$WORK"; }
trap cleanup EXIT

if [ -z "$BIN_DIR" ]; then
	# Рядом со скриптом уже лежат? Так бывает, когда человек скачал архив и
	# распаковал его руками — тогда качать второй раз незачем.
	HERE=$(cd "$(dirname "$0")" && pwd)
	if [ -f "$HERE/marvia-panel" ]; then
		BIN_DIR="$HERE"
	fi
fi

if [ -z "$BIN_DIR" ]; then
	case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "разрядность $(uname -m) не поддерживается: собери бинарники сам и укажи --bin-dir" ;;
	esac

	[ -n "$FROM" ] || FROM="https://github.com/$REPO/releases/latest/download/marvia_linux_$ARCH.tar.gz"

	WORK=$(mktemp -d)
	say "скачиваю ядро для $ARCH"

	if ! curl -fsSL --max-time 300 "$FROM" -o "$WORK/marvia.tar.gz"; then
		say ''
		say "не скачалось: $FROM"
		say ''
		say 'Чаще всего это значит, что с сервера не открывается github.com:'
		say 'так бывает у хостеров в Иране и Китае. Скачай архив на машину, с'
		say 'которой открывается, положи рядом и укажи --bin-dir,'
		say 'либо задай своё зеркало: --from https://…/marvia_linux_'"$ARCH"'.tar.gz'
		die 'нет откуда взять бинарники'
	fi

	# Сумму сверяем, когда есть с чем: подменённый архив на сервере с правами
	# root — это не «неудобство», а чужой доступ ко всем покупателям.
	SUMS="${FROM%/*}/SHA256SUMS"
	if curl -fsSL --max-time 60 "$SUMS" -o "$WORK/SHA256SUMS" 2>/dev/null &&
		command -v sha256sum >/dev/null 2>&1; then
		WANT=$(awk -v f="marvia_linux_$ARCH.tar.gz" '$2 == f || $2 == "*"f {print $1}' "$WORK/SHA256SUMS" | head -1)
		if [ -n "$WANT" ]; then
			GOT=$(sha256sum "$WORK/marvia.tar.gz" | awk '{print $1}')
			[ "$WANT" = "$GOT" ] || die "контрольная сумма архива не сошлась. Ожидалась $WANT, получена $GOT"
			say 'контрольная сумма сошлась'
		fi
	else
		say 'ВНИМАНИЕ: контрольную сумму сверить не с чем, ставлю как есть'
	fi

	mkdir -p "$WORK/bin"
	tar -xzf "$WORK/marvia.tar.gz" -C "$WORK/bin"
	BIN_DIR="$WORK/bin"
fi

for f in marvia-panel marvia-node marvia-keygen; do
	[ -f "$BIN_DIR/$f" ] || die "в $BIN_DIR нет $f"
done

# ---------------------------------------------------------------- установка

say "ставлю панель в $DIR"
# backup — сюда панель сама складывает копии базы. Каталог заводим здесь и
# отдаём его пользователю marvia вместе с остальным: созданный потом руками из-под
# root, он оставит панель без права записи, и продавец узнает об этом в тот
# день, когда копия понадобится.
mkdir -p "$DIR/dist" "$DIR/acme" "$DIR/backup"
umask 077

cp "$BIN_DIR/marvia-panel" "$DIR/marvia-panel"
cp "$BIN_DIR/marvia-node" "$DIR/dist/marvia-node"
cp "$BIN_DIR/marvia-keygen" "$DIR/dist/marvia-keygen"
chmod 755 "$DIR/marvia-panel" "$DIR/dist/marvia-node" "$DIR/dist/marvia-keygen"

# Приложения покупателей кладём рядом: раздавать их будет сама панель, с
# домена продавца.
#
# Иначе покупатель идёт за приложением в магазин или на github, а в России
# рубят и то, и другое: загрузка из Google Play и App Store ломается вместе с
# международными CDN, через которые раздаётся и github. Ссылка «скачай
# приложение» отваливается первой — когда человек уже заплатил.
#
# Не скачалось — не беда: панель просто не покажет ссылку, а продавец положит
# файлы руками позже.
for app in marvia-android.apk marvia-windows.exe; do
	if [ -f "$BIN_DIR/$app" ]; then
		cp "$BIN_DIR/$app" "$DIR/dist/$app"
	else
		curl -fsSL --max-time 300 \
			"https://github.com/$REPO/releases/latest/download/$app" \
			-o "$DIR/dist/$app" 2>/dev/null || rm -f "$DIR/dist/$app"
	fi
	[ -f "$DIR/dist/$app" ] && chmod 644 "$DIR/dist/$app"
done

if [ -f "$DIR/dist/marvia-android.apk" ]; then
	say 'приложения на месте: панель раздаёт их покупателям сама'
else
	say 'ВНИМАНИЕ: приложений нет — покупателям их скачивать неоткуда.'
	say "Положи marvia-android.apk и marvia-windows.exe в $DIR/dist"
fi

ADMIN_TOKEN=$("$DIR/marvia-panel" -new-token)
[ -n "$ADMIN_TOKEN" ] || die 'не выпустился админский токен'

# Токен уезжает в файл окружения, а не в строку запуска: в строке его видел
# бы любой пользователь системы через ps.
printf 'MARVIA_ADMIN_TOKEN=%s\n' "$ADMIN_TOKEN" > "$DIR/env"
chmod 600 "$DIR/env"

BASE="https://$DOMAIN"
if [ "$PORT" != "443" ]; then
	BASE="https://$DOMAIN:$PORT"
fi

ACME_EMAIL=''
[ -n "$EMAIL" ] && ACME_EMAIL=" -acme-email $EMAIL"

# Служебный пользователь. Старая установка работала под veil; заводим marvia
# и передаём ему каталог целиком, чтобы после переезда не осталось файлов,
# которые панель не может перезаписать.
id -u marvia >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin marvia
chown -R marvia:marvia ""

cat > /etc/systemd/system/marvia-panel.service <<UNITEOF
[Unit]
Description=Marvia panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=marvia
Group=marvia
WorkingDirectory=$DIR
EnvironmentFile=$DIR/env
ExecStart=$DIR/marvia-panel -listen 0.0.0.0:$PORT -db $DIR/panel.db -dist $DIR/dist -sub-base $BASE -acme-domain $DOMAIN -acme-cache $DIR/acme$ACME_EMAIL
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
systemctl enable --now marvia-panel >/dev/null 2>&1

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
	journalctl -u marvia-panel -n 20 --no-pager -o cat || true
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
say "  журнал      journalctl -u marvia-panel -f"
if [ "$MIGRATED" = 1 ]; then
	say ""
	say "Старый каталог сохранён в $OLD_DIR.before-marvia — удали его, когда убедишься, что панель работает:"
	say "  rm -rf $OLD_DIR.before-marvia"
	say ""
fi

say "  снести      systemctl disable --now marvia-panel && rm -rf $DIR"
say "              внимание: в $DIR лежит база со всеми подписчиками"
say ''
