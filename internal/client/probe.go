package client

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/veilproject/veil/internal/vp1"
)

// Замер доступности ноды.
//
// Панель стоит за границей и видит ноду живой ровно тогда, когда для телефона
// в Иркутске она уже мертва. Единственный, кто знает правду, — само
// устройство, поэтому мерить приходится с него.
//
// Меряем не время установки TCP, а время до работающего туннеля. Разница
// принципиальная: заблокированная нода обычно принимает TCP-соединение и
// молча роняет пакеты дальше, уже во время TLS. Проверка «порт открыт»
// показала бы такую ноду живой.

const (
	// ProbeTimeout — сколько ждём одну ноду. Больше нет смысла: человек не
	// станет ждать соединения дольше нескольких секунд.
	ProbeTimeout = 8 * time.Second

	// maxParallelProbes — сколько нод щупаем одновременно.
	//
	// Ограничение не ради экономии: пачка одновременных хендшейков — сама по
	// себе примета, по которой поведенческий анализ узнаёт туннель.
	maxParallelProbes = 4
)

// Measurement — результат замера одной ноды.
type Measurement struct {
	Node    Node
	Latency time.Duration
	Err     error

	// dialer остаётся живым только у победителя: переустанавливать
	// соединение сразу после удачного замера — лишний круг по сети.
	dialer *Dialer
}

// OK сообщает, годится ли нода.
func (m Measurement) OK() bool { return m.Err == nil }

// Probe измеряет одну ноду.
func Probe(ctx context.Context, node Node, key vp1.KeyPair, opts Options) Measurement {
	start := time.Now()

	dialer, err := NewDialer(node, key, opts)
	if err != nil {
		return Measurement{Node: node, Err: err}
	}

	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	if err := dialer.Warmup(probeCtx); err != nil {
		_ = dialer.Close()
		return Measurement{Node: node, Latency: time.Since(start), Err: err}
	}

	return Measurement{Node: node, Latency: time.Since(start), dialer: dialer}
}

// SelectBest меряет ноды и возвращает дозвон до самой быстрой живой.
//
// Возвращаются все замеры, а не только победитель: их надо отправить панели,
// иначе продавец так и не узнает, что половина его нод не работает у людей.
func SelectBest(ctx context.Context, nodes []Node, key vp1.KeyPair, opts Options) (*Dialer, []Measurement, error) {
	if len(nodes) == 0 {
		return nil, nil, errors.New("список нод пуст")
	}

	results := make([]Measurement, len(nodes))
	slots := make(chan struct{}, maxParallelProbes)
	var wg sync.WaitGroup

	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node Node) {
			defer wg.Done()

			slots <- struct{}{}
			defer func() { <-slots }()

			results[i] = Probe(ctx, node, key, opts)
		}(i, node)
	}
	wg.Wait()

	// Сортируем копию: порядок замеров должен совпадать с порядком нод,
	// иначе отчёт панели уедет не про те ноды.
	ranked := make([]Measurement, len(results))
	copy(ranked, results)
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].OK() != ranked[b].OK() {
			return ranked[a].OK()
		}
		return ranked[a].Latency < ranked[b].Latency
	})

	winner := ranked[0]
	if !winner.OK() {
		closeAll(results, nil)
		return nil, results, fmt.Errorf("ни одна нода не ответила: %w", winner.Err)
	}

	closeAll(results, winner.dialer)
	return winner.dialer, results, nil
}

// closeAll закрывает все соединения, кроме соединения победителя.
func closeAll(list []Measurement, keep *Dialer) {
	for _, m := range list {
		if m.dialer != nil && m.dialer != keep {
			_ = m.dialer.Close()
		}
	}
}

// Warmup доводит соединение до готовности, ничего не передавая.
//
// Открытие потока протаскивает через всё: внешний слой, хендшейк VP1 и
// мультиплексор. Именно это и есть «нода работает», в отличие от «порт
// отвечает».
func (d *Dialer) Warmup(ctx context.Context) error {
	stream, err := d.pool.Open(ctx)
	if err != nil {
		return err
	}
	return stream.Close()
}
