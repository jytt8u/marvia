// Package netpath привязывает только сокеты до VPN-ноды к внешней сети.
// Исключать IP ноды из маршрутов всей системы нельзя: за CDN на том же
// адресе живут посторонние сайты, которые должны оставаться в туннеле.
package netpath

import (
	"context"
	"net"
	"sync/atomic"
	"syscall"
)

var enabled atomic.Bool

// Enable вызывается Windows-клиентом до первого соединения с нодой.
// Настройка действует и на последующие переподключения и замеры.
func Enable() { enabled.Store(true) }

func Enabled() bool { return enabled.Load() }

func Dialer() *net.Dialer { return &net.Dialer{Control: controlSocket} }

func ListenPacket(ctx context.Context, network, address, remote string) (net.PacketConn, error) {
	lc := net.ListenConfig{Control: func(network, _ string, raw syscall.RawConn) error { return controlSocket(network, remote, raw) }}
	return lc.ListenPacket(ctx, network, address)
}
