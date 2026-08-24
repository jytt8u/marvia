package inbound_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/veilproject/veil/internal/inbound"
	"github.com/veilproject/veil/internal/rewind"
	"github.com/veilproject/veil/internal/users"
	"github.com/veilproject/veil/internal/vp1"
)

// feed отдаёт данные через соединение, обёрнутое в rewind.
func feed(t *testing.T, data []byte) *rewind.Conn {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		_, _ = client.Write(data)
	}()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return rewind.New(server)
}

// vlessRequest собирает запрос VLESS вручную — так же, как его собрал бы
// v2rayNG или Hiddify.
func vlessRequest(uuid []byte, host string, port uint16) []byte {
	buf := []byte{0x00}
	buf = append(buf, uuid...)
	buf = append(buf, 0x00) // дополнений нет
	buf = append(buf, 0x01) // команда TCP
	buf = binary.BigEndian.AppendUint16(buf, port)
	buf = append(buf, 0x02, byte(len(host))) // тип адреса: домен
	buf = append(buf, host...)
	return append(buf, "GET / HTTP/1.1\r\n\r\n"...)
}

// trojanRequest собирает запрос Trojan.
func trojanRequest(password, host string, port uint16) []byte {
	buf := []byte(users.TrojanDigestHex(password))
	buf = append(buf, '\r', '\n')
	buf = append(buf, 0x01, 0x03, byte(len(host))) // CONNECT, домен
	buf = append(buf, host...)
	buf = binary.BigEndian.AppendUint16(buf, port)
	buf = append(buf, '\r', '\n')
	return append(buf, "GET / HTTP/1.1\r\n\r\n"...)
}

// vp1First изображает начало первого сообщения VP1: два байта длины и тело.
func vp1First(length int) []byte {
	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[:2], uint16(length))
	for i := 2; i < len(buf); i++ {
		buf[i] = byte(i * 7)
	}
	return buf
}

func TestClassifyTrojan(t *testing.T) {
	conn := feed(t, trojanRequest("пароль", "example.com", 443))

	proto, err := inbound.Classify(conn, nil)
	if err != nil {
		t.Fatalf("опознание: %v", err)
	}
	if proto != inbound.ProtoTrojan {
		t.Fatalf("опознано как %s, ожидался trojan", proto)
	}
}

// TestClassifyVLESSAndVP1ShareFirstByte — главная проверка диспетчера.
//
// У VLESS первый байт равен нулю (версия), и у VP1 первый байт тоже ноль,
// когда длина первого сообщения меньше 256. По форме они неразличимы, и
// единственный способ решить — поискать UUID в списке пользователей.
func TestClassifyVLESSAndVP1ShareFirstByte(t *testing.T) {
	uuid, err := users.ParseUUID("2b1c9f3e-7a45-4d18-9c0e-6f8a1b2c3d4e")
	if err != nil {
		t.Fatalf("UUID: %v", err)
	}

	known := func(candidate []byte) bool { return bytes.Equal(candidate, uuid) }

	// Свой клиент VLESS: UUID в списке — значит VLESS.
	conn := feed(t, vlessRequest(uuid, "example.com", 443))
	proto, err := inbound.Classify(conn, known)
	if err != nil {
		t.Fatalf("опознание VLESS: %v", err)
	}
	if proto != inbound.ProtoVLESS {
		t.Fatalf("опознано как %s, ожидался vless", proto)
	}

	// Чужой UUID: в списке нет — значит это не VLESS, пробуем VP1.
	stranger, _ := users.ParseUUID("ffffffff-ffff-ffff-ffff-ffffffffffff")
	conn = feed(t, vlessRequest(stranger, "example.com", 443))
	proto, err = inbound.Classify(conn, known)
	if err != nil {
		t.Fatalf("опознание чужого UUID: %v", err)
	}
	if proto != inbound.ProtoVP1 {
		t.Fatalf("опознано как %s, ожидался vp1", proto)
	}

	// Настоящий VP1 с длиной меньше 256: первый байт ноль, UUID не совпадёт.
	conn = feed(t, vp1First(150))
	proto, err = inbound.Classify(conn, known)
	if err != nil {
		t.Fatalf("опознание VP1: %v", err)
	}
	if proto != inbound.ProtoVP1 {
		t.Fatalf("опознано как %s, ожидался vp1", proto)
	}
}

