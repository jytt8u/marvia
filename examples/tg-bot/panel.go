package main

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
)

// ErrNoUser — такого покупателя панель не знает.
var ErrNoUser = errors.New("покупателя нет в панели")

// Panel — клиент управляющей панели.
//
// Тонкий по замыслу: панель уже отвечает готовыми ссылками, считает сроки и
// защищается от повторных уведомлений об оплате. Всё, что бот повторил бы у
// себя, однажды разошлось бы с тем, что думает панель, — и разбираться в этом
// пришлось бы продавцу.
type Panel struct {
	base string
	key  string
	http *http.Client
}

// NewPanel собирает клиент.
func NewPanel(base, key string) *Panel {
	return &Panel{
		base: strings.TrimRight(base, "/"),
		key:  key,
		// Панель отвечает быстро; долгое ожидание здесь означало бы, что
		// покупатель смотрит на замерший бот после списания денег.
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// User — подписчик глазами бота.
type User struct {
	ID           int64      `json:"id"`
	Label        string     `json:"label"`
	Enabled      bool       `json:"enabled"`
	ExpiresAt    *time.Time `json:"expires_at"`
	TrafficLimit int64      `json:"traffic_limit"`
	Used         int64      `json:"used"`
	ExternalID   string     `json:"external_id"`
}

// Links — то, что бот пересылает покупателю.
type Links struct {
	Account      string   `json:"account"`
	Subscription string   `json:"subscription"`
	Stock        []string `json:"stock"`
}

// Sale — итог продажи.
type Sale struct {
	User    User  `json:"user"`
	Created bool  `json:"created"`
	Links   Links `json:"links"`
}

// Sell заводит покупателя и выдаёт ему доступ.
//
// Повтор с тем же externalID ничего не создаёт: панель возвращает уже
// заведённого подписчика и created=false. Поэтому бот, упавший между списанием
// денег и ответом покупателю, при следующем запуске не выдаст второй доступ.
func (p *Panel) Sell(ctx context.Context, externalID, label string, t Tariff, key string) (Sale, error) {
	body := map[string]any{
		"external_id":   externalID,
		"label":         label,
		"expires_at":    fmt.Sprintf("%dd", t.Days),
		"traffic_limit": t.trafficBytes(),
		"max_ips":       t.Devices,
		// Все три набора сразу: наш клиент понимает vp1, а на компьютере у
		// покупателя, скорее всего, уже стоит что-то, что понимает vless.
		"kinds": []string{"vp1", "vless", "trojan"},
	}

	// key — номер платежа в Idempotency-Key. По нему панель отличает повтор
	// уведомления от второй покупки, и своей памяти о платежах боту держать не
	// нужно. Повтор вернёт того же покупателя с created=false и без секретов:
	// доступ выдан один раз, и потерянную ссылку выдают новым набором.
	var out Sale
	if err := p.do(ctx, http.MethodPost, "/api/v1/users", key, body, &out); err != nil {
		return Sale{}, err
	}
	return out, nil
}

// Extend продлевает подписку на срок тарифа.
//
// key — ключ идемпотентности, обычно номер платежа от телеграма. Панель по
// нему отличает повтор от второй покупки: телеграм повторяет уведомление, если
// бот не ответил, и без этого покупатель получал бы два месяца за одни деньги,
// а продавец узнавал бы об этом из статистики.
func (p *Panel) Extend(ctx context.Context, id int64, t Tariff, key string) (User, error) {
	body := map[string]any{"extend_by": fmt.Sprintf("%dd", t.Days)}

	var out struct {
		User User `json:"user"`
	}
	if err := p.do(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", id), key, body, &out); err != nil {
		return User{}, err
	}
	return out.User, nil
}

// ByExternalID находит покупателя по его telegram id.
func (p *Panel) ByExternalID(ctx context.Context, externalID string) (User, error) {
	var out struct {
		Users []User `json:"users"`
	}
	path := "/api/v1/users?external_id=" + urlValue(externalID)
	if err := p.do(ctx, http.MethodGet, path, "", nil, &out); err != nil {
		return User{}, err
	}
	if len(out.Users) == 0 {
		return User{}, ErrNoUser
	}
	return out.Users[0], nil
}

// List отдаёт всех подписчиков: по нему бот ищет тех, у кого кончается срок.
func (p *Panel) List(ctx context.Context) ([]User, error) {
	var out struct {
		Users []User `json:"users"`
	}
	if err := p.do(ctx, http.MethodGet, "/api/v1/users", "", nil, &out); err != nil {
		return nil, err
	}
	return out.Users, nil
}

// Links отдаёт ссылки уже заведённого покупателя.
//
// Секретной части нашего протокола здесь нет и не будет: панель её не хранит.
// Поэтому «пришлите ссылку ещё раз» решается выдачей нового набора доступа, а
// не повторной выдачей старого.
func (p *Panel) Links(ctx context.Context, id int64) (Links, error) {
	var out Links
	if err := p.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/%d/links", id), "", nil, &out); err != nil {
		return Links{}, err
	}
	return out, nil
}

// NewDevice выдаёт покупателю ещё один набор доступа и возвращает готовую
// ссылку для нашего клиента.
//
// Так решается «потерял ссылку»: старую взять неоткуда — приватной части
// панель не хранит, — а новая выдаётся за один запрос. Квота и срок у всех
// наборов одного покупателя общие, поэтому лишнего трафика это не даёт.
func (p *Panel) NewDevice(ctx context.Context, id int64) (string, error) {
	var out struct {
		Links Links `json:"links"`
	}
	body := map[string]any{"kind": "vp1"}
	if err := p.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/%d/credentials", id), "", body, &out); err != nil {
		return "", err
	}
	if out.Links.Account == "" {
		return "", errors.New("панель не вернула ссылку доступа")
	}
	return out.Links.Account, nil
}

func (p *Panel) do(ctx context.Context, method, path, idempotency string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotency != "" {
		req.Header.Set("Idempotency-Key", idempotency)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("панель не отвечает: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusNotFound {
		return ErrNoUser
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("панель ответила %d: %s", resp.StatusCode, e.Error)
	}

	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("ответ панели не разобрался: %w", err)
	}
	return nil
}

// urlValue кодирует значение для строки запроса.
func urlValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~', r == ':':
			b.WriteRune(r)
		default:
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}
