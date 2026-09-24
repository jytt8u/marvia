//go:build !windows

package foreign

import (
	"context"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
)

func dialSystem(d internet.SystemDialer, ctx context.Context, src xnet.Address, dest xnet.Destination, opts *internet.SocketConfig) (xnet.Conn, error) {
	return d.Dial(ctx, src, dest, opts)
}
