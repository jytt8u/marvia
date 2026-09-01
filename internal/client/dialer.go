package client

import (
	"bytes"
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

// clockGrain — во что упирается точность замера.
//
// Windows меряет время грубо. Всё, что быстрее одной миллисекунды, для нас
// неотличимо, и притворяться, что мы видим микросекунды, значит выдавать шум
// за измерение.
const clockGrain = time.Millisecond

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
	return d.open(ctx, target, vp1.KindTCP, nil)
}

// DialDatagrams открывает поток датаграмм до цели.
//
// Отдельный метод, а не флаг в DialTarget: вернувшееся соединение живёт по
// другим правилам — границы датаграмм в нём сохраняются, и обращаться с ним
// как с потоком байтов нельзя.
func (d *Dialer) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	stream, err := d.open(ctx, target, vp1.KindUDP, nil)
	if err != nil {
		return nil, err
	}
	return vp1.Datagrams(stream), nil
}

// MeasureSpeed узнаёт, с какой скоростью нода отдаёт данные.
//
// Возвращает байты в секунду. Цель в запросе не участвует — наружу нода не
// пойдёт, отдаст своё, — но адрес в протоколе обязателен, поэтому шлём
// заведомо пустой.
func (d *Dialer) MeasureSpeed(ctx context.Context, size int) (float64, error) {
	// Размер уходит вместе с запросом, до ответа ноды. Отправлять его после
	// статуса нельзя: обе стороны встанут ждать друг друга.
	var ask bytes.Buffer
	if err := vp1.RequestSample(&ask, size); err != nil {
		return 0, err
	}

	stream, err := d.open(ctx, vp1.Address{Type: vp1.AtypIPv4, Host: "0.0.0.0", Port: 0}, vp1.KindProbe, ask.Bytes())
	if err != nil {
		return 0, err
	}
	defer stream.Close()

	granted, err := vp1.ReadGrant(stream)
	if err != nil {
		return 0, err
	}

	// Отсчёт с первого байта, а не с запроса: круг до ноды и обратно — это
	// задержка, её мы уже померили отдельно. Здесь нужна скорость.
	var first [1]byte
	if _, err := io.ReadFull(stream, first[:]); err != nil {
		return 0, fmt.Errorf("замер не начался: %w", err)
	}

	start := time.Now()
	read, err := io.CopyN(io.Discard, stream, int64(granted-1))
	took := time.Since(start)
	if err != nil {
		return 0, fmt.Errorf("замер оборван: %w", err)
	}
	if read <= 0 {
		return 0, errors.New("замер пустой")
	}

	// Часы у Windows грубые, а замер маленький: на быстром канале он
	// укладывается в один тик, и деление даёт бесконечность. Считаем по нижней
	// границе — точное число не нужно, нужно сравнить ноды, а такая нода
	// выиграет при любом округлении.
	if took < clockGrain {
		took = clockGrain
	}
	return float64(read) / took.Seconds(), nil
}

// Granted — сколько байт нода согласилась отдать на последний замер.
//
// Нужно проверке потолка: клиент может попросить сколько угодно, а решает нода.
func (d *Dialer) Granted(ctx context.Context, size int) (int, error) {
	var ask bytes.Buffer
	if err := vp1.RequestSample(&ask, size); err != nil {
		return 0, err
	}

	stream, err := d.open(ctx, vp1.Address{Type: vp1.AtypIPv4, Host: "0.0.0.0", Port: 0}, vp1.KindProbe, ask.Bytes())
	if err != nil {
		return 0, err
	}
	defer stream.Close()

	return vp1.ReadGrant(stream)
}

// open открывает поток нужного вида.
//
// Хвост дописывается сразу за запросом, до чтения статуса: если чего-то ждёт
// нода, а мы уже сели ждать её ответа, встанут обе стороны.
func (d *Dialer) open(ctx context.Context, target vp1.Address, kind vp1.Kind, tail []byte) (net.Conn, error) {
	stream, err := d.pool.Open(ctx)
	if err != nil {
		return nil, err
	}

	if err := vp1.WriteRequestOf(stream, target, kind); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("запрос на %s: %w", target, err)
	}
	if len(tail) > 0 {
		if _, err := stream.Write(tail); err != nil {
			_ = stream.Close()
			return nil, fmt.Errorf("запрос на %s: %w", target, err)
		}
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
