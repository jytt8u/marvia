package panel_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

// Приложения раздаёт сама панель.
//
// Магазины приложений и международные CDN в России отваливаются первыми, а
// домен панели у покупателя уже рабочий: он ходит на него за подпиской.

const fakeAPK = "это как бы apk"

// appPanel поднимает панель с каталогом раздачи и кладёт туда приложения.
func appPanel(t *testing.T, files map[string]string) (*httptest.Server, string) {
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

	dist := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dist, name), []byte(body), 0o644); err != nil {
			t.Fatalf("выкладка %s: %v", name, err)
		}
	}

	srv := httptest.NewServer(panel.NewAPI(store, admin, "https://panel.example.test", dist).Handler())
	t.Cleanup(srv.Close)

	return srv, admin
}

// buySubscription заводит покупателя и отдаёт его токен подписки и ссылки.
func buySubscription(t *testing.T, srv *httptest.Server, admin string) (token, body string) {
	t.Helper()

	code, body := do(t, srv, "POST", "/api/v1/users", admin, `{"label":"покупатель"}`)
	if code != http.StatusOK {
		t.Fatalf("покупатель не завёлся: %d %s", code, body)
	}
	return between(t, body, `"sub_token":"`, `"`), body
}

// TestAppComesFromPanel — покупатель качает приложение с домена продавца.
func TestAppComesFromPanel(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	token, body := buySubscription(t, srv, admin)

	// Ссылка приходит вместе с доступом: отдельно её искать негде.
	if !strings.Contains(body, `"apps"`) || !strings.Contains(body, "/app/android") {
		t.Fatalf("в ответе нет ссылки на приложение: %s", body)
	}

	code, got := do(t, srv, "GET", "/sub/"+token+"/app/android", "", "")
	if code != http.StatusOK {
		t.Fatalf("приложение не отдалось: %d %s", code, got)
	}
	if got != fakeAPK {
		t.Errorf("отдалось не то: %q", got)
	}
}

// TestAppNeedsSubToken — по чужому токену приложение не отдаётся.
//
// Открытый файл apk на домене панели признаётся сканеру, что это панель обхода
// блокировок, на первом же запросе. У покупателя токен есть — он пришёл в той
// же ссылке, что и доступ.
func TestAppNeedsSubToken(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	buySubscription(t, srv, admin)

	for _, token := range []string{"чужой", "", "../../etc/passwd"} {
		code, _ := do(t, srv, "GET", "/sub/"+token+"/app/android", "", "")
		if code == http.StatusOK {
			t.Errorf("приложение отдалось по токену %q", token)
		}
	}
}

// TestUnknownAppIsNotAPath — имя приложения не превращается в путь на диске.
func TestUnknownAppIsNotAPath(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	token, _ := buySubscription(t, srv, admin)

	for _, name := range []string{"linux", "panel.db", "..%2Fpanel.db"} {
		code, _ := do(t, srv, "GET", "/sub/"+token+"/app/"+name, "", "")
		if code == http.StatusOK {
			t.Errorf("панель отдала %q", name)
		}
	}
}

// TestNoAppNoLink — ссылки на невыложенное приложение не бывает.
//
// Ссылка, ведущая в никуда, хуже её отсутствия: покупатель по ней сходит и
// придёт с вопросом к продавцу.
func TestNoAppNoLink(t *testing.T) {
	srv, admin := appPanel(t, nil)
	token, body := buySubscription(t, srv, admin)

	if strings.Contains(body, `"apps"`) {
		t.Errorf("ссылка на приложение есть, а файла нет: %s", body)
	}

	code, got := do(t, srv, "GET", "/sub/"+token+"/app/android", "", "")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("ожидался внятный отказ, пришло %d %s", code, got)
	}
	if !strings.Contains(got, "marvia-android.apk") {
		t.Errorf("в отказе не сказано, какой файл положить: %s", got)
	}
}

// TestListAppsTellsWhatIsLaidOut — продавец видит, что у него выложено.
func TestListAppsTellsWhatIsLaidOut(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})

	code, body := do(t, srv, "GET", "/api/v1/apps", admin, "")
	if code != http.StatusOK {
		t.Fatalf("список приложений не отдался: %d %s", code, body)
	}

	sum := sha256.Sum256([]byte(fakeAPK))
	if !strings.Contains(body, hex.EncodeToString(sum[:])) {
		t.Errorf("сумма не сошлась или её нет: %s", body)
	}
	if strings.Contains(body, "marvia-windows.exe") {
		t.Errorf("панель обещает то, чего не выложено: %s", body)
	}

	// А без токена — не отдаётся: что за приложения у продавца, посторонним
	// знать незачем.
	if code, _ := do(t, srv, "GET", "/api/v1/apps", "", ""); code != http.StatusUnauthorized {
		t.Errorf("список приложений отдался без токена: %d", code)
	}
}

// TestSubTokenRotates — утёкшую ссылку подписки можно сменить, не отзывая доступ.
//
// Ссылку покупатели раздают знакомым, а по ней отдаются список нод и секреты
// vless с trojan. Отзывать за это весь доступ — терять покупателя.
func TestSubTokenRotates(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	old, _ := buySubscription(t, srv, admin)

	if code, _ := do(t, srv, "GET", "/sub/"+old, "", ""); code != http.StatusOK {
		t.Fatalf("подписка не работает до смены: %d", code)
	}

	code, body := do(t, srv, "POST", "/api/v1/users/1/sub-token", admin, "")
	if code != http.StatusOK {
		t.Fatalf("токен не сменился: %d %s", code, body)
	}
	fresh := between(t, body, `"sub_token":"`, `"`)
	if fresh == old {
		t.Fatal("токен остался прежним")
	}
	if strings.Contains(body, "marvia://") {
		t.Errorf("смена адреса подписки выдала ключ доступа заново: %s", body)
	}

	if code, _ := do(t, srv, "GET", "/sub/"+old, "", ""); code != http.StatusNotFound {
		t.Errorf("старая ссылка всё ещё работает: %d", code)
	}
	if code, _ := do(t, srv, "GET", "/sub/"+fresh, "", ""); code != http.StatusOK {
		t.Errorf("новая ссылка не работает: %d", code)
	}

	// И приложение по старой ссылке больше не качается.
	if code, _ := do(t, srv, "GET", "/sub/"+old+"/app/android", "", ""); code == http.StatusOK {
		t.Error("приложение отдаётся по старой ссылке подписки")
	}
}
