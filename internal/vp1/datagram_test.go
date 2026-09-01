package vp1_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/veilproject/veil/internal/vp1"
)

// Датаграммы проверяем на границах, а не на содержимом.
//
// Байты внутри доедут в любом случае — их несёт тот же поток, что и всё
// остальное. Ломается здесь другое: две датаграммы слипаются в одну или одна
// разъезжается на две. Для TCP это незаметно, для UDP это порча данных,
// причём тихая: приложение получит склеенный мусор и решит, что сеть плохая.

// TestDatagramsKeepTheirEdges — две датаграммы не слипаются в одну.
func TestDatagramsKeepTheirEdges(t *testing.T) {
	var wire bytes.Buffer

	first := []byte("первая")
	second := []byte("вторая, подлиннее")

	if err := vp1.WriteDatagram(&wire, first); err != nil {
		t.Fatalf("первая: %v", err)
	}
	if err := vp1.WriteDatagram(&wire, second); err != nil {
		t.Fatalf("вторая: %v", err)
	}

	buf := make([]byte, vp1.MaxDatagram)

	n, err := vp1.ReadDatagram(&wire, buf)
	if err != nil {
		t.Fatalf("чтение первой: %v", err)
	}
	if string(buf[:n]) != string(first) {
		t.Fatalf("первая пришла как %q", buf[:n])
	}

	n, err = vp1.ReadDatagram(&wire, buf)
	if err != nil {
		t.Fatalf("чтение второй: %v", err)
	}
	if string(buf[:n]) != string(second) {
		t.Fatalf("вторая пришла как %q", buf[:n])
	}
}

// TestEmptyDatagramSurvives — пустая датаграмма остаётся датаграммой.
//
// В UDP пакет нулевой длины законен, и приложения им пользуются: так, например,
// проверяют, что путь до собеседника жив. Потерять его — значит потерять
// событие, а не байты.
func TestEmptyDatagramSurvives(t *testing.T) {
	var wire bytes.Buffer

	if err := vp1.WriteDatagram(&wire, nil); err != nil {
		t.Fatalf("запись: %v", err)
	}
	if err := vp1.WriteDatagram(&wire, []byte("следом")); err != nil {
		t.Fatalf("запись следующей: %v", err)
	}

	buf := make([]byte, 64)

	n, err := vp1.ReadDatagram(&wire, buf)
	if err != nil {
		t.Fatalf("чтение пустой: %v", err)
	}
	if n != 0 {
		t.Fatalf("пустая пришла длиной %d", n)
	}

	n, err = vp1.ReadDatagram(&wire, buf)
	if err != nil {
		t.Fatalf("чтение следующей: %v", err)
	}
	if string(buf[:n]) != "следом" {
		t.Fatalf("следующая съехала: %q", buf[:n])
	}
}

// TestTooLongDatagramIsRefused — не влезающая датаграмма отвергается целиком.
//
// Отдать первые сколько-то байт было бы хуже всего: приложение получило бы
// обрезанный пакет как целый, а хвост разъехался бы по следующим чтениям и
// сломал весь поток.
func TestTooLongDatagramIsRefused(t *testing.T) {
	var wire bytes.Buffer

	if err := vp1.WriteDatagram(&wire, bytes.Repeat([]byte("ы"), 100)); err != nil {
		t.Fatalf("запись: %v", err)
	}

	if _, err := vp1.ReadDatagram(&wire, make([]byte, 10)); err == nil {
		t.Fatal("длинная датаграмма прошла в маленький буфер")
	}
}

// TestHalfWrittenDatagramIsAnError — оборванная передача не выдаётся за целую.
func TestHalfWrittenDatagramIsAnError(t *testing.T) {
	var full bytes.Buffer
	if err := vp1.WriteDatagram(&full, []byte("длинная датаграмма")); err != nil {
		t.Fatalf("запись: %v", err)
	}

	cut := full.Bytes()[:full.Len()-4]

	if _, err := vp1.ReadDatagram(bytes.NewReader(cut), make([]byte, 64)); err == nil {
		t.Fatal("обрезанная датаграмма прочиталась как целая")
	}
}

// TestClosedStreamEndsQuietly — закрытый поток даёт io.EOF, а не панику.
func TestClosedStreamEndsQuietly(t *testing.T) {
	if _, err := vp1.ReadDatagram(bytes.NewReader(nil), make([]byte, 8)); !errors.Is(err, io.EOF) {
		t.Fatalf("пустой поток вернул %v, а ожидался io.EOF", err)
	}
}

// TestRequestCarriesItsKind — вид запроса доезжает вместе с адресом.
//
// Именно этим TCP отличается от UDP на проводе: отдельного байта команды нет,
// вид закодирован в старшем бите типа адреса.
func TestRequestCarriesItsKind(t *testing.T) {
	cases := []struct {
		name string
		addr vp1.Address
	}{
		{"IPv4", vp1.Address{Type: vp1.AtypIPv4, Host: "8.8.8.8", Port: 443}},
		{"IPv6", vp1.Address{Type: vp1.AtypIPv6, Host: "2001:4860:4860::8888", Port: 443}},
		{"домен", vp1.Address{Type: vp1.AtypDomain, Host: "example.com", Port: 443}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, kind := range []vp1.Kind{vp1.KindTCP, vp1.KindUDP} {
				var wire bytes.Buffer
				if err := vp1.WriteRequestOf(&wire, c.addr, kind); err != nil {
					t.Fatalf("%s: запись: %v", kind, err)
				}

				got, gotKind, err := vp1.ReadRequestOf(&wire)
				if err != nil {
					t.Fatalf("%s: чтение: %v", kind, err)
				}
				if gotKind != kind {
					t.Errorf("%s: вид приехал как %s", kind, gotKind)
				}
				if got.Port != c.addr.Port || got.Type != c.addr.Type {
					t.Errorf("%s: адрес приехал как %+v, ожидался %+v", kind, got, c.addr)
				}
			}
		})
	}
}

// TestOldNodeRefusesDatagrams — старая нода отвергает запрос, а не молчит.
//
// Ноды обновляет продавец, и часть какое-то время остаётся старой. Важно,
// чтобы она на запрос датаграмм отвечала внятной ошибкой: по ней клиент
// поймёт, что надо вернуться к прежнему поведению, а не будет думать, что
// цель недоступна, и ждать.
func TestOldNodeRefusesDatagrams(t *testing.T) {
	var wire bytes.Buffer
	addr := vp1.Address{Type: vp1.AtypIPv4, Host: "8.8.8.8", Port: 53}

	if err := vp1.WriteRequestOf(&wire, addr, vp1.KindUDP); err != nil {
		t.Fatalf("запись: %v", err)
	}

	// Старая нода читает запрос обычным ReadAddress — она про старший бит не
	// знает и видит незнакомый тип адреса.
	_, err := vp1.ReadAddress(&wire)
	if err == nil {
		t.Fatal("старая нода приняла запрос датаграмм как обычный адрес")
	}
	if !strings.Contains(err.Error(), "тип адреса") {
		t.Fatalf("отказ пришёл не про тип адреса: %v", err)
	}
}
