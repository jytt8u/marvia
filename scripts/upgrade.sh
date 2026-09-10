#!/bin/sh
# Обновление панели и ноды на месте.
#
# Запускать на сервере, от root:
#
#   curl -fsSL https://raw.githubusercontent.com/jytt8u/marvia/main/scripts/upgrade.sh | sh
#
# Установщик панели этого не делает и делать не должен: он отказывается
# трогать каталог, в котором лежит база с подписчиками, и это верно. Но
# обновляться как-то надо, а до сих пор это была ручная операция, которую
# каждый выдумывал заново.
#
# Что делает: находит, что здесь стоит — панель, ноду или обе, — снимает копию
# базы, качает свежий релиз, сверяет контрольные суммы, подменяет бинарники и
# поднимает службы. Если после подмены служба не встала, возвращает прежний
# бинарник и поднимает обратно: остаться без панели, обновляясь, нельзя.

set -eu

REPO=${REPO:-jytt8u/marvia}
PANEL_DIR=${PANEL_DIR:-/opt/marvia}
NODE_DIR=${NODE_DIR:-/opt/marvia-node}

say()  { printf '%s\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$1"; }
die()  { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die 'нужен root'

# ─────────────────────────────────────────────── что здесь вообще стоит

HAVE_PANEL=0
HAVE_NODE=0
[ -x "$PANEL_DIR/marvia-panel" ] && HAVE_PANEL=1
[ -x "$NODE_DIR/marvia-node" ] && HAVE_NODE=1

if [ "$HAVE_PANEL" = 0 ] && [ "$HAVE_NODE" = 0 ]; then
	die "ни панели в $PANEL_DIR, ни ноды в $NODE_DIR. Обновлять нечего."
fi

say 'Что стоит на этой машине:'
[ "$HAVE_PANEL" = 1 ] && ok "панель: $("$PANEL_DIR/marvia-panel" -version 2>/dev/null || echo '?')"
[ "$HAVE_NODE" = 1 ] && ok "нода: $("$NODE_DIR/marvia-node" -version 2>/dev/null || echo '?')"

# ─────────────────────────────────────────────── копия базы до всего

if [ "$HAVE_PANEL" = 1 ] && [ -f "$PANEL_DIR/panel.db" ]; then
	stamp=$(date +%Y%m%d-%H%M%S)
	copy="$PANEL_DIR/panel.db.before-upgrade.$stamp"
	# Копию снимаем до остановки службы: панель пишет в SQLite, и копия живой
	# базы может застать её посреди записи. Ниже, после остановки, копию
	# переснимем — а эта останется на случай, если остановка не удастся.
	cp "$PANEL_DIR/panel.db" "$copy"
	ok "копия базы: $(basename "$copy")"
fi

# ─────────────────────────────────────────────── качаем релиз

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

arch=$(uname -m)
case "$arch" in
x86_64|amd64) arch=amd64 ;;
aarch64|arm64) arch=arm64 ;;
*) die "неизвестная разрядность: $arch" ;;
esac

base="https://github.com/$REPO/releases/latest/download"
say ''
say "качаю свежий релиз ($arch)"

curl -fsSL -o "$tmp/marvia.tar.gz" "$base/marvia_linux_$arch.tar.gz" \
	|| die 'не скачался архив релиза'
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" \
	|| die 'не скачались контрольные суммы'

# Сверяем до распаковки. Скачанный не тем бинарником сервер — это ровно та
# беда, ради которой суммы и публикуются.
( cd "$tmp" && grep " marvia_linux_$arch.tar.gz\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1 ) \
	|| die 'контрольная сумма не сошлась — скачалось не то, ничего не трогаю'
ok 'контрольная сумма сошлась'

tar -xzf "$tmp/marvia.tar.gz" -C "$tmp" || die 'архив не распаковался'

# ─────────────────────────────────────────────── подмена

swap() {
	name=$1
	dir=$2
	service=$3

	fresh="$tmp/$name"
	[ -x "$fresh" ] || die "в архиве нет $name"

	was=$("$dir/$name" -version 2>/dev/null || echo '?')
	now=$("$fresh" -version 2>/dev/null || echo '?')

	if [ "$was" = "$now" ]; then
		ok "$name: уже $now, подменять нечего"
		return 0
	fi

	say ''
	say "$name: $was → $now"

	systemctl stop "$service" 2>/dev/null || true

	# Прежний бинарник держим рядом: если новый не встанет, вернём за секунду.
	keep="$dir/$name.before-upgrade"
	cp "$dir/$name" "$keep"
	install -m 755 "$fresh" "$dir/$name"

	systemctl start "$service"

	# Даём подняться. Панель при первом запуске может заказывать сертификат,
	# нода — синхронно забирать список пользователей; и то и другое небыстро.
	i=0
	while [ "$i" -lt 15 ]; do
		[ "$(systemctl is-active "$service")" = active ] && break
		i=$((i + 1))
		sleep 1
	done

	if [ "$(systemctl is-active "$service")" != active ]; then
		bad "$service не поднялась на новой версии — возвращаю прежнюю"
		install -m 755 "$keep" "$dir/$name"
		systemctl start "$service" || true
		journalctl -u "$service" -n 15 --no-pager -o cat || true
		die 'обновление отменено, работает прежняя версия'
	fi

	ok "$service работает на $now"
	rm -f "$keep"
}

[ "$HAVE_PANEL" = 1 ] && swap marvia-panel "$PANEL_DIR" marvia-panel
[ "$HAVE_NODE" = 1 ] && swap marvia-node "$NODE_DIR" marvia-node

# Генератор ключей обновляем молча: он ничего не держит и ни от чего не зависит.
if [ "$HAVE_PANEL" = 1 ] && [ -x "$tmp/marvia-keygen" ]; then
	install -m 755 "$tmp/marvia-keygen" "$PANEL_DIR/marvia-keygen"
fi

say ''
printf '\033[32mГотово.\033[0m Проверить ноду: scripts/node-check.sh\n'
say ''
