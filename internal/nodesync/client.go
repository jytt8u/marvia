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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/veilproject/veil/internal/users"
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
}

// New создаёт клиента панели.
func New(baseURL, token string) *Client {
	return &Client{
		base:  strings.TrimRight(baseURL, "/"),
		token: token,
		http:  &http.Client{Timeout: requestTimeout},
	}
}

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

// ReportUsage отправляет панели накопленный расход.
func (c *Client) ReportUsage(ctx context.Context, report map[string]users.Usage) error {
	if len(report) == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{"usage": report})
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
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("панель ответила %s", resp.Status)
	}
	return nil
}

// Events — обратная связь для журнала ноды.
type Events struct {
	OnUsers func(count int)
	OnError func(error)
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

// Run синхронизирует реестр с панелью, пока не отменят контекст.
//
// Порядок внутри одного цикла важен: сначала сдаём статистику, потом забираем
// список. Так панель успевает учесть свежий расход, прежде чем пересчитает
// остаток общей квоты и вернёт нам обновлённые лимиты.
func (c *Client) Run(ctx context.Context, registry *users.Registry, interval time.Duration, events Events) {
	sync := func() {
		report := make(map[string]users.Usage)
		for _, s := range registry.Stats() {
			if s.Usage.Total() > 0 {
				report[s.Account] = s.Usage
			}
		}
		if err := c.ReportUsage(ctx, report); err != nil {
			events.fail(err)
		}

		list, err := c.FetchUsers(ctx)
		if err != nil {
			events.fail(err)
			return
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
			report := make(map[string]users.Usage)
			for _, s := range registry.Stats() {
				if s.Usage.Total() > 0 {
					report[s.Account] = s.Usage
				}
			}
			farewell, cancel := context.WithTimeout(context.Background(), requestTimeout)
			_ = c.ReportUsage(farewell, report)
			cancel()
			return
		case <-ticker.C:
			sync()
		}
	}
}
