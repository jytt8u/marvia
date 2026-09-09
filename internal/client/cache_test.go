package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// Тесты кэша подписки.
//
// Проверяем ровно одно свойство, ради которого кэш и заводился: пока работает
// хоть одна нода из кэша, клиент не ходит в панель. Каждый такой поход — это
// запрос имени непубличного домена, ушедший провайдеру открытым текстом, и
// именно по частоте таких запросов домен подписки находят и закрывают.

// panelStub — панель, которая считает, сколько раз к ней сходили.
type panelStub struct {
	*httptest.Server
	hits atomic.Int64
}

func startPanel(t *testing.T, nodes ...client.Node) *panelStub {
	t.Helper()

	p := &panelStub{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.hits.Add(1)
		_ = json.NewEncoder(w).Encode(client.Subscription{Nodes: nodes, TrafficLimit: 1000, Used: 400})
	}))
	t.Cleanup(p.Close)

	return p
}

func (p *panelStub) subURL() string { return p.URL + "/sub/token" }

// closedAddress — адрес, на котором заведомо никого нет.
func closedAddress(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	return addr
}

// ageCache сдвигает время в кэше назад.
//
// Другого способа получить протухший кэш в тесте нет, а ждать сутки — плохой
// план. Заодно это проверка, что формат файла и правда такой, как записано.
func ageCache(t *testing.T, path string, age time.Duration) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("кэш не прочитан: %v", err)
	}

	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("кэш не разбирается: %v", err)
	}
	stored["fetched_at"] = time.Now().Add(-age).UTC().Format(time.RFC3339Nano)

	updated, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("кэш не собирается обратно: %v", err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatalf("кэш не записан: %v", err)
	}
}

func cachePathIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "subscription.json")
}

// testContext — общий срок на весь тест. Замер мёртвой ноды упирается в
// ProbeTimeout, и без запаса тест уронил бы сам себя.
func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func TestCacheRoundTrip(t *testing.T) {
	const subURL = "https://panel.example/sub/token"
	path := cachePathIn(t)

	sub := client.Subscription{
		Nodes:        []client.Node{{ID: 1, Name: "msk", Address: "msk.example:443", PublicKey: "k"}},
		TrafficLimit: 1000,
		Used:         400,
	}
	if err := client.SaveCache(path, subURL, sub); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	cached, err := client.LoadCache(path, subURL)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if !cached.Fresh() {
		t.Fatal("только что сохранённый кэш считается протухшим")
	}
	if len(cached.Nodes()) != 1 || cached.Nodes()[0].ID != 1 {
		t.Fatalf("ноды не те: %+v", cached.Nodes())
	}
	if cached.Subscription.Remaining() != 600 {
		t.Fatalf("остаток квоты %d, ожидалось 600", cached.Subscription.Remaining())
	}

	// Адрес подписки в кэш не попадает: в нём токен покупателя, и класть его
	// в лишний файл незачем — хватает отпечатка.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл кэша: %v", err)
	}
	if strings.Contains(string(raw), "sub/token") {
		t.Fatal("адрес подписки записан в кэш открытым текстом")
	}
}

// TestCacheOfAnotherSubscriptionIsIgnored: человек сменил ссылку доступа —
// ноды прошлого продавца ему не подойдут, и стучаться в них нельзя.
func TestCacheOfAnotherSubscriptionIsIgnored(t *testing.T) {
	path := cachePathIn(t)
	sub := client.Subscription{Nodes: []client.Node{{ID: 1, Address: "msk.example:443", PublicKey: "k"}}}

	if err := client.SaveCache(path, "https://panel.example/sub/первый", sub); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	_, err := client.LoadCache(path, "https://panel.example/sub/второй")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("кэш чужой подписки принят: %v", err)
	}
}

