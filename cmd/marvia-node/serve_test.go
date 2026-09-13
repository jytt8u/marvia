package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

// Нода не пишет в журнал, куда ходил человек. Это обещание README и правило
// архитектуры: ноду изымают, и её журнал не должен быть историей посещений.
//
// Самое тонкое место — не строки формата, а сами ошибки сети: они несут адрес
// внутри себя. «dial tcp 93.184.216.34:443: connect: connection refused» —
// это уже запись о том, куда человек шёл, и записать её целиком значит свести
// на нет всё остальное. Поэтому в журнал уходит не ошибка, а why(ошибка).

// адреса и имена, которые не должны просочиться ни при каком виде ошибки.
const (
	secretHost = "тайный-сайт.example"
	secretIP   = "93.184.216.34"
	secretPort = "8443"
)

func TestWhyNeverNamesTheDestination(t *testing.T) {
	tcpAddr := &net.TCPAddr{IP: net.ParseIP(secretIP), Port: 8443}

	cases := map[string]error{
		"имя не найдено": &net.DNSError{
			Err: "no such host", Name: secretHost, IsNotFound: true,
		},
		"таймаут разрешения имени": &net.DNSError{
			Err: "timeout", Name: secretHost, IsTimeout: true,
		},
		"отказ в соединении": &net.OpError{
			Op: "dial", Net: "tcp", Addr: tcpAddr,
			Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
		},
		"таймаут соединения": &net.OpError{
			Op: "dial", Net: "tcp", Addr: tcpAddr, Err: errors.New("i/o timeout"),
		},
		"негодный адрес": &net.AddrError{
			Err: "missing port in address", Addr: secretHost,
		},
		"имя внутри обёртки": fmt.Errorf("не подключились: %w", &net.DNSError{
			Err: "no such host", Name: secretHost, IsNotFound: true,
		}),
		"адрес внутри обёртки": fmt.Errorf("поток: %w", &net.OpError{
			Op: "dial", Net: "tcp", Addr: tcpAddr,
			Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
		}),
	}

	for name, err := range cases {
		got := why(err)
		for _, leak := range []string{secretHost, secretIP, secretPort} {
			if strings.Contains(got, leak) {
				t.Errorf("%s: в журнал уехало %q — why вернул %q", name, leak, got)
			}
		}
		if got == "" {
			t.Errorf("%s: why промолчал, оператору не о чем судить", name)
		}
	}
}

// Причина остаётся полезной: оператор обязан отличить «цель отказала» от
// «имя не разрешилось», иначе журнал незачем вести вовсе.
func TestWhyStillTellsTheReasonApart(t *testing.T) {
	tcpAddr := &net.TCPAddr{IP: net.ParseIP(secretIP), Port: 8443}

	refused := why(&net.OpError{
		Op: "dial", Net: "tcp", Addr: tcpAddr,
		Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
	})
	missing := why(&net.DNSError{Err: "no such host", Name: secretHost, IsNotFound: true})

	if refused == missing {
		t.Fatalf("отказ и ненайденное имя неразличимы: оба %q", refused)
	}
	if !strings.Contains(missing, "имя") {
		t.Errorf("про имя ничего не сказано: %q", missing)
	}
	if !strings.Contains(refused, "connect") {
		t.Errorf("про отказ ничего не сказано: %q", refused)
	}
}

// Своя ошибка проходит как есть: в ней адресов нет, а текст писали мы сами.
func TestWhyKeepsOurOwnErrors(t *testing.T) {
	if got := why(errors.New("протокол не опознан")); got != "протокол не опознан" {
		t.Errorf("своя ошибка исказилась: %q", got)
	}
	if got := why(nil); got == "" {
		t.Error("why(nil) промолчал — в формате останется пустое место")
	}
}
