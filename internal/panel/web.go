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
//go:embed web/index.html
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
