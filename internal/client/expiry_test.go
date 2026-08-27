package client_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/client"
)

// Кончившаяся подписка обязана называться своим именем.
//
// Это самая частая беда из всех и единственная, где человеку надо не чинить, а
// заплатить. Нода про срок знает, но сказать не может: она отказывает молча,
// иначе по её ответам перебирали бы чужие ключи. Раньше человек читал «ни один
// сервер не отвечает», решал, что сломались мы, и шёл к продавцу — а продавец
// разбирался с каждым таким вручную.

// expiredPanel — панель, отдающая подписку с заданными сроком и квотой.
func expiredPanel(t *testing.T, sub client.Subscription) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(sub)
	}))
	t.Cleanup(srv.Close)

	return srv.URL + "/sub/token"
}

func TestExpiredSubscriptionIsNamed(t *testing.T) {
	node := startTestNode(t)

	cases := []struct {
		name string
		sub  client.Subscription
		want error
	}{
		{
			name: "срок вышел вчера",
			sub: client.Subscription{
				Nodes:     []client.Node{node.info},
				ExpiresAt: time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			},
			want: client.ErrExpired,
		},
		{
			name: "трафик выбран весь",
			sub: client.Subscription{
				Nodes:        []client.Node{node.info},
				TrafficLimit: 100,
				Used:         100,
			},
			want: client.ErrQuota,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := client.Connect(testContext(t), client.ConnectConfig{
				Account: client.Account{SubscriptionURL: expiredPanel(t, tc.sub)},
				Key:     node.clientKey,
				Dial:    node.opts,
			})
			if err == nil {
				t.Fatal("подключение прошло с кончившейся подпиской")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("вид неудачи не тот: %v", err)
			}
			// «Ни один сервер не отвечает» здесь было бы враньём: ноды живы.
			if errors.Is(err, client.ErrPanel) {
				t.Errorf("кончившаяся подписка выдана за недоступную панель: %v", err)
			}
		})
	}
}

// TestLiveSubscriptionConnects — живая подписка ничем не задета.
func TestLiveSubscriptionConnects(t *testing.T) {
	node := startTestNode(t)

	sub := client.Subscription{
		Nodes:        []client.Node{node.info},
		ExpiresAt:    time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		TrafficLimit: 1000,
		Used:         400,
	}

	dialer, _, err := client.Connect(testContext(t), client.ConnectConfig{
		Account: client.Account{SubscriptionURL: expiredPanel(t, sub)},
		Key:     node.clientKey,
		Dial:    node.opts,
	})
	if err != nil {
		t.Fatalf("живая подписка не подключилась: %v", err)
	}
	defer dialer.Close()

	// И заодно доезжает до окна: приложение обязано уметь ответить «сколько
	// осталось» само, иначе на этот вопрос отвечает продавец каждому вручную.
	if left := dialer.Subscription().Remaining(); left != 600 {
		t.Errorf("остаток трафика не доехал до приложения: %d вместо 600", left)
	}
	if _, set := dialer.Subscription().Until(); !set {
		t.Error("срок подписки не доехал до приложения")
	}
}
