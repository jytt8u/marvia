package panel_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
)

// Оповещения проверяем без телеграма: подменяем отправку и смотрим, что и
// когда панель говорит. Настоящий телеграм проверял бы телеграм.

func alertStore(t *testing.T) *panel.Store {
	t.Helper()
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	err = store.SetAlertSettings(context.Background(), panel.AlertSettings{
		Enabled: true, BotToken: "тестовый-токен", ChatID: "42",
	})
	if err != nil {
		t.Fatalf("настройки оповещений: %v", err)
	}
	return store
}

// sent собирает отправленное вместо телеграма.
type sent struct{ texts []string }

func (s *sent) capture(a *panel.Alerts) {
	a.SendWith(func(_ context.Context, _ panel.AlertSettings, text string) error {
		s.texts = append(s.texts, text)
		return nil
	})
}

func (s *sent) last() string {
	if len(s.texts) == 0 {
		return ""
	}
	return s.texts[len(s.texts)-1]
}

// TestSilentNodeIsReportedOnceAndThenItsReturn — про замолчавшую ноду говорим
// один раз, а не каждые пять минут.
//
// Оповещение, которое повторяется, перестают читать — и вместе с ним
// перестают читать те, что важны.
func TestSilentNodeIsReportedOnceAndThenItsReturn(t *testing.T) {
	store := alertStore(t)
	ctx := context.Background()

	node, _, err := store.CreateNode(ctx, panel.CreateNodeParams{Name: "Хельсинки", Address: "1.2.3.4:443"})
	if err != nil {
		t.Fatalf("нода: %v", err)
	}

	var box sent
	alerts := panel.NewAlerts(store)
	box.capture(alerts)

	// Нода не отмечалась никогда — значит молчит.
	alerts.Check(ctx)
	if len(box.texts) != 1 {
		t.Fatalf("сообщений %d, ожидалось одно: %v", len(box.texts), box.texts)
	}
	if !strings.Contains(box.last(), "Хельсинки") || !strings.Contains(box.last(), "молчит") {
		t.Fatalf("непонятное сообщение: %q", box.last())
	}

	// Повторные проверки молчат.
	alerts.Check(ctx)
	alerts.Check(ctx)
	if len(box.texts) != 1 {
		t.Fatalf("про одну и ту же беду сказано %d раз", len(box.texts))
	}

	// Нода ожила — об этом сказать надо.
	if err := store.TouchNode(ctx, node.ID); err != nil {
		t.Fatalf("отметка ноды: %v", err)
	}
	alerts.Check(ctx)
	if len(box.texts) != 2 {
		t.Fatalf("о возвращении ноды не сказано: %v", box.texts)
	}
	if !strings.Contains(box.last(), "снова на связи") {
		t.Fatalf("непонятное сообщение о возвращении: %q", box.last())
	}
}

// Выключенная продавцом нода молчащей не считается: он сам её выключил.
func TestDisabledNodeIsNotReportedSilent(t *testing.T) {
	store := alertStore(t)
	ctx := context.Background()

	node, _, err := store.CreateNode(ctx, panel.CreateNodeParams{Name: "выключенная", Address: "1.2.3.4:443"})
	if err != nil {
		t.Fatalf("нода: %v", err)
	}
	off := false
	if _, err := store.UpdateNode(ctx, node.ID, panel.UpdateNodeParams{Enabled: &off}); err != nil {
		t.Fatalf("выключение: %v", err)
	}

	var box sent
	alerts := panel.NewAlerts(store)
	box.capture(alerts)

	alerts.Check(ctx)
	if len(box.texts) != 0 {
		t.Fatalf("про выключенную ноду сказано: %v", box.texts)
	}
}

