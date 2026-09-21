package vp1

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/flynn/noise"
)

const (
	// MaxPlaintext — сколько байт уезжает в один кадр после добивки.
	// 16 КиБ выбраны не случайно: столько же в TLS-рекорде, и наш трафик
	// не должен выделяться на общем фоне размером кадров.
	MaxPlaintext = 16384

	// tagLen — размер аутентификационного тега ChaCha20-Poly1305.
	tagLen = 16
)

// Conn — установленное шифрованное соединение поверх транспорта.
//
// Реализует net.Conn, поэтому дальше по коду его можно использовать как
// обычный сокет: io.Copy, дедлайны, адреса — всё работает.
type Conn struct {
	net.Conn

	writeMu sync.Mutex
	send    *noise.CipherState

	readMu  sync.Mutex
	recv    *noise.CipherState
	pending []byte // остаток расшифрованного кадра, не отданный вызывающему

	// Буферы на всё время жизни соединения: один под исходящий кадр, один под
	// входящий. Кадр собирается, шифруется и уходит в сеть в одном и том же
	// месте памяти, без единой аллокации на пути данных. До этого каждый кадр
	// стоил три выделения и две копии по 16 КиБ — на ноде с сотней
	// покупателей это был сборщик мусора в главной роли.
	wbuf []byte
	rbuf []byte

	closeOnce sync.Once
}

// frameCap — сколько места нужно под кадр с заголовком длины и тегом.
const frameCap = frameLenHeader + MaxPlaintext + tagLen

// frameLenHeader — заголовок кадра на проводе: длина шифротекста.
const frameLenHeader = 2

func newConn(transport net.Conn, send, recv *noise.CipherState) *Conn {
	return &Conn{
		Conn: transport,
		send: send,
		recv: recv,
		wbuf: make([]byte, frameCap),
		rbuf: make([]byte, MaxPlaintext+tagLen),
	}
}

// Read отдаёт расшифрованные данные без добивки.
//
// Read и Write защищены разными мьютексами и работают с разными
// CipherState — значит, читать и писать можно одновременно из двух горутин,
// как и положено net.Conn. А вот два параллельных Read друг друга бы сломали
// (счётчик nonce один на поток), поэтому мьютекс всё же нужен.
func (c *Conn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	// Кадр может целиком состоять из добивки — тогда читаем следующий.
	// Буфер один: pending указывает в него, и следующий кадр читается только
	// когда прежний отдан до конца.
	for len(c.pending) == 0 {
		frame, err := readFrameInto(c.Conn, c.rbuf)
		if err != nil {
			return 0, err
		}
		// Расшифровка на месте: AEAD разрешает dst, совпадающий с началом
		// шифротекста.
		plain, err := c.recv.Decrypt(frame[:0], nil, frame)
		if err != nil {
			// Расшифровка не прошла: либо кто-то поменял байты в потоке,
			// либо рассинхрон nonce. Продолжать нельзя — рвём соединение.
			_ = c.Conn.Close()
			return 0, fmt.Errorf("расшифровка кадра: %w", err)
		}
		payload, err := unpackPadded(plain)
		if err != nil {
			_ = c.Conn.Close()
			return 0, fmt.Errorf("разбор кадра: %w", err)
		}
		c.pending = payload
	}

	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

// Write шифрует и отправляет данные, разбивая их на кадры и добивая каждый
// до непредсказуемой длины.
func (c *Conn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	written := 0
	for len(p) > 0 {
		chunk := p
		// Крупный кусок идёт без добивки и занимает кадр целиком, как
		// TLS-рекорд при скачивании; мелкому и среднему оставлено место под
		// добивку. framePad для всего, что длиннее MaxPayload, даёт ноль,
		// так что кадр всегда помещается в wbuf.
		if len(chunk) > BulkPayload {
			chunk = chunk[:BulkPayload]
		}
		if err := c.writeFramed(chunk); err != nil {
			return written, err
		}
		written += len(chunk)
		p = p[len(chunk):]
	}
	return written, nil
}

func (c *Conn) writeFramed(chunk []byte) error {
	// Кадр собирается прямо в wbuf: [длина шифротекста][длина содержимого]
	// [содержимое][добивка] — и шифруется на месте, тег дописывается следом.
	pad := framePad(len(chunk))
	plainLen := frameHeaderLen + len(chunk) + pad
	body := c.wbuf[frameLenHeader : frameLenHeader+plainLen]
	binary.BigEndian.PutUint16(body[:frameHeaderLen], uint16(len(chunk)))
	copy(body[frameHeaderLen:], chunk)
	// Добивка — нули, и буфер переиспользуется: чистить обязательно, иначе
	// в добивку уедут байты прошлого кадра. Внутри AEAD их не видно, но
	// содержимое добивки — это обещание протокола, а не случайность.
	clear(body[frameHeaderLen+len(chunk):])

	sealed, err := c.send.Encrypt(body[:0], nil, body)
	if err != nil {
		return fmt.Errorf("шифрование кадра: %w", err)
	}
	binary.BigEndian.PutUint16(c.wbuf[:frameLenHeader], uint16(len(sealed)))
	_, err = c.Conn.Write(c.wbuf[:frameLenHeader+len(sealed)])
	return err
}

// CloseWrite закрывает исходящую половину соединения, оставляя входящую
// открытой. Нужно, чтобы корректно проксировать протоколы, где одна сторона
// говорит «я всё сказал» и продолжает слушать ответ.
func (c *Conn) CloseWrite() error {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return errors.New("транспорт не умеет CloseWrite")
}

// Close закрывает транспорт. Безопасно вызывать несколько раз.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.Conn.Close() })
	return err
}
