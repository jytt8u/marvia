package rewind

import (
	"bytes"
	"io"
	"net"
	"testing"
)

// pipeWith отправляет данные в соединение и возвращает читающую сторону.
func pipeWith(t *testing.T, data []byte) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		_, _ = client.Write(data)
		_ = client.Close()
	}()
	t.Cleanup(func() { _ = server.Close() })
	return server
}

// TestMultipleRewinds — то, ради чего пакет переписан: диспетчер перебирает
// протоколы один за другим, и каждая неудачная попытка должна оставлять поток
// нетронутым для следующей.
func TestMultipleRewinds(t *testing.T) {
	const payload = "первая попытка съедает начало, вторая должна увидеть то же самое"

	conn := New(pipeWith(t, []byte(payload)))

	head := make([]byte, 20)
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := io.ReadFull(conn, head); err != nil {
			t.Fatalf("попытка %d: чтение: %v", attempt, err)
		}
		if string(head) != payload[:20] {
			t.Fatalf("попытка %d: получено %q", attempt, head)
		}
		if err := conn.Rewind(); err != nil {
			t.Fatalf("попытка %d: отмотка: %v", attempt, err)
		}
	}

	// После последней отмотки поток должен читаться целиком и без пропусков.
	got, err := io.ReadAll(conn)
	if err != nil && err != io.EOF {
		t.Fatalf("дочитывание: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("получено %q, ожидалось %q", got, payload)
	}
}

// TestCommitKeepsUnreadTail: после опознания протокола хвост записи не должен
// потеряться — его ещё предстоит разобрать самому протоколу.
func TestCommitKeepsUnreadTail(t *testing.T) {
	conn := New(pipeWith(t, []byte("ЗАГОЛОВОКполезные данные")))

	head := make([]byte, len("ЗАГОЛОВОК"))
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("чтение заголовка: %v", err)
	}
	if err := conn.Rewind(); err != nil {
		t.Fatalf("отмотка: %v", err)
	}

	// Заново читаем заголовок и фиксируем выбор.
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("повторное чтение заголовка: %v", err)
	}
	conn.Commit()

	rest, err := io.ReadAll(conn)
	if err != nil && err != io.EOF {
		t.Fatalf("чтение остатка: %v", err)
	}
	if string(rest) != "полезные данные" {
		t.Fatalf("после Commit получено %q", rest)
	}
}

// TestRewindAfterOverflow: если гость молча залил больше лимита, отматывать
// нечего — но соединение должно остаться живым, а не паниковать.
func TestRewindAfterOverflow(t *testing.T) {
	payload := bytes.Repeat([]byte{'x'}, 200)
	conn := NewLimited(pipeWith(t, payload), 64)

	buf := make([]byte, 200)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if err := conn.Rewind(); err != ErrLimitExceeded {
		t.Fatalf("ожидался ErrLimitExceeded, получено: %v", err)
	}
}

// TestCommitWithoutRewind: обычный путь, когда протокол опознан с первой
// попытки, тоже не должен терять данные.
func TestCommitWithoutRewind(t *testing.T) {
	conn := New(pipeWith(t, []byte("012345678901234567890123456789")))

	head := make([]byte, 10)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	conn.Commit()

	rest, err := io.ReadAll(conn)
	if err != nil && err != io.EOF {
		t.Fatalf("чтение остатка: %v", err)
	}
	if string(rest) != "01234567890123456789" {
		t.Fatalf("получено %q", rest)
	}
}
