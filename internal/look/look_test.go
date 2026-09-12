package look

import (
	"net/http"
	"net/http/httptest"
	"os"
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

	looks := LookList()
	if len(looks) != 28 {
		t.Errorf("видов %d, ожидалось 28", len(looks))
	}
	// Каждая ручка вида должна быть из своего списка: код темы кодирует их
	// по первым буквам, и незнакомое слово не влезет ни в один клиент.
	in := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	var radii, glows, densities, dirs []string
	for _, r := range Radii() {
		radii = append(radii, r.Key)
	}
	for _, g := range Glows() {
		glows = append(glows, g.Key)
	}
	for _, d := range Densities() {
		densities = append(densities, d.Key)
	}
	for _, d := range Dirs() {
		dirs = append(dirs, d.Key)
	}
	for _, l := range looks {
		switch {
		case !presets[l.Preset]:
			t.Errorf("вид ссылается на пресет %q, которого нет", l.Preset)
		case !in(Words("KINDS"), l.Kind), !in(dirs, l.Dir), !in(radii, l.Radius), !in(densities, l.Density),
			!in(Words("BUTTONS"), l.Btn), !in(glows, l.Glow), !in(Words("CARDS"), l.Card):
			t.Errorf("вид %q ссылается на незнакомую ручку: %+v", l.Preset, l)
		case l.Depth < 0.08 || l.Depth > 0.98:
			t.Errorf("вид %q: глубина %v вне 0.08…0.98", l.Preset, l.Depth)
		}
	}
	if len(radii) != 4 || len(glows) != 4 || len(dirs) != 9 || len(Words("KINDS")) != 4 ||
		len(Words("BUTTONS")) != 4 || len(Words("CARDS")) != 3 {
		t.Errorf("ручек разобрано не столько, сколько в таблице: radii %d, glows %d, dirs %d, kinds %d, buttons %d, cards %d",
			len(radii), len(glows), len(dirs), len(Words("KINDS")), len(Words("BUTTONS")), len(Words("CARDS")))
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

// Таблица на Kotlin не отстала от look.js.
//
// Android читает цвета из сгенерированного файла, и забытый go generate
// означал бы, что телефон красится вчерашними цветами, а панель и окно —
// сегодняшними. Никто из троих сам об этом не скажет.
func TestKotlinTableIsFresh(t *testing.T) {
	want := Kotlin()
	got, err := os.ReadFile(KotlinPath)
	if err != nil {
		t.Fatalf("таблицы для Android нет: %v — запусти go generate ./internal/look", err)
	}
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Error("LookTable.kt отстал от look.js — запусти go generate ./internal/look")
	}
}

// Генератор видит всё, что есть в таблице: каждый пресет, каждую плотность.
func TestKotlinTableCarriesWholeTable(t *testing.T) {
	presets := PresetList()
	if len(presets) != len(Presets()) {
		t.Errorf("строк пресетов разобрано %d, ключей %d — формат строки в look.js изменился", len(presets), len(Presets()))
	}
	if n := len(Densities()); n != 3 {
		t.Errorf("плотностей %d, ожидалось 3", n)
	}
	kt := Kotlin()
	for _, p := range presets {
		if !strings.Contains(kt, "Preset(\""+p.Key+"\"") {
			t.Errorf("пресет %q не попал в Kotlin", p.Key)
		}
	}
	// Вид из коробки — первый в списке видов, и в Kotlin он тоже первый.
	if !strings.Contains(kt, "    val looks: List<Look> = listOf(\n        Look(\"steel\", \"linear\"") {
		t.Error("вид из коробки не совпал с look.js")
	}
	for _, l := range LookList() {
		if !strings.Contains(kt, "Look(\""+l.Preset+"\", \""+l.Kind+"\", \""+l.Dir+"\"") {
			t.Errorf("вид %q не попал в Kotlin", l.Preset)
		}
	}
}

// У каждого пресета есть название в приложении на Android — на обоих языках.
//
// Телефон берёт название по ключу: look_<пресет>. Пропущенная строка не
// ломает сборку — экран покажет ключ «teal» вместо «Лагуны», и заметит это
// только тот, кто выбрал именно её.
func TestEveryPresetIsNamedOnAndroid(t *testing.T) {
	for _, file := range []string{
		"../../android/app/src/main/res/values/strings.xml",
		"../../android/app/src/main/res/values-ru/strings.xml",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("строки приложения не читаются: %v", err)
		}
		for key := range Presets() {
			if !strings.Contains(string(raw), `name="look_`+key+`"`) {
				t.Errorf("%s: пресет %q без названия", file, key)
			}
		}
	}
}
