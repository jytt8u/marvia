// Package egress — куда ноде можно дозваниваться по просьбе клиента.
//
// Клиент называет цель сам, и без проверки нода — это дверь во внутреннюю
// сеть хостера: 127.0.0.1 с тем, что слушает сама машина, 10.0.0.0/8 соседей
// в облаке, 169.254.169.254 с метаданными и токенами облачного аккаунта.
// Покупатель — не хозяин ноды, и заглядывать ей за спину ему нечего.
//
// Проверяется адрес после разрешения имени, а не до: домен, указывающий на
// 127.0.0.1, обходит любую проверку по строке.
package egress

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// ErrForbidden — цель внутри хоста или его сети; туда нода не ходит.
var ErrForbidden = errors.New("цель внутри сети ноды")

// ErrPort — порт, на который нода не ходит ни к кому.
var ErrPort = errors.New("порт закрыт на ноде")

// ForbiddenPort — порты, закрытые наружу для всех.
//
// 25 — SMTP без аутентификации: единственное, зачем он нужен через VPN, —
// спам, и первая же жалоба уводит ноду в бан у хостера. Почтовые клиенты
// ходят по 465 и 587 с логином, их это не трогает.
func ForbiddenPort(port int) bool {
	return port == 25
}

// Forbidden говорит, закрыт ли адрес для исходящих соединений.
//
// Закрыто всё, что не маршрутизируется в интернет: петля, приватные сети,
// link-local (там живут метаданные облаков), multicast, неуказанный адрес,
// а также IPv4, завёрнутый в IPv6, — по нему проверяется вложенный адрес.
func Forbidden(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		// 100.64.0.0/10 — CGNAT и внутренние сети облаков; 192.0.0.0/24 — IETF.
		cgnat.Contains(ip) || ietf.Contains(ip) ||
		// 0.0.0.0/8 ядро Linux трактует как «этот хост».
		zeroNet.Contains(ip)
}

var (
	cgnat   = netip.MustParsePrefix("100.64.0.0/10")
	ietf    = netip.MustParsePrefix("192.0.0.0/24")
	zeroNet = netip.MustParsePrefix("0.0.0.0/8")
)

// Dial открывает соединение с целью, отказывая закрытым адресам.
//
// Имя разрешается здесь же, и каждый адрес из ответа проверяется: DNS может
// вернуть публичный и приватный адрес вперемешку (так делают некоторые
// CDN для внутренних имён), и пробовать «первый попавшийся» нельзя.
func Dial(ctx context.Context, network, hostport string, timeout time.Duration) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return nil, errors.New("некорректный порт")
	}
	if ForbiddenPort(port) {
		return nil, ErrPort
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		addrs = found
	}
	// Имя в текст ошибки не попадает: нода пишет ошибки в журнал, а имя —
	// это «куда ходил человек».
	if len(addrs) == 0 {
		return nil, errors.New("имя не разрешилось")
	}
	for _, ip := range addrs {
		if Forbidden(ip) {
			return nil, ErrForbidden
		}
	}

	d := net.Dialer{Timeout: timeout}
	var last error
	for _, ip := range addrs {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), portText))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}
