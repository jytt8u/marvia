// Команда check — проверка переезда между нодами на живом.
//
// Это не тест: она ходит в настоящую панель и настоящие ноды. Запускается
// руками, когда надо убедиться, что обещание платформы выполняется — ноду
// выключили, а покупатель этого не заметил.
//
//	go run ./internal/client/check "marvia://…"
//
// Печатает, какую ноду выбрал клиент и какой адрес видит внешний мир через
// туннель. Второе важнее первого: выбрать ноду мало, через неё должен пойти
// трафик.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/vp1"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "нужна ссылка доступа")
		os.Exit(1)
	}

	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(link string) error {
	account, err := client.ParseAccountLink(link)
	if err != nil {
		return err
	}
	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	start := time.Now()
	dialer, measurements, err := client.Connect(ctx, client.ConnectConfig{
		Account: account,
		Key:     key,
		// Кэш намеренно не берём: проверяем выбор, а не память о нём.
		Log: func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) },
	})
	if err != nil {
		return fmt.Errorf("подключение: %w", err)
	}
	defer dialer.Close()

	node := dialer.Node()
	fmt.Printf("выбрана нода: %s (%s), %s, за %s\n",
		node.Name, node.Country, node.Address, time.Since(start).Round(time.Millisecond))

	for _, m := range measurements {
		state := "ответила"
		if !m.OK() {
			state = "молчит"
		}
		fmt.Printf("  замер: %-26s %s %s\n", m.Node.Name, state, m.Latency.Round(time.Millisecond))
	}

	ip, err := egressIP(ctx, dialer)
	if err != nil {
		return fmt.Errorf("через туннель: %w", err)
	}
	fmt.Printf("внешний мир видит адрес: %s\n", ip)

	return nil
}

// egressIP спрашивает у внешней службы, каким адресом мы для неё выглядим.
func egressIP(ctx context.Context, dialer *client.Dialer) (string, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				return nil, err
			}
			target, err := vp1.AddressFromHostPort(host, uint16(port))
			if err != nil {
				return nil, err
			}
			return dialer.DialTarget(ctx, target)
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}

	resp, err := (&http.Client{Transport: transport, Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	return string(body), err
}
