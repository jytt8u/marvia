package foreign

import (
	"context"
	"net"
	"testing"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
)

// plainDialer отдаёт одно и то же соединение, чтобы было видно, обернули ли
// его.
type plainDialer struct{ conn net.Conn }

func (d plainDialer) Dial(context.Context, xnet.Address, xnet.Destination, *internet.SocketConfig) (xnet.Conn, error) {
	return d.conn, nil
}

func (plainDialer) DestIpAddress() xnet.IP { return nil }

// TestFragmentTouchesOnlyTCPAndOnlyWhenAsked: выключенное дробление не
// меняет ничего, а включённое не трогает датаграммы — у QUIC и Hysteria нет
// TCP-сегментов, которые можно было бы резать.
func TestFragmentTouchesOnlyTCPAndOnlyWhenAsked(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	d := fragmentDialer{plainDialer{a}}
	tcp := xnet.TCPDestination(xnet.LocalHostIP, 443)
	udp := xnet.UDPDestination(xnet.LocalHostIP, 443)
	t.Cleanup(func() { SetFragment(false) })

	SetFragment(false)
	if c, _ := d.Dial(context.Background(), nil, tcp, nil); c != a {
		t.Fatal("выключенное дробление обернуло соединение")
	}
	SetFragment(true)
	if c, _ := d.Dial(context.Background(), nil, udp, nil); c != a {
		t.Fatal("дробление обернуло датаграммы")
	}
	if c, _ := d.Dial(context.Background(), nil, tcp, nil); c == a {
		t.Fatal("включённое дробление не тронуло TCP")
	}
}

// TestXrayDialsThroughOurDialer: подмена стоит, пока пакет подключён, —
// иначе выключатель в настройках был бы нарисованным.
func TestXrayDialsThroughOurDialer(t *testing.T) {
	SetFragment(true)
	defer SetFragment(false)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	c, err := internet.DialSystem(context.Background(), xnet.TCPDestination(xnet.LocalHostIP, xnet.Port(port)), nil)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer c.Close()
	if _, raw := c.(*net.TCPConn); raw {
		t.Fatal("Xray дозвонился мимо нашего дозвона: дробление не сработало бы")
	}
}
