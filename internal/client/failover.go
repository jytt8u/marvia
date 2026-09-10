package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Переезд между нодами на живом туннеле.
//
// Ноду выбирали один раз — при подключении, — и дальше держались за неё, что
// бы с ней ни случилось. Проверка на живом показала, чем это оборачивается:
// заблокированную ноду пакеты не отвергают, а роняют молча, поэтому туннель не
// рвётся, а глохнет. Приложение продолжает писать «Подключено», видео доигрывает
// из буфера, а всё остальное просто не грузится — комментарии, картинки, любой
// новый запрос. Человек не понимает, что сломалось, и винит телефон, оператора,
// сайт: кого угодно, кроме нас.
//
// Обещание при этом было прямо противоположное: «ноду заблокировали, покупатель
// не заметил». Supervisor его выполняет — сам замечает, что нода замолчала, сам
// выбирает другую и подменяет дозвон под мостом.

// Значения — переменные, а не константы, ради тестов: проверка переезда с
// настоящей полуминутой шла бы дольше, чем её кто-нибудь согласится ждать, а
// проверку, которую не ждут, перестают запускать.
var (
	// Как часто щупаем текущую ноду.
	//
	// Полминуты — предел того, сколько человек согласен смотреть на
	// наполовину работающий интернет, не начав искать виноватого.
	probeEvery = 30 * time.Second

	// Сколько ждём ответа. Живая нода отвечает за доли секунды; молчащая не
	// ответит и за минуту, а ждать минуту значит растянуть поломку впятеро.
	probeTimeout = 8 * time.Second

	// Сколько проверок подряд должны провалиться, прежде чем переезжать.
	//
	// Одиночный промах — это моргнувший мобильный интернет, и переезжать по
	// нему значит гонять человека между нодами на каждой поездке в метро.
	probeMisses = 2

	// Сколько байт просим на проверку: килобайт раз в полминуты.
	probeSample = 1024

	// Сколько даём на сам переезд: он и есть обычное подключение — поход в
	// панель, замеры всех нод, — только человек его не нажимал.
	moveTimeout = 80 * time.Second

	// Сколько ждём между неудачными попытками переезда.
	//
	// Если не отвечает ни одна нода — дело не в ноде, а в сети у человека.
	// Долбить панель и все ноды подряд в этом случае незачем.
	retryAfter = 20 * time.Second
)

// Events — то, о чём надзор сообщает наружу.
//
// Оба обработчика необязательны и оба зовутся из сторожевой горутины, поэтому
// внутри нельзя ни блокироваться надолго, ни трогать туннель: своё состояние
// поправить и выйти.
type Events struct {
	// OnSwitch — переехали на другую ноду. Окну надо переписать имя: иначе
	// оно будет показывать ту, через которую трафик давно не идёт.
	OnSwitch func(Node)

	// OnTrouble — нода замолчала, а переехать не вышло: не ответила ни одна.
	//
	// Это единственный случай, когда человеку надо сказать. Удачный переезд
	// он замечать не должен — в этом и смысл, — а вот «сейчас не работает
	// ничего» лучше прочитать у нас, чем выяснять самому.
	OnTrouble func(reason string)
}

// Supervisor — дозвон, который сам меняет ноду, когда текущая замолчала.
//
// Подставляется мосту вместо *Dialer: методы те же, а внутри живёт текущий
// дозвон, который можно заменить, не трогая ни мост, ни интерфейс.
type Supervisor struct {
	mu     sync.Mutex
	dialer *Dialer
	closed bool

	cfg ConnectConfig
	log func(string, ...any)

	events Events

	cancel context.CancelFunc
	done   chan struct{}
}

// Supervise подключается и заводит сторожа.
//
// Первое подключение ничем не отличается от прежнего Connect: те же кэш,
// подписка и замеры. Разница начинается после — с этой минуты за нодой следят.
func Supervise(ctx context.Context, cfg ConnectConfig, events Events) (*Supervisor, []Measurement, error) {
	dialer, m, err := Connect(ctx, cfg)
	if err != nil {
		return nil, m, err
	}

	logf := cfg.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	// Сторож живёт своей жизнью, а не жизнью вызова: ctx у Connect кончается
	// вместе с подключением, а следить надо всё время, пока стоит туннель.
	watchCtx, cancel := context.WithCancel(context.Background())

	s := &Supervisor{
		dialer: dialer,
		cfg:    cfg,
		log:    logf,
		events: events,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go s.watch(watchCtx)
	return s, m, nil
}

// DialTarget открывает поток до цели через ту ноду, которая сейчас выбрана.
func (s *Supervisor) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	d, err := s.current()
	if err != nil {
		return nil, err
	}
	return d.DialTarget(ctx, target)
}

