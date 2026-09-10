package mobile

import (
	"strings"
	"testing"
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
			_, err := connect(tc.link, t.TempDir(), nil)
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
