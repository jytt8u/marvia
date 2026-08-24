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

type links struct {
	Account      string   `json:"account"`
	Subscription string   `json:"subscription"`
	Stock        []string `json:"stock"`
}

type createUserResponse struct {
	User   panel.User     `json:"user"`
	Issued []panel.Issued `json:"issued"`
	Links  links          `json:"links"`
}

// secretOf находит выданный секрет нужного вида.
func (r createUserResponse) secretOf(kind string) string {
	for _, i := range r.Issued {
		if i.Kind == kind {
			return i.Secret
		}
	}
	return ""
}

type createNodeResponse struct {
	Node  panel.Node `json:"node"`
	Token string     `json:"token"`
}

type nodeUsersResponse struct {
	Users []users.User `json:"users"`
}

func (h *harness) createUser(limit int64, kinds ...string) createUserResponse {
	h.t.Helper()

	body := map[string]any{"label": "заказ 1043", "traffic_limit": limit, "max_ips": 3}
	if len(kinds) > 0 {
		body["kinds"] = kinds
	}

	var out createUserResponse
	if code := h.do(http.MethodPost, "/api/v1/users", adminToken, body, &out); code != http.StatusOK {
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

func TestCreateUserReturnsSecretOnce(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)

	private := created.secretOf(panel.CredVP1)
	if private == "" {
		t.Fatal("приватный ключ не выдан")
	}
	if _, err := vp1.DecodeKey(private); err != nil {
		t.Fatalf("приватный ключ не разбирается: %v", err)
	}
	if !strings.Contains(created.Links.Account, private) {
		t.Fatal("ссылка для покупателя не содержит его ключ")
	}

	// Повторное чтение пользователя секреты вернуть не должно.
	var out struct {
		User   panel.User     `json:"user"`
		Issued []panel.Issued `json:"issued"`
	}
	h.do(http.MethodGet, fmt.Sprintf("/api/v1/users/%d", created.User.ID), adminToken, nil, &out)
	if len(out.Issued) != 0 {
		t.Fatal("панель отдала секреты повторно")
	}
	for _, c := range out.User.Credentials {
		if c.Secret == private {
			t.Fatal("панель хранит приватный ключ vp1 — она не должна этого делать")
		}
	}
}

// TestCreateUserWithAllKinds — то, ради чего всё затевалось: продавец одним
// запросом получает и наш доступ, и доступ для приложений, которые у
// покупателя уже стоят.
func TestCreateUserWithAllKinds(t *testing.T) {
	h := newHarness(t)
	h.createNode("msk")

	created := h.createUser(0, panel.CredVP1, panel.CredVLESS, panel.CredTrojan)

	if len(created.Issued) != 3 {
		t.Fatalf("выдано %d наборов, ожидалось 3: %+v", len(created.Issued), created.Issued)
	}
	if _, err := users.ParseUUID(created.secretOf(panel.CredVLESS)); err != nil {
		t.Fatalf("секрет vless не похож на UUID: %v", err)
	}
	if len(created.secretOf(panel.CredTrojan)) < 16 {
		t.Fatalf("пароль trojan подозрительно короткий: %q", created.secretOf(panel.CredTrojan))
	}

	if len(created.Links.Stock) != 2 {
		t.Fatalf("готовых ссылок для чужих клиентов %d, ожидалось 2: %v", len(created.Links.Stock), created.Links.Stock)
	}

	var haveVLESS, haveTrojan bool
	for _, link := range created.Links.Stock {
		switch {
		case strings.HasPrefix(link, "vless://"):
			haveVLESS = true
			for _, must := range []string{"encryption=none", "security=tls", "sni=msk.example.com", "fp=chrome"} {
				if !strings.Contains(link, must) {
					t.Fatalf("в ссылке vless нет %q: %s", must, link)
				}
			}
		case strings.HasPrefix(link, "trojan://"):
			haveTrojan = true
			if !strings.Contains(link, "security=tls") {
				t.Fatalf("в ссылке trojan нет security=tls: %s", link)
			}
		}
	}
	if !haveVLESS || !haveTrojan {
		t.Fatalf("не хватает ссылок: %v", created.Links.Stock)
	}
}

// TestNodeSeesAllKinds: нода должна получить все наборы подписчика, и все они
// обязаны попасть в один аккаунт — иначе квота размножится по числу
// протоколов.
func TestNodeSeesAllKinds(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("single")
	h.createUser(5000, panel.CredVP1, panel.CredVLESS, panel.CredTrojan)

	list := h.nodeUsers(node.Token)
	if len(list) != 3 {
		t.Fatalf("нода получила %d записей, ожидалось 3", len(list))
	}

	kinds := make(map[string]bool)
	account := list[0].AccountID()
	for _, u := range list {
		kinds[u.Kind] = true
		if u.AccountID() != account {
			t.Fatalf("наборы одного подписчика попали в разные аккаунты: %q и %q", account, u.AccountID())
		}
		if u.TrafficLimit != 5000 {
			t.Fatalf("набор %s: лимит %d, ожидалось 5000", u.Kind, u.TrafficLimit)
		}
		if _, err := users.Identity(u.Kind, u.Secret); err != nil {
			t.Fatalf("набор %s: нода не сможет его опознать: %v", u.Kind, err)
		}
	}
	for _, kind := range []string{users.KindVP1, users.KindVLESS, users.KindTrojan} {
		if !kinds[kind] {
			t.Fatalf("нода не получила набор вида %s", kind)
		}
	}
}

func TestAddCredentialOfKind(t *testing.T) {
	h := newHarness(t)
	h.createNode("msk")
	created := h.createUser(0)

	var out struct {
		Credential panel.Credential `json:"credential"`
		Issued     []panel.Issued   `json:"issued"`
		Links      links            `json:"links"`
	}
	code := h.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/credentials", created.User.ID),
		adminToken, map[string]any{"kind": panel.CredVLESS, "label": "телефон"}, &out)
	if code != http.StatusOK {
		t.Fatalf("выпуск vless: код %d", code)
	}
	if out.Credential.Kind != panel.CredVLESS {
		t.Fatalf("выдан набор вида %q", out.Credential.Kind)
	}
	if len(out.Links.Stock) != 1 || !strings.HasPrefix(out.Links.Stock[0], "vless://") {
		t.Fatalf("не собралась ссылка vless: %v", out.Links.Stock)
	}

	// Неизвестный вид — понятная ошибка, а не пятисотка.
	if code := h.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/credentials", created.User.ID),
		adminToken, map[string]any{"kind": "wireguard"}, nil); code != http.StatusBadRequest {
		t.Fatalf("неизвестный вид: код %d, ожидался 400", code)
	}
}

