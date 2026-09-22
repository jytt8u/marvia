package client

import (
	"context"
	"net"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Backend — работающее подключение, какого бы вида оно ни было: своя
// подписка на VP1 или чужая на VLESS, Trojan и прочем.
//
// Телефон и окно на компьютере держат именно его, а не надзор VP1: им
// всё равно, какой протокол везёт байты, — нужны поток до цели, список нод,
// замер и выбор. Второй вид подключения поэтому не потребовал второго
// телефона и второго окна.
type Backend interface {
	DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error)
	DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error)

	// Node — через кого трафик идёт сейчас; Nodes — все, что есть в подписке.
	Node() Node
	Nodes() []Node

	// Measurement — последний замер текущей ноды; Measure — замер всех, секунды.
	Measurement() Measurement
	Measure(ctx context.Context) []Measurement
	// Ping — отклик текущей ноды, дёшево и часто.
	Ping(ctx context.Context) (time.Duration, error)

	// Select переводит на ноду по номеру; ноль — обратно к автовыбору.
	Select(ctx context.Context, id int64) error
	Selected() int64

	Subscription() Subscription
	Close() error
}

var _ Backend = (*Supervisor)(nil)