// DialDatagrams — то же для датаграмм.
func (s *Supervisor) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	d, err := s.current()
	if err != nil {
		return nil, err
	}
	return d.DialDatagrams(ctx, target)
}

// Node — нода, через которую идёт трафик прямо сейчас.
func (s *Supervisor) Node() Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return Node{}
	}
	return s.dialer.Node()
}

// Subscription — срок и остаток квоты.
func (s *Supervisor) Subscription() Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return Subscription{}
	}
	return s.dialer.Subscription()
}

// Close гасит сторожа и закрывает текущий дозвон.
func (s *Supervisor) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	d := s.dialer
	s.dialer = nil
	s.mu.Unlock()

	s.cancel()
	<-s.done

	if d != nil {
		return d.Close()
	}
	return nil
}

func (s *Supervisor) current() (*Dialer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return nil, errors.New("туннель закрыт")
	}
	return s.dialer, nil
}

// watch щупает текущую ноду и переезжает, когда она перестаёт отвечать.
func (s *Supervisor) watch(ctx context.Context) {
	defer close(s.done)

	ticker := time.NewTicker(probeEvery)
	defer ticker.Stop()

	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		d, err := s.current()
		if err != nil {
			return
		}

		// Щупаем тем же замером, каким выбирали ноду: он проходит весь путь —
		// сессия, поток, ответ ноды, — и потому ловит не только оборванный
		// провод, но и ноду, которая жива, а обслуживать перестала.
		probe, cancel := context.WithTimeout(ctx, probeTimeout)
		_, err = d.MeasureFetch(probe, probeSample)
		cancel()

		if ctx.Err() != nil {
			return
		}
		if err == nil {
			misses = 0
			continue
		}

		misses++
		if misses < probeMisses {
			continue
		}

		s.log("нода %s не отвечает на %d проверки подряд: %v", d.Node().Title(), misses, err)
		if s.move(ctx, d) {
			misses = 0
			continue
		}

		// Переехать не вышло — молчать об этом нельзя. Удачный переезд человек
		// замечать не должен, а «не отвечает ни одна нода» лучше прочитать у
		// нас, чем гадать, почему интернет наполовину.
		if s.events.OnTrouble != nil {
			s.events.OnTrouble("нода не отвечает, и переехать не на что")
		}

		// Переехать не вышло — подождём и попробуем снова. Счётчик не
		// сбрасываем: следующая же неудачная проверка снова приведёт сюда.
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryAfter):
		}
	}
}

// move выбирает другую ноду и подменяет дозвон.
//
// Список берётся обычным путём — сначала кэш, потом панель, — то есть тем же
// Connect, что и при первом подключении. Мёртвую ноду отдельно исключать не
// надо: замер до неё не пройдёт, и SelectBest отложит её сам. Если она к тому
// же выбыла из подписки, панель её и не пришлёт.
func (s *Supervisor) move(ctx context.Context, dead *Dialer) bool {
	// На переезд даём столько же, сколько на обычное подключение: он и есть
	// обычное подключение, только человек его не нажимал.
	pick, cancel := context.WithTimeout(ctx, moveTimeout)
	defer cancel()

	fresh, _, err := Connect(pick, s.cfg)
	if err != nil {
		s.log("переехать не удалось: %v", err)
		return false
	}

	if ctx.Err() != nil {
		_ = fresh.Close()
		return false
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = fresh.Close()
		return false
	}
	same := s.dialer != nil && s.dialer.Node().Address == fresh.Node().Address
	s.dialer = fresh
	s.mu.Unlock()

	// Старый дозвон закрываем после подмены, а не до: между «закрыл» и
	// «поставил новый» мост остался бы без дозвона, и запросы в этот зазор
	// получили бы ошибку на ровном месте.
	if dead != nil {
		_ = dead.Close()
	}

	if same {
		// Выбор вернулся к той же ноде: значит она ожила, пока мы искали
		// замену. Соединение всё равно новое — старое-то не работало.
		s.log("нода %s ответила заново, туннель пересобран", fresh.Node().Title())
	} else {
		s.log("переехали на %s", fresh.Node().Title())
	}

	if s.events.OnSwitch != nil {
		s.events.OnSwitch(fresh.Node())
	}
	return true
}
