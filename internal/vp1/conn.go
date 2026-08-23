package vp1

import (
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
	scratch []byte // переиспользуемый буфер под расшифровку

	closeOnce sync.Once
}

func newConn(transport net.Conn, send, recv *noise.CipherState) *Conn {
	return &Conn{
		Conn:    transport,
		send:    send,
		recv:    recv,
		scratch: make([]byte, 0, MaxPlaintext),
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
	for len(c.pending) == 0 {
		frame, err := readFrame(c.Conn, MaxPlaintext+tagLen)
		if err != nil {
			return 0, err
		}
		plain, err := c.recv.Decrypt(c.scratch[:0], nil, frame)
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
		c.scratch = plain[:0]
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
		if len(chunk) > MaxPayload {
			chunk = chunk[:MaxPayload]
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
	sealed, err := c.send.Encrypt(nil, nil, packPadded(chunk, framePad(len(chunk))))
	if err != nil {
		return fmt.Errorf("шифрование кадра: %w", err)
	}
	return writeFrame(c.Conn, sealed)
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
