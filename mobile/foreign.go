package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
)

// Чужие подписки и ссылки: VLESS, VMess, Trojan, Shadowsocks, Hysteria2,
// WireGuard. Приложению они приходят той же строкой, что и ключ Marvia, —
// «+» на экране «Серверы» и первый экран принимают любую, — а здесь ядро
// решает, каким путём её вести. Подробности про протоколы — internal/foreign.

func isForeign(link string) bool { return foreign.IsForeign(link) }

func firstLine(s string) string { return foreign.FirstLine(s) }

func foreignSubscription(link, cacheDir string, refresh bool) (foreign.Subscription, time.Time, bool, error) {
	return foreign.Load(link, foreign.CachePath(cacheDir, link), refresh)
}

// connectForeign поднимает подключение к чужой подписке.
//
// Адреса нод здесь не закрепляются, в отличие от окна на компьютере:
// приложение исключено из собственного туннеля, и его запросы имён идут
// обычной сетью телефона — петли нет.
func connectForeign(link, cacheDir string, prefer int64, events client.Events, set tunnelSettings) (client.Backend, error) {
	sub, _, _, err := foreignSubscription(link, cacheDir, false)
	if err != nil {
		// Ссылка ноды не разобралась — это ключ, а не панель: идти с этим
		// надо к тому, кто её дал, а не проверять интернет.
		if errors.Is(err, foreign.ErrUnsupported) || foreign.IsLink(firstLine(link)) {
			return nil, fail(FailAccount, err)
		}
		return nil, fail(FailPanel, err)
	}
	if !sub.Expire.IsZero() && time.Now().After(sub.Expire) {
		return nil, fail(FailExpired, errors.New("подписка кончилась"))
	}
	if sub.Total > 0 && sub.Remaining() == 0 {
		return nil, fail(FailQuota, errors.New("трафик подписки исчерпан"))
	}
	s, _, err := foreign.Supervise(context.Background(), sub, prefer, events, set.foreign())
	if err != nil {
		return nil, fail(FailNodes, err)
	}
	return s, nil
}

// foreignView — чужая подписка для экрана «Серверы».
func foreignView(link, cacheDir string, refresh bool) (string, error) {
	sub, fetched, stale, err := foreignSubscription(link, cacheDir, refresh)
	if err != nil {
		return "", fail(FailPanel, err)
	}
	return viewJSON(foreign.View(sub), fetched, stale)
}

// foreignMeasure меряет чужие ноды без туннеля.
func foreignMeasure(link, cacheDir string, set tunnelSettings) (string, error) {
	sub, fetched, stale, err := foreignSubscription(link, cacheDir, false)
	if err != nil {
		return "", fail(FailPanel, err)
	}
	raw, err := viewJSON(foreign.View(sub), fetched, stale)
	if err != nil {
		return "", err
	}
	var view SubscriptionView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), measureTimeout)
	defer cancel()
	byID := map[int64]client.Measurement{}
	for _, m := range foreign.MeasureAll(ctx, sub.Links, set.foreign()) {
		byID[m.Node.ID] = m
	}
	for i := range view.Nodes {
		if m, ok := byID[view.Nodes[i].ID]; ok {
			view.Nodes[i].Alive = m.OK()
			view.Nodes[i].MS = m.PingMS()
			if m.OK() {
				view.Nodes[i].SetupMS = max(1, m.Latency.Milliseconds())
			}
		}
	}
	out, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
