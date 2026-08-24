// Package tunbridge превращает пакеты сетевого интерфейса в соединения
// туннеля.
//
// Это то, без чего на телефоне не бывает системного VPN. Android даёт
// приложению файловый дескриптор интерфейса TUN, в который операционная
// система пишет сырые IP-пакеты всех приложений. Пакеты — не соединения:
// чтобы отправить их в наш туннель, кто-то должен собрать из них TCP-потоки,
// то есть выполнить работу сетевого стека в пространстве пользователя.
//
// Свой стек TCP мы не пишем по той же причине, по которой не писали свой
// TLS: цена ошибки высокая, а выигрыша нет. Берём проверенный gvisor через
// обёртку tun2socks и подключаем к нему свой обработчик.
package tunbridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"github.com/xjasonlyu/tun2socks/v2/core/device/iobased"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/veilproject/veil/internal/relay"
	"github.com/veilproject/veil/internal/vp1"
)

const (
	// DefaultMTU — размер, который выставляет большинство клиентов.
	// Меньше обычного из-за накладных расходов туннеля.
	DefaultMTU = 1500

	// dialTimeout — сколько ждём открытия потока до цели.
	dialTimeout = 15 * time.Second

	// dnsPort — порт, на котором мы перехватываем запросы имён.
	dnsPort = 53
)

// Dialer открывает поток до цели через туннель.
type Dialer interface {
	DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error)
}

// Config описывает мост.
type Config struct {
	// FD — дескриптор интерфейса от VpnService. Взаимоисключающ с Device.
	//
	// Дескриптор переходит мосту во владение: мост закроет его и при неудачном
	// запуске, и при остановке. Приложение поэтому отдаёт его через detachFd
	// и больше к нему не прикасается. Иначе выйдет двойное закрытие, а это на
	// Linux означает, что вторым разом мы закроем чужой сокет, успевший занять
	// освободившийся номер.
	FD int

	// Device — интерфейс в виде потока байтов. Нужен для проверок на
	// настольной машине, где никакого TUN нет.
	Device io.ReadWriter

	// MTU интерфейса.
	MTU uint32

	// Dialer — как добраться до цели.
	Dialer Dialer

	// DNS — куда отправлять перехваченные запросы имён, в виде host:port.
	//
	// Запросы приложений уходят в туннель по TCP, а не по UDP. Так сделано
	// не из любви к TCP: наш протокол UDP пока не проксирует, а без работающих
	// имён телефон бесполезен. Заодно это закрывает утечку — запрос имени,
	// ушедший мимо туннеля, выдаёт цензору весь список посещённых сайтов.
	DNS string

	// OnError вызывается на ошибках отдельных соединений. Может быть nil.
	OnError func(error)
}

// Bridge — работающий мост.
type Bridge struct {
	device stack.LinkEndpoint
	stack  *stack.Stack
}

// Start поднимает мост и начинает разбирать пакеты.
func Start(cfg Config) (*Bridge, error) {
	if cfg.Dialer == nil {
		return nil, errors.New("не задан способ дозвона до цели")
	}
	if cfg.DNS == "" {
		return nil, errors.New("не задан адрес для запросов имён")
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = DefaultMTU
	}

	dev, err := openDevice(cfg, mtu)
	if err != nil {
		// Вернуть дескриптор уже некому: на той стороне от него отказались
		// насовсем. Не закроем — интерфейс останется висеть до конца работы
		// приложения, и следующая попытка подключения упрётся в него же.
		CloseFD(cfg.FD)
		return nil, err
	}

	handler := &handler{
		dialer:  cfg.Dialer,
		dns:     cfg.DNS,
		onError: cfg.OnError,
	}

	st, err := core.CreateStack(&core.Config{
		LinkEndpoint:     dev,
		TransportHandler: handler,
	})
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("сетевой стек: %w", err)
	}

	return &Bridge{device: dev, stack: st}, nil
}

