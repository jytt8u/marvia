package panel_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

// subscriptionNodes возвращает список нод из подписки для нашего клиента.
func subscriptionNodes(t *testing.T, h *harness, subToken string) []struct {
	Name    string `json:"name"`
	Country string `json:"country"`
	Link    string `json:"link"`
} {
	t.Helper()

	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + subToken + "?format=json")
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		Nodes []struct {
			Name    string `json:"name"`
			Country string `json:"country"`
			Link    string `json:"link"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("разбор подписки: %v", err)
	}
	return out.Nodes
}

// stockSubscription возвращает подписку для чужих клиентов уже развёрнутой.
func stockSubscription(t *testing.T, h *harness, subToken string) string {
	t.Helper()

	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + subToken)
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	body, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		t.Fatalf("подписка не в base64: %v", err)
	}
	return string(body)
}

// TestNodeRename — продавец даёт ноде человеческое имя.
//
// От хостера она приходит как vm-4823917-ubuntu, и покупатель видит это же в
// списке серверов. Раньше исправить было нельзя вовсе.
func TestNodeRename(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("vm-4823917")
	created := h.createUser(0, panel.CredVP1)

	var out struct {
		Node panel.Node `json:"node"`
	}
	code := h.do(http.MethodPatch, path("/api/v1/nodes/", node.Node.ID), adminToken,
		map[string]any{"name": "Амстердам-1"}, &out)
	if code != http.StatusOK {
		t.Fatalf("переименование: код %d", code)
	}
	if out.Node.Name != "Амстердам-1" {
		t.Fatalf("имя не изменилось: %q", out.Node.Name)
	}

	// Новое имя должно дойти до покупателя, а не остаться в панели.
	nodes := subscriptionNodes(t, h, created.User.SubToken)
	if len(nodes) != 1 || nodes[0].Name != "Амстердам-1" {
		t.Fatalf("в подписке %+v", nodes)
	}
}

// TestNodeCountryReachesClients — страна доходит и до нашего клиента, и до чужих.
//
// Чужим её отдельно не передать: флажок они подбирают по названию, поэтому
// страна встаёт в него первой.
func TestNodeCountryReachesClients(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("ams")
	created := h.createUser(0, panel.CredVP1, panel.CredVLESS)

	code := h.do(http.MethodPatch, path("/api/v1/nodes/", node.Node.ID), adminToken,
		map[string]any{"name": "Амстердам-1", "country": "Нидерланды"}, nil)
	if code != http.StatusOK {
		t.Fatalf("страна не проставилась: код %d", code)
	}

	nodes := subscriptionNodes(t, h, created.User.SubToken)
	if len(nodes) != 1 {
		t.Fatalf("нод в подписке %d", len(nodes))
	}
	if nodes[0].Country != "Нидерланды" {
		t.Fatalf("страна не дошла до нашего клиента: %+v", nodes[0])
	}
	if title := fragmentOf(t, nodes[0].Link); !strings.HasPrefix(title, "Нидерланды") {
		t.Errorf("страна не первая в названии ноды: %q", title)
	}

	// Чужому клиенту страну передать больше нечем: он читает только название.
	for _, link := range strings.Split(stockSubscription(t, h, created.User.SubToken), "\n") {
		if title := fragmentOf(t, link); !strings.HasPrefix(title, "Нидерланды") {
			t.Errorf("страны нет в названии для чужого клиента: %q", title)
		}
	}
}

// TestNodeDisableRemovesItFromSubscriptions — выключение вместо удаления.
//
// При подозрении на блокировку ноду надо вывести из работы, не потеряв её
// статистику и не закрыв себе дорогу назад.
func TestNodeDisableRemovesItFromSubscriptions(t *testing.T) {
	h := newHarness(t)
	first := h.createNode("alpha")
	h.createNode("beta")
	created := h.createUser(0, panel.CredVP1, panel.CredVLESS)

	if n := subscriptionNodes(t, h, created.User.SubToken); len(n) != 2 {
		t.Fatalf("до выключения нод %d, ожидалось 2", len(n))
	}

	code := h.do(http.MethodPatch, path("/api/v1/nodes/", first.Node.ID), adminToken,
		map[string]any{"enabled": false}, nil)
	if code != http.StatusOK {
		t.Fatalf("выключение: код %d", code)
	}

	nodes := subscriptionNodes(t, h, created.User.SubToken)
	if len(nodes) != 1 || nodes[0].Name != "beta" {
		t.Fatalf("выключенная нода осталась в подписке: %+v", nodes)
	}
	if stock := stockSubscription(t, h, created.User.SubToken); strings.Contains(stock, "alpha") {
		t.Errorf("выключенная нода осталась в подписке для чужих клиентов: %s", stock)
	}

	// Но из панели не пропала — иначе продавец решит, что она удалилась.
	var list struct {
		Nodes []panel.Node `json:"nodes"`
	}
	if code := h.do(http.MethodGet, "/api/v1/nodes", adminToken, nil, &list); code != http.StatusOK {
		t.Fatalf("список нод: код %d", code)
	}
	if len(list.Nodes) != 2 {
		t.Fatalf("нод в панели %d, ожидалось 2", len(list.Nodes))
	}

	// И включается обратно.
	if code := h.do(http.MethodPatch, path("/api/v1/nodes/", first.Node.ID), adminToken,
		map[string]any{"enabled": true}, nil); code != http.StatusOK {
		t.Fatalf("включение: код %d", code)
	}
	if n := subscriptionNodes(t, h, created.User.SubToken); len(n) != 2 {
		t.Fatalf("после включения нод %d, ожидалось 2", len(n))
	}
}

// TestNodePatchRejectsEmptyName — пустое имя не проходит.
//
// Иначе нода теряет и имя хостера, и человеческое, и в списке у покупателя
// остаётся пустая строка.
func TestNodePatchRejectsEmptyName(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("alpha")

	if code := h.do(http.MethodPatch, path("/api/v1/nodes/", node.Node.ID), adminToken,
		map[string]any{"name": "  "}, nil); code != http.StatusBadRequest {
		t.Fatalf("пустое имя принято: код %d", code)
	}
}

// TestNodePatchNeedsNodesScope — ключ бота до нод не дотягивается и здесь.
func TestNodePatchNeedsNodesScope(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("alpha")

	var key struct {
		Secret string `json:"secret"`
	}
	if code := h.do(http.MethodPost, "/api/v1/keys", adminToken,
		map[string]any{"name": "бот", "scopes": []string{"users"}}, &key); code != http.StatusOK {
		t.Fatalf("выпуск ключа: код %d", code)
	}

	if code := h.do(http.MethodPatch, path("/api/v1/nodes/", node.Node.ID), key.Secret,
		map[string]any{"name": "чужими руками"}, nil); code != http.StatusForbidden {
		t.Fatalf("ключ на покупателей переименовал ноду: код %d", code)
	}
}

func path(prefix string, id int64) string {
	return prefix + strconv.FormatInt(id, 10)
}

// fragmentOf достаёт из ссылки то, что клиент покажет в списке серверов.
func fragmentOf(t *testing.T, link string) string {
	t.Helper()

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("ссылка не разбирается: %q: %v", link, err)
	}
	return u.Fragment
}
