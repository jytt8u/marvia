#!/bin/sh
# Проверка ноды: что с ней и почему она не работает.
#
# Запускать на самой ноде, от root:
#
#   curl -fsSL https://raw.githubusercontent.com/jytt8u/marvia/main/scripts/node-check.sh | sh
#
# или, если репозиторий уже склонирован, просто sh scripts/node-check.sh
#
# Скрипт ничего не меняет — только смотрит и печатает. Вывод можно целиком
# отдать в поддержку: секретов в нём нет. Приватный ключ ноды, её токен и
# содержимое env не печатаются никогда — про них сообщается только то, есть ли
# они и правильные ли у них права.

set -eu

DIR=${DIR:-/opt/marvia-node}
SERVICE=marvia-node

ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$1"; PROBLEMS=$((PROBLEMS + 1)); }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; }
# Просто факт: ни хорошо, ни плохо. Зелёная галочка рядом с таким фактом врёт —
# человек читает её как «проверено, всё в порядке».
note() { printf '  \033[2m·\033[0m %s\n' "$1"; }
info() { printf '    %s\n' "$1"; }
head_() { printf '\n\033[1m%s\033[0m\n' "$1"; }

PROBLEMS=0

# ─────────────────────────────────────────────────────────────── машина

head_ 'Машина'
info "$(uname -srm)"
if [ -r /etc/os-release ]; then
	. /etc/os-release
	info "${PRETTY_NAME:-неизвестная система}"
fi
info "работает $(uptime -p 2>/dev/null || uptime)"

# ─────────────────────────────────────────────────────────────── служба

head_ 'Служба'

if [ ! -d "$DIR" ]; then
	bad "$DIR не существует — нода здесь не установлена"
	printf '\nПроверять больше нечего.\n'
	exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
	bad 'нет systemd — служба не могла быть настроена'
else
	state=$(systemctl is-active "$SERVICE" 2>/dev/null || true)
	boot=$(systemctl is-enabled "$SERVICE" 2>/dev/null || true)

	case "$state" in
	active) ok "запущена ($(systemctl show -p ActiveEnterTimestamp --value "$SERVICE" 2>/dev/null || echo '?'))" ;;
	*)      bad "не запущена: состояние «$state»" ;;
	esac

	case "$boot" in
	enabled) ok 'включена в автозапуск' ;;
	*)       bad "не в автозапуске: «$boot» — после перезагрузки сервера нода не поднимется" ;;
	esac

	# Перезапуски. Одиночный — норма после обновления; десятки означают, что
	# нода падает и systemd её поднимает по кругу, а покупатели всё это время
	# видят обрывы.
	restarts=$(systemctl show -p NRestarts --value "$SERVICE" 2>/dev/null || echo 0)
	if [ "${restarts:-0}" -gt 3 ]; then
		bad "перезапусков: $restarts — нода падает и поднимается по кругу"
	else
		ok "перезапусков: ${restarts:-0}"
	fi
fi

if [ -x "$DIR/marvia-node" ]; then
	ver=$("$DIR/marvia-node" -version 2>/dev/null || echo 'не отвечает на -version')
	ok "версия: $ver"
else
	bad "$DIR/marvia-node нет или он не исполняемый"
fi

# ─────────────────────────────────────────────────────────────── секреты

head_ 'Ключи и настройки'
#
# Содержимое не печатаем ни при каких условиях: этот вывод пересылают. Смотрим
# только на то, что можно показать, — есть ли файл и кто его может прочитать.

for f in node.key env; do
	path="$DIR/$f"
	if [ ! -f "$path" ]; then
		bad "$path нет — нода не сможет ни представиться, ни дойти до панели"
		continue
	fi

	mode=$(stat -c '%a' "$path" 2>/dev/null || stat -f '%Lp' "$path" 2>/dev/null || echo '?')
	owner=$(stat -c '%U' "$path" 2>/dev/null || stat -f '%Su' "$path" 2>/dev/null || echo '?')

	case "$mode" in
	600|400) ok "$f: права $mode, владелец $owner" ;;
	*)       bad "$f: права $mode — их видит кто угодно на этой машине, надо 600" ;;
	esac
done

# ─────────────────────────────────────────────────────────────── порт

head_ 'Порт'

port=$(sed -n 's/.*-listen [0-9.]*:\([0-9]\+\).*/\1/p' \
	"/etc/systemd/system/$SERVICE.service" 2>/dev/null | head -1)
port=${port:-443}

if command -v ss >/dev/null 2>&1; then
	line=$(ss -lntp 2>/dev/null | grep ":$port " || true)
	if [ -n "$line" ]; then
		ok "слушается :$port"
		info "$(printf '%s' "$line" | tr -s ' ')"
	else
		bad ":$port никто не слушает — снаружи нода выглядит мёртвой"
	fi
else
	warn 'нет ss — проверить занятость порта нечем'
fi

if [ "$port" != 443 ]; then
	warn "порт $port, а не 443: настоящие сайты на нестандартных портах не живут,"
	info 'и корпоративные с гостиничными сетями такие порты режут'
