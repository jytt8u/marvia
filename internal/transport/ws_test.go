package transport_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/vp1"
)

const wsPath = "/assets/app.js"

// wsNode поднимает ноду с транспортом WebSocket поверх TLS.
type wsNode struct {
	addr     string
	pool     *tls.Config
	accepted chan net.Conn
}

func startWSNode(t *testing.T, coverBody string) *wsNode {
	t.Helper()

	cert, err := transport.SelfSignedCertificate("cdn.example")
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	pool, err := transport.CertificatePool(cert)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}

	cover := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(coverBody))
	})

	ln, err := transport.ListenWS(tcp, transport.WSConfig{
		Path:        wsPath,
		Certificate: &cert,
		Cover:       cover,
	})
	if err != nil {
		t.Fatalf("транспорт WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	node := &wsNode{
		addr:     tcp.Addr().String(),
		pool:     &tls.Config{ServerName: "cdn.example", RootCAs: pool},
		accepted: make(chan net.Conn, 4),
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			select {
			case node.accepted <- conn:
			default:
				_ = conn.Close()
			}
		}
	}()

	return node
}

func (n *wsNode) dial(t *testing.T) net.Conn {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := transport.DialWS(ctx, n.addr, transport.WSDialConfig{
		Host: "cdn.example",
		Path: wsPath,
		TLS:  transport.ClientConfig{ServerName: "cdn.example", RootCAs: n.pool.RootCAs},
	})
	if err != nil {
		t.Fatalf("подключение через WebSocket: %v", err)
	}
	return conn
}

// TestWSTunnelCarriesBytes: через WebSocket должен проходить произвольный
// поток — внутри него поедет VP1 или чужой протокол.
func TestWSTunnelCarriesBytes(t *testing.T) {
	node := startWSNode(t, "<html>обычный сайт</html>")

	client := node.dial(t)
	defer client.Close()

	var server net.Conn
	select {
	case server = <-node.accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("нода не приняла соединение")
	}
	defer server.Close()

	// Данных должно быть заметно больше одного кадра WebSocket.
	payload := strings.Repeat("поток внутри вебсокета, ", 4000)

	go func() {
		_, _ = client.Write([]byte(payload))
	}()

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(server, got); err != nil {
		t.Fatalf("чтение на ноде: %v", err)
	}
	if string(got) != payload {
		t.Fatal("данные исказились по дороге")
	}

	// И обратно.
	go func() { _, _ = server.Write([]byte("ответ ноды")) }()
	back := make([]byte, len("ответ ноды"))
	if _, err := io.ReadFull(client, back); err != nil {
		t.Fatalf("чтение на клиенте: %v", err)
	}
	if string(back) != "ответ ноды" {
		t.Fatalf("получено %q", back)
	}
}

// TestWSOtherPathsGetCover — то же требование, что и везде: посторонний
// должен получить сайт, а не признак туннеля. За CDN это особенно важно,
// потому что путь к ноде можно перебирать.
func TestWSOtherPathsGetCover(t *testing.T) {
	const body = "<html>ничего особенного</html>"
	node := startWSNode(t, body)

	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				raw, err := d.DialContext(ctx, "tcp", node.addr)
				if err != nil {
					return nil, err
				}
				conn := tls.Client(raw, node.pool)
				if err := conn.HandshakeContext(ctx); err != nil {
					_ = raw.Close()
					return nil, err
				}
				return conn, nil
			},
		},
		Timeout: 15 * time.Second,
	}

	for _, path := range []string{"/", "/ws", "/vless", "/assets/app.css"} {
		resp, err := client.Get("https://cdn.example" + path)
		if err != nil {
			t.Fatalf("путь %s: %v", path, err)
		}
		got, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK || !strings.Contains(string(got), "ничего особенного") {
			t.Fatalf("путь %s: отдали не сайт-прикрытие (код %d, тело %q)", path, resp.StatusCode, got)
		}
	}

	select {
	case <-node.accepted:
		t.Fatal("посторонний запрос дошёл до ноды как туннель")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestWSCarriesVP1: внутри WebSocket должен работать наш протокол целиком.
// Это и есть ответ на то, что CDN расшифровывает внешний TLS: ему достаётся
// поток, который он прочитать не может.
func TestWSCarriesVP1(t *testing.T) {
	node := startWSNode(t, "<html>сайт</html>")

	serverKey, _ := vp1.GenerateKeyPair()
	clientKey, _ := vp1.GenerateKeyPair()
	guard := vp1.NewReplayGuard(vp1.ClockSkew)

	outer := node.dial(t)
	defer outer.Close()

	var carrier net.Conn
	select {
	case carrier = <-node.accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("нода не приняла соединение")
	}
	defer carrier.Close()

	done := make(chan *vp1.Conn, 1)
	go func() {
		tunnel, _, err := vp1.ServerHandshake(carrier, serverKey, guard, vp1.AllowAll)
		if err != nil {
			done <- nil
			return
		}
		done <- tunnel
	}()

	client, err := vp1.ClientHandshake(outer, clientKey, serverKey.Public)
	if err != nil {
		t.Fatalf("хендшейк VP1 внутри WebSocket: %v", err)
	}
	server := <-done
	if server == nil {
		t.Fatal("нода не завершила хендшейк VP1")
	}

	want := "данные внутри трёх слоёв: TLS, WebSocket, VP1"
	go func() { _, _ = client.Write([]byte(want)) }()

	buf := make([]byte, len(want))
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("получено %q", buf)
	}
}

// TestWSRejectsPlainRequestOnTunnelPath: обычный запрос по пути туннеля, без
// переговоров о WebSocket, не должен ронять ноду и не должен выглядеть особо.
func TestWSRejectsPlainRequestOnTunnelPath(t *testing.T) {
	node := startWSNode(t, "<html>сайт</html>")

	raw, err := net.DialTimeout("tcp", node.addr, 10*time.Second)
	if err != nil {
		t.Fatalf("соединение: %v", err)
	}
	defer raw.Close()

	conn := tls.Client(raw, node.pool)
	if err := conn.Handshake(); err != nil {
		t.Fatalf("TLS: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	request := "GET " + wsPath + " HTTP/1.1\r\nHost: cdn.example\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatalf("запрос: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("на обычный запрос не ответили: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatal("нода подняла WebSocket без переговоров")
	}
	if resp.StatusCode == http.StatusUpgradeRequired {
		t.Fatal("путь туннеля отвечает 426 — по этому коду ноду находят перебором путей")
	}

	// Ответ должен быть неотличим от ответа на любой другой путь.
	got, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if !strings.Contains(string(got), "сайт") {
		t.Fatalf("путь туннеля выделяется среди прочих: код %d, тело %q", resp.StatusCode, got)
	}

	select {
	case <-node.accepted:
		t.Fatal("обычный запрос дошёл до ноды как туннель")
	case <-time.After(300 * time.Millisecond):
	}
}
