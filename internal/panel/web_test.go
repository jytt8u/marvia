package panel

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/look"
)

// Страница несёт метку, на место которой встаёт общая тема, и не несёт
// своей копии таблицы.
//
// Без метки страница отдастся целой и упадёт на первой же строке, где
// спросят Look: панель окажется пустой, а ошибка — только в консоли браузера.
// Своя копия таблицы страшнее: она работает, и именно поэтому расходится с
// окном на компьютере незаметно.
func TestPageCarriesSharedLook(t *testing.T) {
	raw, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("страница не читается: %v", err)
	}
	page := string(raw)

	if n := strings.Count(page, look.Marker); n != 1 {
		t.Fatalf("метка темы встречается %d раз, нужна ровно одна", n)
	}
	if strings.Contains(page, "const PRESETS = {") {
		t.Error("в странице своя таблица пресетов — она должна быть только в internal/look/look.js")
	}

	// После вшивания метки не остаётся, а тема на месте.
	served := look.Inline(page)
	if strings.Contains(served, look.Marker) || !strings.Contains(served, "const Look = ") {
		t.Error("тема не встала на место метки")
	}
}

// Названий видов в словаре столько же, сколько видов в таблице, — на обоих
// языках. Названия лежат по номерам, и лишний или недостающий вид подписал
// бы карточки со сдвигом: «Изумруд» стал бы «Нефритом».
func TestEveryLookHasANameInBothLanguages(t *testing.T) {
	raw, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("страница не читается: %v", err)
	}

	want := len(look.Looks())
	if want == 0 {
		t.Fatal("в таблице нет ни одного вида")
	}

	// Список названий: themes: [ "…", "…", ], по одному на язык.
	lists := regexp.MustCompile(`themes: \[([^\]]*)\]`).FindAllStringSubmatch(string(raw), -1)
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

// Страница обязана начинаться с DOCTYPE.
//
// Проверка выглядит нелепой ровно до первого раза, когда она срабатывает.
// Правка скриптом дважды вписалась в начало файла вместо нужного места, и
// строка кода уехала на страницу продавца — он увидел её над панелью. Браузер
// при этом молчит: документ без DOCTYPE он не отвергает, а переключается в
// режим совместимости, и вёрстка едет незаметно.
func TestPageStartsWithDoctype(t *testing.T) {
	raw, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("страница не читается: %v", err)
	}

	const want = "<!DOCTYPE html>"
	if !strings.HasPrefix(string(raw), want) {
		head := string(raw)
		if len(head) > 120 {
			head = head[:120]
		}
		t.Fatalf("страница начинается не с %s, а с %q", want, head)
	}

	// И ровно один раз: второй DOCTYPE означает, что в файл что-то вклеилось.
	if n := strings.Count(string(raw), want); n != 1 {
		t.Errorf("DOCTYPE встречается %d раз, ожидался один", n)
	}
}

// Словарь d18 живёт только внутри renderVals.
//
// Это не вкусовщина, а единственное место, где такую ошибку можно поймать
// заранее. JavaScript не жалуется на неизвестное имя при загрузке файла: он
// падает в тот миг, когда до строки дошло исполнение. Функция sinceLabel
// обращалась к d18 снаружи и вызывалась только при живой ноде — на пустой
// панели всё выглядело здоровым, а у продавца с нодами страница навсегда
// замирала на «Обновляю…», потому что render() падал целиком.
//
// В разметке шаблона d18.* законен: туда его подставляет сам renderVals.
// Поэтому смотрим только на текст скрипта.
func TestDictionaryStaysInsideRenderVals(t *testing.T) {
	raw, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("страница не читается: %v", err)
	}
	lines := strings.Split(string(raw), "\n")

	scriptAt, valsAt, renderAt := -1, -1, -1
	for i, l := range lines {
		switch {
		case scriptAt < 0 && strings.HasPrefix(l, "<script"):
			scriptAt = i
		case valsAt < 0 && strings.HasPrefix(l, "function renderVals()"):
			valsAt = i
		case valsAt >= 0 && renderAt < 0 && strings.HasPrefix(l, "function render()"):
			renderAt = i
		}
	}
	if scriptAt < 0 || valsAt < 0 || renderAt < 0 {
		t.Fatalf("не нашёл границы: script=%d renderVals=%d render=%d", scriptAt, valsAt, renderAt)
	}

	// \b перед d18 отсекает шестнадцатеричные цвета вида #1fd18d, точка
	// после — обращение к полю, а не совпадение внутри другого слова.
	use := regexp.MustCompile(`\bd18\.`)

	for i := scriptAt; i < len(lines); i++ {
		if i >= valsAt && i < renderAt {
			continue // законная область
		}
		if use.MatchString(lines[i]) {
			t.Errorf("строка %d обращается к d18 вне renderVals — снаружи такого имени нет: %s",
				i+1, strings.TrimSpace(lines[i]))
		}
	}
}