// TestExpiringAlertCountsButDoesNotName — в оповещении число, а не имена.
//
// Переписка с ботом живёт в телеграме и переживёт и панель, и продавца.
// Превращать её в список клиентов нельзя: продавцу хватает числа, а кто
// именно — видно в панели.
func TestExpiringAlertCountsButDoesNotName(t *testing.T) {
	store := alertStore(t)
	ctx := context.Background()

	soon := time.Now().UTC().Add(24 * time.Hour)
	for _, label := range []string{"Артём", "Нина"} {
		if _, _, err := store.CreateUser(ctx, panel.CreateUserParams{
			Label: label, ExpiresAt: panel.ExpiryAt(soon),
		}); err != nil {
			t.Fatalf("покупатель %s: %v", label, err)
		}
	}

	var box sent
	alerts := panel.NewAlerts(store)
	box.capture(alerts)

	alerts.Check(ctx)
	if len(box.texts) != 1 {
		t.Fatalf("сообщений %d, ожидалось одно: %v", len(box.texts), box.texts)
	}
	if !strings.Contains(box.last(), "2") {
		t.Fatalf("числа в сообщении нет: %q", box.last())
	}
	for _, name := range []string{"Артём", "Нина"} {
		if strings.Contains(box.last(), name) {
			t.Fatalf("имя покупателя уехало в телеграм: %q", box.last())
		}
	}
}

// Пока оповещения не включены, панель наружу не ходит вовсе.
func TestNothingIsSentUntilTurnedOn(t *testing.T) {
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if _, _, err := store.CreateNode(ctx, panel.CreateNodeParams{Name: "тихая", Address: "1.2.3.4:443"}); err != nil {
		t.Fatalf("нода: %v", err)
	}

	var box sent
	alerts := panel.NewAlerts(store)
	box.capture(alerts)

	alerts.Check(ctx)
	if len(box.texts) != 0 {
		t.Fatalf("панель пошла наружу с выключенными оповещениями: %v", box.texts)
	}

	// Настройки без токена — тоже выключено.
	if err := store.SetAlertSettings(ctx, panel.AlertSettings{Enabled: true, ChatID: "42"}); err != nil {
		t.Fatalf("настройки: %v", err)
	}
	alerts.Check(ctx)
	if len(box.texts) != 0 {
		t.Fatalf("панель пошла наружу без токена бота: %v", box.texts)
	}
}

// Про несделанную копию базы говорим один раз и сообщаем, когда починилось.
func TestBackupFailureIsReportedOnce(t *testing.T) {
	store := alertStore(t)
	ctx := context.Background()

	var box sent
	alerts := panel.NewAlerts(store)
	box.capture(alerts)

	alerts.BackupFailed(ctx, errors.New("нет места на диске"))
	alerts.BackupFailed(ctx, errors.New("нет места на диске"))
	if len(box.texts) != 1 {
		t.Fatalf("про копию сказано %d раз: %v", len(box.texts), box.texts)
	}
	if !strings.Contains(box.last(), "нет места") {
		t.Fatalf("причина потерялась: %q", box.last())
	}

	alerts.BackupFailed(ctx, nil)
	if len(box.texts) != 2 || !strings.Contains(box.last(), "снова") {
		t.Fatalf("о починке не сказано: %v", box.texts)
	}
}

// Токен бота наружу не отдаётся: в ответе API вместо него признак «задан».
func TestBotTokenIsNotHandedOut(t *testing.T) {
	h := newHarness(t)

	code := h.do("PUT", "/api/v1/alerts", adminToken, map[string]any{
		"enabled": true, "bot_token": "секрет-бота", "chat_id": "42",
	}, nil)
	if code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("сохранение настроек: код %d", code)
	}

	var out map[string]any
	if code := h.do(http.MethodGet, "/api/v1/alerts", adminToken, nil, &out); code != http.StatusOK {
		t.Fatalf("чтение настроек: код %d", code)
	}
	if _, leaked := out["bot_token"]; leaked {
		t.Fatalf("токен бота отдан наружу: %v", out)
	}
	if out["has_token"] != true {
		t.Fatalf("признак «токен задан» не пришёл: %v", out)
	}
}
