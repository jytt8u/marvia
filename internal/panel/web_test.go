package panel

import "testing"

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
