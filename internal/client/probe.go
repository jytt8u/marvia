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

	// SpeedTimeout — сколько ждём замера скорости одной ноды.
	//
	// Короче, чем ожидание хендшейка: замер необязателен. Не успели — нода
	// просто сравнится по задержке, и это лучше, чем задержать человека на
	// экране подключения ради точности, которой он не заметит.
	SpeedTimeout = 6 * time.Second
)

// Measurement — результат замера одной ноды.
type Measurement struct {
	Node    Node
	Latency time.Duration
	Err     error

	// Speed — сколько байт в секунду нода отдаёт. Ноль, если не мерили:
	// живая нода одна, или она старой версии и такого не умеет.
	Speed float64

	// dialer остаётся живым только у победителя: переустанавливать
	// соединение сразу после удачного замера — лишний круг по сети.
	dialer *Dialer
}

// OK сообщает, годится ли нода.
func (m Measurement) OK() bool { return m.Err == nil }

// typicalPage — объём, на котором сравниваются ноды.
//
// Мегабайт — порядок обычной страницы с картинками или куска видео.
const typicalPage = 1 << 20

// Cost — сколько эта нода потратит на обычную страницу.
//
// Одно число вместо двух, и оно объясняется вслух: сначала круг до ноды, потом
// сама передача. Замер на живом канале: Дубай отвечал за 150 мс и качал
// 8 Мбит/с — это 1,15 секунды на страницу. Хельсинки: 28 мс и 170 Мбит/с — 83
// миллисекунды. По задержке разница пятикратная, по делу — четырнадцати-, и
// человек чувствует вторую.
//
// Где скорость не мерили, остаётся задержка — как было до сих пор.
func (m Measurement) Cost() time.Duration {
	if m.Speed <= 0 {
		return m.Latency
	}
	return m.Latency + time.Duration(float64(typicalPage)/m.Speed*float64(time.Second))
}

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

	measureSpeeds(ctx, results)

	// Сортируем копию: порядок замеров должен совпадать с порядком нод,
	// иначе отчёт панели уедет не про те ноды.
	ranked := make([]Measurement, len(results))
	copy(ranked, results)
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].OK() != ranked[b].OK() {
			return ranked[a].OK()
		}
		return ranked[a].Cost() < ranked[b].Cost()
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

// measureSpeeds доспрашивает у живых нод, с какой скоростью они отдают данные.
//
// Только когда живых больше одной. Смысл замера — выбрать, а выбирать не из
// чего: единственную ноду мы возьмём в любом случае, и тратить на неё трафик
// продавца незачем.
//
// Неудача замера не выбрасывает ноду. Старая нода такого не умеет и закроет
// поток — это не повод считать её мёртвой: она только что ответила на
// хендшейк. Останется без скорости, и сравнится по задержке, как раньше.
func measureSpeeds(ctx context.Context, results []Measurement) {
	alive := 0
	for _, m := range results {
		if m.OK() && m.dialer != nil {
			alive++
		}
	}
	if alive < 2 {
		return
	}

	var wg sync.WaitGroup
	for i := range results {
		if !results[i].OK() || results[i].dialer == nil {
			continue
		}

		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			speedCtx, cancel := context.WithTimeout(ctx, SpeedTimeout)
			defer cancel()

			speed, err := results[i].dialer.MeasureSpeed(speedCtx, vp1.DefaultSpeedSample)
			if err != nil {
				return
			}
			results[i].Speed = speed
		}(i)
	}
	wg.Wait()
}
