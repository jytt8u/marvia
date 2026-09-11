// Package mobile — то, что видит приложение на телефоне.
//
// Пакет собирается в библиотеку через gomobile bind, поэтому здесь намеренно
// нет ничего сложного в сигнатурах: только строки, числа и указатели на типы
// этого же пакета. Всё остальное gomobile переносить не умеет, а ловить это
// на этапе сборки под Android — худшее место для такого сюрприза.
//
// Разделение обязанностей такое. Приложение на Kotlin поднимает VpnService,
// получает от системы файловый дескриптор сетевого интерфейса и отдаёт его
// сюда. Дальше всё делает Go: разбирает пакеты, поднимает туннель, считает
// трафик. Логика не дублируется между платформами — на настольной машине тот
// же код работает под SOCKS вместо интерфейса.
package mobile

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

// DefaultDNS — куда уходят перехваченные запросы имён.
//
// Через туннель и по TCP: запрос имени, ушедший мимо туннеля, выдаёт цензору
// весь список посещённых сайтов, даже когда сам трафик он расшифровать не
// может.
const DefaultDNS = "1.1.1.1:53"

// CacheName — имя файла с кэшем подписки внутри каталога приложения.
//
// Наружу вынесено не ради красоты: приложению это имя нужно, чтобы стереть
// кэш вместе со ссылкой доступа, когда человек удаляет ключ.
const CacheName = "subscription.json"

// Сколько живёт сообщение о неудаче, прежде чем погаснуть.
//
// Минута: достаточно, чтобы человек успел прочитать, и мало, чтобы старая
// неудача не висела рядом с работающим туннелем.
const errorLifetime = time.Minute

// Виды неудач.
//
// Текст ошибки из ядра точен и подробен, но покупателю он не говорит ничего:
// «dial tcp: lookup panel.example: no such host» — язык не для человека,
// который заплатил за интернет. Поэтому ядро сообщает вместе с подробностями
// ещё и вид неудачи, а человеческую фразу под него подбирает приложение.
//
// Разделение именно такое, а не «пусть ядро сразу пишет по-человечески»,
// потому что тогда русский текст интерфейса расползся бы по ядру. А он нужен
// будет ещё на фарси и на китайском — это те же страны, ради которых всё и
// затевалось.
//
// Вид едет первой строкой сообщения, подробности — следующими.
const (
	// FailAccount — ссылка доступа не та: испорчена при пересылке, обрезана,
	// выдана не нами.
	FailAccount = "account"

	// FailPanel — до панели продавца не достучались. Обычно это просто
	// отсутствие интернета, но может быть и блокировка самой панели.
	FailPanel = "panel"

	// FailNodes — панель ответила, но подключиться не вышло ни к одной ноде.
	FailNodes = "nodes"

	// FailSystem — не сложилось на стороне телефона: система не дала
	// интерфейс, не поднялся сетевой мост.
	FailSystem = "system"

	// FailExpired — кончился срок подписки.
	// FailQuota — кончился оплаченный трафик.
	//
	// Отдельно от FailNodes, и это главное: раньше кончившаяся подписка
	// показывалась как «ни один сервер не отвечает». Человек читал, что
	// сломались мы, шёл к продавцу с «у вас всё лежит», и продавец разбирался
	// с каждым таким вручную — хотя надо было одно слово «продли».
	FailExpired = "expired"
	FailQuota   = "quota"
)

// fail помечает ошибку видом: первая строка — вид, остальное — подробности.
func fail(kind string, err error) error {
	return fmt.Errorf("%s\n%w", kind, err)
}

