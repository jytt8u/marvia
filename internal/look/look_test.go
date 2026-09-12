package look

import (
	"net/http"
	"net/http/httptest"
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
		if !FontName(name) {
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
		if FontName(name) {
			t.Errorf("%s: имя %q прошло проверку", what, name)
		}
	}
}

func TestAssetNameAcceptsOurFiles(t *testing.T) {
	if !AssetName("marvia-mark.png") {
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
		if AssetName(name) {
			t.Errorf("%s: имя %q прошло проверку", what, name)
		}
	}
}

// Файлы действительно попали в сборку.
//
// Директива go:embed молча ничего не кладёт, если каталог пуст или переехал:
// сборка проходит, а страницы отдают 404 на каждый шрифт и рисуются
// системным. Каталог только что переезжал из панели — проверка не лишняя.
func TestEmbeddedFontsAreThere(t *testing.T) {
	want := []string{
		"fonts/onest-latin.woff2",
		"fonts/onest-cyrillic.woff2",
		"fonts/manrope-latin.woff2",
		"fonts/manrope-cyrillic.woff2",
		"fonts/geologica-latin.woff2",
		"fonts/geologica-cyrillic.woff2",
		"fonts/spectral-400-latin.woff2",
		"fonts/spectral-400-cyrillic.woff2",
		"fonts/spectral-600-latin.woff2",
		"fonts/spectral-600-cyrillic.woff2",
		// Ими набраны страницы: моноширинный — цифры и метки, Unbounded —
		// заголовки, Chakra Petch — надпись MARVIA.
		"fonts/jetbrains-latin.woff2",
		"fonts/jetbrains-cyrillic.woff2",
		"fonts/unbounded-latin.woff2",
		"fonts/unbounded-cyrillic.woff2",
		"fonts/chakra-petch-latin.woff2",
	}

	for _, path := range want {
		raw, err := files.ReadFile(path)
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

func TestEmbeddedAssetIsThere(t *testing.T) {
	raw, err := files.ReadFile("assets/marvia-mark.png")
	if err != nil {
		t.Fatalf("нет в сборке: %v", err)
	}
	// PNG начинается с \x89PNG. Проверяем, что это картинка, а не страница
	// ошибки, скачанная вместо неё.
	if len(raw) < 4 || string(raw[:4]) != "\x89PNG" {
		t.Errorf("не похож на png, первые байты %q", raw[:min(4, len(raw))])
	}
}

// Маршрут отдаёт шрифт по имени и отказывает на чужом — через настоящий
// mux, с тем же образцом пути, что и в панели и в окне.
func TestServeFontThroughMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fonts/{name}", ServeFont)
	mux.HandleFunc("GET /assets/{name}", ServeAsset)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	if rec := get("/fonts/onest-latin.woff2"); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("свой шрифт: код %d, тип %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get("/assets/marvia-mark.png"); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("свой знак: код %d, тип %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get("/fonts/nope.woff2"); rec.Code != http.StatusNotFound {
		t.Errorf("чужое имя: код %d, ожидался 404", rec.Code)
	}
	if rec := get("/fonts/look.js"); rec.Code != http.StatusNotFound {
		t.Errorf("не шрифт: код %d, ожидался 404", rec.Code)
	}
}

// Таблица тем цела: 27 пресетов, 28 видов, у каждого вида есть пресет.
//
// JavaScript не выполняется в тестах, поэтому читаем таблицу как текст. Этого
// достаточно: ключи записаны одинаково, и пропажу или опечатку в ключе вида
// увидит регулярное выражение — а не продавец, у которого одна из карточек
// вдруг красит панель в серый.
func TestThemeTableIsWhole(t *testing.T) {
	presets := Presets()
	if len(presets) != 27 {
		t.Errorf("пресетов %d, ожидалось 27", len(presets))
	}

	looks := Looks()
	if len(looks) != 28 {
		t.Errorf("видов %d, ожидалось 28", len(looks))
	}
	for _, l := range looks {
		if !presets[l[0]] {
			t.Errorf("вид ссылается на пресет %q, которого нет", l[0])
		}
	}

	if strings.Contains(js, Marker) {
		t.Error("метка вшивания попала в сам файл темы — при отдаче она осталась бы в странице")
	}
}

// Inline ставит тему ровно на место метки.
func TestInlineReplacesMarker(t *testing.T) {
	page := "<script>" + Marker + "\nconst x = Look.DEFAULT;</script>"
	out := Inline(page)
	if strings.Contains(out, Marker) {
		t.Error("метка осталась в странице")
	}
	if !strings.Contains(out, "const Look = ") {
		t.Error("тема не встала на место метки")
	}
	if got := Inline("без метки"); got != "без метки" {
		t.Errorf("страница без метки изменилась: %q", got)
	}
}
