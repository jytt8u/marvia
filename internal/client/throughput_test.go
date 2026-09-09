package client_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/vp1"
)

// Сколько протокол вывозит сам по себе.
//
// Меряется по петле: клиент, нода и цель на одной машине, сети между ними
// никакой. Остаётся только наша работа — шифрование, добивка кадров,
// мультиплексор, перекладывание байтов. Число отсюда — потолок, выше которого
// не будет никогда, и оно отвечает на единственный вопрос, который стоит
// задавать первым: мы упираемся в себя или в канал.
//
// Запускать руками:
//
//	go test ./internal/client -run TestThroughput -v

func TestThroughputOverLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("замер скорости в коротком прогоне не нужен")
	}

	const size = 200 << 20 // 200 МиБ

	// Цель, которая просто льёт нули.
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("цель: %v", err)
	}
	defer target.Close()

	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.CopyN(conn, zeros{}, size)
			}()
		}
	}()

	node := startTestNode(t)

	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	host, port := splitHostPort(t, target.Addr().String())
	addr, err := vp1.AddressFromHostPort(host, port)
	if err != nil {
		t.Fatalf("адрес цели: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	stream, err := dialer.DialTarget(ctx, addr)
	if err != nil {
		t.Fatalf("поток до цели: %v", err)
	}
	defer stream.Close()

	start := time.Now()
	read, err := io.Copy(io.Discard, io.LimitReader(stream, size))
	took := time.Since(start)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}

	mbps := float64(read) * 8 / took.Seconds() / 1e6
	fmt.Printf("\nсквозь протокол по петле: %.0f Мбит/с (%.0f МиБ за %s)\n\n",
		mbps, float64(read)/(1<<20), took.Round(time.Millisecond))

	// Порог намеренно низкий: он ловит не «стало на пять процентов медленнее»,
	// а обвал — когда кто-то поставил в горячий путь лишнюю аллокацию на кадр
	// или синхронный вызов. По петле всё, что ниже сотни, — это обвал.
	if mbps < 100 {
		t.Errorf("протокол сам по себе тянет всего %.0f Мбит/с — это не канал, это мы", mbps)
	}
}

// zeros — бесконечный источник нулей без аллокаций.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) { return len(p), nil }
