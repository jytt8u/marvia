package transport_test

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/vp1"
)

// realityNode поднимает ноду с маскировкой REALITY поверх настоящего сайта.
type realityNode struct {
	addr     string
	dest     string
	destCert *x509.Certificate
	pub      []byte
	accepted chan net.Conn
}

func startRealityNode(t *testing.T, body string) *realityNode {
	t.Helper()

	// «Настоящий сайт», которым прикрывается нода. В бою это чужой популярный
	// ресурс; здесь — локальный сервер со своим сертификатом на example.com.
	real := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(real.Close)

	parsed, err := url.Parse(real.URL)
	if err != nil {
		t.Fatalf("адрес сайта прикрытия: %v", err)
	}

	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи REALITY: %v", err)
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

	node := &realityNode{
		addr:     tcp.Addr().String(),
		dest:     parsed.Host,
		destCert: real.Certificate(),
		pub:      pair.Public,
		accepted: make(chan net.Conn, 4),
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// Неопознанного гостя REALITY уже отправил настоящему сайту,
				// и до нас соединение не доходит. Для ноды это штатный ход
				// событий, а не сбой.
				if strings.Contains(err.Error(), "use of closed") {
					return
				}
				continue
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

// TestRealityProbeGetsTheRealSite — главная проверка всей затеи.
//
// Сканер цензора приходит обычным TLS-клиентом, ключа не знает. Он обязан
// получить настоящий чужой сайт: его содержимое и, что важнее, его подлинный
// сертификат. Именно этим REALITY отличается от маскировки своим
// сертификатом — отличить ноду от зеркала того сайта нельзя, потому что она
// им в этот момент и является.
func TestRealityProbeGetsTheRealSite(t *testing.T) {
	const body = "<html><body>настоящий чужой сайт</body></html>"
	node := startRealityNode(t, body)

	conn, err := net.DialTimeout("tcp", node.addr, 15*time.Second)
	if err != nil {
		t.Fatalf("соединение: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         "example.com",
		InsecureSkipVerify: true, // сертификат сверяем руками, ниже
	})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("сканер не смог завершить TLS-хендшейк: %v", err)
	}

	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		t.Fatal("сертификат не предъявлен")
	}
	if !bytes.Equal(certs[0].Raw, node.destCert.Raw) {
		t.Fatal("сканеру предъявлен не сертификат сайта прикрытия — маскировка не работает")
	}

	request := "GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"
	if _, err := tlsConn.Write([]byte(request)); err != nil {
		t.Fatalf("запрос: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}
	defer resp.Body.Close()

	got, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if !strings.Contains(string(got), "настоящий чужой сайт") {
		t.Fatalf("сканер получил не содержимое сайта прикрытия: %q", got)
	}

	// И до самой ноды это соединение дойти не должно.
	select {
	case <-node.accepted:
		t.Fatal("неопознанное соединение дошло до ноды вместо сайта прикрытия")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestRealityRejectsForeignSNI: имя, которого нода не ждёт, тоже уходит на
// сайт прикрытия — реакция обязана быть одинаковой во всех случаях отказа.
func TestRealityRejectsForeignSNI(t *testing.T) {
	node := startRealityNode(t, "<html>прикрытие</html>")

	conn, err := net.DialTimeout("tcp", node.addr, 15*time.Second)
	if err != nil {
		t.Fatalf("соединение: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         "чужое-имя.example.org",
		InsecureSkipVerify: true,
	})
	// Хендшейк может и не сойтись — сайт прикрытия про это имя не знает.
	// Важно другое: соединение не должно попасть в ноду.
	_ = tlsConn.Handshake()

	select {
	case <-node.accepted:
		t.Fatal("соединение с чужим SNI дошло до ноды")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestListenRealityValidatesConfig(t *testing.T) {
	pair, _ := vp1.GenerateKeyPair()

	cases := map[string]transport.RealityConfig{
		"без сайта прикрытия":     {ServerNames: []string{"example.com"}, PrivateKey: pair.Private},
		"адрес без порта":         {Dest: "example.com", ServerNames: []string{"example.com"}, PrivateKey: pair.Private},
		"без имён для SNI":        {Dest: "example.com:443", PrivateKey: pair.Private},
		"короткий ключ":           {Dest: "example.com:443", ServerNames: []string{"example.com"}, PrivateKey: []byte{1, 2, 3}},
		"нечётный идентификатор":  {Dest: "example.com:443", ServerNames: []string{"example.com"}, PrivateKey: pair.Private, ShortIDs: []string{"abc"}},
		"не шестнадцатеричный id": {Dest: "example.com:443", ServerNames: []string{"example.com"}, PrivateKey: pair.Private, ShortIDs: []string{"zzzz"}},
		"слишком длинный id":      {Dest: "example.com:443", ServerNames: []string{"example.com"}, PrivateKey: pair.Private, ShortIDs: []string{"00112233445566778899"}},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			tcp, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("слушатель: %v", err)
			}
			defer tcp.Close()

			if _, err := transport.ListenReality(tcp, cfg); err == nil {
				t.Fatal("ошибка в настройках принята молча")
			}
		})
	}
}

func TestListenRealityAcceptsShortIDs(t *testing.T) {
	pair, _ := vp1.GenerateKeyPair()

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	defer tcp.Close()

	ln, err := transport.ListenReality(tcp, transport.RealityConfig{
		Dest:        "example.com:443",
		ServerNames: []string{"example.com"},
		PrivateKey:  pair.Private,
		ShortIDs:    []string{"", "0123456789abcdef", "aabb"},
	})
	if err != nil {
		t.Fatalf("корректные идентификаторы отвергнуты: %v", err)
	}
	_ = ln.Close()
}
