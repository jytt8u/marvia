#!/bin/sh
# Служба обновления Marvia. Её запускает systemd (marvia-upgrade.path), когда
# панель или нода кладёт файл-просьбу; сама по себе она не делает ничего.
#
# Панель и нода работают не от root и обновить себя не могут — и не должны:
# взломанная панель, которая умеет переписывать свои бинарники, — это взлом
# сервера. Поэтому здесь разделение ролей. Панель только просит. Что именно
# ставить, решает не она: сценарий обновления и архив берутся из последнего
# релиза на GitHub и сверяются с его SHA256SUMS. Содержимое просьбы не
# читается вовсе — файл лежит в каталоге, куда пишет служебный пользователь,
# и доверять в нём нечему.
#
# Этот файл встроен в бинарники (internal/updater) и ставится ими же
# (-install-updater), чтобы у панели, ноды и сценариев был один источник.

set -u

REPO=jytt8u/marvia
STATE=/var/lib/marvia-upgrade

# Просьбы убираем первым делом: path-юнит срабатывает, пока файл есть, и
# забытая просьба крутила бы обновление по кругу. rm удаляет саму запись
# каталога, даже если на её месте подложили ссылку, — за ссылкой не ходит.
rm -f /opt/marvia/upgrade/request /opt/marvia-node/upgrade/request

mkdir -p "$STATE"
chmod 755 "$STATE"

now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# status пишет состояние атомарно: временный файл и переименование. Каталог
# принадлежит root, так что подменить файл статуса ссылкой некому, но
# полузаписанный статус панель всё равно не должна увидеть.
status() {
	tmpf=$(mktemp "$STATE/.status.XXXXXX") || return 0
	printf 'state=%s\nat=%s\n' "$1" "$(now)" >"$tmpf"
	[ -n "${2:-}" ] && printf 'reason=%s\n' "$2" >>"$tmpf"
	chmod 644 "$tmpf"
	mv -f "$tmpf" "$STATE/status"
}

# Не чаще раза в пять минут. Просьбу может положить и взломанная панель, и
# каждая попытка качает приложения по десятку мегабайт; частое нажатие ничего
# не ускорит, а обстрел GitHub с сервера продавца ни к чему.
if [ -f "$STATE/started" ]; then
	last=$(stat -c %Y "$STATE/started" 2>/dev/null || echo 0)
	if [ $(($(date +%s) - last)) -lt 300 ]; then
		exit 0
	fi
fi
touch "$STATE/started"
status running

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if ! command -v gh >/dev/null 2>&1; then
	status failed 'нужен GitHub CLI с gh attestation verify; установи его из доверенного источника'
	exit 1
fi
url=$(curl --proto '=https' --proto-redir '=https' -fsSL --max-time 60 -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") || {
	status failed 'не удалось узнать релиз GitHub'; exit 1;
}
case "$url" in
	"https://github.com/$REPO/releases/tag/"*) tag=${url##*/} ;;
	*) status failed 'GitHub вернул посторонний адрес релиза'; exit 1 ;;
esac
if ! printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	status failed 'неверный тег релиза'; exit 1
fi
base="https://github.com/$REPO/releases/download/$tag"

if ! curl -fsSL --max-time 120 -o "$work/SHA256SUMS" "$base/SHA256SUMS" ||
	! curl -fsSL --max-time 120 -o "$work/upgrade.sh" "$base/upgrade.sh"; then
	status failed 'не скачался сценарий обновления с GitHub'
	exit 1
fi
if ! curl --proto '=https' --proto-redir '=https' -fsSL --max-time 120 -o "$work/SHA256SUMS.sigstore.json" "$base/SHA256SUMS.sigstore.json" ||
	! gh attestation verify "$work/SHA256SUMS" --bundle "$work/SHA256SUMS.sigstore.json" \
		--repo "$REPO" --cert-identity "https://github.com/$REPO/.github/workflows/release.yml@refs/tags/$tag" \
		--source-ref "refs/tags/$tag" --deny-self-hosted-runners >"$STATE/log" 2>&1; then
	status failed 'подпись релиза не прошла проверку'; exit 1
fi
if ! (cd "$work" && grep '[ *]upgrade.sh$' SHA256SUMS | sha256sum -c - >/dev/null 2>&1); then
	status failed 'сценарий обновления не сошёлся с SHA256SUMS релиза'
	exit 1
fi

if MARVIA_RELEASE_TAG="$tag" sh "$work/upgrade.sh" >>"$STATE/log" 2>&1; then
	status ok
else
	status failed 'сценарий обновления завершился ошибкой, подробности в журнале'
	exit 1
fi
