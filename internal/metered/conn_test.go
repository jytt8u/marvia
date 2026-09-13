package metered_test

import (
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/jytt8u/marvia/internal/metered"
)

// pipe даёт пару соединений в памяти: сеть здесь ни при чём, меряем только то,
// что делает сам ограничитель.
func pipe(t *testing.T) (client, server net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return a, b
}

// TestLimitHoldsTheSpeed — потолок скорости действительно держит.
//
// Обещание продавцу: тариф «столько-то мегабит» — это не надпись, а то, чего
// покупатель не превысит. Проверяем по времени: на скорости 64 КиБ/с 192 КиБ
// не могут уехать быстрее, чем за пару секунд, сколько бы их ни писали.
func TestLimitHoldsTheSpeed(t *testing.T) {
	client, server := pipe(t)

	const speed = 64 << 10 // байт в секунду
	const burst = 64 << 10
	const payload = 3 * speed

	m := metered.New(client)
	m.Limit(rate.NewLimiter(rate.Limit(speed), burst))

	go func() { _, _ = io.Copy(io.Discard, server) }()

	start := time.Now()
	if _, err := m.Write(make([]byte, payload)); err != nil {
		t.Fatalf("запись: %v", err)
	}
	spent := time.Since(start)

	// Ведро отдаёт первые burst байт сразу, остальные — по speed в секунду.
	// Значит на payload уходит не меньше (payload-burst)/speed секунд.
	least := time.Duration(float64(payload-burst)/float64(speed)*float64(time.Second)) * 9 / 10
	if spent < least {
		t.Fatalf("%d байт уехали за %v — быстрее потолка (ожидалось не меньше %v)", payload, spent, least)
	}
	// И не должно быть кратно дольше: потолок придерживает, а не душит.
	if spent > 6*time.Second {
		t.Fatalf("%d байт уехали за %v — потолок душит вместо того, чтобы придерживать", payload, spent)
	}
}

// Без потолка соединение не придерживается вовсе: у большинства покупателей
// лимита нет, и лишний вызов на каждый кадр им незачем.
func TestWithoutLimitNothingIsHeld(t *testing.T) {
	client, server := pipe(t)
	go func() { _, _ = io.Copy(io.Discard, server) }()

	m := metered.New(client)

	start := time.Now()
	if _, err := m.Write(make([]byte, 1<<20)); err != nil {
		t.Fatalf("запись: %v", err)
	}
	if spent := time.Since(start); spent > time.Second {
		t.Fatalf("мегабайт без потолка уехал за %v", spent)
	}
}

// Счётчики продолжают считать под потолком: за эти байты продавец платит
// хостеру, и ограничение скорости не должно ломать учёт.
func TestCountersKeepWorkingUnderLimit(t *testing.T) {
	client, server := pipe(t)
	go func() { _, _ = io.Copy(io.Discard, server) }()

	m := metered.New(client)
	m.Limit(rate.NewLimiter(rate.Limit(1<<20), 1<<20))

	const n = 4096
	if _, err := m.Write(make([]byte, n)); err != nil {
		t.Fatalf("запись: %v", err)
	}
	if _, down := m.Totals(); down != n {
		t.Fatalf("счётчик отдачи показывает %d вместо %d", down, n)
	}
}

// Снятие потолка возвращает соединению полную скорость: продавец перевёл
// покупателя на тариф без ограничения, и это должно подействовать.
func TestLimitCanBeLifted(t *testing.T) {
	client, server := pipe(t)
	go func() { _, _ = io.Copy(io.Discard, server) }()

	m := metered.New(client)
	m.Limit(rate.NewLimiter(rate.Limit(1<<10), 1<<10))
	m.Limit(nil)

	start := time.Now()
	if _, err := m.Write(make([]byte, 1<<20)); err != nil {
		t.Fatalf("запись: %v", err)
	}
	if spent := time.Since(start); spent > time.Second {
		t.Fatalf("после снятия потолка мегабайт уехал за %v", spent)
	}
}
