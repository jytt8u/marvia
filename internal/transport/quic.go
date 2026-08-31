package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// Транспорт поверх QUIC.
//
// Зачем он нужен. Всё остальное у нас едет по TCP, а TCP цензор умеет ломать
// не разглядывая: достаточно резать соединения по поведению — по времени
// установки, по паузам, по тому, сколько байт ушло до первого ответа. QUIC
// живёт в UDP, и такие приёмы к нему не применяются: там нет рукопожатия,
// которое можно оборвать посередине, и нет соединения, которое можно
// «подвесить».
//
// Чем платим. QUIC — это HTTP/3, и выглядеть он должен как HTTP/3 настоящего
// сайта. Отпечаток здесь свой: набор параметров транспорта, форма первых
// пакетов, порядок расширений. Библиотека quic-go даёт свой отпечаток, а не
// хромовский, и на этом ноду можно опознать — не по содержимому, а по тому,
// каким стеком она разговаривает. Это осознанный первый шаг: сперва рабочий
// транспорт, потом подделка отпечатка.
//
// REALITY здесь невозможен по устройству: он зеркалит TCP-рукопожатие чужого
// сайта, а тут рукопожатия в этом смысле нет.

// quicALPN — то, чем представляется соединение.
//
// h3 и только h3: любой другой ALPN поверх QUIC сам по себе аномалия, потому
// что QUIC в интернете сегодня — это почти исключительно HTTP/3.
var quicALPN = []string{"h3"}

// quicIdleTimeout — через сколько тишины соединение считается мёртвым.
//
// Полторы минуты: у мобильных операторов записи в NAT живут около двух минут,
// и молчать дольше — значит однажды писать в никуда.
const quicIdleTimeout = 90 * time.Second

// quicLinger — сколько соединение живёт после закрытия потока, чтобы
// собеседник успел дочитать последний ответ.
const quicLinger = 5 * time.Second

// QUICDialConfig — как клиент подключается по QUIC.
type QUICDialConfig struct {
	// TLS — то же, что у обычного транспорта: имя в SNI и проверка сертификата.
	TLS ClientConfig
}

// DialQUIC поднимает поток QUIC до ноды и отдаёт его как обычное соединение.
//
// Наружу отдаётся net.Conn, потому что всё, что выше, — VP1, мультиплексор,
// учёт — про транспорт знать не должно и не знает.
func DialQUIC(ctx context.Context, addr string, cfg QUICDialConfig) (net.Conn, error) {
	name := cfg.TLS.ServerName
	if name == "" {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("адрес ноды %q: %w", addr, err)
		}
		name = host
	}

	tlsCfg := &tls.Config{
		ServerName:         name,
		RootCAs:            cfg.TLS.RootCAs,
		InsecureSkipVerify: cfg.TLS.InsecureSkipVerify, //nolint:gosec // только для отладки, как и в TLS-транспорте
		NextProtos:         quicALPN,
		MinVersion:         tls.VersionTLS13,
	}

	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsCfg, &quic.Config{
		MaxIdleTimeout:  quicIdleTimeout,
		KeepAlivePeriod: quicIdleTimeout / 3,
	})
	if err != nil {
		return nil, fmt.Errorf("quic до %s: %w", addr, err)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "")
		return nil, fmt.Errorf("поток quic до %s: %w", addr, err)
	}

	return &quicConn{stream: stream, conn: conn}, nil
}

// QUICListener принимает соединения по QUIC.
type QUICListener struct {
	ln    *quic.Listener
	conns chan net.Conn
	done  chan struct{}
}

// ListenQUIC поднимает слушателя на UDP.
//
// Каждое соединение даёт один поток: мультиплексированием занимается слой
// выше, и делать это дважды — значит платить дважды за одно и то же.
func ListenQUIC(addr string, cert tls.Certificate) (*QUICListener, error) {
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   quicALPN,
		MinVersion:   tls.VersionTLS13,
	}

	ln, err := quic.ListenAddr(addr, tlsCfg, &quic.Config{
		MaxIdleTimeout:  quicIdleTimeout,
		KeepAlivePeriod: quicIdleTimeout / 3,
	})
	if err != nil {
		return nil, fmt.Errorf("quic на %s: %w", addr, err)
	}

	l := &QUICListener{
		ln:    ln,
		conns: make(chan net.Conn),
		done:  make(chan struct{}),
	}
	go l.accept()

	return l, nil
}

func (l *QUICListener) accept() {
	for {
		conn, err := l.ln.Accept(context.Background())
		if err != nil {
			close(l.conns)
			return
		}

		// Потоки принимаем в своей горутине: пока один гость медлит с первым
		// потоком, остальные не должны ждать в очереди.
		go func() {
			for {
				stream, err := conn.AcceptStream(context.Background())
				if err != nil {
					return
				}
				select {
				case l.conns <- &quicConn{stream: stream, conn: conn}:
				case <-l.done:
					_ = conn.CloseWithError(0, "")
					return
				}
			}
		}()
	}
}

// Accept отдаёт следующее соединение.
func (l *QUICListener) Accept() (net.Conn, error) {
	conn, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}

// Close закрывает слушателя.
func (l *QUICListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return l.ln.Close()
}

// Addr отдаёт адрес, на котором слушаем.
func (l *QUICListener) Addr() net.Addr { return l.ln.Addr() }

// quicConn — поток QUIC в обличье обычного соединения.
//
// Адреса берём у соединения, а чтение и запись — у потока: всё, что выше,
// работает с net.Conn и не должно знать, что под ним не TCP.
type quicConn struct {
	stream *quic.Stream
	conn   *quic.Conn
	once   sync.Once
}

func (c *quicConn) Read(b []byte) (int, error)  { return c.stream.Read(b) }
func (c *quicConn) Write(b []byte) (int, error) { return c.stream.Write(b) }

// Close закрывает поток и, погодя, само соединение.
//
// Погодя — потому что в QUIC закрытие соединения выбрасывает всё, что ещё не
// прочитано: собеседник получает CONNECTION_CLOSE вместо последнего ответа.
// В TCP такого нет, там FIN приходит после данных, и код выше рассчитан
// именно на это. Закрыть сразу — значит терять последний ответ на каждом
// разговоре, где ответили и сразу попрощались.
//
// Держать соединение до тайм-аута простоя тоже нельзя: на одно соединение у
// нас один поток, и полторы минуты после конца разговора — это запись в NAT
// оператора, которая никому уже не нужна.
func (c *quicConn) Close() error {
	err := c.stream.Close()

	// Ждём именно по часам, а не по контексту потока: контекст отменяется в
	// тот же миг, когда мы закрыли свою половину, и толку от него здесь нет —
	// собеседник в этот момент ещё не дочитал.
	c.once.Do(func() {
		go func() {
			time.Sleep(quicLinger)
			_ = c.conn.CloseWithError(0, "")
		}()
	})

	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (c *quicConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *quicConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *quicConn) SetDeadline(t time.Time) error      { return c.stream.SetDeadline(t) }
func (c *quicConn) SetReadDeadline(t time.Time) error  { return c.stream.SetReadDeadline(t) }
func (c *quicConn) SetWriteDeadline(t time.Time) error { return c.stream.SetWriteDeadline(t) }
