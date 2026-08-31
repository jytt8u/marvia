package transport_test

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/vp1"
)

// QUIC проверяем живым разговором, а не разбором структур.
//
// Транспорт — это место, где ошибаются не в логике, а в стыках: кто закрывает
// поток, чьи адреса отдаются наверх, что происходит с полузакрытием. Такое
// ловится только настоящим соединением.

func quicPair(t *testing.T) (*transport.QUICListener, *x509.CertPool) {
	t.Helper()

	cert, err := transport.SelfSignedCertificate("localhost")
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	pool, err := transport.CertificatePool(cert)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	ln, err := transport.ListenQUIC("127.0.0.1:0", cert)
	if err != nil {
		t.Fatalf("слушатель quic: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	return ln, pool
}

// TestQUICRoundTrip — байты доезжают в обе стороны.
func TestQUICRoundTrip(t *testing.T) {
	ln, pool := quicPair(t)

	const hello = "привет"

	served := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			served <- err
			return
		}
		defer conn.Close()

		buf := make([]byte, len(hello))
		if _, err := io.ReadFull(conn, buf); err != nil {
			served <- err
			return
		}
		_, err = conn.Write([]byte("эхо: " + string(buf)))
		served <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := transport.DialQUIC(ctx, ln.Addr().String(), transport.QUICDialConfig{
		TLS: transport.ClientConfig{ServerName: "localhost", RootCAs: pool},
	})
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(hello)); err != nil {
		t.Fatalf("запись: %v", err)
	}

	want := "эхо: " + hello
	answer := make([]byte, len(want))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, answer); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if string(answer) != want {
		t.Fatalf("ответ не тот: %q", answer)
	}

	if err := <-served; err != nil {
		t.Fatalf("сторона ноды: %v", err)
	}
}

// TestQUICCarriesVP1 — под транспортом работает наш протокол целиком.
//
// Ради этого всё и делается: QUIC меняет только то, как соединение выглядит на
// проводе, а шифрование, подлинность и всё остальное остаются прежними.
func TestQUICCarriesVP1(t *testing.T) {
	ln, pool := quicPair(t)

	const secret = "тайна"

	server, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи ноды: %v", err)
	}
	client, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи клиента: %v", err)
	}

	greeted := make(chan error, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			greeted <- err
			return
		}
		conn, _, err := vp1.ServerHandshake(raw, server, vp1.NewReplayGuard(2*time.Minute), vp1.AllowAll)
		if err != nil {
			greeted <- err
			return
		}
		defer conn.Close()

		buf := make([]byte, len(secret))
		if _, err := io.ReadFull(conn, buf); err != nil {
			greeted <- err
			return
		}
		_, err = conn.Write(buf)
		greeted <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	raw, err := transport.DialQUIC(ctx, ln.Addr().String(), transport.QUICDialConfig{
		TLS: transport.ClientConfig{ServerName: "localhost", RootCAs: pool},
	})
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer raw.Close()

	conn, err := vp1.ClientHandshake(raw, client, server.Public)
	if err != nil {
		t.Fatalf("рукопожатие vp1 поверх quic: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(secret)); err != nil {
		t.Fatalf("запись: %v", err)
	}

	back := make([]byte, len(secret))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, back); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if string(back) != secret {
		t.Fatalf("вернулось не то: %q", back)
	}

	if err := <-greeted; err != nil {
		t.Fatalf("сторона ноды: %v", err)
	}
}

// TestQUICRejectsWrongCertificate — подставная нода не проходит.
//
// Проверка сертификата на QUIC настраивается отдельно от TLS-транспорта, и
// забыть её здесь — значит пустить любого, кто перехватил UDP.
func TestQUICRejectsWrongCertificate(t *testing.T) {
	ln, _ := quicPair(t)

	// Своё хранилище доверия, в котором сертификата ноды нет.
	stranger, err := transport.SelfSignedCertificate("localhost")
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	pool, err := transport.CertificatePool(stranger)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := transport.DialQUIC(ctx, ln.Addr().String(), transport.QUICDialConfig{
		TLS: transport.ClientConfig{ServerName: "localhost", RootCAs: pool},
	})
	if err == nil {
		_ = conn.Close()
		t.Fatal("клиент принял чужой сертификат")
	}

	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) && !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("отказ пришёл с другой ошибкой, но отказ: %v", err)
	}
}

// TestQUICListenerCloses — закрытый слушатель отпускает Accept.
//
// Иначе остановка ноды подвешивала бы горутину приёма навсегда.
func TestQUICListenerCloses(t *testing.T) {
	ln, _ := quicPair(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := ln.Accept(); err == nil {
			t.Error("Accept вернул соединение после закрытия")
		}
	}()

	_ = ln.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Accept не вернулся после закрытия слушателя")
	}
}
