package transport

import (
	"encoding/binary"
	mrand "math/rand/v2"
	"net"
	"time"
)

// Дробление TLS-приветствия.
//
// Самая частая блокировка в России — по имени в ClientHello: DPI провайдера
// смотрит в пакет с приветствием, находит там SNI и рвёт или замедляет
// соединение. Многие такие фильтры не собирают поток из нескольких пакетов, а
// читают один. Если имя разрезано между двумя TCP-сегментами, целиком его нет
// ни в одном, и фильтр его не видит. Сервер получает ровно те же байты: TCP
// склеивает их обратно, и ни TLS, ни REALITY разницы не замечают.
//
// Режем на уровне TCP, а не на TLS-записи. Несколько записей вместо одной —
// приём сильнее против фильтров, которые TCP собирают, но сервер REALITY
// читает приветствие одной записью, прежде чем решить, чей это клиент, и
// разрезанное по записям принял бы за чужое.
//
// Место разреза ищем, а не угадываем. Chrome перемешивает расширения в
// каждом приветствии, а постквантовый обмен ключами растянул его почти до
// двух килобайт, так что имя бывает и в начале, и в самом конце. «Порежем
// первые двести байт» промахивалось бы мимо имени в большинстве соединений.
//
// По умолчанию выключено: у кого блокировки по SNI нет, лишние пакеты и
// миллисекунды ничего не дают, а сама форма «приветствие несколькими
// сегментами» — тоже примета.

// pieceDelay — пауза между кусками. Без неё ядро вправе отправить их одним
// сегментом, и тогда резать было незачем.
const pieceDelay = 2 * time.Millisecond

// blindPieces — на сколько кусков режем приветствие, в котором не нашли имя:
// разбор споткнулся, а резать всё равно надо.
const blindPieces = 6

// splitHello оборачивает соединение: первая запись, если это TLS-приветствие,
// уходит кусками.
type splitHello struct {
	net.Conn
	done bool
}

// FragmentHello включает дробление приветствия на соединении.
func FragmentHello(c net.Conn) net.Conn {
	if tcp, ok := c.(*net.TCPConn); ok {
		// Без NoDelay ядро склеило бы куски обратно в один сегмент. У Go он
		// включён и так, но полагаться на умолчание здесь нельзя.
		_ = tcp.SetNoDelay(true)
	}
	return &splitHello{Conn: c}
}

func (c *splitHello) Write(p []byte) (int, error) {
	if c.done {
		return c.Conn.Write(p)
	}
	c.done = true
	// Режем только настоящее приветствие: запись рукопожатия (0x16) версии
	// 3.x с сообщением ClientHello (0x01). Первый пакет Shadowsocks и VMess
	// случаен, и по одному первому байту резалось бы каждое двухсотпятьдесят
	// шестое такое соединение — с паузами и странной формой.
	if len(p) < 6 || p[0] != 0x16 || p[1] != 0x03 || p[5] != 0x01 {
		return c.Conn.Write(p)
	}
	written := 0
	for _, end := range helloCuts(p) {
		n, err := c.Conn.Write(p[written:end])
		written += n
		if err != nil {
			return written, err
		}
		time.Sleep(pieceDelay)
	}
	if written < len(p) {
		n, err := c.Conn.Write(p[written:])
		return written + n, err
	}
	return written, nil
}

// helloCuts — где резать приветствие: концы кусков по возрастанию.
//
// Два разреза: один случайный до имени, чтобы начало записи ушло отдельно, и
// второй посреди самого имени. Случайные — чтобы и сама нарезка не стала
// отпечатком.
func helloCuts(p []byte) []int {
	start, end, ok := serverNameAt(p)
	if !ok || end-start < 2 {
		return blindCuts(len(p))
	}
	before := 1 + mrand.IntN(start-1)
	inside := start + 1 + mrand.IntN(end-start-1)
	return []int{before, inside}
}

// blindCuts режет приветствие на равные примерно куски, когда имя не нашлось.
func blindCuts(n int) []int {
	if n < blindPieces*2 {
		return nil
	}
	step := n / blindPieces
	cuts := make([]int, 0, blindPieces-1)
	for i := 1; i < blindPieces; i++ {
		cuts = append(cuts, i*step+mrand.IntN(step/2+1)-step/4)
	}
	return cuts
}

// serverNameAt находит имя из SNI в TLS-записи с ClientHello: где оно
// начинается и где кончается.
//
// Разбор по RFC 8446: заголовок записи, заголовок сообщения, версия, случайные
// байты, идентификатор сессии, шифры, сжатие, расширения. Всё, что не
// сходится по длинам, — «не нашли», и тогда режем вслепую: ошибиться в эту
// сторону безопасно, соединение от этого не ломается.
func serverNameAt(p []byte) (int, int, bool) {
	const (
		recordHeader  = 5
		messageHeader = 4
		serverName    = 0x0000
		hostName      = 0x00
	)
	if len(p) < recordHeader+messageHeader || p[recordHeader] != 0x01 {
		return 0, 0, false
	}
	end := min(len(p), recordHeader+int(binary.BigEndian.Uint16(p[3:5])))
	i := recordHeader + messageHeader + 2 + 32 // версия и случайные байты

	skip := func(width int) bool {
		if i+width > end {
			return false
		}
		n := 0
		for _, b := range p[i : i+width] {
			n = n<<8 | int(b)
		}
		i += width + n
		return i <= end
	}
	if !skip(1) || !skip(2) || !skip(1) { // сессия, шифры, сжатие
		return 0, 0, false
	}
	if i+2 > end {
		return 0, 0, false
	}
	extEnd := min(end, i+2+int(binary.BigEndian.Uint16(p[i:i+2])))
	i += 2

	for i+4 <= extEnd {
		kind := binary.BigEndian.Uint16(p[i : i+2])
		size := int(binary.BigEndian.Uint16(p[i+2 : i+4]))
		body := i + 4
		if body+size > extEnd {
			return 0, 0, false
		}
		if kind == serverName {
			// Список имён: длина списка, затем тип имени, длина и само имя.
			if size < 5 || p[body+2] != hostName {
				return 0, 0, false
			}
			n := int(binary.BigEndian.Uint16(p[body+3 : body+5]))
			start := body + 5
			if start+n > body+size {
				return 0, 0, false
			}
			return start, start + n, true
		}
		i = body + size
	}
	return 0, 0, false
}
