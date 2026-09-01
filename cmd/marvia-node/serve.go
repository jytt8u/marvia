package main

import (
	"errors"
	"io"
	"log"
	"net"
	"time"

	"github.com/veilproject/veil/internal/fallback"
	"github.com/veilproject/veil/internal/inbound"
	"github.com/veilproject/veil/internal/metered"
	"github.com/veilproject/veil/internal/mux"
	"github.com/veilproject/veil/internal/relay"
	"github.com/veilproject/veil/internal/rewind"
	"github.com/veilproject/veil/internal/users"
	"github.com/veilproject/veil/internal/vp1"
)

// classifyTimeout — сколько ждём, пока гость обозначит себя.
//
// Без дедлайна сюда набивались бы соединения, открытые и брошенные молча:
// сканеры интернета так делают постоянно, и каждое такое соединение держало бы
// горутину и память.
const classifyTimeout = 15 * time.Second

// serve обслуживает одно входящее соединение.
//
// Нода двуязычна: на одном порту она понимает свой VP1 и чужие VLESS и Trojan.
// Смысл в том, чтобы продавец переехал на нашу платформу, не заставляя своих
// покупателей менять приложение, — это самая дорогая часть любого переезда.
func serve(conn net.Conn, d deps) {
	peer := conn.RemoteAddr()

	// Счётчик стоит снаружи протокола: считаем то, что реально прошло по
	// проводу. Именно за эти байты владелец ноды платит хостеру.
	meter := metered.New(conn)

	// Пока протокол не опознан, запоминаем прочитанное: неудачная попытка
	// должна оставить поток нетронутым для следующей, а в самом конце —
	// для сайта-прикрытия.
	rc := rewind.New(meter)
	defer rc.Close()

	_ = rc.SetDeadline(time.Now().Add(classifyTimeout))

	var knownVLESS inbound.KnownVLESS
	if d.registry != nil {
		knownVLESS = func(uuid []byte) bool {
			return d.registry.HasCredential(users.KindVLESS, uuid)
		}
	}

	proto, err := inbound.Classify(rc, knownVLESS)
	if err != nil {
		serveCover(rc, peer, err, d.fallback)
		return
	}

	switch proto {
	case inbound.ProtoVP1:
		serveVP1(rc, meter, peer, d)
	case inbound.ProtoVLESS:
		serveVLESS(rc, meter, peer, d)
	case inbound.ProtoTrojan:
		serveTrojan(rc, meter, peer, d)
	default:
		serveCover(rc, peer, errors.New("протокол не опознан"), d.fallback)
	}
}

// serveVP1 обслуживает наш протокол: одна сессия, много логических потоков.
func serveVP1(rc *rewind.Conn, meter *metered.Conn, peer net.Addr, d deps) {
	var session *users.Session
	authorize := vp1.AllowAll
	if d.registry != nil {
		authorize = func(pub []byte) error {
			s, err := d.registry.Admit(users.KindVP1, pub, peer)
			if err != nil {
				return err
			}
			session = s
			return nil
		}
	}

	tunnel, clientPub, err := vp1.ServerHandshake(rc, d.static, d.guard, authorize)
	if err != nil {
		serveCover(rc, peer, err, d.fallback)
		return
	}
	rc.Commit()
	defer tunnel.Close()

	client := label(session, vp1.EncodeKey(clientPub))
	stop := startAccounting(meter, session, tunnel, peer, client)
	defer stop()

	muxSession, err := mux.Server(tunnel)
	if err != nil {
		log.Printf("[%s] клиент %s: %v", peer, client, err)
		return
	}
	defer muxSession.Close()

	log.Printf("[%s] клиент %s (vp1): сессия открыта", peer, client)
	for {
		stream, err := mux.Accept(muxSession)
		if err != nil {
			log.Printf("[%s] клиент %s (vp1): сессия закрыта", peer, client)
			return
		}
		go serveStream(stream, peer, client)
	}
}

