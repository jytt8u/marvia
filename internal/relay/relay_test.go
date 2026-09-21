package relay

import (
	"bytes"
	"io"
	"net"
	"testing"
)

// TestReplyAfterRequestEndIsNotLost: клиент договорил и закрыл свою половину,
// а сервер отвечает уже после этого — ответ обязан дойти целиком. Именно на
// этом ломаются самодельные прокси: полное закрытие вместо половинного, и
// «страница загрузилась наполовину».
func TestReplyAfterRequestEndIsNotLost(t *testing.T) {
	listen := func() net.Listener {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return ln
	}
	upstream := listen()
	front := listen()

	// Сервер: читает запрос до конца и только потом отвечает.
	reply := bytes.Repeat([]byte("ответ"), 20000)
	go func() {
		c, err := upstream.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.ReadAll(c)
		_, _ = c.Write(reply)
	}()

	// Нода: принимает клиента и перекладывает байты к серверу.
	done := make(chan error, 1)
	go func() {
		c, err := front.Accept()
		if err != nil {
			done <- err
			return
		}
		up, err := net.Dial("tcp", upstream.Addr().String())
		if err != nil {
			done <- err
			return
		}
		done <- Bidirectional(c, up)
	}()

	client, err := net.Dial("tcp", front.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write(bytes.Repeat([]byte("запрос"), 10000)); err != nil {
		t.Fatal(err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("ответ дошёл не целиком: %d из %d байт", len(got), len(reply))
	}
	if err := <-done; err != nil {
		t.Fatalf("перекладка кончилась ошибкой: %v", err)
	}
}
