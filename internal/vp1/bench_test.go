package vp1

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// Отдельно видна цена чтения заголовка без аллокаций и шифрования TLS.
func BenchmarkReadFrameReuse(b *testing.B) {
	wire := make([]byte, frameCap)
	binary.BigEndian.PutUint16(wire[:2], uint16(len(wire)-2))
	buf := make([]byte, MaxPlaintext+tagLen)
	r := bytes.NewReader(wire)
	b.SetBytes(int64(len(wire)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Reset(wire)
		if _, err := readFrameInto(r, buf); err != nil {
			b.Fatal(err)
		}
	}
}

// Бенчмарк пути данных: сколько байт в секунду прогоняет одно соединение VP1
// через TCP на петле. Петля, а не net.Pipe: тот синхронный и мерил бы себя.
//
// Это потолок шифрования и кадрирования на одном ядре — выше него нода не
// отдаст ни по какому каналу. Цифра нужна, чтобы правки в этом файле были
// измерены, а не угаданы.

func benchPair(b *testing.B) (client, server *Conn) {
	b.Helper()

	serverKey, err := GenerateKeyPair()
	if err != nil {
		b.Fatal(err)
	}
	clientKey, err := GenerateKeyPair()
	if err != nil {
		b.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()

	type result struct {
		conn *Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			done <- result{nil, err}
			return
		}
		conn, _, err := ServerHandshake(raw, serverKey, NewReplayGuard(ClockSkew), AllowAll)
		done <- result{conn, err}
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	client, err = ClientHandshake(raw, clientKey, serverKey.Public)
	if err != nil {
		b.Fatal(err)
	}
	res := <-done
	if res.err != nil {
		b.Fatal(res.err)
	}
	b.Cleanup(func() {
		_ = client.Close()
		_ = res.conn.Close()
	})
	return client, res.conn
}

// BenchmarkTransfer — поток в одну сторону крупными кусками, как при скачивании.
func BenchmarkTransfer(b *testing.B) {
	client, server := benchPair(b)

	const chunk = 64 << 10
	buf := make([]byte, chunk)
	sink := make([]byte, chunk)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, err := server.Read(sink); err != nil {
				return
			}
		}
	}()

	b.SetBytes(chunk)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Write(buf); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	_ = client.Close()
	<-done
}

// BenchmarkEcho — туда и обратно мелкими кусками, как при обычном сёрфинге.
func BenchmarkEcho(b *testing.B) {
	client, server := benchPair(b)

	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := server.Read(buf)
			if err != nil {
				return
			}
			if _, err := server.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	const msg = 1400
	out := make([]byte, msg)
	in := make([]byte, msg)

	b.SetBytes(msg * 2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Write(out); err != nil {
			b.Fatal(err)
		}
		if _, err := io.ReadFull(client, in); err != nil {
			b.Fatal(err)
		}
	}
}
