package foreign

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/jytt8u/marvia/internal/client"
)

// vlessNode поднимает VLESS-сервер и отдаёт ссылку на него.
func vlessNode(t *testing.T, name string) (string, *core.Instance) {
	t.Helper()
	const uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	port := freePort(t)
	inst := server(t, map[string]any{"port": port, "listen": "127.0.0.1", "protocol": "vless",
		"settings": map[string]any{"clients": []any{map[string]any{"id": uuid}}, "decryption": "none"}})
	return "vless://" + uuid + "@127.0.0.1:" + strconv.Itoa(port) + "?security=none#" + name, inst
}

// probeHere подменяет адрес замера местным сервером: интернета в тесте нет.
func probeHere(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(srv.Close)
	old := probeURL
	probeURL = srv.URL
	t.Cleanup(func() { probeURL = old })
}

// TestDeadNodeIsSkippedAndChosenOneIsKept: из подписки выбирается живая нода,
// а выбранная руками — если жива — побеждает быструю.
func TestDeadNodeIsSkippedAndChosenOneIsKept(t *testing.T) {
	probeHere(t)
	alive, _ := vlessNode(t, "Жива")
	second, _ := vlessNode(t, "Вторая")
	dead := "vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:" + strconv.Itoa(freePort(t)) + "?security=none#Мертва"
	sub := ParseList([]byte(dead + "\n" + alive + "\n" + second))

	s, results, err := Supervise(context.Background(), sub, 0, client.Events{}, Options{})
	if err != nil {
		t.Fatalf("надзор: %v", err)
	}
	defer s.Close()
	if s.Node().Name == "Мертва" {
		t.Fatal("выбрана мёртвая нода")
	}
	failed := 0
	for _, m := range results {
		if !m.OK() {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("неудачных замеров %d, ожидался один", failed)
	}

	secondID := NodeID(sub.Links[2])
	s2, _, err := Supervise(context.Background(), sub, secondID, client.Events{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.Node().ID != secondID {
		t.Fatalf("выбранная руками живая нода не взята: %s", s2.Node().Name)
	}
}

// TestSilentNodeIsLeftForALiveOne: текущая нода замолчала — надзор сам
// переезжает на живую и говорит об этом, человеку ничего нажимать не надо.
func TestSilentNodeIsLeftForALiveOne(t *testing.T) {
	probeHere(t)
	oldEvery, oldDead := watchEvery, deadAfter
	watchEvery, deadAfter = 100*time.Millisecond, 2
	t.Cleanup(func() { watchEvery, deadAfter = oldEvery, oldDead })

	first, firstInst := vlessNode(t, "Первая")
	second, _ := vlessNode(t, "Вторая")
	sub := ParseList([]byte(first + "\n" + second))

	moved := make(chan client.Node, 1)
	s, _, err := Supervise(context.Background(), sub, NodeID(sub.Links[0]), client.Events{
		OnSwitch: func(n client.Node) { moved <- n },
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_ = firstInst.Close()
	select {
	case n := <-moved:
		if n.Name != "Вторая" {
			t.Fatalf("переехали на %s", n.Name)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("за десять секунд надзор так и не переехал с молчащей ноды")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Ping(ctx); err != nil {
		t.Fatalf("после переезда нода не отвечает: %v", err)
	}
}

// TestNothingSurvivesTheDisconnect: движок, поднятый переездом уже после
// отключения, не ставится, а закрывается — иначе он жил бы дальше, держа
// сокеты к ноде, и закрыть его было бы некому.
func TestNothingSurvivesTheDisconnect(t *testing.T) {
	s := newSupervisor(Subscription{}, 0, client.Events{
		OnSwitch: func(client.Node) { t.Error("переезд объявлен после отключения") },
	}, Options{})
	_ = s.Close()
	l, err := Parse("trojan://p@127.0.0.1:4?sni=x.test")
	if err != nil {
		t.Fatal(err)
	}
	e, err := Start(l)
	if err != nil {
		t.Fatal(err)
	}
	if s.swap(e) {
		t.Fatal("движок поставлен после отключения")
	}
	if _, err := s.now(); err == nil {
		t.Fatal("после отключения у надзора есть движок")
	}
}
