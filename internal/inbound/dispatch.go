package inbound

import (
	"fmt"
	"io"

	"github.com/veilproject/veil/internal/rewind"
)

// Protocol — что нода собирается разбирать в этом соединении.
type Protocol int

const (
	ProtoUnknown Protocol = iota
	ProtoVP1
	ProtoVLESS
	ProtoTrojan
)

func (p Protocol) String() string {
	switch p {
	case ProtoVP1:
		return "vp1"
	case ProtoVLESS:
		return "vless"
	case ProtoTrojan:
		return "trojan"
	default:
		return "неопознанный"
	}
}

// KnownVLESS отвечает, есть ли такой UUID в списке пользователей.
type KnownVLESS func(uuid []byte) bool

// Classify заглядывает в начало потока и решает, какой протокол пробовать.
// Поток после вызова всегда отмотан к началу.
//
// Различение построено на первом байте:
//
//	'0'…'f'  — шестнадцатеричная цифра, так начинается только Trojan
//	0x01     — старший байт длины первого сообщения VP1 (256…361 байт)
//	0x00     — либо версия VLESS, либо старший байт длины VP1 (106…255)
//
// Последний случай по форме неразличим, и решает его поиск UUID в списке
// пользователей. Вероятность, что случайные байты хендшейка VP1 совпадут с
// чьим-то UUID, — одна на 2¹²⁸, о ней можно не думать.
//
// Читается не больше 58 байт, и только когда это оправдано формой. Это не
// придирка к экономии: если запросить у клиента больше, чем он собирался
// прислать, чтение подвиснет до самого таймаута, и нода будет держать
// соединение впустую.
func Classify(conn *rewind.Conn, knownVLESS KnownVLESS) (Protocol, error) {
	defer func() { _ = conn.Rewind() }()

	head := make([]byte, VLESSPrefixLen)
	if _, err := io.ReadFull(conn, head); err != nil {
		return ProtoUnknown, fmt.Errorf("чтение начала потока: %w", err)
	}

	switch {
	case isHexDigit(head[0]):
		return classifyTrojan(conn)

	case head[0] == vlessVersion:
		if knownVLESS != nil && knownVLESS(head[1:]) {
			return ProtoVLESS, nil
		}
		return ProtoVP1, nil

	case head[0] == 0x01:
		return ProtoVP1, nil

	default:
		return ProtoUnknown, nil
	}
}

// classifyTrojan дочитывает заголовок и проверяет его форму.
//
// Проверка идёт по мере поступления байтов, а не после того, как наберутся все
// 58. Причина конкретная: запрос браузера `CONNECT host:443 HTTP/1.1` тоже
// начинается с шестнадцатеричной цифры — буквы C. Если ждать полный заголовок,
// такое соединение зависнет до самого таймаута, потому что браузер прислал
// всего три десятка байт и ждёт ответа. С поэтапной проверкой мы отсеиваем
// его на втором байте, где стоит не подходящая под шестнадцатеричную цифру
// буква O.
func classifyTrojan(conn *rewind.Conn) (Protocol, error) {
	if err := conn.Rewind(); err != nil {
		return ProtoUnknown, err
	}

	head := make([]byte, TrojanHeaderLen)
	read := 0
	for read < TrojanHeaderLen {
		n, err := conn.Read(head[read:])
		if n > 0 {
			limit := min(read+n, trojanHexLen)
			for i := read; i < limit; i++ {
				if !isHexDigit(head[i]) {
					return ProtoUnknown, nil
				}
			}
			read += n
		}
		if err != nil {
			// Соединение закончилось раньше заголовка — значит это не Trojan.
			return ProtoUnknown, nil
		}
	}

	if !LooksLikeTrojan(head) {
		return ProtoUnknown, nil
	}
	return ProtoTrojan, nil
}