func TestLoadCacheWithoutFile(t *testing.T) {
	_, err := client.LoadCache(filepath.Join(t.TempDir(), "нет-такого"), "https://panel.example/sub/token")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("отсутствие файла отдано как %v, ожидалось os.ErrNotExist", err)
	}

	// Пустой путь означает работу без кэша — тоже «файла нет», а не ошибка,
	// которую надо показывать человеку.
	if _, err := client.LoadCache("", "https://panel.example/sub/token"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("пустой путь отдан как %v", err)
	}
}

// TestCacheGoesStale: кэш старше суток свежим не считается, но ноды из него
// отдаёт — это последняя надежда, когда заблокировали саму панель.
func TestCacheGoesStale(t *testing.T) {
	const subURL = "https://panel.example/sub/token"
	path := cachePathIn(t)
	sub := client.Subscription{Nodes: []client.Node{{ID: 1, Address: "msk.example:443", PublicKey: "k"}}}

	if err := client.SaveCache(path, subURL, sub); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	cases := map[string]time.Duration{
		"старше суток":      client.CacheTTL + time.Hour,
		"время из будущего": -48 * time.Hour,
	}

	for name, age := range cases {
		t.Run(name, func(t *testing.T) {
			ageCache(t, path, age)

			cached, err := client.LoadCache(path, subURL)
			if err != nil {
				t.Fatalf("чтение: %v", err)
			}
			if cached.Fresh() {
				t.Fatal("кэш считается свежим, хотя доверять ему нельзя")
			}
			if len(cached.Nodes()) != 1 {
				t.Fatal("ноды из протухшего кэша потеряны")
			}
		})
	}
}

// TestConnectUsesCache — главное, ради чего всё затевалось.
//
// Кэш свежий и нода в нём живая: панель трогать незачем, и её домен не должен
// появиться в запросах имён вообще.
func TestConnectUsesCache(t *testing.T) {
	node := startTestNode(t)

	// Панель нод не отдаёт: любой поход к ней здесь — уже провал.
	panel := startPanel(t)
	path := cachePathIn(t)

	live := node.info
	live.ID = 42
	if err := client.SaveCache(path, panel.subURL(), client.Subscription{Nodes: []client.Node{live}}); err != nil {
		t.Fatalf("сохранение кэша: %v", err)
	}

	dialer, measurements, err := client.Connect(testContext(t), client.ConnectConfig{
		Account:   client.Account{SubscriptionURL: panel.subURL()},
		Key:       node.clientKey,
		Dial:      node.opts,
		CachePath: path,
	})
	if err != nil {
		t.Fatalf("подключение по кэшу: %v", err)
	}
	defer dialer.Close()

	if hits := panel.hits.Load(); hits != 0 {
		t.Fatalf("сходили в панель %d раз при свежем кэше: домен подписки светится зря", hits)
	}
	if dialer.Node().ID != 42 {
		t.Fatalf("подключились не к ноде из кэша: %+v", dialer.Node())
	}

	// Отчёты обязаны собираться и из замеров по кэшу. Иначе продавец
	// перестанет узнавать про мёртвые ноды у всех, кто ходит через кэш, —
	// то есть почти у всех.
	reports := client.ReportsFrom(measurements)
	if len(reports) != 1 || reports[0].NodeID != 42 || !reports[0].OK {
		t.Fatalf("отчёт по замерам из кэша собран неверно: %+v", reports)
	}
}

