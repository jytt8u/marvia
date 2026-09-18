package mobile

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/relay"
)

func TestTrafficCountsDeliveredBytes(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	d := &trafficDialer{}
	c := &trafficConn{Conn: a, owner: d}
	done := make(chan error, 1)
	go func() { _, e := b.Write([]byte("hello")); done <- e }()
	buf := make([]byte, 5)
	if _, e := io.ReadFull(c, buf); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	go func() { _, e := io.ReadFull(b, buf); done <- e }()
	if _, e := c.Write([]byte("world")); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	b.Close()
	c.Read(buf)
	if d.rx.Load() != 5 || d.tx.Load() != 5 {
		t.Fatalf("rx=%d tx=%d", d.rx.Load(), d.tx.Load())
	}
}

// HTTP и другие протоколы могут ждать конца запроса перед отправкой ответа.
func TestTrafficMeterPreservesResponseAfterRequestEOF(t *testing.T) {
	pair := func() (*net.TCPConn, *net.TCPConn) {
		t.Helper()
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		server, err := listener.AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { server.Close() })
		client.SetDeadline(time.Now().Add(3 * time.Second))
		server.SetDeadline(time.Now().Add(3 * time.Second))
		return client, server
	}
	app, downstream := pair()
	upstream, target := pair()
	meter := &trafficDialer{}
	relayDone := make(chan error, 1)
	go func() { relayDone <- relay.Bidirectional(downstream, &trafficConn{Conn: upstream, owner: meter}) }()
	targetDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(target)
		if err == nil {
			_, err = target.Write([]byte("response after EOF"))
		}
		target.CloseWrite()
		targetDone <- err
	}()
	if _, err := app.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := app.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(app)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "response after EOF" {
		t.Errorf("ответ потерян: %q", got)
	}
	if err := <-targetDone; err != nil {
		t.Error(err)
	}
	if err := <-relayDone; err != nil {
		t.Error(err)
	}
	if meter.rx.Load() != int64(len(got)) || meter.tx.Load() != 7 {
		t.Errorf("счётчики: rx=%d tx=%d", meter.rx.Load(), meter.tx.Load())
	}
}
