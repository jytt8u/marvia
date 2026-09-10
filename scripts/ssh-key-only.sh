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
KEYS=${KEYS:-$HOME/.ssh/authorized_keys}

die() { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 1; }
say() { printf '%s\n' "$1"; }

[ "$(id -u)" = 0 ] || die 'нужен root'
[ -f "$CONF" ] || die "$CONF не найден"

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

backup="$CONF.before-key-only.$(date +%Y%m%d-%H%M%S)"
cp "$CONF" "$backup"
say "прежняя настройка сохранена: $backup"

set_opt() {
	name=$1
	value=$2
	# Убираем все прежние упоминания, включая закомментированные: оставить их
	# значит гадать, какое из двух значений подействует.
	sed -i "/^[[:space:]]*#\?[[:space:]]*$name[[:space:]]/d" "$CONF"
	printf '%s %s\n' "$name" "$value" >> "$CONF"
}

printf '\n# Поставлено scripts/ssh-key-only.sh, %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" >> "$CONF"
set_opt PasswordAuthentication no
set_opt KbdInteractiveAuthentication no
set_opt ChallengeResponseAuthentication no
set_opt PermitRootLogin prohibit-password
set_opt PubkeyAuthentication yes

# ─────────────────────────────────────────────── проверить до применения

if ! sshd -t 2>/tmp/sshd-test.$$; then
	cp "$backup" "$CONF"
	say "$(cat /tmp/sshd-test.$$)"
	rm -f /tmp/sshd-test.$$
	die 'настройка не прошла проверку — вернул как было, ничего не изменилось'
fi
rm -f /tmp/sshd-test.$$
say 'настройка проверена'

# В Ubuntu 22.10 и новее ssh поднимается через сокет, и служба зовётся то ssh,
# то sshd. Перезагружаем ту, что есть; reload, а не restart — текущая сессия
# не рвётся.
if systemctl reload ssh 2>/dev/null || systemctl reload sshd 2>/dev/null; then
	say 'sshd перечитал настройки'
else
	cp "$backup" "$CONF"
	die 'не удалось перезагрузить sshd — вернул как было'
fi

# Debian и Ubuntu кладут свои переопределения сюда, и они сильнее основного
# файла: пароль может остаться включённым, хотя выше мы его выключили.
extra=$(grep -rlE '^[[:space:]]*PasswordAuthentication[[:space:]]+yes' \
	/etc/ssh/sshd_config.d/ 2>/dev/null || true)
if [ -n "$extra" ]; then
	printf '\n\033[33m!\033[0m пароль всё ещё включён в этих файлах — они сильнее основного:\n'
	printf '%s\n' "$extra" | sed 's/^/    /'
	printf '    поправь их и повтори: systemctl reload ssh\n'
fi

printf '\n\033[32mГотово.\033[0m Вход по паролю выключен.\n\n'
say 'ПРОВЕРЬ СЕЙЧАС, не закрывая это окно: открой новое и зайди по ключу.'
say "Если не пустит — вернуть как было: cp $backup $CONF && systemctl reload ssh"
printf '\n'
