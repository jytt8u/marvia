package panel_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/updater"
)

// TestANodeThatReachedTheVersionDropsTheRequest: продавец просит отставшие
// ноды обновиться; нода, сообщившая новую версию, снимает просьбу сама, а
// выключенную ноду просьба не трогает вовсе.
func TestANodeThatReachedTheVersionDropsTheRequest(t *testing.T) {
	h := newHarness(t)
	fresh := h.createNode("fresh")
	stale := h.createNode("stale")
	off := h.createNode("off")
	h.do(http.MethodPatch, path("/api/v1/nodes/", off.Node.ID), adminToken, map[string]any{"enabled": false}, nil)

	report := func(token, version string) {
		t.Helper()
		code := h.do(http.MethodPost, "/api/v1/node/usage", token,
			map[string]any{"usage": map[string]any{}, "version": version, "arch": "amd64"}, nil)
		if code != http.StatusNoContent {
			t.Fatalf("отчёт ноды: код %d", code)
		}
	}
	report(fresh.Token, "v0.12.0")
	report(stale.Token, "v0.11.0")

	// Номер последнего релиза панель узнаёт у GitHub; здесь — у подставного.
	defer h.fakeRelease("v0.12.0")()

	var asked struct {
		Target string `json:"target"`
		Asked  int    `json:"asked"`
	}
	if code := h.do(http.MethodPost, "/api/v1/updates/nodes", adminToken, nil, &asked); code != http.StatusOK {
		t.Fatalf("просьба нодам: код %d", code)
	}
	if asked.Target != "v0.12.0" || asked.Asked != 1 {
		t.Fatalf("попросили %+v, ждали одну ноду до v0.12.0", asked)
	}

	upgradeTo := func(token string) string {
		var out struct {
			UpgradeTo string `json:"upgrade_to"`
		}
		h.do(http.MethodGet, "/api/v1/node/users", token, nil, &out)
		return out.UpgradeTo
	}
	if got := upgradeTo(stale.Token); got != "v0.12.0" {
		t.Fatalf("отставшая нода получила просьбу %q", got)
	}
	if got := upgradeTo(fresh.Token); got != "" {
		t.Fatalf("свежую ноду попросили обновиться до %q", got)
	}

	report(stale.Token, "v0.12.0")
	if got := upgradeTo(stale.Token); got != "" {
		t.Fatalf("дошедшая нода всё ещё просит %q", got)
	}

	var view struct {
		Latest string `json:"latest"`
		Nodes  []struct {
			Name, Version, Arch, UpgradeTo string
		} `json:"nodes"`
	}
	h.do(http.MethodGet, "/api/v1/updates", adminToken, nil, &view)
	if view.Latest != "v0.12.0" || len(view.Nodes) != 3 || view.Nodes[1].Version != "v0.12.0" || view.Nodes[1].Arch != "amd64" {
		t.Fatalf("страница обновлений: %+v", view)
	}
}

// TestPanelUpgradeNeedsTheService: без службы обновления кнопка честно
// отказывает и говорит, что сделать; со службой — кладёт просьбу, которую
// служба и сторожит.
func TestPanelUpgradeNeedsTheService(t *testing.T) {
	home := t.TempDir()
	h := newHarnessAt(t, home)
	was := updater.AgentPath
	updater.AgentPath = filepath.Join(t.TempDir(), "agent.sh")
	t.Cleanup(func() { updater.AgentPath = was })

	if code := h.do(http.MethodPost, "/api/v1/updates/panel", adminToken, nil, nil); code != http.StatusConflict {
		t.Fatalf("без службы: код %d", code)
	}
	_ = os.WriteFile(updater.AgentPath, []byte("#!/bin/sh\n"), 0o755)
	if code := h.do(http.MethodPost, "/api/v1/updates/panel", adminToken, nil, nil); code != http.StatusAccepted {
		t.Fatalf("со службой: код %d", code)
	}
	if _, err := os.Stat(updater.RequestPath(home)); err != nil {
		t.Fatalf("просьба не легла: %v", err)
	}
	if code := h.do(http.MethodPost, "/api/v1/updates/panel", "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("без токена: код %d", code)
	}
}

