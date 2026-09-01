package vp1

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
)

// Типы адреса назначения. Значения намеренно совпадают с ATYP из SOCKS5 —
// так запрос перекладывается из локального инбаунда в туннель без конвертации.
const (
	AtypIPv4   byte = 0x01
	AtypDomain byte = 0x03
	AtypIPv6   byte = 0x04
)

// Address — куда сервер должен подключиться от имени клиента.
//
// Важно: домен мы передаём доменом, а не резолвим на клиенте. Это не мелочь —
// DNS-запрос, ушедший в обход туннеля, выдаёт цензору всё, что ты открываешь,
// даже если сам трафик он расшифровать не может.
type Address struct {
	Type byte
	Host string // IP в текстовом виде либо доменное имя
	Port uint16
}

func (a Address) String() string {
	return net.JoinHostPort(a.Host, strconv.Itoa(int(a.Port)))
}

// AddressFromHostPort строит Address, сам определяя тип по виду хоста.
func AddressFromHostPort(host string, port uint16) (Address, error) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return Address{Type: AtypIPv4, Host: v4.String(), Port: port}, nil
		}
		return Address{Type: AtypIPv6, Host: ip.String(), Port: port}, nil
	}
	if len(host) == 0 || len(host) > 255 {
		return Address{}, fmt.Errorf("некорректная длина доменного имени: %d", len(host))
	}
	return Address{Type: AtypDomain, Host: host, Port: port}, nil
}

// Marshal сериализует адрес в компактный бинарный вид.
func (a Address) Marshal() ([]byte, error) {
	var buf []byte
	switch a.Type {
	case AtypIPv4:
		ip := net.ParseIP(a.Host)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("%q не является IPv4-адресом", a.Host)
		}
		buf = append(buf, AtypIPv4)
		buf = append(buf, ip.To4()...)
	case AtypIPv6:
		ip := net.ParseIP(a.Host)
		if ip == nil || ip.To16() == nil {
			return nil, fmt.Errorf("%q не является IPv6-адресом", a.Host)
		}
		buf = append(buf, AtypIPv6)
		buf = append(buf, ip.To16()...)
	case AtypDomain:
		if len(a.Host) == 0 || len(a.Host) > 255 {
			return nil, fmt.Errorf("некорректная длина доменного имени: %d", len(a.Host))
		}
		buf = append(buf, AtypDomain, byte(len(a.Host)))
		buf = append(buf, a.Host...)
	default:
		return nil, fmt.Errorf("неизвестный тип адреса 0x%02x", a.Type)
	}
	return binary.BigEndian.AppendUint16(buf, a.Port), nil
}

// ReadAddress читает адрес из потока (уже расшифрованного).
func ReadAddress(r io.Reader) (Address, error) {
	var head [1]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Address{}, fmt.Errorf("чтение типа адреса: %w", err)
	}
	return ReadAddressAfterType(r, head[0])
}

// ReadAddressAfterType дочитывает адрес, когда тип уже снят с потока.
//
// Нужен там, где по первому байту принимается решение до разбора адреса:
// в запросе клиента в нём же закодировано, TCP это или UDP.
func ReadAddressAfterType(r io.Reader, atyp byte) (Address, error) {
	var host string
	switch atyp {
	case AtypIPv4:
		var raw [4]byte
		if _, err := io.ReadFull(r, raw[:]); err != nil {
			return Address{}, fmt.Errorf("чтение IPv4: %w", err)
		}
		host = net.IP(raw[:]).String()
	case AtypIPv6:
		var raw [16]byte
		if _, err := io.ReadFull(r, raw[:]); err != nil {
			return Address{}, fmt.Errorf("чтение IPv6: %w", err)
		}
		host = net.IP(raw[:]).String()
	case AtypDomain:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return Address{}, fmt.Errorf("чтение длины домена: %w", err)
		}
		if l[0] == 0 {
			return Address{}, fmt.Errorf("пустое доменное имя")
		}
		raw := make([]byte, l[0])
		if _, err := io.ReadFull(r, raw); err != nil {
			return Address{}, fmt.Errorf("чтение домена: %w", err)
		}
		host = string(raw)
	default:
		return Address{}, fmt.Errorf("неизвестный тип адреса 0x%02x", atyp)
	}

	var port [2]byte
	if _, err := io.ReadFull(r, port[:]); err != nil {
		return Address{}, fmt.Errorf("чтение порта: %w", err)
	}
	return Address{Type: atyp, Host: host, Port: binary.BigEndian.Uint16(port[:])}, nil
}
