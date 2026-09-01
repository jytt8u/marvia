package client

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/tunnel"
	"github.com/veilproject/veil/internal/vp1"
)

// Dialer держит соединение до одной ноды и раздаёт потоки до целей.
type Dialer struct {
	node Node
	pool *tunnel.Pool

	// sub — подписка, с которой выбрана эта нода: срок и остаток трафика.
	//
	// Нужна не дозвону, а тому, кто рисует окно: приложение обязано уметь
	// ответить «до какого числа и сколько осталось», не спрашивая продавца.
	// Второй раз ходить за подпиской ради этого незачем — она уже в руках.
	sub Subscription
}

// quicAttempt — сколько ждём дозвона по UDP, прежде чем уйти на TCP.
//
// Четыре секунды: этого хватает на честное рукопожатие даже на плохой
// мобильной сети, но человек не успевает решить, что приложение зависло.
const quicAttempt = 4 * time.Second

// Options — необязательные настройки дозвона.
//
// В бою оба поля пустые: доверяем системному хранилищу, как браузер. Нужны
// они для отладки — когда нода поднята с самоподписанным сертификатом.
type Options struct {
	// RootCAs — своё хранилище доверия вместо системного.
	RootCAs *x509.CertPool

	// InsecureSkipVerify отключает проверку сертификата. Только для отладки:
	// с ним любой, кто вклинится в соединение, становится нашей нодой.
	InsecureSkipVerify bool
}

// NewDialer готовит дозвон до ноды. Соединение поднимается лениво, при первом
// обращении: на телефоне включение VPN не должно упираться в сеть.
func NewDialer(node Node, key vp1.KeyPair, opts Options) (*Dialer, error) {
	if node.Address == "" {
		return nil, errors.New("у ноды нет адреса")
	}
	serverPub, err := vp1.DecodeKey(node.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("публичный ключ ноды %s: %w", node.Name, err)
	}

	name := node.SNI
	if name == "" {
		host, _, splitErr := net.SplitHostPort(node.Address)
		if splitErr != nil {
			return nil, fmt.Errorf("адрес ноды %q: %w", node.Address, splitErr)
		}
		name = host
	}

	dial, err := transportDialer(node, name, opts)
	if err != nil {
		return nil, err
	}

	pool := tunnel.NewPool(func(ctx context.Context) (net.Conn, error) {
		raw, err := dial(ctx)
		if err != nil {
			return nil, err
		}
		conn, err := vp1.ClientHandshake(raw, key, serverPub)
		if err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	}, 0, 0)

	return &Dialer{node: node, pool: pool}, nil
}

// transportDialer выбирает внешний слой под то, как настроена нода.
func transportDialer(node Node, serverName string, opts Options) (func(context.Context) (net.Conn, error), error) {
	// QUIC пробуем первым и откатываемся на TCP, если не вышло.
	//
	// Только для обычного TLS: под REALITY нода не может держать QUIC (там
	// нет своего сертификата), а за CDN адрес принадлежит не ноде, и UDP до
	// неё не дойдёт.
	if node.QUIC && node.Transport() == TransportTLS {
		tcp, err := tcpDialer(node, serverName, opts)
		if err != nil {
			return nil, err
		}
		return quicFirst(node, serverName, opts, tcp), nil
	}

	return tcpDialer(node, serverName, opts)
}

// quicFirst пробует UDP, а потом навсегда переходит на TCP.
//
// «Навсегда» важнее, чем кажется. UDP режут не по одному пакету, а целой
// сетью: у оператора, в офисе, в белом списке. Там первая попытка не просто
// не удастся — она будет молча висеть до тайм-аута, и так на каждом новом
// соединении. Один раз выяснили, что дороги нет, и больше туда не ходим.
func quicFirst(node Node, serverName string, opts Options, tcp func(context.Context) (net.Conn, error)) func(context.Context) (net.Conn, error) {
	var udpDead atomic.Bool

	return func(ctx context.Context) (net.Conn, error) {
		if udpDead.Load() {
			return tcp(ctx)
		}

		quicCtx, cancel := context.WithTimeout(ctx, quicAttempt)
		defer cancel()

		conn, err := transport.DialQUIC(quicCtx, node.Address, transport.QUICDialConfig{
			TLS: transport.ClientConfig{
				ServerName:         serverName,
				RootCAs:            opts.RootCAs,
				InsecureSkipVerify: opts.InsecureSkipVerify,
			},
		})
		if err == nil {
			return conn, nil
		}

		// Отмена самим человеком — не приговор дороге: он просто нажал
		// «отключиться», и в следующий раз UDP надо пробовать снова.
		if ctx.Err() == nil {
			udpDead.Store(true)
		}
		return tcp(ctx)
	}
}

