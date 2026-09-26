package panel_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestSecurityAdministrativeRoutesRejectOtherCredentialTypes(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("security")
	user := h.createUser(0)
	type credential struct {
		name, token string
		status      int
	}
	credentials := []credential{
		{"anonymous", "", http.StatusUnauthorized},
		{"unknown", "not-a-valid-credential", http.StatusUnauthorized},
		{"node", node.Token, http.StatusUnauthorized},
		{"subscription", user.User.SubToken, http.StatusUnauthorized},
	}
	for _, scopes := range [][]string{{"read"}, {"users", "nodes", "read"}} {
		var issued struct {
			Secret string `json:"secret"`
		}
		if code := h.do("POST", "/api/v1/keys", adminToken, map[string]any{"name": "проверка", "scopes": scopes}, &issued); code != http.StatusOK || issued.Secret == "" {
			t.Fatalf("выпуск ключа: %d", code)
		}
		credentials = append(credentials, credential{scopes[0], issued.Secret, http.StatusForbidden})
	}
	// Тела запросов намеренно корректные: отказ должен произойти до действия,
	// а не случайно из-за ошибки разбора JSON.
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/keys", nil},
		{"POST", "/api/v1/keys", map[string]any{"name": "эскалация", "scopes": []string{"users", "nodes", "read"}}},
		{"DELETE", "/api/v1/keys/1", nil},
		{"GET", "/api/v1/backup", nil},
		{"GET", "/api/v1/events", nil},
		{"GET", "/api/v1/alerts", nil},
		{"PUT", "/api/v1/alerts", map[string]any{"enabled": false}},
		{"POST", "/api/v1/updates/panel", map[string]any{"version": "0.12.0"}},
		{"POST", "/api/v1/updates/nodes", map[string]any{"version": "0.12.0"}},
	} {
		for _, c := range credentials {
			t.Run(c.name+"/"+route.method+route.path, func(t *testing.T) {
				if code := h.do(route.method, route.path, c.token, route.body, nil); code != c.status {
					t.Fatalf("ожидался отказ %d, получен %d", c.status, code)
				}
			})
		}
	}
	// Токен подписки и токен ноды также не должны удалять покупателей.
	for _, c := range credentials[:5] {
		t.Run(c.name+"/delete-user", func(t *testing.T) {
			if code := h.do("DELETE", fmt.Sprintf("/api/v1/users/%d", user.User.ID), c.token, nil, nil); code != c.status {
				t.Fatalf("ожидался отказ %d, получен %d", c.status, code)
			}
		})
	}
	if code := h.do("GET", fmt.Sprintf("/api/v1/users/%d", user.User.ID), adminToken, nil, nil); code != http.StatusOK {
		t.Fatal("отклонённый запрос изменил пользователя")
	}
}

func TestSecurityNodeEndpointsDoNotAcceptAdministrativeOrSubscriptionTokens(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("security")
	user := h.createUser(0)
	for _, token := range []string{adminToken, user.User.SubToken, ""} {
		for _, route := range []struct {
			method, path string
			body         any
		}{
			{"GET", "/api/v1/node/users", nil},
			{"POST", "/api/v1/node/usage", map[string]any{"users": []any{}}},
		} {
			if code := h.do(route.method, route.path, token, route.body, nil); code != http.StatusUnauthorized {
				t.Fatalf("чужой вид токена принят на %s: %d", route.path, code)
			}
		}
	}
	if code := h.do("GET", "/api/v1/node/users", node.Token, nil, nil); code != http.StatusOK {
		t.Fatalf("собственный токен ноды перестал работать: %d", code)
	}
}
