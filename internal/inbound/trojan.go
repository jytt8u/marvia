// Package inbound разбирает чужие протоколы, которые нода принимает наравне
// со своим.
//
// Смысл этого пакета — продуктовый, а не технический. Покупатель продавца уже
// сидит в v2rayNG или Hiddify, и заставить его переставить приложение — самое
// дорогое, что есть в переезде на новую платформу. Нода, которая понимает и
// VLESS, и Trojan, снимает эту стену: продавец переезжает на нашу панель, а
// его клиенты ничего не замечают.
//
// Цена честная и её надо понимать. У VLESS и Trojan нет собственной
// криптографии: они полагаются на TLS снаружи, а секрет клиента — это строка,
// которую он предъявляет целиком и которую сервер обязан знать. Ни взаимной
// аутентификации, ни разделения на публичную и приватную часть, ни защиты от
// повтора. Всё это есть только в VP1.
package inbound

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/veilproject/veil/internal/vp1"
)

// Формат запроса Trojan:
//
//	56 байт  hex(SHA-224(пароль))
//	 2 байта CRLF
//	 1 байт  команда: 1 CONNECT, 3 UDP ASSOCIATE
//	 1 байт  тип адреса, как в SOCKS5
//	 …       адрес
//	 2 байта порт
//	 2 байта CRLF
//	 …       полезная нагрузка
//
// Ответного заголовка нет: сразу после запроса идут данные в обе стороны.
const (
	trojanHexLen = 56
	trojanCmdTCP = 0x01
	trojanCmdUDP = 0x03
)

// TrojanHeaderLen — сколько байт нужно прочитать, чтобы опознать Trojan.
const TrojanHeaderLen = trojanHexLen + 2

// ErrUDPNotSupported — клиент попросил UDP, а нода пока умеет только TCP.
var ErrUDPNotSupported = errors.New("UDP пока не поддерживается")

// TrojanRequest — разобранный запрос.
type TrojanRequest struct {
	// Digest — двоичный SHA-224 пароля. Именно он ищется в списке
	// пользователей; сам пароль по проводу не передаётся.
	Digest []byte

	// Target — куда клиент хочет подключиться.
	Target vp1.Address
}

// LooksLikeTrojan определяет по первым байтам, стоит ли пробовать Trojan.
//
// Признак надёжный: запрос начинается с 56 шестнадцатеричных цифр и CRLF.
// Ни VP1, ни VLESS так начинаться не могут — у обоих первый байт 0x00 или
// 0x01, а это не ASCII-символы шестнадцатеричной цифры.
func LooksLikeTrojan(head []byte) bool {
	if len(head) < TrojanHeaderLen {
		return false
	}
	for _, b := range head[:trojanHexLen] {
		if !isHexDigit(b) {
			return false
		}
	}
	return head[trojanHexLen] == '\r' && head[trojanHexLen+1] == '\n'
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// ReadTrojanRequest читает запрос Trojan с начала потока.
func ReadTrojanRequest(r io.Reader) (*TrojanRequest, error) {
	head := make([]byte, TrojanHeaderLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, fmt.Errorf("чтение пароля: %w", err)
	}
	if !LooksLikeTrojan(head) {
		return nil, errors.New("это не запрос Trojan")
	}

	digest, err := hex.DecodeString(string(head[:trojanHexLen]))
	if err != nil {
		return nil, fmt.Errorf("пароль не в шестнадцатеричном виде: %w", err)
	}

	var cmd [1]byte
	if _, err := io.ReadFull(r, cmd[:]); err != nil {
		return nil, fmt.Errorf("чтение команды: %w", err)
	}
	switch cmd[0] {
	case trojanCmdTCP:
	case trojanCmdUDP:
		return nil, ErrUDPNotSupported
	default:
		return nil, fmt.Errorf("неизвестная команда 0x%02x", cmd[0])
	}

	// Адрес в Trojan закодирован ровно как в SOCKS5, поэтому разбор общий.
	target, err := vp1.ReadAddress(r)
	if err != nil {
		return nil, fmt.Errorf("чтение адреса: %w", err)
	}

	var tail [2]byte
	if _, err := io.ReadFull(r, tail[:]); err != nil {
		return nil, fmt.Errorf("чтение конца запроса: %w", err)
	}
	if tail[0] != '\r' || tail[1] != '\n' {
		return nil, errors.New("запрос не заканчивается CRLF")
	}

	return &TrojanRequest{Digest: digest, Target: target}, nil
}
