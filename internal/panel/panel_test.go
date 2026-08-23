package panel_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veilproject/veil/internal/panel"
	"github.com/veilproject/veil/internal/users"
	"github.com/veilproject/veil/internal/vp1"
)

const adminToken = "тестовый-админский-токен-достаточной-длины"

type harness struct {
	t      *testing.T
	server *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	api := panel.NewAPI(store, adminToken, "https://sub.example.com")
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	return &harness{t: t, server: server}
}

// do выполняет запрос и разбирает ответ.
func (h *harness) do(method, path, token string, body any, out any) int {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

type createUserResponse struct {
	User        panel.User `json:"user"`
	PrivateKey  string     `json:"private_key"`
	AccountLink string     `json:"account_link"`
}

type createNodeResponse struct {
	Node  panel.Node `json:"node"`
	Token string     `json:"token"`
}

type nodeUsersResponse struct {
	Users []users.User `json:"users"`
}

func (h *harness) createUser(limit int64) createUserResponse {
	h.t.Helper()
	var out createUserResponse
	code := h.do(http.MethodPost, "/api/v1/users", adminToken,
		map[string]any{"label": "заказ 1043", "traffic_limit": limit, "max_ips": 3}, &out)
	if code != http.StatusOK {
		h.t.Fatalf("создание пользователя: код %d", code)
	}
	return out
}

func (h *harness) createNode(name string) createNodeResponse {
	h.t.Helper()
	pair, _ := vp1.GenerateKeyPair()
	var out createNodeResponse
	code := h.do(http.MethodPost, "/api/v1/nodes", adminToken, map[string]any{
		"name": name, "address": name + ".example.com:443",
		"sni": name + ".example.com", "public_key": vp1.EncodeKey(pair.Public),
	}, &out)
	if code != http.StatusOK {
		h.t.Fatalf("создание ноды: код %d", code)
	}
	return out
}

func (h *harness) nodeUsers(token string) []users.User {
	h.t.Helper()
	var out nodeUsersResponse
	code := h.do(http.MethodGet, "/api/v1/node/users", token, nil, &out)
	if code != http.StatusOK {
		h.t.Fatalf("список для ноды: код %d", code)
	}
	return out.Users
}

func TestCreateUserReturnsPrivateKeyOnce(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)

	if created.PrivateKey == "" {
		t.Fatal("приватный ключ не выдан")
	}
	if _, err := vp1.DecodeKey(created.PrivateKey); err != nil {
		t.Fatalf("приватный ключ не разбирается: %v", err)
	}
	if !strings.Contains(created.AccountLink, created.PrivateKey) {
		t.Fatal("ссылка для покупателя не содержит его ключ")
	}

	// Повторное чтение пользователя приватный ключ вернуть не должно.
	var out struct {
		User       panel.User `json:"user"`
		PrivateKey string     `json:"private_key"`
	}
	h.do(http.MethodGet, fmt.Sprintf("/api/v1/users/%d", created.User.ID), adminToken, nil, &out)
	if out.PrivateKey != "" {
		t.Fatal("панель отдала приватный ключ повторно — она не должна его хранить")
	}
	if len(out.User.Credentials) != 1 || out.User.Credentials[0].Kind != panel.CredVP1 {
		t.Fatalf("ожидался один ключ вида vp1, получено: %+v", out.User.Credentials)
	}
}

func TestAdminEndpointsRequireToken(t *testing.T) {
	h := newHarness(t)

	if code := h.do(http.MethodGet, "/api/v1/users", "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("без токена ожидался 401, получено %d", code)
	}
	if code := h.do(http.MethodGet, "/api/v1/users", "не-тот-токен", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("с чужим токеном ожидался 401, получено %d", code)
	}
	if code := h.do(http.MethodGet, "/api/v1/node/users", "не-тот-токен", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("нода с чужим токеном: ожидался 401, получено %d", code)
	}
}

// TestQuotaSharedAcrossNodes — главная проверка панели.
//
// Каждая нода считает только свой трафик. Если не вычитать израсходованное на
// других, квоту в 100 ГБ можно потратить на каждой ноде отдельно, и продавец
// раздаст в разы больше трафика, чем продал.
func TestQuotaSharedAcrossNodes(t *testing.T) {
	h := newHarness(t)

	const limit = 1000
	created := h.createUser(limit)
	first := h.createNode("first")
	second := h.createNode("second")

	list := h.nodeUsers(first.Token)
	if len(list) != 1 {
		t.Fatalf("нода получила %d пользователей, ожидался 1", len(list))
	}
	if list[0].TrafficLimit != limit {
		t.Fatalf("свежий пользователь: лимит %d, ожидался %d", list[0].TrafficLimit, limit)
	}
	account := list[0].AccountID()

	// Первая нода израсходовала 600 из 1000.
	if code := h.do(http.MethodPost, "/api/v1/node/usage", first.Token,
		map[string]any{"usage": map[string]users.Usage{account: {Up: 300, Down: 300}}}, nil); code != http.StatusNoContent {
		t.Fatalf("отчёт первой ноды: код %d", code)
	}

	// Вторая должна увидеть остаток.
	list = h.nodeUsers(second.Token)
	if list[0].TrafficLimit != 400 {
		t.Fatalf("вторая нода: лимит %d, ожидалось 400", list[0].TrafficLimit)
	}
	if !list[0].Enabled {
		t.Fatal("пользователь с остатком квоты должен быть включён")
	}

	// Вторая нода добирает остаток.
	if code := h.do(http.MethodPost, "/api/v1/node/usage", second.Token,
		map[string]any{"usage": map[string]users.Usage{account: {Up: 200, Down: 200}}}, nil); code != http.StatusNoContent {
		t.Fatalf("отчёт второй ноды: код %d", code)
	}

	// Каждая из двух работавших нод получает остаток за вычетом чужого
	// расхода и упирается в него своим локальным счётчиком: первая уже
	// израсходовала 600 при лимите 600, вторая — 400 при лимите 400.
	if got := h.nodeUsers(first.Token)[0].TrafficLimit; got != 600 {
		t.Fatalf("первая нода: лимит %d, ожидалось 600 (1000 минус 400 на второй)", got)
	}
	if got := h.nodeUsers(second.Token)[0].TrafficLimit; got != 400 {
		t.Fatalf("вторая нода: лимит %d, ожидалось 400 (1000 минус 600 на первой)", got)
	}

	// А вот нода, которая этого человека ещё не видела, обязана считать его
	// отключённым: весь его лимит уже израсходован на других.
	third := h.createNode("third")
	list = h.nodeUsers(third.Token)
	if list[0].Enabled {
		t.Fatal("новая нода: пользователь с исчерпанной квотой должен быть выключен")
	}
	if list[0].TrafficLimit != 0 {
		t.Fatalf("новая нода: лимит %d, у выключенного пользователя он должен быть нулевым", list[0].TrafficLimit)
	}

	// А панель — показывать полный расход.
	var out struct {
		User panel.User `json:"user"`
	}
	h.do(http.MethodGet, fmt.Sprintf("/api/v1/users/%d", created.User.ID), adminToken, nil, &out)
	if out.User.Used != 1000 {
		t.Fatalf("панель показывает расход %d, ожидалось 1000", out.User.Used)
	}
}

