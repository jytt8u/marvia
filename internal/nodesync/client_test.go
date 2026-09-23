package nodesync

import (
	"context"
	"encoding/json"
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

// TestNodeTellsItsVersionAndPassesOnTheUpgradeRequest: нода сообщает панели
// версию и разрядность, а просьбу панели обновиться передаёт дальше один раз,
// а не на каждом тике.
func TestNodeTellsItsVersionAndPassesOnTheUpgradeRequest(t *testing.T) {
	var reported atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/node/users":
			_, _ = w.Write([]byte(`{"users":[],"upgrade_to":"v0.12.0"}`))
		case "/api/v1/node/usage":
			var body struct{ Version, Arch string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			reported.Store(body.Version + "/" + body.Arch)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	registry, _ := users.NewRegistry(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var asked atomic.Int32
	var target atomic.Value
	go New(srv.URL, "t").WithBuild("v0.11.0", "arm64").Run(ctx, registry, time.Millisecond, Events{
		OnUpgrade: func(v string) { asked.Add(1); target.Store(v) },
	})

	waitFor(t, "нода попросила обновиться", func() bool { return asked.Load() > 0 })
	time.Sleep(50 * time.Millisecond)
	if n := asked.Load(); n != 1 {
		t.Fatalf("просьба передана %d раз за полсотни тиков", n)
	}
	if target.Load() != "v0.12.0" {
		t.Fatalf("просьба до %v", target.Load())
	}
	if reported.Load() != "v0.11.0/arm64" {
		t.Fatalf("панели сообщено %v", reported.Load())
	}
}

// TestUpgradeIsNotAskedForTheSameVersion: нода на той версии, до которой
// просят, ничего не просит; через полчаса просьба повторяется — служба могла
// не достучаться до релиза.
func TestUpgradeIsNotAskedForTheSameVersion(t *testing.T) {
	var u upgrader
	now := time.Now()
	if u.due("v1.0.0", "v1.0.0", now) || u.due("", "v1.0.0", now) {
		t.Fatal("просьба без нужды")
	}
	if !u.due("v1.1.0", "v1.0.0", now) {
		t.Fatal("первая просьба не передана")
	}
	if u.due("v1.1.0", "v1.0.0", now.Add(time.Minute)) {
		t.Fatal("повтор через минуту")
	}
	if !u.due("v1.1.0", "v1.0.0", now.Add(upgradeRetry+time.Second)) {
		t.Fatal("повтора нет и через полчаса")
	}
}

// TestOldPanelStillGetsTheUsage: панель старше 0.12 отвечает 400 на
// незнакомое поле version. Расход при этом обязан дойти — отчёт повторяется
// в прежнем виде, — а следующие полчаса нода версию не шлёт вовсе.
func TestOldPanelStillGetsTheUsage(t *testing.T) {
	var strictHits, accepted atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var body struct {
			Usage    map[string]users.Usage    `json:"usage"`
			Presence map[string]users.Presence `json:"presence"`
			SNIExtra []string                  `json:"sni_extra"`
		}
		if err := dec.Decode(&body); err != nil {
			strictHits.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		accepted.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "t").WithBuild("v0.12.0", "amd64")
	for range 3 {
		if err := c.ReportUsage(context.Background(), map[string]users.Usage{"a": {Up: 1}}, nil); err != nil {
			t.Fatalf("отчёт старой панели не дошёл: %v", err)
		}
	}
	if accepted.Load() != 3 || strictHits.Load() != 1 {
		t.Fatalf("принято %d, отказов %d: ждали три отчёта и один отказ", accepted.Load(), strictHits.Load())
	}
}
