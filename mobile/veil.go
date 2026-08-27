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
	"path/filepath"
	"strings"
	"sync"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/tunbridge"
	"github.com/veilproject/veil/internal/vp1"
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

	dialer *client.Dialer
	bridge *tunbridge.Bridge

	nodeName  string
	lastError string
	running   bool
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

	dialer, err := connect(accountLink, cacheDir)
	if err != nil {
		return nil, err
	}

	t := &Tunnel{dialer: dialer, nodeName: dialer.Node().Title(), running: true}

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
	t.bridge = bridge

	return t, nil
}

// connect делает всё, что не касается сетевого интерфейса: разбирает ссылку,
// забирает список нод и выбирает лучшую.
//
// Вынесено отдельно не ради красоты: так эту часть можно проверить тестом, не
// выдумывая дескриптор интерфейса. Выдуманный дескриптор в тесте — это номер,
// который на Linux принадлежит чему-то настоящему.
func connect(accountLink, cacheDir string) (*client.Dialer, error) {
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
	dialer, measurements, err := client.Connect(context.Background(), client.ConnectConfig{
		Account:   account,
		Key:       key,
		CachePath: cachePath(cacheDir),
	})

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
	t.mu.Unlock()
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