// TestConnectRefreshesStaleCache: кэшу больше суток — идём в панель и
// перезаписываем его свежим списком.
func TestConnectRefreshesStaleCache(t *testing.T) {
	node := startTestNode(t)

	fresh := node.info
	fresh.ID = 2
	fresh.Name = "сегодняшняя"
	panel := startPanel(t, fresh)

	path := cachePathIn(t)
	old := node.info
	old.ID = 1
	old.Name = "вчерашняя"
	old.Address = closedAddress(t)
	if err := client.SaveCache(path, panel.subURL(), client.Subscription{Nodes: []client.Node{old}}); err != nil {
		t.Fatalf("сохранение кэша: %v", err)
	}
	ageCache(t, path, client.CacheTTL+time.Hour)

	dialer, _, err := client.Connect(testContext(t), client.ConnectConfig{
		Account:   client.Account{SubscriptionURL: panel.subURL()},
		Key:       node.clientKey,
		Dial:      node.opts,
		CachePath: path,
	})
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	defer dialer.Close()

	if hits := panel.hits.Load(); hits != 1 {
		t.Fatalf("сходили в панель %d раз, ожидался ровно один", hits)
	}
	if dialer.Node().ID != 2 {
		t.Fatalf("подключились не к свежей ноде: %+v", dialer.Node())
	}

	cached, err := client.LoadCache(path, panel.subURL())
	if err != nil {
		t.Fatalf("кэш после обновления: %v", err)
	}
	if !cached.Fresh() {
		t.Fatal("кэш остался протухшим — завтра снова пойдём в панель")
	}
	if len(cached.Nodes()) != 1 || cached.Nodes()[0].ID != 2 {
		t.Fatalf("в кэше не тот список: %+v", cached.Nodes())
	}
}

// TestConnectWithoutCacheAsksPanel: кэша нет — всё работает ровно как раньше,
// а по дороге кэш заводится.
func TestConnectWithoutCacheAsksPanel(t *testing.T) {
	node := startTestNode(t)

	live := node.info
	live.ID = 7
	panel := startPanel(t, live)
	path := cachePathIn(t)

	dialer, measurements, err := client.Connect(testContext(t), client.ConnectConfig{
		Account:   client.Account{SubscriptionURL: panel.subURL()},
		Key:       node.clientKey,
		Dial:      node.opts,
		CachePath: path,
	})
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	defer dialer.Close()

	if hits := panel.hits.Load(); hits != 1 {
		t.Fatalf("сходили в панель %d раз, ожидался ровно один", hits)
	}
	if dialer.Node().ID != 7 {
		t.Fatalf("подключились не к той ноде: %+v", dialer.Node())
	}
	if got := client.ReportsFrom(measurements); len(got) != 1 || got[0].NodeID != 7 || !got[0].OK {
		t.Fatalf("отчёт собран неверно: %+v", got)
	}

	cached, err := client.LoadCache(path, panel.subURL())
	if err != nil {
		t.Fatalf("кэш не создан: %v", err)
	}
	if !cached.Fresh() || len(cached.Nodes()) != 1 || cached.Nodes()[0].ID != 7 {
		t.Fatalf("кэш заведён неверно: %+v", cached)
	}
}

// TestConnectWithoutCachePathTouchesNoDisk: пустой путь означает прежнее
// поведение целиком — и никаких файлов.
func TestConnectWithoutCachePathTouchesNoDisk(t *testing.T) {
	node := startTestNode(t)

	live := node.info
	live.ID = 3
	panel := startPanel(t, live)
	dir := t.TempDir()

	dialer, _, err := client.Connect(testContext(t), client.ConnectConfig{
		Account: client.Account{SubscriptionURL: panel.subURL()},
		Key:     node.clientKey,
		Dial:    node.opts,
	})
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	defer dialer.Close()

	if hits := panel.hits.Load(); hits != 1 {
		t.Fatalf("сходили в панель %d раз, ожидался ровно один", hits)
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("без пути кэша на диск всё равно что-то легло: %v", left)
	}
}

