// Package panel — управляющий слой: пользователи, подписки, ноды, статистика.
//
// Панель живёт отдельно от нод и никогда не пропускает через себя трафик
// пользователей. Нода — расходник, её теряют и блокируют; панель должна
// пережить любое число потерянных нод.
package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/veilproject/veil/internal/users"
	"github.com/veilproject/veil/internal/vp1"

	_ "modernc.org/sqlite" // чистый Go, без cgo: нода и панель кросс-компилируются одной командой
)

// ErrNotFound — запрошенной записи нет.
var ErrNotFound = errors.New("запись не найдена")

// Виды учётных данных.
//
// Один пользователь может иметь несколько наборов: ключ для нашего протокола,
// UUID для VLESS, пароль для Trojan. Так продавец переводит покупателей на
// Veil, не заставляя их менять приложение, — а это самое дорогое в переезде.
const (
	CredVP1    = "vp1"
	CredVLESS  = "vless"
	CredTrojan = "trojan"
)

// User — подписчик в терминах панели.
type User struct {
	ID           int64      `json:"id"`
	Label        string     `json:"label"`
	Enabled      bool       `json:"enabled"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	TrafficLimit int64      `json:"traffic_limit"`
	MaxIPs       int        `json:"max_ips"`
	MaxConns     int        `json:"max_conns"`
	SubToken     string     `json:"sub_token"`

	// ExternalID — ключ покупателя в системе продавца, обычно telegram id.
	//
	// Нужен затем, чтобы бот не держал вторую базу соответствий. Панель
	// считает его уникальным: повторная продажа тому же ключу не заводит
	// второго подписчика, а возвращает существующего. Платёжные системы
	// повторяют уведомление при сбое, и без этого одна оплата давала бы два
	// доступа.
	ExternalID string    `json:"external_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`

	// Used — суммарный расход по всем нодам, заполняется при чтении.
	Used int64 `json:"used"`

	// Credentials — наборы доступа. Секреты здесь публичные (публичный ключ,
	// UUID): приватную часть панель не хранит.
	Credentials []Credential `json:"credentials,omitempty"`
}

// Credential — один набор доступа пользователя.
type Credential struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Secret    string    `json:"secret"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// Node — точка входа.
type Node struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	SNI       string `json:"sni"`
	PublicKey string `json:"public_key"`

	// RealityPublicKey и RealityShortID заполняются, когда нода работает под
	// маскировкой REALITY. По ним собираются ссылки для чужих клиентов:
	// параметры pbk и sid.
	RealityPublicKey string `json:"reality_public_key,omitempty"`
	RealityShortID   string `json:"reality_short_id,omitempty"`

	// WSPath заполняется, когда нода работает за CDN через WebSocket.
	// Тогда в ссылки уходит type=ws, а адрес указывает на CDN, а не на ноду.
	WSPath    string     `json:"ws_path,omitempty"`
	Enabled   bool       `json:"enabled"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Store — хранилище панели поверх SQLite.
type Store struct {
	db *sql.DB
}

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    label         TEXT    NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    expires_at    TEXT,
    traffic_limit INTEGER NOT NULL DEFAULT 0,
    max_ips       INTEGER NOT NULL DEFAULT 0,
    max_conns     INTEGER NOT NULL DEFAULT 0,
    sub_token     TEXT    NOT NULL UNIQUE,
    external_id   TEXT,
    created_at    TEXT    NOT NULL
);



CREATE TABLE IF NOT EXISTS credentials (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL,
    secret     TEXT    NOT NULL,
    label      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL,
    UNIQUE (kind, secret)
);

CREATE INDEX IF NOT EXISTS credentials_user ON credentials(user_id);

CREATE TABLE IF NOT EXISTS nodes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    address    TEXT    NOT NULL,
    sni        TEXT    NOT NULL DEFAULT '',
    public_key TEXT    NOT NULL,
    reality_public_key TEXT NOT NULL DEFAULT '',
    reality_short_id   TEXT NOT NULL DEFAULT '',
    ws_path            TEXT NOT NULL DEFAULT '',
    token_hash TEXT    NOT NULL UNIQUE,
    enabled    INTEGER NOT NULL DEFAULT 1,
    last_seen  TEXT,
    created_at TEXT    NOT NULL
);


