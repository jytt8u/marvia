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
func Start(accountLink string, tunFD int, dns string) (*Tunnel, error) {
	if tunFD <= 0 {
		return nil, errors.New("не передан дескриптор сетевого интерфейса")
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

	account, err := client.ParseAccountLink(accountLink)
	if err != nil {
		return nil, fmt.Errorf("ссылка доступа: %w", err)
	}

	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("личный ключ: %w", err)
	}

	sub, err := client.FetchSubscription(context.Background(), account.SubscriptionURL)
	if err != nil {
		return nil, fmt.Errorf("список нод: %w", err)
	}

	// Ноду выбираем замерами с самого устройства, а не берём первую из
	// списка: панель стоит за границей и видит ноду живой ровно тогда, когда
	// для телефона в Иркутске она уже мертва.
	dialer, measurements, err := client.SelectBest(context.Background(), sub.Nodes, key, client.Options{})

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
		return nil, err
	}

	t := &Tunnel{dialer: dialer, nodeName: dialer.Node().Name, running: true}

	bridgeOwnsFD = true
	bridge, err := tunbridge.Start(tunbridge.Config{
		FD:      tunFD,
		Dialer:  dialer,
		DNS:     dns,
		OnError: t.note,
	})
	if err != nil {
		_ = dialer.Close()
		return nil, fmt.Errorf("сетевой мост: %w", err)
	}
	t.bridge = bridge

	return t, nil
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
