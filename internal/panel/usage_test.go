package panel_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/panel"
	"github.com/veilproject/veil/internal/users"
)

// История расхода — то, из чего рисуется вся статистика.
//
// Нода присылает накопительный итог, а не прирост, поэтому вся арифметика тут
// на разницах между отчётами. Ошибка в ней не падает и не пишет в журнал: она
// просто рисует неправильный график, и продавец принимает по нему решения.

// usageStore поднимает базу с одним покупателем и одной нодой.
func usageStore(t *testing.T) (*panel.Store, int64, int64) {
	t.Helper()

	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	user, _, err := store.CreateUser(ctx, panel.CreateUserParams{Label: "покупатель"})
	if err != nil {
		t.Fatalf("покупатель: %v", err)
	}

	node, _, err := store.CreateNode(ctx, panel.CreateNodeParams{
		Name: "ae-1", Country: "ОАЭ", Address: "1.2.3.4:443", PublicKey: "",
	})
	if err != nil {
		t.Fatalf("нода: %v", err)
	}

	return store, user.ID, node.ID
}

// report отправляет накопительный итог, как это делает нода.
func report(t *testing.T, store *panel.Store, nodeID, userID, up, down int64) {
	t.Helper()

	err := store.ReportUsage(context.Background(), nodeID, map[string]users.Usage{
		strconv.FormatInt(userID, 10): {Up: up, Down: down},
	})
	if err != nil {
		t.Fatalf("отчёт ноды: %v", err)
	}
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

// TestDailyUsageCountsDeltas — за сутки копится прирост, а не последний итог.
func TestDailyUsageCountsDeltas(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()

	report(t, store, nodeID, userID, 100, 200)
	report(t, store, nodeID, userID, 150, 260)
	report(t, store, nodeID, userID, 150, 300)

	days, err := store.UsageByDay(ctx, 7)
	if err != nil {
		t.Fatalf("расход по суткам: %v", err)
	}

	last := days[len(days)-1]
	if last.Day != today() {
		t.Fatalf("последний день не сегодняшний: %s", last.Day)
	}
	// Итог ноды — 150 и 300, значит за сутки столько же и прошло.
	if last.Up != 150 || last.Down != 300 {
		t.Fatalf("прирост посчитан неверно: вверх %d, вниз %d", last.Up, last.Down)
	}
}

// TestNodeRestartDoesNotEatTheDay — перезапуск ноды не уводит сутки в минус.
//
// После перезапуска нода начинает счёт заново, и «новое минус старое» даёт
// отрицательное число. Без защиты график провалился бы вниз, а суммарный
// расход за месяц уменьшился бы сам собой.
func TestNodeRestartDoesNotEatTheDay(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()

	report(t, store, nodeID, userID, 500, 500)
	// Нода перезапустилась: счётчик начался с нуля и дошёл до 40.
	report(t, store, nodeID, userID, 40, 10)

	days, err := store.UsageByDay(ctx, 2)
	if err != nil {
		t.Fatalf("расход по суткам: %v", err)
	}

	last := days[len(days)-1]
	if last.Up < 0 || last.Down < 0 {
		t.Fatalf("сутки ушли в минус: вверх %d, вниз %d", last.Up, last.Down)
	}
	if last.Up != 540 || last.Down != 510 {
		t.Fatalf("после перезапуска прирост посчитан неверно: вверх %d, вниз %d", last.Up, last.Down)
	}
}

// TestEmptyDaysStayInPlace — дни без трафика остаются нулями на своих местах.
//
// Пропустить их — значит сдвинуть соседние столбики: провал в графике это тоже
// ответ, и рисовать его надо там, где он был.
func TestEmptyDaysStayInPlace(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()

	report(t, store, nodeID, userID, 10, 10)

	days, err := store.UsageByDay(ctx, 5)
	if err != nil {
		t.Fatalf("расход по суткам: %v", err)
	}

	if len(days) != 5 {
		t.Fatalf("дней должно быть 5, пришло %d", len(days))
	}
	for i, d := range days[:4] {
		if d.Up != 0 || d.Down != 0 {
			t.Errorf("день %d (%s) не пустой: %d/%d", i, d.Day, d.Up, d.Down)
		}
	}
	if days[4].Up != 10 {
		t.Errorf("сегодняшний день пустой, хотя трафик был")
	}
}

// TestUsageByNodeNamesNodes — разбивка по нодам приходит с именем и страной.
func TestUsageByNodeNamesNodes(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()

	report(t, store, nodeID, userID, 1000, 2000)

	byNode, err := store.UsageByNode(ctx, 30)
	if err != nil {
		t.Fatalf("расход по нодам: %v", err)
	}
	if len(byNode) != 1 {
		t.Fatalf("нод в разбивке %d, ожидалась одна", len(byNode))
	}
	if byNode[0].Name != "ae-1" || byNode[0].Country != "ОАЭ" {
		t.Errorf("нода без имени или страны: %+v", byNode[0])
	}
	if byNode[0].Up+byNode[0].Down != 3000 {
		t.Errorf("расход по ноде посчитан неверно: %+v", byNode[0])
	}
}

// TestDeletedUserLeavesNoHistory — удаление покупателя уносит и его историю.
//
// Иначе панель хранила бы расход людей, которых давно нет, и обещание «удалили
// — значит удалили» оказалось бы неправдой.
func TestDeletedUserLeavesNoHistory(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()

	report(t, store, nodeID, userID, 100, 100)
	if err := store.DeleteUser(ctx, userID); err != nil {
		t.Fatalf("удаление: %v", err)
	}

	days, err := store.UsageByDay(ctx, 2)
	if err != nil {
		t.Fatalf("расход по суткам: %v", err)
	}
	for _, d := range days {
		if d.Up != 0 || d.Down != 0 {
			t.Fatalf("история удалённого покупателя осталась: %+v", d)
		}
	}
}
