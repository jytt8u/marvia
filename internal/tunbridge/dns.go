package tunbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Перевод запроса имени с UDP на TCP.
//
// Приложения на телефоне спрашивают имена по UDP, а наш протокол UDP пока не
// проксирует. Оставить запросы идти мимо туннеля нельзя: список посещённых
// сайтов — ровно то, что цензор и хочет узнать, и получить его из открытых
// запросов имён проще, чем расшифровывать трафик.
//
// Выход штатный: тот же самый запрос по TCP. Это не обходной манёвр, а часть
// стандарта — так работает любой резолвер, когда ответ не влезает в датаграмму.
// Разница только в двухбайтовом заголовке длины перед сообщением.

const (
	// maxDNSMessage — верхняя граница размера сообщения. Больше в заголовок
	// длины всё равно не поместится.
	maxDNSMessage = 65535

	// minDNSMessage — заголовок сообщения по стандарту занимает 12 байт.
	// Всё, что короче, — не запрос.
	minDNSMessage = 12
)

// ErrShortDNSMessage — сообщение короче обязательного заголовка.
var ErrShortDNSMessage = errors.New("сообщение короче заголовка DNS")

// ExchangeOverTCP отправляет запрос по потоку и возвращает ответ.
func ExchangeOverTCP(stream io.ReadWriter, query []byte) ([]byte, error) {
	if len(query) < minDNSMessage {
		return nil, fmt.Errorf("%w: %d байт", ErrShortDNSMessage, len(query))
	}
	if len(query) > maxDNSMessage {
		return nil, fmt.Errorf("запрос длиной %d байт не помещается в заголовок длины", len(query))
	}

	// Заголовок и сообщение уходят одной записью: разбивать их на две — значит
	// рисовать на проводе лишнюю пару мелких пакетов там, где её не бывает.
	framed := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(framed[:2], uint16(len(query)))
	copy(framed[2:], query)

	if _, err := stream.Write(framed); err != nil {
		return nil, fmt.Errorf("отправка запроса: %w", err)
	}

	var head [2]byte
	if _, err := io.ReadFull(stream, head[:]); err != nil {
		return nil, fmt.Errorf("чтение длины ответа: %w", err)
	}
	size := int(binary.BigEndian.Uint16(head[:]))
	if size < minDNSMessage {
		return nil, fmt.Errorf("%w: ответ %d байт", ErrShortDNSMessage, size)
	}

	answer := make([]byte, size)
	if _, err := io.ReadFull(stream, answer); err != nil {
		return nil, fmt.Errorf("чтение ответа: %w", err)
	}
	return answer, nil
}