func TestClassifyVP1LongHandshake(t *testing.T) {
	// Длина больше 255 — старший байт единица, ни с чем не спутать.
	conn := feed(t, vp1First(300))

	proto, err := inbound.Classify(conn, func([]byte) bool { return true })
	if err != nil {
		t.Fatalf("опознание: %v", err)
	}
	if proto != inbound.ProtoVP1 {
		t.Fatalf("опознано как %s, ожидался vp1", proto)
	}
}

// TestClassifyBrowserIsUnknown: обычный HTTP-запрос должен уйти
// сайту-прикрытию, а не в разбор протоколов.
func TestClassifyBrowserIsUnknown(t *testing.T) {
	cases := map[string]string{
		"GET":     "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n",
		"POST":    "POST /api HTTP/1.1\r\nHost: example.com\r\n\r\n",
		"CONNECT": "CONNECT example.com:443 HTTP/1.1\r\n\r\n",
		"мусор":   "\x7f\x45\x4c\x46\x02\x01\x01\x00 и дальше всякое, чего мы не знаем",
	}

	for name, payload := range cases {
		conn := feed(t, []byte(payload))
		proto, err := inbound.Classify(conn, func([]byte) bool { return true })
		if err != nil {
			t.Fatalf("%s: опознание: %v", name, err)
		}
		if proto != inbound.ProtoUnknown {
			t.Fatalf("%s: опознано как %s, ожидался неопознанный", name, proto)
		}
	}
}

// TestClassifyRewindsStream: после опознания поток обязан быть отмотан к
// началу — иначе разбор протокола начнётся с середины запроса.
func TestClassifyRewindsStream(t *testing.T) {
	payload := trojanRequest("пароль", "example.com", 443)
	conn := feed(t, payload)

	if _, err := inbound.Classify(conn, nil); err != nil {
		t.Fatalf("опознание: %v", err)
	}

	head := make([]byte, 16)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("чтение после опознания: %v", err)
	}
	if !bytes.Equal(head, payload[:16]) {
		t.Fatalf("поток не отмотан: получено %q", head)
	}
}

func TestReadTrojanRequest(t *testing.T) {
	conn := feed(t, trojanRequest("пароль", "example.com", 8443))

	request, err := inbound.ReadTrojanRequest(conn)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !bytes.Equal(request.Digest, users.TrojanDigest("пароль")) {
		t.Fatal("хеш пароля разобран неверно")
	}
	if request.Target.Host != "example.com" || request.Target.Port != 8443 {
		t.Fatalf("адрес разобран неверно: %+v", request.Target)
	}

	// Полезная нагрузка должна остаться в потоке нетронутой.
	rest := make([]byte, 3)
	if _, err := io.ReadFull(conn, rest); err != nil {
		t.Fatalf("чтение данных после запроса: %v", err)
	}
	if string(rest) != "GET" {
		t.Fatalf("после запроса получено %q", rest)
	}
}

func TestReadVLESSRequest(t *testing.T) {
	uuid, _ := users.ParseUUID("2b1c9f3e-7a45-4d18-9c0e-6f8a1b2c3d4e")
	conn := feed(t, vlessRequest(uuid, "example.org", 443))

	request, err := inbound.ReadVLESSRequest(conn)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !bytes.Equal(request.UUID, uuid) {
		t.Fatal("UUID разобран неверно")
	}
	if request.Target.Type != vp1.AtypDomain || request.Target.Host != "example.org" || request.Target.Port != 443 {
		t.Fatalf("адрес разобран неверно: %+v", request.Target)
	}
}

// TestVLESSResponseHeaderRidesWithData: ответный заголовок VLESS не должен
// уезжать отдельным крошечным пакетом — такая запись сама по себе примета.
func TestVLESSResponseHeaderRidesWithData(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := client.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()

	conn := inbound.NewVLESSConn(server)
	if _, err := conn.Write([]byte("данные")); err != nil {
		t.Fatalf("запись: %v", err)
	}

	first := <-got
	want := append([]byte{0x00, 0x00}, "данные"...)
	if !bytes.Equal(first, want) {
		t.Fatalf("первая порция %q, ожидалась %q", first, want)
	}
}

func TestUUIDRoundTrip(t *testing.T) {
	const canonical = "2b1c9f3e-7a45-4d18-9c0e-6f8a1b2c3d4e"

	raw, err := users.ParseUUID(canonical)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(raw) != 16 {
		t.Fatalf("длина %d, ожидалось 16", len(raw))
	}
	if got := users.FormatUUID(raw); got != canonical {
		t.Fatalf("получено %q, ожидалось %q", got, canonical)
	}
	if _, err := users.ParseUUID("не-uuid"); err == nil {
		t.Fatal("мусор должен отвергаться")
	}
}
