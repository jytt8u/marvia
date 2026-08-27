package panel_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// userView — то, что нужно от ответа: срок подписчика.
type userView struct {
	User struct {
		ID        int64      `json:"id"`
		ExpiresAt *time.Time `json:"expires_at"`
	} `json:"user"`
}

// withKey выполняет запрос с ключом идемпотентности.
//
// Отдельно от harness.do: тому заголовки не нужны, а здесь весь смысл в них.
func (h *harness) withKey(method, path, key string, body any, out any) int {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("сериализация запроса: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("запрос: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			h.t.Fatalf("%s %s: разбор ответа: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// newSubscriber заводит подписчика на месяц и отдаёт его id и срок.
func (h *harness) newSubscriber(label string) (int64, time.Time) {
	h.t.Helper()

	var out userView
	code := h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{
		"label":      label,
		"expires_at": "30d",
	}, &out)
	if code != http.StatusOK {
		h.t.Fatalf("подписчик не завёлся: код %d", code)
	}
	if out.User.ExpiresAt == nil {
		h.t.Fatal("у нового подписчика нет срока")
	}
	return out.User.ID, *out.User.ExpiresAt
}

// TestExtendIsIdempotent — повтор уведомления об оплате не удваивает срок.
//
// Главный сценарий, ради которого ключ и появился. Платёжные системы шлют
// уведомление заново, пока не получат ответ, и бот честно позовёт панель
// дважды за одну оплату. Без ключа покупатель получал два месяца за один
// оплаченный, а продавец узнавал об этом через месяц и не понимал почему.
func TestExtendIsIdempotent(t *testing.T) {
	h := newHarness(t)

	id, before := h.newSubscriber("телефон")
	path := "/api/v1/users/" + itoa(id)
	body := map[string]any{"extend_by": "30d"}
	const payment = "payment-2026-08-25-0001"

	var first userView
	if code := h.withKey(http.MethodPatch, path, payment, body, &first); code != http.StatusOK {
		t.Fatalf("продление не прошло: код %d", code)
	}
	after := *first.User.ExpiresAt

	if added := after.Sub(before); added < 29*24*time.Hour || added > 31*24*time.Hour {
		t.Fatalf("первое продление добавило %v, ожидался примерно месяц", added)
	}

	// Тот же ключ, тот же запрос: платёжная система повторила уведомление.
	var second userView
	if code := h.withKey(http.MethodPatch, path, payment, body, &second); code != http.StatusOK {
		t.Fatalf("повтор ответил кодом %d", code)
	}
	if got := *second.User.ExpiresAt; !got.Equal(after) {
		t.Fatalf("повтор сдвинул срок с %s на %s — продление не идемпотентно", after, got)
	}

	// Следующая оплата — другой ключ, и она обязана пройти по-настоящему.
	var third userView
	h.withKey(http.MethodPatch, path, "payment-2026-09-25-0002", body, &third)
	if got := *third.User.ExpiresAt; !got.After(after) {
		t.Fatalf("следующая оплата не продлила: было %s, стало %s", after, got)
	}
}

// TestExtendWithoutKeyStaysAsBefore — без ключа поведение прежнее.
//
// Ключ необязателен: боты, написанные до его появления, обязаны продолжать
// работать ровно как раньше — пусть и без защиты от повтора.
func TestExtendWithoutKeyStaysAsBefore(t *testing.T) {
	h := newHarness(t)

	id, _ := h.newSubscriber("без ключа")
	path := "/api/v1/users/" + itoa(id)
	body := map[string]any{"extend_by": "30d"}

	var first, second userView
	h.withKey(http.MethodPatch, path, "", body, &first)
	h.withKey(http.MethodPatch, path, "", body, &second)

	if !second.User.ExpiresAt.After(*first.User.ExpiresAt) {
		t.Fatal("без ключа второе продление обязано сдвинуть срок")
	}
}

// TestIdempotencyKeyIsBoundToOperation — один ключ на две разные операции.
//
// Это не повтор, а ошибка бота: один идентификатор платежа отправлен на
// продление двух разных подписчиков. Выполнить молча — сделать не то, что
// просили; вернуть чужой ответ — тем более. Поэтому отказ, и внятный.
func TestIdempotencyKeyIsBoundToOperation(t *testing.T) {
	h := newHarness(t)

	one, _ := h.newSubscriber("первый")
	two, _ := h.newSubscriber("второй")
	body := map[string]any{"extend_by": "30d"}
	const key = "payment-одинаковый"

	h.withKey(http.MethodPatch, "/api/v1/users/"+itoa(one), key, body, nil)

	code := h.withKey(http.MethodPatch, "/api/v1/users/"+itoa(two), key, body, nil)
	if code != http.StatusConflict {
		t.Fatalf("чужой ключ принят с кодом %d, ожидался 409", code)
	}
}
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// TestFirstSaleIsIdempotent — повтор оплаты не выдаёт второй доступ.
//
// Продление ключ защищал давно, а первую продажу — нет: панель про платежи не
// знает, а ErrAlreadyExists ловит только повтор по external_id. Продавец,
// который его не заполняет, не был защищён ничем, и повтор уведомления от
// платёжной системы заводил второго подписчика за те же деньги.
func TestFirstSaleIsIdempotent(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{"label": "покупатель", "expires_at": "30d"}
	const payment = "charge-777"

	var first userView
	if code := h.withKey(http.MethodPost, "/api/v1/users", payment, body, &first); code != http.StatusOK {
		t.Fatalf("продажа не прошла: код %d", code)
	}

	var second userView
	if code := h.withKey(http.MethodPost, "/api/v1/users", payment, body, &second); code != http.StatusOK {
		t.Fatalf("повтор ответил кодом %d", code)
	}
	if second.User.ID != first.User.ID {
		t.Fatalf("повтор завёл второго подписчика: %d и %d", first.User.ID, second.User.ID)
	}

	var list struct {
		Users []struct {
			ID int64 `json:"id"`
		} `json:"users"`
	}
	h.do(http.MethodGet, "/api/v1/users", adminToken, nil, &list)
	if len(list.Users) != 1 {
		t.Fatalf("в панели %d подписчиков вместо одного", len(list.Users))
	}
}

// TestReplayedSaleHasNoSecrets — повтор не отдаёт ключ доступа заново.
//
// Приватную часть vp1 панель не хранит нигде — утечка её базы не даёт доступа
// ни к одному покупателю нашего протокола. Сложить секрет в таблицу повторов
// ради удобства бота значило бы разменять это свойство на сутки хранения.
func TestReplayedSaleHasNoSecrets(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{"label": "покупатель"}
	const payment = "charge-778"

	var first struct {
		Created bool `json:"created"`
		Links   struct {
			Account string `json:"account"`
		} `json:"links"`
	}
	h.withKey(http.MethodPost, "/api/v1/users", payment, body, &first)
	if !first.Created || first.Links.Account == "" {
		t.Fatalf("первая продажа не выдала доступ: %+v", first)
	}

	var second struct {
		Created bool `json:"created"`
		Links   struct {
			Account string `json:"account"`
		} `json:"links"`
	}
	h.withKey(http.MethodPost, "/api/v1/users", payment, body, &second)

	if second.Created {
		t.Error("повтор объявил, что завёл покупателя заново")
	}
	if second.Links.Account != "" {
		t.Errorf("повтор отдал ключ доступа из хранилища повторов: %q", second.Links.Account)
	}
}

// TestSaleKeyIsBoundToBuyer — один ключ на двух покупателей это ошибка бота.
//
// Молча вернуть ответ первого — значит оставить второго без доступа, за
// который он заплатил, и продавец узнает об этом от него же.
func TestSaleKeyIsBoundToBuyer(t *testing.T) {
	h := newHarness(t)

	const payment = "charge-779"
	h.withKey(http.MethodPost, "/api/v1/users", payment,
		map[string]any{"external_id": "tg:1", "label": "первый"}, nil)

	code := h.withKey(http.MethodPost, "/api/v1/users", payment,
		map[string]any{"external_id": "tg:2", "label": "второй"}, nil)
	if code != http.StatusConflict {
		t.Fatalf("тот же ключ на другого покупателя прошёл с кодом %d", code)
	}
}
