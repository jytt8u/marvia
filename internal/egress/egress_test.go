package egress_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/egress"
)

// TestTheNodeDoesNotReachIntoItsOwnNetwork: покупатель называет цель сам, и
// без этой проверки нода — дверь к тому, что слушает сама машина и её соседи
// в облаке. Каждый из этих адресов кто-нибудь однажды попросит.
func TestTheNodeDoesNotReachIntoItsOwnNetwork(t *testing.T) {
	closed := []string{
		"127.0.0.1", "127.9.9.9", "::1",
		"10.0.0.1", "172.16.5.5", "192.168.1.1",
		"169.254.169.254", // метаданные облаков
		"fe80::1", "fd00::1",
		"100.64.0.1", // CGNAT и внутренние сети облаков
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
	}
	for _, s := range closed {
		if !egress.Forbidden(netip.MustParseAddr(s)) {
			t.Errorf("%s должен быть закрыт", s)
		}
	}

	open := []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"}
	for _, s := range open {
		if egress.Forbidden(netip.MustParseAddr(s)) {
			t.Errorf("%s должен быть открыт", s)
		}
	}
}

// TestALoopbackTargetIsRefusedBeforeDialing: отказ — до сокета, чтобы даже
// попытка не оставила следа на закрытом порту.
func TestALoopbackTargetIsRefusedBeforeDialing(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
			accepted <- struct{}{}
		}
	}()

	_, err = egress.Dial(context.Background(), "tcp", ln.Addr().String(), 2*time.Second)
	if !errors.Is(err, egress.ErrForbidden) {
		t.Fatalf("ожидался ErrForbidden, получено %v", err)
	}
	select {
	case <-accepted:
		t.Fatal("нода всё же дозвонилась до петли")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestLocalhostByNameIsRefusedToo: имя, указывающее внутрь, — та же дверь,
// только через DNS. Проверять надо адрес, а не строку.
func TestLocalhostByNameIsRefusedToo(t *testing.T) {
	_, err := egress.Dial(context.Background(), "tcp", "localhost:1", 2*time.Second)
	if !errors.Is(err, egress.ErrForbidden) {
		t.Fatalf("ожидался ErrForbidden, получено %v", err)
	}
}

// TestSMTPIsClosedForEveryone: через VPN на 25-й порт ходят только спамеры,
// а расплачивается за них нода — баном у хостера.
func TestSMTPIsClosedForEveryone(t *testing.T) {
	_, err := egress.Dial(context.Background(), "tcp", "1.1.1.1:25", 2*time.Second)
	if !errors.Is(err, egress.ErrPort) {
		t.Fatalf("ожидался ErrPort, получено %v", err)
	}
	if egress.ForbiddenPort(587) || egress.ForbiddenPort(465) {
		t.Fatal("почта с логином должна ходить")
	}
}

// TestErrorsCarryNoTarget: ошибка уходит в журнал ноды, а журнал не должен
// знать, куда ходил человек. Ошибка разрешения от системы пусть носит имя —
// её разбирает why в ноде, — но свои тексты egress пишет без адреса.
func TestErrorsCarryNoTarget(t *testing.T) {
	for _, target := range []string{"127.0.0.1:80", "8.8.8.8:25", "example.com:x"} {
		_, err := egress.Dial(context.Background(), "tcp", target, time.Second)
		if err == nil {
			t.Fatalf("%s: ожидалась ошибка", target)
		}
		host, _, _ := net.SplitHostPort(target)
		if strings.Contains(err.Error(), host) {
			t.Errorf("ошибка %q выдаёт цель %s", err, host)
		}
	}
}
