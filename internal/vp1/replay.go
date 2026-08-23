package vp1

import (
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

// ErrReplay возвращается, когда первое сообщение хендшейка уже было получено.
var ErrReplay = errors.New("повтор ранее полученного хендшейка")

// ErrClockSkew — время в хендшейке слишком далеко от нашего.
var ErrClockSkew = errors.New("время в хендшейке вне допустимого окна")

// ReplayGuard защищает от повторной отправки перехваченного первого сообщения.
//
// Зачем это нужно именно нам: сам по себе Noise IK от replay не защищает.
// Цензор, записавший msg1 настоящего клиента, может переслать его на сервер
// снова и посмотреть на реакцию — так китайский GFW отличает прокси от
// обычного сервера (это и называется active probing). Сервер, который на
// повтор отвечает так же, как на оригинал, выдаёт себя.
//
// Решение то же, что у WireGuard: клиент кладёт в msg1 своё время, сервер
// отбрасывает всё за пределами окна и помнит уже виденное внутри окна.
type ReplayGuard struct {
	mu     sync.Mutex
	window time.Duration
	seen   map[[sha256.Size]byte]time.Time
	now    func() time.Time
}

// NewReplayGuard создаёт защиту с заданным допуском на расхождение часов.
func NewReplayGuard(window time.Duration) *ReplayGuard {
	return &ReplayGuard{
		window: window,
		seen:   make(map[[sha256.Size]byte]time.Time),
		now:    time.Now,
	}
}

// Check проверяет сообщение и запоминает его. Возвращает ошибку, если
// сообщение — повтор или его метка времени вне окна.
func (g *ReplayGuard) Check(message []byte, stamp time.Time) error {
	now := g.now()
	if stamp.Before(now.Add(-g.window)) || stamp.After(now.Add(g.window)) {
		return ErrClockSkew
	}

	key := sha256.Sum256(message)

	g.mu.Lock()
	defer g.mu.Unlock()

	g.evictLocked(now)
	if _, ok := g.seen[key]; ok {
		return ErrReplay
	}
	g.seen[key] = now
	return nil
}

// evictLocked выбрасывает записи, вышедшие за окно: их всё равно отсечёт
// проверка времени, а память они занимают.
func (g *ReplayGuard) evictLocked(now time.Time) {
	deadline := now.Add(-2 * g.window)
	for k, seenAt := range g.seen {
		if seenAt.Before(deadline) {
			delete(g.seen, k)
		}
	}
}

// Size возвращает число запомненных хендшейков (нужно тестам).
func (g *ReplayGuard) Size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.seen)
}