CREATE TABLE IF NOT EXISTS node_invites (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT    NOT NULL UNIQUE,
    label      TEXT    NOT NULL DEFAULT '',
    expires_at TEXT    NOT NULL,
    used_at    TEXT,
    node_id    INTEGER REFERENCES nodes(id) ON DELETE SET NULL,
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS node_reports (
    node_id     INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ok          INTEGER NOT NULL,
    latency_ms  INTEGER NOT NULL DEFAULT 0,
    reported_at TEXT    NOT NULL,
    PRIMARY KEY (node_id, user_id)
);

CREATE TABLE IF NOT EXISTS usage (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id    INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    up         INTEGER NOT NULL DEFAULT 0,
    down       INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, node_id)
);

CREATE TABLE IF NOT EXISTS idempotency (
    key        TEXT NOT NULL PRIMARY KEY,
    scope      TEXT NOT NULL,
    response   TEXT NOT NULL,
    created_at TEXT NOT NULL
);
`

// Open открывает или создаёт базу панели.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("открытие базы %s: %w", path, err)
	}
	// SQLite не любит параллельных писателей, а выигрыш от пула здесь нулевой.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("создание схемы: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate доводит уже существующую базу до текущей схемы.
//
// CREATE TABLE IF NOT EXISTS новых столбцов не добавляет, а обновление панели
// не должно требовать от продавца ручных действий с базой. Попытка добавить
// уже существующий столбец — не ошибка, а признак, что миграция отработала
// в прошлый запуск.
func migrate(db *sql.DB) error {
	steps := []string{
		`ALTER TABLE nodes ADD COLUMN reality_public_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE nodes ADD COLUMN reality_short_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE nodes ADD COLUMN ws_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN external_id TEXT`,
		// Индекс живёт только здесь, а не в схеме. Схема выполняется первой, и
		// на уже существующей базе CREATE TABLE IF NOT EXISTS столбца не
		// добавляет — индекс по нему упал бы раньше, чем миграция успела бы
		// этот столбец завести. Панель не поднялась бы вовсе, и не у меня, а у
		// каждого продавца при обновлении.
		`CREATE UNIQUE INDEX IF NOT EXISTS users_external ON users(external_id) WHERE external_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS idempotency (key TEXT NOT NULL PRIMARY KEY, scope TEXT NOT NULL, response TEXT NOT NULL, created_at TEXT NOT NULL)`,
	}

	for _, step := range steps {
		if _, err := db.Exec(step); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("обновление схемы (%s): %w", step, err)
		}
	}
	return nil
}

// Close закрывает базу.
func (s *Store) Close() error { return s.db.Close() }

// CreateUserParams — что нужно, чтобы завести подписчика.
type CreateUserParams struct {
	Label        string  `json:"label"`
	ExpiresAt    *Expiry `json:"expires_at,omitempty"`
	TrafficLimit int64   `json:"traffic_limit"`
	MaxIPs       int     `json:"max_ips"`
	MaxConns     int     `json:"max_conns"`

	// Kinds — какие наборы доступа выдать сразу: vp1, vless, trojan.
	// Пусто означает только vp1.
	//
	// Продавцу, переводящему покупателей с чужой панели, обычно нужны все
	// три: vless и trojan работают в приложениях, которые у людей уже стоят,
	// а vp1 — в нашем клиенте, когда они до него дойдут.
	Kinds []string `json:"kinds,omitempty"`

	// ExternalID — ключ покупателя в системе продавца, обычно telegram id.
	// Задан — повторная продажа тому же ключу вернёт существующего подписчика
	// вместо второго доступа за ту же оплату.
	ExternalID string `json:"external_id,omitempty"`
}

// Issued — выданный набор доступа. Поле Secret показывается ровно один раз.
type Issued struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Secret string `json:"secret"`
}

// CreateUser заводит подписчика и выдаёт ему запрошенные наборы доступа.
func (s *Store) CreateUser(ctx context.Context, p CreateUserParams) (User, []Issued, error) {
	kinds := p.Kinds
	if len(kinds) == 0 {
		kinds = []string{CredVP1}
	}
	for _, kind := range kinds {
		if kind != CredVP1 && kind != CredVLESS && kind != CredTrojan {
			return User{}, nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
		}
	}

	// Повторная продажа тому же ключу ничего не создаёт.
	//
	// Платёжные системы повторяют уведомление, когда бот не ответил, а бот мог
	// упасть ровно между вызовом панели и записью у себя. Без этой проверки
	// одна оплата давала бы два доступа: покупатель получил бы два ключа, а
	// продавец потерял бы месяц выручки и заметил бы это нескоро.
	if p.ExternalID != "" {
		existing, err := s.UserByExternalID(ctx, p.ExternalID)
		switch {
		case err == nil:
			return existing, nil, ErrAlreadyExists
		case !errors.Is(err, ErrNotFound):
			return User{}, nil, err
		}
	}

	subToken, err := NewToken()
	if err != nil {
		return User{}, nil, err
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO users (label, enabled, expires_at, traffic_limit, max_ips, max_conns, sub_token, created_at, external_id)
		 VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		p.Label, nullTime(p.ExpiresAt.at()), p.TrafficLimit, p.MaxIPs, p.MaxConns, subToken, format(now),
		nullString(p.ExternalID))
	if err != nil {
		return User{}, nil, fmt.Errorf("создание пользователя: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, nil, err
	}

	// Всё в одной транзакции: подписчик без единого набора доступа —
	// бесполезная запись, которую продавцу пришлось бы чинить руками.
	var (
		issued []Issued
		creds  []Credential
	)
	for _, kind := range kinds {
		stored, shown, err := newSecret(kind)
		if err != nil {
			return User{}, nil, err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO credentials (user_id, kind, secret, label, created_at) VALUES (?, ?, ?, '', ?)`,
			id, kind, stored, format(now))
		if err != nil {
			return User{}, nil, fmt.Errorf("создание набора %s: %w", kind, err)
		}
		credID, err := res.LastInsertId()
		if err != nil {
			return User{}, nil, err
		}
		issued = append(issued, Issued{ID: credID, Kind: kind, Secret: shown})
		creds = append(creds, Credential{ID: credID, Kind: kind, Secret: stored, CreatedAt: now})
	}

	if err := tx.Commit(); err != nil {
		return User{}, nil, err
	}

	user := User{
		ID: id, Label: p.Label, Enabled: true, ExpiresAt: p.ExpiresAt.at(),
		TrafficLimit: p.TrafficLimit, MaxIPs: p.MaxIPs, MaxConns: p.MaxConns,
		SubToken: subToken, CreatedAt: now, Credentials: creds,
	}
	return user, issued, nil
}

