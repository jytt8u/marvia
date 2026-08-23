package transport_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/fallback"
	"github.com/veilproject/veil/internal/rewind"
	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/vp1"
)

const coverDomain = "cover.example"

// testNode — нода целиком: TLS-маскировка, VP1 внутри, настоящий сайт для
// всех остальных. Ровно та конструкция, что работает в бою.
type testNode struct {
	addr      string
	serverKey vp1.KeyPair
	cert      tls.Certificate
	closer    func()
}

func startNode(t *testing.T, coverURL string) *testNode {
	t.Helper()

	serverKey, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи сервера: %v", err)
	}
	cert, err := transport.SelfSignedCertificate(coverDomain)
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	cover, err := fallback.NewReverseProxy(coverURL)
	if err != nil {
		t.Fatalf("сайт-прикрытие: %v", err)
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	ln := transport.Listen(tcp, transport.ServerConfig{Certificate: cert})
	guard := vp1.NewReplayGuard(vp1.ClockSkew)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				rc := rewind.New(conn)
				defer rc.Close()

				tunnel, _, err := vp1.ServerHandshake(rc, serverKey, guard, vp1.AllowAll)
				if err != nil {
					if err := rc.Rewind(); err != nil {
						return
					}
					_ = rc.SetDeadline(time.Time{})
					cover.Serve(rc)
					return
				}
				rc.Commit()
				defer tunnel.Close()
				// Внутри туннеля — простое эхо, содержимое здесь не важно.
				_, _ = io.Copy(tunnel, tunnel)
			}()
		}
	}()

	node := &testNode{
		addr:      tcp.Addr().String(),
		serverKey: serverKey,
		cert:      cert,
		closer:    func() { _ = ln.Close() },
	}
	t.Cleanup(node.closer)
	return node
}

// TestTunnelThroughCamouflage: наш клиент проходит сквозь маскировку и
// получает рабочий шифрованный туннель.
func TestTunnelThroughCamouflage(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("cover site"))
	}))
	defer backend.Close()

	node := startNode(t, backend.URL)

	pool, err := transport.CertificatePool(node.cert)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	outer, err := transport.Dial(ctx, node.addr, transport.ClientConfig{
		ServerName: coverDomain,
		RootCAs:    pool,
	})
	if err != nil {
		t.Fatalf("маскированное соединение: %v", err)
	}
	defer outer.Close()

	clientKey, _ := vp1.GenerateKeyPair()
	tunnel, err := vp1.ClientHandshake(outer, clientKey, node.serverKey.Public)
	if err != nil {
		t.Fatalf("хендшейк VP1 внутри TLS: %v", err)
	}
	defer tunnel.Close()

	want := "данные внутри двух слоёв шифрования"
	if _, err := tunnel.Write([]byte(want)); err != nil {
		t.Fatalf("запись: %v", err)
	}
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(tunnel, buf); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("получено %q, ожидалось %q", buf, want)
	}
}

// TestProbeSeesCoverSite — самый важный тест этапа M1.
//
// Тот, кто пришёл обычным браузером и не знает нашего ключа, должен получить
// настоящий сайт. Не разрыв, не таймаут, не пустой ответ — сайт. Именно на
// разнице в этом поведении сканеры цензора и вычисляют прокси.
func TestProbeSeesCoverSite(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Обычный сайт, путь " + r.URL.Path + "</body></html>"))
	}))
	defer backend.Close()

	node := startNode(t, backend.URL)

	pool, err := transport.CertificatePool(node.cert)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := &net.Dialer{}
				raw, err := d.DialContext(ctx, "tcp", node.addr)
				if err != nil {
					return nil, err
				}
				conn := tls.Client(raw, &tls.Config{ServerName: coverDomain, RootCAs: pool})
				if err := conn.HandshakeContext(ctx); err != nil {
					_ = raw.Close()
					return nil, err
				}
				return conn, nil
			},
		},
		Timeout: 15 * time.Second,
	}

	resp, err := client.Get("https://" + coverDomain + "/some/page")
	if err != nil {
		t.Fatalf("сканер не получил ответа (именно так палятся прокси): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код ответа %d, ожидался 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("чтение тела: %v", err)
	}
	if !strings.Contains(string(body), "Обычный сайт") {
		t.Fatalf("отдали не сайт-прикрытие, а %q", body)
	}
	if !strings.Contains(string(body), "/some/page") {
		t.Fatalf("путь запроса до сайта-прикрытия не доехал: %q", body)
	}
}

// TestGarbageProbeSeesCoverSite: сканер шлёт мусор, не похожий ни на что.
// Реакция должна быть такой же, как у настоящего веб-сервера на мусор.
func TestGarbageProbeSeesCoverSite(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	node := startNode(t, backend.URL)

	pool, err := transport.CertificatePool(node.cert)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	raw, err := net.DialTimeout("tcp", node.addr, 10*time.Second)
	if err != nil {
		t.Fatalf("соединение: %v", err)
	}
	defer raw.Close()

	conn := tls.Client(raw, &tls.Config{ServerName: coverDomain, RootCAs: pool})
	if err := conn.Handshake(); err != nil {
		t.Fatalf("TLS-хендшейк: %v", err)
	}

	if _, err := conn.Write([]byte("\x00\x01мусор, не похожий на протокол\r\n\r\n")); err != nil {
		t.Fatalf("отправка мусора: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("на мусор не ответили вовсе (%v) — настоящий сервер ответил бы ошибкой HTTP", err)
	}
	if !strings.HasPrefix(string(buf[:n]), "HTTP/1.1 400") {
		t.Fatalf("ответ на мусор не похож на ответ веб-сервера: %q", buf[:n])
	}
}
