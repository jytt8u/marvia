package mobile

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// TestConnectNamesFailureKind следит за тем, что вид неудачи действительно
// доезжает до приложения.
//
// Проверка выглядит мелкой, но держит важное свойство: приложение выбирает
// человеческую фразу по этой самой первой строке. Если вид перестанет
// проставляться, покупатель увидит «Что-то пошло не так» вместо внятного
// «ключ не подошёл» — и мы об этом узнаем только от него.
func TestConnectNamesFailureKind(t *testing.T) {
	cases := []struct {
		name string
		link string
		want string
	}{
		{
			name: "не ссылка вовсе",
			link: "просто текст",
			want: FailAccount,
		},
		{
			name: "чужая схема",
			link: "vless://тут-другое-приложение",
			want: FailAccount,
		},
		{
			name: "нет ключа",
			link: "marvia://panel.example.test/sub/token",
			want: FailAccount,
		},
		{
			name: "ключ не разбирается",
			link: "marvia://не-base64@panel.example.test/sub/token",
			want: FailAccount,
		},
		{
			name: "панель недостижима",
			// .invalid не резолвится никогда и нигде — это записано в
			// стандарте, поэтому тест не ходит в сеть по-настоящему.
			link: "marvia://YH3odubQhSmWQfCRwteeGN6pehcHq3DDcX_uGCvfl_Q@panel.example.invalid/sub/token",
			want: FailPanel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Каталог пустой: кэша в нём ещё нет, и подключение обязано идти
			// ровно тем же путём, каким шло до появления кэша.
			_, err := connect(tc.link, t.TempDir(), client.Events{})
			if err == nil {
				t.Fatalf("ожидалась ошибка, но подключение прошло")
			}

			kind, detail, found := strings.Cut(err.Error(), "\n")
			if !found {
				t.Fatalf("в ошибке нет вида, только текст: %q", err)
			}
			if kind != tc.want {
				t.Errorf("вид %q, ожидался %q (подробности: %s)", kind, tc.want, detail)
			}
			if strings.TrimSpace(detail) == "" {
				t.Errorf("вид есть, а подробностей нет — продавцу нечего будет разбирать")
			}
		})
	}
}

// TestCheckAccountLinkStaysPlain — проверка ссылки видом не помечается.
//
// Она показывается прямо под полем ввода, где человек только что вставил
// ссылку: там и так понятно, о чём речь, а лишняя строка сверху смотрелась бы
// мусором.
func TestCheckAccountLinkStaysPlain(t *testing.T) {
	err := CheckAccountLink("совсем не ссылка")
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("сообщение разбито на строки, а должно быть одной: %q", err)
	}
}

// Неудача обязана гаснуть.
//
// Один неудавшийся поток — обычное дело: цель недоступна, заблокирована или у
// неё только IPv6, до которого ноде не дотянуться. Такое сообщение висело
// рядом с «Подключено» вечно, пока всё работало, — и приучало не читать эту
// строку вовсе.
func TestLastErrorFades(t *testing.T) {
	tun := &Tunnel{}
	tun.note(errors.New("поток до цели: цель недоступна"))

	if tun.LastError() == "" {
		t.Fatal("свежая неудача не показана")
	}

	// Отматываем время вместо ожидания: тест, который ждёт минуту, перестают
	// запускать.
	tun.mu.Lock()
	tun.lastErrAt = time.Now().Add(-2 * errorLifetime)
	tun.mu.Unlock()

	if got := tun.LastError(); got != "" {
		t.Errorf("старая неудача всё ещё висит: %q", got)
	}
}

// А настоящая беда гаснуть не должна: надзор ставит её заново, пока не пройдёт.
func TestTroubleStaysWhileItLasts(t *testing.T) {
	tun := &Tunnel{}
	tun.trouble("нода не отвечает, и переехать не на что")

	if tun.LastError() == "" {
		t.Fatal("сообщение о неудачном переезде не показано")
	}
}
