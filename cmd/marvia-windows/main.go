//go:build windows

// Команда marvia-windows — клиент для Windows с системным туннелем.
//
// В отличие от marvia-client, который поднимает SOCKS5 и требует настроить
// каждое приложение отдельно, этот забирает весь трафик компьютера целиком —
// так же, как приложение на телефоне. Ядро под ними одно и то же, включая
// выбор ноды по замерам с самой машины.
//
// Показывает окно. Внутри окна обычная страница, которую отдаёт сама
// программа: так не нужен ни сторонний набор виджетов, ни компилятор C, а
// выглядит она одинаково на любой машине.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/jytt8u/marvia/internal/tunbridge"
)

const (
	// adapterName — как интерфейс будет называться в настройках сети.
	adapterName = "Marvia"

	// tunnelAddress — адрес компьютера внутри туннеля. Наружу не выходит.
	tunnelAddress = "10.19.84.2/32"

	// defaultDNS — куда уходят перехваченные запросы имён.
	defaultDNS = "1.1.1.1:53"
)

func main() {
	dns := flag.String("dns", defaultDNS, "адрес для запросов имён внутри туннеля")
	mtu := flag.Uint("mtu", tunbridge.DefaultMTU, "MTU интерфейса")
	noElevate := flag.Bool("no-elevate", false, "не просить прав администратора (окно откроется, туннель не поднимется)")
	urlFile := flag.String("url-file", "", "записать адрес интерфейса в файл (для отладки и поддержки)")
	flag.Parse()

	// Без прав администратора Windows не даст ни создать адаптер, ни трогать
	// маршруты. Просим их сразу и обычным путём — через то самое окно, которое
	// человек видит при установке любой программы. Запускать что-то из
	// командной строки от администратора он не обязан.
	if !elevated() && !*noElevate {
		if err := relaunchElevated(); err != nil {
			alert("Marvia", "Не получилось запросить права администратора:\n"+err.Error()+
				"\n\nБез них Windows не даст создать сетевой адаптер.")
			os.Exit(1)
		}
		return
	}

	if err := run(*dns, uint32(*mtu), *urlFile); err != nil {
		alert("Marvia", err.Error())
		os.Exit(1)
	}
}

func run(dns string, mtu uint32, urlFile string) error {
	log := newJournal()
	ctl := NewController(dns, mtu, log)

	if !elevated() {
		log.add("прав администратора нет: туннель поднять не выйдет")
	}

	// Схему регистрируем на каждом запуске, а ссылку из аргументов принимаем
	// сразу: покупатель нажал её в телеграме, и это первое, что он делает
	// после оплаты. Подробности в scheme_windows.go.
	if link := setupScheme(log); link != "" {
		if err := ctl.SetAccount(link); err != nil {
			log.add("ссылка из телеграма не подошла: %v", err)
		} else {
			log.add("ключ доступа взят из ссылки")
		}
	}

	log.add("готов к работе")

	url, server, err := serveUI(ctl, log)
	if err != nil {
		return err
	}
	defer func() {
		_ = server.Close()
	}()

	// Адрес кладём в журнал, а не печатаем: печатать некуда, программа
	// оконная. В журнале он пригодится, если окно откроется пустым.
	log.add("интерфейс на %s", url)
	if urlFile != "" {
		_ = os.WriteFile(urlFile, []byte(url), 0o600)
	}

	// Что бы ни случилось дальше — маршруты снимутся. Человек закроет окно
	// крестиком, а не кнопкой, и это нормально: убирать за собой должна
	// программа, а не он.
	defer ctl.Disconnect()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctl.Disconnect()
		os.Exit(0)
	}()

	return showWindow(url)
}

// elevated сообщает, запущены ли мы с правами администратора.
func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// relaunchElevated перезапускает программу с запросом прав.
//
// Windows не умеет повышать права работающему процессу — можно только
// запустить новый. Поэтому мы просим права, запускаем себя заново и тихо
// выходим: человек видит привычное окно согласия, а не совет открыть
// PowerShell.
func relaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := strings.Join(os.Args[1:], " ")

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(exe))

	var params *uint16
	if args != "" {
		params, _ = syscall.UTF16PtrFromString(args)
	}

	return windows.ShellExecute(0, verb, file, params, dir, windows.SW_NORMAL)
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
