package nodesync

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/users"
)

func TestSnapshotIsPrivateBoundAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.access")
	c := New("https://panel.example", "random-node-token").WithSnapshot(path)
	u := testUser("secret-not-in-plaintext")
	u.Label = "имя покупателя"
	u.ExpiresAt = time.Now().Add(time.Hour)
	now := time.Now()
	if err := c.saveSnapshot([]users.User{u}, now); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(u.Secret)) || bytes.Contains(raw, []byte(u.Label)) {
		t.Fatal("снимок раскрывает доступ")
	}
	s, err := c.loadSnapshot(now.Add(time.Minute))
	if err != nil || len(s.Users) != 1 || s.Users[0].Label != "" || !s.Users[0].ExpiresAt.Equal(u.ExpiresAt) {
		t.Fatalf("неверный снимок: %+v %v", s, err)
	}
	for _, other := range []*Client{New(c.base, "other-token").WithSnapshot(path), New("https://other.example", c.token).WithSnapshot(path)} {
		if _, err := other.loadSnapshot(now.Add(time.Minute)); err == nil {
			t.Fatal("принят снимок чужой ноды или панели")
		}
	}
	for _, at := range []time.Time{now.Add(-time.Second), now.Add(OfflineTTL)} {
		if _, err := c.loadSnapshot(at); err == nil {
			t.Fatal("принят будущий или просроченный снимок")
		}
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.loadSnapshot(now.Add(time.Minute)); err == nil {
		t.Fatal("принят повреждённый снимок")
	}
}

func TestSnapshotNeverOverridesAnExplicitRevocation(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"disabled"}`))
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "node.access")
			c := New(srv.URL, "token").WithSnapshot(path)
			if err := c.saveSnapshot([]users.User{testUser("secret")}, time.Now()); err != nil {
				t.Fatal(err)
			}
			list, _, offline, err := c.Bootstrap(context.Background(), nil)
			if err == nil || offline || len(list) != 0 {
				t.Fatalf("отзыв проигнорирован: %+v %v %v", list, offline, err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("отозванный снимок остался: %v", err)
			}
		})
	}
}

func TestOfflineBootstrapPreservesUsageAndDoesNotRenewLease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	c := New(srv.URL, "token").WithSnapshot(filepath.Join(t.TempDir(), "node.access"))
	u := testUser("secret")
	u.Account = "account"
	c.snapshotUsage = map[string]users.Usage{"account": {Up: 90, Down: 10}}
	at := time.Now().Add(-23 * time.Hour)
	if err := c.saveSnapshot([]users.User{u}, at); err != nil {
		t.Fatal(err)
	}
	list, usage, offline, err := c.Bootstrap(context.Background(), map[string]users.Usage{"account": {Up: 80, Down: 20}})
	if err != nil || !offline {
		t.Fatalf("снимок не восстановлен: %v", err)
	}
	if usage["account"].Up != 90 || usage["account"].Down != 20 {
		t.Fatalf("расход уменьшен: %+v", usage)
	}
	if !list[0].ExpiresAt.Equal(at.Add(OfflineTTL)) {
		t.Fatal("перезапуск продлил автономный доступ")
	}
	if _, err := c.loadSnapshot(at.Add(OfflineTTL)); err == nil {
		t.Fatal("снимок продлён на диске")
	}
}

func TestSuccessfulSyncReplacesRevokedCachedCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"users":[]}`)) }))
	defer srv.Close()
	c := New(srv.URL, "token").WithSnapshot(filepath.Join(t.TempDir(), "node.access"))
	if err := c.saveSnapshot([]users.User{testUser("old")}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchUsers(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := c.loadSnapshot(time.Now())
	if err != nil || len(s.Users) != 0 {
		t.Fatalf("старый доступ сохранился: %+v %v", s.Users, err)
	}
}
