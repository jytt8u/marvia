package tunbridge

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/jytt8u/marvia/internal/vp1"
)

// query собирает DNS-запрос одного имени нужного типа, с EDNS в хвосте, как
// шлёт его Android.
func query(name string, qtype uint16) []byte {
	q := []byte{0xAB, 0xCD, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 1}
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			q = append(q, byte(i-start))
			q = append(q, name[start:i]...)
			start = i + 1
		}
	}
	q = append(q, 0)
	q = binary.BigEndian.AppendUint16(q, qtype)
	q = binary.BigEndian.AppendUint16(q, 1)
	// OPT: имя корня, тип 41, размер 1232, без опций.
	q = append(q, 0, 0, 41, 0x04, 0xD0, 0, 0, 0, 0, 0, 0)
	return q
}

// TestNodeWithoutIPv6StopsGettingIPv6: три отказа по IPv6 подряд у старой
// ноды или один код «нет пути» у новой — и мост больше не шлёт ей IPv6, а на
// AAAA отвечает сам. Пока признака нет — AAAA уходит ноде как обычно.
func TestNodeWithoutIPv6StopsGettingIPv6(t *testing.T) {
	aaaa := query("www.google.com", dnsTypeAAAA)

	var old v6State
	if _, ok := old.localAnswer(aaaa); ok {
		t.Fatal("IPv6 выключен без единого отказа")
	}
	unreachable := &vp1.RefusedError{Target: "[2a00::1]:443", Status: vp1.StatusUnreachable}
	old.observe(unreachable)
	old.observe(unreachable)
	if old.off.Load() {
		t.Fatal("двух отказов хватило, чтобы решить за всю ноду")
	}
	old.observe(unreachable)
	if !old.off.Load() {
		t.Fatal("три отказа подряд не выключили IPv6")
	}

	var fresh v6State
	fresh.observe(&vp1.RefusedError{Status: vp1.StatusNoRoute})
	if !fresh.off.Load() {
		t.Fatal("код «нет пути» не выключил IPv6")
	}

	// Удачное IPv6-соединение между отказами — значит, у ноды IPv6 есть.
	var mixed v6State
	mixed.observe(unreachable)
	mixed.observe(unreachable)
	mixed.observe(nil)
	mixed.observe(unreachable)
	if mixed.off.Load() {
		t.Fatal("отказы одного сайта выключили IPv6 ноде, у которой он работает")
	}

	// Обрыв до ноды — не отказ по цели, ничего не говорит про IPv6.
	var dead v6State
	for range 5 {
		dead.observe(errors.New("нода недоступна"))
	}
	if dead.off.Load() {
		t.Fatal("смерть ноды принята за отсутствие IPv6")
	}
}

// TestAAAAIsAnsweredEmptyNotNonexistent: ответ на AAAA — «адресов нет», а не
// «имени нет»: иначе приложение не стало бы пробовать и IPv4.
func TestAAAAIsAnsweredEmptyNotNonexistent(t *testing.T) {
	var s v6State
	s.off.Store(true)
	q := query("example.com", dnsTypeAAAA)
	answer, ok := s.localAnswer(q)
	if !ok {
		t.Fatal("AAAA ушёл ноде без IPv6")
	}
	if answer[0] != 0xAB || answer[1] != 0xCD {
		t.Fatal("в ответе чужой номер запроса")
	}
	if answer[2]&0x80 == 0 || answer[3]&0x0F != 0 {
		t.Fatalf("ответ не NOERROR: флаги %08b %08b", answer[2], answer[3])
	}
	if binary.BigEndian.Uint16(answer[6:8]) != 0 || binary.BigEndian.Uint16(answer[10:12]) != 0 {
		t.Fatal("в пустом ответе есть записи")
	}
	if len(answer) != len(q)-11 {
		t.Fatalf("ответ %d байт: EDNS из запроса уехал в ответ без счётчика", len(answer))
	}

	if _, ok := s.localAnswer(query("example.com", 1)); ok {
		t.Fatal("запрос IPv4-адреса перехвачен")
	}
	if _, ok := s.localAnswer([]byte{1, 2, 3}); ok {
		t.Fatal("мусор принят за запрос")
	}
}

// TestSiteRefusalIsNotATunnelFailure: «сайт не отвечает» не попадает в то, что
// видит человек, а «нода не отвечает» — попадает.
func TestSiteRefusalIsNotATunnelFailure(t *testing.T) {
	var got []error
	h := &handler{onError: func(err error) { got = append(got, err) }}
	h.failDial(&vp1.RefusedError{Target: "93.158.177.1:443", Status: vp1.StatusUnreachable})
	if len(got) != 0 {
		t.Fatalf("отказ по сайту показан как беда туннеля: %v", got)
	}
	h.failDial(vp1.ErrNodeUnreachable)
	if len(got) != 1 {
		t.Fatal("смерть ноды не показана")
	}
}
