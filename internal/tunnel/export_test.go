package tunnel

import "time"

// SetClock подменяет часы пула и срок смены сессии — чтобы проверить ротацию,
// не выжидая реальные двадцать минут. Только для тестов.
func (p *Pool) SetClock(now func() time.Time, rotateAfter time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = now
	p.rotateAfter = func() time.Duration { return rotateAfter }
}

// NewPoolForTest — пул с укороченным шагом сборщика. В бою шаг минутный, и
// тест, ждущий минуту, никто бы не гонял.
func NewPoolForTest(dial DialFunc, maxSessions, maxStreams int, reap time.Duration) *Pool {
	return newPool(dial, maxSessions, maxStreams, reap)
}

// Sessions — сколько сессий пул сейчас держит. Тесту нужно увидеть, что
// отработавшая и опустевшая сессия действительно ушла, а не просто перестала
// брать потоки.
func (p *Pool) Sessions() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}
