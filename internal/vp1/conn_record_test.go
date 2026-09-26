package vp1

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

type capturedWrites struct {
	net.Conn
	frames [][]byte
}

func (c *capturedWrites) Write(p []byte) (int, error) {
	c.frames = append(c.frames, bytes.Clone(p))
	return len(p), nil
}

// Размер проверяется после шифрования, а не по длине исходной нагрузки:
// заголовок и тег тоже занимают место внутри записи TLS.
func TestBulkWritesFitTLSRecord(t *testing.T) {
	client, server, _ := handshakePair(t, AllowAll)
	wire := &capturedWrites{Conn: client.Conn}
	client.Conn = wire
	payload := bytes.Repeat([]byte("передача"), 20000)
	n, err := client.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("запись: %d, %v", n, err)
	}
	var received []byte
	for _, frame := range wire.frames {
		if len(frame) > 16384 {
			t.Fatalf("кадр %d байт требует лишней записи TLS", len(frame))
		}
		if int(binary.BigEndian.Uint16(frame[:2])) != len(frame)-2 {
			t.Fatal("длина кадра не совпадает с заголовком")
		}
		plain, err := server.recv.Decrypt(nil, nil, frame[2:])
		if err != nil {
			t.Fatal(err)
		}
		body, err := unpackPadded(plain)
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, body...)
	}
	if !bytes.Equal(received, payload) {
		t.Fatal("получатель восстановил другой поток")
	}
}

// Старые отправители используют весь прежний лимит. Новый получатель
// обязан принимать такие кадры и при чтении маленькими порциями.
func TestReceiverAcceptsLegacyFullSizeFrame(t *testing.T) {
	client, server, _ := handshakePair(t, AllowAll)
	payload := bytes.Repeat([]byte{0xa7}, BulkPayload)
	plain := packPadded(payload, 0)
	sealed, err := client.send.Encrypt(nil, nil, plain)
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 2+len(sealed))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(sealed)))
	copy(frame[2:], sealed)
	done := make(chan error, 1)
	go func() { _, err := client.Conn.Write(frame); done <- err }()
	got := make([]byte, len(payload))
	for offset := 0; offset < len(got); {
		n := min(137, len(got)-offset)
		if _, err := io.ReadFull(server, got[offset:offset+n]); err != nil {
			t.Fatal(err)
		}
		offset += n
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("старый кадр повреждён")
	}
}
