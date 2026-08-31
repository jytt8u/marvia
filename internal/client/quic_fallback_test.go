package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/vp1"
)

// Откат с QUIC на TCP — то, ради чего вся развилка и написана.
//
// UDP режут не по пакету, а целой сетью: у оператора, в офисе, в белом списке.
// Клиент, который этого не переживёт, окажется бесполезен ровно там, где он
// нужнее всего.

// TestQUICFallsBackToTCP — нода объявила QUIC, но UDP не проходит.
//
// Так выглядит любая сеть с вырезанным UDP: TCP-порт отвечает, дозвон по UDP
// молчит. Туннель обязан встать по TCP, и человек не должен ничего заметить.
func TestQUICFallsBackToTCP(t *testing.T) {
	static, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи ноды: %v", err)
	}

	cert, err := transport.SelfSignedCertificate("localhost")
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	ln := transport.Listen(raw, transport.ServerConfig{Certificate: cert})
	t.Cleanup(func() { _ = ln.Close() })

	// Нода отвечает только по TCP: слушателя QUIC здесь нет намеренно.
	greeted := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tunnel, _, err := vp1.ServerHandshake(conn, static, vp1.NewReplayGuard(vp1.ClockSkew), vp1.AllowAll)
				if err != nil {
					_ = conn.Close()
					return
				}
				select {
				case greeted <- struct{}{}:
				default:
				}
				_ = tunnel.Close()
			}()
		}
	}()

	node := client.Node{
		Name:      "проверочная",
		Address:   raw.Addr().String(),
		SNI:       "localhost",
		PublicKey: vp1.EncodeKey(static.Public),
		// Панель сказала, что нода умеет QUIC. Сеть — что не умеет.
		QUIC: true,
	}

	key, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи клиента: %v", err)
	}

	dialer, err := client.NewDialer(node, key, client.Options{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	start := time.Now()
	_, _ = dialer.DialTarget(ctx, vp1.Address{Host: "example.org", Port: 443})

	select {
	case <-greeted:
	case <-time.After(20 * time.Second):
		t.Fatal("нода так и не дождалась клиента: откат на TCP не сработал")
	}

	// Ждать дольше нескольких секунд нельзя: столько человек смотрит на
	// «подключаюсь», прежде чем решить, что приложение сломалось.
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("откат занял слишком долго: %s", took)
	}
}