// UpdateUserParams — изменяемые поля. nil означает «не трогать».
type UpdateUserParams struct {
	Label        *string `json:"label,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
	ExpiresAt    *Expiry `json:"expires_at,omitempty"`
	TrafficLimit *int64  `json:"traffic_limit,omitempty"`
	MaxIPs       *int    `json:"max_ips,omitempty"`
	MaxConns     *int    `json:"max_conns,omitempty"`
}

// UpdateUser меняет заданные поля подписчика.
func (s *Store) UpdateUser(ctx context.Context, id int64, p UpdateUserParams) (User, error) {
	sets := make([]string, 0, 6)
	args := make([]any, 0, 7)

	if p.Label != nil {
		sets = append(sets, "label = ?")
		args = append(args, *p.Label)
	}
	if p.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolInt(*p.Enabled))
	}
	if p.ExpiresAt != nil {
		sets = append(sets, "expires_at = ?")
		args = append(args, format(p.ExpiresAt.Time.UTC()))
	}
	if p.TrafficLimit != nil {
		sets = append(sets, "traffic_limit = ?")
		args = append(args, *p.TrafficLimit)
	}
	if p.MaxIPs != nil {
		sets = append(sets, "max_ips = ?")
		args = append(args, *p.MaxIPs)
	}
	if p.MaxConns != nil {
		sets = append(sets, "max_conns = ?")
		args = append(args, *p.MaxConns)
	}
	if len(sets) == 0 {
		return s.GetUser(ctx, id)
	}

	args = append(args, id)
	query := "UPDATE users SET " + join(sets, ", ") + " WHERE id = ?"
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return User{}, fmt.Errorf("обновление пользователя: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrNotFound
	}
	return s.GetUser(ctx, id)
}

// DeleteUser удаляет подписчика вместе с его ключами и статистикой.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("удаление пользователя: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetUser читает подписчика вместе с ключами и расходом.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	list, err := s.queryUsers(ctx, `WHERE u.id = ?`, id)
	if err != nil {
		return User{}, err
	}
	if len(list) == 0 {
		return User{}, ErrNotFound
	}
	return list[0], nil
}

// UserBySubToken находит подписчика по токену подписки.
func (s *Store) UserBySubToken(ctx context.Context, token string) (User, error) {
	list, err := s.queryUsers(ctx, `WHERE u.sub_token = ?`, token)
	if err != nil {
		return User{}, err
	}
	if len(list) == 0 {
		return User{}, ErrNotFound
	}
	return list[0], nil
}

// ListUsers возвращает всех подписчиков.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return s.queryUsers(ctx, `ORDER BY u.id`)
}

func (s *Store) queryUsers(ctx context.Context, where string, args ...any) ([]User, error) {
	query := `
		SELECT u.id, u.label, u.enabled, u.expires_at, u.traffic_limit, u.max_ips,
		       u.max_conns, u.sub_token, u.created_at, u.external_id,
		       COALESCE((SELECT SUM(up + down) FROM usage WHERE user_id = u.id), 0)
		FROM users u ` + where

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("чтение пользователей: %w", err)
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var (
			u         User
			enabled   int
			expires   sql.NullString
			createdAt string
			external  sql.NullString
		)
		if err := rows.Scan(&u.ID, &u.Label, &enabled, &expires, &u.TrafficLimit,
			&u.MaxIPs, &u.MaxConns, &u.SubToken, &createdAt, &external, &u.Used); err != nil {
			return nil, err
		}
		u.Enabled = enabled != 0
		u.ExpiresAt = parseNullTime(expires)
		u.CreatedAt = parse(createdAt)
		u.ExternalID = external.String
		list = append(list, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range list {
		creds, err := s.credentials(ctx, list[i].ID)
		if err != nil {
			return nil, err
		}
		list[i].Credentials = creds
	}
	return list, nil
}

func (s *Store) credentials(ctx context.Context, userID int64) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, secret, label, created_at FROM credentials WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("чтение ключей: %w", err)
	}
	defer rows.Close()

	var list []Credential
	for rows.Next() {
		var (
			c         Credential
			createdAt string
		)
		if err := rows.Scan(&c.ID, &c.Kind, &c.Secret, &c.Label, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = parse(createdAt)
		list = append(list, c)
	}
	return list, rows.Err()
}

// CreateNodeParams — что нужно, чтобы завести ноду.
type CreateNodeParams struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	SNI       string `json:"sni"`
	PublicKey string `json:"public_key"`

	RealityPublicKey string `json:"reality_public_key"`
	RealityShortID   string `json:"reality_short_id"`
	WSPath           string `json:"ws_path"`
}

// CreateNode регистрирует ноду и выдаёт ей токен.
// Токен возвращается один раз, в базе лежит только его хеш.
func (s *Store) CreateNode(ctx context.Context, p CreateNodeParams) (Node, string, error) {
	if p.PublicKey != "" {
		if _, err := vp1.DecodeKey(p.PublicKey); err != nil {
			return Node{}, "", fmt.Errorf("публичный ключ ноды: %w", err)
		}
	}

	token, err := NewToken()
	if err != nil {
		return Node{}, "", err
	}
	now := time.Now().UTC()

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO nodes (name, address, sni, public_key, reality_public_key, reality_short_id, ws_path, token_hash, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		p.Name, p.Address, p.SNI, p.PublicKey, p.RealityPublicKey, p.RealityShortID, p.WSPath, HashToken(token), format(now))
	if err != nil {
		return Node{}, "", fmt.Errorf("создание ноды: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Node{}, "", err
	}

	return Node{ID: id, Name: p.Name, Address: p.Address, SNI: p.SNI,
		PublicKey: p.PublicKey, RealityPublicKey: p.RealityPublicKey, RealityShortID: p.RealityShortID,
		WSPath: p.WSPath, Enabled: true, CreatedAt: now}, token, nil
}

// ListNodes возвращает все ноды.
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, address, sni, public_key, reality_public_key, reality_short_id, ws_path, enabled, last_seen, created_at FROM nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("чтение нод: %w", err)
	}
	defer rows.Close()

	var list []Node
	for rows.Next() {
		var (
			n         Node
			enabled   int
			lastSeen  sql.NullString
			createdAt string
		)
		if err := rows.Scan(&n.ID, &n.Name, &n.Address, &n.SNI, &n.PublicKey,
			&n.RealityPublicKey, &n.RealityShortID, &n.WSPath, &enabled, &lastSeen, &createdAt); err != nil {
			return nil, err
		}
		n.Enabled = enabled != 0
		n.LastSeen = parseNullTime(lastSeen)
		n.CreatedAt = parse(createdAt)
		list = append(list, n)
	}
	return list, rows.Err()
}

// DeleteNode удаляет ноду вместе с её статистикой.
func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("удаление ноды: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AuthenticateNode находит ноду по её токену и отмечает, что она на связи.
func (s *Store) AuthenticateNode(ctx context.Context, token string) (Node, error) {
	var (
		n         Node
		enabled   int
		lastSeen  sql.NullString
		createdAt string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, address, sni, public_key, reality_public_key, reality_short_id, ws_path, enabled, last_seen, created_at
		 FROM nodes WHERE token_hash = ?`, HashToken(token)).
		Scan(&n.ID, &n.Name, &n.Address, &n.SNI, &n.PublicKey,
			&n.RealityPublicKey, &n.RealityShortID, &n.WSPath, &enabled, &lastSeen, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, err
	}

	n.Enabled = enabled != 0
	n.LastSeen = parseNullTime(lastSeen)
	n.CreatedAt = parse(createdAt)

	if _, err := s.db.ExecContext(ctx, `UPDATE nodes SET last_seen = ? WHERE id = ?`,
		format(time.Now().UTC()), n.ID); err != nil {
		return Node{}, err
	}
	return n, nil
}

