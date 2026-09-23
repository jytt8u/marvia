package mobile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
	"github.com/jytt8u/marvia/internal/vp1"
)

// SubscriptionView — подписка глазами экрана «Серверы»: ноды, срок, остаток.
//
// Экран показывает серверы и до подключения — это список того, что человеку
// продали, а не того, что сейчас работает. Поэтому подписка читается отдельно
// от туннеля, и по возможности из кэша: каждый поход в панель — это запрос
// имени её домена, а он уходит провайдеру открытым текстом.
type SubscriptionView struct {
	Nodes []NodeView `json:"nodes"`

	// Until — до какого числа оплачено, «2026-09-27»; пусто — без срока.
	Until string `json:"until,omitempty"`
	// Limit и Left — байты; ноль в Limit — без ограничения.
	Limit int64 `json:"limit"`
	Left  int64 `json:"left"`

	// FetchedAt — когда список получен из панели, unix-секунды. Экран пишет
	// по нему «обновлено 1 ч назад», а не выдумывает свежесть.
	FetchedAt int64 `json:"fetched_at"`
	// Stale — список из протухшего кэша: панель не ответила, показываем
	// вчерашний. Человеку это надо видеть, а не догадываться.
	Stale bool `json:"stale"`
}

// Subscription отдаёт подписку по ссылке доступа: из свежего кэша, иначе из
// панели; refresh — идти в панель, не глядя на кэш. Если панель молчит, а
// кэш есть, отдаётся кэш с пометкой Stale. Ошибка — только когда показать
// нечего совсем.
func Subscription(accountLink, cacheDir string, refresh bool) (string, error) {
	if isForeign(accountLink) {
		return foreignView(accountLink, cacheDir, refresh)
	}
	account, err := client.ParseAccountLink(accountLink)
	if err != nil {
		return "", fail(FailAccount, fmt.Errorf("ссылка доступа: %w", err))
	}
	path := accountCachePath(cacheDir, account.SubscriptionURL)

	var cached client.CachedSubscription
	if path != "" {
		cached, err = client.LoadCache(path, account.SubscriptionURL)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			cached = client.CachedSubscription{}
		}
	}
	if !refresh && cached.Fresh() {
		return viewJSON(cached.Subscription, cached.FetchedAt, false)
	}

	ctx, cancel := context.WithTimeout(context.Background(), subscriptionTimeout)
	defer cancel()
	sub, err := client.FetchSubscription(ctx, account.SubscriptionURL, account.PanelIPs)
	if err != nil {
		if len(cached.Nodes()) > 0 {
			return viewJSON(cached.Subscription, cached.FetchedAt, true)
		}
		return "", fail(FailPanel, err)
	}
	if path != "" {
		// Не сохранился кэш — не беда: в следующий раз сходим в панель снова.
		_ = client.SaveCache(path, account.SubscriptionURL, sub)
	}
	return viewJSON(sub, time.Now(), false)
}

