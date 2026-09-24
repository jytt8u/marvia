//go:build windows

package netpath

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const unicastIF = 31

type candidate struct {
	index  uint32
	prefix netip.Prefix
	metric uint64
}

func chooseDefault(routes []candidate) (uint32, error) {
	var best *candidate
	for i := range routes {
		r := &routes[i]
		// /1 туннеля и /32 отдельных сайтов не должны стать внешним путём.
		if !r.prefix.IsValid() || r.prefix.Bits() != 0 || r.index == 0 {
			continue
		}
		if best == nil || r.metric < best.metric {
			best = r
		}
	}
	if best == nil {
		return 0, errors.New("нет внешнего маршрута по умолчанию")
	}
	return best.index, nil
}

func defaultInterface(family winipcfg.AddressFamily) (uint32, error) {
	routes, err := winipcfg.GetIPForwardTable2(family)
	if err != nil {
		return 0, err
	}
	interfaces, err := winipcfg.GetIPInterfaceTable(family)
	if err != nil {
		return 0, err
	}
	metrics := make(map[winipcfg.LUID]uint32)
	for _, iface := range interfaces {
		if iface.Connected {
			metrics[iface.InterfaceLUID] = iface.Metric
		}
	}
	var candidates []candidate
	for _, route := range routes {
		metric, up := metrics[route.InterfaceLUID]
		if !up {
			continue
		}
		candidates = append(candidates, candidate{route.InterfaceIndex, route.DestinationPrefix.Prefix(), uint64(route.Metric) + uint64(metric)})
	}
	return chooseDefault(candidates)
}

func controlSocket(network, address string, raw syscall.RawConn) error {
	if !Enabled() {
		return nil
	}
	host, _, _ := net.SplitHostPort(address)
	ip, _ := netip.ParseAddr(host)
	if ip.IsLoopback() {
		return nil
	}
	family := winipcfg.AddressFamily(windows.AF_INET)
	if strings.HasSuffix(network, "6") {
		family = windows.AF_INET6
	}
	index, err := defaultInterface(family)
	if err != nil {
		return fmt.Errorf("внешний интерфейс: %w", err)
	}
	return bindSocket(raw, family, index)
}

func bindSocket(raw syscall.RawConn, family winipcfg.AddressFamily, index uint32) error {
	var socketErr error
	err := raw.Control(func(fd uintptr) {
		level, value := windows.IPPROTO_IPV6, int(index)
		if family == windows.AF_INET {
			level = windows.IPPROTO_IP
			var bytes [4]byte
			binary.BigEndian.PutUint32(bytes[:], index)
			value = int(binary.NativeEndian.Uint32(bytes[:]))
		}
		socketErr = windows.SetsockoptInt(windows.Handle(fd), level, unicastIF, value)
	})
	if err != nil {
		return err
	}
	if socketErr != nil {
		return fmt.Errorf("привязка сокета к внешнему интерфейсу: %w", socketErr)
	}
	return nil
}
