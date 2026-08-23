package vp1

import (
	"net"
	"testing"
)

// Границы размеров сообщений хендшейка.
//
//	msg1 = e(32) + статик клиента(32+16) + payload(2+8+добивка+16)
//	msg2 = e(32) + payload(2+добивка+16)
const (
	minMsg1 = 106
	maxMsg1 = minMsg1 + maxHandshakePad
	minMsg2 = 50
	maxMsg2 = minMsg2 + maxHandshakePad
)

// TestHandshakeSizesVary — тест на отсутствие отпечатка.
//
// До появления добивки msg1 всегда был ровно 104 байта, а msg2 ровно 48.
// Такую пару DPI ловит простым правилом, без всякого машинного обучения.
// Тест проверяет, что размеры теперь плавают и остаются в разумных границах.
func TestHandshakeSizesVary(t *testing.T) {
	const rounds = 40

	seen1 := make(map[int]struct{})
	seen2 := make(map[int]struct{})

	for i := 0; i < rounds; i++ {
		got1, got2 := measureHandshake(t)

		if got1 < minMsg1 || got1 > maxMsg1 {
			t.Fatalf("msg1 = %d байт, ожидался диапазон %d..%d", got1, minMsg1, maxMsg1)
		}
		if got2 < minMsg2 || got2 > maxMsg2 {
			t.Fatalf("msg2 = %d байт, ожидался диапазон %d..%d", got2, minMsg2, maxMsg2)
		}
		seen1[got1] = struct{}{}
		seen2[got2] = struct{}{}
	}

	// Порог намеренно мягкий: тест ловит «размер снова стал постоянным»,
	// а не проверяет качество распределения.
	if len(seen1) < rounds/4 {
		t.Fatalf("за %d хендшейков msg1 принял всего %d разных размеров — добивка не работает", rounds, len(seen1))
	}
	if len(seen2) < rounds/4 {
		t.Fatalf("за %d хендшейков msg2 принял всего %d разных размеров — добивка не работает", rounds, len(seen2))
	}
}

// measureHandshake проводит один хендшейк и возвращает размеры сообщений.
func measureHandshake(t *testing.T) (msg1Len, msg2Len int) {
	t.Helper()

	serverKey, _ := GenerateKeyPair()
	clientKey, _ := GenerateKeyPair()

	c1, c2 := net.Pipe()
	defer c1.Close()

	type result struct {
		sizes [2]int
		err   error
	}
	done := make(chan result, 1)

	go func() {
		defer c2.Close()

		msg1, err := readFrame(c2, maxHandshakeMessage)
		if err != nil {
			done <- result{err: err}
			return
		}
		hs, err := newServerState(serverKey)
		if err != nil {
			done <- result{err: err}
			return
		}
		if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
			done <- result{err: err}
			return
		}
		msg2, _, _, err := hs.WriteMessage(nil, packPadded(nil, handshakePad()))
		if err != nil {
			done <- result{err: err}
			return
		}
		if err := writeFrame(c2, msg2); err != nil {
			done <- result{err: err}
			return
		}
		done <- result{sizes: [2]int{len(msg1), len(msg2)}}
	}()

	if _, err := ClientHandshake(c1, clientKey, serverKey.Public); err != nil {
		t.Fatalf("хендшейк клиента: %v", err)
	}
	res := <-done
	if res.err != nil {
		t.Fatalf("сторона сервера: %v", res.err)
	}
	return res.sizes[0], res.sizes[1]
}
