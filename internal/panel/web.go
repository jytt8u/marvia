package panel

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"net/http"
	"strings"
)

// Страница панели вшита в бинарник: нода и панель должны оставаться одним
// файлом, который копируют на сервер и запускают. Отдельная папка со
// статикой — это ещё одна вещь, которую забудут положить рядом.
//
// Шрифты там же, и это не прихоть. Панель ставят на сервер продавца и
// открывают откуда угодно — в том числе оттуда, где половина зарубежных
// адресов недоступна. Панель для обхода блокировок, которая сама не
// отрисовывается без файла из-за границы, выглядела бы издевательством.
// Заодно это снимает утечку: каждое открытие панели иначе сообщало бы
// стороннему хосту, что её открыли и откуда.
//
//go:embed web/index.html
//go:embed web/fonts
var webFS embed.FS

// noncePlaceholder заменяется на одноразовое значение при каждой отдаче.
const noncePlaceholder = "%NONCE%"

// ServeApp отдаёт одностраничное приложение панели.
//
// Про заголовки. На этой странице живёт админский токен — в localStorage, как
// у всех подобных панелей. Значит любая возможность выполнить чужой скрипт в
// этом origin равносильна выдаче полного доступа. Поэтому политика жёсткая:
// разрешён ровно один скрипт, наш, опознаваемый по одноразовому значению
// nonce. Ни внешних адресов, ни выполнения строк, попавших в разметку.
func (a *API) ServeApp(w http.ResponseWriter, _ *http.Request) {
	raw, err := webFS.ReadFile("web/index.html")
	if err != nil {
		fail(w, http.StatusInternalServerError, "страница панели не найдена в сборке")
		return
	}

	nonce, err := newNonce()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	page := strings.Replace(string(raw), noncePlaceholder, nonce, 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'none'",
		"script-src 'nonce-" + nonce + "'",
		"style-src 'unsafe-inline'",
		// Шрифты только свои, вшитые. Внешних адресов здесь нет и не будет.
		"font-src 'self'",
		// Только встроенные картинки, и никаких внешних. Значок вкладки
		// нарисован прямо в странице; сеть для картинок панели не нужна
		// вовсе, а единственный внешний адрес в интерфейсе — это уже утечка
		// того, что панель открыли, и откуда.
		"img-src data:",
		"connect-src 'self'",
		"form-action 'none'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; "))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// Панель не должна попадать в кэши промежуточных прокси: в ответах API
	// уезжают секреты, а страница отдаёт одноразовый nonce.
	w.Header().Set("Cache-Control", "no-store")

	_, _ = w.Write([]byte(page))
}

func newNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(raw), nil
}

// ServeFont отдаёт вшитый шрифт.
//
// Отдельным маршрутом, а не строкой внутри страницы: файл в разметке пришлось
// бы кодировать в base64, он раздулся бы на треть, и браузер перекачивал бы
// его при каждой отдаче страницы — а страница отдаётся с запретом кэширования,
// потому что на ней одноразовый nonce.
func (a *API) ServeFont(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// Имя разрешаем по списку допустимых знаков, а не запрещаем опасные.
	// Запретный список на путях всегда оказывается неполным: кто-нибудь
	// присылает «..%2f» или юникодную косую, и маршрут отдаёт что попало.
	if !fontName(name) {
		fail(w, http.StatusNotFound, "нет такого шрифта")
		return
	}

	raw, err := webFS.ReadFile("web/fonts/" + name)
	if err != nil {
		fail(w, http.StatusNotFound, "нет такого шрифта")
		return
	}

	w.Header().Set("Content-Type", "font/woff2")
	// Шрифт не меняется никогда: имя файла привязано к сборке. Кэшируем
	// надолго, чтобы панель открывалась быстро и на плохой связи.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(raw)
}

// fontName проверяет, что это имя одного из наших файлов, а не путь.
func fontName(name string) bool {
	if !strings.HasSuffix(name, ".woff2") || len(name) > 64 {
		return false
	}

	base := name[:len(name)-len(".woff2")]
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