// newHarnessAt — панель с каталогом home, куда кладутся просьбы службе
// обновления.
func newHarnessAt(t *testing.T, home string) *harness {
	t.Helper()
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	dist := t.TempDir()
	server := httptest.NewServer(panel.NewAPI(store, adminToken, "https://sub.example.com", dist).WithHome(home).Handler())
	t.Cleanup(server.Close)
	return &harness{t: t, server: server, dist: dist}
}

// fakeRelease подставляет GitHub, у которого последний релиз — tag.
func (h *harness) fakeRelease(tag string) (restore func()) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://github.com/jytt8u/marvia/releases/tag/"+tag, http.StatusFound)
	}))
	undo := panel.SetReleaseURL(gh.URL + "/releases/latest")
	return func() { undo(); gh.Close() }
}

// TestLatestReleaseIsReadFromTheRedirect: номер релиза — из переадресации
// GitHub, а мусор вместо номера — ошибка с причиной, а не пустая строка.
func TestLatestReleaseIsReadFromTheRedirect(t *testing.T) {
	h := newHarness(t)
	defer h.fakeRelease("v1.2.3")()
	var view struct {
		Latest      string `json:"latest"`
		LatestError string `json:"latest_error"`
	}
	h.do(http.MethodGet, "/api/v1/updates", adminToken, nil, &view)
	if view.Latest != "v1.2.3" || view.LatestError != "" {
		t.Fatalf("релиз %+v", view)
	}

	h2 := newHarness(t)
	defer h2.fakeRelease("latest; rm -rf /")()
	h2.do(http.MethodGet, "/api/v1/updates", adminToken, nil, &view)
	if view.Latest != "" || view.LatestError == "" {
		t.Fatalf("мусор принят за релиз: %+v", view)
	}
	if code := h2.do(http.MethodPost, "/api/v1/updates/nodes", adminToken, nil, nil); code != http.StatusConflict {
		t.Fatalf("ноды попросили обновиться неизвестно до чего: код %d", code)
	}
}

// TestANewerNodeIsNeverRolledBack: нода из свежей сборки бывает новее
// релиза, и просьба «обновись до релиза» откатила бы её назад.
func TestANewerNodeIsNeverRolledBack(t *testing.T) {
	h := newHarness(t)
	ahead := h.createNode("ahead")
	h.do(http.MethodPost, "/api/v1/node/usage", ahead.Token, map[string]any{"usage": map[string]any{}, "version": "v0.13.0"}, nil)
	defer h.fakeRelease("v0.12.0")()
	var asked struct{ Asked int }
	h.do(http.MethodPost, "/api/v1/updates/nodes", adminToken, nil, &asked)
	if asked.Asked != 0 {
		t.Fatalf("ноду новее релиза попросили откатиться")
	}
	for _, c := range []struct {
		a, b  string
		older bool
	}{
		{"v0.11.0", "v0.12.0", true}, {"v0.12.0", "v0.12.0", false}, {"v0.13.0", "v0.12.0", false},
		{"v0.9.9", "v0.10.0", true}, {"dev", "v0.12.0", false}, {"v1.0.0-rc1", "v1.0.1", true},
	} {
		if got := panel.OlderVersion(c.a, c.b); got != c.older {
			t.Errorf("%s старше %s: %v", c.a, c.b, got)
		}
	}
}

// TestNodeFromTheFutureIsHeard: нода новее панели шлёт поля, которых панель
// не знает, — отчёт всё равно принимается, а не отвергается целиком.
func TestNodeFromTheFutureIsHeard(t *testing.T) {
	h := newHarness(t)
	n := h.createNode("future")
	code := h.do(http.MethodPost, "/api/v1/node/usage", n.Token,
		map[string]any{"usage": map[string]any{}, "version": "v9.0.0", "cpu_load": 0.4}, nil)
	if code != http.StatusNoContent {
		t.Fatalf("отчёт с незнакомым полем: код %d", code)
	}
}
