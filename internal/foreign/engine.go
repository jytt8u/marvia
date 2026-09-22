package foreign

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// Только исходящая сторона нужных протоколов и транспортов. Полный
	// дистрибутив Xray тянет серверы, API, статистику и три формата
	// настроек — это мегабайты в приложении, которые никогда не исполнятся.
	_ "github.com/xtls/xray-core/app/dispatcher"
	// Менеджер входящих нужен Xray даже без единого входящего: без него
	// экземпляр не создаётся («InboundConfig is not registered»).
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/hysteria"
	_ "github.com/xtls/xray-core/proxy/shadowsocks"
	_ "github.com/xtls/xray-core/proxy/trojan"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"
	_ "github.com/xtls/xray-core/proxy/vmess/outbound"
	_ "github.com/xtls/xray-core/proxy/wireguard"
	_ "github.com/xtls/xray-core/transport/internet/grpc"
	_ "github.com/xtls/xray-core/transport/internet/httpupgrade"
	_ "github.com/xtls/xray-core/transport/internet/hysteria"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/splithttp"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/tls"
	_ "github.com/xtls/xray-core/transport/internet/udp"
	_ "github.com/xtls/xray-core/transport/internet/websocket"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Engine — запущенный Xray с одной исходящей нодой.
//
// Отдаёт соединения тем же интерфейсом, что и дозвон VP1: мост телефона и
// окна на компьютере не знает, какой протокол под ним.
type Engine struct {
	link Link
	inst *core.Instance
}

// Start поднимает Xray для одной ноды. Входящих нет: соединения берутся
// вызовом Dial, а не через порт, который кто-то ещё на машине мог бы найти.
func Start(l Link) (*Engine, error) {
	out := map[string]any{"tag": "proxy"}
	for k, v := range l.Outbound {
		out[k] = v
	}
	cfg := map[string]any{
		// Журнал Xray молчит: он пишет адреса назначений, а клиент не ведёт
		// журнала «куда ходил человек» — ровно как нода.
		"log":       map[string]any{"loglevel": "none", "access": "none"},
		"outbounds": []any{out},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	pb, err := serial.LoadJSONConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("настройки %s: %w", l.Protocol, err)
	}
	// Создание экземпляра Xray переписывает общий на процесс системный
	// дозвон — без очереди четыре замера разом гонялись бы за ним (это
	// поймал детектор гонок). Сама работа движков идёт параллельно.
	creating.Lock()
	inst, err := core.New(pb)
	creating.Unlock()
	if err != nil {
		return nil, fmt.Errorf("запуск %s: %w", l.Protocol, err)
	}
	if err := inst.Start(); err != nil {
		return nil, fmt.Errorf("запуск %s: %w", l.Protocol, err)
	}
	return &Engine{link: l, inst: inst}, nil
}

// creating — очередь на создание экземпляров Xray.
var creating sync.Mutex

// Link — нода, на которую смотрит движок.
func (e *Engine) Link() Link { return e.link }

func destination(network xnet.Network, a vp1.Address) xnet.Destination {
	return xnet.Destination{Network: network, Address: xnet.ParseAddress(a.Host), Port: xnet.Port(a.Port)}
}

// DialTarget открывает поток до цели через ноду.
//
// Контекст здесь — срок дозвона, а не жизни соединения: мост отменяет его,
// как только поток открыт. Xray же привязывает к контексту всё соединение,
// и с отменой оно умирало сразу после открытия — туннель поднят, а сайты
// не идут. Поэтому Xray получает контекст без отмены (значения — те же), а
// живёт соединение до своего Close.
func (e *Engine) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return core.Dial(context.WithoutCancel(ctx), e.inst, destination(xnet.Network_TCP, target))
}

// DialDatagrams открывает поток датаграмм до цели: один Write — один пакет,
// один Read — один пакет, как у датаграмм VP1. Про контекст — см. DialTarget.
func (e *Engine) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return core.Dial(context.WithoutCancel(ctx), e.inst, destination(xnet.Network_UDP, target))
}

// Close останавливает движок и рвёт все его соединения.
func (e *Engine) Close() error { return e.inst.Close() }

// probeURL — что запрашиваем, чтобы померить ноду. Так же меряют Hiddify и
// v2rayNG: ответ пустой, сервер быстрый и есть везде. Переменная — ради
// тестов, где интернета нет.
var probeURL = "http://cp.cloudflare.com/generate_204"

// Probe меряет ноду настоящим запросом через неё: время до ответа.
//
// Не TCP до ноды и не пинг: заблокированная нода обычно принимает
// соединение и молча роняет пакеты после, и замер «порт открыт» показал бы
// её живой. Здесь — полный путь: протокол, маскировка, нода, выход в сеть.
func (e *Engine) Probe(ctx context.Context) (time.Duration, error) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				return nil, err
			}
			a, err := vp1.AddressFromHostPort(host, uint16(port))
			if err != nil {
				return nil, err
			}
			return e.DialTarget(ctx, a)
		},
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("нода не отвечает: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("нода ответила %s", resp.Status)
	}
	return time.Since(start), nil
}
