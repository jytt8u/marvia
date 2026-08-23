// Package rewind даёт соединение, у которого прочитанное можно «отмотать»
// обратно и отдать другому обработчику.
//
// Зачем это нужно. Сервер не может заранее знать, кто к нему пришёл: наш
// клиент или браузер. Отличить их можно только попробовав разобрать первое
// сообщение протокола — а попытка съедает байты из потока. Если это оказался
// браузер, съеденные байты надо вернуть на место, иначе отдать его настоящему
// сайту уже не получится.
package rewind

import (
	"bytes"
	"errors"
	"net"
)

// DefaultLimit — сколько байт запоминать. Первое сообщение VP1 укладывается
// в 106 байт; запас нужен на случай, если транспорт отдаст данные крупным
// куском. Расти без ограничения буфер не должен: иначе любой, кто откроет
// соединение и будет молча слать мусор, съест нам память.
const DefaultLimit = 16 * 1024

// ErrLimitExceeded — записано больше, чем разрешено, отмотать нельзя.
var ErrLimitExceeded = errors.New("превышен лимит записи, отмотка невозможна")

// Conn запоминает прочитанное, пока включена запись.
type Conn struct {
	net.Conn

	limit     int
	recorded  bytes.Buffer
	replay    []byte
	recording bool
	overflow  bool
}

// New оборачивает соединение и включает запись.
func New(c net.Conn) *Conn { return NewLimited(c, DefaultLimit) }

// NewLimited позволяет задать свой лимит буфера.
func NewLimited(c net.Conn, limit int) *Conn {
	return &Conn{Conn: c, limit: limit, recording: true}
}

func (c *Conn) Read(p []byte) (int, error) {
	// Сначала отдаём отмотанное, только потом идём в сеть.
	if len(c.replay) > 0 {
		n := copy(p, c.replay)
		c.replay = c.replay[n:]
		return n, nil
	}

	n, err := c.Conn.Read(p)
	if n > 0 && c.recording {
		if c.recorded.Len()+n > c.limit {
			c.overflow = true
			c.recording = false
			c.recorded.Reset()
		} else {
			c.recorded.Write(p[:n])
		}
	}
	return n, err
}

// Rewind возвращает записанные байты в начало потока чтения и выключает запись.
func (c *Conn) Rewind() error {
	if c.overflow {
		return ErrLimitExceeded
	}
	c.recording = false
	// Буфер не сбрасываем: replay ссылается на его память.
	c.replay = c.recorded.Bytes()
	return nil
}

// Commit выключает запись и освобождает буфер. Вызывается, когда стало ясно,
// что отматывать не придётся.
func (c *Conn) Commit() {
	c.recording = false
	c.recorded.Reset()
}
