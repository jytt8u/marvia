package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Клиент Bot API телеграма.
//
// Своими руками, без библиотеки: боту нужно семь методов из двух сотен, а
// чужая зависимость в проекте про обход цензуры — это ещё один человек,
// которому мы доверяем сборку кода, идущего на сервер продавца.

const telegramAPI = "https://api.telegram.org"

// longPoll — сколько телеграм держит запрос, пока нет новостей. Тридцать
// секунд вместо частых пустых опросов: и телеграму легче, и ответ приходит
// сразу, как только покупатель что-то нажал.
const longPoll = 30

type telegram struct {
	base  string
	token string
	http  *http.Client
}

func newTelegram(token string) *telegram {
	return &telegram{
		base:  telegramAPI,
		token: token,
		// Дольше, чем держится опрос: иначе каждый пустой опрос выглядел бы
		// обрывом связи.
		http: &http.Client{Timeout: (longPoll + 20) * time.Second},
	}
}

// Update — одно событие: сообщение, нажатие кнопки или платёж.
type Update struct {
	UpdateID         int64             `json:"update_id"`
	Message          *Message          `json:"message"`
	CallbackQuery    *CallbackQuery    `json:"callback_query"`
	PreCheckoutQuery *PreCheckoutQuery `json:"pre_checkout_query"`
}

type Message struct {
	MessageID         int64              `json:"message_id"`
	From              *TGUser            `json:"from"`
	Chat              Chat               `json:"chat"`
	Text              string             `json:"text"`
	SuccessfulPayment *SuccessfulPayment `json:"successful_payment"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type TGUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    TGUser   `json:"from"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

type PreCheckoutQuery struct {
	ID       string `json:"id"`
	From     TGUser `json:"from"`
	Currency string `json:"currency"`
	Total    int64  `json:"total_amount"`
	Payload  string `json:"invoice_payload"`
}

type SuccessfulPayment struct {
	Currency string `json:"currency"`
	Total    int64  `json:"total_amount"`
	Payload  string `json:"invoice_payload"`

	// ChargeID — номер платежа. Он же ключ идемпотентности при продлении:
	// телеграм повторяет уведомление, если бот не ответил.
	ChargeID string `json:"telegram_payment_charge_id"`
}

// Button — кнопка под сообщением.
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data,omitempty"`
	URL  string `json:"url,omitempty"`
	Pay  bool   `json:"pay,omitempty"`
}

func (t *telegram) updates(ctx context.Context, offset int64) ([]Update, error) {
	req := map[string]any{
		"offset":  offset,
		"timeout": longPoll,
		// Остальные виды событий боту не нужны, а лишние — это лишний разбор
		// чужого ввода.
		"allowed_updates": []string{"message", "callback_query", "pre_checkout_query"},
	}

	var out []Update
	if err := t.call(ctx, "getUpdates", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// send отправляет сообщение с необязательными кнопками.
func (t *telegram) send(ctx context.Context, chat int64, text string, rows ...[]Button) error {
	req := map[string]any{
		"chat_id":    chat,
		"text":       text,
		"parse_mode": "HTML",
		// Ссылки в наших сообщениях не разворачиваем: превью адреса панели
		// в чате — лишняя подсказка о том, чем человек пользуется.
		"link_preview_options": map[string]any{"is_disabled": true},
	}
	if len(rows) > 0 {
		req["reply_markup"] = map[string]any{"inline_keyboard": rows}
	}
	return t.call(ctx, "sendMessage", req, nil)
}

// answer гасит «часики» на нажатой кнопке.
func (t *telegram) answer(ctx context.Context, id, text string) error {
	req := map[string]any{"callback_query_id": id}
	if text != "" {
		req["text"] = text
		req["show_alert"] = true
	}
	return t.call(ctx, "answerCallbackQuery", req, nil)
}

// invoice выставляет счёт в звёздах телеграма.
func (t *telegram) invoice(ctx context.Context, chat int64, title, desc, payload string, price int64) error {
	req := map[string]any{
		"chat_id":     chat,
		"title":       title,
		"description": desc,
		"payload":     payload,
		// Звёзды — единственная валюта, для которой не нужен договор с
		// платёжной системой: provider_token при ней пустой.
		"currency": "XTR",
		"prices":   []map[string]any{{"label": title, "amount": price}},
	}
	return t.call(ctx, "sendInvoice", req, nil)
}

// approve подтверждает телеграму, что счёт ещё в силе.
//
// Ответить надо за десять секунд, иначе телеграм отменяет платёж сам.
func (t *telegram) approve(ctx context.Context, id string, ok bool, reason string) error {
	req := map[string]any{"pre_checkout_query_id": id, "ok": ok}
	if !ok {
		req["error_message"] = reason
	}
	return t.call(ctx, "answerPreCheckoutQuery", req, nil)
}

func (t *telegram) call(ctx context.Context, method string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/bot%s/%s", t.base, t.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("телеграм не отвечает: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}

	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("телеграм ответил непонятным: %w", err)
	}
	if !envelope.OK {
		// Токен в текст ошибки не попадает: он в адресе, а его мы не печатаем.
		return fmt.Errorf("телеграм отказал в %s: %s", method, envelope.Description)
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}
