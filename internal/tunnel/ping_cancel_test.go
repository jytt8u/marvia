package tunnel

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/mux"
)

func TestCancelPingDoesNotCloseWorkingSession(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	session, err := mux.Client(a)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPool(nil, 1, 32)
	defer p.Close()
	p.mu.Lock()
	p.sessions = []*pooled{{sess: session, retireAt: time.Now().Add(time.Hour)}}
	p.mu.Unlock()
	// Собеседник не читает: запрос должен ждать сеть, но не удерживать UI.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("отмена: %v", err)
	}
	if session.IsClosed() {
		t.Fatal("отмена замера оборвала сессию")
	}
	if _, err := p.Ping(context.Background()); err == nil {
		t.Fatal("запущен второй зависший замер")
	}
}
