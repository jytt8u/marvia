package mobile

import (
	"context"
	"net"
	"sync/atomic"

	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Считаем полезные байты потоков, а не служебные пакеты и повторные передачи.
type trafficDialer struct {
	tunbridge.Dialer
	rx atomic.Int64
	tx atomic.Int64
}

func (d *trafficDialer) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	c, err := d.Dialer.DialTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	return &trafficConn{Conn: c, owner: d}, nil
}

func (d *trafficDialer) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	c, err := d.Dialer.DialDatagrams(ctx, target)
	if err != nil {
		return nil, err
	}
	return &trafficConn{Conn: c, owner: d}, nil
}

type trafficConn struct {
	net.Conn
	owner *trafficDialer
}

func (c *trafficConn) Read(b []byte) (int, error) {
	n, e := c.Conn.Read(b)
	c.owner.rx.Add(int64(n))
	return n, e
}
func (c *trafficConn) Write(b []byte) (int, error) {
	n, e := c.Conn.Write(b)
	c.owner.tx.Add(int64(n))
	return n, e
}

// Счётчик не должен менять жизненный цикл потока: после конца запроса
// обратная половина остаётся открытой, чтобы дождаться ответа.
func (c *trafficConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Conn.Close()
}

func (t *Tunnel) ReceivedBytes() int64 {
	if t.traffic == nil {
		return 0
	}
	return t.traffic.rx.Load()
}
func (t *Tunnel) SentBytes() int64 {
	if t.traffic == nil {
		return 0
	}
	return t.traffic.tx.Load()
}
