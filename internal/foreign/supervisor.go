package foreign

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Сколько ждём замера одной ноды. Столько же, сколько у VP1: человек не
// станет ждать дольше нескольких секунд, а заблокированная нода не ответит
// и через минуту.
const probeTimeout = 8 * time.Second

// maxParallel — сколько нод меряем разом. Сотня одновременных рукопожатий
// с одного телефона — сама по себе примета для поведенческого анализа.
const maxParallel = 4

// Как часто надзор проверяет, жива ли текущая нода, и сколько неудач подряд
// считать смертью. Xray не говорит об обрыве при дозвоне — поток
// открывается сразу, а ломается потом, — поэтому смерть видна только по
// проверке. Две неудачи подряд, а не одна: одиночный таймаут бывает и у
// живой ноды на плохой сети.
// Переменные, а не константы, — ради тестов: ждать полминуты на проверку
// никто не станет.
var (
	watchEvery   = 30 * time.Second
	deadAfter    = 2
	switchBudget = 40 * time.Second
)

// Supervisor держит подключение к чужой подписке: выбирает ноду, следит за
// ней и переезжает на живую, когда она замолчала. Для телефона и окна он —
// тот же client.Backend, что и надзор VP1.
type Supervisor struct {
	mu       sync.Mutex
	sub      Subscription
	nodes    []client.Node
	engine   *Engine
	current  client.Node
	selected int64
	measured map[int64]client.Measurement
	events   client.Events

	stop chan struct{}
	once sync.Once

	// Ритм надзора — свой у каждого, снятый при запуске.
	every time.Duration
	dead  int
}

var _ client.Backend = (*Supervisor)(nil)

// NodeID — постоянный номер ноды: из ссылки, а не из места в списке.
// Подписка приходит заново каждый день, и порядок в ней меняется; номер
// выбранной руками ноды хранится в приложении и должен найти её снова.
func NodeID(l Link) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(l.Raw))
	return int64(h.Sum64() >> 1)
}

func nodeOf(l Link) client.Node {
	return client.Node{ID: NodeID(l), Name: l.Title(), Address: net.JoinHostPort(l.Host, strconv.Itoa(l.Port))}
}

// Supervise меряет ноды подписки, поднимает самую быструю (или выбранную
// руками, если она жива) и начинает следить за ней.
func Supervise(ctx context.Context, sub Subscription, prefer int64, events client.Events) (*Supervisor, []client.Measurement, error) {
	if len(sub.Links) == 0 {
		return nil, nil, errors.New("в подписке нет ни одной ноды")
	}
	s := &Supervisor{sub: sub, selected: prefer, events: events, measured: map[int64]client.Measurement{}, stop: make(chan struct{}), every: watchEvery, dead: deadAfter}
	for _, l := range sub.Links {
		s.nodes = append(s.nodes, nodeOf(l))
	}
	results, winner := s.pick(ctx, sub.Links, prefer)
	if winner == nil {
		// С причиной первой неудачи: «не отвечает» без причины не говорит ни
		// человеку, ни продавцу, куда смотреть — сеть, ключ или сама нода.
		for _, m := range results {
			if m.Err != nil {
				return nil, results, fmt.Errorf("ни одна нода подписки не отвечает: %w", m.Err)
			}
		}
		return nil, results, errors.New("ни одна нода подписки не отвечает")
	}
	s.engine = winner
	s.current = nodeOf(winner.Link())
	go s.watch()
	return s, results, nil
}

// pick меряет ноды и отдаёт движок победителя; остальные закрывает.
// Выбранная руками выигрывает, если жива, — иначе самая быстрая.
func (s *Supervisor) pick(ctx context.Context, links []Link, prefer int64) ([]client.Measurement, *Engine) {
	type result struct {
		m client.Measurement
		e *Engine
	}
	out := make([]result, len(links))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i, l := range links {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			n := nodeOf(l)
			e, err := Start(l)
			if err != nil {
				out[i] = result{m: client.Measurement{Node: n, Err: err}}
				return
			}
			pctx, cancel := context.WithTimeout(ctx, probeTimeout)
			rtt, err := e.Probe(pctx)
			cancel()
			if err != nil {
				_ = e.Close()
				out[i] = result{m: client.Measurement{Node: n, Err: err}}
				return
			}
			out[i] = result{m: client.Measurement{Node: n, Latency: rtt, RTT: rtt}, e: e}
		}()
	}
	wg.Wait()

	best := -1
	for i, r := range out {
		if r.e == nil {
			continue
		}
		if prefer != 0 && r.m.Node.ID == prefer {
			best = i
			break
		}
		if best < 0 || r.m.RTT < out[best].m.RTT {
			best = i
		}
	}
	measurements := make([]client.Measurement, len(out))
	var winner *Engine
	s.mu.Lock()
	for i, r := range out {
		measurements[i] = r.m
		s.measured[r.m.Node.ID] = r.m
		if i == best {
			winner = r.e
		} else if r.e != nil {
			_ = r.e.Close()
		}
	}
	s.mu.Unlock()
	return measurements, winner
}

// watch проверяет текущую ноду и переезжает, когда она умерла.
func (s *Supervisor) watch() {
	fails := 0
	t := time.NewTicker(s.every)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		_, err := s.Ping(ctx)
		cancel()
		if err == nil {
			if fails >= s.dead && s.events.OnRecovered != nil {
				s.events.OnRecovered()
			}
			fails = 0
			continue
		}
		fails++
		if fails < s.dead {
			continue
		}
		if !s.move() && s.events.OnTrouble != nil {
			s.events.OnTrouble("no-node")
		}
	}
}

