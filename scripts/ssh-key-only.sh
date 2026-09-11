#!/bin/sh
# Отключить вход по паролю, оставив только ключ.
#
# Запускать на сервере, от root, и обязательно — не закрывая текущую сессию.
#
# Порядок такой, а не иначе:
#
#   1. на своей машине:  ssh-keygen -t ed25519
#   2. на своей машине:  ssh-copy-id root@адрес
#   3. проверить, что вход по ключу работает — новым окном, старое не закрывать
#   4. только теперь запустить этот скрипт
#
# Скрипт откажется работать, если ключей на сервере нет: отключить пароль,
# не положив ключ, — это запереть себя снаружи, и останется только консоль
# хостера.

set -eu

CONF=/etc/ssh/sshd_config
DROPIN_DIR=/etc/ssh/sshd_config.d
DROPIN="$DROPIN_DIR/00-marvia-key-only.conf"
KEYS=${KEYS:-$HOME/.ssh/authorized_keys}

# sshd живёт в /usr/sbin, которого может не быть в PATH даже у root — например,
# когда скрипт запускают через sudo с урезанным окружением.
SSHD=$(command -v sshd 2>/dev/null || echo /usr/sbin/sshd)

die() { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 1; }
say() { printf '%s\n' "$1"; }

[ "$(id -u)" = 0 ] || die 'нужен root'
[ -f "$CONF" ] || die "$CONF не найден"
[ -x "$SSHD" ] || die "не нашёл sshd ($SSHD) — без него проверить настройку нечем"

# ─────────────────────────────────────────────── есть ли чем входить потом

if [ ! -s "$KEYS" ]; then
	die "в $KEYS нет ни одного ключа.

Сначала положи туда свой публичный ключ — со своей машины:

    ssh-copy-id root@$(hostname -I 2>/dev/null | awk '{print $1}')

и убедись, что вход по ключу работает, НЕ закрывая текущую сессию."
fi

count=$(grep -cE '^(ssh|ecdsa|sk-)' "$KEYS" || true)
[ "${count:-0}" -gt 0 ] || die "в $KEYS нет строк, похожих на ключи"
say "ключей найдено: $count"

# Права. sshd молча игнорирует authorized_keys, если до него может дотянуться
# кто-то кроме владельца, — и вход по ключу перестаёт работать без объяснений
# ровно тогда, когда пароль уже выключен.
chmod 700 "$(dirname "$KEYS")"
chmod 600 "$KEYS"
say 'права на ключи приведены в порядок'

# ─────────────────────────────────────────────────────────────── правка
#
# Куда писать — половина дела.
#
# Раньше директивы дописывались в конец sshd_config, и это ломалось двумя
# способами, оба тихие. Первый: в конце файла обычно стоит блок Match
# (Match User ansible, Match Address 10.0.0.0/8) — всё, что после него, живёт
# внутри блока и действует только на подходящие соединения, а для остальных
# пароль остаётся включён. Второй: sshd берёт ПЕРВОЕ встреченное значение, а не
# последнее, так что строка в конце проигрывает такой же строке выше.
#
# Поэтому пишем отдельным файлом-переопределением: на Debian и Ubuntu Include
# /etc/ssh/sshd_config.d/*.conf стоит первой строкой основного файла, а файл с
# префиксом 00- читается первым среди включаемых — его значения и побеждают.
# Если Include нет (старый sshd), вписываем в НАЧАЛО основного файла: там нас
# не съест Match и не перебьёт строка выше.

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

stamp=$(date +%Y%m%d-%H%M%S)
backup="$CONF.before-key-only.$stamp"
cp "$CONF" "$backup"
say "прежняя настройка сохранена: $backup"

DROPIN_BACKUP=''
if [ -f "$DROPIN" ]; then
	DROPIN_BACKUP="$DROPIN.before-key-only.$stamp"
	cp "$DROPIN" "$DROPIN_BACKUP"
fi

use_dropin=0
if grep -qiE '^[[:space:]]*Include[[:space:]]+.*sshd_config\.d' "$CONF"; then
	use_dropin=1
fi

# Возврат к тому, что было: основной файл из копии, наш файл — либо в прежнее
# состояние, либо прочь, если его до нас не существовало.
restore() {
	cp "$backup" "$CONF"
	if [ "$use_dropin" = 1 ]; then
		if [ -n "$DROPIN_BACKUP" ]; then
			cp "$DROPIN_BACKUP" "$DROPIN"
		else
			rm -f "$DROPIN"
		fi
	fi
	return 0
}

# Прежние упоминания этих директив убираем из основного файла, включая
# закомментированные. Не ради чистоты: любая такая строка выше Include окажется
# для sshd первой — и победит уже она, а не мы.
for opt in PasswordAuthentication KbdInteractiveAuthentication \
	ChallengeResponseAuthentication PermitRootLogin PubkeyAuthentication; do
	sed -i "/^[[:space:]]*#\?[[:space:]]*$opt[[:space:]]/d" "$CONF"
done

probe_opt() {
	# Спрашиваем у самого sshd, знает ли он директиву. Имена менялись:
	# ChallengeResponseAuthentication стал KbdInteractiveAuthentication, и на
	# старом sshd незнакомое имя роняет проверку целиком — то есть мы откатимся
	# и не выключим ничего вовсе. Лучше пропустить одну строку, чем не выключить
	# пароль.
	printf '%s %s\n' "$1" "$2" > "$work/probe.conf"
	if "$SSHD" -t -f "$work/probe.conf" 2>&1 | grep -qi 'bad configuration option'; then
		return 1
	fi
	return 0
}

