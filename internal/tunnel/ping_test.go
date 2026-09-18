package tunnel_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/tunnel"
)

func TestPingUsesEstablishedTunnelWithoutRedial(t *testing.T) {
	srv := startServer(t, echoHandler)
	var calls atomic.Int32
	pool := tunnel.NewPool(dialer(t, srv, &calls), 1, 32)
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := pool.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	rtt, err := pool.Ping(ctx)
	if err != nil || rtt <= 0 {
		t.Fatalf("отклик: %v, %v", rtt, err)
	}
	if calls.Load() != 1 {
		t.Fatal("замер заново подключился к ноде")
	}
}

func TestPingDoesNotCreateTunnelAndHonorsCancellation(t *testing.T) {
	pool := tunnel.NewPool(func(context.Context) (net.Conn, error) {
		t.Error("замер не должен устанавливать соединение")
		return nil, errors.New("неожиданный дозвон")
	}, 1, 32)
	defer pool.Close()
	if _, err := pool.Ping(context.Background()); err == nil {
		t.Fatal("без туннеля получен пинг")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("отмена: %v", err)
	}
}
