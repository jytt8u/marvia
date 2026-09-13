package panel_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

// TestUpgradeFromOlderSchema — обновление панели не должно ломать базу.
//
// Проверка появилась после того, как обновление её сломало по-настоящему.
// Индекс по новому столбцу стоял в схеме, а схема выполняется до миграции: на
// уже существующей базе CREATE TABLE IF NOT EXISTS столбца не добавляет, и
// индекс по нему падал раньше, чем миграция успевала этот столбец завести.
// Панель не поднималась вовсе — причём не у меня на чистой базе, где всё
// работало, а у каждого продавца при обновлении.
//
// Обычные тесты этого поймать не могли: они всегда начинают с пустого файла.
// Поэтому здесь база создаётся руками в том виде, в каком её оставила
// предыдущая версия.
func TestUpgradeFromOlderSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("создание старой базы: %v", err)
	}

	old := []string{
		`CREATE TABLE users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			label         TEXT    NOT NULL DEFAULT '',
			enabled       INTEGER NOT NULL DEFAULT 1,
			expires_at    TEXT,
			traffic_limit INTEGER NOT NULL DEFAULT 0,
			max_ips       INTEGER NOT NULL DEFAULT 0,
			max_conns     INTEGER NOT NULL DEFAULT 0,
			sub_token     TEXT    NOT NULL UNIQUE,
			created_at    TEXT    NOT NULL)`,
		`CREATE TABLE nodes (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT    NOT NULL,
			address    TEXT    NOT NULL,
			sni        TEXT    NOT NULL DEFAULT '',
			public_key TEXT    NOT NULL,
			token_hash TEXT    NOT NULL UNIQUE,
			enabled    INTEGER NOT NULL DEFAULT 1,
			last_seen  TEXT,
			created_at TEXT    NOT NULL)`,
		`INSERT INTO users (label, sub_token, created_at) VALUES ('старый покупатель', 'токен-из-прошлой-версии', '2026-01-01T00:00:00Z')`,
		`INSERT INTO nodes (name, address, public_key, token_hash, created_at)
			VALUES ('vm-4823917', '1.2.3.4:443', '', 'хеш-токена-старой-ноды', '2026-01-01T00:00:00Z')`,
	}
	for _, step := range old {
		if _, err := db.Exec(step); err != nil {
			t.Fatalf("подготовка старой базы: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("закрытие старой базы: %v", err)
	}

	store, err := panel.Open(path)
	if err != nil {
		t.Fatalf("панель не открыла базу прошлой версии: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Покупатель из старой базы на месте: обновление не должно его потерять.
	list, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("чтение подписчиков после обновления: %v", err)
	}
	if len(list) != 1 || list[0].Label != "старый покупатель" {
		t.Fatalf("после обновления подписчики не те: %+v", list)
	}

	// И новое поле работает — значит миграция действительно прошла, а не
	// просто не упала.
	if _, _, err := store.CreateUser(ctx, panel.CreateUserParams{
		Label:      "новый",
		ExternalID: "tg:1",
	}); err != nil {
		t.Fatalf("создание подписчика с ключом продавца: %v", err)
	}
	found, err := store.UserByExternalID(ctx, "tg:1")
	if err != nil {
		t.Fatalf("поиск по ключу продавца: %v", err)
	}
	if found.Label != "новый" {
		t.Errorf("нашёлся не тот подписчик: %+v", found)
	}

	// Нода из старой базы тоже на месте, и у неё появилась страна — пустая,
	// пока продавец её не написал.
	nodes, err := store.ListNodes(ctx)
	if err != nil {
		t.Fatalf("чтение нод после обновления: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "vm-4823917" {
		t.Fatalf("после обновления ноды не те: %+v", nodes)
	}
	if nodes[0].Country != "" {
		t.Errorf("страна взялась из ниоткуда: %q", nodes[0].Country)
	}

	country := "Нидерланды"
	updated, err := store.UpdateNode(ctx, nodes[0].ID, panel.UpdateNodeParams{Country: &country})
	if err != nil {
		t.Fatalf("страна не проставилась на ноде из старой базы: %v", err)
	}
	if updated.Country != country {
		t.Errorf("страна не сохранилась: %+v", updated)
	}
}

// TestUpgradeForgetsWhoWentWhere — обновление панели стирает накопленную
// посуточную историю «кто на какой ноде сидел».
//
// Перестать её писать мало: у продавца, который обновляется, она уже лежит за
// последний месяц. Обновление обязано свернуть её по дням и нодам — цифры на
// графиках от этого не меняются, а людей в таблице не остаётся.
func TestUpgradeForgetsWhoWentWhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// База прежней формы: в посуточной истории есть покупатель.
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("старая база: %v", err)
	}
	mustExec(t, old, `CREATE TABLE nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL DEFAULT '', address TEXT NOT NULL DEFAULT '', public_key TEXT NOT NULL DEFAULT '', token_hash TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '')`)
	mustExec(t, old, `INSERT INTO nodes (id) VALUES (7)`)
	mustExec(t, old, `CREATE TABLE usage_daily (
		day TEXT NOT NULL, user_id INTEGER NOT NULL, node_id INTEGER NOT NULL,
		up INTEGER NOT NULL DEFAULT 0, down INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (day, user_id, node_id))`)
	// Два покупателя на одной ноде в один день: после свёртки должна остаться
	// одна строка с их суммой.
	mustExec(t, old, `INSERT INTO usage_daily VALUES ('2026-09-01', 1, 7, 100, 200)`)
	mustExec(t, old, `INSERT INTO usage_daily VALUES ('2026-09-01', 2, 7, 300, 400)`)
	mustExec(t, old, `INSERT INTO usage_daily VALUES ('2026-09-02', 1, 7, 10, 20)`)
	if err := old.Close(); err != nil {
		t.Fatalf("закрытие старой базы: %v", err)
	}

	store, err := panel.Open(path)
	if err != nil {
		t.Fatalf("обновление: %v", err)
	}
	defer store.Close()

	columns, err := store.DailyHistoryColumns()
	if err != nil {
		t.Fatalf("столбцы: %v", err)
	}
	for _, name := range columns {
		if name == "user_id" {
			t.Fatalf("покупатель остался в истории после обновления: %v", columns)
		}
	}

	// Цифры сохранились: 100+300 вверх и 200+400 вниз за первое сентября.
	days, err := store.UsageByDay(context.Background(), 400)
	if err != nil {
		t.Fatalf("расход по суткам: %v", err)
	}
	var up, down int64
	for _, d := range days {
		up += d.Up
		down += d.Down
	}
	if up != 410 || down != 620 {
		t.Fatalf("после свёртки расход стал %d/%d вместо 410/620", up, down)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("подготовка старой базы (%s): %v", query, err)
	}
}

// TestUpgradeSurvivesOrphanHistory — обновление не падает на строке исчезнувшей
// ноды.
//
// По уму таких строк быть не может: удаление ноды уносит их каскадом. Но если
// одна всё же нашлась, панель обязана подняться: продавец, у которого после
// обновления не стартует панель, теряет сервис целиком, и чинить это ему нечем.
func TestUpgradeSurvivesOrphanHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orphan.db")

	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("старая база: %v", err)
	}
	mustExec(t, old, `CREATE TABLE nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL DEFAULT '', address TEXT NOT NULL DEFAULT '', public_key TEXT NOT NULL DEFAULT '', token_hash TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '')`)
	mustExec(t, old, `INSERT INTO nodes (id) VALUES (7)`)
	mustExec(t, old, `CREATE TABLE usage_daily (
		day TEXT NOT NULL, user_id INTEGER NOT NULL, node_id INTEGER NOT NULL,
		up INTEGER NOT NULL DEFAULT 0, down INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (day, user_id, node_id))`)
	mustExec(t, old, `INSERT INTO usage_daily VALUES ('2026-09-01', 1, 7, 100, 200)`)
	// Нода 99 в базе не значится.
	mustExec(t, old, `INSERT INTO usage_daily VALUES ('2026-09-01', 1, 99, 5, 5)`)
	if err := old.Close(); err != nil {
		t.Fatalf("закрытие старой базы: %v", err)
	}

	store, err := panel.Open(path)
	if err != nil {
		t.Fatalf("панель не поднялась на осиротевшей строке: %v", err)
	}
	defer store.Close()

	days, err := store.UsageByDay(context.Background(), 400)
	if err != nil {
		t.Fatalf("расход по суткам: %v", err)
	}
	var up int64
	for _, d := range days {
		up += d.Up
	}
	if up != 100 {
		t.Fatalf("расход после свёртки %d вместо 100", up)
	}
}