block="$work/block.conf"
{
	printf '# Поставлено scripts/ssh-key-only.sh, %s\n' "$(date '+%Y-%m-%d %H:%M:%S')"
	printf '#\n'
	printf '# Вернуть вход по паролю: убрать этот файл и systemctl reload ssh\n'
} > "$block"

add_opt() {
	if probe_opt "$1" "$2"; then
		printf '%s %s\n' "$1" "$2" >> "$block"
	else
		say "  $1 этот sshd не знает — пропускаю"
	fi
}

add_opt PasswordAuthentication no
add_opt KbdInteractiveAuthentication no
add_opt ChallengeResponseAuthentication no
add_opt PermitRootLogin prohibit-password
add_opt PubkeyAuthentication yes

# Ради одной этой строки всё и затевалось. Если её в блоке нет — значит проверка
# отбраковала даже её, и писать остальное бессмысленно: получится настройка,
# которая выглядит сделанной, а пароль принимает.
if ! grep -q '^PasswordAuthentication no$' "$block"; then
	restore
	die "этот sshd не принимает даже PasswordAuthentication — вернул как было"
fi

if [ "$use_dropin" = 1 ]; then
	mkdir -p "$DROPIN_DIR"
	cat "$block" > "$DROPIN"
	# Секретов в файле нет, а вот читать его должен уметь кто угодно, кто
	# разбирается с настройкой; главное — не групповая и не чужая запись.
	chmod 644 "$DROPIN"
	chown root:root "$DROPIN" 2>/dev/null || true
	say "настройки записаны в $DROPIN"
else
	# Не mv: он заменил бы файл вместе с правами и владельцем, а sshd
	# отказывается читать конфиг, до которого дотягивается не root.
	{ cat "$block"; printf '\n'; cat "$CONF"; } > "$work/conf"
	cat "$work/conf" > "$CONF"
	say "у этого sshd нет Include — настройки вписаны в начало $CONF"
fi

# ─────────────────────────────────────────────── проверить до применения
#
# Файл для вывода — во временном каталоге от mktemp, а не /tmp/sshd-test.$$.
# Номер процесса предсказуем, /tmp пишут все, и root, перенаправляющий вывод в
# такое имя, однажды пишет в чужую подсунутую ссылку.
err="$work/sshd-t"
if ! "$SSHD" -t 2>"$err"; then
	restore
	say "$(cat "$err")"
	die 'настройка не прошла проверку — вернул как было, ничего не изменилось'
fi
say 'настройка проверена'

# В Ubuntu 22.10 и новее ssh поднимается через сокет, и служба зовётся то ssh,
# то sshd. Перезагружаем ту, что есть; reload, а не restart — текущая сессия
# не рвётся.
if systemctl reload ssh 2>/dev/null || systemctl reload sshd 2>/dev/null; then
	say 'sshd перечитал настройки'
else
	restore
	die 'не удалось перезагрузить sshd — вернул как было'
fi

# ─────────────────────────────────────────── что получилось на самом деле
#
# Спрашиваем не файлы, а сам sshd: `sshd -T` печатает настройку, с которой он
# будет работать, уже с учётом всех Include и порядка чтения. Это единственный
# ответ, которому можно верить, — и единственный способ не напечатать «готово»
# там, где пароль остался включён.

status=0
left=''

if effective=$("$SSHD" -T 2>/dev/null); then
	while IFS=' ' read -r key value; do
		case "$key $value" in
		'passwordauthentication yes' | 'kbdinteractiveauthentication yes' | \
			'challengeresponseauthentication yes' | 'permitrootlogin yes')
			left="$left $key"
			;;
		esac
	done <<EOF
$effective
EOF
fi

# sshd -T не говорит, чьи это строки. Поэтому заодно показываем файлы
# переопределений, где вход всё ещё разрешён: чинить придётся именно их.
# Смотрим туда только если Include есть: без него эти файлы просто лежат на
# диске и ни на что не влияют, и ругаться на них — пугать зря.
guilty=''
if [ "$use_dropin" = 1 ] && [ -d "$DROPIN_DIR" ]; then
	guilty=$(grep -rilE \
		'^[[:space:]]*(PasswordAuthentication|KbdInteractiveAuthentication|ChallengeResponseAuthentication|PermitRootLogin)[[:space:]]+yes' \
		"$DROPIN_DIR" 2>/dev/null | grep -v "^$DROPIN\$" || true)
fi

undo="cp $backup $CONF"
if [ "$use_dropin" = 1 ]; then
	undo="rm -f $DROPIN && $undo"
fi

printf '\n'
if [ -n "$left" ] || [ -n "$guilty" ]; then
	status=1
	printf '\033[33mСделано наполовину.\033[0m\n'
	if [ -n "$left" ]; then
		say "sshd, по его собственному ответу на -T, всё ещё разрешает:$left"
	fi
	if [ -n "$guilty" ]; then
		say 'включено вот здесь:'
		printf '%s\n' "$guilty" | sed 's/^/    /'
		say ''
		say 'sshd берёт первое встреченное значение, а порядок чтения задаётся именами'
		say "файлов. $(basename "$DROPIN") читается раньше почти всех, но держать рядом"
		say 'файл с обратным значением нельзя: достаточно переименовать его на'
		say '0-что-нибудь, и пароль включится обратно, ничего об этом не сказав.'
	fi
	say ''
	say 'Убери эти строки и повтори: systemctl reload ssh'
else
	printf '\033[32mГотово.\033[0m Вход по паролю выключен.\n'
fi

printf '\n'
say 'ПРОВЕРЬ СЕЙЧАС, не закрывая это окно: открой новое и зайди по ключу.'
say "Если не пустит — вернуть как было: $undo && systemctl reload ssh"
printf '\n'

exit "$status"
