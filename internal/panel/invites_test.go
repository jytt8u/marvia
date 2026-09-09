package panel_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/vp1"
)

// inviteResponse — что панель отвечает на просьбу выпустить приглашение.
type inviteResponse struct {
	Token   string `json:"token"`
	Command string `json:"command"`
	Invite  struct {
		ID int64 `json:"id"`
	} `json:"invite"`
}

// registerResponse — что получает нода, записавшись по приглашению.
type registerResponse struct {
	Token string `json:"token"`
	Node  struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Address string `json:"address"`
		SNI     string `json:"sni"`
	} `json:"node"`
}

// nodeParams — тело запроса от установщика.
func nodeParams(t *testing.T, name string) map[string]any {
	t.Helper()

	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключ ноды: %v", err)
	}
	reality, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключ REALITY: %v", err)
	}

	return map[string]any{
		"name":               name,
		"port":               443,
		"sni":                "www.samsung.com",
		"public_key":         vp1.EncodeKey(pair.Public),
		"reality_public_key": vp1.EncodeKey(reality.Public),
		"reality_short_id":   "b5ed22c25143cd72",
	}
}

// TestInviteOnlyWorksOnce — главное свойство приглашения.
//
// Приглашение уезжает на чужой сервер в открытом виде: оно стоит в адресе
// команды, попадает в историю оболочки и в переписку с тем, кто эту ноду
// ставит. Единственное, что делает такую передачу допустимой, — что второй раз
// оно не сработает.
func TestInviteOnlyWorksOnce(t *testing.T) {
	h := newHarness(t)

	var invite inviteResponse
	if code := h.do(http.MethodPost, "/api/v1/nodes/invite", adminToken, map[string]any{"label": "первая нода"}, &invite); code != http.StatusOK {
		t.Fatalf("выпуск приглашения: код %d", code)
	}
	if invite.Token == "" {
		t.Fatal("панель не отдала приглашение")
	}
	if !strings.Contains(invite.Command, invite.Token) {
		t.Errorf("в готовой команде нет приглашения: %q", invite.Command)
	}

	var first registerResponse
	if code := h.do(http.MethodPost, "/api/v1/nodes/register", invite.Token, nodeParams(t, "ае-1"), &first); code != http.StatusOK {
		t.Fatalf("первая регистрация: код %d", code)
	}
	if first.Token == "" {
		t.Fatal("нода не получила своего токена")
	}
	if first.Token == invite.Token {
		t.Error("нода получила обратно приглашение вместо постоянного токена")
	}

	code := h.do(http.MethodPost, "/api/v1/nodes/register", invite.Token, nodeParams(t, "самозванец"), nil)
	if code != http.StatusForbidden {
		t.Errorf("повторная регистрация прошла с кодом %d, ожидался 403", code)
	}
}

// TestInviteGivesNoOtherRights — приглашение не заменяет админский токен.
//
// Оно уезжает на машину, которой мы не доверяем, и должно давать ровно одно
// право: один раз назваться нодой. Ни списка покупателей, ни выпуска доступов.
func TestInviteGivesNoOtherRights(t *testing.T) {
	h := newHarness(t)

	var invite inviteResponse
	if code := h.do(http.MethodPost, "/api/v1/nodes/invite", adminToken, nil, &invite); code != http.StatusOK {
		t.Fatalf("выпуск приглашения: код %d", code)
	}

	forbidden := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPost, "/api/v1/users"},
		{http.MethodGet, "/api/v1/nodes"},
		{http.MethodPost, "/api/v1/nodes"},
		{http.MethodGet, "/api/v1/node/users"},
	}

	for _, c := range forbidden {
		code := h.do(c.method, c.path, invite.Token, nil, nil)
		if code != http.StatusUnauthorized && code != http.StatusBadRequest {
			t.Errorf("%s %s по приглашению вернул %d, а должен был отказать", c.method, c.path, code)
		}
	}
}

// TestInviteWithoutAdminTokenRefused — приглашения выпускает только хозяин.
func TestInviteWithoutAdminTokenRefused(t *testing.T) {
	h := newHarness(t)

	if code := h.do(http.MethodPost, "/api/v1/nodes/invite", "", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("без токена вернулся код %d, ожидался 401", code)
	}
	if code := h.do(http.MethodPost, "/api/v1/nodes/invite", "чужой-токен-достаточной-длины", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("с чужим токеном вернулся код %d, ожидался 401", code)
	}
}

// TestInstallScriptHiddenWithoutInvite — установщик не лежит на виду.
//
// Найденный посторонним установщик был бы однозначной вывеской «здесь панель
// обхода блокировок». Поэтому по неверному приглашению отвечаем как обычный
// сайт на несуществующий путь.
func TestInstallScriptHiddenWithoutInvite(t *testing.T) {
	h := newHarness(t)

	for _, token := range []string{"нет-такого", "", strings.Repeat("a", 43)} {
		code := h.do(http.MethodGet, "/install/"+token, "", nil, nil)
		if code != http.StatusNotFound && code != http.StatusMovedPermanently {
			t.Errorf("по приглашению %q установщик отдался с кодом %d, ожидался 404", token, code)
		}
	}
}

// TestInstallScriptCarriesPanelAndInvite — выданный скрипт готов к запуску.
func TestInstallScriptCarriesPanelAndInvite(t *testing.T) {
	h := newHarness(t)

	var invite inviteResponse
	if code := h.do(http.MethodPost, "/api/v1/nodes/invite", adminToken, nil, &invite); code != http.StatusOK {
		t.Fatalf("выпуск приглашения: код %d", code)
	}

	resp, err := h.server.Client().Get(h.server.URL + "/install/" + invite.Token)
	if err != nil {
		t.Fatalf("запрос установщика: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("установщик отдался с кодом %d", resp.StatusCode)
	}

	body := readAll(t, resp)
	for _, want := range []string{
		"#!/bin/sh",
		invite.Token,
		"https://sub.example.com",
		"/api/v1/nodes/register",
		"marvia-node.service",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в установщике нет %q", want)
		}
	}

	// Приглашение отдачей скрипта не гасится: нода ещё не записалась.
	if code := h.do(http.MethodPost, "/api/v1/nodes/register", invite.Token, nodeParams(t, "ае-1"), nil); code != http.StatusOK {
		t.Errorf("после скачивания установщика регистрация вернула %d", code)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("чтение ответа: %v", err)
	}
	return string(raw)
}
