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
# Что делает: находит, что здесь стоит — панель, ноду или обе, — качает свежий
# релиз, сверяет контрольные суммы, останавливает службу, снимает копию базы
# (только с остановленной, см. backup_panel_db), подменяет бинарники и поднимает
# службы обратно. Если после подмены служба не встала, возвращает прежний
# бинарник вместе с той же копией базы: остаться без панели, обновляясь, нельзя.

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

# ─────────────────────────────────────────────── копия базы панели
#
# Копию снимаем не сейчас, а после остановки панели, и это принципиально.
#
# Панель держит SQLite в режиме WAL (internal/panel/store.go, PRAGMA
# journal_mode = WAL). Свежие транзакции — выданные ключи, заведённые ноды,
# продления — лежат не в panel.db, а рядом, в panel.db-wal, пока не случится
# контрольная точка. Копия одного panel.db с работающей панели — это база на
# момент последней контрольной точки, то есть без последних покупателей, и
# узнают об этом ровно тогда, когда копия понадобится.
#
# Сама панель поэтому снимает копии через VACUUM INTO (internal/panel/backup.go)
# — целостный снимок, не блокируя работу. Нам этот путь недоступен: у
# marvia-panel нет ключа командной строки «снять копию и выйти», копию умеет
# только работающая панель, по HTTP и под админским токеном, которого у скрипта
# нет. Остаётся второй честный способ — дождаться остановки: при штатном
# закрытии SQLite сливает WAL в основной файл, и panel.db становится полным.
#
# -wal и -shm всё равно копируем, если они остались: панель могли убить по
# таймауту, не дав закрыть базу, и тогда неслитый WAL — единственное место, где
# лежат последние транзакции.

PANEL_DB_COPY=''

backup_panel_db() {
	if [ -f "$PANEL_DIR/panel.db" ]; then
		PANEL_DB_COPY="$PANEL_DIR/panel.db.before-upgrade.$(date +%Y%m%d-%H%M%S)"
		cp "$PANEL_DIR/panel.db" "$PANEL_DB_COPY"
		# В базе токены подписок и секреты покупателей: права как у самой базы,
		# не шире.
		chmod 600 "$PANEL_DB_COPY"

		for side in -wal -shm; do
			[ -f "$PANEL_DIR/panel.db$side" ] || continue
			# Имя вида <копия>-wal: SQLite ищет журнал по имени основного файла,
			# так что возвращённая копия подхватит свой журнал, а не чужой.
			cp "$PANEL_DIR/panel.db$side" "$PANEL_DB_COPY$side"
			chmod 600 "$PANEL_DB_COPY$side"
		done

		ok "копия базы: $(basename "$PANEL_DB_COPY")"
	fi
	return 0
}

# Откат базы к той самой копии, что снята после остановки. Нужен, если новая
# версия успела привести базу к своей схеме и не поднялась: прежний бинарник с
# уехавшей вперёд базой — это вторая беда поверх первой.
restore_panel_db() {
	if [ -n "$PANEL_DB_COPY" ] && [ -f "$PANEL_DB_COPY" ]; then
		# Сначала убираем журналы, оставшиеся от неудачной версии. Иначе SQLite
		# при первом же открытии накатит чужой WAL на возвращённый файл, и
		# вместо отката получится смесь двух состояний.
		rm -f "$PANEL_DIR/panel.db-wal" "$PANEL_DIR/panel.db-shm"

		cp "$PANEL_DB_COPY" "$PANEL_DIR/panel.db"
		for side in -wal -shm; do
			[ -f "$PANEL_DB_COPY$side" ] || continue
			cp "$PANEL_DB_COPY$side" "$PANEL_DIR/panel.db$side"
			# Панель работает не от root: файл, созданный нами, должен
			# принадлежать тому же, кому принадлежит база, иначе панель не
			# сможет в него писать.
			chown --reference="$PANEL_DIR/panel.db" "$PANEL_DIR/panel.db$side" 2>/dev/null || true
		done

		say "    база возвращена из $(basename "$PANEL_DB_COPY")"
	fi
	return 0
}

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

# Имя сохраняем такое же, как в SHA256SUMS: sha256sum -c ищет файл по имени
# из списка, а не по тому, куда мы его положили.
archive="marvia_linux_$arch.tar.gz"

curl -fsSL -o "$tmp/$archive" "$base/$archive" \
	|| die 'не скачался архив релиза'
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" \
	|| die 'не скачались контрольные суммы'