// MeasureNodes меряет ноды подписки без туннеля, настоящим подключением к
// каждой, и отдаёт тот же список с временами. Занимает секунды.
//
// Когда туннель поднят, пользоваться этим не надо: замер уйдёт через сам
// туннель и покажет не то. Для поднятого есть Tunnel.Measure.
//
// settings — те же настройки, что уходят в Start: нода, до которой доходит
// только разрезанное приветствие, без дробления показалась бы мёртвой.
func MeasureNodes(accountLink, cacheDir, settings string) (string, error) {
	set := parseSettings(settings)
	if isForeign(accountLink) {
		return foreignMeasure(accountLink, cacheDir, set)
	}
	account, err := client.ParseAccountLink(accountLink)
	if err != nil {
		return "", fail(FailAccount, fmt.Errorf("ссылка доступа: %w", err))
	}
	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return "", fail(FailAccount, fmt.Errorf("личный ключ: %w", err))
	}

	raw, err := Subscription(accountLink, cacheDir, false)
	if err != nil {
		return "", err
	}
	var view SubscriptionView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		return "", err
	}

	// Ноды с адресами есть только в кэше: наружу они не отдаются, в списке
	// экрана им делать нечего.
	cached, err := client.LoadCache(accountCachePath(cacheDir, account.SubscriptionURL), account.SubscriptionURL)
	if err != nil {
		return "", fail(FailPanel, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), measureTimeout)
	defer cancel()
	byID := make(map[int64]client.Measurement)
	for _, m := range client.MeasureAll(ctx, cached.Nodes(), key, set.dial()) {
		byID[m.Node.ID] = m
	}
	for i := range view.Nodes {
		m, ok := byID[view.Nodes[i].ID]
		if !ok {
			continue
		}
		view.Nodes[i].Alive = m.OK()
		view.Nodes[i].MS = m.PingMS()
		if m.OK() {
			view.Nodes[i].SetupMS = max(1, m.Latency.Milliseconds())
		}
	}
	out, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func viewJSON(sub client.Subscription, fetched time.Time, stale bool) (string, error) {
	view := SubscriptionView{
		Nodes:     make([]NodeView, 0, len(sub.Nodes)),
		Limit:     sub.TrafficLimit,
		Left:      sub.Remaining(),
		FetchedAt: fetched.Unix(),
		Stale:     stale,
	}
	if until, set := sub.Until(); set {
		view.Until = until.Local().Format("2006-01-02")
	}
	for _, n := range sub.Nodes {
		view.Nodes = append(view.Nodes, NodeView{ID: n.ID, Name: n.Name, Country: n.Country})
	}
	out, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// accountCachePath — свой файл кэша на каждую подписку.
//
// Доступ у человека бывает от двух продавцов сразу, а общий файл помнил бы
// только последнего: LoadCache сверяет отпечаток ссылки и чужой кэш
// отбрасывает. Имя — от отпечатка адреса подписки, чтобы токен не лежал в
// имени файла.
func accountCachePath(dir, subURL string) string {
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(subURL))
	return filepath.Join(dir, "subscription-"+hex.EncodeToString(sum[:4])+".json")
}

// ForgetSubscription стирает кэш подписки: человек удалил ключ, и список
// его нод на телефоне оставаться не должен.
func ForgetSubscription(accountLink, cacheDir string) {
	if isForeign(accountLink) {
		if path := foreign.CachePath(cacheDir, accountLink); path != "" {
			_ = os.Remove(path)
		}
		return
	}
	account, err := client.ParseAccountLink(accountLink)
	if err != nil {
		return
	}
	if path := accountCachePath(cacheDir, account.SubscriptionURL); path != "" {
		_ = os.Remove(path)
	}
}

const subscriptionTimeout = 20 * time.Second

// BypassRoutes забирает у панели российские подсети — по строке на подсеть.
//
// Идёт тем же путём, что подписка: по адресам панели из ссылки, если они там
// есть. Ошибка — с понятной причиной, её приложение покажет под
// переключателем, а не «панель была недоступна» на всё подряд.
func BypassRoutes(accountLink string) (string, error) {
	if isForeign(accountLink) {
		// Список отдаёт панель Marvia; у чужой подписки его взять неоткуда.
		// Приложения мимо туннеля по-прежнему работают — там список не нужен.
		return "", fail(FailPanel, errors.New("этот список даёт только панель Marvia, а подписка чужая"))
	}
	account, err := client.ParseAccountLink(accountLink)
	if err != nil {
		return "", fail(FailAccount, fmt.Errorf("ссылка доступа: %w", err))
	}
	prefixes, err := client.FetchBypass(context.Background(), account.SubscriptionURL, account.PanelIPs)
	if err != nil {
		return "", fail(FailPanel, err)
	}
	return strings.Join(prefixes, "\n"), nil
}
