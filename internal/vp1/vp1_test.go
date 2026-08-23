package vp1

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// handshakePair поднимает клиента и сервера на паре связанных сокетов.
func handshakePair(t *testing.T, allow Authorizer) (client, server *Conn, clientPub []byte) {
	t.Helper()

	serverKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи сервера: %v", err)
	}
	clientKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи клиента: %v", err)
	}

	c1, c2 := net.Pipe()
	guard := NewReplayGuard(ClockSkew)

	type result struct {
		conn *Conn
		pub  []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, pub, err := ServerHandshake(c2, serverKey, guard, allow)
		done <- result{conn, pub, err}
	}()

	client, err = ClientHandshake(c1, clientKey, serverKey.Public)
	res := <-done
	if err != nil {
		t.Fatalf("хендшейк клиента: %v", err)
	}
	if res.err != nil {
		t.Fatalf("хендшейк сервера: %v", res.err)
	}

	if !bytes.Equal(res.pub, clientKey.Public) {
		t.Fatalf("сервер увидел чужой ключ клиента")
	}

	t.Cleanup(func() {
		_ = client.Close()
		_ = res.conn.Close()
	})
	return client, res.conn, clientKey.Public
}

func TestHandshakeAndTransfer(t *testing.T) {
	client, server, _ := handshakePair(t, AllowAll)

	// Клиент -> сервер.
	go func() { _, _ = client.Write([]byte("привет, сервер")) }()
	buf := make([]byte, 64)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatalf("чтение на сервере: %v", err)
	}
	if got := string(buf[:n]); got != "привет, сервер" {
		t.Fatalf("сервер получил %q", got)
	}

	// Сервер -> клиент.
	go func() { _, _ = server.Write([]byte("привет, клиент")) }()
	n, err = client.Read(buf)
	if err != nil {
		t.Fatalf("чтение на клиенте: %v", err)
	}
	if got := string(buf[:n]); got != "привет, клиент" {
		t.Fatalf("клиент получил %q", got)
	}
}

// TestTransferLargerThanFrame проверяет нарезку на кадры: полезная нагрузка
// заведомо больше MaxPlaintext, значит уедет несколькими кадрами и должна
// собраться обратно байт в байт.
func TestTransferLargerThanFrame(t *testing.T) {
	client, server, _ := handshakePair(t, AllowAll)

	payload := make([]byte, MaxPlaintext*2+1234)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("генерация данных: %v", err)
	}

	go func() {
		_, _ = client.Write(payload)
		_ = client.Close()
	}()

	got, err := io.ReadAll(server)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("чтение: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("получено %d байт вместо %d, содержимое не совпадает", len(got), len(payload))
	}
}

// TestWrongServerKey: клиент, который ошибся публичным ключом сервера, не
// должен установить соединение. Это же свойство защищает от MitM — подменить
// сервер, не зная его приватного ключа, нельзя.
func TestWrongServerKey(t *testing.T) {
	serverKey, _ := GenerateKeyPair()
	clientKey, _ := GenerateKeyPair()
	impostor, _ := GenerateKeyPair()

	c1, c2 := net.Pipe()
	guard := NewReplayGuard(ClockSkew)

	serverErr := make(chan error, 1)
	go func() {
		_, _, err := ServerHandshake(c2, serverKey, guard, AllowAll)
		_ = c2.Close()
		serverErr <- err
	}()

	if _, err := ClientHandshake(c1, clientKey, impostor.Public); err == nil {
		t.Fatal("клиент установил соединение с неверным ключом сервера")
	}
	if err := <-serverErr; err == nil {
		t.Fatal("сервер принял хендшейк, зашифрованный не на его ключ")
	}
}

// TestUnauthorizedClient: сервер обязан отказать клиенту, которого нет
// в списке разрешённых, даже если тот знает публичный ключ сервера.
func TestUnauthorizedClient(t *testing.T) {
	serverKey, _ := GenerateKeyPair()
	clientKey, _ := GenerateKeyPair()

	c1, c2 := net.Pipe()
	guard := NewReplayGuard(ClockSkew)

	serverErr := make(chan error, 1)
	go func() {
		_, _, err := ServerHandshake(c2, serverKey, guard, func([]byte) error { return errors.New("нет в списке") })
		_ = c2.Close()
		serverErr <- err
	}()

	_, _ = ClientHandshake(c1, clientKey, serverKey.Public)
	_ = c1.Close()

	if err := <-serverErr; !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ожидался ErrUnauthorized, получено: %v", err)
	}
}

