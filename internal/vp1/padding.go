package vp1

import (
	"encoding/binary"
	"fmt"
	mrand "math/rand/v2"
)

// Паддинг — добивка кадров мусором до непредсказуемой длины.
//
// Он решает две разные задачи, и обе для нас критичны.
//
// Первая: постоянные размеры. Без добивки первое сообщение хендшейка всегда
// ровно 104 байта, второе ровно 48. Такую пару DPI узнаёт без всякого
// машинного обучения — достаточно правила «106 туда, 50 обратно».
//
// Вторая, более коварная: TLS внутри TLS. Когда пользователь открывает
// HTTPS-сайт, его собственный TLS едет внутри нашего, и размеры наших записей
// оказываются жёстко связаны с размерами его записей — своя структура
// просвечивает сквозь чужую оболочку. Именно так GFW научился ловить Trojan и
// VLESS-over-TLS. Добивка разрывает эту связь.
//
// Содержимое добивки — нули. Внутри AEAD они неотличимы от любого другого
// содержимого, а генерировать случайные байты на каждый кадр дорого.
// Случайной должна быть длина, а не содержимое.

const (
	// frameHeaderLen — сколько байт занимает длина полезной части.
	frameHeaderLen = 2

	// maxFramePad — верхняя граница добивки одного кадра.
	maxFramePad = 1024

	// MaxPayload — сколько полезных данных влезает в кадр с учётом заголовка
	// и максимально возможной добивки.
	MaxPayload = MaxPlaintext - frameHeaderLen - maxFramePad

	// maxHandshakePad — верхняя граница добивки сообщений хендшейка.
	// Больше не нужно: задача — размыть постоянную длину, а не раздуть трафик.
	maxHandshakePad = 255

	// smallFrame — граница, ниже которой кадр считается мелким и добивается
	// сильнее всего. Мелкие кадры опаснее: именно в них видна структура
	// вложенного протокола.
	smallFrame = 256

	// mediumFrame — граница умеренной добивки.
	mediumFrame = 2048
)

// packPadded упаковывает содержимое в кадр с добивкой.
//
//	┌────────────┬──────────────┬──────────┐
//	│ uint16 BE  │  содержимое  │  добивка │
//	│   длина    │              │  (нули)  │
//	└────────────┴──────────────┴──────────┘
func packPadded(content []byte, pad int) []byte {
	out := make([]byte, frameHeaderLen+len(content)+pad)
	binary.BigEndian.PutUint16(out[:frameHeaderLen], uint16(len(content)))
	copy(out[frameHeaderLen:], content)
	return out
}

// unpackPadded достаёт содержимое, отбрасывая добивку.
// Возвращает срез исходного буфера, а не копию.
func unpackPadded(raw []byte) ([]byte, error) {
	if len(raw) < frameHeaderLen {
		return nil, fmt.Errorf("кадр длиной %d байт короче заголовка", len(raw))
	}
	n := int(binary.BigEndian.Uint16(raw[:frameHeaderLen]))
	if frameHeaderLen+n > len(raw) {
		return nil, fmt.Errorf("заявлено %d байт содержимого, а в кадре всего %d", n, len(raw)-frameHeaderLen)
	}
	return raw[frameHeaderLen : frameHeaderLen+n], nil
}

// framePad выбирает размер добивки для кадра с полезной частью длиной n.
//
// Крупные кадры не добиваем: они и так выглядят как обычная передача данных,
// а лишний трафик стоит денег и пользователю, и владельцу ноды.
func framePad(n int) int {
	switch {
	case n < smallFrame:
		return mrand.IntN(maxFramePad)
	case n < mediumFrame:
		return mrand.IntN(maxFramePad / 4)
	default:
		return 0
	}
}

// handshakePad выбирает размер добивки для сообщения хендшейка.
func handshakePad() int { return mrand.IntN(maxHandshakePad + 1) }
