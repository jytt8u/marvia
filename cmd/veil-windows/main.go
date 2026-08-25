//go:build windows

// Команда veil-windows — клиент для Windows с системным туннелем.
//
// В отличие от veil-client, который поднимает SOCKS5 и требует настроить
// каждое приложение отдельно, этот забирает весь трафик компьютера целиком —
// так же, как приложение на телефоне. Ядро под ними одно и то же.
//
// Требует прав администратора: без них Windows не даёт ни создать сетевой
// адаптер, ни трогать таблицу маршрутов.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/tunbridge"
	"github.com/veilproject/veil/internal/vp1"
	"github.com/veilproject/veil/internal/wintun"
)

const (
	// adapterName — как интерфейс будет называться в настройках сети.
	adapterName = "Veil"

	// tunnelAddress — адрес компьютера внутри туннеля. Наружу не выходит.
	tunnelAddress = "10.19.84.2/32"

	// defaultDNS — куда уходят перехваченные запросы имён.
	defaultDNS = "1.1.1.1:53"
)

func main() {
	account := flag.String("account", "", "ссылка доступа veil-account:// (по умолчанию — из VEIL_ACCOUNT)")
	dns := flag.String("dns", defaultDNS, "адрес для запросов имён внутри туннеля")
	mtu := flag.Uint("mtu", tunbridge.DefaultMTU, "MTU интерфейса")
	flag.Parse()

	if err := run(*account, *dns, uint32(*mtu)); err != nil {
		fmt.Fprintf(os.Stderr, "\nошибка: %v\n", err)
		os.Exit(1)
	}
}

func run(accountLink, dns string, mtu uint32) error {
	if accountLink == "" {
		accountLink = os.Getenv("VEIL_ACCOUNT")
	}
	if strings.TrimSpace(accountLink) == "" {
		return errors.New("не задана ссылка доступа: укажи -account veil-account://… или переменную VEIL_ACCOUNT")
	}

	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("нужны права администратора: без них Windows не даст создать сетевой адаптер.\n" +
			"Запусти PowerShell от имени администратора и повтори команду")
	}

	address, err := netip.ParsePrefix(tunnelAddress)
	if err != nil {
		return err
	}
	dnsAddr, err := dnsAddress(dns)
	if err != nil {
		return err
	}

	account, err := client.ParseAccountLink(accountLink)
	if err != nil {
		return fmt.Errorf("ссылка доступа: %w", err)
	}
	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return fmt.Errorf("личный ключ: %w", err)
	}

	log.Printf("забираю список нод у %s", account.SubscriptionURL)
	sub, err := client.FetchSubscription(context.Background(), account.SubscriptionURL)
	if err != nil {
		return fmt.Errorf("список нод: %w", err)
	}

	log.Printf("замеряю ноды, их %d", len(sub.Nodes))
	dialer, measurements, err := client.SelectBest(context.Background(), sub.Nodes, key, client.Options{})

	// Отчёт уходит в любом случае, в том числе когда не подключилось никуда:
	// продавцу важнее всего узнать именно про такой случай.
	go func() {
		_ = client.SendReports(context.Background(), account.SubscriptionURL, client.ReportsFrom(measurements))
	}()

	if err != nil {
		return err
	}
	defer dialer.Close()

	node := dialer.Node()
	log.Printf("нода %s, адрес %s", node.Name, node.Address)

	// Адреса ноды выясняем до того, как заберём себе трафик: после этого
	// запросы имён пойдут в туннель, которого ещё нет.
	bypass, err := nodeAddresses(node.Address)
	if err != nil {
		return err
	}

	adapter, err := wintun.Open(wintun.Config{
		Name:    adapterName,
		MTU:     mtu,
		Address: address,
		DNS:     dnsAddr,
		Bypass:  bypass,
	})
	if err != nil {
		return err
	}
	defer func() {
		log.Printf("снимаю маршруты и убираю адаптер")
		if err := adapter.Close(); err != nil {
			log.Printf("при уборке: %v", err)
		}
	}()

	bridge, err := tunbridge.Start(tunbridge.Config{
		Endpoint: adapter.Endpoint(),
		MTU:      mtu,
		Dialer:   dialer,
		DNS:      dns,
		OnError:  func(err error) { log.Printf("соединение: %v", err) },
	})
	if err != nil {
		return fmt.Errorf("сетевой мост: %w", err)
	}
	defer bridge.Close()

	log.Printf("туннель поднят: весь трафик идёт через %s", node.Name)
	log.Printf("для выхода нажми Ctrl+C — маршруты снимутся сами")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Printf("выключаюсь")
	return nil
}

// nodeAddresses выясняет, какие адреса надо вывести мимо туннеля.
//
// Адрес ноды в подписке может быть и именем — так бывает, когда нода стоит за
// CDN. Тогда обходить надо все адреса, которые за этим именем стоят: подключат
// нас к любому из них.
func nodeAddresses(hostPort string) ([]netip.Addr, error) {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, fmt.Errorf("адрес ноды %q: %w", hostPort, err)
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("не удалось выяснить адрес ноды %s: %w", host, err)
	}

	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, addr.Unmap())
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("у ноды %s не нашлось ни одного адреса", host)
	}
	return out, nil
}

// dnsAddress достаёт адрес из записи вида host:port.
func dnsAddress(hostPort string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("адрес для запросов имён %q: нужен вид 1.1.1.1:53", hostPort)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("адрес для запросов имён %q: %w", host, err)
	}
	return addr, nil
}
