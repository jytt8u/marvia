package socks5_test

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/socks5"
	"github.com/jytt8u/marvia/internal/vp1"
)

// SOCKS5 — то, через что с клиентом разговаривает каждая программа на машине.
// Ошибка здесь выглядит не как поломка туннеля, а как «интернет не работает»,
// поэтому проверяем разговор целиком, байтами, а не разбор структур.

// talk поднимает пару соединений и проводит Handshake на одной стороне,
// отдавая вторую тесту, чтобы тот отыграл клиента.
//
// Пара — настоящая, по петле, а не net.Pipe. net.Pipe не буферизует: на отказе
// мы пишем ответ клиенту, пока тот ещё дописывает запрос, и обе стороны встают
// насмерть. У сокета есть буфер, и такого не бывает — а проверяем мы именно
// поведение на сокете.
func talk(t *testing.T, client func(net.Conn)) (vp1.Address, error) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("петля: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Errorf("подключение: %v", err)
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		client(c)
	}()

	ours, err := ln.Accept()
	if err != nil {
		t.Fatalf("приём: %v", err)
	}
	defer ours.Close()
	_ = ours.SetDeadline(time.Now().Add(5 * time.Second))

	addr, hsErr := socks5.Handshake(ours)
	<-done
	return addr, hsErr
}

// TestBrowserReachesTheHostItAskedFor — обычный CONNECT по имени доходит
// до нас тем же адресом, каким его назвал браузер.
func TestBrowserReachesTheHostItAskedFor(t *testing.T) {
	addr, err := talk(t, func(c net.Conn) {
		// Согласование: версия 5, один метод — без аутентификации.
		_, _ = c.Write([]byte{0x05, 0x01, 0x00})
		var reply [2]byte
		_, _ = io.ReadFull(c, reply[:])
		if reply[0] != 0x05 || reply[1] != 0x00 {
			t.Errorf("согласование ответило % x, ждали 05 00", reply)
		}

		// CONNECT на example.org:443 доменным именем.
		host := "example.org"
		req := []byte{0x05, 0x01, 0x00, vp1.AtypDomain, byte(len(host))}
		req = append(req, host...)
		req = append(req, 0x01, 0xBB) // 443
		_, _ = c.Write(req)
	})
	if err != nil {
		t.Fatalf("рукопожатие не прошло: %v", err)
	}
	if addr.Host != "example.org" || addr.Port != 443 {
		t.Fatalf("адрес разобран как %s, ждали example.org:443", addr)
	}
	if addr.Type != vp1.AtypDomain {
		t.Errorf("тип адреса %d, ждали доменное имя: имя должно резолвиться на"+
			" той стороне туннеля, иначе провайдер видит, куда мы идём", addr.Type)
	}
}

// TestIPv4RequestSurvivesIntact — адрес числом доезжает без искажения.
func TestIPv4RequestSurvivesIntact(t *testing.T) {
	addr, err := talk(t, func(c net.Conn) {
		_, _ = c.Write([]byte{0x05, 0x01, 0x00})
		var reply [2]byte
		_, _ = io.ReadFull(c, reply[:])
		_, _ = c.Write([]byte{0x05, 0x01, 0x00, vp1.AtypIPv4, 93, 184, 216, 34, 0x00, 0x50})
	})
	if err != nil {
		t.Fatalf("рукопожатие не прошло: %v", err)
	}
	if addr.Host != "93.184.216.34" || addr.Port != 80 {
		t.Fatalf("адрес разобран как %s, ждали 93.184.216.34:80", addr)
	}
}

// TestUDPAssociateIsRefusedOutLoud — на неподдерживаемую команду клиент
// получает ответ с кодом, а не тишину.
//
// Тишина для браузера — это зависшая вкладка: он ждёт ответа до таймаута.
// Отказ с кодом он показывает сразу.
func TestUDPAssociateIsRefusedOutLoud(t *testing.T) {
	var answer [10]byte
	_, err := talk(t, func(c net.Conn) {
		_, _ = c.Write([]byte{0x05, 0x01, 0x00})
		var reply [2]byte
		_, _ = io.ReadFull(c, reply[:])

		// 0x03 — UDP ASSOCIATE, которого у нас нет.
		_, _ = c.Write([]byte{0x05, 0x03, 0x00, vp1.AtypIPv4, 0, 0, 0, 0, 0, 0})
		_, _ = io.ReadFull(c, answer[:])
	})
	if !errors.Is(err, socks5.ErrUnsupportedCommand) {
		t.Fatalf("ошибка %v, ждали ErrUnsupportedCommand", err)
	}
	if answer[0] != 0x05 || answer[1] != 0x07 {
		t.Fatalf("ответ % x, ждали код 07 «команда не поддерживается»", answer[:2])
	}
}

// TestClientWithoutNoAuthIsTurnedAway — клиент, не умеющий «без пароля»,
// получает 0xFF, а не молчание.
func TestClientWithoutNoAuthIsTurnedAway(t *testing.T) {
	var reply [2]byte
	_, err := talk(t, func(c net.Conn) {
		// Единственный предложенный метод — GSSAPI (0x01), которого у нас нет.
		_, _ = c.Write([]byte{0x05, 0x01, 0x01})
		_, _ = io.ReadFull(c, reply[:])
	})
	if err == nil {
		t.Fatal("клиент без подходящего метода прошёл рукопожатие")
	}
	if reply[0] != 0x05 || reply[1] != 0xFF {
		t.Fatalf("ответ % x, ждали 05 FF «нет подходящего метода»", reply)
	}
}

// TestSocks4IsNotMistakenForSocks5 — четвёртая версия отвергается.
func TestSocks4IsNotMistakenForSocks5(t *testing.T) {
	_, err := talk(t, func(c net.Conn) {
		_, _ = c.Write([]byte{0x04, 0x01, 0x00})
	})
	if err == nil {
		t.Fatal("SOCKS4 приняли за SOCKS5")
	}
}

// TestReplyFailureTellsHostUnreachable — сетевая ошибка превращается в код
// «хост недоступен», а не в общий отказ.
//
// Браузер по этому коду пишет человеку «сайт не отвечает», а по общему —
// «ошибка прокси». Второе отправляет человека чинить не то.
func TestReplyFailureTellsHostUnreachable(t *testing.T) {
	ours, theirs := net.Pipe()
	defer ours.Close()
	defer theirs.Close()
	_ = theirs.SetDeadline(time.Now().Add(5 * time.Second))

	var answer [10]byte
	read := make(chan struct{})
	go func() { defer close(read); _, _ = io.ReadFull(theirs, answer[:]) }()

	timeout := &net.OpError{Op: "dial", Err: &timeoutError{}}
	if err := socks5.ReplyFailure(ours, timeout); err != nil {
		t.Fatalf("ответ не ушёл: %v", err)
	}
	<-read

	if answer[1] != 0x04 {
		t.Fatalf("код ответа %#x, ждали 04 «хост недоступен»", answer[1])
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "таймаут" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