// TestConnectGoesToPanelWhenCachedNodesDead: кэш свежий, но все ноды в нём
// мертвы — блокировка случилась в тот же день. Тогда в панель идти всё-таки
// приходится.
func TestConnectGoesToPanelWhenCachedNodesDead(t *testing.T) {
	node := startTestNode(t)

	live := node.info
	live.ID = 2
	panel := startPanel(t, live)

	path := cachePathIn(t)
	dead := node.info
	dead.ID = 1
	dead.Name = "заблокированная"
	dead.Address = closedAddress(t)
	if err := client.SaveCache(path, panel.subURL(), client.Subscription{Nodes: []client.Node{dead}}); err != nil {
		t.Fatalf("сохранение кэша: %v", err)
	}

	dialer, _, err := client.Connect(testContext(t), client.ConnectConfig{
		Account:   client.Account{SubscriptionURL: panel.subURL()},
		Key:       node.clientKey,
		Dial:      node.opts,
		CachePath: path,
	})
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	defer dialer.Close()

	if hits := panel.hits.Load(); hits != 1 {
		t.Fatalf("сходили в панель %d раз, ожидался ровно один", hits)
	}
	if dialer.Node().ID != 2 {
		t.Fatalf("подключились не к живой ноде: %+v", dialer.Node())
	}
}

// TestConnectUsesStaleCacheWhenPanelIsDown: заблокировали саму панель.
// Вчерашняя нода лучше, чем никакой.
func TestConnectUsesStaleCacheWhenPanelIsDown(t *testing.T) {
	node := startTestNode(t)

	panel := startPanel(t)
	subURL := panel.subURL()
	panel.Close() // адрес остался, отвечать некому

	path := cachePathIn(t)
	live := node.info
	live.ID = 5
	if err := client.SaveCache(path, subURL, client.Subscription{Nodes: []client.Node{live}}); err != nil {
		t.Fatalf("сохранение кэша: %v", err)
	}
	ageCache(t, path, client.CacheTTL+time.Hour)

	dialer, _, err := client.Connect(testContext(t), client.ConnectConfig{
		Account:   client.Account{SubscriptionURL: subURL},
		Key:       node.clientKey,
		Dial:      node.opts,
		CachePath: path,
	})
	if err != nil {
		t.Fatalf("протухший кэш не спас при недоступной панели: %v", err)
	}
	defer dialer.Close()

	if dialer.Node().ID != 5 {
		t.Fatalf("подключились не к ноде из кэша: %+v", dialer.Node())
	}
}

// TestConnectTellsPanelFailureApart: вид неудачи доезжает наружу.
//
// Приложение по нему выбирает, что сказать человеку: «проверь интернет» или
// «ноды не отвечают, пиши продавцу». Замеры при этом отдаются в обоих
// случаях — именно про «не работает ничего» продавцу важнее всего узнать.
func TestConnectTellsPanelFailureApart(t *testing.T) {
	node := startTestNode(t)

	t.Run("панель молчит", func(t *testing.T) {
		panel := startPanel(t)
		subURL := panel.subURL()
		panel.Close()

		_, _, err := client.Connect(testContext(t), client.ConnectConfig{
			Account:   client.Account{SubscriptionURL: subURL},
			Key:       node.clientKey,
			Dial:      node.opts,
			CachePath: cachePathIn(t),
		})
		if !errors.Is(err, client.ErrPanel) {
			t.Fatalf("неудача панели помечена как %v", err)
		}
	})

	t.Run("ноды молчат", func(t *testing.T) {
		dead := node.info
		dead.ID = 9
		dead.Address = closedAddress(t)
		panel := startPanel(t, dead)

		_, measurements, err := client.Connect(testContext(t), client.ConnectConfig{
			Account:   client.Account{SubscriptionURL: panel.subURL()},
			Key:       node.clientKey,
			Dial:      node.opts,
			CachePath: cachePathIn(t),
		})
		if err == nil {
			t.Fatal("подключение прошло там, где ни одна нода не отвечает")
		}
		if errors.Is(err, client.ErrPanel) {
			t.Fatalf("мёртвые ноды выданы за неудачу панели: %v", err)
		}
		if got := client.ReportsFrom(measurements); len(got) != 1 || got[0].NodeID != 9 || got[0].OK {
			t.Fatalf("отчёт про мёртвую ноду не собран: %+v", got)
		}
	})
}
