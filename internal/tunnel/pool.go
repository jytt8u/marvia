// Package tunnel держит пул мультиплексированных соединений до ноды.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/jytt8u/marvia/internal/mux"
)

// DialFunc устанавливает одно шифрованное соединение до ноды.
type DialFunc func(ctx context.Context) (net.Conn, error)

// Значения по умолчанию.
//
// Почему не одна сессия на всё: head-of-line blocking — потерянный TCP-сегмент
// останавливает все потоки разом, и одна медленная загрузка подвешивает
// страницу. Почему не по соединению на запрос: пачка коротких одинаковых
// соединений видна за версту.
//
// Почему именно две, а не четыре, как было сначала. По разборам поведения
// ТСПУ 2026 года одним из сигналов служит несколько параллельных
// TLS-хендшейков к одному имени в коротком окне: три и больше подряд —
// и соединения начинают молча дропаться примерно на две минуты. Пул,
// открывающий четыре сессии на старте, попадает под это правило сам.
const (
	DefaultMaxSessions = 2
	DefaultMaxStreams  = 32
)

// Разбег между открытием сессий.
//
// Новое соединение до ноды не поднимается сразу вслед за предыдущим: между
// ними выдерживается пауза со случайной добавкой. Фиксированная пауза была бы
// собственной приметой, поэтому она плавает.
const (
	dialGapBase   = 1500 * time.Millisecond
	dialGapJitter = 1500 // миллисекунд
)

// Окно смены сессии.
//
// Сессия живёт не дольше случайного срока в этих пределах, после чего уходит
// на покой: новых потоков не берёт, а опустев — закрывается, и на её место
// поднимается свежая с новым рукопожатием.
//
// Зачем. Во-первых, forward secrecy: эфемерный ключ Noise живёт ровно одну
// сессию, и суточное соединение — это сутки на одном ключе; смена раз в
// полчаса ограничивает, что вскрывается при его утечке, получасом трафика.
// Во-вторых, картина соединений: браузер не держит одно TCP-соединение к
// сайту сутками, а туннель, открывший его на старте и не трогавший до вечера,
// этим и выделяется. Плавающий срок не даёт и самой смене стать приметой.
//
// Активный поток при этом не рвётся никогда: сессия ждёт, пока он договорит.
// Одна долгая закачка законно держит одно долгое соединение — рвать её ради
// ротации значило бы чинить скрытность ценой того, ради чего всё и работает.
const (
	rotateMin = 20 * time.Minute
	rotateMax = 60 * time.Minute
)

// reapEvery — как часто пул сам оглядывается на свои сессии.
//
// Без этого смена случалась бы только при открытии следующего потока, и
// молчащий туннель держал бы одно соединение до ноды хоть десять часов — ровно
// ту картину, ради ухода от которой смена и заведена. Минута: смена всё равно
// назначена на десятки минут, чаще смотреть незачем.
const reapEvery = time.Minute

// pooled — сессия и срок её жизни.
type pooled struct {
	sess *yamux.Session
	// retireAt — когда сессия перестаёт брать новые потоки. Опустев после
	// этого срока, она закрывается.
	retireAt time.Time
}

// Pool раздаёт логические потоки, пряча за собой управление сессиями.
type Pool struct {
	dial        DialFunc
	maxSessions int
	maxStreams  int

	// now и rotateAfter вынесены, чтобы тест мог управлять временем и сроком
	// смены, не выжидая реальные минуты. В бою — time.Now и случайный срок.
	now         func() time.Time
	rotateAfter func() time.Duration

	mu       sync.Mutex
	sessions []*pooled
	closed   bool

	// done останавливает сборщик, когда пул закрывают.
	done chan struct{}

	// dialMu не даёт двум хендшейкам идти одновременно, lastDial хранит
	// время последнего, чтобы выдержать разбег.
	dialMu   sync.Mutex
	lastDial time.Time
	pingBusy atomic.Bool
}

// Ping меряет запрос-ответ внутри уже установленного туннеля, включая очередь
// записи. Хендшейк сюда не входит. Отмена замера не рвёт рабочие потоки:
// yamux завершит запрос по своему тайм-ауту; до этого второй не запускаем.
func (p *Pool) Ping(ctx context.Context) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !p.pingBusy.CompareAndSwap(false, true) {
		return 0, errors.New("замер уже идёт")
	}
	p.mu.Lock()
	var session *yamux.Session
	for _, s := range p.sessions {
		if !s.sess.IsClosed() {
			session = s.sess
			break
		}
	}
	p.mu.Unlock()
	if session == nil {
		p.pingBusy.Store(false)
		return 0, errors.New("нет установленного туннеля")
	}
	type result struct {
		rtt time.Duration
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer p.pingBusy.Store(false)
		start := time.Now()
		_, err := session.Ping()
		done <- result{time.Since(start), err}
	}()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return 0, r.err
		}
		return max(time.Nanosecond, r.rtt), nil
	}
}

// NewPool создаёт пул. Нулевые значения лимитов заменяются на умолчания.
func NewPool(dial DialFunc, maxSessions, maxStreams int) *Pool {
	if maxSessions <= 0 {
		maxSessions = DefaultMaxSessions
	}
	if maxStreams <= 0 {
		maxStreams = DefaultMaxStreams
	}
	return newPool(dial, maxSessions, maxStreams, reapEvery)
}

// newPool — тот же конструктор с настраиваемым шагом сборщика: тест не может
// ждать минуту, а в бою шаг всегда один.
func newPool(dial DialFunc, maxSessions, maxStreams int, reap time.Duration) *Pool {
	p := &Pool{
		dial:        dial,
		maxSessions: maxSessions,
		maxStreams:  maxStreams,
		now:         time.Now,
		rotateAfter: func() time.Duration { return rotateMin + time.Duration(mrand.Int64N(int64(rotateMax-rotateMin))) },
		done:        make(chan struct{}),
	}
	go p.reap(reap)
	return p
}

