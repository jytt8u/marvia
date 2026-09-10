package panel

import (
	"strings"
	"testing"
)

// Маршрут шрифтов читает вшитый файл по имени из адреса.
//
// Это то самое место, где в чужих панелях находят чтение любого файла: имя
// приходит снаружи, а дальше подставляется в путь. Проверяем не «работает ли
// хороший случай», а что плохие не проходят.

func TestFontNameAcceptsOurFiles(t *testing.T) {
	ours := []string{
		"onest-latin.woff2",
		"onest-cyrillic.woff2",
		"manrope-cyrillic.woff2",
		"geologica-latin.woff2",
		"spectral-400-cyrillic.woff2",
		"spectral-600-latin.woff2",
	}
	for _, name := range ours {
		if !fontName(name) {
			t.Errorf("свой файл отвергнут: %q", name)
		}
	}
}

func TestFontNameRejectsPaths(t *testing.T) {
	bad := map[string]string{
		"выход из каталога":     "../index.html.woff2",
		"точки внутри":          "..%2findex.woff2",
		"косая":                 "fonts/onest.woff2",
		"обратная косая":        `..\index.woff2`,
		"чужое расширение":      "index.html",
		"без расширения":        "onest",
		"пусто":                 "",
		"верхний регистр":       "Onest-Latin.WOFF2",
		"пробел":                "onest latin.woff2",
		"ноль-байт":             "onest\x00.woff2",
		"юникодная косая":       "onest∕latin.woff2",
		"слишком длинное":       string(make([]byte, 80)) + ".woff2",
		"только расширение":     ".woff2",
		"подчёркивание":         "onest_latin.woff2",
		"двойное расширение":    "onest.woff2.woff2",
		"скрытая точка в конце": "onest-latin..woff2",
	}

	for what, name := range bad {
		if fontName(name) {
			t.Errorf("%s: имя %q прошло проверку", what, name)
		}
	}
}

// TestEmbeddedFontsAreThere — файлы действительно попали в сборку.
//
// Директива go:embed молча ничего не кладёт, если каталог пуст или переехал:
// сборка проходит, а панель отдаёт 404 на каждый шрифт и рисуется системным.
func TestEmbeddedFontsAreThere(t *testing.T) {
	want := []string{
		"web/fonts/onest-latin.woff2",
		"web/fonts/onest-cyrillic.woff2",
		"web/fonts/manrope-latin.woff2",
		"web/fonts/manrope-cyrillic.woff2",
		"web/fonts/geologica-latin.woff2",
		"web/fonts/geologica-cyrillic.woff2",
		"web/fonts/spectral-400-latin.woff2",
		"web/fonts/spectral-400-cyrillic.woff2",
		"web/fonts/spectral-600-latin.woff2",
		"web/fonts/spectral-600-cyrillic.woff2",
		// Ими набрана нынешняя страница: моноширинный — цифры и метки,
		// Unbounded — заголовки, Chakra Petch — надпись MARVIA PARTNER.
		"web/fonts/jetbrains-latin.woff2",
		"web/fonts/jetbrains-cyrillic.woff2",
		"web/fonts/unbounded-latin.woff2",
		"web/fonts/unbounded-cyrillic.woff2",
		"web/fonts/chakra-petch-latin.woff2",
	}

	for _, path := range want {
		raw, err := webFS.ReadFile(path)
		if err != nil {
			t.Errorf("нет в сборке: %s (%v)", path, err)
			continue
		}
		// woff2 начинается с «wOF2». Проверяем, что это шрифт, а не
		// html-страница ошибки, скачанная вместо него.
		if len(raw) < 4 || string(raw[:4]) != "wOF2" {
			t.Errorf("%s не похож на woff2, первые байты %q", path, raw[:min(4, len(raw))])
		}
	}
}

// Маршрут картинок — то же слабое место, что и у шрифтов: имя приходит из
// адреса и подставляется в путь. Проверяем не «работает ли хороший случай»,
// а что плохие не проходят.

func TestAssetNameAcceptsOurFiles(t *testing.T) {
	if !assetName("marvia-mark.png") {
		t.Error("свой файл отвергнут: marvia-mark.png")
	}
}

func TestAssetNameRejectsPaths(t *testing.T) {
	bad := map[string]string{
		"выход из каталога":  "../index.html.png",
		"точки внутри":       "..%2findex.png",
		"косая":              "assets/mark.png",
		"обратная косая":     `..\index.png`,
		"чужое расширение":   "index.html",
		"без расширения":     "marvia-mark",
		"пусто":              "",
		"верхний регистр":    "Marvia-Mark.PNG",
		"пробел":             "marvia mark.png",
		"ноль-байт":          "marvia\x00.png",
		"юникодная косая":    "marvia∕mark.png",
		"слишком длинное":    string(make([]byte, 80)) + ".png",
		"только расширение":  ".png",
		"подчёркивание":      "marvia_mark.png",
		"двойное расширение": "mark.png.png",
	}

	for what, name := range bad {
		if assetName(name) {
			t.Errorf("%s: имя %q прошло проверку", what, name)
		}
	}
}

// TestEmbeddedAssetIsThere — знак действительно попал в сборку.
//
// Директива go:embed молча ничего не кладёт, если каталог переехал: сборка
// проходит, а панель отдаёт 404 и рисуется без знака.
func TestEmbeddedAssetIsThere(t *testing.T) {
	raw, err := webFS.ReadFile("web/assets/marvia-mark.png")
	if err != nil {
		t.Fatalf("нет в сборке: %v", err)
	}
	// PNG начинается с \x89PNG. Проверяем, что это картинка, а не страница
	// ошибки, скачанная вместо неё.
	if len(raw) < 4 || string(raw[:4]) != "\x89PNG" {
		t.Errorf("не похож на png, первые байты %q", raw[:min(4, len(raw))])
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
