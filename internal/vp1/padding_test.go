package vp1

import (
	"bytes"
	"crypto/rand"
	"net"
	"testing"
)

func TestPaddingRoundTrip(t *testing.T) {
	cases := [][]byte{
		{},
		[]byte("a"),
		[]byte("обычный запрос к example.com"),
		bytes.Repeat([]byte{0xAB}, MaxPayload),
	}

	for _, content := range cases {
		for _, pad := range []int{0, 1, 17, maxFramePad} {
			packed := packPadded(content, pad)
			if len(packed) != frameHeaderLen+len(content)+pad {
				t.Fatalf("длина упакованного кадра %d, ожидалась %d", len(packed), frameHeaderLen+len(content)+pad)
			}
			got, err := unpackPadded(packed)
			if err != nil {
				t.Fatalf("распаковка (len=%d, pad=%d): %v", len(content), pad, err)
			}
			if !bytes.Equal(got, content) {
				t.Fatalf("получено %q, ожидалось %q", got, content)
			}
		}
	}
}

func TestUnpackPaddedRejectsGarbage(t *testing.T) {
	if _, err := unpackPadded([]byte{0x01}); err == nil {
		t.Fatal("кадр короче заголовка должен отвергаться")
	}
	// Заявлено 500 байт содержимого, а в кадре всего два байта после заголовка.
	if _, err := unpackPadded([]byte{0x01, 0xF4, 0x00, 0x00}); err == nil {
		t.Fatal("кадр с завышенной длиной содержимого должен отвергаться")
	}
}

func TestFramePadBounds(t *testing.T) {
	for _, n := range []int{0, 1, smallFrame - 1, smallFrame, mediumFrame - 1, mediumFrame, MaxPayload} {
		for i := 0; i < 200; i++ {
			pad := framePad(n)
			if pad < 0 || pad > maxFramePad {
				t.Fatalf("для кадра %d байт добивка %d вне границ 0..%d", n, pad, maxFramePad)
			}
			if frameHeaderLen+n+pad > MaxPlaintext {
				t.Fatalf("кадр %d + добивка %d не влезает в %d", n, pad, MaxPlaintext)
			}
		}
	}
}

// TestFrameSizesVary проверяет, что одинаковые полезные нагрузки уезжают
// кадрами разной длины. Без этого структура вложенного протокола просвечивает
// сквозь наш — именно так ловят TLS внутри TLS.
func TestFrameSizesVary(t *testing.T) {
	client, server, _ := handshakePair(t, AllowAll)

	const rounds = 40
	payload := []byte("GET / HTTP/1.1\r\n") // короткий кадр — самый опасный случай

	go func() {
		for i := 0; i < rounds; i++ {
			_, _ = client.Write(payload)
		}
	}()

	sizes := make(map[int]struct{})
	buf := make([]byte, len(payload))
	for i := 0; i < rounds; i++ {
		// Читаем сырой кадр с транспорта, минуя расшифровку, чтобы увидеть
		// именно то, что уходит в сеть.
		_ = buf
		n, err := readFrame(server.Conn, MaxPlaintext+tagLen)
		if err != nil {
			t.Fatalf("чтение кадра: %v", err)
		}
		sizes[len(n)] = struct{}{}
	}

	if len(sizes) < rounds/4 {
		t.Fatalf("за %d кадров встретилось всего %d разных размеров — добивка не работает", rounds, len(sizes))
	}
}

// TestPaddingSurvivesLargePayload: добивка не должна ломать передачу больших
// объёмов и не должна раздувать их сверх лимита кадра.
func TestPaddingSurvivesLargePayload(t *testing.T) {
	client, server, _ := handshakePair(t, AllowAll)

	payload := make([]byte, MaxPayload*3+17)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("генерация данных: %v", err)
	}

	go func() {
		_, _ = client.Write(payload)
	}()

	got := make([]byte, len(payload))
	if _, err := readFull(server, got); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("данные не совпали после прохода через добивку")
	}
}

// readFull — io.ReadFull без импорта io в тесте с net.Pipe.
func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
