// Package tunnel держит пул мультиплексированных соединений до ноды.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/veilproject/veil/internal/mux"
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

// Pool раздаёт логические потоки, пряча за собой управление сессиями.
type Pool struct {
	dial        DialFunc
	maxSessions int
	maxStreams  int

	mu       sync.Mutex
	sessions []*yamux.Session
	closed   bool

	// dialMu не даёт двум хендшейкам идти одновременно, lastDial хранит
	// время последнего, чтобы выдержать разбег.
	dialMu   sync.Mutex
	lastDial time.Time
}

// NewPool создаёт пул. Нулевые значения лимитов заменяются на умолчания.
func NewPool(dial DialFunc, maxSessions, maxStreams int) *Pool {
	if maxSessions <= 0 {
		maxSessions = DefaultMaxSessions
	}
	if maxStreams <= 0 {
		maxStreams = DefaultMaxStreams
	}
	return &Pool{dial: dial, maxSessions: maxSessions, maxStreams: maxStreams}
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
	p.closed = true
	p.mu.Unlock()

	for _, s := range sessions {
		_ = s.Close()
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
	atCapacity := len(p.sessions) >= p.maxSessions
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
	p.sessions = append(p.sessions, session)
	p.mu.Unlock()

	return session, nil
}

// drop убирает сессию из пула и закрывает её.
func (p *Pool) drop(target *yamux.Session) {
	p.mu.Lock()
	for i, s := range p.sessions {
		if s == target {
			p.sessions = append(p.sessions[:i], p.sessions[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
	_ = target.Close()
}

// pruneLocked выбрасывает умершие сессии.
func (p *Pool) pruneLocked() {
	alive := p.sessions[:0]
	for _, s := range p.sessions {
		if s.IsClosed() {
			continue
		}
		alive = append(alive, s)
	}
	p.sessions = alive
}

// leastLoadedLocked возвращает сессию с наименьшим числом потоков.
func (p *Pool) leastLoadedLocked() *yamux.Session {
	var best *yamux.Session
	bestLoad := -1
	for _, s := range p.sessions {
		load := s.NumStreams()
		if bestLoad < 0 || load < bestLoad {
			best, bestLoad = s, load
		}
	}
	return best
}

// dialGap выбирает паузу перед открытием очередной сессии.
func (p *Pool) dialGap() time.Duration {
	return dialGapBase + time.Duration(mrand.IntN(dialGapJitter))*time.Millisecond
}

// roomy возвращает живую сессию, в которой ещё есть место под поток.
func (p *Pool) roomy() *yamux.Session {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pruneLocked()
	for _, s := range p.sessions {
		if s.NumStreams() < p.maxStreams {
			return s
		}
	}
	return nil
}
