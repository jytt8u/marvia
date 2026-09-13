package panel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Оповещения продавцу.
//
// Поломку замечают последней: нода молчит третий час, подходит срок, копия
// базы не снялась неделю назад. Без оповещений всё это обнаруживается тогда,
// когда уже мешает, — а часто и позже.
//
// Шлём в телеграм-бот того, кто держит панель: бот заводится за минуту, и
// поднимать ради оповещений отдельную службу незачем.
//
// Что в сообщениях НЕ появляется: имена, метки и идентификаторы людей.
// «У троих кончается срок», а не «у Артёма, Нины и Дмитрия». Переписка с ботом
// живёт в телеграме, и превращать её в список людей нельзя: она переживёт и
// панель, и того, кто её держит. Числа отвечают на вопрос «надо ли идти
// смотреть» ничем не хуже, а кто именно — видно в панели.
//
// Чем платим: панель начинает ходить наружу, на api.telegram.org. Её адрес
// становится известен телеграму и виден тому, кто смотрит на её трафик. Выбор
// осознанный — поэтому оповещения выключены, пока их не включат.

const (
	// silentFor — сколько нода должна молчать, прежде чем это станет поводом.
	//
	// Панель считает ноду живой, если та отмечалась последние десять минут.
	// Для оповещения порог выше: перезапуск ноды, обновление или минута
	// плохой связи не должны будить продавца среди ночи. Час означает, что
	// нода пропустила десяток попыток подряд, — это уже не рябь.
	silentFor = time.Hour

	// expiringWithin — за сколько предупреждать об истечении подписок.
	expiringWithin = 3 * 24 * time.Hour

	// alertEvery — как часто панель смотрит, не случилось ли чего.
	alertEvery = 5 * time.Minute

	// telegramTimeout — сколько ждём телеграм. Оповещение не стоит того,
	// чтобы держать горутину дольше.
	telegramTimeout = 15 * time.Second
)

// AlertSettings — куда и слать ли оповещения.
type AlertSettings struct {
	// Enabled — выключенные оповещения не шлются и наружу панель не ходит.
	Enabled bool `json:"enabled"`

	// BotToken — токен телеграм-бота продавца. Наружу не отдаётся: в ответах
	// API вместо него идёт признак «задан».
	BotToken string `json:"bot_token,omitempty"`

	// ChatID — куда слать: личка продавца или его служебный чат.
	ChatID string `json:"chat_id,omitempty"`
}

// Ready сообщает, есть ли чем и куда слать.
func (s AlertSettings) Ready() bool {
	return s.Enabled && s.BotToken != "" && s.ChatID != ""
}

// AlertSettings читает настройки оповещений.
func (s *Store) AlertSettings(ctx context.Context) (AlertSettings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'alerts'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return AlertSettings{}, nil
	}
	if err != nil {
		return AlertSettings{}, fmt.Errorf("чтение настроек оповещений: %w", err)
	}

	var out AlertSettings
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		// Испорченная запись — не повод не подниматься: оповещения просто
		// окажутся выключены, и продавец задаст их заново.
		return AlertSettings{}, nil
	}
	return out, nil
}

// SetAlertSettings сохраняет настройки оповещений.
func (s *Store) SetAlertSettings(ctx context.Context, v AlertSettings) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES ('alerts', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(raw))
	if err != nil {
		return fmt.Errorf("сохранение настроек оповещений: %w", err)
	}
	return nil
}

// Alerts следит за бедами и рассказывает о них продавцу.
type Alerts struct {
	store *Store

	// send отправляет одно сообщение. Подменяется в тестах: настоящий
	// телеграм в тестах не нужен, а проверять надо, что и когда мы говорим.
	send func(ctx context.Context, cfg AlertSettings, text string) error

	// told помнит, о чём уже сказано, чтобы не повторяться каждые пять минут.
	//
	// В памяти, а не в базе: перезапуск панели редок, и одно повторное
	// сообщение после него дешевле лишней таблицы. Ключ — сама беда, а не
	// время: «нода 7 молчит» сказано один раз и повторится, только когда она
	// оживёт и замолчит снова.
	mu   sync.Mutex
	told map[string]bool
}

// NewAlerts собирает наблюдателя.
func NewAlerts(store *Store) *Alerts {
	return &Alerts{store: store, send: sendTelegram, told: make(map[string]bool)}
}

// Watch смотрит за бедами, пока не отменят контекст.
func (a *Alerts) Watch(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = alertEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Check(ctx)
		}
	}
}

// Check проходит по всем поводам разом.
func (a *Alerts) Check(ctx context.Context) {
	cfg, err := a.store.AlertSettings(ctx)
	if err != nil || !cfg.Ready() {
		return
	}
	a.checkNodes(ctx, cfg)
	a.checkExpiring(ctx, cfg)
}

