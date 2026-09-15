package panel_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/users"
)

// «Он вообще подключался?» — первый вопрос при «не работает». Панель отвечает
// на него двумя числами и временем, без адресов и без истории: нода и так
// держит их ради лимитов, панель лишь показывает.

func presence(t *testing.T, store *panel.Store, nodeID, userID int64, p users.Presence) {
	t.Helper()
	m := map[string]users.Presence{}
	if p.Conns > 0 || p.IPs > 0 {
		m[strconv.FormatInt(userID, 10)] = p
	}
	if err := store.ReportPresence(context.Background(), nodeID, m); err != nil {
		t.Fatalf("отчёт о присутствии: %v", err)
	}
}

// TestPanelKnowsWhoIsOnlineRightNow — подключился, и панель это показывает.
func TestPanelKnowsWhoIsOnlineRightNow(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()
	if err := store.TouchNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}

	presence(t, store, nodeID, userID, users.Presence{Conns: 2, IPs: 1})

	u, err := store.GetUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Online != 2 || u.Devices != 1 {
		t.Fatalf("на связи %d соединений и %d устройств, ждали 2 и 1", u.Online, u.Devices)
	}
	if u.LastSeen == nil || time.Since(*u.LastSeen) > time.Minute {
		t.Fatalf("время последней связи не проставлено: %v", u.LastSeen)
	}

	// Отключился — нода прислала отчёт без него. Числа обнулились, а «был
	// на связи» остался: это ответ на вопрос «когда он был в последний раз».
	presence(t, store, nodeID, userID, users.Presence{})
	u, err = store.GetUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Online != 0 || u.Devices != 0 {
		t.Fatalf("после ухода на связи %d/%d, ждали нули", u.Online, u.Devices)
	}
	if u.LastSeen == nil {
		t.Fatal("после ухода панель забыла, что он вообще был")
	}
}

// TestSilentNodeCountsNobodyOnline — нода замолчала, и её «на связи» не в счёт.
//
// Иначе продавец видел бы «2 устройства на связи» у покупателя, чья нода
// выключена третий час: последний отчёт застыл бы в базе навсегда.
func TestSilentNodeCountsNobodyOnline(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()
	if err := store.TouchNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	presence(t, store, nodeID, userID, users.Presence{Conns: 1})

	// Нода не выходит на связь дольше, чем панель готова ждать.
	if err := store.TouchNodeAt(ctx, nodeID, time.Now().Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	u, err := store.GetUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Online != 0 {
		t.Fatalf("молчащая нода всё ещё держит %d на связи", u.Online)
	}
	if u.LastSeen == nil {
		t.Fatal("время последней связи пропало вместе с нодой")
	}
}

// TestPresenceIsNotAJournal — панель не помнит ничего, кроме текущего среза.
//
// Один срез на пару «покупатель — нода», без строк за прошлые отчёты. Это
// проверка того, что «устройства» не выросли в журнал подключений.
func TestPresenceIsNotAJournal(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()
	if err := store.TouchNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		presence(t, store, nodeID, userID, users.Presence{Conns: i + 1})
	}
	if n := store.PresenceRows(ctx); n != 1 {
		t.Fatalf("после пяти отчётов строк присутствия %d, ждали одну", n)
	}
}

// TestTrafficCountsAsBeingSeen — прошедший трафик отмечает «был на связи».
//
// Нода отчитывается раз в пятнадцать секунд, и сессия короче тика в отчёт о
// присутствии не попадает вовсе. Панель тогда отвечала «не подключался ни
// разу» про человека, у которого расход уже записан, — и на вопрос «он вообще
// подключался?» врала при живом трафике. Поймано на живой установке.
func TestTrafficCountsAsBeingSeen(t *testing.T) {
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()
	if err := store.TouchNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}

	// Отчёта о присутствии не было вовсе — только расход.
	report(t, store, nodeID, userID, 12_000, 8_426)

	u, err := store.GetUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Used == 0 {
		t.Fatal("расход не записался — проверять нечего")
	}
	if u.LastSeen == nil {
		t.Fatal("расход есть, а «был на связи» пусто: панель скажет «не подключался ни разу»")
	}
	if time.Since(*u.LastSeen) > time.Minute {
		t.Fatalf("время последней связи не свежее: %v", u.LastSeen)
	}

	// Но «на связи прямо сейчас» из трафика не следует: он мог уже уйти.
	if u.Online != 0 {
		t.Errorf("по одному расходу насчитали %d соединений", u.Online)
	}
}