# Сверяем до распаковки. Скачанный не тем бинарником сервер — это ровно та
# беда, ради которой суммы и публикуются.
if ! ( cd "$tmp" && grep "[ *]$archive\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1 ); then
	# Печатаем, что именно сказал sha256sum: без этого «сумма не сошлась»
	# одинаково означает и подмену файла, и нашу же опечатку в имени, и
	# отсутствие строки в списке — а чинить это три разных дела.
	( cd "$tmp" && grep "[ *]$archive\$" SHA256SUMS | sha256sum -c - 2>&1 | head -3 | sed 's/^/    /' ) || true
	die 'контрольная сумма не сошлась — скачалось не то, ничего не трогаю'
fi
ok 'контрольная сумма сошлась'

tar -xzf "$tmp/$archive" -C "$tmp" || die 'архив не распаковался'

# ─────────────────────────────────────────────── подмена

swap() {
	name=$1
	dir=$2
	service=$3
	# Что сделать после остановки службы, пока никто не пишет в её файлы.
	after_stop=${4:-}

	fresh="$tmp/$name"
	[ -x "$fresh" ] || die "в архиве нет $name"

	was=$("$dir/$name" -version 2>/dev/null || echo '?')
	now=$("$fresh" -version 2>/dev/null || echo '?')

	# Неизвестная версия не равна ничему, в том числе другой неизвестной.
	# Флаг -version появился не сразу: старый бинарник его не знает, обе
	# стороны превращаются в '?', и простое сравнение объявляло «уже ?,
	# подменять нечего» — обновление молча не происходило ровно там, где оно
	# нужнее всего, на самых старых установках.
	if [ "$was" != '?' ] && [ "$now" != '?' ] && [ "$was" = "$now" ]; then
		ok "$name: уже $now, подменять нечего"
		return 0
	fi

	say ''
	if [ "$was" = '?' ]; then
		say "$name: прежняя версия неизвестна (старый бинарник молчит на -version) → $now"
	else
		say "$name: $was → $now"
	fi

	systemctl stop "$service" 2>/dev/null || true

	if [ -n "$after_stop" ]; then
		"$after_stop"
	fi

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
		# Явно останавливаем: служба могла умирать не сразу, а перезапускаться
		# по кругу, и возвращать файлы под пишущим процессом нельзя.
		systemctl stop "$service" 2>/dev/null || true
		install -m 755 "$keep" "$dir/$name"
		if [ "$name" = marvia-panel ]; then
			restore_panel_db
		fi
		systemctl start "$service" || true
		journalctl -u "$service" -n 15 --no-pager -o cat || true
		die 'обновление отменено, работает прежняя версия'
	fi

	ok "$service работает на $now"
	rm -f "$keep"
}

# ─────────────────────────────────────────────── юнит ноды
#
# StartLimitIntervalSec и StartLimitBurst — ключи секции [Unit]. Установщик
# когда-то писал их в [Service], а там systemd их не знает и молча выбрасывает:
# «Unknown key name 'StartLimitIntervalSec' in section 'Service', ignoring».
# На живой ноде это видно по systemctl show: StartLimitIntervalUSec оставался
# 10s, то есть предела на перезапуски не было вовсе — упавшая нода поднималась
# по кругу и столько же раз била запросом по панели. Шаблон установщика уже
# поправлен, но у нод, поставленных раньше, юнит на диске остался прежним:
# сама собой эта правка к ним не приедет, переносим здесь.

fix_node_unit() {
	unit=/etc/systemd/system/marvia-node.service
	[ -f "$unit" ] || return 0
	command -v systemctl >/dev/null 2>&1 || return 0

	# Чиним только тот случай, ради которого пришли: ключи есть, но лежат не в
	# [Unit]. Правильный юнит не трогаем — переписывать чужой файл «на всякий
	# случай» значит однажды переписать его неправильно.
	if ! awk '
		/^[[:space:]]*\[/ { section = $1; next }
		/^[[:space:]]*StartLimit(IntervalSec|Burst)[[:space:]]*=/ {
			if (section != "[Unit]") misplaced = 1
		}
		END { exit(misplaced ? 0 : 1) }
	' "$unit"; then
		return 0
	fi

	work=$(mktemp -d)
	new="$work/marvia-node.service"

	# Ключи выносим сразу под заголовок [Unit]: внутри секции порядок неважен,
	# зато не приходится угадывать, куда воткнуть строки, чтобы не разорвать
	# чужой комментарий.
	if ! awk '
		/^[[:space:]]*StartLimit(IntervalSec|Burst)[[:space:]]*=/ { next }
		{ print }
		/^[[:space:]]*\[Unit\]/ && !moved {
			print "# Перенесено сюда из [Service]: там systemd эти ключи не читает."
			print "StartLimitIntervalSec=600"
			print "StartLimitBurst=20"
			moved = 1
		}
		END { exit(moved ? 0 : 1) }
	' "$unit" > "$new"; then
		rm -rf "$work"
		bad 'в юните ноды нет секции [Unit] — не трогаю его, почини руками'
		return 0
	fi

	# Пусть systemd сам скажет, что получилось: юнит службы, которая держит
	# всех покупателей этой ноды, чинят один раз и не вслепую.
	if command -v systemd-analyze >/dev/null 2>&1; then
		if ! out=$(systemd-analyze verify "$new" 2>&1); then
			bad 'systemd не принял исправленный юнит — оставляю прежний'
			printf '%s\n' "$out" | head -5 | sed 's/^/    /'
			rm -rf "$work"
			return 0
		fi
	fi

	keep_unit="$unit.before-upgrade.$(date +%Y%m%d-%H%M%S)"
	cp "$unit" "$keep_unit"
	install -m 644 "$new" "$unit"
	rm -rf "$work"

	systemctl daemon-reload

	ok 'юнит ноды: StartLimitIntervalSec и StartLimitBurst перенесены из [Service] в [Unit]'
	say "    прежний юнит сохранён: $(basename "$keep_unit")"
	say '    предел перезапусков теперь действует: 20 попыток за 10 минут'
	return 0
}

if [ "$HAVE_PANEL" = 1 ]; then
	swap marvia-panel "$PANEL_DIR" marvia-panel backup_panel_db
fi

if [ "$HAVE_NODE" = 1 ]; then
	swap marvia-node "$NODE_DIR" marvia-node
	# Отдельно от подмены: юнит чиним и тогда, когда бинарник уже свежий.
	fix_node_unit
fi

# Генератор ключей обновляем молча: он ничего не держит и ни от чего не зависит.
if [ "$HAVE_PANEL" = 1 ] && [ -x "$tmp/marvia-keygen" ]; then
	install -m 755 "$tmp/marvia-keygen" "$PANEL_DIR/marvia-keygen"
fi

say ''
printf '\033[32mГотово.\033[0m Проверить ноду: scripts/node-check.sh\n'
say ''
