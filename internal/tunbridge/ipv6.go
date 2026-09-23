package tunbridge

import (
	"encoding/binary"
	"errors"
	"sync/atomic"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Нода без IPv6.
//
// Телефон заворачивает IPv6 в туннель всегда: оставить его снаружи — утечка,
// цензор увидел бы ровно ту часть трафика, которую мы прячем. Но у многих нод
// хостер IPv6 не дал, и каждое IPv6-соединение приложения тогда кончается
// отказом ноды — на живой ноде это было 700 отказов из 835 за два часа. Браузер
// после отказа переходит на IPv4, но платит за это кругом до ноды, а журнал
// телефона превращается в стену «цель недоступна».
//
// Поэтому, узнав, что у ноды IPv6 нет, мост перестаёт его предлагать: на
// запросы AAAA отвечает «таких адресов нет», и приложения сразу идут по IPv4;
// а IPv6-соединение, если оно всё же пришло (адрес из кэша), закрывает сам, не
// беспокоя ноду. Наружу IPv6 по-прежнему не выходит — утечки нет.
//
// Узнаём двумя путями. Новая нода отвечает кодом StatusNoRoute. Старая
// говорит просто «недоступна», и тогда признак — несколько IPv6-отказов
// подряд без единого удачного IPv6-соединения: один сайт без IPv6 бывает,
// а подряд все — это нода.
//
// Третий путь — человек выключил IPv6 сам (Config.NoIPv6). Тогда мост ведёт
// себя так, будто у любой ноды его нет, и переезд этого не сбрасывает.
// Причины бывают свои: IPv6-адрес ноды сайты иногда относят к другой стране,
// чем IPv4, и один и тот же сервис видит человека то там, то здесь.

// v6FailsToGiveUp — сколько IPv6-отказов подряд считать приметой ноды без
// IPv6. Меньше — и один сломанный сайт выключал бы IPv6 всей сессии; больше —
// и первые секунды после подключения тонули бы в отказах.
const v6FailsToGiveUp = 3

// v6State — что мост знает про IPv6 у текущей ноды. Сбрасывается при
// переезде (Bridge.NodeChanged): у другой ноды IPv6 может и быть.
type v6State struct {
	off   atomic.Bool
	fails atomic.Int32

	// never — IPv6 выключил человек. Ставится при запуске моста и больше
	// не меняется, поэтому без атомарности.
	never bool
}

func (s *v6State) reset() {
	s.off.Store(s.never)
	s.fails.Store(0)
}

// observe учитывает исход IPv6-соединения.
func (s *v6State) observe(err error) {
	if err == nil {
		s.fails.Store(0)
		return
	}
	var refused *vp1.RefusedError
	if !errors.As(err, &refused) {
		return
	}
	switch refused.Status {
	case vp1.StatusNoRoute:
		s.off.Store(true)
	case vp1.StatusUnreachable:
		if s.fails.Add(1) >= v6FailsToGiveUp {
			s.off.Store(true)
		}
	}
}

// dnsTypeAAAA — тип запроса IPv6-адреса.
const dnsTypeAAAA = 28

// aaaaQuestion говорит, спрашивает ли датаграмма DNS только IPv6-адрес, и
// где кончается вопрос: ответ собирается из заголовка и вопроса, а всё, что
// дальше (EDNS), в него не идёт.
//
// Разбор минимальный: заголовок, одно имя из меток, тип. Сжатых имён в
// вопросе не бывает, а всё необычное — несколько вопросов, обрезанный пакет —
// считаем «не AAAA» и отдаём ноде как есть: ошибиться в эту сторону безвредно.
func aaaaQuestion(q []byte) (int, bool) {
	if len(q) < 12 || q[2]&0x80 != 0 || binary.BigEndian.Uint16(q[4:6]) != 1 {
		return 0, false
	}
	i := 12
	for {
		if i >= len(q) {
			return 0, false
		}
		l := int(q[i])
		if l == 0 {
			i++
			break
		}
		if l&0xC0 != 0 {
			return 0, false
		}
		i += 1 + l
	}
	if i+4 > len(q) {
		return 0, false
	}
	return i + 4, binary.BigEndian.Uint16(q[i:i+2]) == dnsTypeAAAA
}

// localAnswer — ответ, который мост даёт сам, не спрашивая ноду: пустой на
// AAAA, когда у ноды нет IPv6. Второе значение — ответили ли.
func (s *v6State) localAnswer(q []byte) ([]byte, bool) {
	if !s.off.Load() {
		return nil, false
	}
	end, ok := aaaaQuestion(q)
	if !ok {
		return nil, false
	}
	return emptyAnswer(q[:end]), true
}

// emptyAnswer собирает ответ «имя есть, IPv6-адресов у него нет».
//
// Именно пустой ответ без ошибки (NOERROR, ноль записей), а не NXDOMAIN:
// «такого имени нет» заставило бы приложение не пробовать и IPv4.
func emptyAnswer(q []byte) []byte {
	out := append([]byte(nil), q...)
	out[2] = 0x80 | (q[2] & 0x79) // QR, код операции и RD как в запросе
	out[3] = 0x80                 // RA, RCODE = 0
	binary.BigEndian.PutUint16(out[6:8], 0)
	binary.BigEndian.PutUint16(out[8:10], 0)
	binary.BigEndian.PutUint16(out[10:12], 0)
	return out
}
