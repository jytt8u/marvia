package tunbridge_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/tunbridge"
	"github.com/veilproject/veil/internal/vp1"
)

// dnsMessage собирает похожее на запрос сообщение нужной длины.
func dnsMessage(id uint16, size int) []byte {
	msg := make([]byte, size)
	binary.BigEndian.PutUint16(msg[:2], id)
	return msg
}

// TestExchangeOverTCP — перевод запроса имени с UDP на TCP.
//
// Приложения на телефоне спрашивают имена по UDP, а наш протокол UDP пока не
// проксирует. Оставить запросы идти мимо туннеля нельзя: список посещённых
// сайтов — ровно то, что цензор хочет узнать, и добыть его из открытых
// запросов имён проще, чем расшифровывать трафик.
func TestExchangeOverTCP(t *testing.T) {
	query := dnsMessage(0x1234, 30)
	answer := dnsMessage(0x1234, 64)

	local, remote := net.Pipe()
	defer local.Close()

	go func() {
		defer remote.Close()

		var head [2]byte
		if _, err := io.ReadFull(remote, head[:]); err != nil {
			return
		}
		got := make([]byte, binary.BigEndian.Uint16(head[:]))
		if _, err := io.ReadFull(remote, got); err != nil {
			return
		}

		framed := make([]byte, 2+len(answer))
		binary.BigEndian.PutUint16(framed[:2], uint16(len(answer)))
		copy(framed[2:], answer)
		_, _ = remote.Write(framed)
	}()

	got, err := tunbridge.ExchangeOverTCP(local, query)
	if err != nil {
		t.Fatalf("обмен: %v", err)
	}
	if len(got) != len(answer) {
		t.Fatalf("ответ %d байт, ожидалось %d", len(got), len(answer))
	}
	if binary.BigEndian.Uint16(got[:2]) != 0x1234 {
		t.Fatal("идентификатор запроса не совпал с ответом")
	}
}

func TestExchangeOverTCPRejectsShortQuery(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()

	if _, err := tunbridge.ExchangeOverTCP(local, []byte{1, 2, 3}); !errors.Is(err, tunbridge.ErrShortDNSMessage) {
		t.Fatalf("ожидался ErrShortDNSMessage, получено: %v", err)
	}
}

// TestExchangeOverTCPRejectsShortAnswer: обрезанный ответ нельзя отдавать
// приложению как настоящий — оно решит, что имени не существует.
func TestExchangeOverTCPRejectsShortAnswer(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()

	go func() {
		defer remote.Close()

		var head [2]byte
		if _, err := io.ReadFull(remote, head[:]); err != nil {
			return
		}
		body := make([]byte, binary.BigEndian.Uint16(head[:]))
		if _, err := io.ReadFull(remote, body); err != nil {
			return
		}
		// Заявляем ответ короче обязательного заголовка.
		_, _ = remote.Write([]byte{0x00, 0x04, 1, 2, 3, 4})
	}()

	if _, err := tunbridge.ExchangeOverTCP(local, dnsMessage(1, 20)); !errors.Is(err, tunbridge.ErrShortDNSMessage) {
		t.Fatalf("ожидался ErrShortDNSMessage, получено: %v", err)
	}
}

// stubDialer изображает туннель, никуда не ходя.
type stubDialer struct{}

func (stubDialer) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("некуда")
}

func (stubDialer) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("некуда")
}

// TestBridgeLifecycle: мост поднимается на потоке вместо настоящего
// интерфейса и закрывается без зависаний. На телефоне включение и выключение
// VPN — самая частая операция, и подвисание здесь заметят все.
func TestBridgeLifecycle(t *testing.T) {
	device, peer := net.Pipe()
	defer peer.Close()

	bridge, err := tunbridge.Start(tunbridge.Config{
		Device: device,
		MTU:    tunbridge.DefaultMTU,
		Dialer: stubDialer{},
		DNS:    "1.1.1.1:53",
	})
	if err != nil {
		t.Fatalf("запуск моста: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- bridge.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("остановка моста: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("мост не остановился за десять секунд")
	}
}

func TestBridgeRejectsIncompleteConfig(t *testing.T) {
	device, peer := net.Pipe()
	defer device.Close()
	defer peer.Close()

	cases := map[string]tunbridge.Config{
		"без дозвона":     {Device: device, DNS: "1.1.1.1:53"},
		"без адреса имён": {Device: device, Dialer: stubDialer{}},
		"без интерфейса":  {Dialer: stubDialer{}, DNS: "1.1.1.1:53"},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tunbridge.Start(cfg); err == nil {
				t.Fatal("неполная настройка принята молча")
			}
		})
	}
}
