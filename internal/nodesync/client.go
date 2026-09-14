// Package nodesync — сторона ноды в разговоре с панелью.
//
// Нода забирает свой список пользователей и отдаёт статистику. Ничего больше
// панель ей не сообщает и ничем не управляет: если связь пропала, нода
// продолжает работать по последнему полученному списку. Потерять панель на
// час — неприятно; отключить из-за этого всех клиентов — недопустимо.
package nodesync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/users"
)

const (
	requestTimeout = 20 * time.Second
	maxResponse    = 8 << 20 // 8 МиБ: список на десятки тысяч подписчиков влезает с запасом
)

// Client общается с панелью.
type Client struct {
	base  string
	token string
	http  *http.Client

	// cover — имена прикрытия, которые нода принимает (флаг -reality-sni).
	//
	// Нода сообщает их панели сама, вместе с расходом. Иначе один и тот же
	// список пришлось бы держать руками в двух местах: на ноде флагом и в
	// панели полем. Расходятся такие списки молча — клиент постучится именем,
	// которого нода уже не принимает, и соединение просто не соберётся.
	cover []string
}

// New создаёт клиента панели.
func New(baseURL, token string) *Client {
	return &Client{
		base:  strings.TrimRight(baseURL, "/"),
		token: token,
		http:  &http.Client{Timeout: requestTimeout},
	}
}

// WithCoverNames задаёт имена прикрытия, о которых нода сообщает панели.
func (c *Client) WithCoverNames(names []string) *Client {
	c.cover = names
	return c
}

// ErrNodeDisabled — панель говорит, что нода выключена.
//
// Отдельная ошибка, потому что реакция на неё принципиально другая: не
// падать, а ждать. Продавец выключил ноду сам и включит обратно тем же
// нажатием — нода обязана дожить до этого нажатия.
var ErrNodeDisabled = errors.New("нода выключена в панели")

