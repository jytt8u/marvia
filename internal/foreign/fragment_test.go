package foreign

import (
	"context"
	"net"
	"testing"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
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

	cut, err := StartWith(mustParse(t, "trojan://p@127.0.0.1:1?sni=x.test"), Options{Fragment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cut.Close()
	whole, err := Start(mustParse(t, "trojan://p@127.0.0.1:2?sni=x.test"))
	if err != nil {
		t.Fatal(err)
	}
	defer whole.Close()
	of := func(e *Engine) context.Context {
		return context.WithValue(context.Background(), core.XrayKey(1), e.inst)
	}

	if c, _ := d.Dial(of(whole), nil, tcp, nil); c != a {
		t.Fatal("движок без дробления обернул соединение")
	}
	if c, _ := d.Dial(of(cut), nil, udp, nil); c != a {
		t.Fatal("дробление обернуло датаграммы")
	}
	if c, _ := d.Dial(of(cut), nil, tcp, nil); c == a {
		t.Fatal("движок с дроблением не тронул TCP")
	}
	cut.Close()
	if c, _ := d.Dial(of(cut), nil, tcp, nil); c != a {
		t.Fatal("закрытый движок остался в списке режущих")
	}
}

func mustParse(t *testing.T, link string) Link {
	t.Helper()
	l, err := Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestXrayDialsThroughOurDialer: подмена стоит, пока пакет подключён, и
// контекст дозвона несёт экземпляр движка — иначе выключатель в настройках
// был бы нарисованным.
func TestXrayDialsThroughOurDialer(t *testing.T) {
	e, err := StartWith(mustParse(t, "trojan://p@127.0.0.1:3?sni=x.test"), Options{Fragment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
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
	ctx := context.WithValue(context.Background(), core.XrayKey(1), e.inst)
	c, err := internet.DialSystem(ctx, xnet.TCPDestination(xnet.LocalHostIP, xnet.Port(port)), nil)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer c.Close()
	if _, raw := c.(*net.TCPConn); raw {
		t.Fatal("Xray дозвонился мимо нашего дозвона: дробление не сработало бы")
	}
}
