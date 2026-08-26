package panel_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veilproject/veil/internal/panel"
)

// keyPanel поднимает панель с админским токеном для проверок ключей.
func keyPanel(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	admin, err := panel.NewToken()
	if err != nil {
		t.Fatalf("токен: %v", err)
	}

	srv := httptest.NewServer(panel.NewAPI(store, admin, "https://panel.example.test", t.TempDir()).Handler())
	t.Cleanup(srv.Close)
	return srv, admin
}

func do(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	t.Helper()

	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}

	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("обращение к панели: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(raw)
}

// TestBotKeyCannotTouchNodes — ради этого всё и делалось.
//
// Раньше бот ходил админским токеном и мог снести ноду вместе со всей её
// статистикой. Теперь у него ключ только на покупателей, и до нод он не
// дотягивается.
func TestBotKeyCannotTouchNodes(t *testing.T) {
	srv, admin := keyPanel(t)

	code, body := do(t, srv, "POST", "/api/v1/keys", admin, `{"name":"бот","scopes":["users"]}`)
	if code != http.StatusOK {
		t.Fatalf("ключ не выпустился: %d %s", code, body)
	}

	secret := between(t, body, `"secret":"`, `"`)
	if !strings.HasPrefix(secret, "vk_") {
		t.Errorf("у ключа нет приставки vk_: %q", secret)
	}

	// Своё дело делает.
	if code, body := do(t, srv, "POST", "/api/v1/users", secret, `{"label":"покупатель"}`); code != http.StatusOK {
		t.Fatalf("ключ не смог завести покупателя: %d %s", code, body)
	}

	// Чужое — нет.
	code, body = do(t, srv, "POST", "/api/v1/nodes/invite", secret, `{"label":"нода"}`)
	if code != http.StatusForbidden {
		t.Fatalf("ключ на покупателей добрался до нод: %d %s", code, body)
	}
	if !strings.Contains(body, "nodes") {
		t.Errorf("в отказе не сказано, какого права не хватает: %s", body)
	}
}

// TestKeyCannotMintKeys — ключ не выпускает ключи.
//
// Иначе разделение ничего не стоит: утёкший ключ бота выписал бы себе полный
// доступ за один запрос.
func TestKeyCannotMintKeys(t *testing.T) {
	srv, admin := keyPanel(t)

	_, body := do(t, srv, "POST", "/api/v1/keys", admin, `{"name":"всё","scopes":["users","nodes","read"]}`)
	secret := between(t, body, `"secret":"`, `"`)

	code, _ := do(t, srv, "POST", "/api/v1/keys", secret, `{"name":"ещё","scopes":["users"]}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("ключ со всеми правами выпустил себе ещё один: %d", code)
	}
}

// TestRevokedKeyStopsWorking — отзыв действует сразу.
func TestRevokedKeyStopsWorking(t *testing.T) {
	srv, admin := keyPanel(t)

	_, body := do(t, srv, "POST", "/api/v1/keys", admin, `{"name":"бот","scopes":["users"]}`)
	secret := between(t, body, `"secret":"`, `"`)
	id := between(t, body, `"id":`, `,`)

	if code, _ := do(t, srv, "GET", "/api/v1/users", secret, ""); code != http.StatusOK {
		t.Fatalf("ключ не работает до отзыва: %d", code)
	}

	if code, body := do(t, srv, "DELETE", "/api/v1/keys/"+id, admin, ""); code != http.StatusNoContent {
		t.Fatalf("ключ не отозвался: %d %s", code, body)
	}

	if code, _ := do(t, srv, "GET", "/api/v1/users", secret, ""); code != http.StatusUnauthorized {
		t.Fatalf("отозванный ключ всё ещё работает: %d", code)
	}

	// И пропал из списка — иначе продавец решит, что отзыв не сработал.
	if _, body := do(t, srv, "GET", "/api/v1/keys", admin, ""); strings.Contains(body, `"бот"`) {
		t.Errorf("отозванный ключ остался в списке: %s", body)
	}
}

// TestAdminTokenStillWorks — старый способ не сломался.
//
// У продавца, который уже написал бота на админском токене, ничего не должно
// перестать работать от одного обновления панели.
func TestAdminTokenStillWorks(t *testing.T) {
	srv, admin := keyPanel(t)

	for _, path := range []string{"/api/v1/users", "/api/v1/nodes", "/api/v1/keys"} {
		if code, body := do(t, srv, "GET", path, admin, ""); code != http.StatusOK {
			t.Errorf("%s под админским токеном: %d %s", path, code, body)
		}
	}
}

// TestUnknownScopeRejected — опечатка в правах не проходит молча.
func TestUnknownScopeRejected(t *testing.T) {
	srv, admin := keyPanel(t)

	code, body := do(t, srv, "POST", "/api/v1/keys", admin, `{"name":"бот","scopes":["userz"]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("неизвестное право принято: %d %s", code, body)
	}
	if !strings.Contains(body, "userz") {
		t.Errorf("в ошибке не названо само право: %s", body)
	}
}

// between достаёт кусок ответа между двумя метками.
func between(t *testing.T, s, from, to string) string {
	t.Helper()

	i := strings.Index(s, from)
	if i < 0 {
		t.Fatalf("в ответе нет %q: %s", from, s)
	}
	rest := s[i+len(from):]

	j := strings.Index(rest, to)
	if j < 0 {
		t.Fatalf("в ответе нет закрывающего %q: %s", to, s)
	}
	return rest[:j]
}

// TestVersionNeedsAuth — версия не отдаётся мимо авторизации.
//
// Точная версия сборки говорит сканеру, какие дыры пробовать, и опознаёт
// панель как нашу. Поэтому в открытом /healthz её нет, а за токеном — есть:
// продавцу она нужна ровно в тот момент, когда он пишет в поддержку.
func TestVersionNeedsAuth(t *testing.T) {
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	admin, err := panel.NewToken()
	if err != nil {
		t.Fatalf("токен: %v", err)
	}

	api := panel.NewAPI(store, admin, "https://panel.example.test", t.TempDir()).WithVersion("v9.9.9")
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	if code, body := do(t, srv, "GET", "/healthz", "", ""); strings.Contains(body, "9.9.9") {
		t.Errorf("версия видна без авторизации: %d %s", code, body)
	}

	if code, _ := do(t, srv, "GET", "/api/v1/version", "", ""); code != http.StatusUnauthorized {
		t.Errorf("версия отдалась без токена: %d", code)
	}

	code, body := do(t, srv, "GET", "/api/v1/version", admin, "")
	if code != http.StatusOK || !strings.Contains(body, "v9.9.9") {
		t.Errorf("под админским токеном версии нет: %d %s", code, body)
	}
}
