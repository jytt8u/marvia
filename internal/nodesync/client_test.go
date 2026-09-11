package nodesync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/users"
)

// testUser — минимально валидная запись для реестра. Trojan выбран намеренно:
// его учётные данные — любой непустой пароль, без возни с ключами vp1.
func testUser(password string) users.User {
	return users.User{Kind: users.KindTrojan, Secret: password, Enabled: true}
}

// TestFetchUsersDisabled: 403 с телом панели → именно ErrNodeDisabled.
func TestFetchUsersDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"нода отключена"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "t").FetchUsers(context.Background())
	if !errors.Is(err, ErrNodeDisabled) {
		t.Fatalf("ожидали ErrNodeDisabled, получили %v", err)
	}
}

// TestFetchUsersForbiddenWithoutPanelBody: 403 от WAF (не JSON панели) — это
// обычная недоступность, а не выключение. Иначе случайный 403 прокси выбросил
// бы всех клиентов.
func TestFetchUsersForbiddenWithoutPanelBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html>403 Forbidden</html>"))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "t").FetchUsers(context.Background())
	if err == nil {
		t.Fatal("ожидали ошибку на 403 от WAF")
	}
	if errors.Is(err, ErrNodeDisabled) {
		t.Fatalf("403 от WAF не должен трактоваться как выключение: %v", err)
	}
}

// TestFetchUsersUnauthorized: 401 — обычная ошибка, не ErrNodeDisabled.
func TestFetchUsersUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "t").FetchUsers(context.Background())
	if err == nil {
		t.Fatal("ожидали ошибку на 401")
	}
	if errors.Is(err, ErrNodeDisabled) {
		t.Fatalf("401 не должен быть ErrNodeDisabled: %v", err)
	}
}

// TestFetchUsersOK: 200 возвращает корректный список.
func TestFetchUsersOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users":[{"kind":"trojan","secret":"alice","enabled":true}]}`))
	}))
	defer srv.Close()

	list, err := New(srv.URL, "t").FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(list) != 1 || list[0].Secret != "alice" {
		t.Fatalf("список разобран неверно: %+v", list)
	}
}

// TestRunDisabledMidLife — главный сценарий бага: нода работала, её выключили
// в панели. Реестр должен очиститься (Replace(nil)), процесс не паникует,
// цикл продолжается, а состояние журналируется один раз на переход.
func TestRunDisabledMidLife(t *testing.T) {
	// enabled переключает ответ сервера: сначала нода включена и отдаёт список,
	// после — выключена и отвечает 403 с телом панели.
	var enabled atomic.Bool
	enabled.Store(true)

	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/node/users":
			fetches.Add(1)
			if enabled.Load() {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"users":[{"kind":"trojan","secret":"alice","enabled":true}]}`))
				return
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"нода отключена"}`))
		case "/api/v1/node/usage":
			// Выключенная нода получает 403 и здесь — это ожидаемо.
			if !enabled.Load() {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"нода отключена"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	registry, err := users.NewRegistry([]users.User{testUser("bob")})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	var mu sync.Mutex
	var disabledCalls, enabledCalls int
	lastCount := -1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	client := New(srv.URL, "t")
	go func() {
		// Маленький интервал делает тест быстрым и детерминированным: ждём не
		// время, а факт нужного состояния реестра.
		client.Run(ctx, registry, time.Millisecond, Events{
			OnUsers: func(count int) {
				mu.Lock()
				lastCount = count
				mu.Unlock()
			},
			OnDisabled: func() {
				mu.Lock()
				disabledCalls++
				mu.Unlock()
			},
			OnEnabled: func() {
				mu.Lock()
				enabledCalls++
				mu.Unlock()
			},
			OnError: func(err error) {
				// Отмена контекста на остановке рвёт запрос в полёте — это
				// штатное завершение, а не сбой синхронизации.
				if errors.Is(err, context.Canceled) {
					return
				}
				t.Errorf("неожиданная ошибка синхронизации: %v", err)
			},
		})
		close(done)
	}()

	// Фаза 1: нода включена — дожидаемся непустого реестра.
	waitFor(t, "реестр наполнился", func() bool {
		return registry.Len() == 1
	})

	// Фаза 2: выключаем ноду в середине жизни.
	enabled.Store(false)
	waitFor(t, "реестр очищен после выключения", func() bool {
		return registry.Len() == 0
	})

	// Фаза 3: включаем обратно — нода сама возобновляет обслуживание.
	enabled.Store(true)
	waitFor(t, "реестр восстановлен после включения", func() bool {
		return registry.Len() == 1
	})

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run не завершился после отмены контекста")
	}

	mu.Lock()
	defer mu.Unlock()
	// Переход журналируется по одному разу, а не на каждом тике, хотя тиков за
	// время теста прошли сотни.
	if disabledCalls != 1 {
		t.Errorf("выключение должно журналироваться один раз, получили %d", disabledCalls)
	}
	if enabledCalls != 1 {
		t.Errorf("включение должно журналироваться один раз, получили %d", enabledCalls)
	}
	if lastCount != 1 {
		t.Errorf("после включения ожидали список из 1 пользователя, получили %d", lastCount)
	}
}

// waitFor крутит условие с коротким шагом вместо фиксированной задержки, чтобы
// тест был детерминированным и не зависел от скорости машины.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}