// tcpDialer собирает дозвон по TCP — тот, что был до появления QUIC.
func tcpDialer(node Node, serverName string, opts Options) (func(context.Context) (net.Conn, error), error) {
	tlsCfg := transport.ClientConfig{
		ServerName:         serverName,
		RootCAs:            opts.RootCAs,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	}

	switch node.Transport() {
	case TransportWS:
		cfg := transport.WSDialConfig{Host: serverName, Path: node.WSPath, TLS: tlsCfg}
		return func(ctx context.Context) (net.Conn, error) {
			return transport.DialWS(ctx, node.Address, cfg)
		}, nil

	case TransportReality:
		pub, err := vp1.DecodeKey(node.RealityPublicKey)
		if err != nil {
			return nil, fmt.Errorf("публичный ключ REALITY ноды %s: %w", node.Name, err)
		}
		cfg := transport.RealityDialConfig{
			ServerName: serverName,
			PublicKey:  pub,
			ShortID:    node.RealityShortID,
		}
		return func(ctx context.Context) (net.Conn, error) {
			return transport.DialReality(ctx, node.Address, cfg)
		}, nil

	default:
		return func(ctx context.Context) (net.Conn, error) {
			return transport.Dial(ctx, node.Address, tlsCfg)
		}, nil
	}
}

// DialTarget открывает поток до цели через туннель.
func (d *Dialer) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	return d.open(ctx, target, vp1.KindTCP)
}

// DialDatagrams открывает поток датаграмм до цели.
//
// Отдельный метод, а не флаг в DialTarget: вернувшееся соединение живёт по
// другим правилам — границы датаграмм в нём сохраняются, и обращаться с ним
// как с потоком байтов нельзя.
func (d *Dialer) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	stream, err := d.open(ctx, target, vp1.KindUDP)
	if err != nil {
		return nil, err
	}
	return datagramConn{stream}, nil
}

func (d *Dialer) open(ctx context.Context, target vp1.Address, kind vp1.Kind) (net.Conn, error) {
	stream, err := d.pool.Open(ctx)
	if err != nil {
		return nil, err
	}

	if err := vp1.WriteRequestOf(stream, target, kind); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("запрос на %s: %w", target, err)
	}

	status, err := vp1.ReadStatus(stream)
	if err != nil {
		_ = stream.Close()
		// Старая нода не знает про датаграммы: она видит незнакомый тип
		// адреса и закрывает поток, не ответив. Обрыв ровно здесь и ровно на
		// запросе датаграмм — это она, а не сеть.
		if kind == vp1.KindUDP && closedEarly(err) {
			return nil, vp1.ErrDatagramsUnsupported
		}
		return nil, fmt.Errorf("ответ ноды по %s: %w", target, err)
	}
	if status != vp1.StatusOK {
		_ = stream.Close()
		return nil, fmt.Errorf("нода отказала по %s: %s", target, vp1.StatusText(status))
	}
	return stream, nil
}

// datagramConn сохраняет границы датаграмм поверх потока.
//
// Обёртка нужна, чтобы вызывающий работал с привычным net.Conn: один Read —
// одна датаграмма, один Write — одна датаграмма. Без неё границы пришлось бы
// вручную соблюдать в каждом месте, где такой поток используется.
type datagramConn struct{ net.Conn }

func (c datagramConn) Read(p []byte) (int, error) {
	return vp1.ReadDatagram(c.Conn, p)
}

func (c datagramConn) Write(p []byte) (int, error) {
	if err := vp1.WriteDatagram(c.Conn, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Node возвращает ноду, к которой подключён этот дозвон.
func (d *Dialer) Node() Node { return d.node }

// Close закрывает все соединения до ноды.
func (d *Dialer) Close() error { return d.pool.Close() }

// Subscription — срок и квота, с которыми выбрана нода.
func (d *Dialer) Subscription() Subscription { return d.sub }

// withSubscription запоминает подписку в дозвоне.
func (d *Dialer) withSubscription(s Subscription) *Dialer {
	if d != nil {
		d.sub = s
	}
	return d
}

// closedEarly отличает «собеседник закрыл поток, не ответив» от прочих бед.
func closedEarly(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.ErrClosedPipe)
}
