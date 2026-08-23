// Package socks5 реализует минимальный SOCKS5-инбаунд: столько, сколько нужно
// клиенту Veil, и ни байтом больше.
//
// SOCKS5 выбран точкой входа на M0 потому, что его понимают браузеры, curl,
// Telegram и почти всё остальное — можно проверить туннель, не трогая сетевой
// стек системы. Полноценный перехват всего трафика через TUN придёт на M2.
package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/veilproject/veil/internal/vp1"
)

const version byte = 0x05

// Методы аутентификации.
const (
	methodNoAuth       byte = 0x00
	methodNoAcceptable byte = 0xFF
)

// Команды.
const cmdConnect byte = 0x01

// Коды ответа.
const (
	replySuccess         byte = 0x00
	replyGeneralFailure  byte = 0x01
	replyHostUnreachable byte = 0x04
	replyCommandNotSupp  byte = 0x07
	replyAddrTypeNotSupp byte = 0x08
)

// ErrUnsupportedCommand — клиент попросил BIND или UDP ASSOCIATE.
var ErrUnsupportedCommand = errors.New("поддерживается только CONNECT")

// Handshake проводит согласование метода аутентификации и читает запрос.
// Возвращает адрес, к которому клиент хочет подключиться.
//
// Ответ клиенту здесь ещё не отправляется: сначала надо попробовать
// достучаться до цели через туннель, и только потом сказать «готово».
// Иначе браузер будет считать соединение установленным, когда его нет.
func Handshake(conn net.Conn) (vp1.Address, error) {
	if err := negotiate(conn); err != nil {
		return vp1.Address{}, err
	}
	return readRequest(conn)
}

func negotiate(conn net.Conn) error {
	var head [2]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return fmt.Errorf("чтение приветствия: %w", err)
	}
	if head[0] != version {
		return fmt.Errorf("версия SOCKS %d не поддерживается", head[0])
	}
	if head[1] == 0 {
		return errors.New("клиент не предложил ни одного метода аутентификации")
	}

	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("чтение списка методов: %w", err)
	}

	// Локальный инбаунд слушает только лупбек, поэтому аутентификация здесь
	// не нужна: до него дотянется лишь тот, кто уже сидит на этой машине.
	for _, m := range methods {
		if m == methodNoAuth {
			_, err := conn.Write([]byte{version, methodNoAuth})
			return err
		}
	}
	_, _ = conn.Write([]byte{version, methodNoAcceptable})
	return errors.New("клиент не предложил метод «без аутентификации»")
}

func readRequest(conn net.Conn) (vp1.Address, error) {
	var head [3]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return vp1.Address{}, fmt.Errorf("чтение запроса: %w", err)
	}
	if head[0] != version {
		return vp1.Address{}, fmt.Errorf("версия SOCKS %d не поддерживается", head[0])
	}
	if head[1] != cmdConnect {
		_ = Reply(conn, replyCommandNotSupp)
		return vp1.Address{}, ErrUnsupportedCommand
	}

	// Формат адреса в SOCKS5 совпадает с нашим, поэтому переиспользуем разбор.
	addr, err := vp1.ReadAddress(conn)
	if err != nil {
		_ = Reply(conn, replyAddrTypeNotSupp)
		return vp1.Address{}, err
	}
	return addr, nil
}

// Reply отправляет клиенту ответ на запрос CONNECT.
func Reply(conn net.Conn, code byte) error {
	// BND.ADDR/BND.PORT честно заполнять нечем: соединение к цели устанавливает
	// сервер на другом конце туннеля, его локальный адрес нас не касается.
	// Нули здесь — общепринятая практика, клиенты их не проверяют.
	resp := []byte{version, code, 0x00, vp1.AtypIPv4, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(resp[8:], 0)
	_, err := conn.Write(resp)
	return err
}

// ReplySuccess подтверждает, что туннель до цели установлен.
func ReplySuccess(conn net.Conn) error { return Reply(conn, replySuccess) }

// ReplyFailure сообщает об ошибке, стараясь выбрать осмысленный код.
func ReplyFailure(conn net.Conn, err error) error {
	code := replyGeneralFailure
	var netErr net.Error
	if errors.As(err, &netErr) {
		code = replyHostUnreachable
	}
	return Reply(conn, code)
}