fi

# ─────────────────────────────────────────────────────────────── прикрытие

head_ 'Сайт прикрытия'

dest=$(sed -n 's/.*-reality-dest \([^ ]*\).*/\1/p' \
	"/etc/systemd/system/$SERVICE.service" 2>/dev/null | head -1)

if [ -z "$dest" ]; then
	warn 'в строке запуска нет -reality-dest — REALITY не настроен'
else
	host=${dest%%:*}
	info "$dest"
	if command -v openssl >/dev/null 2>&1; then
		if echo | timeout 10 openssl s_client -connect "$dest" -servername "$host" \
			-tls1_3 >/dev/null 2>&1; then
			ok 'отвечает и умеет TLS 1.3'
		else
			bad 'недоступен отсюда или не умеет TLS 1.3 — с мёртвым прикрытием нода не спрячется'
		fi
	else
		warn 'нет openssl — прикрытие не проверить'
	fi
fi

# ─────────────────────────────────────────────────────────────── часы
#
# Самая обидная поломка: всё настроено верно, а клиенты не подключаются.
# VP1 отбрасывает рукопожатия, чьё время ушло больше чем на две минуты, — так
# он защищается от повтора. Разошлись часы — и нода отказывает всем подряд,
# ничем это не объясняя.

head_ 'Часы'

if command -v timedatectl >/dev/null 2>&1; then
	synced=$(timedatectl show -p NTPSynchronized --value 2>/dev/null || echo no)
	if [ "$synced" = yes ]; then
		ok 'время синхронизировано по NTP'
	else
		bad 'время НЕ синхронизировано — при расхождении больше двух минут нода откажет всем клиентам'
		info 'починить: timedatectl set-ntp true'
	fi
	info "$(timedatectl show -p Timezone --value 2>/dev/null || true), сейчас $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
else
	warn 'нет timedatectl — синхронность часов не проверить'
	info "сейчас $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
fi

# ─────────────────────────────────────────────────────────────── панель

head_ 'Связь с панелью'

panel=$(sed -n 's/.*-panel \([^ ]*\).*/\1/p' \
	"/etc/systemd/system/$SERVICE.service" 2>/dev/null | head -1)

if [ -z "$panel" ]; then
	warn 'в строке запуска нет -panel: нода работает сама по себе'
	info 'это рабочий режим, но список пользователей она не обновит'
else
	info "$panel"
	code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$panel/healthz" 2>/dev/null || echo 000)
	case "$code" in
	200) ok 'панель отвечает' ;;
	000) bad 'панель недоступна: не отвечает или не разрешается имя' ;;
	*)   bad "панель ответила $code" ;;
	esac
fi

# Файл расхода. Он же — признак того, что нода вообще кого-то обслуживает:
# пустой и нетронутый файл при живой службе означает, что до неё не доходят.
usage=$(ls -1t "$DIR"/*.usage.json 2>/dev/null | head -1 || true)
if [ -n "$usage" ]; then
	ok "расход пишется: $(basename "$usage"), обновлён $(date -r "$usage" '+%Y-%m-%d %H:%M' 2>/dev/null || echo '?')"
else
	warn 'файла расхода нет — либо нода только что поднялась, либо через неё никто не ходил'
fi

# ─────────────────────────────────────────────────────────────── firewall

head_ 'Межсетевой экран'

if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
	if ufw status 2>/dev/null | grep -q "^$port"; then
		ok "ufw включён, $port разрешён"
	else
		bad "ufw включён, а $port в правилах не найден — снаружи нода недоступна"
		info "починить: ufw allow $port/tcp"
	fi
else
	# Не успех и не провал. Для ноды отсутствующий экран — обычное дело: она
	# и должна принимать соединения снаружи, а прикрывать на ней, кроме ssh,
	# нечего. Плохо это становится только если на машине живёт что-то ещё.
	note 'ufw выключен или не установлен — порт ничем не прикрыт'
	info 'для ноды это обычно нормально: её порт и так должен быть открыт наружу'
fi

# ─────────────────────────────────────────────────────────────── журнал

head_ 'Последние ошибки в журнале'

if command -v journalctl >/dev/null 2>&1; then
	errs=$(journalctl -u "$SERVICE" --since '24 hours ago' --no-pager -o cat 2>/dev/null \
		| grep -iE 'error|ошибк|не удалось|отказ|panic' | tail -10 || true)
	if [ -n "$errs" ]; then
		printf '%s\n' "$errs" | sed 's/^/    /'
	else
		ok 'за сутки ошибок нет'
	fi
else
	warn 'нет journalctl'
fi

# ─────────────────────────────────────────────────────────────── итог

printf '\n'
if [ "$PROBLEMS" -eq 0 ]; then
	printf '\033[32mПроблем не найдено.\033[0m\n'
else
	printf '\033[31mНайдено проблем: %s\033[0m — они помечены ✗ выше.\n' "$PROBLEMS"
fi
printf '\n'
