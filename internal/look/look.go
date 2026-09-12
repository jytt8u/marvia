// Пакет look — то, что делает панель и клиенты одинаковыми на вид: таблица
// тем, шрифты и знак.
//
// Тема живёт в одном файле look.js, который вшивается в страницу при отдаче.
// Держать копию таблицы в каждой странице было бы проще прямо сейчас — и через
// месяц цвета разъехались бы: одну правку внесли в панель и забыли про окно.
//
// Шрифты и знак тоже здесь, и тоже внутри бинарника. Панель ставят на сервер
// продавца и открывают откуда угодно — в том числе оттуда, где половина
// зарубежных адресов недоступна. Страница для обхода блокировок, которая сама
// не отрисовывается без файла из-за границы, выглядела бы издевательством.
// Заодно это снимает утечку: каждое открытие иначе сообщало бы стороннему
// хосту, что страницу открыли и откуда.
package look

import (
	"embed"
	"net/http"
	"regexp"
	"strings"
)

//go:embed look.js
var js string

//go:embed fonts
//go:embed assets
var files embed.FS

// Marker — место в странице, куда встаёт look.js. Стоит внутри её единственного
// скрипта: у панели он один и опознаётся по nonce, второго тега быть не может.
const Marker = "/*%LOOK%*/"

// Inline вшивает тему в страницу. Страница без метки возвращается как есть —
// это ошибка сборки, а не отдачи, и её ловит тест в пакете страницы.
func Inline(page string) string {
	return strings.Replace(page, Marker, js, 1)
}

// JS отдаёт текст темы — тестам страниц, чтобы сверить словари с таблицей.
func JS() string { return js }

// Presets отдаёт ключи пресетов из таблицы.
//
// Читаем таблицу как текст, а не выполняем: JavaScript в тестах на Go не
// запустить, а ключи записаны одинаково — имя, двоеточие, фигурная скобка.
// Нужно страницам: у каждой свой словарь названий, и тест сверяет, что ни один
// пресет не остался без имени и ни одно имя — без пресета.
func Presets() map[string]bool {
	out := make(map[string]bool)
	for _, m := range presetKey.FindAllStringSubmatch(js, -1) {
		out[m[1]] = true
	}
	return out
}

// Looks отдаёт готовые виды в порядке таблицы: пары «пресет, плотность».
func Looks() [][2]string {
	var out [][2]string
	for _, m := range lookRow.FindAllStringSubmatch(js, -1) {
		out = append(out, [2]string{m[1], m[2]})
	}
	return out
}

var (
	presetKey = regexp.MustCompile(`(?m)^    ([a-z]+): +\{ bg:`)
	lookRow   = regexp.MustCompile(`\["([a-z]+)", "(compact|normal|roomy)"\]`)
)

// ServeFont отдаёт вшитый шрифт по имени из адреса.
//
// Отдельным маршрутом, а не строкой внутри страницы: файл в разметке пришлось
// бы кодировать в base64, он раздулся бы на треть, и браузер перекачивал бы
// его при каждой отдаче страницы — а страницы отдаются с запретом кэширования.
func ServeFont(w http.ResponseWriter, r *http.Request) {
	serve(w, r.PathValue("name"), FontName, "fonts/", "font/woff2")
}

// ServeAsset отдаёт вшитую картинку по имени из адреса.
func ServeAsset(w http.ResponseWriter, r *http.Request) {
	serve(w, r.PathValue("name"), AssetName, "assets/", "image/png")
}

func serve(w http.ResponseWriter, name string, ok func(string) bool, dir, mime string) {
	// Имя приходит снаружи и подставляется в путь — ровно то место, где в
	// чужих панелях находят чтение любого файла. Поэтому проверяем не
	// «похоже ли на наше», а что ничего, кроме наших имён, не пройдёт.
	if !ok(name) {
		http.Error(w, "нет такого файла", http.StatusNotFound)
		return
	}
	raw, err := files.ReadFile(dir + name)
	if err != nil {
		http.Error(w, "нет такого файла", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", mime)
	// Файл не меняется никогда: имя привязано к сборке. Кэшируем надолго,
	// чтобы страница открывалась быстро и на плохой связи.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(raw)
}

// FontName проверяет, что это имя одного из наших шрифтов, а не путь.
func FontName(name string) bool { return plainName(name, ".woff2") }

// AssetName проверяет имя картинки тем же правилом.
func AssetName(name string) bool { return plainName(name, ".png") }

// plainName разрешает имя по списку допустимых знаков, а не запрещает
// опасные. Запретный список на путях всегда оказывается неполным: кто-нибудь
// присылает «..%2f» или юникодную косую, и маршрут отдаёт что попало.
func plainName(name, ext string) bool {
	if !strings.HasSuffix(name, ext) || len(name) > 64 {
		return false
	}
	base := name[:len(name)-len(ext)]
	if base == "" {
		return false
	}
	for _, c := range base {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}
