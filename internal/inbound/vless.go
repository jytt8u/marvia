package inbound

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/veilproject/veil/internal/vp1"
)

// Формат запроса VLESS:
//
//	 1 байт  версия, всегда 0
//	16 байт  UUID
//	 1 байт  длина дополнений M
//	 M байт  дополнения
//	 1 байт  команда: 1 TCP, 2 UDP, 3 mux
//	 2 байта порт, big endian
//	 1 байт  тип адреса: 1 IPv4, 2 домен, 3 IPv6
//	 …       адрес
//	 …       полезная нагрузка
//
// Ответ сервера: 1 байт версии и 1 байт длины дополнений, затем данные.
//
// Обрати внимание на две ловушки. Порт идёт до адреса, а не после, как в
// SOCKS5. И типы адреса пронумерованы иначе: домен здесь 2, а не 3.
const (
	vlessVersion = 0x00

	vlessCmdTCP = 0x01
	vlessCmdUDP = 0x02
	vlessCmdMux = 0x03

	vlessAtypIPv4   = 0x01
	vlessAtypDomain = 0x02
	vlessAtypIPv6   = 0x03

	vlessUUIDLen = 16
)

// VLESSPrefixLen — сколько байт нужно, чтобы достать UUID и решить, наш ли это
// клиент. Версия плюс UUID, больше для опознания не требуется.
const VLESSPrefixLen = 1 + vlessUUIDLen

// VLESSRequest — разобранный запрос.
type VLESSRequest struct {
	UUID   []byte
	Target vp1.Address
}

// PeekVLESSUUID достаёт UUID из начала потока, ничего больше не разбирая.
//
// Отдельный шаг нужен из-за неприятного совпадения: у VLESS первый байт равен
// нулю (версия), и у VP1 первый байт тоже часто ноль — это старший байт длины
// первого сообщения. Различить их по форме нельзя, поэтому решает поиск UUID
// в списке пользователей: нашли — VLESS, не нашли — пробуем VP1.
//
// Читается ровно 17 байт. Это важно: любой из наших протоколов присылает
// столько сразу, и чтение не подвиснет в ожидании данных, которых нет.
func PeekVLESSUUID(r io.Reader) ([]byte, error) {
	head := make([]byte, VLESSPrefixLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}
	if head[0] != vlessVersion {
		return nil, errors.New("это не VLESS: версия не нулевая")
	}
	return head[1:], nil
}

// ReadVLESSRequest читает запрос VLESS с начала потока.
func ReadVLESSRequest(r io.Reader) (*VLESSRequest, error) {
	uuid, err := PeekVLESSUUID(r)
	if err != nil {
		return nil, fmt.Errorf("чтение UUID: %w", err)
	}

	var addonLen [1]byte
	if _, err := io.ReadFull(r, addonLen[:]); err != nil {
		return nil, fmt.Errorf("чтение длины дополнений: %w", err)
	}
	if addonLen[0] > 0 {
		// Дополнения нам сейчас нечего обрабатывать, но пропустить их надо:
		// иначе следующие поля прочитаются со сдвигом.
		if _, err := io.CopyN(io.Discard, r, int64(addonLen[0])); err != nil {
			return nil, fmt.Errorf("пропуск дополнений: %w", err)
		}
	}

	var cmd [1]byte
	if _, err := io.ReadFull(r, cmd[:]); err != nil {
		return nil, fmt.Errorf("чтение команды: %w", err)
	}
	switch cmd[0] {
	case vlessCmdTCP:
	case vlessCmdUDP:
		return nil, ErrUDPNotSupported
	case vlessCmdMux:
		return nil, errors.New("mux.cool не поддерживается")
	default:
		return nil, fmt.Errorf("неизвестная команда 0x%02x", cmd[0])
	}

	var portRaw [2]byte
	if _, err := io.ReadFull(r, portRaw[:]); err != nil {
		return nil, fmt.Errorf("чтение порта: %w", err)
	}
	port := binary.BigEndian.Uint16(portRaw[:])

	var atyp [1]byte
	if _, err := io.ReadFull(r, atyp[:]); err != nil {
		return nil, fmt.Errorf("чтение типа адреса: %w", err)
	}

	var target vp1.Address
	switch atyp[0] {
	case vlessAtypIPv4:
		var raw [4]byte
		if _, err := io.ReadFull(r, raw[:]); err != nil {
			return nil, fmt.Errorf("чтение IPv4: %w", err)
		}
		target = vp1.Address{Type: vp1.AtypIPv4, Host: net.IP(raw[:]).String(), Port: port}
	case vlessAtypIPv6:
		var raw [16]byte
		if _, err := io.ReadFull(r, raw[:]); err != nil {
			return nil, fmt.Errorf("чтение IPv6: %w", err)
		}
		target = vp1.Address{Type: vp1.AtypIPv6, Host: net.IP(raw[:]).String(), Port: port}
	case vlessAtypDomain:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return nil, fmt.Errorf("чтение длины домена: %w", err)
		}
		if l[0] == 0 {
			return nil, errors.New("пустое доменное имя")
		}
		host := make([]byte, l[0])
		if _, err := io.ReadFull(r, host); err != nil {
			return nil, fmt.Errorf("чтение домена: %w", err)
		}
		target = vp1.Address{Type: vp1.AtypDomain, Host: string(host), Port: port}
	default:
		return nil, fmt.Errorf("неизвестный тип адреса 0x%02x", atyp[0])
	}

	return &VLESSRequest{UUID: uuid, Target: target}, nil
}

// VLESSConn дописывает ответный заголовок к первой же порции данных.
//
// Заголовок всего два байта, и отправить его отдельным пакетом было бы
// проще — но тогда каждое соединение начиналось бы с крошечной TLS-записи
// в два байта. Такая запись сама по себе примета: обычный HTTPS так себя
// не ведёт. Приклеиваем заголовок к первой полезной порции.
type VLESSConn struct {
	net.Conn
	once sync.Once
	err  error
}

// NewVLESSConn оборачивает соединение с клиентом.
func NewVLESSConn(c net.Conn) *VLESSConn { return &VLESSConn{Conn: c} }

func (c *VLESSConn) Write(p []byte) (int, error) {
	var sent bool
	c.once.Do(func() {
		buf := make([]byte, 0, 2+len(p))
		buf = append(buf, vlessVersion, 0x00) // версия и пустые дополнения
		buf = append(buf, p...)

		n, err := c.Conn.Write(buf)
		c.err = err
		sent = true
		if err == nil && n < len(buf) {
			c.err = io.ErrShortWrite
		}
	})
	if sent {
		if c.err != nil {
			return 0, c.err
		}
		return len(p), nil
	}
	if c.err != nil {
		return 0, c.err
	}
	return c.Conn.Write(p)
}

// CloseWrite пробрасывает полузакрытие, если транспорт его умеет.
func (c *VLESSConn) CloseWrite() error {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return errors.New("транспорт не умеет CloseWrite")
}
