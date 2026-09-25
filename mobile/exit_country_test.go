package mobile

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jytt8u/marvia/internal/vp1"
)

func TestExitCountryDialsOnlyThroughBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fi\n"))
	}))
	defer server.Close()
	called := false
	dial := func(ctx context.Context, target vp1.Address) (net.Conn, error) {
		called = true
		if target.Host != "geo.example" || target.Type != vp1.AtypDomain {
			t.Errorf("destination was resolved outside tunnel: %+v", target)
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	code, err := queryExitCountry(context.Background(), dial, "http://geo.example/country/")
	if err != nil || code != "FI" || !called {
		t.Fatalf("code=%q err=%v backend called=%t", code, err, called)
	}
}

func TestExitCountryRejectsBadResponseAndRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://example.org/", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("not a country"))
	}))
	defer server.Close()
	dial := func(ctx context.Context, _ vp1.Address) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	for _, path := range []string{"/bad", "/redirect"} {
		if code, err := queryExitCountry(context.Background(), dial, "http://geo.example"+path); err == nil || code != "" {
			t.Errorf("%s: code=%q err=%v", path, code, err)
		}
	}
}
