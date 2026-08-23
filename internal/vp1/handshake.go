package vp1

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/flynn/noise"
)

const (
	// Prologue подмешивается в хеш хендшейка с обеих сторон. Если строки не
	// совпали — хендшейк просто не сойдётся. Меняя её, мы намеренно ломаем
	// совместимость: это, по сути, версия протокола.
	Prologue = "veil/vp1"

	// maxHandshakeMessage — верхняя граница размера хендшейк-сообщения. Нужна,
	// чтобы случайный (или злонамеренный) мусор не заставил нас выделить
	// гигабайт памяти.
	maxHandshakeMessage = 1024

	// timestampLen — 8 байт unix-времени в наносекундах, big endian.
	timestampLen = 8

	// ClockSkew — допустимое расхождение часов клиента и сервера.
	ClockSkew = 2 * time.Minute

	// HandshakeTimeout — сколько ждём завершения хендшейка.
	HandshakeTimeout = 10 * time.Second
)

// ErrUnauthorized — клиент предъявил ключ, которого нет в списке разрешённых.
var ErrUnauthorized = errors.New("клиентский ключ не авторизован")

// Authorizer решает, пускать ли клиента с данным публичным ключом.
//
// Возвращает nil, если пускать. Причина отказа нужна только для журнала: сам
// клиент в любом случае получит сайт-прикрытие. Отвечать по-разному на «ключ
// не найден» и «квота исчерпана» нельзя — эта разница видна снаружи и
// превращается в способ прощупать ноду.
type Authorizer func(clientPub []byte) error

// AllowAll пускает любого клиента, знающего публичный ключ сервера.
// Годится для отладки; в бою нужен список ключей.
func AllowAll([]byte) error { return nil }

// ClientHandshake выполняет хендшейк со стороны клиента.
//
// Паттерн — Noise IK: клиент заранее знает статический публичный ключ сервера
// (I = Initiator шлёт свой статик, K = ключ ответчика Known). Отсюда два
// полезных свойства:
//
//   - хендшейк укладывается в один round-trip;
//   - тот, кто не знает публичный ключ сервера, не может даже начать разговор,
//     а сервер волен слить такое соединение куда угодно — на этом будет
//     построена защита от active probing на этапе M1.
func ClientHandshake(conn net.Conn, static KeyPair, serverPub []byte) (*Conn, error) {
	if len(serverPub) != KeyLen {
		return nil, fmt.Errorf("длина публичного ключа сервера %d байт, ожидается %d", len(serverPub), KeyLen)
	}

	if err := conn.SetDeadline(time.Now().Add(HandshakeTimeout)); err != nil {
		return nil, fmt.Errorf("установка дедлайна: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   CipherSuite,
		Random:        rand.Reader,
		Pattern:       noise.HandshakeIK,
		Initiator:     true,
		Prologue:      []byte(Prologue),
		StaticKeypair: static.noiseKey(),
		PeerStatic:    serverPub,
	})
	if err != nil {
		return nil, fmt.Errorf("инициализация хендшейка: %w", err)
	}

	// msg1: e, es, s, ss + payload. В payload кладём время — по нему сервер
	// отсекает повторы (см. ReplayGuard) — и добивку случайной длины, чтобы
	// у сообщения не было постоянного размера (см. padding.go).
	stamp := make([]byte, timestampLen)
	binary.BigEndian.PutUint64(stamp, uint64(time.Now().UnixNano()))

	msg1, _, _, err := hs.WriteMessage(nil, packPadded(stamp, handshakePad()))
	if err != nil {
		return nil, fmt.Errorf("формирование msg1: %w", err)
	}
	if err := writeFrame(conn, msg1); err != nil {
		return nil, fmt.Errorf("отправка msg1: %w", err)
	}

	// msg2: e, ee, se — после него обе стороны получают транспортные ключи.
	msg2, err := readFrame(conn, maxHandshakeMessage)
	if err != nil {
		return nil, fmt.Errorf("чтение msg2: %w", err)
	}
	_, csSend, csRecv, err := hs.ReadMessage(nil, msg2)
	if err != nil {
		return nil, fmt.Errorf("разбор msg2: %w", err)
	}
	if csSend == nil || csRecv == nil {
		return nil, errors.New("хендшейк не завершился выдачей транспортных ключей")
	}

	// У инициатора первый CipherState шифрует исходящий поток, второй —
	// расшифровывает входящий.
	return newConn(conn, csSend, csRecv), nil
}

