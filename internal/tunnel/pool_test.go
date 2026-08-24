package tunnel_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/mux"
	"github.com/veilproject/veil/internal/tunnel"
	"github.com/veilproject/veil/internal/vp1"
)

// testServer — нода без маскировки: только VP1 и мультиплексирование.
// Маскировка проверяется отдельно, в internal/transport.
type testServer struct {
	addr string
	pub  []byte
}

func startServer(t *testing.T, handle func(stream net.Conn)) *testServer {
	t.Helper()

	key, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	guard := vp1.NewReplayGuard(vp1.ClockSkew)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tun, _, err := vp1.ServerHandshake(conn, key, guard, vp1.AllowAll)
				if err != nil {
					_ = conn.Close()
					return
				}
				defer tun.Close()

				session, err := mux.Server(tun)
				if err != nil {
					return
				}
				defer session.Close()

				for {
					stream, err := mux.Accept(session)
					if err != nil {
						return
					}
					go handle(stream)
				}
			}()
		}
	}()

	return &testServer{addr: ln.Addr().String(), pub: key.Public}
}

// dialer собирает функцию дозвона для пула и считает вызовы.
func dialer(t *testing.T, srv *testServer, calls *atomic.Int32) tunnel.DialFunc {
	t.Helper()
	clientKey, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи клиента: %v", err)
	}

	return func(ctx context.Context) (net.Conn, error) {
		if calls != nil {
			calls.Add(1)
		}
		var d net.Dialer
		raw, err := d.DialContext(ctx, "tcp", srv.addr)
		if err != nil {
			return nil, err
		}
		conn, err := vp1.ClientHandshake(raw, clientKey, srv.pub)
		if err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	}
}

// echoHandler читает до конца, потом отвечает. Именно такой обработчик
// требует корректного полузакрытия: пока клиент не скажет «я всё сказал»,
// ответа не будет.
func echoHandler(stream net.Conn) {
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		return
	}
	_, _ = stream.Write(append([]byte("ответ: "), data...))
}

// TestHalfCloseThroughStream — проверка того, что клиент, закрывший свою
// половину потока, всё ещё может прочитать ответ.
//
// Без этого свойства ломается всё, где запрос заканчивается закрытием записи:
// HTTP-запрос без keep-alive, отправка письма по SMTP, загрузка файла.
// Проявляется как «страница загрузилась наполовину» — самый неприятный класс
// багов в прокси, потому что он плавающий.
func TestHalfCloseThroughStream(t *testing.T) {
	srv := startServer(t, echoHandler)
	pool := tunnel.NewPool(dialer(t, srv, nil), 0, 0)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := pool.Open(ctx)
	if err != nil {
		t.Fatalf("открытие потока: %v", err)
	}
	defer stream.Close()

	if _, err := stream.Write([]byte("запрос")); err != nil {
		t.Fatalf("запись: %v", err)
	}

	// Говорим «я всё сказал», не закрывая приём.
	type closeWriter interface{ CloseWrite() error }
	cw, ok := stream.(closeWriter)
	if !ok {
		t.Fatal("поток не умеет закрывать только исходящую половину")
	}
	if err := cw.CloseWrite(); err != nil {
		t.Fatalf("полузакрытие: %v", err)
	}

	_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("чтение ответа после полузакрытия: %v", err)
	}
	if string(got) != "ответ: запрос" {
		t.Fatalf("получено %q", got)
	}
}

