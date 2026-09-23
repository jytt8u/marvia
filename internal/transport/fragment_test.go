package transport_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/jytt8u/marvia/internal/transport"
	"github.com/jytt8u/marvia/internal/vp1"
)

// pieces записывает, какими кусками уходило соединение.
type pieces struct {
	net.Conn
	got [][]byte
}

func (p *pieces) Write(b []byte) (int, error) {
	p.got = append(p.got, append([]byte(nil), b...))
	return len(b), nil
}

// chromeHello собирает TLS-запись с приветствием Chrome — таким, какое уходит
// в сеть: с перемешанными расширениями и постквантовым ключом.
func chromeHello(t *testing.T, name string) []byte {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	u := utls.UClient(a, &utls.Config{ServerName: name}, utls.HelloChrome_Auto)
	if err := u.BuildHandshakeState(); err != nil {
		t.Fatalf("сборка приветствия: %v", err)
	}
	msg := u.HandshakeState.Hello.Raw
	return append([]byte{0x16, 0x03, 0x01, byte(len(msg) >> 8), byte(len(msg))}, msg...)
}

// TestNoPieceOfAFragmentedHelloCarriesTheWholeName — ради этого дробление и
// есть: фильтр, читающий один пакет, не должен найти имя ни в одном. Chrome
// кладёт имя каждый раз в новое место, поэтому приветствий много.
func TestNoPieceOfAFragmentedHelloCarriesTheWholeName(t *testing.T) {
	const name = "blocked.example.com"
	for range 200 {
		hello := chromeHello(t, name)
		rec := &pieces{}
		n, err := transport.FragmentHello(rec).Write(hello)
		if err != nil || n != len(hello) {
			t.Fatalf("запись: %d из %d, %v", n, len(hello), err)
		}
		if len(rec.got) < 2 {
			t.Fatalf("приветствие ушло одним куском")
		}
		for _, piece := range rec.got {
			if bytes.Contains(piece, []byte(name)) {
				t.Fatalf("имя целиком в одном куске из %d", len(rec.got))
			}
		}
		if !bytes.Equal(bytes.Join(rec.got, nil), hello) {
			t.Fatal("куски не складываются в исходное приветствие")
		}
	}
}

// TestOnlyTheFirstHelloIsFragmented: дальше соединение идёт как обычно —
// резать каждую запись значило бы платить паузами за весь трафик.
func TestOnlyTheFirstHelloIsFragmented(t *testing.T) {
	rec := &pieces{}
	c := transport.FragmentHello(rec)
	_, _ = c.Write(chromeHello(t, "example.com"))
	before := len(rec.got)
	_, _ = c.Write(chromeHello(t, "example.com"))
	if len(rec.got) != before+1 {
		t.Fatalf("вторая запись ушла %d кусками", len(rec.got)-before)
	}

	plain := &pieces{}
	_, _ = transport.FragmentHello(plain).Write([]byte("GET / HTTP/1.1\r\n\r\n"))
	if len(plain.got) != 1 {
		t.Fatalf("не-TLS ушло %d кусками", len(plain.got))
	}
}

// TestFragmentedHelloReachesATLSServer: сервер получает то же приветствие,
// только по частям, и договаривается как обычно.
func TestFragmentedHelloReachesATLSServer(t *testing.T) {
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("сайт"))
	}))
	defer site.Close()
	pool := x509.NewCertPool()
	pool.AddCert(site.Certificate())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := transport.Dial(ctx, strings.TrimPrefix(site.URL, "https://"), transport.ClientConfig{
		ServerName: "example.com",
		RootCAs:    pool,
		Fragment:   true,
	})
	if err != nil {
		t.Fatalf("TLS с дроблением: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}
	defer resp.Body.Close()
	if body, _ := io.ReadAll(resp.Body); string(body) != "сайт" {
		t.Fatalf("тело %q", body)
	}
}

// TestFragmentedHelloIsRecognisedByREALITY — главное опасение: нода REALITY
// решает, свой ли клиент, по приветствию, и разрезанное не должна принять за
// чужое и отправить на сайт прикрытия.
func TestFragmentedHelloIsRecognisedByREALITY(t *testing.T) {
	real := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("сайт прикрытия"))
	}))
	defer real.Close()
	parsed, err := url.Parse(real.URL)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.ListenReality(tcp, transport.RealityConfig{
		Dest:        parsed.Host,
		ServerNames: []string{"example.com"},
		PrivateKey:  pair.Private,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if strings.Contains(err.Error(), "use of closed") {
					return
				}
				continue
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	for range 5 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		conn, err := transport.DialReality(ctx, tcp.Addr().String(), transport.RealityDialConfig{
			ServerName: "example.com",
			PublicKey:  pair.Public,
			Fragment:   true,
		})
		cancel()
		if err != nil {
			t.Fatalf("REALITY с дроблением: %v", err)
		}
		want := "внутри"
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.WriteString(conn, want)
		got := make([]byte, len(want))
		if _, err := io.ReadFull(conn, got); err != nil || string(got) != want {
			t.Fatalf("эхо %q, %v", got, err)
		}
		_ = conn.Close()
	}
}
