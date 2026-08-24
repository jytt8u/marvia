// Package rewind даёт соединение, у которого прочитанное можно «отмотать»
// обратно и прочитать заново.
//
// Зачем это нужно. Нода не знает заранее, кто к ней пришёл: наш клиент,
// чужой клиент по VLESS или Trojan, браузер, сканер цензора. Отличить их
// можно только заглянув в первые байты — а взгляд съедает их из потока.
// Если догадка не подтвердилась, байты надо вернуть на место и попробовать
// следующий вариант, а в самом конце отдать гостя настоящему сайту.
package rewind

import (
	"bytes"
	"errors"
	"net"
)

// DefaultLimit — сколько байт запоминать. Самое длинное, что мы разбираем до
// опознания, — первое сообщение VP1 (до 363 байт на проводе). Запас нужен на
// случай, если транспорт отдаст данные крупным куском. Расти без ограничения
// буфер не должен: иначе любой, кто откроет соединение и будет молча слать
// мусор, съест нам память.
const DefaultLimit = 16 * 1024

// ErrLimitExceeded — записано больше, чем разрешено, отмотать нельзя.
var ErrLimitExceeded = errors.New("превышен лимит записи, отмотка невозможна")

// Conn запоминает прочитанное, пока включена запись, и умеет отматывать
// поток к началу сколько угодно раз.
type Conn struct {
	net.Conn

	limit     int
	recorded  bytes.Buffer
	pos       int // сколько байт из записанного уже отдано вызывающему
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
	// Сначала отдаём то, что уже записано и не дочитано после отмотки.
	if c.pos < c.recorded.Len() {
		n := copy(p, c.recorded.Bytes()[c.pos:])
		c.pos += n
		return n, nil
	}

	n, err := c.Conn.Read(p)
	if n > 0 && c.recording {
		if c.recorded.Len()+n > c.limit {
			// Дальше отматывать не дадим, но поток продолжает работать:
			// к этому моменту протокол уже должен быть опознан.
			c.overflow = true
			c.recording = false
			c.recorded.Reset()
			c.pos = 0
		} else {
			c.recorded.Write(p[:n])
			c.pos = c.recorded.Len()
		}
	}
	return n, err
}

// Rewind возвращает чтение к началу записанного. Запись при этом продолжается,
// поэтому отматывать можно многократно — ровно это и нужно диспетчеру,
// перебирающему протоколы один за другим.
func (c *Conn) Rewind() error {
	if c.overflow {
		return ErrLimitExceeded
	}
	c.pos = 0
	return nil
}

// Commit выключает запись: протокол опознан, отматывать больше не придётся.
// Ещё не отданный хвост записи сохраняется — его дочитает уже сам протокол.
func (c *Conn) Commit() {
	c.recording = false
	if c.pos > 0 {
		rest := append([]byte(nil), c.recorded.Bytes()[c.pos:]...)
		c.recorded.Reset()
		c.recorded.Write(rest)
		c.pos = 0
	}
}

// Buffered сообщает, сколько байт уже записано. Нужно тестам и диагностике.
func (c *Conn) Buffered() int { return c.recorded.Len() }