// Tunnel — работающее подключение.
type Tunnel struct {
	mu sync.Mutex

	// Не просто дозвон, а надзор над ним: он сам меняет ноду, когда текущая
	// замолчала, и подменяет её под мостом. Мост об этом не знает.
	dialer *client.Supervisor
	bridge *tunbridge.Bridge

	nodeName string
	running  bool

	// Последняя неудача и когда она случилась.
	//
	// Время нужно, чтобы забывать: отдельный неудавшийся поток — обычное дело
	// в интернете, а не поломка туннеля. Цель могла быть недоступна, заблокирована
	// или просто не иметь IPv6, до которого ноде не дотянуться. Показывать такое
	// рядом с надписью «Подключено» и не стирать значит приучить не читать эту
	// строку вовсе — а она понадобится, когда сломается что-то настоящее.
	lastError string
	lastErrAt time.Time

	// troubleCode — держащаяся беда: нода молчит, и переехать не на что.
	// Живёт до тех пор, пока надзор не скажет, что связь вернулась.
	troubleCode string
}

// Start поднимает туннель поверх сетевого интерфейса, полученного от системы.
//
// accountLink — ссылка, которую покупатель получил от бота.
// tunFD — дескриптор от VpnService.
// dns — адрес для запросов имён; пусто означает значение по умолчанию.
// cacheDir — каталог приложения под кэш подписки; пусто означает работу без
// кэша, как было раньше.
//
// Каталог передаёт приложение, а не выясняет ядро: на Android путь к своим
// файлам знает только Context, и достать его из Go неоткуда.
func Start(accountLink string, tunFD int, dns string, cacheDir string) (*Tunnel, error) {
	if tunFD <= 0 {
		return nil, fail(FailSystem, errors.New("не передан дескриптор сетевого интерфейса"))
	}

	// Дескриптор нам отдали насовсем: приложение вызвало detachFd и само его
	// уже не закроет. Пока за него не взялся мост, отвечаем за него мы — иначе
	// каждая неудачная попытка подключения оставляла бы висеть по интерфейсу.
	bridgeOwnsFD := false
	defer func() {
		if !bridgeOwnsFD {
			tunbridge.CloseFD(tunFD)
		}
	}()
	if strings.TrimSpace(dns) == "" {
		dns = DefaultDNS
	}

	t := &Tunnel{running: true}

	// Имя ноды переписываем при переезде: иначе окно будет показывать ту,
	// через которую трафик давно не идёт.
	dialer, err := connect(accountLink, cacheDir, client.Events{OnSwitch: t.switched, OnTrouble: t.trouble, OnRecovered: t.recovered})
	if err != nil {
		return nil, err
	}
	t.dialer = dialer
	t.nodeName = dialer.Node().Title()

	bridgeOwnsFD = true
	bridge, err := tunbridge.Start(tunbridge.Config{
		FD:      tunFD,
		Dialer:  dialer,
		DNS:     dns,
		OnError: t.note,
	})
	if err != nil {
		_ = dialer.Close()
		return nil, fail(FailSystem, fmt.Errorf("сетевой мост: %w", err))
	}
	// Под замком: надзор уже работает и может позвать switched в любой миг, а
	// тот читает мост, чтобы снять с него запрет датаграмм.
	t.mu.Lock()
	t.bridge = bridge
	t.mu.Unlock()

	return t, nil
}