// FetchUsers забирает список пользователей для этой ноды.
func (c *Client) FetchUsers(ctx context.Context) ([]users.User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/node/users", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("запрос списка: %w", err)
	}
	defer resp.Body.Close()

	// Отключённую ноду отличаем от всего остального отдельной ошибкой.
	//
	// Это не сбой, а решение продавца: он убрал ноду из подписок и вправе
	// вернуть её одним нажатием. Нода, которая от такого ответа умирает,
	// делает решение необратимым — и на живой машине это вылилось в семь с
	// лишним тысяч перезапусков подряд, каждый со своим запросом к панели.
	//
	// Но 403 бывает и не от панели: WAF или обратный прокси перед ней тоже
	// отвечают 403, и принять такой ответ за «выключили» — значит выбросить
	// всех клиентов на ровном месте при случайной блокировке. Поэтому 403
	// считаем выключением только если тело подтверждает, что это ответ самой
	// панели (она кладёт туда JSON вида {"error":"..."}); иначе это обычная
	// недоступность — работаем по последнему списку, пока блокировка не спадёт.
	if resp.StatusCode == http.StatusForbidden {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if panelSaysDisabled(raw) {
			return nil, ErrNodeDisabled
		}
		return nil, fmt.Errorf("панель ответила %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("панель ответила %s", resp.Status)
	}

	var body struct {
		Users []users.User `json:"users"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(&body); err != nil {
		return nil, fmt.Errorf("разбор списка: %w", err)
	}
	return body.Users, nil
}

// panelSaysDisabled отличает «ноду выключил продавец» от 403 чужого WAF.
//
// Панель на отключённой ноде кладёт в тело ответа JSON с непустым полем error.
// Пустое тело, HTML-страница обороны прокси или любой другой формат к решению
// продавца не относятся: поднимать тревогу и очищать реестр из-за случайного
// 403 хуже, чем доработать по последнему списку, пока блокировка не спадёт.
// Разбор намеренно снисходителен — на неожиданном теле просто возвращаем false,
// а не падаем.
func panelSaysDisabled(body []byte) bool {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return strings.TrimSpace(parsed.Error) != ""
}

// ReportUsage отправляет панели накопленный расход и тех, кто на связи.
func (c *Client) ReportUsage(ctx context.Context, report map[string]users.Usage, presence map[string]users.Presence) error {
	// Пустой отчёт тоже отправляем. Он говорит панели две вещи, которых из
	// молчания не вывести: имена прикрытия ноды и то, что на связи никого —
	// последний покупатель отключился, и его «на связи» пора обнулить.

	payload, err := json.Marshal(map[string]any{"usage": report, "presence": presence, "sni_extra": c.cover})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/node/usage", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("отправка статистики: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	// Тот же 403, что и у FetchUsers: выключенная нода получает его и здесь.
	// Отдаём ту же ErrNodeDisabled, чтобы цикл синхронизации распознал отказ
	// как ожидаемый и не считал сдачу статистики отдельным сбоем — иначе
	// выключенная нода сыпала бы в журнал 403 каждые 15 секунд. Чужой 403
	// (WAF) телом не подтверждается и остаётся обычной ошибкой.
	if resp.StatusCode == http.StatusForbidden {
		if panelSaysDisabled(raw) {
			return ErrNodeDisabled
		}
		return fmt.Errorf("панель ответила %s", resp.Status)
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("панель ответила %s", resp.Status)
	}
	return nil
}

// Events — обратная связь для журнала ноды.
type Events struct {
	OnUsers func(count int)
	OnError func(error)

	// OnDisabled и OnEnabled сообщают о смене состояния «нода выключена в
	// панели» и строго по одному разу на переход. Само выключение проверяется
	// на каждом тике, но журналировать его каждые 15 секунд незачем.
	OnDisabled func()
	OnEnabled  func()
}

func (e Events) users(count int) {
	if e.OnUsers != nil {
		e.OnUsers(count)
	}
}

func (e Events) fail(err error) {
	if e.OnError != nil {
		e.OnError(err)
	}
}

func (e Events) disabled() {
	if e.OnDisabled != nil {
		e.OnDisabled()
	}
}

func (e Events) enabled() {
	if e.OnEnabled != nil {
		e.OnEnabled()
	}
}

// gather снимает с реестра то, что уезжает панели: расход у тех, у кого он
// есть, и присутствие у тех, кто на связи. Остальные в отчёт не попадают —
// панель считает молчание нулём.
func gather(registry *users.Registry) (map[string]users.Usage, map[string]users.Presence) {
	report := make(map[string]users.Usage)
	presence := make(map[string]users.Presence)
	for _, s := range registry.Stats() {
		if s.Usage.Total() > 0 {
			report[s.Account] = s.Usage
		}
		if s.Conns > 0 || s.IPs > 0 {
			presence[s.Account] = users.Presence{Conns: s.Conns, IPs: s.IPs}
		}
	}
	return report, presence
}

// Run синхронизирует реестр с панелью, пока не отменят контекст.
//
// Порядок внутри одного цикла важен: сначала сдаём статистику, потом забираем
// список. Так панель успевает учесть свежий расход, прежде чем пересчитает
// остаток общей квоты и вернёт нам обновлённые лимиты.
func (c *Client) Run(ctx context.Context, registry *users.Registry, interval time.Duration, events Events) {
	// disabled помнит, выключена ли нода прямо сейчас. Нужен, чтобы
	// журналировать переходы «работала → выключили» и «выключена → включили
	// обратно» по одному разу, а не на каждом тике: иначе выключенная нода
	// каждые 15 секунд писала бы одну и ту же строку вместе с 403 от панели.
	disabled := false

	sync := func() {
		report, presence := gather(registry)
		usageErr := c.ReportUsage(ctx, report, presence)

		list, err := c.FetchUsers(ctx)
		switch {
		case errors.Is(err, ErrNodeDisabled):
			// Ноду выключили в середине жизни — ведём себя ровно как при старте
			// с выключенной нодой: очищаем реестр (никого не пускаем), но живём
			// и продолжаем спрашивать панель. Включат обратно — сами возобновим
			// обслуживание. 403 от /node/usage при этом ожидаем и сбоем не
			// считается, поэтому usageErr здесь сознательно не показываем.
			_ = registry.Replace(nil)
			if !disabled {
				disabled = true
				events.disabled()
			}
			return
		case err != nil:
			// Настоящая недоступность панели: работаем по последнему списку.
			// Заодно показываем и ошибку сдачи статистики — но не 403
			// выключенной ноды, который к обычной недоступности не относится.
			if usageErr != nil && !errors.Is(usageErr, ErrNodeDisabled) {
				events.fail(usageErr)
			}
			events.fail(err)
			return
		}

		// Список получен — нода включена. Если её только что включили обратно,
		// сообщаем об этом один раз.
		if disabled {
			disabled = false
			events.enabled()
		}
		if usageErr != nil && !errors.Is(usageErr, ErrNodeDisabled) {
			events.fail(usageErr)
		}
		if err := registry.Replace(list); err != nil {
			events.fail(err)
			return
		}
		events.users(len(list))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Прощальный отчёт: расход, накопленный после последнего тика,
			// иначе он потеряется при штатной остановке ноды.
			// На связи в этот момент уже никого: нода уходит, и панели лучше
			// узнать об этом от неё, чем догадываться по молчанию.
			report, _ := gather(registry)
			farewell, cancel := context.WithTimeout(context.Background(), requestTimeout)
			_ = c.ReportUsage(farewell, report, nil)
			cancel()
			return
		case <-ticker.C:
			sync()
		}
	}
}
