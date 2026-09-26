package vp1

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/noise"
)

// Подменяем только провод: настоящие хендшейк, шифрование и Read остаются
// в работе. Так проверяем отказ приложения, а не одну библиотечную функцию.
type attackWire struct {
	net.Conn
	input  *bytes.Reader
	output bytes.Buffer
	closed bool
}

func (w *attackWire) Read(p []byte) (int, error)  { return w.input.Read(p) }
func (w *attackWire) Write(p []byte) (int, error) { return w.output.Write(p) }
func (w *attackWire) Close() error                { w.closed = true; return w.Conn.Close() }

func securityFirstMessage(t *testing.T, node KeyPair, stamp time.Time) []byte {
	t.Helper()
	client, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: CipherSuite, Random: rand.Reader, Pattern: noise.HandshakeIK,
		Initiator: true, Prologue: []byte(Prologue), StaticKeypair: client.noiseKey(), PeerStatic: node.Public,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, timestampLen)
	binary.BigEndian.PutUint64(payload, uint64(stamp.UnixNano()))
	msg, _, _, err := hs.WriteMessage(nil, packPadded(payload, 0))
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := writeFrame(&wire, msg); err != nil {
		t.Fatal(err)
	}
	return wire.Bytes()
}

func securityServerAttempt(t *testing.T, key KeyPair, guard *ReplayGuard, data []byte) (*attackWire, error) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	wire := &attackWire{Conn: a, input: bytes.NewReader(data)}
	_, _, err := ServerHandshake(wire, key, guard, AllowAll)
	return wire, err
}

func TestSecurityCapturedHandshakeCannotBeReplayed(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	guard := NewReplayGuard(ClockSkew)
	msg := securityFirstMessage(t, key, time.Now())
	wire, err := securityServerAttempt(t, key, guard, msg)
	if err != nil || wire.output.Len() == 0 {
		t.Fatalf("исходный вход не принят: %v", err)
	}
	wire, err = securityServerAttempt(t, key, guard, msg)
	if !errors.Is(err, ErrReplay) || wire.output.Len() != 0 {
		t.Fatalf("повтор получил ответ: %v", err)
	}
}

func TestSecurityHandshakeOutsideTimeWindowGetsNoResponse(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []time.Duration{-3 * ClockSkew, 3 * ClockSkew} {
		t.Run(offset.String(), func(t *testing.T) {
			guard := NewReplayGuard(ClockSkew)
			wire, err := securityServerAttempt(t, key, guard, securityFirstMessage(t, key, time.Now().Add(offset)))
			if !errors.Is(err, ErrClockSkew) || wire.output.Len() != 0 || guard.Size() != 0 {
				t.Fatalf("вход вне окна изменил состояние или получил ответ: %v", err)
			}
		})
	}
}

func TestSecurityConcurrentReplayHasOnlyOneWinner(t *testing.T) {
	guard := NewReplayGuard(ClockSkew)
	stamp := time.Now()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := guard.Check([]byte("одновременно перехваченный msg1"), stamp)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrReplay) {
				t.Errorf("ошибка: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("повтор принят %d раз", accepted.Load())
	}
}

func TestSecurityTransportRejectsRecordAttacks(t *testing.T) {
	for _, kind := range []string{"ciphertext", "tag", "replay", "reorder", "other-session"} {
		t.Run(kind, func(t *testing.T) {
			client, server, _ := handshakePair(t, AllowAll)
			capture := &capturedWrites{Conn: client.Conn}
			client.Conn = capture
			for _, msg := range []string{"первый", "второй"} {
				if _, err := client.Write([]byte(msg)); err != nil {
					t.Fatal(err)
				}
			}
			first, second := bytes.Clone(capture.frames[0]), bytes.Clone(capture.frames[1])
			var wire []byte
			switch kind {
			case "ciphertext":
				first[2] ^= 1
				wire = first
			case "tag":
				first[len(first)-1] ^= 1
				wire = first
			case "replay":
				wire = append(first, first...)
			case "reorder":
				wire = append(second, first...)
			case "other-session":
				_, other, _ := handshakePair(t, AllowAll)
				server = other
				wire = first
			}
			transport := &attackWire{Conn: server.Conn, input: bytes.NewReader(wire)}
			server.Conn = transport
			if kind == "replay" {
				good := make([]byte, len([]byte("первый")))
				if _, err := io.ReadFull(server, good); err != nil || string(good) != "первый" {
					t.Fatalf("исходный кадр: %v", err)
				}
			}
			buf := make([]byte, 128)
			if n, err := server.Read(buf); n != 0 || err == nil || !transport.closed {
				t.Fatalf("атака не закрыла поток без выдачи данных: n=%d, err=%v, closed=%v", n, err, transport.closed)
			}
		})
	}
}
