package tunbridge_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/relay"
	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

// pairDialer отдаёт половину настоящей пары соединений: вторая остаётся у
// теста, который играет за дальний конец.
type pairDialer struct{ mine, theirs net.Conn }

func newPairDialer() *pairDialer {
	mine, theirs := net.Pipe()
	return &pairDialer{mine: mine, theirs: theirs}
}

func (d *pairDialer) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	return d.mine, nil
}

func (d *pairDialer) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	return d.mine, nil
}

// TestBytesThroughTheTunnelLandInTheCounters: и окно, и телефон показывают
// расход по этим двум числам. Ошибка в них не видна ничем, кроме них самих.
func TestBytesThroughTheTunnelLandInTheCounters(t *testing.T) {
	var up, down atomic.Int64

	d := newPairDialer()
	defer d.theirs.Close()

	conn, err := tunbridge.Metered(d, &up, &down).DialTarget(context.Background(), vp1.Address{})
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer conn.Close()

	// net.Pipe не буферизует: читатель на другом конце обязан быть раньше.
	sent := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("двенадцать!"))
		sent <- err
	}()
	buf := make([]byte, 64)
	n, err := d.theirs.Read(buf)
	if err != nil {
		t.Fatalf("дальний конец не прочитал: %v", err)
	}
	if err := <-sent; err != nil {
		t.Fatalf("отправка: %v", err)
	}

	got := make(chan int, 1)
	go func() {
		n, _ := conn.Read(buf)
		got <- n
	}()
	if _, err := d.theirs.Write([]byte("ответ")); err != nil {
		t.Fatalf("дальний конец не записал: %v", err)
	}
	back := <-got

	if up.Load() != int64(n) {
		t.Fatalf("ушло %d байт, счётчик показывает %d", n, up.Load())
	}
	if down.Load() != int64(back) {
		t.Fatalf("пришло %d байт, счётчик показывает %d", back, down.Load())
	}
}

// TestAFailedDialIsNotCounted: неудачный дозвон не должен ни падать внутри
// обёртки, ни прибавлять байтов — иначе расход растёт от того, что нода
// молчит.
func TestAFailedDialIsNotCounted(t *testing.T) {
	var up, down atomic.Int64

	_, err := tunbridge.Metered(refusing{}, &up, &down).DialTarget(context.Background(), vp1.Address{})
	if err == nil {
		t.Fatal("ожидался отказ дозвона")
	}
	if up.Load() != 0 || down.Load() != 0 {
		t.Fatalf("счётчики тронулись на неудаче: up=%d down=%d", up.Load(), down.Load())
	}
}

type refusing struct{}

func (refusing) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("некуда")
}

func (refusing) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("некуда")
}

// TestTheMeterKeepsTheResponseAfterTheRequestEnds: HTTP и многие другие
// протоколы ждут конца запроса, прежде чем отвечать. Обёртка, спрятавшая
// CloseWrite, превращает этот конец в полное закрытие — и ответ теряется.
// Найдено на живом телефоне как «страница загрузилась наполовину».
func TestTheMeterKeepsTheResponseAfterTheRequestEnds(t *testing.T) {
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
		_ = client.SetDeadline(time.Now().Add(3 * time.Second))
		_ = server.SetDeadline(time.Now().Add(3 * time.Second))
		return client, server
	}
	app, downstream := pair()
	upstream, target := pair()

	var up, down atomic.Int64
	metered, err := tunbridge.Metered(fixed{upstream}, &up, &down).DialTarget(context.Background(), vp1.Address{})
	if err != nil {
		t.Fatal(err)
	}

	relayDone := make(chan error, 1)
	go func() { relayDone <- relay.Bidirectional(downstream, metered) }()

	targetDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(target)
		if err == nil {
			_, err = target.Write([]byte("ответ после конца запроса"))
		}
		_ = target.CloseWrite()
		targetDone <- err
	}()

	if _, err := app.Write([]byte("запрос")); err != nil {
		t.Fatal(err)
	}
	if err := app.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(app)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ответ после конца запроса" {
		t.Errorf("ответ потерян: %q", got)
	}
	if err := <-targetDone; err != nil {
		t.Error(err)
	}
	if err := <-relayDone; err != nil {
		t.Error(err)
	}
	if down.Load() != int64(len(got)) || up.Load() != int64(len("запрос")) {
		t.Errorf("счётчики: up=%d down=%d", up.Load(), down.Load())
	}
}

// fixed отдаёт одно заранее открытое соединение вместо дозвона.
type fixed struct{ conn net.Conn }

func (f fixed) DialTarget(context.Context, vp1.Address) (net.Conn, error)    { return f.conn, nil }
func (f fixed) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) { return f.conn, nil }
