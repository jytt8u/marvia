// Package tunnel держит пул мультиплексированных соединений до ноды.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/hashicorp/yamux"
	"github.com/veilproject/veil/internal/mux"
)

// DialFunc устанавливает одно шифрованное соединение до ноды.
type DialFunc func(ctx context.Context) (net.Conn, error)

// Значения по умолчанию.
//
// Почему не одна сессия на всё: во-первых, head-of-line blocking — потерянный
// TCP-сегмент останавливает все потоки разом, и одна медленная загрузка
// подвешивает страницу. Во-вторых, единственное соединение, тянущее весь
// трафик пользователя, выглядит неестественно: браузеры держат несколько.
//
// Почему не по соединению на запрос: это ровно то, от чего мы ушли, — пачка
// коротких одинаковых соединений видна за версту.
const (
	DefaultMaxSessions = 4
	DefaultMaxStreams  = 16
)

// Pool раздаёт логические потоки, пряча за собой управление сессиями.
type Pool struct {
	dial        DialFunc
	maxSessions int
	maxStreams  int

	mu       sync.Mutex
	sessions []*yamux.Session
	closed   bool
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

// spawn поднимает новую сессию. Дозвон идёт без удержания мьютекса: он может
// занять секунды, и держать на нём весь пул нельзя.
func (p *Pool) spawn(ctx context.Context) (*yamux.Session, error) {
	conn, err := p.dial(ctx)
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