// connect делает всё, что не касается сетевого интерфейса: разбирает ссылку,
// забирает список нод и выбирает лучшую.
//
// Вынесено отдельно не ради красоты: так эту часть можно проверить тестом, не
// выдумывая дескриптор интерфейса. Выдуманный дескриптор в тесте — это номер,
// который на Linux принадлежит чему-то настоящему.
func connect(accountLink, cacheDir string, events client.Events) (*client.Supervisor, error) {
	account, err := client.ParseAccountLink(accountLink)
	if err != nil {
		return nil, fail(FailAccount, fmt.Errorf("ссылка доступа: %w", err))
	}

	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return nil, fail(FailAccount, fmt.Errorf("личный ключ: %w", err))
	}

	// Ноду выбираем замерами с самого устройства, а не берём первую из
	// списка: панель стоит за границей и видит ноду живой ровно тогда, когда
	// для телефона в Иркутске она уже мертва.
	//
	// Список нод при этом по возможности берём из кэша: каждый поход в панель
	// — это запрос имени её домена, а он с недавних пор уходит провайдеру
	// открытым текстом. Подробности в internal/client/cache.go.
	// Supervise, а не Connect: подключение то же самое, но за нодой дальше
	// следят и меняют её, если она замолчала. Без этого туннель, чью ноду
	// заблокировали, не рвётся — он глохнет, и телефон продолжает показывать
	// «Подключено», пока у человека наполовину не грузится всё подряд.
	//
	// Журнал ядра прокидываем обязательно.
	//
	// Без него надзор на телефоне нем: ни «нода не отвечает», ни «переехали»,
	// ни «переехать не удалось» наружу не выходят, и живая проверка не может
	// отличить «сторож не сработал» от «сторож сработал и не смог». Ровно на
	// этом застряла первая проверка переезда.
	dialer, measurements, err := client.Supervise(context.Background(), client.ConnectConfig{
		Account:   account,
		Key:       key,
		CachePath: cachePath(cacheDir),
		Log:       func(format string, args ...any) { log.Printf(format, args...) },
	}, events)

	// Отчёт уходит в любом случае, в том числе когда не подключилось ни к
	// одной ноде: продавцу важнее всего узнать именно про такой случай.
	go func() {
		if err := client.SendReports(context.Background(), account.SubscriptionURL, client.ReportsFrom(measurements)); err != nil {
			// Панель недоступна — не повод не работать. Туннель от неё не
			// зависит, список нод уже получен.
			_ = err
		}
	}()

	if err != nil {
		// Вид неудачи человека ведёт в разные стороны: до панели не
		// достучались — проверь интернет, ноды молчат — пиши продавцу,
		// подписка кончилась — продли, и никуда писать не надо.
		switch {
		case errors.Is(err, client.ErrExpired):
			return nil, fail(FailExpired, err)
		case errors.Is(err, client.ErrQuota):
			return nil, fail(FailQuota, err)
		case errors.Is(err, client.ErrPanel):
			// Без «список нод:» сверху: ошибка и так начинается с «панель
			// недоступна», а три слоя пояснений подряд читать невозможно.
			return nil, fail(FailPanel, err)
		}
		return nil, fail(FailNodes, err)
	}
	return dialer, nil
}

// cachePath — где лежит кэш подписки. Пустой каталог означает работу без кэша.
func cachePath(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, CacheName)
}

// note запоминает последнюю ошибку, чтобы приложение могло её показать.
//
// Ошибки отдельных соединений не должны ронять туннель: одна недоступная цель
// — это норма, а не повод обрывать человеку весь интернет.
func (t *Tunnel) note(err error) {
	if err == nil {
		return
	}
	t.mu.Lock()
	t.lastError = err.Error()
	t.lastErrAt = time.Now()
	t.mu.Unlock()
}

// switched зовётся надзором после переезда на другую ноду.
//
// Имя переписываем обязательно: иначе окно продолжит показывать ноду, через
// которую трафик давно не идёт, и человек, глядя на «Финляндия», будет думать
// про Финляндию, когда он уже в Дубае. Прежнюю ошибку заодно стираем —
// переезд её и лечил.
func (t *Tunnel) switched(n client.Node) {
	t.mu.Lock()
	t.nodeName = n.Title()
	t.lastError = ""
	bridge := t.bridge
	t.mu.Unlock()

	// Мосту говорим отдельно: он мог выключить датаграммы, решив, что нода их
	// не умеет, а на самом деле нода в тот миг умирала. Новая нода за это не
	// отвечает.
	bridge.NodeChanged()
}

