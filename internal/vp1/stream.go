package vp1

import (
	"fmt"
	"io"
)

// Коды ответа сервера на запрос соединения.
const (
	StatusOK          byte = 0x00 // соединение с целью установлено
	StatusUnreachable byte = 0x01 // не смогли подключиться к цели
	StatusForbidden   byte = 0x02 // цель запрещена политикой сервера
	StatusInternal    byte = 0x03 // внутренняя ошибка сервера
)

// StatusText переводит код ответа в текст для логов.
func StatusText(code byte) string {
	switch code {
	case StatusOK:
		return "успех"
	case StatusUnreachable:
		return "цель недоступна"
	case StatusForbidden:
		return "цель запрещена"
	case StatusInternal:
		return "внутренняя ошибка сервера"
	default:
		return fmt.Sprintf("неизвестный код 0x%02x", code)
	}
}

// WriteRequest отправляет серверу адрес, к которому нужно подключиться.
// Это первый кадр после хендшейка.
func WriteRequest(w io.Writer, addr Address) error {
	raw, err := addr.Marshal()
	if err != nil {
		return err
	}
	// Один Write — один кадр: адрес не должен размазываться по нескольким
	// пакетам, иначе его длину видно по таймингам.
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("отправка запроса: %w", err)
	}
	return nil
}

// ReadRequest читает адрес назначения из первого кадра клиента.
func ReadRequest(r io.Reader) (Address, error) {
	return ReadAddress(r)
}

// WriteStatus сообщает клиенту, удалось ли подключиться к цели.
//
// Без этого ответа клиент был бы вынужден рапортовать браузеру «соединение
// установлено» ещё до того, как сервер вообще попробовал дозвониться до
// целевого хоста, — и все ошибки выглядели бы как молчаливый обрыв.
func WriteStatus(w io.Writer, code byte) error {
	if _, err := w.Write([]byte{code}); err != nil {
		return fmt.Errorf("отправка статуса: %w", err)
	}
	return nil
}

// ReadStatus читает ответ сервера.
func ReadStatus(r io.Reader) (byte, error) {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, fmt.Errorf("чтение статуса: %w", err)
	}
	return buf[0], nil
}