// TestSubscriptionCarriesOnlyStockLinks: подписку читают чужие приложения, и
// незнакомая схема veil:// ломает часть из них целиком.
func TestSubscriptionCarriesOnlyStockLinks(t *testing.T) {
	h := newHarness(t)
	h.createNode("alpha")
	h.createNode("beta")
	created := h.createUser(1000, panel.CredVP1, panel.CredVLESS, panel.CredTrojan)

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

	if strings.Contains(body, "veil://") {
		t.Fatalf("в подписку для чужих клиентов попала наша схема: %q", body)
	}
	// Две ноды на два набора доступа.
	if got := strings.Count(body, "://"); got != 4 {
		t.Fatalf("ссылок %d, ожидалось 4 (две ноды на два набора): %q", got, body)
	}
	for _, must := range []string{"vless://", "trojan://", "alpha.example.com:443", "beta.example.com:443"} {
		if !strings.Contains(body, must) {
			t.Fatalf("в подписке нет %q: %q", must, body)
		}
	}
	if strings.Contains(body, created.secretOf(panel.CredVP1)) {
		t.Fatal("приватный ключ vp1 утёк в подписку")
	}
}

// TestSubscriptionJSONForOwnClient: наш клиент берёт из подписки только список
// нод, без единого секрета — свой ключ у него уже есть.
func TestSubscriptionJSONForOwnClient(t *testing.T) {
	h := newHarness(t)
	h.createNode("alpha")
	created := h.createUser(1000, panel.CredVP1, panel.CredVLESS)

	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + created.User.SubToken + "?format=json")
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		Nodes []struct {
			Name      string `json:"name"`
			Address   string `json:"address"`
			PublicKey string `json:"public_key"`
			Link      string `json:"link"`
		} `json:"nodes"`
		TrafficLimit int64 `json:"traffic_limit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("разбор: %v", err)
	}

	if len(out.Nodes) != 1 {
		t.Fatalf("нод в ответе %d, ожидалась 1", len(out.Nodes))
	}
	if !strings.HasPrefix(out.Nodes[0].Link, "veil://") {
		t.Fatalf("ссылка не той схемы: %q", out.Nodes[0].Link)
	}
	if out.TrafficLimit != 1000 {
		t.Fatalf("лимит %d, ожидалось 1000", out.TrafficLimit)
	}

	body, _ := json.Marshal(out)
	if strings.Contains(string(body), created.secretOf(panel.CredVLESS)) {
		t.Fatal("секрет vless утёк в подписку для нашего клиента")
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

	// Каждая из работавших нод получает остаток за вычетом чужого расхода и
	// упирается в него своим локальным счётчиком.
	if got := h.nodeUsers(first.Token)[0].TrafficLimit; got != 600 {
		t.Fatalf("первая нода: лимит %d, ожидалось 600", got)
	}
	if got := h.nodeUsers(second.Token)[0].TrafficLimit; got != 400 {
		t.Fatalf("вторая нода: лимит %d, ожидалось 400", got)
	}

	// А нода, которая этого человека ещё не видела, обязана считать его
	// отключённым: весь его лимит уже израсходован на других.
	third := h.createNode("third")
	list = h.nodeUsers(third.Token)
	if list[0].Enabled {
		t.Fatal("новая нода: пользователь с исчерпанной квотой должен быть выключен")
	}

	var out struct {
		User panel.User `json:"user"`
	}
	h.do(http.MethodGet, fmt.Sprintf("/api/v1/users/%d", created.User.ID), adminToken, nil, &out)
	if out.User.Used != 1000 {
		t.Fatalf("панель показывает расход %d, ожидалось 1000", out.User.Used)
	}
}

// TestDevicesShareOneAccount: второй набор — это второе устройство, а не
// второй лимит.
func TestDevicesShareOneAccount(t *testing.T) {
	h := newHarness(t)

	created := h.createUser(5000)
	node := h.createNode("single")

	var added struct {
		Credential panel.Credential `json:"credential"`
		Issued     []panel.Issued   `json:"issued"`
	}
	code := h.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/credentials", created.User.ID),
		adminToken, map[string]any{"label": "телефон"}, &added)
	if code != http.StatusOK {
		t.Fatalf("выпуск второго ключа: код %d", code)
	}
	if len(added.Issued) != 1 || added.Issued[0].Secret == "" {
		t.Fatal("приватный ключ второго устройства не выдан")
	}

	list := h.nodeUsers(node.Token)
	if len(list) != 2 {
		t.Fatalf("нода получила %d наборов, ожидалось 2", len(list))
	}
	if list[0].AccountID() != list[1].AccountID() {
		t.Fatalf("наборы одного человека попали в разные аккаунты: %q и %q",
			list[0].AccountID(), list[1].AccountID())
	}
	if list[0].Secret == list[1].Secret {
		t.Fatal("у двух устройств оказался один и тот же ключ")
	}

	if code := h.do(http.MethodDelete, fmt.Sprintf("/api/v1/credentials/%d", added.Credential.ID),
		adminToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("отзыв ключа: код %d", code)
	}
	if list = h.nodeUsers(node.Token); len(list) != 1 {
		t.Fatalf("после отзыва осталось %d наборов, ожидался 1", len(list))
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

	var out struct {
		User panel.User `json:"user"`
	}
	code := h.do(http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.User.ID),
		adminToken, map[string]any{"enabled": false}, &out)
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

// TestWebAppIsServedWithStrictPolicy: на странице панели живёт админский
// токен, поэтому чужой скрипт в этом origin равносилен выдаче полного
// доступа. Проверяем, что политика жёсткая и что одноразовое значение
// подставлено, а не осталось шаблоном.
func TestWebAppIsServedWithStrictPolicy(t *testing.T) {
	h := newHarness(t)

	resp, err := h.server.Client().Get(h.server.URL + "/")
	if err != nil {
		t.Fatalf("страница: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("тип содержимого %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if strings.Contains(page, "%NONCE%") {
		t.Fatal("шаблон nonce не заменён — скрипт не запустится под политикой")
	}

	policy := resp.Header.Get("Content-Security-Policy")
	for _, must := range []string{"default-src 'none'", "script-src 'nonce-", "connect-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, must) {
			t.Fatalf("в политике нет %q: %s", must, policy)
		}
	}

	// Значение в заголовке и в разметке обязано совпадать.
	start := strings.Index(policy, "'nonce-") + len("'nonce-")
	end := strings.Index(policy[start:], "'") + start
	nonce := policy[start:end]
	if nonce == "" || !strings.Contains(page, `nonce="`+nonce+`"`) {
		t.Fatalf("значение nonce в заголовке и на странице не совпадает: %q", nonce)
	}

	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("страница кэшируется: %q", resp.Header.Get("Cache-Control"))
	}
}

func TestUnknownPathIsNotTheApp(t *testing.T) {
	h := newHarness(t)

	resp, err := h.server.Client().Get(h.server.URL + "/wp-admin")
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("на случайный путь ответили %d, ожидался 404", resp.StatusCode)
	}
}

// TestUserLinksForExistingUser: самое частое обращение в поддержку —
// «потерял конфиг». Для чужих протоколов ссылку можно собрать заново.
func TestUserLinksForExistingUser(t *testing.T) {
	h := newHarness(t)
	h.createNode("msk")
	created := h.createUser(0, panel.CredVP1, panel.CredVLESS, panel.CredTrojan)

	var out struct {
		Subscription string   `json:"subscription"`
		Stock        []string `json:"stock"`
	}
	code := h.do(http.MethodGet, fmt.Sprintf("/api/v1/users/%d/links", created.User.ID), adminToken, nil, &out)
	if code != http.StatusOK {
		t.Fatalf("ссылки: код %d", code)
	}
	if !strings.HasPrefix(out.Subscription, "https://sub.example.com/sub/") {
		t.Fatalf("адрес подписки: %q", out.Subscription)
	}
	if len(out.Stock) != 2 {
		t.Fatalf("ссылок %d, ожидалось 2: %v", len(out.Stock), out.Stock)
	}
	for _, link := range out.Stock {
		if strings.Contains(link, created.secretOf(panel.CredVP1)) {
			t.Fatal("приватный ключ vp1 попал в ссылку для чужого клиента")
		}
	}
}