// NodeUsers собирает список пользователей для конкретной ноды в том же виде,
// в каком нода читает его из файла.
//
// Главная тонкость — общая квота при нескольких нодах. Каждая нода считает
// только свой трафик, поэтому лимит для неё уменьшается на то, что человек уже
// израсходовал на других. Иначе квоту в 100 ГБ можно было бы потратить на
// каждой ноде отдельно.
func (s *Store) NodeUsers(ctx context.Context, nodeID int64) ([]users.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.label, u.enabled, u.expires_at, u.traffic_limit, u.max_ips, u.max_conns,
		       c.kind, c.secret,
		       COALESCE((SELECT SUM(up + down) FROM usage WHERE user_id = u.id AND node_id <> ?), 0)
		FROM users u
		JOIN credentials c ON c.user_id = u.id
		ORDER BY u.id, c.id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("сборка списка для ноды: %w", err)
	}
	defer rows.Close()

	var list []users.User
	for rows.Next() {
		var (
			userID    int64
			label     string
			enabled   int
			expires   sql.NullString
			limit     int64
			maxIPs    int
			maxConns  int
			kind      string
			secret    string
			elsewhere int64
		)
		if err := rows.Scan(&userID, &label, &enabled, &expires, &limit, &maxIPs, &maxConns,
			&kind, &secret, &elsewhere); err != nil {
			return nil, err
		}

		u := users.User{
			Kind:     kind,
			Secret:   secret,
			Label:    label,
			Enabled:  enabled != 0,
			MaxIPs:   maxIPs,
			MaxConns: maxConns,
			// Все наборы одного подписчика попадают в один аккаунт: квота,
			// срок и лимит устройств у телефона, ноутбука и записи для
			// чужого приложения общие.
			Account: strconv.FormatInt(userID, 10),
		}
		if t := parseNullTime(expires); t != nil {
			u.ExpiresAt = *t
		}

		if limit > 0 {
			remaining := limit - elsewhere
			if remaining <= 0 {
				// Ноль в поле лимита означает «без ограничений», поэтому
				// исчерпавшего квоту выключаем целиком, а не шлём лимит 0.
				u.Enabled = false
				u.TrafficLimit = 0
			} else {
				u.TrafficLimit = remaining
			}
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// ReportUsage принимает от ноды её счётчики, разложенные по аккаунтам.
//
// Нода присылает накопленные с её стороны итоги, а не приращения: они
// переживают её перезапуск, и повторная доставка того же отчёта ничего не
// испортит. Панель просто складывает итоги всех нод.
func (s *Store) ReportUsage(ctx context.Context, nodeID int64, report map[string]users.Usage) error {
	if len(report) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := format(time.Now().UTC())
	for account, usage := range report {
		userID, err := strconv.ParseInt(account, 10, 64)
		if err != nil {
			// Отчёт не от нашей панели: нода работала по локальному файлу,
			// там идентификатор аккаунта — сам публичный ключ.
			continue
		}

		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			// Пользователя удалили, пока нода копила отчёт. Не ошибка.
			continue
		}
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage (user_id, node_id, up, down, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, node_id) DO UPDATE SET up = excluded.up, down = excluded.down, updated_at = excluded.updated_at`,
			userID, nodeID, usage.Up, usage.Down, now); err != nil {
			return fmt.Errorf("запись расхода: %w", err)
		}
	}
	return tx.Commit()
}

func format(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parse(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseNullTime(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t := parse(v.String)
	return &t
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return format(*t)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// ErrAlreadyExists — подписчик с таким ключом продавца уже заведён.
var ErrAlreadyExists = errors.New("подписчик с таким external_id уже есть")

// nullString превращает пустую строку в NULL.
//
// Для уникального индекса это принципиально: NULL в SQLite повторяться может,
// а пустая строка нет. Иначе второй подписчик без ключа продавца упёрся бы в
// уникальность на пустом месте.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UserByExternalID находит подписчика по ключу продавца.
func (s *Store) UserByExternalID(ctx context.Context, externalID string) (User, error) {
	list, err := s.queryUsers(ctx, `WHERE u.external_id = ?`, externalID)
	if err != nil {
		return User{}, err
	}
	if len(list) == 0 {
		return User{}, ErrNotFound
	}
	return list[0], nil
}

// ExtendUser продлевает подписку на заданный срок.
//
// Считается от текущего окончания, а не от «сейчас»: покупатель, продливший за
// неделю до конца, не должен терять эту неделю. Если срок уже вышел или его не
// было вовсе — считаем от текущего момента.
//
// Чтение и запись идут одной транзакцией, и это не украшательство. Продление
// в три раздельных шага «прочитать, посчитать, записать» ломается на двух
// одновременных платежах: оба прочитали бы один и тот же срок, оба записали бы
// одно и то же значение, и один оплаченный месяц пропал бы бесследно.
func (s *Store) ExtendUser(ctx context.Context, id int64, d time.Duration) (User, error) {
	if d <= 0 {
		return User{}, errors.New("срок продления должен быть положительным")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var current sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT expires_at FROM users WHERE id = ?`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}

	// Считаем от большего из двух: текущего окончания и «сейчас». Первое —
	// чтобы не съесть остаток у того, кто продлевает заранее. Второе — чтобы
	// вернувшийся через полгода не купил месяц, истёкший пять месяцев назад.
	base := time.Now().UTC()
	if current.Valid {
		if t := parse(current.String); t.After(base) {
			base = t
		}
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET expires_at = ? WHERE id = ?`, format(base.Add(d)), id)
	if err != nil {
		return User{}, fmt.Errorf("продление подписки: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrNotFound
	}

	list, err := s.queryUsers(ctx, `WHERE u.id = ?`, id)
	if err != nil {
		return User{}, err
	}
	if len(list) == 0 {
		return User{}, ErrNotFound
	}
	return list[0], nil
}

// Expiry — срок подписки в запросах к API.
//
// Принимает и дату в RFC3339, и «через сколько»: 30d, 12h. Второе — то, чем
// думает бот: он продаёт месяц, а не «до двадцать третьего сентября». Обратно
// всегда уезжает дата — «через месяц» в ответе было бы неправдой уже к моменту,
// когда бот его прочитает.
type Expiry struct{ time.Time }

// UnmarshalJSON разбирает обе записи срока.
func (e *Expiry) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return errors.New("срок подписки: нужна строка вида 30d либо дата RFC3339")
	}
	t, err := ParseExpiry(s)
	if err != nil {
		return err
	}
	if t == nil {
		return errors.New("срок подписки: пустая строка")
	}
	e.Time = *t
	return nil
}

// MarshalJSON отдаёт срок датой.
func (e Expiry) MarshalJSON() ([]byte, error) { return json.Marshal(e.Time) }

// at превращает срок в указатель на время, понимая отсутствие срока.
func (e *Expiry) at() *time.Time {
	if e == nil {
		return nil
	}
	t := e.Time.UTC()
	return &t
}