// serveVLESS обслуживает чужой клиент по VLESS: одно соединение — одна цель.
func serveVLESS(rc *rewind.Conn, meter *metered.Conn, peer net.Addr, d deps) {
	request, err := inbound.ReadVLESSRequest(rc)
	if err != nil {
		serveCover(rc, peer, err, d.fallback)
		return
	}

	session, err := admit(d, users.KindVLESS, request.UUID, peer)
	if err != nil {
		serveCover(rc, peer, err, d.fallback)
		return
	}
	rc.Commit()
	_ = rc.SetDeadline(time.Time{})

	client := label(session, users.FormatUUID(request.UUID))
	stop := startAccounting(meter, session, rc, peer, client)
	defer stop()

	// Ответный заголовок VLESS уедет вместе с первой порцией данных.
	pipeSingle(inbound.NewVLESSConn(rc), request.Target, peer, client, "vless")
}

// serveTrojan обслуживает чужой клиент по Trojan.
func serveTrojan(rc *rewind.Conn, meter *metered.Conn, peer net.Addr, d deps) {
	request, err := inbound.ReadTrojanRequest(rc)
	if err != nil {
		serveCover(rc, peer, err, d.fallback)
		return
	}

	session, err := admit(d, users.KindTrojan, request.Digest, peer)
	if err != nil {
		// Пароль не подошёл — гость получает сайт, как и любой посторонний.
		// Так и задуман Trojan: сервер неотличим от обычного веб-сервера.
		serveCover(rc, peer, err, d.fallback)
		return
	}
	rc.Commit()
	_ = rc.SetDeadline(time.Time{})

	client := label(session, "trojan")
	stop := startAccounting(meter, session, rc, peer, client)
	defer stop()

	// У Trojan ответного заголовка нет: сразу данные.
	pipeSingle(rc, request.Target, peer, client, "trojan")
}

// admit проверяет учётные данные. Пустой реестр означает отладочный режим,
// когда пускают любого, кто знает публичный ключ ноды.
func admit(d deps, kind string, identity []byte, peer net.Addr) (*users.Session, error) {
	if d.registry == nil {
		if kind == users.KindVP1 {
			return nil, nil
		}
		// У VLESS и Trojan нет ключа ноды: без списка пользователей пускать
		// по ним некого, иначе нода станет открытым прокси для всех подряд.
		return nil, errors.New("список пользователей не задан")
	}
	return d.registry.Admit(kind, identity, peer)
}

// pipeSingle доводит до конца одно соединение чужого протокола.
func pipeSingle(client net.Conn, target vp1.Address, peer net.Addr, name, proto string) {
	upstream, err := net.DialTimeout("tcp", target.String(), dialTimeout)
	if err != nil {
		log.Printf("[%s] клиент %s (%s): не подключились к %s: %v", peer, name, proto, target, err)
		return
	}
	defer upstream.Close()

	log.Printf("[%s] клиент %s (%s) -> %s", peer, name, proto, target)
	if err := relay.Bidirectional(client, upstream); err != nil {
		log.Printf("[%s] клиент %s (%s) -> %s: обрыв: %v", peer, name, proto, target, err)
	}
}

// serveStream обслуживает один логический поток внутри сессии VP1 — то есть
// одно соединение приложения пользователя.
func serveStream(stream net.Conn, peer net.Addr, client string) {
	defer stream.Close()

	_ = stream.SetReadDeadline(time.Now().Add(requestTimeout))
	addr, kind, err := vp1.ReadRequestOf(stream)
	if err != nil {
		log.Printf("[%s] клиент %s: чтение запроса: %v", peer, client, err)
		return
	}
	_ = stream.SetReadDeadline(time.Time{})

	if kind == vp1.KindUDP {
		serveDatagrams(stream, addr, peer, client)
		return
	}

	target, err := net.DialTimeout("tcp", addr.String(), dialTimeout)
	if err != nil {
		log.Printf("[%s] клиент %s: не подключились к %s: %v", peer, client, addr, err)
		_ = vp1.WriteStatus(stream, vp1.StatusUnreachable)
		return
	}
	defer target.Close()

	if err := vp1.WriteStatus(stream, vp1.StatusOK); err != nil {
		log.Printf("[%s] клиент %s: отправка статуса: %v", peer, client, err)
		return
	}

	log.Printf("[%s] клиент %s -> %s", peer, client, addr)
	if err := relay.Bidirectional(stream, target); err != nil {
		log.Printf("[%s] клиент %s -> %s: обрыв: %v", peer, client, addr, err)
	}
}