// checkNodes сообщает о замолчавших нодах и об их возвращении.
func (a *Alerts) checkNodes(ctx context.Context, cfg AlertSettings) {
	nodes, err := a.store.ListNodes(ctx)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	for _, n := range nodes {
		// Выключенную ноду молчащей не считаем: её выключил сам продавец,
		// и напоминать ему об этом каждый час — верный способ приучить его
		// не читать оповещения вовсе.
		if !n.Enabled {
			continue
		}

		key := fmt.Sprintf("node-silent-%d", n.ID)
		silent := n.LastSeen == nil || now.Sub(n.LastSeen.UTC()) > silentFor

		switch {
		case silent && !a.already(key):
			a.mark(key, true)
			a.say(ctx, cfg, "Нода «"+n.Name+"» молчит больше часа. Покупатели на ней сейчас не подключатся.")
		case !silent && a.already(key):
			a.mark(key, false)
			a.say(ctx, cfg, "Нода «"+n.Name+"» снова на связи.")
		}
	}
}

// checkExpiring предупреждает, что у людей кончается срок.
//
// Числом, а не списком: имена покупателей в переписку с ботом не уходят.
// Продавцу этого хватает — он идёт в панель и видит, у кого именно.
func (a *Alerts) checkExpiring(ctx context.Context, cfg AlertSettings) {
	list, err := a.store.ListUsers(ctx)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	soon := 0
	for _, u := range list {
		if !u.Enabled || u.ExpiresAt == nil {
			continue
		}
		left := u.ExpiresAt.Sub(now)
		if left > 0 && left <= expiringWithin {
			soon++
		}
	}

	// Ключ с числом: пока их столько же, второй раз не говорим, а когда
	// прибавится — скажем снова.
	key := fmt.Sprintf("expiring-%d", soon)
	if soon == 0 {
		a.forgetPrefix("expiring-")
		return
	}
	if a.already(key) {
		return
	}
	a.forgetPrefix("expiring-")
	a.mark(key, true)
	a.say(ctx, cfg, fmt.Sprintf("У %d %s кончается срок в ближайшие три дня.", soon, plural(soon)))
}

// BackupFailed сообщает, что копия базы не снялась.
//
// Зовётся из того же места, где панель снимает копии: отдельный опрос тут не
// нужен, а беда важная: о несделанных копиях узнают обычно в тот день, когда
// они понадобились.
func (a *Alerts) BackupFailed(ctx context.Context, cause error) {
	cfg, err := a.store.AlertSettings(ctx)
	if err != nil || !cfg.Ready() {
		return
	}

	const key = "backup-failed"
	if cause == nil {
		if a.already(key) {
			a.mark(key, false)
			a.say(ctx, cfg, "Копия базы снова снимается.")
		}
		return
	}
	if a.already(key) {
		return
	}
	a.mark(key, true)
	a.say(ctx, cfg, "Копия базы не снялась: "+cause.Error())
}

// say отправляет сообщение, не роняя панель из-за недоступного телеграма.
func (a *Alerts) say(ctx context.Context, cfg AlertSettings, text string) {
	ctx, cancel := context.WithTimeout(ctx, telegramTimeout)
	defer cancel()
	_ = a.send(ctx, cfg, text)
}

func (a *Alerts) already(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.told[key]
}

func (a *Alerts) mark(key string, v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v {
		a.told[key] = true
		return
	}
	delete(a.told, key)
}

func (a *Alerts) forgetPrefix(prefix string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k := range a.told {
		if strings.HasPrefix(k, prefix) {
			delete(a.told, k)
		}
	}
}

// plural склоняет «человек» под число.
func plural(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "человека"
	}
	return "человек"
}

// sendTelegram отправляет сообщение ботом продавца.
//
// Без библиотеки: один POST с формой. Тянуть ради этого зависимость, которая
// умеет опрашивать обновления и разбирать сотню видов сообщений, незачем —
// панель ничего не слушает, она только говорит.
func sendTelegram(ctx context.Context, cfg AlertSettings, text string) error {
	form := url.Values{}
	form.Set("chat_id", cfg.ChatID)
	form.Set("text", text)
	// Ссылки в оповещениях не нужны, а разметка сломалась бы на первом же
	// имени ноды со скобкой.
	form.Set("disable_web_page_preview", "true")

	endpoint := "https://api.telegram.org/bot" + cfg.BotToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Токен лежит в адресе, а ошибка http его печатает: в журнал он
		// попасть не должен.
		return errors.New("телеграм недоступен")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("телеграм ответил %s", resp.Status)
	}
	return nil
}

// SendWith подменяет отправку. Только для тестов: настоящий телеграм в них не
// нужен, а проверять надо, что и когда панель говорит.
func (a *Alerts) SendWith(send func(context.Context, AlertSettings, string) error) {
	a.send = send
}
