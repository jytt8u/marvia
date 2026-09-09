package transport_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/transport"
	"github.com/jytt8u/marvia/internal/vp1"
	utls "github.com/refraction-networking/utls"
)

// TestRealityClientAcrossFingerprints — проверка на всех отпечатках сразу.
//
// Тест поймал настоящую ошибку, и она стоит объяснения. Свежие отпечатки
// Chrome шлют два обмена ключами: гибридный постквантовый и обычный X25519.
// Нода сначала ищет обычный и только при его отсутствии берёт половину
// гибрида. Клиент брал наоборот — и на Chrome 131 и новее общий секрет
// расходился.
//
// Проявлялось это худшим из возможных способов: нода не узнавала клиента и
// молча отправляла его на сайт прикрытия. Со стороны — «интернет не
// работает», без единой внятной ошибки. Отпечаток при этом выбирает продавец
// в конфиге, так что поймать такое в бою почти невозможно.
func TestRealityClientAcrossFingerprints(t *testing.T) {
	fingerprints := map[string]utls.ClientHelloID{
		"Chrome последний":  utls.HelloChrome_Auto,
		"Chrome 133":        utls.HelloChrome_133,
		"Chrome 131":        utls.HelloChrome_131,
		"Chrome 120 с ПКК":  utls.HelloChrome_120_PQ,
		"Chrome 120":        utls.HelloChrome_120,
		"Chrome 115 с ПКК":  utls.HelloChrome_115_PQ,
		"Chrome 106":        utls.HelloChrome_106_Shuffle,
		"Firefox последний": utls.HelloFirefox_Auto,
	}

	for name, fingerprint := range fingerprints {
		t.Run(name, func(t *testing.T) {
			node, pub := startEchoRealityNode(t)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			conn, err := transport.DialReality(ctx, node, transport.RealityDialConfig{
				ServerName:  "example.com",
				PublicKey:   pub,
				Fingerprint: fingerprint,
			})
			if err != nil {
				t.Fatalf("нода не узнала клиента: %v", err)
			}
			defer conn.Close()

			_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
			want := "проверка отпечатка"
			if _, err := conn.Write([]byte(want)); err != nil {
				t.Fatalf("запись: %v", err)
			}
			got := make([]byte, len(want))
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Fatalf("чтение: %v", err)
			}
			if string(got) != want {
				t.Fatalf("получено %q", got)
			}
		})
	}
}

// startEchoRealityNode поднимает ноду, отвечающую эхом, и возвращает её адрес
// и публичный ключ.
func startEchoRealityNode(t *testing.T) (string, []byte) {
	t.Helper()

	real := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("сайт прикрытия"))
	}))
	t.Cleanup(real.Close)

	parsed, err := url.Parse(real.URL)
	if err != nil {
		t.Fatalf("адрес сайта прикрытия: %v", err)
	}

	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи: %v", err)
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}

	ln, err := transport.ListenReality(tcp, transport.RealityConfig{
		Dest:        parsed.Host,
		ServerNames: []string{"example.com"},
		PrivateKey:  pair.Private,
	})
	if err != nil {
		t.Fatalf("маскировка REALITY: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	return tcp.Addr().String(), pair.Public
}
