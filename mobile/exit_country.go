package mobile

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

const exitCountryURL = "https://ipapi.co/country/"

// ExitCountry returns the two-letter country of the current *exit* IP. The
// Android app itself is excluded from the system VPN, so its ordinary HTTP
// client would expose the phone's country. This request explicitly dials via
// the active backend and resolves the destination name at the node.
// A failed lookup never affects the tunnel.
func (t *Tunnel) ExitCountry() string {
	t.mu.Lock()
	backend := t.dialer
	t.mu.Unlock()
	if backend == nil || backend.Node().Country != "" {
		return ""
	}
	nodeID := backend.Node().ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := queryExitCountry(ctx, backend.DialTarget, exitCountryURL)
	if err != nil || backend.Node().ID != nodeID {
		return ""
	}
	return code
}

func queryExitCountry(ctx context.Context, dial func(context.Context, vp1.Address) (net.Conn, error), endpoint string) (string, error) {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				return nil, err
			}
			target, err := vp1.AddressFromHostPort(host, uint16(port))
			if err != nil {
				return nil, err
			}
			return dial(ctx, target)
		},
	}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{
		Transport:     transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("country lookup failed")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16))
	if err != nil {
		return "", err
	}
	code := strings.ToUpper(strings.TrimSpace(string(body)))
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return "", errors.New("invalid country code")
	}
	return code, nil
}
