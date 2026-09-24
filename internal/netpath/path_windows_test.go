//go:build windows

package netpath

import (
	"net"
	"net/netip"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTunnelAndSiteRoutesCannotBecomeTheUplink(t *testing.T) {
	routes := []candidate{
		{9, netip.MustParsePrefix("0.0.0.0/1"), 0},
		{10, netip.MustParsePrefix("1.1.1.1/32"), 0},
		{3, netip.MustParsePrefix("0.0.0.0/0"), 80},
		{4, netip.MustParsePrefix("0.0.0.0/0"), 50},
	}
	index, err := chooseDefault(routes)
	if err != nil || index != 4 {
		t.Fatalf("внешний интерфейс %d: %v", index, err)
	}
	if _, err := chooseDefault(routes[:2]); err == nil {
		t.Fatal("туннель принят за внешнюю сеть")
	}
}

func TestBindingOneSocketDoesNotExemptOtherTraffic(t *testing.T) {
	index, err := defaultInterface(windows.AF_INET)
	if err != nil {
		t.Skipf("нет внешнего IPv4 интерфейса: %v", err)
	}
	for _, network := range []string{"udp4", "tcp4"} {
		t.Run(network, func(t *testing.T) {
			var handles []windows.Handle
			for i := 0; i < 2; i++ {
				kind := windows.SOCK_DGRAM
				if network == "tcp4" {
					kind = windows.SOCK_STREAM
				}
				fd, err := windows.Socket(windows.AF_INET, kind, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer windows.Closesocket(fd)
				handles = append(handles, fd)
			}
			if err := bindSocket(testRaw{handles[0]}, windows.AF_INET, index); err != nil {
				t.Fatal(err)
			}
			bound, err := windows.GetsockoptInt(handles[0], windows.IPPROTO_IP, unicastIF)
			if err != nil || uint32(bound) != index {
				t.Fatalf("сокет ноды: %d %v", bound, err)
			}
			ordinary, err := windows.GetsockoptInt(handles[1], windows.IPPROTO_IP, unicastIF)
			if err != nil || ordinary != 0 {
				t.Fatalf("посторонний сокет получил обход: %d %v", ordinary, err)
			}
		})
	}
}

type testRaw struct{ fd windows.Handle }

func (r testRaw) Control(f func(uintptr)) error  { f(uintptr(r.fd)); return nil }
func (r testRaw) Read(func(uintptr) bool) error  { return net.ErrClosed }
func (r testRaw) Write(func(uintptr) bool) error { return net.ErrClosed }
