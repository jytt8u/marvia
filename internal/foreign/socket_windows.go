//go:build windows

package foreign

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jytt8u/marvia/internal/netpath"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
)

func dialSystem(d internet.SystemDialer, ctx context.Context, src xnet.Address, dest xnet.Destination, opts *internet.SocketConfig) (xnet.Conn, error) {
	if !netpath.Enabled() {
		return d.Dial(ctx, src, dest, opts)
	}
	// Xray проглатывает ошибки контроллеров сокета. Дозваниваемся сами:
	// неудачная привязка должна прервать подключение, а не замкнуть туннель.
	if opts != nil && (opts.BindPort != 0 || len(opts.BindAddress) != 0 || opts.Interface != "") {
		return nil, fmt.Errorf("чужой ключ задаёт несовместимую привязку сокета")
	}
	if dest.Network == xnet.Network_UDP {
		remote, err := net.ResolveUDPAddr("udp", dest.NetAddr())
		if err != nil {
			return nil, err
		}
		network, local := "udp6", "[::]:0"
		if remote.IP.To4() != nil {
			network, local = "udp4", "0.0.0.0:0"
		}
		if src != nil && src != xnet.AnyIP {
			local = net.JoinHostPort(src.IP().String(), "0")
		}
		pc, err := netpath.ListenPacket(ctx, network, local, remote.String())
		if err != nil {
			return nil, err
		}
		return &internet.PacketConnWrapper{PacketConn: pc, Dest: remote}, nil
	}
	if dest.Network != xnet.Network_TCP {
		return nil, fmt.Errorf("неподдерживаемая сеть ноды")
	}
	dialer := netpath.Dialer()
	dialer.Timeout = 16 * time.Second
	dialer.KeepAliveConfig = net.KeepAliveConfig{Enable: true, Idle: 45 * time.Second, Interval: 45 * time.Second, Count: -1}
	if src != nil && src != xnet.AnyIP {
		dialer.LocalAddr = &net.TCPAddr{IP: src.IP()}
	}
	return dialer.DialContext(ctx, "tcp", dest.NetAddr())
}
