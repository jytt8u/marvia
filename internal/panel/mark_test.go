package panel

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/look"
)

// Знак и надпись — единственное на странице, что ломается беззвучно.
//
// Картинка по битому пути, пропавший слой смешения, потерянная изоляция: ни в
// одном из случаев браузер не ругается. Панель просто открывается с серым
// знаком вместо цветного или с цветом, разлитым по шапке, — и продавец не
// понимает, он это сломал или так и было.

// page отдаёт разметку панели.
func page(t *testing.T) string {
	t.Helper()
	raw, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("страница не читается: %v", err)
	}
	return string(raw)
}

// rule достаёт тело одного CSS-правила.
//
// Селектор ищется с начала строки: иначе `.logo::after` нашёлся бы внутри
// общего `.logo::before, .logo::after`, и тест читал бы не то правило.
func rule(t *testing.T, selector string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`)
	m := re.FindStringSubmatch(page(t))
	if m == nil {
		t.Fatalf("правила %s на странице нет", selector)
	}
	return m[1]
}

var assetRef = regexp.MustCompile(`assets/([A-Za-z0-9._-]+)`)

// TestPageAssetsExist — каждый файл, на который ссылается страница, и правда
// отдаётся по своему адресу.
//
// Спрашиваем у настоящего раздатчика, а не смотрим в сборку: файл может в ней
// лежать и всё равно не отдаваться — имя не пройдёт проверку пути.
func TestPageAssetsExist(t *testing.T) {
	found := assetRef.FindAllStringSubmatch(page(t), -1)
	if len(found) == 0 {
		t.Fatal("страница не ссылается ни на один файл — знака и надписи на ней нет")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/{name}", look.ServeAsset)

	seen := map[string]bool{}
	for _, m := range found {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/"+name, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("страница просит assets/%s, а панель отвечает %d", name, rec.Code)
			continue
		}
		if rec.Body.Len() == 0 {
			t.Errorf("assets/%s отдался пустым", name)
		}
	}
}

// TestMarkTakesItsColourFromTheTheme — знак перекрашивается темой.
//
// Устроено это так: нижний слой обесцвечивает настоящий знак, верхний кладёт
// на него акцент в режиме color. Пропадёт обесцвечивание — акцент будет
// бороться с собственной синевой металла и в тёплых темах знак останется
// грязным. Пропадёт слой с акцентом — знак навсегда серый.
func TestMarkTakesItsColourFromTheTheme(t *testing.T) {
	under := rule(t, ".logo::before")
	if !strings.Contains(under, "grayscale(1)") {
		t.Errorf("знак не обесцвечен перед покраской: %s", strings.TrimSpace(under))
	}

	over := rule(t, ".logo::after")
	if !strings.Contains(over, "var(--acc") {
		t.Errorf("верхний слой знака залит не акцентом темы: %s", strings.TrimSpace(over))
	}
	if !strings.Contains(over, "mix-blend-mode: color") {
		t.Error("акцент кладётся не в режиме color — объём знака он закрасит целиком")
	}
	if !strings.Contains(over, "mask:") {
		t.Error("акцент не обрезан по форме знака — он зальёт прямоугольник")
	}
}

// TestMarkStaysGreyWithoutATheme — без темы знак серый, а не залитый чем
// попало.
//
// Экран входа живёт до того, как тема появилась: там --acc нет. Запасное
// значение обязано быть прозрачным — это и есть «бренд начинается с
// нейтрального холста».
func TestMarkStaysGreyWithoutATheme(t *testing.T) {
	over := rule(t, ".logo::after")
	if !regexp.MustCompile(`var\(--acc,\s*transparent\)`).MatchString(over) {
		t.Errorf("без темы знак закрасится непонятно чем: %s", strings.TrimSpace(over))
	}
}

// TestMarkDoesNotBleedOntoThePage — смешение заперто внутри знака.
//
// mix-blend-mode смешивает со всем, что под ним, а под ним — шапка панели.
// Без isolation акцент разливается по всей полосе, и выглядит это как
// испорченная тема, а не как ошибка в одном правиле.
func TestMarkDoesNotBleedOntoThePage(t *testing.T) {
	if !strings.Contains(rule(t, ".logo"), "isolation: isolate") {
		t.Error("у знака нет isolation: цвет протечёт на шапку")
	}
}

// TestWordmarkFollowsTheTextBesideIt — надпись заливается цветом текста.
//
// Надпись нарисована формой, а не набрана: «A» без перекладины и с точкой.
// Раз она стоит в строке рядом с текстом, то и цвет обязана брать оттуда же —
// иначе при смене темы надпись останется от прошлой.
func TestWordmarkFollowsTheTextBesideIt(t *testing.T) {
	word := rule(t, ".wordmark")

	if !strings.Contains(word, "background: currentColor") {
		t.Errorf("надпись залита не цветом текста: %s", strings.TrimSpace(word))
	}
	if !strings.Contains(word, "mask:") {
		t.Error("надпись не обрезана по форме — вместо букв будет прямоугольник")
	}
	// Без заданных пропорций маска растянется по высоте строки и буквы поплывут.
	if !strings.Contains(word, "aspect-ratio:") {
		t.Error("у надписи нет пропорций — буквы растянет")
	}
}

// TestBrandIsUsedWhereItWasDrawn — знак и надпись стоят и на входе, и в шапке.
func TestBrandIsUsedWhereItWasDrawn(t *testing.T) {
	p := page(t)

	if n := strings.Count(p, `class="logo"`); n < 2 {
		t.Errorf("знак встречается %d раз, ожидался на экране входа и в шапке", n)
	}
	if n := strings.Count(p, `class="wordmark"`); n < 2 {
		t.Errorf("надпись встречается %d раз, ожидалась на экране входа и в шапке", n)
	}
}