// TestDevicesShareOneAccount: второй ключ — это второе устройство, а не второй
// лимит. Иначе покупатель удваивал бы себе квоту, попросив ещё один конфиг.
func TestDevicesShareOneAccount(t *testing.T) {
	h := newHarness(t)

	created := h.createUser(5000)
	node := h.createNode("single")

	var added struct {
		Credential panel.Credential `json:"credential"`
		PrivateKey string           `json:"private_key"`
	}
	code := h.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/credentials", created.User.ID),
		adminToken, map[string]any{"label": "телефон"}, &added)
	if code != http.StatusOK {
		t.Fatalf("выпуск второго ключа: код %d", code)
	}
	if added.PrivateKey == "" {
		t.Fatal("приватный ключ второго устройства не выдан")
	}

	list := h.nodeUsers(node.Token)
	if len(list) != 2 {
		t.Fatalf("нода получила %d ключей, ожидалось 2", len(list))
	}
	if list[0].AccountID() != list[1].AccountID() {
		t.Fatalf("ключи одного человека попали в разные аккаунты: %q и %q",
			list[0].AccountID(), list[1].AccountID())
	}
	if list[0].PublicKey == list[1].PublicKey {
		t.Fatal("у двух устройств оказался один и тот же ключ")
	}

	// Отзыв одного устройства не должен трогать второе.
	if code := h.do(http.MethodDelete, fmt.Sprintf("/api/v1/credentials/%d", added.Credential.ID),
		adminToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("отзыв ключа: код %d", code)
	}
	if list = h.nodeUsers(node.Token); len(list) != 1 {
		t.Fatalf("после отзыва осталось %d ключей, ожидался 1", len(list))
	}
}

func TestSubscriptionListsNodes(t *testing.T) {
	h := newHarness(t)

	created := h.createUser(1000)
	h.createNode("alpha")
	h.createNode("beta")

	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + created.User.SubToken)
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("подписка вернула %d", resp.StatusCode)
	}
	if info := resp.Header.Get("Subscription-Userinfo"); !strings.Contains(info, "total=1000") {
		t.Fatalf("нет заголовка с остатком квоты: %q", info)
	}

	raw, _ := io.ReadAll(resp.Body)
	decoded, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		t.Fatalf("подписка не в base64: %v", err)
	}
	body := string(decoded)
	if !strings.Contains(body, "alpha.example.com:443") || !strings.Contains(body, "beta.example.com:443") {
		t.Fatalf("в подписке нет обеих нод: %q", body)
	}
	if !strings.HasPrefix(body, "veil://") {
		t.Fatalf("ссылка не той схемы: %q", body)
	}
	// В списке нод не должно быть ничего секретного: подписку могут перехватить.
	if strings.Contains(body, created.PrivateKey) {
		t.Fatal("приватный ключ покупателя утёк в подписку")
	}
}

func TestUnknownSubscriptionIsNotFound(t *testing.T) {
	h := newHarness(t)

	resp, err := h.server.Client().Get(h.server.URL + "/sub/такого-токена-нет")
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ожидался 404, получено %d", resp.StatusCode)
	}
}

func TestDisableAndExtendUser(t *testing.T) {
	h := newHarness(t)

	created := h.createUser(0)
	node := h.createNode("single")

	disabled := false
	var out struct {
		User panel.User `json:"user"`
	}
	code := h.do(http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.User.ID),
		adminToken, map[string]any{"enabled": disabled}, &out)
	if code != http.StatusOK {
		t.Fatalf("отключение: код %d", code)
	}
	if out.User.Enabled {
		t.Fatal("пользователь остался включённым")
	}

	list := h.nodeUsers(node.Token)
	if len(list) != 1 || list[0].Enabled {
		t.Fatalf("нода не увидела отключение: %+v", list)
	}

	// Оплатил — включаем обратно.
	h.do(http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.User.ID),
		adminToken, map[string]any{"enabled": true, "expires_at": "2027-01-01T00:00:00Z"}, &out)
	if !out.User.Enabled || out.User.ExpiresAt == nil {
		t.Fatalf("продление не применилось: %+v", out.User)
	}
}