// move ищет живую ноду среди остальных и ставит её на место умершей.
func (s *Supervisor) move() bool {
	s.mu.Lock()
	dead := s.current.ID
	var others []Link
	for _, l := range s.sub.Links {
		if NodeID(l) != dead {
			others = append(others, l)
		}
	}
	s.mu.Unlock()
	if len(others) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), switchBudget)
	defer cancel()
	_, winner := s.pick(ctx, others, 0)
	if winner == nil {
		return false
	}
	s.swap(winner)
	return true
}

// swap ставит новый движок. Старый закрывается — его потоки рвутся, и
// приложения переподключаются уже через новую ноду.
func (s *Supervisor) swap(e *Engine) {
	s.mu.Lock()
	old := s.engine
	s.engine = e
	s.current = nodeOf(e.Link())
	n := s.current
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	if s.events.OnSwitch != nil {
		s.events.OnSwitch(n)
	}
}

func (s *Supervisor) now() (*Engine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return nil, errors.New("подключение закрыто")
	}
	return s.engine, nil
}

// DialTarget открывает поток до цели через текущую ноду.
func (s *Supervisor) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	e, err := s.now()
	if err != nil {
		return nil, err
	}
	return e.DialTarget(ctx, target)
}

// DialDatagrams — то же для датаграмм.
func (s *Supervisor) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	e, err := s.now()
	if err != nil {
		return nil, err
	}
	return e.DialDatagrams(ctx, target)
}

func (s *Supervisor) Node() client.Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *Supervisor) Nodes() []client.Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]client.Node(nil), s.nodes...)
}

func (s *Supervisor) Measurement() client.Measurement {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.measured[s.current.ID]
}

// Measure меряет все ноды заново. Текущую — через уже поднятый движок, чтобы
// не рвать ей соединения ради замера.
func (s *Supervisor) Measure(ctx context.Context) []client.Measurement {
	s.mu.Lock()
	cur := s.current.ID
	var others []Link
	for _, l := range s.sub.Links {
		if NodeID(l) != cur {
			others = append(others, l)
		}
	}
	s.mu.Unlock()

	_, _ = s.Ping(ctx)
	results, winner := s.pickAll(ctx, others)
	if winner != nil {
		_ = winner.Close()
	}
	s.mu.Lock()
	all := append([]client.Measurement{s.measured[cur]}, results...)
	s.mu.Unlock()
	sort.SliceStable(all, func(i, j int) bool { return all[i].Node.ID == cur && all[j].Node.ID != cur })
	return all
}

// pickAll — замер без выбора: все движки после него закрываются.
func (s *Supervisor) pickAll(ctx context.Context, links []Link) ([]client.Measurement, *Engine) {
	if len(links) == 0 {
		return nil, nil
	}
	return s.pick(ctx, links, 0)
}

// Ping — отклик текущей ноды настоящим запросом через неё.
func (s *Supervisor) Ping(ctx context.Context) (time.Duration, error) {
	e, err := s.now()
	if err != nil {
		return 0, err
	}
	rtt, err := e.Probe(ctx)
	s.mu.Lock()
	m := client.Measurement{Node: s.current, Latency: rtt, RTT: rtt, Err: err}
	s.measured[s.current.ID] = m
	s.mu.Unlock()
	return rtt, err
}

// Select переводит на выбранную ноду; ноль — к самой быстрой.
func (s *Supervisor) Select(ctx context.Context, id int64) error {
	s.mu.Lock()
	s.selected = id
	var target []Link
	for _, l := range s.sub.Links {
		if id == 0 || NodeID(l) == id {
			target = append(target, l)
		}
	}
	s.mu.Unlock()
	if len(target) == 0 {
		return errors.New("такой ноды в подписке нет")
	}
	_, winner := s.pick(ctx, target, id)
	if winner == nil {
		return errors.New("нода не отвечает")
	}
	s.swap(winner)
	return nil
}

func (s *Supervisor) Selected() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selected
}

// Subscription — подписка в том виде, в каком её понимает приложение.
func (s *Supervisor) Subscription() client.Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	return View(s.sub)
}

// View переводит чужую подписку в вид, который показывает приложение.
func View(sub Subscription) client.Subscription {
	out := client.Subscription{}
	for _, l := range sub.Links {
		out.Nodes = append(out.Nodes, nodeOf(l))
	}
	if sub.Total > 0 {
		out.TrafficLimit = sub.Total
		out.Used = sub.Upload + sub.Download
	}
	if !sub.Expire.IsZero() {
		out.ExpiresAt = sub.Expire.UTC().Format(time.RFC3339)
	}
	return out
}

// Close останавливает надзор и движок.
func (s *Supervisor) Close() error {
	s.once.Do(func() { close(s.stop) })
	s.mu.Lock()
	e := s.engine
	s.engine = nil
	s.mu.Unlock()
	if e != nil {
		return e.Close()
	}
	return nil
}

// MeasureAll меряет ноды без подключения — для списка серверов, когда туннель
// не поднят. Все движки после замера закрываются.
func MeasureAll(ctx context.Context, links []Link) []client.Measurement {
	s := &Supervisor{measured: map[int64]client.Measurement{}}
	results, winner := s.pick(ctx, links, 0)
	if winner != nil {
		_ = winner.Close()
	}
	return results
}