// trouble — нода замолчала, а переехать не на что.
//
// Единственный случай, когда человеку про переезд надо сказать. Удачный он
// замечать не должен: в этом весь смысл. А вот «сейчас не работает ничего»
// лучше прочитать у нас, чем выяснять самому, почему интернет наполовину.
//
// Кладём в отдельное поле, а не в общую строку ошибки, по двум причинам.
//
// Первая: беда должна держаться, пока не кончится, а разовая неудача одного
// потока — гаснуть через минуту. В одном поле это несовместимо, и раньше
// сообщение о беде мигало: надзор ставил его заново раз в минуту с лишним, а
// гасло оно ровно через минуту.
//
// Вторая: здесь код, а не фраза. Фразу подбирает приложение на своём языке —
// иначе английский интерфейс показывает русское предложение из ядра.
func (t *Tunnel) trouble(code string) {
	t.mu.Lock()
	t.troubleCode = code
	t.mu.Unlock()
}

// recovered — нода снова отвечает после того, как её объявили молчащей.
//
// Без этого надпись про беду не гасла бы никогда: надзор ставит её, а снять
// было некому. Человек смотрел бы на «ничего не работает» после того, как всё
// заработало.
func (t *Tunnel) recovered() {
	t.mu.Lock()
	t.troubleCode = ""
	t.mu.Unlock()
}

// Trouble отдаёт код беды: пусто — беды нет.
//
// Приложение подбирает под код свою надпись. Разовые неудачи отдельных
// соединений сюда не попадают — они в LastError и гаснут сами.
func (t *Tunnel) Trouble() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.troubleCode
}

// Stop закрывает туннель. Безопасно вызывать несколько раз.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	bridge, dialer := t.bridge, t.dialer
	t.mu.Unlock()

	var first error
	if bridge != nil {
		if err := bridge.Close(); err != nil {
			first = err
		}
	}
	if dialer != nil {
		if err := dialer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Running сообщает, поднят ли туннель.
func (t *Tunnel) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// NodeName — имя ноды, через которую идёт трафик.
func (t *Tunnel) NodeName() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.nodeName
}

// LastError — последняя ошибка отдельного соединения, для показа в интерфейсе.
// Пустая строка означает, что ошибок не было.
func (t *Tunnel) LastError() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Забываем старое. Настоящая беда о себе напомнит: надзор, не сумевший
	// переехать, ставит её заново каждые двадцать секунд, и строка не гаснет.
	// А единственный неудавшийся поток погаснет — ему там и место.
	if t.lastError != "" && time.Since(t.lastErrAt) > errorLifetime {
		t.lastError = ""
	}
	return t.lastError
}

// CheckAccountLink проверяет ссылку, ничего не подключая.
//
// Нужно интерфейсу: сказать «ссылка не та» сразу при вставке гораздо лучше,
// чем после неудачной попытки соединения.
func CheckAccountLink(accountLink string) error {
	_, err := client.ParseAccountLink(accountLink)
	return err
}

// Until — до какого числа оплачена подписка, в виде «2026-09-27».
// Пустая строка означает «без ограничения по сроку».
//
// Приложение обязано уметь ответить на «сколько у меня осталось» само.
// Иначе на этот вопрос отвечает продавец — каждому и вручную.
func (t *Tunnel) Until() string {
	t.mu.Lock()
	dialer := t.dialer
	t.mu.Unlock()

	if dialer == nil {
		return ""
	}
	until, set := dialer.Subscription().Until()
	if !set {
		return ""
	}
	return until.Local().Format("2006-01-02")
}

// TrafficLimit — сколько байт оплачено. Ноль означает «без ограничения».
func (t *Tunnel) TrafficLimit() int64 {
	t.mu.Lock()
	dialer := t.dialer
	t.mu.Unlock()

	if dialer == nil {
		return 0
	}
	return dialer.Subscription().TrafficLimit
}

// TrafficLeft — сколько байт осталось. Ноль означает либо «без ограничения»,
// либо «всё выбрано»; отличать по TrafficLimit.
func (t *Tunnel) TrafficLeft() int64 {
	t.mu.Lock()
	dialer := t.dialer
	t.mu.Unlock()

	if dialer == nil {
		return 0
	}
	return dialer.Subscription().Remaining()
}