// udpIdleTimeout — сколько держим поток датаграмм без единого пакета.
//
// У UDP нет конца разговора, закрывать поток некому. Полторы минуты выбраны
// по QUIC: он шлёт своё подтверждение жизни куда чаще, так что живое
// соединение сюда не попадёт, а брошенное не будет висеть до отключения
// человека от туннеля.
const udpIdleTimeout = 90 * time.Second

// serveDatagrams обслуживает поток датаграмм до одной цели.
//
// Цель фиксируется запросом и дальше не меняется: поток на неё и заведён.
// Поэтому проверять адрес источника у пришедших ответов не нужно — сокет
// подключённый, ядро само отбросит чужие.
func serveDatagrams(stream net.Conn, addr vp1.Address, peer net.Addr, client string) {
	target, err := net.DialTimeout("udp", addr.String(), dialTimeout)
	if err != nil {
		log.Printf("[%s] клиент %s: не открыли udp до %s: %v", peer, client, addr, err)
		_ = vp1.WriteStatus(stream, vp1.StatusUnreachable)
		return
	}
	defer target.Close()

	if err := vp1.WriteStatus(stream, vp1.StatusOK); err != nil {
		log.Printf("[%s] клиент %s: отправка статуса: %v", peer, client, err)
		return
	}

	log.Printf("[%s] клиент %s -> %s (udp)", peer, client, addr)

	relay.Datagrams(vp1.Datagrams(stream), target, udpIdleTimeout)
}

// startAccounting запускает учёт трафика и присмотр за подпиской.
// Возвращённую функцию нужно вызвать по завершении: она доснимет счётчики.
func startAccounting(meter *metered.Conn, session *users.Session, closer io.Closer, peer net.Addr, client string) func() {
	done := make(chan struct{})
	go meterLoop(meter, session, closer, peer, client, done)

	var once bool
	return func() {
		if once {
			return
		}
		once = true
		close(done)

		// Последняя выгрузка: то, что накопилось после предпоследнего тика,
		// тоже должно попасть в статистику.
		up, down := meter.Drain()
		session.Add(up, down)
		session.Close()
	}
}

// meterLoop переносит счётчики соединения пользователю и обрывает связь,
// когда доступ кончился.
func meterLoop(meter *metered.Conn, session *users.Session, closer io.Closer, peer net.Addr, client string, done <-chan struct{}) {
	ticker := time.NewTicker(meterInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			up, down := meter.Drain()
			if up != 0 || down != 0 {
				session.Add(up, down)
			}
			// Проверяем не только квоту: подписку могли отключить или она
			// могла истечь прямо посреди сессии. Живое соединение обязано
			// это заметить, иначе человек пользуется сервисом до тех пор,
			// пока сам не переподключится.
			if err := session.Valid(); err != nil {
				log.Printf("[%s] клиент %s: доступ прекращён (%v), отключаем", peer, client, err)
				_ = closer.Close()
				return
			}
		}
	}
}

// serveCover обслуживает того, кто не прошёл проверку.
//
// Сюда попадают сканеры, боты, случайные посетители домена, зонды цензора — и
// свои же клиенты с истёкшей подпиской или исчерпанной квотой. Реакция должна
// быть одинаковой во всех случаях: разница в поведении сама становится
// способом прощупать ноду.
func serveCover(rc *rewind.Conn, peer net.Addr, cause error, cover *fallback.Handler) {
	if cover == nil {
		log.Printf("[%s] соединение отклонено (%v), сайт-прикрытие не задан", peer, cause)
		return
	}
	if err := rc.Rewind(); err != nil {
		log.Printf("[%s] соединение отклонено (%v), отмотка не удалась: %v", peer, cause, err)
		return
	}
	_ = rc.SetDeadline(time.Time{})
	cover.Serve(rc)
}

// label выбирает, как называть клиента в журнале: пометка из подписки, если
// она есть, иначе технический идентификатор.
func label(session *users.Session, fallbackName string) string {
	if l := session.Label(); l != "" {
		return l
	}
	return fallbackName
}
