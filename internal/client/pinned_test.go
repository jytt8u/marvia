package client_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/client"
)

const testKey = "YH3odubQhSmWQfCRwteeGN6pehcHq3DDcX_uGCvfl_Q"

// TestAccountLinkCarriesPanelAddresses проверяет разбор подсказки адресов.
func TestAccountLinkCarriesPanelAddresses(t *testing.T) {
	account, err := client.ParseAccountLink(
		"marvia://" + testKey + "@panel.example.test/sub/token?ip=203.0.113.7,%20198.51.100.2")
	if err != nil {
		t.Fatalf("ссылка не разобралась: %v", err)
	}

	if len(account.PanelIPs) != 2 {
		t.Fatalf("адресов %d, ожидалось 2", len(account.PanelIPs))
	}
	if got := account.PanelIPs[0].String(); got != "203.0.113.7" {
		t.Errorf("первый адрес %q", got)
	}
	if got := account.PanelIPs[1].String(); got != "198.51.100.2" {
		t.Errorf("второй адрес %q", got)
	}

	// Подсказка не должна протечь в адрес подписки: по нему ходит токен, и
	// лишний параметр в запросе — это лишняя примета.
	if strings.Contains(account.SubscriptionURL, "ip=") {
		t.Errorf("подсказка попала в адрес подписки: %s", account.SubscriptionURL)
	}
}

// TestAccountLinkRejectsHostnameAsAddress — в подсказке должен быть адрес.
//
// Имя вместо адреса выглядит как рабочая ссылка, но смысла лишено: чтобы им
// воспользоваться, пришлось бы спросить резолвер — ровно то, чего подсказка и
// должна избежать. Молча игнорировать такое нельзя, иначе продавец будет
// уверен, что защитил покупателей, а он не защитил.
func TestAccountLinkRejectsHostnameAsAddress(t *testing.T) {
	_, err := client.ParseAccountLink(
		"marvia://" + testKey + "@panel.example.test/sub/token?ip=panel.example.test")
	if err == nil {
		t.Fatal("имя в подсказке принято, а должно быть отвергнуто")
	}
	if !strings.Contains(err.Error(), "нужен IP") {
		t.Errorf("невнятная ошибка: %v", err)
	}
}

// TestFetchSubscriptionUsesPinnedAddress — главная проверка.
//
// Имя в адресе заведомо несуществующее: .invalid не резолвится нигде и никогда,
// это записано в стандарте. Значит запрос может дойти только одним способом —
// если мы вообще не спрашивали имя, а взяли адрес из подсказки.
func TestFetchSubscriptionUsesPinnedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "panel.example.invalid") {
			t.Errorf("до панели дошло чужое имя: %q", r.Host)
		}
		_, _ = w.Write([]byte(`{"nodes":[{"id":1,"name":"ae-1","address":"203.0.113.9:443","public_key":"` + testKey + `"}]}`))
	}))
	defer srv.Close()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("адрес тестовой панели: %v", err)
	}

	sub, err := client.FetchSubscription(
		context.Background(),
		"http://panel.example.invalid:"+port+"/sub/token",
		pinned(t, "127.0.0.1"),
	)
	if err != nil {
		t.Fatalf("подписка не забралась: %v", err)
	}
	if len(sub.Nodes) != 1 {
		t.Fatalf("нод %d, ожидалась одна", len(sub.Nodes))
	}
}

// TestFetchSubscriptionTriesEveryAddress — первый адрес мёртв, второй жив.
//
// У панели за CDN адресов всегда несколько, и какой-то из них регулярно не
// отвечает. Останавливаться на первом означало бы ронять подключение на ровном
// месте.
func TestFetchSubscriptionTriesEveryAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"nodes":[{"id":1,"name":"ae-1","address":"203.0.113.9:443","public_key":"` + testKey + `"}]}`))
	}))
	defer srv.Close()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("адрес тестовой панели: %v", err)
	}

	// 192.0.2.0/24 отведён под примеры и в живой сети не маршрутизируется.
	sub, err := client.FetchSubscription(
		context.Background(),
		"http://panel.example.invalid:"+port+"/sub/token",
		pinned(t, "192.0.2.1", "127.0.0.1"),
	)
	if err != nil {
		t.Fatalf("до второго адреса не дошли: %v", err)
	}
	if len(sub.Nodes) != 1 {
		t.Fatalf("нод %d, ожидалась одна", len(sub.Nodes))
	}
}

// pinned собирает список адресов для подсказки.
func pinned(t *testing.T, addrs ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, 0, len(addrs))
	for _, raw := range addrs {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			t.Fatalf("адрес %q: %v", raw, err)
		}
		out = append(out, addr)
	}
	return out
}
