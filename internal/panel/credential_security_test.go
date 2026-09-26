package panel_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/users"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Проверяем границу панель → нода: выданный человеку закрытый ключ не должен
// попасть в список доступа, включая дополнительные поля записи.
func TestNodeAccessListDoesNotContainVP1PrivateKey(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("security")
	created := h.createUser(0, panel.CredVP1, panel.CredVLESS, panel.CredTrojan)
	private := created.secretOf(panel.CredVP1)
	raw, err := vp1.DecodeKey(private)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := vp1.KeyPairFromPrivate(raw)
	if err != nil {
		t.Fatal(err)
	}
	list := h.nodeUsers(node.Token)
	if len(list) != 3 {
		t.Fatalf("получено %d записей вместо трёх", len(list))
	}
	wire, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), private) {
		t.Fatal("закрытый ключ клиента передан ноде")
	}
	seen := map[string]bool{}
	for _, entry := range list {
		seen[entry.Kind] = true
		if entry.Kind == users.KindVP1 {
			if entry.Secret != vp1.EncodeKey(pair.Public) {
				t.Fatal("нода получила не публичный ключ клиента")
			}
		} else if entry.Secret != created.secretOf(entry.Kind) {
			t.Fatalf("учётные данные %s не совпадают с выданными клиенту", entry.Kind)
		}
	}
	if !seen[users.KindVP1] || !seen[users.KindVLESS] || !seen[users.KindTrojan] {
		t.Fatal("список не содержит все три протокола")
	}
}
