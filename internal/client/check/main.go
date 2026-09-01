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
		line := fmt.Sprintf("  замер: %-26s %s %s", m.Node.Name, state, m.Latency.Round(time.Millisecond))
		if m.Fetch > 0 {
			line += fmt.Sprintf(", порцию отдала за %s (~%.0f Мбит/с)",
				m.Fetch.Round(time.Millisecond), m.Speed()*8/1e6)
		}
		fmt.Println(line)
	}

	ip, err := egressIP(ctx, dialer)
	if err != nil {
		return fmt.Errorf("через туннель: %w", err)
	}
	fmt.Printf("внешний мир видит адрес: %s\n", ip)

	// Два числа, и путать их нельзя. Первое — короткая порция от самой ноды,
	// по нему клиент выбирает ноду. Второе — настоящее скачивание с чужого
	// сервера, и это то, что человек называет «скорость VPN». Когда жалуются
	// на медленный VPN, виновато обычно второе, а чинят первое.
	if took, err := dialer.MeasureFetch(ctx, vp1.DefaultSpeedSample); err == nil {
		fmt.Printf("порция %d КБ от ноды: за %s (~%.0f Мбит/с вместе с кругом до неё)\n",
			vp1.DefaultSpeedSample>>10, took.Round(time.Millisecond),
			float64(vp1.DefaultSpeedSample)/took.Seconds()*8/1e6)
	} else {
		fmt.Printf("порция от ноды: не померили (%v)\n", err)
	}

	if speed, err := downloadSpeed(ctx, dialer); err == nil {
		fmt.Printf("скачивание сквозь туннель: %.0f Мбит/с\n", speed*8/1e6)
	} else {
		fmt.Printf("скачивание сквозь туннель: не вышло (%v)\n", err)
	}

	return nil
}

// downloadSpeed качает через туннель настоящий файл с чужого сервера.
//
// Это то число, которое человек называет «скорость VPN»: в нём и наш канал, и
// всё, что дальше ноды.
func downloadSpeed(ctx context.Context, dialer *client.Dialer) (float64, error) {
	const source = "https://speed.cloudflare.com/__down?bytes=50000000"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return 0, err
	}

	client := &http.Client{Transport: tunnelTransport(dialer), Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	start := time.Now()
	read, err := io.Copy(io.Discard, resp.Body)
	took := time.Since(start)
	if read == 0 {
		return 0, fmt.Errorf("ничего не скачалось: %w", err)
	}
	return float64(read) / took.Seconds(), nil
}

// tunnelTransport заставляет обычный HTTP-клиент ходить через туннель.
func tunnelTransport(dialer *client.Dialer) *http.Transport {
	return &http.Transport{
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
}

// egressIP спрашивает у внешней службы, каким адресом мы для неё выглядим.
func egressIP(ctx context.Context, dialer *client.Dialer) (string, error) {
	transport := tunnelTransport(dialer)

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
