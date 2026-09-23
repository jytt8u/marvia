package panel

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/updater"
)

// Обновления по кнопке.
//
// Панель знает три вещи: свою версию, версии нод (их присылают сами ноды) и
// номер последнего релиза. По кнопке она ничего не ставит сама — у неё нет
// прав, и это правильно, — а кладёт просьбу службе обновления на своей
// машине (internal/updater) и ставит нодам пометку «обнови до такой-то»,
// которую они передают своим службам. Что именно ставить, решают службы:
// официальный релиз, сверенный с SHA256SUMS.

// releaseURL — страница последнего релиза. GitHub отвечает на неё
// переадресацией на .../tag/vX.Y.Z; номер берём из адреса, не трогая API
// с его ограничением на число запросов.
var releaseURL = "https://github.com/jytt8u/marvia/releases/latest"

// releaseEvery — как долго доверяем узнанному номеру. Релизы выходят раз в
// несколько дней, а страница «Обновления» открывается куда чаще.
const releaseEvery = 6 * time.Hour

var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// releaseCache помнит последний релиз.
type releaseCache struct {
	mu      sync.Mutex
	latest  string
	err     string
	checked time.Time
}

// get отдаёт номер последнего релиза, спрашивая GitHub не чаще releaseEvery.
// Ошибка — строкой для продавца: панель в России может не видеть GitHub, и
// это надо сказать, а не молча показать пустоту.
func (c *releaseCache) get(ctx context.Context) (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.checked.IsZero() && time.Since(c.checked) < releaseEvery {
		return c.latest, c.err
	}
	latest, err := fetchLatest(ctx)
	c.checked = time.Now()
	if err != nil {
		// Прежний известный номер не выбрасываем: он лучше, чем ничего.
		c.err = err.Error()
		return c.latest, c.err
	}
	c.latest, c.err = latest, ""
	return c.latest, ""
}

func fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releaseURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub недоступен: %w", err)
	}
	_ = resp.Body.Close()
	tag := path.Base(resp.Header.Get("Location"))
	if !releaseTag.MatchString(tag) {
		return "", fmt.Errorf("GitHub ответил %s без номера релиза", resp.Status)
	}
	return tag, nil
}

// WithHome сообщает панели её каталог: туда кладётся просьба к службе
// обновления, и его же та сторожит.
func (a *API) WithHome(dir string) *API {
	a.home = dir
	return a
}

// getUpdates — всё для страницы «Обновления»: версия панели, последний
// релиз, состояние службы обновления и ноды с их версиями.
func (a *API) getUpdates(w http.ResponseWriter, r *http.Request) {
	latest, latestErr := a.releases.get(r.Context())
	nodes, err := a.store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	type nodeView struct {
		ID        int64      `json:"id"`
		Name      string     `json:"name"`
		Enabled   bool       `json:"enabled"`
		Version   string     `json:"version"`
		Arch      string     `json:"arch,omitempty"`
		UpgradeTo string     `json:"upgrade_to,omitempty"`
		LastSeen  *time.Time `json:"last_seen,omitempty"`
	}
	views := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		views = append(views, nodeView{n.ID, n.Name, n.Enabled, n.Version, n.Arch, n.UpgradeTo, n.LastSeen})
	}
	ok(w, map[string]any{
		"latest":       latest,
		"latest_error": latestErr,
		"panel": map[string]any{
			"version": a.version,
			"updater": updater.ReadStatus(a.home),
		},
		"nodes": views,
	})
}

// upgradePanel кладёт просьбу обновить машину панели.
//
// Служба ставит последний релиз целиком: панель, ноду, если она стоит тут
// же, и приложения для раздачи. Панель на время подмены перезапускается —
// страница это переживает и ждёт.
func (a *API) upgradePanel(w http.ResponseWriter, r *http.Request) {
	if !updater.ReadStatus(a.home).Installed {
		fail(w, http.StatusConflict, updater.ErrNotInstalled.Error())
		return
	}
	if err := updater.Request(a.home); err != nil {
		fail(w, http.StatusInternalServerError, fmt.Sprintf("просьба не легла: %v", err))
		return
	}
	a.record(r, EventPanelUpgrade, Event{Detail: "до последнего релиза"})
	w.WriteHeader(http.StatusAccepted)
}

// upgradeNodes просит обновиться все включённые ноды, отставшие от
// последнего релиза. Ноды узнают об этом на следующей сверке — в течение
// четверти минуты — и передают просьбу своим службам обновления.
func (a *API) upgradeNodes(w http.ResponseWriter, r *http.Request) {
	latest, latestErr := a.releases.get(r.Context())
	if latest == "" {
		msg := "не удалось узнать последний релиз"
		if latestErr != "" {
			msg += ": " + latestErr
		}
		fail(w, http.StatusConflict, msg)
		return
	}
	n, err := a.store.RequestNodeUpgrade(r.Context(), latest)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.record(r, EventNodesUpgrade, Event{Detail: fmt.Sprintf("до %s: %d", latest, n)})
	ok(w, map[string]any{"target": latest, "asked": n})
}

// clip обрезает строку от ноды: версия и разрядность приходят из сети и в
// базу длиннее разумного не ложатся.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