// ServerHandshake выполняет хендшейк со стороны сервера. Возвращает готовое
// соединение и публичный ключ клиента — по нему дальше считаются трафик,
// лимиты и логи.
func ServerHandshake(conn net.Conn, static KeyPair, guard *ReplayGuard, allow Authorizer) (*Conn, []byte, error) {
	if allow == nil {
		allow = AllowAll
	}

	if err := conn.SetDeadline(time.Now().Add(HandshakeTimeout)); err != nil {
		return nil, nil, fmt.Errorf("установка дедлайна: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	hs, err := newServerState(static)
	if err != nil {
		return nil, nil, fmt.Errorf("инициализация хендшейка: %w", err)
	}

	msg1, err := readFrame(conn, maxHandshakeMessage)
	if err != nil {
		return nil, nil, fmt.Errorf("чтение msg1: %w", err)
	}
	raw, _, _, err := hs.ReadMessage(nil, msg1)
	if err != nil {
		// Сюда попадает всё, что не знает наш ключ: сканеры, боты, случайные
		// соединения. Вызывающий отдаёт таких гостей сайту-прикрытию, а не
		// рвёт соединение (см. internal/fallback).
		return nil, nil, fmt.Errorf("разбор msg1: %w", err)
	}
	payload, err := unpackPadded(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("разбор payload msg1: %w", err)
	}
	if len(payload) != timestampLen {
		return nil, nil, fmt.Errorf("длина payload msg1 %d байт, ожидается %d", len(payload), timestampLen)
	}

	stamp := time.Unix(0, int64(binary.BigEndian.Uint64(payload)))
	if err := guard.Check(msg1, stamp); err != nil {
		return nil, nil, err
	}

	clientPub := hs.PeerStatic()
	if len(clientPub) != KeyLen {
		return nil, nil, errors.New("клиент не предъявил статический ключ")
	}
	if err := allow(clientPub); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnauthorized, err)
	}

	msg2, csRecv, csSend, err := hs.WriteMessage(nil, packPadded(nil, handshakePad()))
	if err != nil {
		return nil, nil, fmt.Errorf("формирование msg2: %w", err)
	}
	if csSend == nil || csRecv == nil {
		return nil, nil, errors.New("хендшейк не завершился выдачей транспортных ключей")
	}
	if err := writeFrame(conn, msg2); err != nil {
		return nil, nil, fmt.Errorf("отправка msg2: %w", err)
	}

	// У ответчика первый CipherState принадлежит инициатору: им клиент шифрует
	// то, что мы читаем. Второй — наш исходящий поток.
	return newConn(conn, csSend, csRecv), append([]byte(nil), clientPub...), nil
}

// writeFrame пишет длину (uint16 big endian) и тело.
//
// На M0 это самый простой каркас, какой бывает. Он же — самая заметная для DPI
// часть протокола: фиксированные размеры первых двух пакетов плюс поток вида
// «2 байта длины + шум высокой энтропии» классификатор узнаёт на раз. Именно
// этот слой заменит камуфляж на M1.
func writeFrame(w io.Writer, body []byte) error {
	if len(body) > 0xFFFF {
		return fmt.Errorf("кадр длиной %d байт не помещается в uint16", len(body))
	}
	buf := make([]byte, 2+len(body))
	binary.BigEndian.PutUint16(buf[:2], uint16(len(body)))
	copy(buf[2:], body)
	_, err := w.Write(buf)
	return err
}

// readFrame читает кадр, ограничивая длину сверху.
func readFrame(r io.Reader, max int) ([]byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(head[:]))
	if n == 0 {
		return nil, errors.New("кадр нулевой длины")
	}
	if n > max {
		return nil, fmt.Errorf("кадр длиной %d байт превышает лимит %d", n, max)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// newServerState собирает состояние хендшейка ответчика.
//
// Вынесено отдельно не ради красоты: на M1 сюда добавится ветка, где мы, не
// сумев расшифровать msg1, отдаём соединение обратному прокси на настоящий
// сайт — и собирать состояние придётся до того, как решение принято.
func newServerState(static KeyPair) (*noise.HandshakeState, error) {
	return noise.NewHandshakeState(noise.Config{
		CipherSuite:   CipherSuite,
		Random:        rand.Reader,
		Pattern:       noise.HandshakeIK,
		Initiator:     false,
		Prologue:      []byte(Prologue),
		StaticKeypair: static.noiseKey(),
	})
}
