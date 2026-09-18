package tunbridge

import (
	"context"
	"net"
	"sync/atomic"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Metered оборачивает дозвон счётчиками байтов.
//
// Счёт нужен обоим клиентам: окну на компьютере — чтобы показать скорость и
// график за час, телефону — чтобы вести расход по дням. Считать внутри самого
// моста было бы проще, но тогда счётчики достались бы и тем, кому они не
// нужны, вместе с атомарной операцией на каждый прочитанный кусок.
//
// Считаем на уровне потока до цели, а не соединения с нодой: так в цифру не
// попадают ни добивка, ни служебные кадры, ни переподключения. Человеку
// показывается то, что он скачал, а не то, за что владелец ноды платит
// хостеру, — на ноде это считается отдельно и по-другому.
func Metered(inner Dialer, up, down *atomic.Int64) Dialer {
	return &meteredDialer{inner: inner, up: up, down: down}
}

type meteredDialer struct {
	inner    Dialer
	up, down *atomic.Int64
}

func (d *meteredDialer) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	conn, err := d.inner.DialTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	return &meteredConn{Conn: conn, up: d.up, down: d.down}, nil
}

func (d *meteredDialer) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	conn, err := d.inner.DialDatagrams(ctx, target)
	if err != nil {
		return nil, err
	}
	// Обёртка та же: она считает байты, не заглядывая внутрь, а границы
	// датаграмм соблюдает нижележащее соединение.
	return &meteredConn{Conn: conn, up: d.up, down: d.down}, nil
}

// meteredConn считает в обе стороны, названные со стороны человека: up — то,
// что ушло от него, down — то, что пришло к нему.
type meteredConn struct {
	net.Conn
	up, down *atomic.Int64
}

func (c *meteredConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.down.Add(int64(n))
	return n, err
}

func (c *meteredConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.up.Add(int64(n))
	return n, err
}

// CloseWrite пропускает половинное закрытие сквозь обёртку.
//
// Без него relay видит обёртку как соединение, не умеющее закрывать одну
// сторону, и вместо CloseWrite делает Close. Для HTTP это значит: запрос
// дописан, соединение закрыто, ответ потерян — «страница загрузилась
// наполовину». Обёртка не должна менять жизненный цикл потока.
func (c *meteredConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Conn.Close()
}
