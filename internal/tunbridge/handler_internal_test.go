package tunbridge

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Обработчик датаграмм обязан возвращаться сразу.
//
// gvisor вызывает его синхронно, прямо из разбора пакета: для соединений он
// заводит горутину сам, а для датаграмм — нет. Всё время, проведённое внутри,
// стек не разбирает пакеты — ни свои, ни чужие, очередь одна.
//
// Проверка внутренняя, а не снаружи пакета, потому что обработчик неэкспортный
// и подсунуть ему подставной дозвон иначе нельзя. Цена оправдана: без неё
// задержка возвращается молча, а выглядит она как «весь интернет встал».

// stuckDialer изображает недоступную ноду: дозвон не отвечает никогда.
type stuckDialer struct{ released chan struct{} }

func (d stuckDialer) DialTarget(ctx context.Context, _ vp1.Address) (net.Conn, error) {
	select {
	case <-d.released:
	case <-ctx.Done():
	}
	return nil, errors.New("некуда")
}

func (d stuckDialer) DialDatagrams(ctx context.Context, _ vp1.Address) (net.Conn, error) {
	select {
	case <-d.released:
	case <-ctx.Done():
	}
	return nil, errors.New("некуда")
}

// fakeUDPConn — датаграммное соединение от стека, каким его видит обработчик.
type fakeUDPConn struct {
	id      stack.TransportEndpointID
	payload []byte
	read    atomic.Int32
	closed  atomic.Bool
	written chan []byte
}

func newFakeUDPConn(port uint16, payload string) *fakeUDPConn {
	return &fakeUDPConn{
		id: stack.TransportEndpointID{
			LocalAddress: tcpip.AddrFrom4([4]byte{8, 8, 8, 8}),
			LocalPort:    port,
		},
		payload: []byte(payload),
		written: make(chan []byte, 4),
	}
}

func (c *fakeUDPConn) ID() stack.TransportEndpointID { return c.id }

func (c *fakeUDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	// Первая датаграмма — та, что подняла соединение. Дальше приложение молчит,
	// как оно обычно и делает, отправив один запрос.
	if c.read.Add(1) > 1 {
		return 0, nil, errors.New("больше нечего")
	}
	n := copy(p, c.payload)
	return n, &net.UDPAddr{IP: net.IPv4(10, 19, 84, 2), Port: 40000}, nil
}

func (c *fakeUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	select {
	case c.written <- append([]byte(nil), p...):
	default:
	}
	return len(p), nil
}

func (c *fakeUDPConn) Read(p []byte) (int, error) {
	n, _, err := c.ReadFrom(p)
	return n, err
}

func (c *fakeUDPConn) Write(p []byte) (int, error) { return c.WriteTo(p, nil) }

func (c *fakeUDPConn) RemoteAddr() net.Addr { return &net.UDPAddr{} }

func (c *fakeUDPConn) Close() error                     { c.closed.Store(true); return nil }
func (c *fakeUDPConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *fakeUDPConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeUDPConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeUDPConn) SetWriteDeadline(time.Time) error { return nil }

// TestHandleUDPReturnsImmediately — обработчик не держит стек.
//
// Дозвон здесь не отвечает вовсе. Если бы обработчик ждал его сам, стек стоял
// бы всё это время — а с ним и весь трафик телефона.
func TestHandleUDPReturnsImmediately(t *testing.T) {
	released := make(chan struct{})
	defer close(released)

	h := &handler{dialer: stuckDialer{released: released}, dns: "1.1.1.1:53"}

	done := make(chan struct{})
	go func() {
		h.HandleUDP(newFakeUDPConn(443, "запрос"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleUDP не вернулся — стек стоит, пока он думает")
	}
}

// TestUDPFallsBackToNamesOverTCP — отказ ноды от датаграмм не ломает имена.
//
// Нода у продавца может быть старой. Датаграммы тогда не поедут, и это
// переживаемо, а вот молча оставить телефон без имён нельзя: без них не
// откроется ничего вообще.
func TestUDPFallsBackToNamesOverTCP(t *testing.T) {
	h := &handler{dialer: refusingDialer{}, dns: "1.1.1.1:53"}
	conn := newFakeUDPConn(dnsPort, "запрос имени")

	h.serveUDP(conn)

	if !h.noUDP.Load() {
		t.Error("отказ ноды от датаграмм не запомнен — платить круг будем на каждом потоке")
	}
	if refusals.Load() == 0 {
		t.Error("запасной путь по TCP даже не пробовали")
	}
}

// refusingDialer изображает ноду, которая не знает про датаграммы.
type refusingDialer struct{}

var refusals atomic.Int32

func (refusingDialer) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	refusals.Add(1)
	return nil, errors.New("нода недоступна")
}

func (refusingDialer) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	return nil, vp1.ErrDatagramsUnsupported
}