// openDevice отдаёт интерфейс канального уровня.
//
// Два источника не для красоты: по дескриптору — это то, что даёт VpnService
// на телефоне, а из потока байтов — единственный способ проверить мост на
// настольной машине, где никакого TUN нет.
func openDevice(cfg Config, mtu uint32) (stack.LinkEndpoint, error) {
	switch {
	case cfg.Device != nil:
		dev, err := iobased.New(cfg.Device, mtu, 0)
		if err != nil {
			return nil, fmt.Errorf("интерфейс из потока: %w", err)
		}
		return dev, nil
	case cfg.FD > 0:
		dev, err := fdbased.Open(strconv.Itoa(cfg.FD), mtu, 0)
		if err != nil {
			return nil, fmt.Errorf("интерфейс по дескриптору %d: %w", cfg.FD, err)
		}
		return dev, nil
	default:
		return nil, errors.New("не задан ни дескриптор интерфейса, ни поток")
	}
}

// Close останавливает мост.
func (b *Bridge) Close() error {
	if b.stack != nil {
		b.stack.Close()
	}
	if b.device != nil {
		b.device.Close()
	}
	return nil
}

// handler получает от стека готовые соединения.
type handler struct {
	dialer  Dialer
	dns     string
	onError func(error)
}

func (h *handler) fail(err error) {
	if h.onError != nil && err != nil {
		h.onError(err)
	}
}

// HandleTCP обслуживает соединение приложения.
func (h *handler) HandleTCP(conn adapter.TCPConn) {
	defer conn.Close()

	target := targetOf(conn.ID().LocalAddress.String(), conn.ID().LocalPort)

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	stream, err := h.dialer.DialTarget(ctx, target)
	cancel()
	if err != nil {
		h.fail(fmt.Errorf("поток до %s: %w", target, err))
		return
	}
	defer stream.Close()

	if err := relay.Bidirectional(conn, stream); err != nil {
		h.fail(fmt.Errorf("%s: обрыв: %w", target, err))
	}
}

// HandleUDP обслуживает датаграммы.
//
// Поддержан только порт имён. Всё остальное молча отбрасывается: наш протокол
// UDP пока не проксирует, и притворяться, что проксирует, хуже, чем честно
// не отвечать — приложение быстрее перейдёт на TCP.
func (h *handler) HandleUDP(conn adapter.UDPConn) {
	defer conn.Close()

	if conn.ID().LocalPort != dnsPort {
		return
	}
	if err := h.serveDNS(conn); err != nil {
		h.fail(fmt.Errorf("запрос имени: %w", err))
	}
}

// serveDNS переводит запрос имени с UDP на TCP и уносит его в туннель.
func (h *handler) serveDNS(conn adapter.UDPConn) error {
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))

	query := make([]byte, maxDNSMessage)
	n, addr, err := conn.ReadFrom(query)
	if err != nil {
		return fmt.Errorf("чтение запроса: %w", err)
	}

	target, err := vp1.AddressFromHostPort(hostOf(h.dns), portOf(h.dns))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	stream, err := h.dialer.DialTarget(ctx, target)
	cancel()
	if err != nil {
		return fmt.Errorf("поток до %s: %w", h.dns, err)
	}
	defer stream.Close()

	answer, err := ExchangeOverTCP(stream, query[:n])
	if err != nil {
		return err
	}
	if _, err := conn.WriteTo(answer, addr); err != nil {
		return fmt.Errorf("отправка ответа: %w", err)
	}
	return nil
}

// targetOf собирает адрес назначения из данных стека.
//
// LocalAddress и LocalPort здесь — это адрес, к которому обращалось
// приложение: с точки зрения стека, перехватившего пакет, он локальный.
func targetOf(host string, port uint16) vp1.Address {
	addr, err := vp1.AddressFromHostPort(host, port)
	if err != nil {
		// Стек не отдаёт кривых адресов, но на всякий случай не роняем мост.
		return vp1.Address{Type: vp1.AtypDomain, Host: host, Port: port}
	}
	return addr
}

func hostOf(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return hostPort
	}
	return host
}

func portOf(hostPort string) uint16 {
	_, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return dnsPort
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		return dnsPort
	}
	return uint16(value)
}