// reap закрывает отработавшие пустые сессии, пока пул жив.
func (p *Pool) reap(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.mu.Lock()
			if !p.closed {
				p.pruneLocked()
			}
			p.mu.Unlock()
		}
	}
}

// Open выдаёт новый логический поток до ноды.
func (p *Pool) Open(ctx context.Context) (net.Conn, error) {
	// Две попытки: сессия могла умереть между выбором и открытием потока —
	// например, нода перезапустилась или провайдер оборвал соединение.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		session, err := p.session(ctx)
		if err != nil {
			return nil, err
		}
		stream, err := mux.Open(session)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		p.drop(session)
	}
	return nil, fmt.Errorf("открытие потока: %w", lastErr)
}

// Close закрывает все сессии.
func (p *Pool) Close() error {
	p.mu.Lock()
	sessions := p.sessions
	p.sessions = nil
	// Close могут позвать дважды: закрытие канала во второй раз — паника,
	// поэтому останавливаем сборщик только на первом.
	if !p.closed {
		p.closed = true
		close(p.done)
	}
	p.mu.Unlock()

	for _, s := range sessions {
		_ = s.sess.Close()
	}
	return nil
}

// session подбирает сессию под новый поток, при необходимости создавая её.
func (p *Pool) session(ctx context.Context) (*yamux.Session, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("пул закрыт")
	}
	p.pruneLocked()
	best := p.leastLoadedLocked()
	// В счёт ёмкости идут только сессии, ещё берущие потоки: уходящая на
	// покой не должна мешать поднять ей смену.
	active := p.activeCountLocked()
	atCapacity := active >= p.maxSessions
	p.mu.Unlock()

	if best != nil && (best.NumStreams() < p.maxStreams || atCapacity) {
		// Если сессий уже максимум, лучше перегрузить существующую, чем
		// отказать пользователю.
		return best, nil
	}
	return p.spawn(ctx)
}

// spawn поднимает новую сессию.
//
// Дозвон сериализован отдельным мьютексом: два хендшейка одновременно — уже
// половина того порога, по которому ТСПУ опознаёт туннель. Общий мьютекс пула
// при этом не удерживается: дозвон занимает секунды, и вешать на него весь пул
// нельзя.
func (p *Pool) spawn(ctx context.Context) (*yamux.Session, error) {
	p.dialMu.Lock()
	defer p.dialMu.Unlock()

	// Пока стояли в очереди, соседняя горутина могла поднять сессию, и место
	// в ней уже есть. Тогда лишнее соединение открывать незачем.
	if s := p.roomy(); s != nil {
		return s, nil
	}

	if gap := p.dialGap() - time.Since(p.lastDial); gap > 0 {
		timer := time.NewTimer(gap)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	conn, err := p.dial(ctx)
	p.lastDial = time.Now()
	if err != nil {
		return nil, err
	}
	session, err := mux.Client(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = session.Close()
		return nil, errors.New("пул закрыт")
	}
	p.sessions = append(p.sessions, &pooled{sess: session, retireAt: p.now().Add(p.rotateAfter())})
	p.mu.Unlock()

	return session, nil
}

// drop убирает сессию из пула и закрывает её.
func (p *Pool) drop(target *yamux.Session) {
	p.mu.Lock()
	for i, s := range p.sessions {
		if s.sess == target {
			p.sessions = append(p.sessions[:i], p.sessions[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
	_ = target.Close()
}

// pruneLocked выбрасывает умершие сессии и закрывает опустевшие после срока
// смены. Уходящую сессию с живыми потоками не трогаем: она дождётся, пока они
// договорят.
func (p *Pool) pruneLocked() {
	now := p.now()
	alive := p.sessions[:0]
	for _, s := range p.sessions {
		if s.sess.IsClosed() {
			continue
		}
		if now.After(s.retireAt) && s.sess.NumStreams() == 0 {
			_ = s.sess.Close()
			continue
		}
		alive = append(alive, s)
	}
	p.sessions = alive
}

// leastLoadedLocked возвращает сессию с наименьшим числом потоков из тех, что
// ещё берут потоки. Ушедшую на покой не отдаём: она должна опустеть.
func (p *Pool) leastLoadedLocked() *yamux.Session {
	now := p.now()
	var best *yamux.Session
	bestLoad := -1
	for _, s := range p.sessions {
		if now.After(s.retireAt) {
			continue
		}
		load := s.sess.NumStreams()
		if bestLoad < 0 || load < bestLoad {
			best, bestLoad = s.sess, load
		}
	}
	return best
}

// activeCountLocked считает сессии, ещё берущие новые потоки.
func (p *Pool) activeCountLocked() int {
	now := p.now()
	n := 0
	for _, s := range p.sessions {
		if !now.After(s.retireAt) {
			n++
		}
	}
	return n
}

// dialGap выбирает паузу перед открытием очередной сессии.
func (p *Pool) dialGap() time.Duration {
	return dialGapBase + time.Duration(mrand.IntN(dialGapJitter))*time.Millisecond
}

// roomy возвращает живую сессию, ещё берущую потоки и не заполненную.
func (p *Pool) roomy() *yamux.Session {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pruneLocked()
	now := p.now()
	for _, s := range p.sessions {
		if !now.After(s.retireAt) && s.sess.NumStreams() < p.maxStreams {
			return s.sess
		}
	}
	return nil
}
