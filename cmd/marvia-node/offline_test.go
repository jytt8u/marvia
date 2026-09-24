package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jytt8u/marvia/internal/users"
)

func TestNodeRestartsWhilePanelIsUnavailable(t *testing.T) {
	var offline atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			w.WriteHeader(503)
			return
		}
		if r.Method == "POST" {
			w.WriteHeader(204)
			return
		}
		_, _ = w.Write([]byte(`{"users":[{"kind":"trojan","secret":"test-only","enabled":true,"traffic_limit":1000}]}`))
	}))
	defer srv.Close()
	opts := serverOptions{panelURL: srv.URL, panelToken: "test-token", usageFile: filepath.Join(t.TempDir(), "node.usage.json")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, _, _, err := preparePanelUsers(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	first.RestoreUsage(map[string]users.Usage{"trojan|test-only": {Up: 900}})
	if err := first.SaveUsage(opts.usageFile); err != nil {
		t.Fatal(err)
	}
	offline.Store(true)
	restarted, _, _, err := preparePanelUsers(ctx, opts)
	if err != nil {
		t.Fatalf("перезапуск без панели: %v", err)
	}
	stats := restarted.Stats()
	if len(stats) != 1 || stats[0].Usage.Up != 900 {
		t.Fatalf("потерян доступ или расход: %+v", stats)
	}
}
