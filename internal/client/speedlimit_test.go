package client_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/metered"
	"github.com/jytt8u/marvia/internal/mux"
	"github.com/jytt8u/marvia/internal/transport"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Потолок скорости держится сквозь весь стек, а не только в счётчике.
//
// Единичный тест на metered.Conn доказывает, что ограничитель придерживает
// байты. Но между ним и покупателем лежат TLS, VP1, добивка кадров и
// мультиплексор, и каждый из них буферизует. Этот тест поднимает настоящую
// цепочку — клиент, нода, цель — и меряет то, что покупатель и почувствует.
func TestSpeedLimitHoldsThroughTheWholeStack(t *testing.T) {
	// Цель льёт нули столько, сколько попросят.
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("цель: %v", err)
	}
	defer target.Close()
	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, zeros{})
			}()
		}
	}()

	const speed = 512 << 10 // 512 КиБ/с, то есть около 4 Мбит/с
	const payload = 2 * speed

	node := startLimitedNode(t, speed)

	dialer, err := client.NewDialer(node.info, node.key, node.opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	host, port := splitHostPort(t, target.Addr().String())
	addr, err := vp1.AddressFromHostPort(host, port)
	if err != nil {
		t.Fatalf("адрес цели: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	stream, err := dialer.DialTarget(ctx, addr)
	if err != nil {
		t.Fatalf("поток до цели: %v", err)
	}
	defer stream.Close()

	start := time.Now()
	read, err := io.Copy(io.Discard, io.LimitReader(stream, payload))
	took := time.Since(start)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if read != payload {
		t.Fatalf("прочитано %d байт вместо %d", read, payload)
	}

	// Ведро отдаёт запас сразу, поэтому нижнюю границу берём с оглядкой на
	// него: за вычетом ведра остаток не может уехать быстрее потолка.
	got := float64(read) / took.Seconds()
	if got > 3*speed {
		t.Fatalf("реальная скорость %.0f Б/с при потолке %d Б/с — потолок не держит", got, speed)
	}
	t.Logf("потолок %d Б/с, вышло %.0f Б/с за %s", speed, got, took.Round(time.Millisecond))
}

// limitedNode — нода, которая считает байты и придерживает их, как в бою.
type limitedNode struct {
	info client.Node
	key  vp1.KeyPair
	opts client.Options
}

func startLimitedNode(t *testing.T, speed int64) *limitedNode {
	t.Helper()

	serverKey, _ := vp1.GenerateKeyPair()
	clientKey, _ := vp1.GenerateKeyPair()

	cert, err := transport.SelfSignedCertificate("node.example")
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	pool, err := transport.CertificatePool(cert)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	ln := transport.Listen(tcp, transport.ServerConfig{Certificate: cert})
	t.Cleanup(func() { _ = ln.Close() })

	guard := vp1.NewReplayGuard(vp1.ClockSkew)

	// Одно ведро на все соединения — так же, как аккаунт делит его в бою.
	limiter := rate.NewLimiter(rate.Limit(speed), int(speed/4))

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				// Тот же порядок, что на настоящей ноде: счётчик снаружи
				// протокола, потолок вешается на него после опознания.
				meter := metered.New(conn)
				meter.Limit(limiter)
				serveLimited(meter, serverKey, guard)
			}()
		}
	}()

	return &limitedNode{
		info: client.Node{
			Name:      "limited",
			Address:   tcp.Addr().String(),
			SNI:       "node.example",
			PublicKey: vp1.EncodeKey(serverKey.Public),
		},
		key:  clientKey,
		opts: client.Options{RootCAs: pool},
	}
}

func serveLimited(conn net.Conn, key vp1.KeyPair, guard *vp1.ReplayGuard) {
	tunnel, _, err := vp1.ServerHandshake(conn, key, guard, vp1.AllowAll)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer tunnel.Close()

	session, err := mux.Server(tunnel)
	if err != nil {
		return
	}
	defer session.Close()

	for {
		stream, err := mux.Accept(session)
		if err != nil {
			return
		}
		go func() {
			defer stream.Close()

			addr, _, err := vp1.ReadRequestOf(stream)
			if err != nil {
				return
			}
			upstream, err := net.Dial("tcp", addr.String())
			if err != nil {
				_ = vp1.WriteStatus(stream, vp1.StatusUnreachable)
				return
			}
			defer upstream.Close()

			if err := vp1.WriteStatus(stream, vp1.StatusOK); err != nil {
				return
			}
			_, _ = io.Copy(stream, upstream)
		}()
	}
}