func TestReplayGuard(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	guard := NewReplayGuard(2 * time.Minute)
	guard.now = func() time.Time { return now }

	msg := []byte("первое сообщение хендшейка")

	if err := guard.Check(msg, now); err != nil {
		t.Fatalf("первый проход должен пройти: %v", err)
	}
	if err := guard.Check(msg, now); !errors.Is(err, ErrReplay) {
		t.Fatalf("повтор должен быть отклонён, получено: %v", err)
	}

	if err := guard.Check([]byte("старое"), now.Add(-5*time.Minute)); !errors.Is(err, ErrClockSkew) {
		t.Fatalf("слишком старое время должно быть отклонено, получено: %v", err)
	}
	if err := guard.Check([]byte("из будущего"), now.Add(5*time.Minute)); !errors.Is(err, ErrClockSkew) {
		t.Fatalf("время из будущего должно быть отклонено, получено: %v", err)
	}

	// Запись, вышедшая за двойное окно, должна быть вычищена.
	now = now.Add(10 * time.Minute)
	if err := guard.Check([]byte("свежее"), now); err != nil {
		t.Fatalf("свежее сообщение должно пройти: %v", err)
	}
	if guard.Size() != 1 {
		t.Fatalf("в кэше %d записей, ожидалась 1 (старые не вычищены)", guard.Size())
	}
}

func TestAddressRoundTrip(t *testing.T) {
	cases := []Address{
		{Type: AtypIPv4, Host: "93.184.216.34", Port: 443},
		{Type: AtypIPv6, Host: "2606:2800:220:1:248:1893:25c8:1946", Port: 80},
		{Type: AtypDomain, Host: "example.com", Port: 8443},
	}

	for _, want := range cases {
		raw, err := want.Marshal()
		if err != nil {
			t.Fatalf("%v: сериализация: %v", want, err)
		}
		got, err := ReadAddress(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%v: разбор: %v", want, err)
		}
		if got.Type != want.Type || got.Host != want.Host || got.Port != want.Port {
			t.Fatalf("получено %+v, ожидалось %+v", got, want)
		}
	}
}

func TestAddressFromHostPort(t *testing.T) {
	cases := map[string]byte{
		"127.0.0.1":   AtypIPv4,
		"::1":         AtypIPv6,
		"example.com": AtypDomain,
	}
	for host, wantType := range cases {
		addr, err := AddressFromHostPort(host, 443)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if addr.Type != wantType {
			t.Fatalf("%s: тип 0x%02x, ожидался 0x%02x", host, addr.Type, wantType)
		}
	}
}

// TestTamperedFrame: изменение хотя бы одного байта в шифротексте должно
// приводить к ошибке, а не к тихой порче данных. Это и есть смысл
// аутентифицированного шифрования.
func TestTamperedFrame(t *testing.T) {
	serverKey, _ := GenerateKeyPair()
	clientKey, _ := GenerateKeyPair()

	c1, c2 := net.Pipe()
	guard := NewReplayGuard(ClockSkew)

	srv := make(chan *Conn, 1)
	go func() {
		conn, _, err := ServerHandshake(c2, serverKey, guard, AllowAll)
		if err != nil {
			srv <- nil
			return
		}
		srv <- conn
	}()

	client, err := ClientHandshake(c1, clientKey, serverKey.Public)
	if err != nil {
		t.Fatalf("хендшейк: %v", err)
	}
	server := <-srv
	if server == nil {
		t.Fatal("сервер не завершил хендшейк")
	}

	// Шифруем кадр руками и портим один байт по дороге.
	sealed, err := client.send.Encrypt(nil, nil, []byte("данные"))
	if err != nil {
		t.Fatalf("шифрование: %v", err)
	}
	sealed[0] ^= 0xFF

	go func() { _ = writeFrame(c1, sealed) }()

	buf := make([]byte, 64)
	if _, err := server.Read(buf); err == nil {
		t.Fatal("испорченный кадр был принят")
	}
}
