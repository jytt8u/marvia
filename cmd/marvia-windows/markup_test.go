package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/look"
)

// Каждый элемент, который ищет код окна, должен быть в разметке.
//
// Проверка появилась не от любви к порядку. Правка разметки однажды убрала
// кнопку, а подписку на неё оставила, и $("proxy-off").addEventListener
// выбросил исключение прямо при загрузке страницы. Всё, что шло ниже, не
// выполнилось: перевод надписей, сохранение ключа, переключатель языка и опрос
// состояния. Окно выглядело живым — с русскими надписями, вшитыми в разметку
// запасным текстом, — и не обновляло статус вообще.
//
// Ни сборка, ни go vet такого не видят: для них это просто строка.
//
// Файл читаем с диска, а не через embed: тот объявлен в файле для Windows, а
// проверка должна идти на любой машине, включая ту, где собирается релиз.
func TestEveryElementLookedUpExists(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatalf("страница окна не читается: %v", err)
	}
	page := string(raw)

	lookup := regexp.MustCompile(`\$\("([a-z0-9-]+)"\)`)
	declared := regexp.MustCompile(`id="([a-z0-9-]+)"`)

	have := make(map[string]bool)
	for _, m := range declared.FindAllStringSubmatch(page, -1) {
		have[m[1]] = true
	}

	missing := make(map[string]bool)
	for _, m := range lookup.FindAllStringSubmatch(page, -1) {
		if !have[m[1]] {
			missing[m[1]] = true
		}
	}

	if len(missing) == 0 {
		return
	}
	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Errorf("код ищет элементы, которых нет в разметке: %s\n"+
		"одна такая пропажа обрывает загрузку страницы на середине",
		strings.Join(names, ", "))
}

// Надписи интерфейса обязаны быть в обоих языках.
//
// Ключ, забытый в одном словаре, не ломает ничего заметного: надпись просто
// исчезает, и заметит её отсутствие только тот, кто переключил язык. У нас так
// уже осиротели три ключа, а один — наоборот, потерялся.
func TestBothLanguagesKnowTheSameWords(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatalf("страница окна не читается: %v", err)
	}
	page := string(raw)

	ru, en := dictionaryKeys(t, page, "ru:"), dictionaryKeys(t, page, "en:")

	for key := range ru {
		if !en[key] {
			t.Errorf("ключ %q есть по-русски и потерян по-английски", key)
		}
	}
	for key := range en {
		if !ru[key] {
			t.Errorf("ключ %q есть по-английски и потерян по-русски", key)
		}
	}
}

// dictionaryKeys выбирает имена полей одного языка из словаря T.
//
// Разбирать JavaScript по-настоящему здесь незачем: словарь плоский, и все
// ключи записаны одинаково — имя, двоеточие, значение.
func dictionaryKeys(t *testing.T, page, lang string) map[string]bool {
	t.Helper()

	// Ищем начало словаря точно: просто «ru:» встречается и в другом месте
	// страницы, и разбор поехал бы от неверной точки.
	head := "\n  " + lang + " {"
	start := strings.Index(page, head)
	if start < 0 {
		t.Fatalf("в странице нет словаря %s", lang)
	}
	rest := page[start+len(head):]

	// Словарь кончается там, где начинается следующий язык или закрывается
	// сам T. Берём ближайшую из границ.
	end := len(rest)
	for _, mark := range []string{"\n  en: {", "\n  ru: {", "\n};"} {
		if at := strings.Index(rest, mark); at >= 0 && at < end {
			end = at
		}
	}

	// Ключи стоят по нескольку в строке: «tabHome: "…", tabKey: "…"». Значит
	// ловим и начало строки, и продолжение после запятой, иначе из каждой
	// строки увиделся бы только первый ключ.
	key := regexp.MustCompile(`(?m)(?:^|,)\s*([a-zA-Z][a-zA-Z0-9]*):`)
	found := make(map[string]bool)
	for _, m := range key.FindAllStringSubmatch(rest[:end], -1) {
		found[m[1]] = true
	}
	if len(found) == 0 {
		t.Fatalf("словарь %s пуст — разбор не сработал", lang)
	}
	return found
}

// Страница несёт метку общей темы — ровно одну — и не несёт своей таблицы.
//
// Без метки страница отдастся целой и упадёт на первой же строке, где спросят
// Look: окно останется серым и мёртвым, а ошибка — только в консоли движка,
// которую никто не откроет. Своя таблица страшнее: она работает, и именно
// поэтому расходится с панелью незаметно.
func TestPageCarriesSharedLook(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatalf("страница окна не читается: %v", err)
	}
	page := string(raw)

	if n := strings.Count(page, look.Marker); n != 1 {
		t.Fatalf("метка темы встречается %d раз, нужна ровно одна", n)
	}
	if strings.Contains(page, "const PRESETS = {") {
		t.Error("в странице своя таблица пресетов — она должна быть только в internal/look/look.js")
	}

	served := look.Inline(page)
	if strings.Contains(served, look.Marker) || !strings.Contains(served, "const Look = ") {
		t.Error("тема не встала на место метки")
	}
}

// У каждого готового вида из таблицы есть название на обоих языках — по
// номеру, как в панели.
//
// Названия лежат по номерам, и лишний или недостающий вид подписал бы
// карточки со сдвигом: «Изумруд» стал бы «Нефритом». Карточка без названия
// показала бы ключ вроде «teal» — рабочее слово, не предназначенное для глаз.
func TestEveryLookIsNamedInBothLanguages(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatalf("страница окна не читается: %v", err)
	}
	page := string(raw)

	want := len(look.Looks())
	if want == 0 {
		t.Fatal("в таблице нет ни одного вида")
	}

	// Список названий: themes: [ "…", "…", ], по одному на язык.
	lists := regexp.MustCompile(`themes: \[([^\]]*)\]`).FindAllStringSubmatch(page, -1)
	if len(lists) != 2 {
		t.Fatalf("списков названий %d, ожидалось два — по одному на язык", len(lists))
	}
	for i, l := range lists {
		got := strings.Count(l[1], `"`) / 2
		if got != want {
			t.Errorf("словарь %d: названий %d, видов в таблице %d", i+1, got, want)
		}
	}
}