// TestPoolConcurrentStreams: много одновременных потоков должны работать
// независимо и не путать данные между собой.
func TestPoolConcurrentStreams(t *testing.T) {
	srv := startServer(t, func(stream net.Conn) {
		defer stream.Close()
		buf := make([]byte, 64)
		n, err := stream.Read(buf)
		if err != nil {
			return
		}
		_, _ = stream.Write(buf[:n])
	})

	pool := tunnel.NewPool(dialer(t, srv, nil), 0, 0)
	defer pool.Close()

	const streams = 40
	var wg sync.WaitGroup
	errs := make(chan error, streams)

	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			stream, err := pool.Open(ctx)
			if err != nil {
				errs <- fmt.Errorf("поток %d: открытие: %w", i, err)
				return
			}
			defer stream.Close()

			want := fmt.Sprintf("поток номер %d", i)
			if _, err := stream.Write([]byte(want)); err != nil {
				errs <- fmt.Errorf("поток %d: запись: %w", i, err)
				return
			}
			buf := make([]byte, len(want))
			if _, err := io.ReadFull(stream, buf); err != nil {
				errs <- fmt.Errorf("поток %d: чтение: %w", i, err)
				return
			}
			if string(buf) != want {
				errs <- fmt.Errorf("поток %d: получено %q, ожидалось %q", i, buf, want)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestPoolReusesSessions: пул не должен открывать новое соединение до ноды
// на каждый запрос — ровно от этого мы и уходили.
func TestPoolReusesSessions(t *testing.T) {
	srv := startServer(t, func(stream net.Conn) {
		defer stream.Close()
		_, _ = io.Copy(stream, stream)
	})

	var calls atomic.Int32
	const maxSessions, maxStreams = 4, 16
	pool := tunnel.NewPool(dialer(t, srv, &calls), maxSessions, maxStreams)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Открываем ровно столько потоков, сколько помещается в одну сессию.
	opened := make([]net.Conn, 0, maxStreams)
	for i := 0; i < maxStreams; i++ {
		stream, err := pool.Open(ctx)
		if err != nil {
			t.Fatalf("поток %d: %v", i, err)
		}
		opened = append(opened, stream)
	}
	defer func() {
		for _, s := range opened {
			_ = s.Close()
		}
	}()

	if got := calls.Load(); got != 1 {
		t.Fatalf("на %d потоков открыто %d соединений до ноды, ожидалось 1", maxStreams, got)
	}
}

// TestPoolDoesNotBurstHandshakes — прямое следствие того, как ТСПУ ловит
// туннели в 2026 году: несколько параллельных TLS-хендшейков к одному имени
// в коротком окне сами по себе служат признаком, и соединения после этого
// молча дропаются на пару минут.
//
// Поэтому пул обязан, во-первых, никогда не вести два хендшейка одновременно,
// а во-вторых, не открывать новое соединение, если в уже поднятом есть место.
func TestPoolDoesNotBurstHandshakes(t *testing.T) {
	srv := startServer(t, func(stream net.Conn) {
		defer stream.Close()
		_, _ = io.Copy(stream, stream)
	})

	var (
		inFlight atomic.Int32
		peak     atomic.Int32
		total    atomic.Int32
	)

	base := dialer(t, srv, nil)
	counting := func(ctx context.Context) (net.Conn, error) {
		now := inFlight.Add(1)
		for {
			best := peak.Load()
			if now <= best || peak.CompareAndSwap(best, now) {
				break
			}
		}
		defer inFlight.Add(-1)

		total.Add(1)
		// Задержка делает гонку заметной: без сериализации сюда влетели бы
		// все горутины разом.
		time.Sleep(50 * time.Millisecond)
		return base(ctx)
	}

	pool := tunnel.NewPool(counting, 4, 32)
	defer pool.Close()

	const streams = 20
	var wg sync.WaitGroup
	opened := make(chan net.Conn, streams)

	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			stream, err := pool.Open(ctx)
			if err != nil {
				t.Errorf("открытие потока: %v", err)
				return
			}
			opened <- stream
		}()
	}
	wg.Wait()
	close(opened)
	for s := range opened {
		_ = s.Close()
	}

	if got := peak.Load(); got > 1 {
		t.Fatalf("одновременных хендшейков: %d, допускается не больше одного", got)
	}
	if got := total.Load(); got != 1 {
		t.Fatalf("на %d потоков открыто %d соединений, хватало одного", streams, got)
	}
}
