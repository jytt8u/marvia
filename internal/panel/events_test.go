package panel_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

// Журнал отвечает на «что делали с панелью». Проверяем три обещания: он
// записывает действия, называет того, кто их сделал, и не становится слежкой.

func events(t *testing.T, h *harness) []panel.Event {
	t.Helper()
	var out struct {
		Events []panel.Event `json:"events"`
	}
	if code := h.do(http.MethodGet, "/api/v1/events", adminToken, nil, &out); code != http.StatusOK {
		t.Fatalf("чтение журнала: код %d", code)
	}
	return out.Events
}

// TestJournalRemembersWhatWasDone — действия попадают в журнал своими словами.
func TestJournalRemembersWhatWasDone(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0, panel.CredVP1)

	// Продлеваем и отключаем.
	if code := h.do(http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.User.ID),
		adminToken, map[string]any{"extend_by": "30d", "enabled": false}, nil); code != http.StatusOK {
		t.Fatalf("правка: код %d", code)
	}

	list := events(t, h)
	if len(list) < 2 {
		t.Fatalf("записей %d, ожидалось хотя бы две: %+v", len(list), list)
	}

	// Новые сверху.
	last := list[0]
	if last.Action != panel.EventUserUpdate {
		t.Fatalf("верхняя запись не о правке: %+v", last)
	}
	if !strings.Contains(last.Detail, "продлён") || !strings.Contains(last.Detail, "отключён") {
		t.Fatalf("правка описана непонятно: %q", last.Detail)
	}
	if last.UserID != created.User.ID {
		t.Fatalf("запись не привязана к покупателю: %+v", last)
	}

	var sawCreate bool
	for _, e := range list {
		if e.Action == panel.EventUserCreate {
			sawCreate = true
		}
	}
	if !sawCreate {
		t.Fatal("заведение покупателя в журнал не попало")
	}
}

// TestJournalNamesTheBotThatActed — видно, чей бот наделал дел.
//
// Иначе журнал отвечает «кто-то», и спор о том, бот это сделал или человек,
// не разрешается ничем.
func TestJournalNamesTheBotThatActed(t *testing.T) {
	h := newHarness(t)

	var key struct {
		Secret string `json:"secret"`
	}
	if code := h.do(http.MethodPost, "/api/v1/keys", adminToken,
		map[string]any{"name": "бот продаж", "scopes": []string{"users"}}, &key); code != http.StatusOK {
		t.Fatalf("выпуск ключа: код %d", code)
	}

	// Заводим покупателя ключом бота, а не админским токеном.
	if code := h.do(http.MethodPost, "/api/v1/users", key.Secret,
		map[string]any{"label": "через бота"}, nil); code != http.StatusOK {
		t.Fatalf("создание ботом: код %d", code)
	}

	list := events(t, h)
	if len(list) == 0 {
		t.Fatal("журнал пуст")
	}
	if list[0].Actor != "бот продаж" {
		t.Fatalf("действие записано на %q, ожидался бот: %+v", list[0].Actor, list[0])
	}

	// А выпуск ключа сделал админ.
	var sawAdmin bool
	for _, e := range list {
		if e.Action == panel.EventKeyCreate && e.Actor == panel.ActorAdmin {
			sawAdmin = true
		}
	}
	if !sawAdmin {
		t.Fatalf("выпуск ключа не записан на админа: %+v", list)
	}
}

// TestJournalKeepsNoSecrets — в журнале нет ни ключей, ни токенов.
//
// Журнал смотрят с экрана и пересылают в поддержку. Секрет, попавший туда
// один раз, дальше живёт своей жизнью.
func TestJournalKeepsNoSecrets(t *testing.T) {
	h := newHarness(t)
	h.createNode("msk")
	created := h.createUser(0, panel.CredVP1)

	var key struct {
		Secret string `json:"secret"`
	}
	if code := h.do(http.MethodPost, "/api/v1/keys", adminToken,
		map[string]any{"name": "ключ", "scopes": []string{"users"}}, &key); code != http.StatusOK {
		t.Fatalf("выпуск ключа: код %d", code)
	}

	// Меняем ключ доступа и адрес подписки — обе операции про секреты.
	if len(created.User.Credentials) == 0 {
		t.Fatal("у покупателя нет наборов")
	}
	if code := h.do(http.MethodPost, fmt.Sprintf("/api/v1/credentials/%d/rotate", created.User.Credentials[0].ID),
		adminToken, nil, nil); code != http.StatusOK {
		t.Fatalf("смена ключа: код %d", code)
	}
	if code := h.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/sub-token", created.User.ID),
		adminToken, nil, nil); code != http.StatusOK {
		t.Fatalf("смена адреса подписки: код %d", code)
	}

	secrets := []string{key.Secret, created.User.SubToken, adminToken}
	for _, e := range events(t, h) {
		for _, secret := range secrets {
			if secret != "" && strings.Contains(e.Detail+e.Actor, secret) {
				t.Fatalf("секрет уехал в журнал: %+v", e)
			}
		}
	}
}

// TestJournalForgetsDeletedPeople — удалили человека, исчезли и записи о нём.
//
// Иначе обещание «удалили значит удалили», которое панель даёт в остальных
// местах, оказалось бы неправдой именно там, где записей больше всего.
func TestJournalForgetsDeletedPeople(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0, panel.CredVP1)

	if code := h.do(http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.User.ID),
		adminToken, map[string]any{"enabled": false}, nil); code != http.StatusOK {
		t.Fatalf("правка: код %d", code)
	}

	before := 0
	for _, e := range events(t, h) {
		if e.UserID == created.User.ID {
			before++
		}
	}
	if before == 0 {
		t.Fatal("записей о покупателе не появилось вовсе")
	}

	if code := h.do(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", created.User.ID),
		adminToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("удаление: код %d", code)
	}

	for _, e := range events(t, h) {
		if e.UserID == created.User.ID {
			t.Fatalf("запись об удалённом покупателе осталась: %+v", e)
		}
	}
}

// Журнал закрыт админским токеном: боту с правом read в летописи панели
// делать нечего.
func TestJournalIsAdminOnly(t *testing.T) {
	h := newHarness(t)

	var key struct {
		Secret string `json:"secret"`
	}
	if code := h.do(http.MethodPost, "/api/v1/keys", adminToken,
		map[string]any{"name": "смотрящий", "scopes": []string{"read"}}, &key); code != http.StatusOK {
		t.Fatalf("выпуск ключа: код %d", code)
	}

	if code := h.do(http.MethodGet, "/api/v1/events", key.Secret, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("ключ с правом read прочитал журнал: код %d", code)
	}
}
